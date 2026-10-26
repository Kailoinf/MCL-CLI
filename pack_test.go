package main

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/Kailoinf/MCL-CLI/internal/logx"
	"github.com/Kailoinf/MCL-CLI/internal/modrinth"
)

func TestPickPackVersion(t *testing.T) {
	versions := []modrinth.Version{
		{ID: "new", Name: "Fabulously Optimized 15.0.0-beta.2", VersionNumber: "15.0.0-beta.2"},
		{ID: "old", Name: "Fabulously Optimized 15.0.0-beta.1", VersionNumber: "15.0.0-beta.1"},
	}
	cases := []struct {
		selector string
		want     string
		ok       bool
	}{
		{"", "new", true},                                   // newest by default
		{"15.0.0-beta.1", "old", true},                      // by version number
		{"old", "old", true},                                // by id
		{"Fabulously Optimized 15.0.0-beta.1", "old", true}, // by name
		{"  ", "new", true},                                 // whitespace is not a selector
		{"nonexistent", "", false},
	}
	for _, c := range cases {
		got, ok := pickPackVersion(versions, c.selector)
		if ok != c.ok || (ok && got.ID != c.want) {
			t.Errorf("selector %q: got %q ok=%v, want %q ok=%v", c.selector, got.ID, ok, c.want, c.ok)
		}
	}
}

func TestDropPackFilesSparesWhatChangedSinceThePack(t *testing.T) {
	dir := t.TempDir()
	modsDir := filepath.Join(dir, "mods")
	if err := os.MkdirAll(modsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	packFile := filepath.Join(modsDir, "dropped.jar")
	edited := filepath.Join(modsDir, "edited.jar")
	unrecorded := filepath.Join(modsDir, "handmade.jar")
	if err := os.WriteFile(packFile, []byte("the pack's file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(edited, []byte("someone replaced it"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unrecorded, []byte("dropped in by hand"), 0o644); err != nil {
		t.Fatal(err)
	}
	// what the pack recorded: it installed both files, with their digests at the
	// time. `edited.jar` no longer matches.
	recorded := map[string]string{
		"mods/dropped.jar": sha1Of(t, packFile),
		"mods/edited.jar":  "0000000000000000000000000000000000000000",
	}

	app := &App{Log: logx.Discard()}
	err := app.dropPackFiles(dir, []string{
		"mods/dropped.jar",  // the pack's own file, not wanted any more
		"mods/edited.jar",   // changed since: someone else's now
		"mods/handmade.jar", // never recorded by any pack
		"mods/absent.jar",   // already gone
		"../escape.jar",     // must never be resolved at all
		"/etc/passwd",       // nor this
	}, recorded)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(packFile); !os.IsNotExist(err) {
		t.Errorf("the pack's own file should have been removed")
	}
	for _, keep := range []string{edited, unrecorded} {
		if _, err := os.Stat(keep); err != nil {
			t.Errorf("%s must be kept: %v", filepath.Base(keep), err)
		}
	}
}

// sha1Of is the digest the test pretends the pack declared.
func sha1Of(t *testing.T, path string) string {
	t.Helper()
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha1.Sum(blob)
	return hex.EncodeToString(sum[:])
}
