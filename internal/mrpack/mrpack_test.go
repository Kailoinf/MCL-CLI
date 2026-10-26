package mrpack

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kailoinf/MCL-CLI/internal/dl"
)

// writePack builds a tiny .mrpack: the index, one client file, one server-only
// file, an override and a client override.
func writePack(t *testing.T, index string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "pack.mrpack")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	add := func(name, body string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	add("modrinth.index.json", index)
	// real zips store their folders as entries of their own, which is exactly the
	// shape that used to break the overrides step
	add("overrides/", "")
	add("overrides/config/", "")
	add("client-overrides/", "")
	add("overrides/config/tuned.toml", "kept = true\n")
	add("client-overrides/options.txt", "guiScale:2\n")
	add("overrides/world/level.dat", "worlddata")
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

const sampleIndex = `{
  "formatVersion": 1,
  "name": "Test Pack",
  "versionId": "abc123",
  "summary": "a pack",
  "dependencies": {"minecraft": "1.21.4", "fabric-loader": "0.16.9"},
  "files": [
    {"path": "mods/client-mod.jar", "hashes": {"sha1": "aa11", "sha512": "bb22"}, "fileSize": 1234,
     "downloads": ["https://cdn.modrinth.com/data/x/client-mod.jar"], "env": {"client": "required", "server": "unsupported"}},
    {"path": "mods/server-mod.jar", "hashes": {"sha1": "cc33"}, "fileSize": 4321,
     "downloads": ["https://cdn.modrinth.com/data/x/server-mod.jar"], "env": {"client": "unsupported", "server": "required"}},
    {"path": "config/plain.toml", "hashes": {"sha1": "dd44"}, "fileSize": 10,
     "downloads": ["https://cdn.modrinth.com/data/x/plain.toml"], "env": {"client": "optional"}}
  ]
}`

func TestOpenReadsTheIndex(t *testing.T) {
	idx, err := Open(writePack(t, sampleIndex))
	if err != nil {
		t.Fatal(err)
	}
	if idx.Name != "Test Pack" || idx.VersionID != "abc123" {
		t.Errorf("index = %+v", idx)
	}
	if idx.GameVersion() != "1.21.4" {
		t.Errorf("game version = %q", idx.GameVersion())
	}
	loader, version := idx.Loader()
	if loader != "fabric" || version != "0.16.9" {
		t.Errorf("loader = %q %q", loader, version)
	}
}

func TestOpenRejectsWhatIsNotAPack(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "not-a-pack.zip")
	if err := os.WriteFile(plain, []byte("nothing to see"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(plain); err == nil {
		t.Error("a file that is not a zip must not be read as a pack")
	}
	if _, err := Open(writePack(t, `{"name":"x","versionId":"y","files":[]}`)); err == nil {
		t.Error("an index without files must be refused")
	}
	if _, err := Open(writePack(t, `{"name":"x","versionId":"y","files":[{"path":"a.jar"}]}`)); err == nil {
		t.Error("an index without a minecraft version must be refused")
	}
}

func TestItemsSkipServerOnlyFilesAndStayInsideTheInstance(t *testing.T) {
	idx, err := Open(writePack(t, sampleIndex))
	if err != nil {
		t.Fatal(err)
	}
	instance := t.TempDir()
	items, err := Items(idx, instance)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2 (the server-only file must stay out)", len(items))
	}
	want := filepath.Join(instance, "mods", "client-mod.jar")
	if items[0].Dest != want || items[0].SHA1 != "aa11" || items[0].Size != 1234 {
		t.Errorf("first item = %+v", items[0])
	}
	if got := TotalBytes(items); got != 1244 {
		t.Errorf("total = %d, want 1244", got)
	}
}

func TestDestinationRefusesToEscape(t *testing.T) {
	instance := t.TempDir()
	for _, bad := range []string{"../outside.jar", "mods/../../outside.jar", "/../etc/passwd"} {
		if _, err := Destination(instance, bad); err == nil {
			t.Errorf("path %q must be refused", bad)
		}
	}
	if _, err := Destination(instance, "mods/ok.jar"); err != nil {
		t.Errorf("a normal path must be accepted: %v", err)
	}
}

func TestExtractOverridesKeepsExistingFiles(t *testing.T) {
	pack := writePack(t, sampleIndex)
	instance := t.TempDir()

	// someone already tuned this config by hand: it must survive the pack
	existing := filepath.Join(instance, "config", "tuned.toml")
	if err := os.MkdirAll(filepath.Dir(existing), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existing, []byte("kept = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	written, kept, err := ExtractOverrides(pack, instance)
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 1 || kept[0] != "config/tuned.toml" {
		t.Errorf("kept = %v, want the existing config", kept)
	}
	if len(written) != 2 {
		t.Errorf("written = %v, want options.txt and world/level.dat", written)
	}
	// and the folder entries must not have been reported as anything
	for _, w := range written {
		if w == "" || strings.HasSuffix(w, "/") {
			t.Errorf("a folder entry was treated as a file: %q", w)
		}
	}
	body, err := os.ReadFile(existing)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "kept = false") {
		t.Errorf("the owner's file was overwritten: %q", body)
	}
	if _, err := os.Stat(filepath.Join(instance, "options.txt")); err != nil {
		t.Errorf("client-overrides were not extracted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(instance, "world", "level.dat")); err != nil {
		t.Errorf("overrides were not extracted: %v", err)
	}
	// and a second run must not re-report the files it already wrote
	written, kept, err = ExtractOverrides(pack, instance)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 0 || len(kept) != 3 {
		t.Errorf("second run: written %v, kept %v", written, kept)
	}
}

func TestFileClientWanted(t *testing.T) {
	cases := map[string]bool{
		"required": true, "optional": true, "": true,
		"unsupported": false, "false": false,
	}
	for env, want := range cases {
		f := File{Env: map[string]string{"client": env}}
		if got := f.ClientWanted(); got != want {
			t.Errorf("client=%q wanted=%v, want %v", env, got, want)
		}
	}
}

var _ = dl.Item{}
