package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gopaytech/pergent/internal/result"
	"github.com/gopaytech/pergent/internal/review"
)

// reviewerFind is a blocking finding of a reviewer.
var reviewerFind = result.Finding{Fingerprint: "fa1", File: "a.go", Line: 3, Severity: "blocking", Title: "Nil map write", Explanation: "e"}

// reviewSkill is a reviewer's review of skill "review" with the given findings.
func reviewSkill(fs ...result.Finding) result.Skill {
	s := result.Skill{Name: "review", Verdict: string(review.NoBlockingIssues), Findings: fs,
		Files: result.Files{Reviewed: []string{"a.go"}}, Batches: 1, BatchesFinished: 1}
	for _, f := range fs {
		if f.Severity == "blocking" {
			s.Verdict = string(review.HasBlockingIssues)
		}
	}
	return s
}

// reviewerCompleted is a completed reviewer result with the given skills.
func reviewerCompleted(skills ...result.Skill) result.Result {
	return result.Result{SchemaVersion: result.SchemaVersion, Status: result.StatusCompleted, Skills: skills}
}

// writeReviewer writes r, encoded, to name in a directory outside the checkout.
func writeReviewer(t *testing.T, name string, r result.Result) string {
	t.Helper()
	data, err := result.Encode(r)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// A result whose base or head differs is not a review of this change.
func TestReviewerMismatch(t *testing.T) {
	r := &result.Result{Base: "b1", Head: "h1"}
	if got := reviewerMismatch(r, "b1", "h1"); got != "" {
		t.Errorf("same change: %q", got)
	}
	if got := reviewerMismatch(r, "b2", "h1"); got != `base "b1", want "b2"` {
		t.Errorf("other base: %q", got)
	}
	if got := reviewerMismatch(r, "b1", "h2"); got != `head "h1", want "h2"` {
		t.Errorf("other head: %q", got)
	}
}

func TestReadReviewer(t *testing.T) {
	ok := writeReviewer(t, "ok.json", reviewerCompleted(reviewSkill(reviewerFind)))
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(t.TempDir(), "big.json")
	if err := os.WriteFile(big, bytes.Repeat([]byte(" "), result.MaxSize+1), 0o600); err != nil {
		t.Fatal(err)
	}
	odd := reviewerCompleted()
	odd.Status = "weird"
	oddPath := writeReviewer(t, "odd.json", odd)

	if r, reason := readReviewer(ok); r == nil || reason != "" || len(r.Skills) != 1 {
		t.Errorf("ok: r %v, reason %q", r, reason)
	}
	for _, c := range []struct{ path, want string }{
		{bad, "decoding"},
		{big, "over the 10 MB limit"},
		{oddPath, `status "weird"`},
		{t.TempDir(), "not a regular file"},
		{filepath.Join(t.TempDir(), "missing.json"), "no such file or directory"},
	} {
		if r, reason := readReviewer(c.path); r != nil || !strings.Contains(reason, c.want) {
			t.Errorf("%s: r %v, reason %q, want %q", c.path, r, reason, c.want)
		}
	}
}

// A FIFO is refused before it is opened: opening one blocks until a writer
// comes.
func TestReadReviewerFIFODoesNotBlock(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "reviewer.json")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	done := make(chan string, 1)
	go func() {
		_, reason := readReviewer(fifo)
		done <- reason
	}()
	select {
	case reason := <-done:
		if !strings.Contains(reason, "not a regular file") {
			t.Errorf("reason = %q", reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("readReviewer blocked on a FIFO")
	}
}

func TestReviewerSkills(t *testing.T) {
	s := reviewSkill(reviewerFind)
	s.Confidence, s.ConfidenceReason = 4, "small"
	s.Notes = []string{"a note"}
	s.Files.Deleted = []string{"a.go"}
	s.Files.Skipped = []result.Skipped{{Path: "go.sum", Rule: "lockfile"}}
	odd := reviewSkill()
	odd.Name, odd.Verdict = "odd", "maybe"
	unfinished := reviewSkill()
	unfinished.Name, unfinished.Batches = "unfinished", 2
	twice := reviewSkill()
	twice.Name = "twice"
	twiceFailed := twice
	twiceFailed.Verdict = string(review.Failed)
	got := reviewerSkills(&result.Result{Skills: []result.Skill{s, odd, unfinished, twice, twiceFailed, twiceFailed, twice}})
	res, ok := got["review"]
	if !ok || res.Skill != "review" || res.Verdict != review.HasBlockingIssues || len(res.Findings) != 1 {
		t.Fatalf("review = %+v", res)
	}
	f := res.Findings[0]
	if f.Fingerprint != "fa1" || f.File != "a.go" || f.Line != 3 || f.Severity != "blocking" || f.Title != "Nil map write" {
		t.Errorf("finding = %+v", f)
	}
	if res.Confidence != 4 || res.ConfidenceReason != "small" || !slices.Equal(res.Notes, []string{"a note"}) ||
		!slices.Equal(res.Reviewed, []string{"a.go"}) || !slices.Equal(res.Deleted, []string{"a.go"}) ||
		len(res.Skipped) != 1 || res.Batches != 1 || res.BatchesFinished != 1 {
		t.Errorf("review = %+v", res)
	}
	// A verdict pergent does not know can't be trusted: the skill is failed.
	if got["odd"].Verdict != review.Failed {
		t.Errorf("odd verdict = %s, want failed", got["odd"].Verdict)
	}
	// A batch that did not finish, with no failure reported, is no pass.
	if got["unfinished"].Verdict != review.Failed {
		t.Errorf("unfinished verdict = %s, want failed", got["unfinished"].Verdict)
	}
	// A skill the result holds twice can't be trusted, whichever came first.
	if got["twice"].Verdict != review.Failed {
		t.Errorf("twice verdict = %s, want failed", got["twice"].Verdict)
	}
	if again := reviewerSkills(&result.Result{Skills: []result.Skill{twiceFailed, twice}}); again["twice"].Verdict != review.Failed {
		t.Errorf("failed first: verdict = %s, want failed", again["twice"].Verdict)
	}
}

// TestReviewerSkillNotReviewedIsFailed: a file the reviewer did not review, with
// no failure and every batch finished, still makes the skill failed.
func TestReviewerSkillNotReviewedIsFailed(t *testing.T) {
	s := reviewSkill(reviewerFind)
	s.Files.NotReviewed = []result.NotReviewed{{Path: "a.go", Cause: "the batch failed"}}
	if got := reviewerSkill(s); got.Verdict != review.Failed {
		t.Errorf("verdict = %s, want failed", got.Verdict)
	}
}
