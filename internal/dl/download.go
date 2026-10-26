// Package dl downloads files from an ordered candidate list, verifying size
// and SHA1, retrying on failure and prompting before large transfers.
package dl

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Kailoinf/MCL-CLI/internal/i18n"
	"github.com/Kailoinf/MCL-CLI/internal/logx"
	"github.com/Kailoinf/MCL-CLI/internal/mirror"
)

// ConfirmFunc asks the user whether a transfer is allowed. It is only called
// for files at or above Policy.LargeMB.
type ConfirmFunc func(desc string, size int64) (bool, error)

// Policy configures retries, timeouts and prompting.
type Policy struct {
	Retries int
	Timeout time.Duration
	LargeMB int
	Confirm ConfirmFunc
	Log     *logx.Logger
	// Health demotes a repeatedly failing source for the rest of a run.
	Health    *SourceHealth
	stallTime time.Duration
}

// Item is one file to fetch.
type Item struct {
	// Candidates are tried in order. When every candidate fails the item
	// fails.
	Candidates []mirror.Candidate
	// Dest is the absolute destination path.
	Dest string
	// SHA1 is the expected lowercase hex digest; empty means "no check".
	SHA1 string
	// Size is the expected size in bytes; 0 means "unknown".
	Size int64
	// Desc is the short human name used in prompts and logs.
	Desc string
	// Perm is the file mode for the destination (default 0644).
	Perm os.FileMode
}

// ErrUserDeclined is returned when the user says no to a large download.
var ErrUserDeclined = errors.New("download declined")

// Verify reports whether an existing file already satisfies the item.
func (it Item) Verify() bool {
	st, err := os.Stat(it.Dest)
	if err != nil {
		return false
	}
	if it.Size > 0 && st.Size() != it.Size {
		return false
	}
	if it.SHA1 == "" {
		return st.Size() > 0
	}
	sum, err := fileSHA1(it.Dest)
	if err != nil {
		return false
	}
	return strings.EqualFold(sum, it.SHA1)
}

func fileSHA1(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Fetch downloads one item, resuming nothing (files are small enough) and
// writing through a .part file.
func (p Policy) Fetch(it Item) error {
	if it.Perm == 0 {
		it.Perm = 0o644
	}
	if it.Verify() {
		p.log().Debugf("%s", i18n.T("dl.skip_existing", it.Desc))
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(it.Dest), 0o755); err != nil {
		return err
	}
	// LargeMB semantics: 0 means "confirm everything", any positive number is the
	// MiB threshold, negative disables confirmation entirely.
	if p.Confirm != nil && p.LargeMB >= 0 && it.Size >= int64(p.LargeMB)*1024*1024 {
		ok, err := p.Confirm(it.Desc, it.Size)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: %s", ErrUserDeclined, it.Desc)
		}
	}

	attempts := p.Retries
	if attempts < 1 {
		attempts = 1
	}
	cands := p.Health.Order(it.Candidates)
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		for _, c := range cands {
			if err := p.fetchOnce(it, c); err != nil {
				if p.Health.Fail(c.Source) {
					p.log().Warnf("%s", i18n.T("dl.source_unhealthy", c.Source))
				}
				lastErr = err
				p.log().Warnf("%s", i18n.T("dl.failed_source", it.Desc, c.Source, err))
				continue
			}
			p.Health.OK(c.Source)
			p.log().Debugf("%s", i18n.T("dl.complete", it.Desc, c.Source))
			return nil
		}
		if attempt < attempts {
			p.log().Infof("%s", i18n.T("dl.retrying", it.Desc, attempt, attempts))
			time.Sleep(time.Duration(attempt) * time.Second)
		}
	}
	return fmt.Errorf("%s", i18n.T("dl.exhausted", it.Desc, attempts, lastErr))
}

func (p Policy) fetchOnce(it Item, c mirror.Candidate) error {
	part := it.Dest + ".part"
	_ = os.Remove(part)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", UserAgent)

	resp, err := p.streamClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	f, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	total := it.Size
	if total == 0 && resp.ContentLength > 0 {
		total = resp.ContentLength
	}
	prog := newProgress(p.log(), it.Desc, total)
	body := resp.Body
	if p.stallTimeout() > 0 {
		body = newStallReader(body, p.stallTimeout(), cancel)
	}

	h := sha1.New()
	if _, err := io.Copy(io.MultiWriter(f, h, prog), body); err != nil {
		prog.done()
		_ = os.Remove(part)
		return err
	}
	prog.done()
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if it.Size > 0 {
		if st, err := os.Stat(part); err == nil && st.Size() != it.Size {
			_ = os.Remove(part)
			return fmt.Errorf("%s", i18n.T("dl.size_mismatch", it.Size, st.Size()))
		}
	}
	if it.SHA1 != "" {
		got := hex.EncodeToString(h.Sum(nil))
		if !strings.EqualFold(got, it.SHA1) {
			_ = os.Remove(part)
			return fmt.Errorf("%s", i18n.T("dl.sha_mismatch", it.SHA1, got))
		}
		p.log().Debugf("%s", i18n.T("dl.sha_ok", it.Desc))
	}
	if err := os.Rename(part, it.Dest); err != nil {
		_ = os.Remove(part)
		return err
	}
	return nil
}

// FetchAll downloads every item using up to workers goroutines. It stops
// scheduling new items after the first error and returns that error.
func FetchAll(p Policy, items []Item, workers int, log *logx.Logger) error {
	if len(items) == 0 {
		return nil
	}
	if workers <= 0 {
		workers = 6
	}
	if workers > len(items) {
		workers = len(items)
	}
	if log == nil {
		log = logx.Discard()
	}
	ch := make(chan Item)
	var wg sync.WaitGroup
	var failed int32
	var mu sync.Mutex
	var firstErr error
	var done int64

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for it := range ch {
				if atomic.LoadInt32(&failed) != 0 {
					continue
				}
				if err := p.Fetch(it); err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
					atomic.StoreInt32(&failed, 1)
					continue
				}
				n := atomic.AddInt64(&done, 1)
				if n%50 == 0 {
					log.Infof("  %d/%d", n, int64(len(items)))
				}
			}
		}()
	}
	for _, it := range items {
		if atomic.LoadInt32(&failed) != 0 {
			break
		}
		ch <- it
	}
	close(ch)
	wg.Wait()
	return firstErr
}

// FetchBytes returns the body of the first candidate that works, up to 32 MiB.
func (p Policy) FetchBytes(cands []mirror.Candidate, desc string) ([]byte, string, error) {
	var lastErr error
	attempts := p.Retries
	if attempts < 1 {
		attempts = 1
	}
	cands = p.Health.Order(cands)
	for attempt := 1; attempt <= attempts; attempt++ {
		for _, c := range cands {
			body, err := p.getBytes(c)
			if err == nil {
				p.Health.OK(c.Source)
				return body, c.Source, nil
			}
			if p.Health.Fail(c.Source) {
				p.log().Warnf("%s", i18n.T("dl.source_unhealthy", c.Source))
			}
			lastErr = err
			p.log().Warnf("%s", i18n.T("dl.failed_source", desc, c.Source, err))
		}
		if attempt < attempts {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
	}
	// The cause is kept wrapped, not just printed: a caller has to be able to tell a
	// refused key from a dead network without matching on message text.
	return nil, "", FetchError{Desc: desc, Err: lastErr}
}

// FetchError is what a fetch that ran out of candidates reports. Unwrap exposes the
// last cause, so errors.As still finds an HTTPStatusError inside it.
type FetchError struct {
	Desc string
	Err  error
}

func (e FetchError) Error() string { return i18n.T("dl.fetch_failed", e.Desc, e.Err) }

func (e FetchError) Unwrap() error { return e.Err }

func (p Policy) getBytes(c mirror.Candidate) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), p.timeout())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	for name, value := range c.Headers {
		req.Header.Set(name, value)
	}
	resp, err := p.streamClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, HTTPStatusError{Code: resp.StatusCode, Source: c.Source}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	return data, nil
}

// HTTPStatusError is a response the server refused. It is a type rather than a
// string so a caller can tell "your key was rejected" (403) from "the network is
// down" without matching on message text.
type HTTPStatusError struct {
	Code   int
	Source string
}

func (e HTTPStatusError) Error() string {
	if e.Source != "" {
		return fmt.Sprintf("HTTP %d from %s", e.Code, e.Source)
	}
	return fmt.Sprintf("HTTP %d", e.Code)
}

// UserAgent is sent with every request.
const UserAgent = "mcli/0.2 (+https://github.com/Kailoinf/MCL-CLI)"

func (p Policy) timeout() time.Duration {
	if p.Timeout > 0 {
		return p.Timeout
	}
	return 30 * time.Second
}

func (p Policy) stallTimeout() time.Duration {
	if p.stallTime > 0 {
		return p.stallTime
	}
	return 20 * time.Second
}

func (p Policy) log() *logx.Logger {
	if p.Log != nil {
		return p.Log
	}
	return logx.Discard()
}

// streamClient has no whole-request timeout: big jars on a slow mirror would
// be killed mid-transfer. Stalls are handled by the reader watchdog instead.
func (p Policy) streamClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ResponseHeaderTimeout: p.timeout(),
			TLSHandshakeTimeout:   15 * time.Second,
			IdleConnTimeout:       60 * time.Second,
			MaxIdleConns:          16,
			MaxIdleConnsPerHost:   8,
		},
	}
}

// ---------------------------------------------------------------------------
// stall watchdog

// stallReader cancels the request when no bytes arrive for d.
type stallReader struct {
	r      io.Reader
	d      time.Duration
	cancel context.CancelFunc
	mu     sync.Mutex
	timer  *time.Timer
}

func newStallReader(r io.Reader, d time.Duration, cancel context.CancelFunc) *stallReader {
	s := &stallReader{r: r, d: d, cancel: cancel}
	s.mu.Lock()
	s.timer = time.AfterFunc(d, cancel)
	s.mu.Unlock()
	return s
}

func (s *stallReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if n > 0 {
		s.mu.Lock()
		if s.timer != nil {
			s.timer.Reset(s.d)
		}
		s.mu.Unlock()
	}
	if err != nil {
		s.mu.Lock()
		if s.timer != nil {
			s.timer.Stop()
			s.timer = nil
		}
		s.mu.Unlock()
	}
	return n, err
}

func (s *stallReader) Close() error {
	s.mu.Lock()
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	s.mu.Unlock()
	if c, ok := s.r.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// ---------------------------------------------------------------------------
// progress rendering

type progress struct {
	log   *logx.Logger
	desc  string
	total int64
	n     int64
	last  time.Time
	live  bool
}

func newProgress(log *logx.Logger, desc string, total int64) *progress {
	return &progress{log: log, desc: desc, total: total, live: log.IsTTY()}
}

func (p *progress) Write(b []byte) (int, error) {
	p.n += int64(len(b))
	if !p.live {
		return len(b), nil
	}
	if time.Since(p.last) < 200*time.Millisecond {
		return len(b), nil
	}
	p.last = time.Now()
	p.render()
	return len(b), nil
}

func (p *progress) render() {
	if p.total > 0 {
		pct := float64(p.n) / float64(p.total) * 100
		p.log.Raw("\r  %-28s %6.1f%%  %s/%s", truncate(p.desc, 28), pct, human(p.n), human(p.total))
		return
	}
	p.log.Raw("\r  %-28s %s", truncate(p.desc, 28), human(p.n))
}

func (p *progress) done() {
	if p.live {
		p.render()
		p.log.Raw("\n")
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func human(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	units := []string{"K", "M", "G", "T"}
	v := float64(n)
	i := -1
	for v >= unit && i < len(units)-1 {
		v /= unit
		i++
	}
	return fmt.Sprintf("%.1f%s", v, units[i])
}
