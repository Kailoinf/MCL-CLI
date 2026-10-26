//go:build windows

package main

import "os"

// foregroundSignals on Windows is Ctrl+C and nothing else: Windows has no SIGTERM
// to catch, which is also why a detached server is stopped with `--force` there.
func foregroundSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}
