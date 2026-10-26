package mcv

import (
	"path/filepath"
	"testing"
)

// vanillaNativesJSON is the plain Mojang form: one directory, no subdirectories.
func vanillaNativesJSON() *Version {
	return &Version{
		Arguments: &Arguments{JVM: []ArgValue{
			{Value: []string{"-Djava.library.path=${natives_directory}"}},
			{Value: []string{"-Djna.tmpdir=${natives_directory}"}},
			{Value: []string{"-cp", "${classpath}"}},
		}},
	}
}

// profileNativesJSON is what PCL/HMCL produce for a modern modded profile.
func profileNativesJSON() *Version {
	return &Version{
		Arguments: &Arguments{JVM: []ArgValue{
			{Value: []string{"-Djava.library.path=${natives_directory}/java"}},
			{Value: []string{"-Djna.tmpdir=${natives_directory}/jna"}},
			{Value: []string{"-Dorg.lwjgl.system.SharedLibraryExtractPath=${natives_directory}/lwjgl"}},
			{Value: []string{"-Dio.netty.native.workdir=${natives_directory}/netty"}},
		}},
	}
}

func TestNativesTargetsVanilla(t *testing.T) {
	dir := filepath.Join("/mc", "versions", "1.21.4", "natives")
	got := vanillaNativesJSON().NativesTargets(dir)
	if len(got) != 1 || got[0] != dir {
		t.Fatalf("vanilla targets = %v, want just %s", got, dir)
	}
}

func TestNativesTargetsProfile(t *testing.T) {
	dir := filepath.Join("/mc", "versions", "26.3-Fabric 0.20.1", "natives")
	got := profileNativesJSON().NativesTargets(dir)
	want := []string{
		filepath.Join(dir, "java"),
		filepath.Join(dir, "jna"),
		filepath.Join(dir, "lwjgl"),
		filepath.Join(dir, "netty"),
	}
	if len(got) != len(want) {
		t.Fatalf("targets = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("targets = %v, want %v", got, want)
		}
	}
}

func TestNativesTargetsIgnoresForeignDirectories(t *testing.T) {
	dir := filepath.Join("/mc", "versions", "x", "natives")
	v := &Version{Arguments: &Arguments{JVM: []ArgValue{
		{Value: []string{"-Djava.library.path=/somewhere/else:/tmp"}},
		{Value: []string{"-Dorg.lwjgl.system.SharedLibraryExtractPath=${natives_directory}/lwjgl"}},
	}}}
	got := v.NativesTargets(dir)
	if len(got) != 1 || got[0] != filepath.Join(dir, "lwjgl") {
		t.Fatalf("targets = %v, want only the directory under the natives dir", got)
	}
}

func TestNativesTargetsSplitOnSemicolon(t *testing.T) {
	dir := "/mc/versions/x/natives"
	v := &Version{Arguments: &Arguments{JVM: []ArgValue{
		{Value: []string{"-Djava.library.path=${natives_directory};${natives_directory}/java"}},
	}}}
	got := v.NativesTargets(dir)
	if len(got) != 2 {
		t.Fatalf("targets = %v, want two entries", got)
	}
}

func TestNativeDirsFor(t *testing.T) {
	dir := "/mc/versions/x/natives"
	targets := []string{
		filepath.Join(dir, "java"),
		filepath.Join(dir, "jna"),
		filepath.Join(dir, "lwjgl"),
		filepath.Join(dir, "netty"),
	}
	cases := []struct {
		lib  string
		want []string
	}{
		{"org.lwjgl:lwjgl:3.4.3:natives-linux", []string{dir, filepath.Join(dir, "lwjgl")}},
		{"net.java.dev.jna:jna:5.17.0:natives-linux", []string{dir, filepath.Join(dir, "jna")}},
		{"io.netty:netty-transport-native-epoll:4.2.16.Final:natives-linux", []string{dir, filepath.Join(dir, "netty")}},
		{"com.mojang:jtracy:1.14.38:natives-linux", []string{dir, filepath.Join(dir, "java")}},
	}
	for _, c := range cases {
		got := nativeDirsFor(targets, dir, c.lib)
		if len(got) != len(c.want) {
			t.Errorf("%s -> %v, want %v", c.lib, got, c.want)
			continue
		}
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Errorf("%s -> %v, want %v", c.lib, got, c.want)
				break
			}
		}
	}
	// a version without subdirectories keeps the flat layout only
	got := nativeDirsFor([]string{dir}, dir, "org.lwjgl:lwjgl:3.4.3:natives-linux")
	if len(got) != 1 || got[0] != dir {
		t.Errorf("flat layout -> %v, want [%s]", got, dir)
	}
}
