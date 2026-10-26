//go:build windows

package mcserver

import "os/exec"

// detachProcess keeps a Windows server running after mcli exits. The default
// behaviour is already "no job object", and a console interrupt is not part of
// the contract there, so nothing has to be arranged.
func detachProcess(cmd *exec.Cmd) {}
