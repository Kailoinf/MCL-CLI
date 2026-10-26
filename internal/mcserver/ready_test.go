package mcserver

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A server's log still holds the previous run until the new one rotates it, so a
// "Done (" left there by that run must not be read as readiness — that is exactly
// how console input got forwarded too early.
func TestWaitReadyIgnoresThePreviousRunsLog(t *testing.T) {
	dir := t.TempDir()
	logs := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(logs, "latest.log")
	old := []byte("[23:59:59] [Server thread/INFO]: Done (12.345s)! For help, type \"help\"\n")
	if err := os.WriteFile(path, old, 0o644); err != nil {
		t.Fatal(err)
	}
	// make the file older than the moment the wait begins
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}

	if WaitReady(dir, time.Now(), time.Second) {
		t.Fatal("the previous run's Done was read as readiness")
	}

	// the new run writes it, and now it counts
	if err := os.WriteFile(path, append(old, []byte("[00:00:01] [Server thread/INFO]: Done (0.5s)! For help, type \"help\"\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if !WaitReady(dir, time.Now().Add(-time.Minute), 5*time.Second) {
		t.Fatal("a fresh Done line was not noticed")
	}
}

func TestWaitReadyGivesUpOnItsOwn(t *testing.T) {
	dir := t.TempDir()
	start := time.Now()
	if WaitReady(dir, start, 500*time.Millisecond) {
		t.Fatal("a server with no log cannot be ready")
	}
	if time.Since(start) < 400*time.Millisecond {
		t.Error("WaitReady returned before its timeout")
	}
}
