package mcserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kailoinf/MCL-CLI/internal/java"
	"github.com/Kailoinf/MCL-CLI/internal/mcv"
	"github.com/Kailoinf/MCL-CLI/internal/mirror"
)

func serverVersion(withServer bool) *mcv.Version {
	v := &mcv.Version{ID: "1.8.9", Type: "release"}
	v.Downloads = map[string]mcv.Download{}
	if withServer {
		v.Downloads["server"] = mcv.Download{
			Path: "server.jar", URL: "https://piston-data.mojang.com/v1/objects/abc/server.jar",
			SHA1: "abc", Size: 1234,
		}
	}
	return v
}

func TestJarItemNeedsAServerDownload(t *testing.T) {
	dir := t.TempDir()
	o := Options{Dir: dir, Version: serverVersion(true), VersionID: "1.8.9"}
	item, ok := JarItem(o, mirror.Mapper{})
	if !ok {
		t.Fatal("a version with a server download must produce an item")
	}
	if item.Dest != filepath.Join(dir, "server.jar") {
		t.Errorf("destination = %s", item.Dest)
	}
	if item.SHA1 != "abc" || item.Size != 1234 {
		t.Errorf("hash/size lost: %+v", item)
	}
	if item.Desc != "server 1.8.9" {
		t.Errorf("description = %q", item.Desc)
	}

	o.Version = serverVersion(false)
	if _, ok := JarItem(o, mirror.Mapper{}); ok {
		t.Error("a version without a server must not produce an item")
	}
	o.Version = nil
	if _, ok := JarItem(o, mirror.Mapper{}); ok {
		t.Error("no version, no item")
	}
}

func TestPropertiesCarriesThePortAndFallsBack(t *testing.T) {
	p := Properties(25566)
	if !strings.Contains(p, "server-port=25566") {
		t.Errorf("port missing: %q", p)
	}
	// a nonsense port must not produce a broken properties file
	for _, bad := range []int{0, -1, 70000} {
		if !strings.Contains(Properties(bad), "server-port=25565") {
			t.Errorf("port %d did not fall back to 25565: %q", bad, Properties(bad))
		}
	}
}

func TestEulaTextNamesTheLicence(t *testing.T) {
	if !strings.Contains(EulaText, EulaURL) || !strings.Contains(EulaText, "eula=true") {
		t.Fatalf("eula.txt must name the URL and accept: %q", EulaText)
	}
}

func TestWriteFilesNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	o := Options{Dir: dir, Port: 25566}
	wrote, err := WriteFiles(o, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(wrote) != 2 {
		t.Fatalf("wrote %v, want both files", wrote)
	}

	// a world owner's own properties must survive the next start
	custom := filepath.Join(dir, "server.properties")
	if err := os.WriteFile(custom, []byte("server-port=1234\nmotd=mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wrote, err = WriteFiles(o, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(wrote) != 0 {
		t.Errorf("existing files were rewritten: %v", wrote)
	}
	body, err := os.ReadFile(custom)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "server-port=1234") {
		t.Errorf("the owner's properties file was overwritten: %q", body)
	}
}

func TestPrepBuildsAMinimalCommandLine(t *testing.T) {
	dir := t.TempDir()
	o := Options{Dir: dir, Version: serverVersion(true), VersionID: "1.8.9", Xmx: "2G", Xms: "512M"}
	cmd := Prep(o, java.Runtime{Path: "/usr/bin/java", Major: 8})
	want := []string{"-Xmx2G", "-Xms512M", "-jar", filepath.Join(dir, "server.jar"), "nogui"}
	if len(cmd.Args) != len(want) {
		t.Fatalf("args = %v, want %v", cmd.Args, want)
	}
	for i := range want {
		if cmd.Args[i] != want[i] {
			t.Errorf("arg %d = %q, want %q", i, cmd.Args[i], want[i])
		}
	}
	if cmd.Dir != dir {
		t.Errorf("working directory = %q, want %q", cmd.Dir, dir)
	}
	// a path with a space has to survive the rendering
	o.Dir = filepath.Join(dir, "my server")
	cmd = Prep(o, java.Runtime{Path: "/usr/bin/java"})
	if !strings.Contains(cmd.CommandLine(), "'"+filepath.Join(o.Dir, "server.jar")+"'") {
		t.Errorf("quoted jar missing from %s", cmd.CommandLine())
	}
}
