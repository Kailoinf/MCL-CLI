package mcv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAssetIndexName(t *testing.T) {
	cases := []struct {
		v    *Version
		want string
	}{
		{&Version{AssetIndex: &Download{Path: "19.json"}}, "19.json"},
		{&Version{AssetIndex: &Download{URL: "https://piston-meta.mojang.com/v1/packages/abc/34.json"}}, "34.json"},
		{&Version{Assets: "legacy"}, "legacy.json"},
	}
	for _, c := range cases {
		if got := assetIndexName(c.v); got != c.want {
			t.Errorf("assetIndexName = %q want %q", got, c.want)
		}
	}
}

func TestLibraryDestPrefersKnownPath(t *testing.T) {
	l := Library{Name: "org.lwjgl.lwjgl:lwjgl-platform:2.9.4"}
	d := Download{Path: "org/lwjgl/lwjgl-platform/2.9.4-native-linux/lwjgl-platform-2.9.4-native-linux.jar"}
	got := libraryDest("/libs", d, l, "natives-linux")
	want := filepath.Join("/libs", "org/lwjgl/lwjgl-platform/2.9.4-native-linux/lwjgl-platform-2.9.4-native-linux.jar")
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
	// without a path we fall back to the maven layout
	got = libraryDest("/libs", Download{}, l, "natives-linux")
	want = filepath.Join("/libs", "org/lwjgl/lwjgl/lwjgl-platform/2.9.4/lwjgl-platform-2.9.4-natives-linux.jar")
	if got != want {
		t.Errorf("fallback got %q want %q", got, want)
	}
}

func TestReadAssetIndex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "19.json")
	body := `{"objects":{"icons/icon_16x16.png":{"hash":"5ff04807c356f1beed0b86ccf659b44b9983e3fa","size":781}}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := ReadAssetIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Objects) != 1 {
		t.Fatalf("objects = %d", len(idx.Objects))
	}
	o := idx.Objects["icons/icon_16x16.png"]
	if o.Size != 781 || len(o.Hash) != 40 {
		t.Errorf("object parsed wrong: %+v", o)
	}
}
