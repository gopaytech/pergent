package review

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gopaytech/pergent/internal/runner"
	"github.com/gopaytech/pergent/internal/skill"
)

// nBatchDiff has one file in each of n directories, sized so that a
// ContextLimit of twoBatchContextLimit puts each in its own batch. It
// returns the diff and the file paths.
func nBatchDiff(n int) (string, []string) {
	var b strings.Builder
	var paths []string
	for i := range n {
		p := fmt.Sprintf("d%d/f.go", i)
		paths = append(paths, p)
		b.WriteString(patch(p, strings.Repeat("x", 200)))
	}
	return b.String(), paths
}

func failureFiles(r SkillResult) []string {
	var out []string
	for _, fl := range r.Failures {
		out = append(out, fl.Files...)
	}
	sort.Strings(out)
	return out
}

func timedOut(opts runner.Options) runner.RunResult {
	return runner.RunResult{SkillName: opts.SkillName, Status: runner.StatusFailed, Cause: &runner.Cause{Kind: runner.CauseTimeout, Timeout: opts.Timeout}}
}

func TestRun_RespectsConcurrency(t *testing.T) {
	var inFlight, peak, calls atomic.Int32
	run := func(_ context.Context, opts runner.Options) runner.RunResult {
		calls.Add(1)
		n := inFlight.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		inFlight.Add(-1)
		return completed(answer(t))
	}
	diffText, _ := nBatchDiff(3)
	req := newRequest(t, &fake{}, diffText,
		skill.Skill{Name: "one"}, skill.Skill{Name: "two"}, skill.Skill{Name: "three"})
	req.Run = run
	req.ContextLimit = twoBatchContextLimit
	req.Concurrency = 2

	got := Run(context.Background(), req)
	if c := calls.Load(); c != 9 {
		t.Errorf("runner called %d times, want 9", c)
	}
	if p := peak.Load(); p > 2 {
		t.Errorf("peak in-flight runs = %d, want at most 2", p)
	}
	if p := peak.Load(); p < 2 {
		t.Errorf("peak in-flight runs = %d, want runs in parallel (2)", p)
	}
	for _, r := range got {
		if r.Verdict != NoBlockingIssues || r.Batches != 3 || r.BatchesFinished != 3 {
			t.Errorf("%s: result = %+v, want 3 of 3 batches finished", r.Skill, r)
		}
	}
}

func TestRun_BudgetExhausted(t *testing.T) {
	f := &fake{}
	run := func(ctx context.Context, opts runner.Options) runner.RunResult {
		f.mu.Lock()
		f.calls = append(f.calls, call{opts: opts})
		f.mu.Unlock()
		<-ctx.Done()
		return timedOut(opts)
	}
	diffText, paths := nBatchDiff(4)
	req := newRequest(t, f, diffText)
	req.Run = run
	req.ContextLimit = twoBatchContextLimit
	req.Budget = 100 * time.Millisecond
	req.Concurrency = 1

	r := Run(context.Background(), req)[0]
	if r.Verdict != Failed {
		t.Errorf("Verdict = %q, want %q", r.Verdict, Failed)
	}
	if r.Batches != 4 || r.BatchesFinished != 0 {
		t.Errorf("Batches = %d, BatchesFinished = %d, want 4 and 0", r.Batches, r.BatchesFinished)
	}
	if len(f.calls) != 1 {
		t.Errorf("runner called %d times, want 1 (one slot, then no time left)", len(f.calls))
	}
	const want = "Not reviewed: the 100ms review budget ran out; 0 of 4 batches finished."
	var budget, timeouts []BatchFailure
	for _, fl := range r.Failures {
		switch {
		case fl.Message == want && fl.Cause == nil:
			budget = append(budget, fl)
		case fl.Cause != nil && fl.Cause.Kind == runner.CauseTimeout:
			timeouts = append(timeouts, fl)
		default:
			t.Errorf("unexpected failure %+v", fl)
		}
	}
	if len(budget) != 1 || len(budget[0].Files) != 3 {
		t.Errorf("Failures = %+v, want one budget failure listing the 3 unstarted batches' files", r.Failures)
	}
	if len(timeouts) != 1 {
		t.Errorf("Failures = %+v, want the started batch to keep its timeout cause", r.Failures)
	}
	if got := failureFiles(r); !slices.Equal(got, paths) {
		t.Errorf("failed files = %q, want every batch's files %q", got, paths)
	}
}

func TestRun_TimeoutIsRemainingBudget(t *testing.T) {
	const pause = 50 * time.Millisecond
	var mu sync.Mutex
	var timeouts []time.Duration
	run := func(_ context.Context, opts runner.Options) runner.RunResult {
		mu.Lock()
		timeouts = append(timeouts, opts.Timeout)
		mu.Unlock()
		time.Sleep(pause)
		return completed(answer(t))
	}
	req := newRequest(t, &fake{}, twoBatchDiff())
	req.Run = run
	req.ContextLimit = twoBatchContextLimit
	req.Budget = time.Minute
	req.Concurrency = 1

	r := Run(context.Background(), req)[0]
	if r.BatchesFinished != 2 {
		t.Fatalf("result = %+v, want 2 batches finished", r)
	}
	if len(timeouts) != 2 {
		t.Fatalf("timeouts = %v, want 2 runs", timeouts)
	}
	for i, d := range timeouts {
		if d <= 0 || d > req.Budget {
			t.Errorf("run %d Timeout = %v, want > 0 and <= %v", i+1, d, req.Budget)
		}
	}
	if timeouts[1] > req.Budget-pause {
		t.Errorf("second run Timeout = %v, want at most %v (the budget left after the first run)", timeouts[1], req.Budget-pause)
	}
}

func TestRun_RepairNotStartedWhenBudgetRunsOut(t *testing.T) {
	var calls atomic.Int32
	run := func(ctx context.Context, opts runner.Options) runner.RunResult {
		calls.Add(1)
		<-ctx.Done()
		return completed("no block")
	}
	req := newRequest(t, &fake{}, patch("cmd/a.go", "x"))
	req.Run = run
	req.Budget = 50 * time.Millisecond

	r := Run(context.Background(), req)[0]
	if c := calls.Load(); c != 1 {
		t.Errorf("runner called %d times, want 1 (no repair run)", c)
	}
	if r.Verdict != Failed || r.BatchesFinished != 0 || len(r.Failures) != 1 {
		t.Fatalf("result = %+v, want failed with one failure", r)
	}
	fl := r.Failures[0]
	const want = "Not reviewed: the 50ms review budget ran out before the repair run could start; the model's answer was not in the required format (no json code block found in the answer)."
	if fl.Cause != nil || fl.Message != want || !slices.Equal(fl.Files, []string{"cmd/a.go"}) {
		t.Errorf("failure = %+v, want cmd/a.go with message %q", fl, want)
	}
}

func TestRun_CancelledReviewIsNotBudget(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls atomic.Int32
	req := newRequest(t, &fake{}, twoBatchDiff())
	req.Run = func(context.Context, runner.Options) runner.RunResult {
		calls.Add(1)
		return completed(answer(t))
	}
	req.ContextLimit = twoBatchContextLimit

	r := Run(ctx, req)[0]
	if c := calls.Load(); c != 0 {
		t.Errorf("runner called %d times, want 0", c)
	}
	const want = "Not reviewed: the review was cancelled (context canceled); 0 of 2 batches finished."
	if r.Verdict != Failed || len(r.Failures) != 1 || r.Failures[0].Message != want || len(r.Failures[0].Files) != 2 {
		t.Errorf("result = %+v, want one failure %q listing both files", r, want)
	}
}

func TestRun_DeterministicOrderWithinSkill(t *testing.T) {
	diffText, paths := nBatchDiff(4)
	run := func(_ context.Context, opts runner.Options) runner.RunResult {
		// Later batches finish first.
		n := 0
		fmt.Sscanf(opts.Message, "Review the attached diff. It is batch %d", &n)
		time.Sleep(time.Duration(5-n) * 10 * time.Millisecond)
		return runner.RunResult{Status: runner.StatusFailed, Cause: &runner.Cause{Kind: runner.CauseNoAnswer}}
	}
	req := newRequest(t, &fake{}, diffText)
	req.Run = run
	req.ContextLimit = twoBatchContextLimit
	req.Concurrency = 4

	r := Run(context.Background(), req)[0]
	var got []string
	for _, fl := range r.Failures {
		got = append(got, fl.Files...)
	}
	if !slices.Equal(got, paths) {
		t.Errorf("failure order = %q, want batch order %q", got, paths)
	}
}
