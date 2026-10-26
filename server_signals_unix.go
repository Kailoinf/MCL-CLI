//go:build !windows

package main

import (
	"os"
	"syscall"
)

// foregroundSignals are the signals that must not leave a server running behind
// mcli's back: a terminal's Ctrl+C (SIGINT), a `timeout` or a service manager
// (SIGTERM), and a closing terminal (SIGHUP). All three are forwarded to the
// server so it saves its world and stops, exactly as Ctrl+C in its own console
// would.
func foregroundSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}
}
