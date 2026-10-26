package dl

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Kailoinf/MCL-CLI/internal/mirror"
)

// cands builds a candidate list; a URL starting with "mirror" counts as the
// mirror source, anything else as the official one.
func cands(urls ...string) []mirror.Candidate {
	out := make([]mirror.Candidate, 0, len(urls))
	for _, u := range urls {
		source := "mojang"
		if strings.HasPrefix(u, "mirror") {
			source = "bmclapi"
		}
		out = append(out, mirror.Candidate{URL: u, Source: source})
	}
	return out
}

func TestSourceHealthDemotesAfterThreshold(t *testing.T) {
	h := NewSourceHealth(3)
	if h.Unhealthy("bmclapi") {
		t.Fatal("a fresh source must be healthy")
	}
	if h.Fail("bmclapi") || h.Fail("bmclapi") {
		t.Fatal("the first two failures must not demote the source")
	}
	if !h.Fail("bmclapi") {
		t.Fatal("the third failure must demote the source")
	}
	if h.Fail("bmclapi") {
		t.Fatal("a source is only reported as newly unhealthy once")
	}
	if !h.Unhealthy("bmclapi") || h.Unhealthy("mojang") {
		t.Fatal("health must be tracked per source")
	}
	// an unrelated success must not promote the demoted source
	h.OK("mojang")
	if !h.Unhealthy("bmclapi") {
		t.Fatal("success of another source must not promote bmclapi")
	}
	// its own success clears the counter for the next run of failures
	h.OK("bmclapi")
	if h.Unhealthy("bmclapi") != true {
		t.Fatal("demotion lasts for the run")
	}
}

func TestSourceHealthOrderKeepsFallback(t *testing.T) {
	h := NewSourceHealth(2)
	order := h.Order(cands("mirror/a", "mojang/a"))
	if order[0].Source != "bmclapi" {
		t.Fatalf("healthy order changed: %+v", order)
	}
	h.Fail("bmclapi")
	h.Fail("bmclapi")
	order = h.Order(cands("mirror/a", "mojang/a"))
	if order[0].Source != "mojang" || order[1].Source != "bmclapi" {
		t.Fatalf("demoted source must go last but stay: %+v", order)
	}
	if len(order) != 2 {
		t.Fatal("the fallback candidate was dropped")
	}
}

// TestFetchFlipsToTheWorkingSource is the behaviour that matters in the wild: a
// mirror that answers 500 for every file must stop costing a round trip per
// file, while the official source keeps delivering.
func TestFetchFlipsToTheWorkingSource(t *testing.T) {
	var mirrorHits, officialHits int32
	mirrorSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&mirrorHits, 1)
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer mirrorSrv.Close()
	officialSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&officialHits, 1)
		_, _ = w.Write([]byte("payload"))
	}))
	defer officialSrv.Close()

	dir := t.TempDir()
	health := NewSourceHealth(3)
	p := Policy{Retries: 1, LargeMB: -1, Health: health}
	items := make([]Item, 0, 6)
	for i := 0; i < 6; i++ {
		items = append(items, Item{
			Candidates: []mirror.Candidate{
				{URL: mirrorSrv.URL + "/f", Source: "bmclapi"},
				{URL: officialSrv.URL + "/f", Source: "mojang"},
			},
			Dest: filepath.Join(dir, "file"+string(rune('a'+i))),
			Desc: "test file",
		})
	}
	if err := FetchAll(p, items, 1, nil); err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	if got := atomic.LoadInt32(&officialHits); got != 6 {
		t.Fatalf("official source served %d files, want 6", got)
	}
	if got := atomic.LoadInt32(&mirrorHits); got != 3 {
		t.Fatalf("the dead mirror was tried %d times, want exactly the 3 that demote it", got)
	}
	for _, it := range items {
		if _, err := os.Stat(it.Dest); err != nil {
			t.Fatalf("%s missing: %v", it.Dest, err)
		}
	}
}
