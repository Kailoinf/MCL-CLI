package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// server start offers a big jar before fetching it, and the download policy offers the
// same file again: the question used to reach the screen twice, so one `y` was not
// enough to answer it. One answer per file per run is the rule.
func TestTheSameLargeFileIsAskedAboutOnce(t *testing.T) {
	t.Setenv("MCLI_HOME", t.TempDir())
	t.Setenv("MCLI_MINECRAFT", filepath.Join(t.TempDir(), "mc"))
	a, err := newApp(newFlags())
	if err != nil {
		t.Fatal(err)
	}
	a.Store.Cfg().ConfirmLargeDownload = true
	a.Store.Cfg().LargeDownloadMB = 1

	// one line of input, and only one: a second question would read EOF and be
	// answered "no"
	in, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := in.WriteString("y\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := in.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	oldIn, oldOut := os.Stdin, os.Stdout
	defer func() { os.Stdin, os.Stdout = oldIn, oldOut }()
	os.Stdin = in
	out, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = out

	const desc, size = "server 26.3", int64(59) << 20
	if ok, err := a.confirmFile(desc, size); err != nil || !ok {
		t.Fatalf("first answer: ok=%v err=%v", ok, err)
	}
	if ok, err := a.confirmFile(desc, size); err != nil || !ok {
		t.Errorf("the same file was asked again: ok=%v err=%v", ok, err)
	}
	// a different file is a different question, and this one gets no input left
	other, _ := a.confirmFile("another.jar", size)
	if other {
		t.Error("a different file must still be asked about")
	}

	os.Stdout = oldOut
	raw, err := os.ReadFile(out.Name())
	if err != nil {
		t.Fatal(err)
	}
	asked := strings.Count(string(raw), "继续下载") + strings.Count(string(raw), "continue")
	if asked != 2 {
		t.Errorf("questions asked = %d, want 2 (the file once, the other one once):\n%s", asked, raw)
	}
}
