package dl

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kailoinf/MCL-CLI/internal/logx"
	"github.com/Kailoinf/MCL-CLI/internal/mirror"
)

func sha1hex(b []byte) string {
	sum := sha1.Sum(b)
	return hex.EncodeToString(sum[:])
}

// newServer serves:
//
//	/good   -> the payload
//	/bad    -> 500 always (simulates a dead mirror)
//	/flaky  -> 500 once, then the payload
func newServer(t *testing.T, payload []byte) (*httptest.Server, *int32) {
	t.Helper()
	var flakyHits int32
	mux := http.NewServeMux()
	mux.HandleFunc("/good", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	})
	mux.HandleFunc("/bad", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	})
	mux.HandleFunc("/flaky", func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&flakyHits, 1) == 1 {
			http.Error(w, "try again", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(payload)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &flakyHits
}

func testPolicy() Policy {
	return Policy{Retries: 2, Timeout: 5 * time.Second, LargeMB: 10, Log: logx.Discard()}
}

func TestFetchUsesFallbackCandidate(t *testing.T) {
	payload := []byte("library bytes")
	srv, _ := newServer(t, payload)
	dest := filepath.Join(t.TempDir(), "lib.jar")
	err := testPolicy().Fetch(Item{
		Candidates: []mirror.Candidate{
			{URL: srv.URL + "/bad", Source: "bmclapi"},
			{URL: srv.URL + "/good", Source: "mojang"},
		},
		Dest: dest,
		SHA1: sha1hex(payload),
		Size: int64(len(payload)),
		Desc: "test library",
	})
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Errorf("content = %q", got)
	}
	if _, err := os.Stat(dest + ".part"); err == nil {
		t.Error(".part file left behind")
	}
}

func TestFetchRetriesAfterTransientError(t *testing.T) {
	payload := []byte("retry me")
	srv, hits := newServer(t, payload)
	dest := filepath.Join(t.TempDir(), "lib.jar")
	err := testPolicy().Fetch(Item{
		Candidates: []mirror.Candidate{{URL: srv.URL + "/flaky", Source: "bmclapi"}},
		Dest:       dest,
		SHA1:       sha1hex(payload),
		Desc:       "flaky library",
	})
	if err != nil {
		t.Fatalf("retry did not recover: %v", err)
	}
	if atomic.LoadInt32(hits) < 2 {
		t.Errorf("expected at least 2 attempts, got %d", *hits)
	}
}

func TestFetchRejectsBadChecksum(t *testing.T) {
	payload := []byte("real content")
	srv, _ := newServer(t, payload)
	dest := filepath.Join(t.TempDir(), "lib.jar")
	err := testPolicy().Fetch(Item{
		Candidates: []mirror.Candidate{{URL: srv.URL + "/good", Source: "bmclapi"}},
		Dest:       dest,
		SHA1:       strings.Repeat("0", 40), // wrong on purpose
		Desc:       "corrupt library",
	})
	if err == nil {
		t.Fatal("a checksum mismatch must fail the download")
	}
	if !strings.Contains(err.Error(), "SHA1") {
		t.Errorf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(dest); statErr == nil {
		t.Error("a file that failed verification must not be left at the destination")
	}
	if _, statErr := os.Stat(dest + ".part"); statErr == nil {
		t.Error(".part file left behind after a failed verification")
	}
}

func TestVerifyShortCircuitsNetwork(t *testing.T) {
	payload := []byte("already here")
	dir := t.TempDir()
	dest := filepath.Join(dir, "lib.jar")
	if err := os.WriteFile(dest, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	// no server needed: a matching file must skip the download entirely
	err := testPolicy().Fetch(Item{
		Candidates: []mirror.Candidate{{URL: "http://127.0.0.1:1/good", Source: "bmclapi"}},
		Dest:       dest,
		SHA1:       sha1hex(payload),
		Size:       int64(len(payload)),
		Desc:       "cached library",
	})
	if err != nil {
		t.Fatalf("existing valid file must not trigger a download: %v", err)
	}
	if !(Item{Dest: dest, SHA1: sha1hex(payload)}).Verify() {
		t.Error("Verify should accept the file")
	}
	if (Item{Dest: dest, SHA1: strings.Repeat("a", 40)}).Verify() {
		t.Error("Verify must reject a wrong checksum")
	}
}

func TestFetchBytesFallback(t *testing.T) {
	payload := []byte(`{"latest":{"release":"26.3"}}`)
	srv, _ := newServer(t, payload)
	p := testPolicy()
	data, source, err := p.FetchBytes([]mirror.Candidate{
		{URL: srv.URL + "/bad", Source: "bmclapi"},
		{URL: srv.URL + "/good", Source: "mojang"},
	}, "version manifest")
	if err != nil {
		t.Fatalf("FetchBytes failed: %v", err)
	}
	if source != "mojang" {
		t.Errorf("source = %q", source)
	}
	if string(data) != string(payload) {
		t.Errorf("bytes = %q", data)
	}
	if _, _, err := p.FetchBytes([]mirror.Candidate{{URL: srv.URL + "/bad", Source: "bmclapi"}}, "x"); err == nil {
		t.Error("all-candidates-failing must return an error")
	}
}

func TestFetchAllRunsInParallelAndReportsFailure(t *testing.T) {
	payload := []byte("parallel")
	srv, _ := newServer(t, payload)
	dir := t.TempDir()
	var items []Item
	for i := 0; i < 12; i++ {
		items = append(items, Item{
			Candidates: []mirror.Candidate{{URL: srv.URL + "/good", Source: "mojang"}},
			Dest:       filepath.Join(dir, fmt.Sprintf("f%02d.jar", i)),
			SHA1:       sha1hex(payload),
			Desc:       "parallel file",
		})
	}
	if err := FetchAll(testPolicy(), items, 4, logx.Discard()); err != nil {
		t.Fatalf("FetchAll failed: %v", err)
	}
	for _, it := range items {
		if _, err := os.Stat(it.Dest); err != nil {
			t.Errorf("%s not written: %v", it.Dest, err)
		}
	}
	items = append(items, Item{
		Candidates: []mirror.Candidate{{URL: srv.URL + "/bad", Source: "bmclapi"}},
		Dest:       filepath.Join(dir, "fails.jar"),
		Desc:       "always fails",
	})
	if err := FetchAll(testPolicy(), items, 4, logx.Discard()); err == nil {
		t.Error("FetchAll must surface the first error")
	}
}

func TestErrUserDeclinedWhenConfirmSaysNo(t *testing.T) {
	payload := []byte("big file")
	srv, _ := newServer(t, payload)
	p := testPolicy()
	p.LargeMB = 0 // everything counts as large
	p.Confirm = func(desc string, size int64) (bool, error) { return false, nil }
	err := p.Fetch(Item{
		Candidates: []mirror.Candidate{{URL: srv.URL + "/good", Source: "mojang"}},
		Dest:       filepath.Join(t.TempDir(), "big.jar"),
		Desc:       "big file",
	})
	if err == nil || !strings.Contains(err.Error(), "declined") {
		t.Errorf("expected a declined error, got %v", err)
	}
}
