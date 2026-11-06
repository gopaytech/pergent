package review

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gopaytech/pergent/internal/finding"
	"github.com/gopaytech/pergent/internal/runner"
	"github.com/gopaytech/pergent/internal/skill"
)

const wantPromptMessage = "The attached review-instructions.md holds your review instructions, including the output format. Follow it."

// ownConfigRun is one run of an own-config review, with the prompt it carried.
type ownConfigRun struct {
	kind   string
	opts   runner.Options
	prompt string
	found  bool
}

// In own-config mode (no OPENCODE_PROVIDER) pergent writes no opencode.json,
// and the agent prompt lives only there. Every kind of run must carry its
// prompt as an attached file instead, or the model gets "Review the attached
// diff." with no output format and every review fails as a format error.
func TestOwnConfig_EveryRunCarriesItsPrompt(t *testing.T) {
	s := skill.Skill{Name: "s", Body: "Review it. Mentions {env:HOME}.", CrossBatch: true}
	var mu sync.Mutex
	var runs []ownConfigRun
	repaired := false
	f := &fake{}
	f.respond = func(_ int, c call) runner.RunResult {
		run := ownConfigRun{opts: c.opts}
		for i, p := range c.opts.Files {
			if filepath.Base(p) == "review-instructions.md" {
				run.prompt, run.found = c.contents[i], true
			}
		}
		var all string // a batch run's diff
		for i, p := range c.opts.Files {
			if filepath.Base(p) == "diff.patch" {
				all = c.contents[i]
			}
		}
		mu.Lock()
		defer mu.Unlock()
		switch {
		case isVerifyCall(c):
			run.kind = "verify"
			runs = append(runs, run)
			return completed(verifyAnswer(t, map[string]any{"id": "1", "real": true, "reason": "r"}))
		case isCrossBatchCall(c):
			run.kind = "cross"
			runs = append(runs, run)
			return completed(answer(t))
		case strings.HasPrefix(c.opts.Message, "The attached review is not in the required output format"):
			run.kind = "repair"
			runs = append(runs, run)
			return completed(answer(t))
		}
		run.kind = "batch"
		runs = append(runs, run)
		switch {
		case strings.Contains(all, "d0/f.go"):
			return completed(answer(t, finding.Finding{File: "d0/f.go", Line: 1, Severity: finding.Blocking, Title: "Bug", Explanation: "e"}))
		case strings.Contains(all, "d1/f.go") && !repaired:
			repaired = true
			return completed("no json block here")
		}
		return completed(answer(t))
	}
	req, _ := crossBatchRequest(t, f, true)
	req.Skills = []skill.Skill{s}
	t.Setenv("OPENCODE_PROVIDER", "")

	Run(context.Background(), req)

	kinds := map[string]int{}
	for _, r := range runs {
		kinds[r.kind]++
		want := s.Prompt()
		if r.kind == "verify" {
			want = verifyPrompt
		}
		if r.opts.ConfigPath != "" {
			t.Errorf("%s run has a config %q, want own-config mode", r.kind, r.opts.ConfigPath)
		}
		if !r.found {
			t.Errorf("%s run has no review-instructions.md among %q", r.kind, r.opts.Files)
			continue
		}
		if r.prompt != want {
			t.Errorf("%s run prompt = %q, want %q", r.kind, r.prompt, want)
		}
		if !strings.HasSuffix(r.opts.Message, " "+wantPromptMessage) {
			t.Errorf("%s run message = %q, want it to end with the instructions sentence", r.kind, r.opts.Message)
		}
	}
	for _, kind := range []string{"batch", "repair", "verify", "cross"} {
		if kinds[kind] == 0 {
			t.Errorf("no %s run happened (runs: %v), the test does not cover it", kind, kinds)
		}
	}
	assertRemoved(t, allFiles(f.calls))
}

// With a generated config the prompt is in the config: nothing is attached
// and the message is unchanged.
func TestGeneratedConfig_NoPromptAttachment(t *testing.T) {
	f := &fake{respond: func(int, call) runner.RunResult { return completed(answer(t)) }}
	req := newRequest(t, f, patch("cmd/a.go", "x"))
	t.Setenv("OPENCODE_PROVIDER", "anthropic")
	t.Setenv("OPENCODE_MODEL", "test-model")

	Run(context.Background(), req)
	if len(f.calls) != 1 {
		t.Fatalf("runner called %d times, want 1", len(f.calls))
	}
	for _, p := range f.calls[0].opts.Files {
		if filepath.Base(p) == "review-instructions.md" {
			t.Errorf("a generated-config run has the instructions attached: %q", f.calls[0].opts.Files)
		}
	}
	if m := f.calls[0].opts.Message; strings.Contains(m, "review-instructions.md") {
		t.Errorf("Message = %q, want no instructions sentence", m)
	}
}

func TestOwnConfig_PromptFileWriteFailureFailsTheBatch(t *testing.T) {
	f := &fake{respond: func(int, call) runner.RunResult { return completed(answer(t)) }}
	req := newRequest(t, f, patch("cmd/a.go", "x"))
	req.Budget = 5 * time.Minute
	t.Setenv("OPENCODE_PROVIDER", "")
	old := writePrompt
	t.Cleanup(func() { writePrompt = old })
	writePrompt = func(string, []byte, os.FileMode) error { return errors.New("disk is full") }

	r := Run(context.Background(), req)[0]
	if len(f.calls) != 0 {
		t.Errorf("runner called %d times, want 0: a run without its prompt would fail as a format error", len(f.calls))
	}
	if r.Verdict != Failed || len(r.Failures) != 1 || !strings.Contains(r.Failures[0].Message, "review instructions") {
		t.Errorf("result = %+v, want a failed batch that names the review instructions", r)
	}
}
