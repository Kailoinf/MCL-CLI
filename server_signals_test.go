//go:build !windows

package main

import (
	"errors"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// signalServer is what `server stop` and the foreground signal forwarding use. What
// it must guarantee is *which* signal arrives: SIGINT, the same one a console sends
// on Ctrl+C, so a server can save before it exits. SIGKILL would end the process
// without that chance, and SIGTERM is what the shell's own `timeout` sends.
//
// The stand-in here is a shell, which exits with the signal rather than catching it
// — that is the shell's business. A real server catching it is verified against a
// real server in docs/status.md.
func TestSignalServerAsksTheProcessToStop(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("no shell to stand in for a server: %v", err)
	}
	proc := exec.Command(sh, "-c", "sleep 30")
	if err := proc.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = proc.Process.Kill() }()

	if err := signalServer(proc.Process.Pid); err != nil {
		t.Fatalf("signalServer: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- proc.Wait() }()
	select {
	case err := <-done:
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			if sig, ok := ee.Sys().(syscall.WaitStatus); ok && sig.Signaled() {
				if sig.Signal() != syscall.SIGINT {
					t.Fatalf("the target was ended with %v, not a console interrupt", sig.Signal())
				}
				return // delivered as SIGINT, which is the property under test
			}
		}
		if err != nil {
			t.Fatalf("the process did not end cleanly: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the process ignored the console interrupt")
	}
}

func TestKillProcessEndsItTheHardWay(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no shell")
	}
	proc := exec.Command(sh, "-c", "sleep 30")
	if err := proc.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = proc.Process.Kill() }()
	if err := killProcess(proc.Process.Pid); err != nil {
		t.Fatalf("killProcess: %v", err)
	}
	done := make(chan struct{}, 1)
	go func() { _, _ = proc.Process.Wait(); done <- struct{}{} }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("--force did not end the process")
	}
}
