//go:build windows

package mcserver

import (
	"errors"
	"os"
)

// errNoFifo is returned instead of pretending a console can be attached.
var errNoFifo = errors.New("attaching to a server console needs a FIFO, which this system does not provide")

// ConsolePipesSupported reports whether mcli can attach to a detached server's
// console here.
func ConsolePipesSupported() bool { return false }

// OpenConsoleInput is unavailable on Windows.
func OpenConsoleInput(dir string) (*os.File, error) { return nil, errNoFifo }

// OpenConsoleWriter is unavailable on Windows.
func OpenConsoleWriter(dir string) (*os.File, error) { return nil, errNoFifo }
