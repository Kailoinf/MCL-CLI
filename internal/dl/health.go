package dl

import (
	"sync"

	"github.com/Kailoinf/MCL-CLI/internal/mirror"
)

// SourceHealth remembers that a download source is not working.
//
// BMCLAPI answers with 302 to redirector nodes that occasionally stall for the
// full watchdog timeout, and a Java runtime is 700-odd small files: paying 20
// seconds of stall per file turns a two-minute install into an hour. When the
// preferred source fails repeatedly the candidate order is flipped for the rest
// of the run — the official source is tried first, and the mirror stays in the
// list as the fallback, so a machine that can only reach the mirror still gets
// its files.
//
// The zero value is unusable; build one with NewSourceHealth and share it across
// every Policy in a run (App does exactly that).
type SourceHealth struct {
	mu        sync.Mutex
	threshold int
	failures  map[string]int
	unhealthy map[string]bool
}

// NewSourceHealth returns a tracker that demotes a source after threshold
// consecutive failures (values below 1 mean 3).
func NewSourceHealth(threshold int) *SourceHealth {
	if threshold < 1 {
		threshold = 3
	}
	return &SourceHealth{
		threshold: threshold,
		failures:  map[string]int{},
		unhealthy: map[string]bool{},
	}
}

// Fail records one failed attempt of a source. It returns true the first time
// that source is declared unhealthy, so the caller can log the decision once.
func (h *SourceHealth) Fail(source string) bool {
	if h == nil || source == "" {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.failures[source]++
	if h.failures[source] >= h.threshold && !h.unhealthy[source] {
		h.unhealthy[source] = true
		return true
	}
	return false
}

// OK records a success: the source's failure count goes back to zero. An
// unhealthy source is not promoted automatically — the order only has to stay
// stable for the rest of the run.
func (h *SourceHealth) OK(source string) {
	if h == nil || source == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.failures[source] = 0
}

// Unhealthy reports whether a source has been demoted.
func (h *SourceHealth) Unhealthy(source string) bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.unhealthy[source]
}

// Order returns the candidates with demoted sources moved to the end, keeping
// the original order inside each group.
func (h *SourceHealth) Order(cands []mirror.Candidate) []mirror.Candidate {
	if h == nil || len(cands) < 2 {
		return cands
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	var good, bad []mirror.Candidate
	for _, c := range cands {
		if h.unhealthy[c.Source] {
			bad = append(bad, c)
			continue
		}
		good = append(good, c)
	}
	if len(bad) == 0 {
		return cands
	}
	return append(good, bad...)
}
