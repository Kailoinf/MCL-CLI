package mcv

import (
	"encoding/json"
	"testing"
)

// profileJSON mirrors what another launcher writes: its own id, the game version in
// `clientVersion`, no `inheritsFrom`, and the loader named only by its libraries.
const pclProfile = `{
  "id": "26.3-Fabric 0.20.1",
  "clientVersion": "26.3",
  "mainClass": "net.fabricmc.loader.impl.launch.knot.KnotClient",
  "assetIndex": {"id": "34"},
  "libraries": [
    {"name": "net.fabricmc:fabric-loader:0.20.1"},
    {"name": "net.fabricmc:sponge-mixin:0.17.5+mixin.0.8.7"}
  ]
}`

const fabricProfile = `{
  "id": "fabric-loader-0.20.1-26.3",
  "inheritsFrom": "26.3",
  "mainClass": "net.fabricmc.loader.impl.launch.knot.KnotClient",
  "libraries": [{"name": "net.fabricmc:fabric-loader:0.20.1"}]
}`

const vanillaJSON = `{"id": "26.3", "mainClass": "net.minecraft.client.main.Main", "libraries": []}`

func TestGameVersionAcrossLauncherShapes(t *testing.T) {
	cases := []struct {
		name string
		json string
		want string
	}{
		{"a launcher profile that says clientVersion", pclProfile, "26.3"},
		{"a profile that inherits", fabricProfile, "26.3"},
		{"a plain version", vanillaJSON, "26.3"},
	}
	for _, c := range cases {
		v, err := parseVersionJSON(c.json)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := v.GameVersion(); got != c.want {
			t.Errorf("%s: GameVersion() = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestLoaderIsReadFromTheProfile(t *testing.T) {
	cases := []struct {
		name        string
		json        string
		wantLoader  string
		wantVersion string
	}{
		{"fabric, its own id", fabricProfile, "fabric", "0.20.1"},
		{"fabric, another launcher's id", pclProfile, "fabric", "0.20.1"},
		{"vanilla", vanillaJSON, "", ""},
	}
	for _, c := range cases {
		v, err := parseVersionJSON(c.json)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		loader, version := v.Loader()
		if loader != c.wantLoader || version != c.wantVersion {
			t.Errorf("%s: Loader() = %q %q, want %q %q", c.name, loader, version, c.wantLoader, c.wantVersion)
		}
	}
}

func TestLoaderFindsOthersByMainClass(t *testing.T) {
	// A main class only settles the question when it is unambiguous. Modded
	// Forge and NeoForge both start through BootstrapLauncher, so those two are
	// decided by the libraries below, never guessed from the class.
	for class, want := range map[string]string{
		"org.quiltmc.loader.impl.launch.knot.KnotClient":    "quilt",
		"net.fabricmc.loader.impl.launch.knot.KnotClient":   "fabric",
		"net.minecraftforge.client.loading.ClientModLoader": "forge",
	} {
		v := &Version{MainClass: class}
		if loader, _ := v.Loader(); loader != want {
			t.Errorf("main class %q: loader = %q, want %q", class, loader, want)
		}
	}
	if loader, _ := (&Version{MainClass: "cpw.mods.bootstraplauncher.BootstrapLauncher"}).Loader(); loader != "" {
		t.Errorf("an ambiguous launcher class must not be guessed: got %q", loader)
	}
}

func TestLoaderIsDecidedByTheLibrariesWhenTheClassCannot(t *testing.T) {
	cases := []struct {
		lib     Library
		loader  string
		version string
	}{
		{Library{Name: "net.neoforged:neoforge:26.3.1"}, "neoforge", "26.3.1"},
		{Library{Name: "net.minecraftforge:forge:57.0.1"}, "forge", "57.0.1"},
	}
	for _, c := range cases {
		v := &Version{MainClass: "cpw.mods.bootstraplauncher.BootstrapLauncher", Libraries: []Library{c.lib}}
		loader, version := v.Loader()
		if loader != c.loader || version != c.version {
			t.Errorf("%s: got %q %q, want %q %q", c.lib.Name, loader, version, c.loader, c.version)
		}
	}
}

// parseVersionJSON reads a version document the way lookup does, without touching
// the network: the fields these tests care about are all in the JSON.
func parseVersionJSON(body string) (*Version, error) {
	var v Version
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func TestLoaderVersionSuffixIsNotADifference(t *testing.T) {
	if loaderDiffersHelper("0.20.1", "0.20.1+build.3") {
		t.Error("a build suffix is not a different loader")
	}
	if !loaderDiffersHelper("0.19.5", "0.20.1") {
		t.Error("different versions must be reported as different")
	}
}

// loaderDiffersHelper mirrors the rule mods_cmd.go applies (the suffix rule).
func loaderDiffersHelper(a, b string) bool {
	base := func(s string) string {
		for i := 0; i < len(s); i++ {
			if s[i] == '+' {
				return s[:i]
			}
		}
		return s
	}
	return base(a) != base(b)
}
