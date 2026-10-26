//go:build !windows

package mcserver

import (
	"os/exec"
	"syscall"
)

// detachProcess puts the child in a session of its own, so closing the terminal
// or killing mcli's process group does not take the server down with it.
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
