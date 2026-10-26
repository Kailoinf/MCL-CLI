//go:build !windows

package mcserver

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// runStub is a Command whose "java" is a shell: the mechanism around starting a
// child (the pid handed to the caller, detaching, waiting) is what is under test,
// not Minecraft.
func runStub(t *testing.T, script string) Command {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("no shell to stand in for java: %v", err)
	}
	return Command{Dir: t.TempDir(), Java: sh, Args: []string{"-c", script}}
}

func TestRunReportsThePidBeforeWaiting(t *testing.T) {
	cmd := runStub(t, "sleep 30")
	started := make(chan int, 1)
	done := make(chan error, 1)
	go func() {
		_, _, err := cmd.Run(true, func(pid int) { started <- pid })
		done <- err
	}()

	var pid int
	select {
	case pid = <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("Run never reported the pid of a long-running child")
	}
	if pid <= 0 {
		t.Fatalf("pid = %d", pid)
	}
	// the child is recorded while it is still running — that is the whole point:
	// a launcher that only learns the pid after the wait cannot stop an orphan
	if err := syscall.Kill(pid, syscall.Signal(0)); err != nil {
		t.Fatalf("the reported pid is not alive: %v", err)
	}
	// stop it the graceful way and let Run finish
	if err := syscall.Kill(pid, syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after the child was signalled")
	}
}

func TestRunDetachesAndKeepsItsPid(t *testing.T) {
	dir := t.TempDir()
	cmd := Command{Dir: dir, Java: "/bin/sh", Args: []string{"-c", "echo started > " + filepath.Join(dir, "out.txt") + "; sleep 30"}}
	pid, _, err := cmd.Run(false, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = syscall.Kill(pid, syscall.SIGKILL) }()
	if pid <= 0 {
		t.Fatal("a detached run must return its pid")
	}
	if _, err := os.Stat(filepath.Join(dir, "out.txt")); err != nil {
		// the child may need a moment; the pid is what matters here
		time.Sleep(200 * time.Millisecond)
		if _, err := os.Stat(filepath.Join(dir, "out.txt")); err != nil {
			t.Errorf("the detached child did not run: %v", err)
		}
	}
}
