package mcv

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func writeZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestExtractNatives(t *testing.T) {
	dir := t.TempDir()
	jar := filepath.Join(dir, "natives.jar")
	writeZip(t, jar, map[string]string{
		"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\n",
		"liblwjgl.so":          "native-library",
		"sub/extra.dll":        "should be excluded",
		"deep/nested/a.so":     "nested",
	})
	dest := filepath.Join(dir, "natives")
	lib := Library{Name: "x", Extract: &struct {
		Exclude []string `json:"exclude"`
	}{Exclude: []string{"sub/"}}}

	if err := ExtractNatives(jar, dest, lib); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "liblwjgl.so")); err != nil {
		t.Errorf("main native missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "deep/nested/a.so")); err != nil {
		t.Errorf("nested native missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "sub/extra.dll")); err == nil {
		t.Error("extract.exclude was not honoured")
	}
	if _, err := os.Stat(filepath.Join(dest, "META-INF/MANIFEST.MF")); err == nil {
		t.Error("META-INF must be skipped")
	}
	// running twice must be a harmless no-op (size check short-circuits)
	if err := ExtractNatives(jar, dest, lib); err != nil {
		t.Fatal(err)
	}
}

func TestExtractNativesRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	jar := filepath.Join(dir, "evil.jar")
	writeZip(t, jar, map[string]string{
		"../../escaped.so": "nope",
		"ok.so":            "fine",
	})
	dest := filepath.Join(dir, "natives")
	if err := ExtractNatives(jar, dest, Library{Name: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "ok.so")); err != nil {
		t.Errorf("normal entry missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "escaped.so")); err == nil {
		t.Error("path traversal escaped the natives directory")
	}
}
