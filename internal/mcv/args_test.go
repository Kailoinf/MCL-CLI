package mcv

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestArgValueUnmarshal(t *testing.T) {
	var list []ArgValue
	raw := `[
		"-Djava.library.path=${natives_directory}",
		{"rules":[{"action":"allow","os":{"name":"windows"}}],"value":"-Dwin=1"},
		{"rules":[{"action":"allow","os":{"name":"osx"}}],"value":["-XstartOnFirstThread","-Xss1M"]}
	]`
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("want 3 entries, got %d", len(list))
	}
	if got := list[0].Value; len(got) != 1 || got[0] != "-Djava.library.path=${natives_directory}" {
		t.Errorf("string form parsed wrong: %#v", got)
	}
	if got := list[1].Value; len(got) != 1 || got[0] != "-Dwin=1" {
		t.Errorf("object string value parsed wrong: %#v", got)
	}
	if got := list[2].Value; len(got) != 2 || got[1] != "-Xss1M" {
		t.Errorf("object array value parsed wrong: %#v", got)
	}
}

func TestSubstitute(t *testing.T) {
	ph := map[string]string{"version_name": "1.21.4", "classpath": "/a:/b"}
	got, unknown := Substitute("--version ${version_name} -cp ${classpath} ${nope}", ph)
	if want := "--version 1.21.4 -cp /a:/b ${nope}"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
	if len(unknown) != 1 || unknown[0] != "nope" {
		t.Errorf("unknown placeholders not reported: %v", unknown)
	}
}

func TestLibraryPathsAndNatives(t *testing.T) {
	lib := Library{Name: "org.lwjgl:lwjgl:3.3.3:natives-linux"}
	env := linuxEnv()
	c, ok := lib.NativeClassifier(env)
	if !ok || c != "natives-linux" {
		t.Fatalf("classifier = %q ok=%v", c, ok)
	}
	if got, want := lib.MavenPath(c), "org/lwjgl/lwjgl/3.3.3/lwjgl-3.3.3-natives-linux.jar"; got != want {
		t.Errorf("maven path = %q want %q", got, want)
	}
	if !lib.IsNative(env) {
		t.Error("modern natives entry must be native")
	}

	legacy := Library{
		Name:    "org.lwjgl.lwjgl:lwjgl-platform:2.9.4",
		Natives: map[string]string{"linux": "natives-linux"},
	}
	legacy.Downloads.Artifact = &Download{Path: "org/lwjgl/lwjgl-platform/2.9.4/lwjgl-platform-2.9.4.jar"}
	legacy.Downloads.Classifiers = map[string]Download{
		"natives-linux": {Path: "org/lwjgl/lwjgl-platform/2.9.4-native-linux/lwjgl-platform-2.9.4-native-linux.jar"},
	}
	if c, ok := legacy.NativeClassifier(env); !ok || c != "natives-linux" {
		t.Errorf("legacy classifier = %q ok=%v", c, ok)
	}
	if d, ok := legacy.NativeDownload(env); !ok || !strings.Contains(d.Path, "2.9.4-native-linux") {
		t.Errorf("legacy native download = %+v ok=%v", d, ok)
	}
	if _, ok := legacy.ArtifactDownload(env); !ok {
		t.Error("a native library may still ship a plain artifact entry")
	}
}

func TestEffectiveMerge(t *testing.T) {
	parentShared := Library{Name: "com.example:shared:1.0"}
	parentOverridden := Library{Name: "com.example:overridden:1.0"}
	parentOverridden.Downloads.Artifact = &Download{Path: "com/example/overridden/1.0/overridden-1.0.jar"}
	childOverridden := Library{Name: "com.example:overridden:1.0"}
	childOverridden.Downloads.Artifact = &Download{Path: "com/example/overridden/1.0/overridden-1.0-fixed.jar"}
	childOnly := Library{Name: "com.example:child-only:1.0"}

	parent := &Version{
		ID:                 "1.21.4",
		MainClass:          "net.minecraft.client.main.Main",
		Assets:             "19",
		MinecraftArguments: "",
		Libraries:          []Library{parentShared, parentOverridden},
		Arguments: &Arguments{
			JVM:  []ArgValue{{Value: []string{"-Dparent=1"}}},
			Game: []ArgValue{{Value: []string{"--parent"}}},
		},
		Downloads: map[string]Download{"client": {URL: "https://example/client.jar", SHA1: "abc"}},
	}
	child := &Version{
		ID:           "custom-1.0",
		InheritsFrom: "1.21.4",
		Libraries:    []Library{childOverridden, childOnly},
		Arguments: &Arguments{
			Game: []ArgValue{{Value: []string{"--child"}}},
		},
	}
	child.parent = parent
	eff := child.Effective()

	if eff.ID != "custom-1.0" {
		t.Errorf("id = %q", eff.ID)
	}
	if eff.MainClass != parent.MainClass {
		t.Errorf("mainClass not inherited: %q", eff.MainClass)
	}
	if eff.Assets != "19" {
		t.Errorf("assets not inherited: %q", eff.Assets)
	}
	if _, ok := eff.Downloads["client"]; !ok {
		t.Error("downloads not inherited")
	}
	if len(eff.Libraries) != 3 {
		names := make([]string, 0, len(eff.Libraries))
		for _, l := range eff.Libraries {
			names = append(names, l.Name)
		}
		t.Fatalf("merged libraries = %d (%v), want 3", len(eff.Libraries), names)
	}
	// a same-named library from the child replaces the parent's entry, and the
	// parent's position is preserved
	if got := eff.Libraries[1].Downloads.Artifact; got == nil || !strings.HasSuffix(got.Path, "overridden-1.0-fixed.jar") {
		t.Errorf("child override not applied: %+v", eff.Libraries[1].Downloads.Artifact)
	}
	if eff.Libraries[2].Name != "com.example:child-only:1.0" {
		t.Errorf("child-only library missing: %v", eff.Libraries[2].Name)
	}
	// arguments are concatenated, parent first
	game := eff.Arguments.Game
	if len(game) != 2 || game[0].Value[0] != "--parent" || game[1].Value[0] != "--child" {
		t.Errorf("game arguments merged wrong: %+v", game)
	}
	if len(eff.Arguments.JVM) != 1 {
		t.Errorf("jvm arguments must keep the parent's when the child has none")
	}
	// libraries with distinct names are never treated as overrides
	different := mergeLibraries(
		[]Library{{Name: "com.example:overridden:1.0"}},
		[]Library{{Name: "com.example:overridden:2.0"}},
	)
	if len(different) != 2 {
		t.Errorf("same artifact at a new version must be kept: %d entries", len(different))
	}
}

func TestClasspathEntries(t *testing.T) {
	libs := []Library{
		{Name: "com.example:plain:1.0"},                // classpath
		{Name: "com.example:native:1.0:natives-linux"}, // natives, excluded
		{Name: "com.example:winonly:1.0", Rules: []Rule{{Action: "allow", OS: &RuleOS{Name: "windows"}}}}, // excluded
		{Name: "com.example:nodownload:1.0"}, // no downloads -> excluded
	}
	libs[0].Downloads.Artifact = &Download{Path: "com/example/plain/1.0/plain-1.0.jar"}
	libs[1].Downloads.Artifact = &Download{Path: "com/example/native/1.0/native-1.0-natives-linux.jar"}
	libs[2].Downloads.Artifact = &Download{Path: "com/example/winonly/1.0/winonly-1.0.jar"}

	v := &Version{ID: "x", Libraries: libs}
	entries := ClasspathEntries(v, linuxEnv(), "/libs", "/libs/versions/x/x.jar")
	if len(entries) != 2 {
		t.Fatalf("classpath = %v, want 2 entries", entries)
	}
	if entries[0] != "/libs/com/example/plain/1.0/plain-1.0.jar" {
		t.Errorf("wrong first classpath entry: %v", entries[0])
	}
	if entries[1] != "/libs/versions/x/x.jar" {
		t.Errorf("client jar must come last: %v", entries[1])
	}
	if got := ClasspathString(entries, ":"); got != "/libs/com/example/plain/1.0/plain-1.0.jar:/libs/versions/x/x.jar" {
		t.Errorf("classpath string = %q", got)
	}
}

func TestMinecraftArgumentsFallback(t *testing.T) {
	v := &Version{
		ID:                 "1.7.10",
		MinecraftArguments: "--username ${auth_player_name} --version ${version_name}",
	}
	spec := Spec{
		Version: v,
		Env:     linuxEnv(),
		Auth:    Auth{PlayerName: "Kailo"},
		Paths:   Paths{VersionName: "1.7.10", Separator: ":"},
	}
	args := spec.GameArgs()
	if len(args) != 4 || args[1] != "Kailo" || args[3] != "1.7.10" {
		t.Errorf("legacy game args = %v", args)
	}
	// legacy versions carry no jvm arguments, so mcli supplies the classic set
	jvm := spec.JVMArgs()
	if len(jvm) != 3 || jvm[0] != "-Djava.library.path=" {
		t.Errorf("legacy jvm args = %v", jvm)
	}
}
