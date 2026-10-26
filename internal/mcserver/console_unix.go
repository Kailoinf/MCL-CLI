//go:build !windows

package mcserver

import (
	"os"
	"syscall"
)

// ConsolePipesSupported reports whether mcli can attach to a detached server's
// console here: it needs a FIFO for input.
func ConsolePipesSupported() bool { return true }

// OpenConsoleInput creates the FIFO a detached server reads commands from and
// opens it read-write. Read-write is the point: opening a FIFO read-only blocks
// until some writer shows up, which would stop the server from ever starting.
func OpenConsoleInput(dir string) (*os.File, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := ConsoleInputPath(dir)
	if _, err := os.Stat(path); err != nil {
		if err := syscall.Mkfifo(path, 0o600); err != nil {
			return nil, err
		}
	}
	return os.OpenFile(path, os.O_RDWR, 0)
}

// OpenConsoleWriter opens the same FIFO to write a command into it. It opens
// read-write on purpose: a FIFO opened write-only blocks until some reader is
// there, so a caller that writes before the server has started would hang forever
// instead of reporting anything. Read-write returns at once, and the server still
// reads every line written by anyone holding the pipe.
func OpenConsoleWriter(dir string) (*os.File, error) {
	return os.OpenFile(ConsoleInputPath(dir), os.O_RDWR, 0)
}
