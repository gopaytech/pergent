package main

import (
	"bytes"
	"errors"
	"log"
	"os"
	"strings"
	"testing"
	"time"
)

// lookupPlatform is a Platform whose FindComment fails its first `failures`
// calls, then finds comment 77.
type lookupPlatform struct {
	failures int
	calls    int
}

func (p *lookupPlatform) FetchDiff() (string, []string, error) { return "", nil, nil }
func (p *lookupPlatform) CreateComment(string) error           { return nil }
func (p *lookupPlatform) UpdateComment(int64, string) error    { return nil }
func (p *lookupPlatform) FindComment(string) (int64, string, error) {
	p.calls++
	if p.calls <= p.failures {
		return 0, "", errors.New("status 502")
	}
	return 77, "<!-- pergent -->\n", nil
}

func TestCommentToUpdate_RetriesAFailedLookupOnce(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	defer log.SetOutput(os.Stderr)

	p := &lookupPlatform{failures: 1}
	if got := commentToUpdate(p, "<!-- pergent -->", time.Millisecond); got != 77 {
		t.Errorf("commentToUpdate = %d, want 77 from the retry", got)
	}
	if p.calls != 2 {
		t.Errorf("FindComment called %d times, want 2", p.calls)
	}
	if strings.Contains(logs.String(), "duplicate") {
		t.Errorf("log = %q, want no duplicate warning when the retry worked", logs.String())
	}
}

func TestCommentToUpdate_StillFailingPostsAndSaysADuplicateMayAppear(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	defer log.SetOutput(os.Stderr)

	p := &lookupPlatform{failures: 5}
	if got := commentToUpdate(p, "<!-- pergent -->", time.Millisecond); got != 0 {
		t.Errorf("commentToUpdate = %d, want 0 (post a new comment)", got)
	}
	if p.calls != 2 {
		t.Errorf("FindComment called %d times, want 2 (one retry, no more)", p.calls)
	}
	if !strings.Contains(logs.String(), "duplicate") {
		t.Errorf("log = %q, want a clear warning that a duplicate may appear", logs.String())
	}
}

func TestCommentToUpdate_WaitsTheDelayBeforeTheRetry(t *testing.T) {
	p := &lookupPlatform{failures: 1}
	start := time.Now()
	commentToUpdate(p, "<!-- pergent -->", 50*time.Millisecond)
	if d := time.Since(start); d < 50*time.Millisecond {
		t.Errorf("retried after %s, want at least the 50ms delay", d)
	}
}

func TestCommentToUpdate_NoRetryWhenTheFirstLookupWorks(t *testing.T) {
	p := &lookupPlatform{}
	if got := commentToUpdate(p, "<!-- pergent -->", time.Hour); got != 77 || p.calls != 1 {
		t.Errorf("got id %d after %d calls, want 77 after 1", got, p.calls)
	}
}
