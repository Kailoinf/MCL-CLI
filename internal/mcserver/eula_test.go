package mcserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// WriteFiles only creates missing files, so accepting the licence has to write it:
// a server whose eula.txt says false must not keep refusing after the person agreed.
func TestAcceptEulaOverwritesARefusal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "eula.txt")
	if err := os.WriteFile(path, []byte("eula=false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if EulaAccepted(dir) {
		t.Fatal("the fixture already accepts the licence")
	}
	if err := AcceptEula(dir); err != nil {
		t.Fatal(err)
	}
	if !EulaAccepted(dir) {
		t.Fatal("eula.txt still refuses after AcceptEula")
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), EulaURL) {
		t.Errorf("the accepted file lost the licence URL:\n%s", raw)
	}
}

func TestAcceptEulaWritesIntoAMissingDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "servers", "new")
	if err := AcceptEula(dir); err != nil {
		t.Fatal(err)
	}
	if !EulaAccepted(dir) {
		t.Fatal("no eula.txt was written")
	}
}
