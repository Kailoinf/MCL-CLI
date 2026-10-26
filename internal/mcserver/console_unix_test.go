//go:build !windows

package mcserver

import (
	"testing"
	"time"
)

// Writing a command into the console must never wait for the server to exist.
// Opening a FIFO write-only blocks until something has it open for reading, which
// is exactly how `mcli server start` hung before it had started anything: the
// console pump opened the pipe first and waited forever. The writer opens
// read-write, which cannot block.
func TestConsoleWriterDoesNotBlockWithoutAReader(t *testing.T) {
	dir := t.TempDir()
	// create the pipe, then let go of it: no reader is left
	create, err := OpenConsoleInput(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := create.Close(); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		w, err := OpenConsoleWriter(dir)
		if err != nil {
			done <- err
			return
		}
		done <- w.Close()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("opening the console writer: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("opening the console writer blocked with no reader: a launcher would hang before starting the server")
	}
}

func TestConsoleInputCreatesThePipeOnce(t *testing.T) {
	dir := t.TempDir()
	first, err := OpenConsoleInput(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	// a second open must reuse the same pipe rather than fail on an existing file
	second, err := OpenConsoleInput(dir)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer second.Close()
	if ConsoleInputPath(dir) != ConsoleInputPath(dir) {
		t.Fatal("unstable path")
	}
}
