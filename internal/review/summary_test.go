package review

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gopaytech/pergent/internal/runner"
)

func summaryAnswer(summary string) string {
	return "```json\n{\"summary\":\"" + summary + "\",\"findings\":[]}\n```\n"
}

func TestRun_SummaryAndReviewedFiles(t *testing.T) {
	f := &fake{}
	f.respond = func(int, call) runner.RunResult { return completed(summaryAnswer("Checked a.")) }
	r := Run(context.Background(), newRequest(t, f, patch("cmd/a.go", "x")))[0]

	if want := []BatchSummary{{Batch: 1, Text: "Checked a."}}; !slices.Equal(r.Summaries, want) {
		t.Errorf("Summaries = %+v, want %+v", r.Summaries, want)
	}
	if want := []string{"cmd/a.go"}; !slices.Equal(r.Reviewed, want) {
		t.Errorf("Reviewed = %q, want %q", r.Reviewed, want)
	}
}

func TestRun_SummariesSkipFailedBatches(t *testing.T) {
	f := &fake{}
	f.respond = func(_ int, c call) runner.RunResult {
		if strings.Contains(c.contents[0], "a/x.go") {
			return runner.RunResult{Status: runner.StatusFailed, Cause: &runner.Cause{Kind: runner.CauseTimeout, Timeout: time.Minute}}
		}
		return completed(summaryAnswer("B"))
	}
	req := newRequest(t, f, twoBatchDiff())
	req.ContextLimit = twoBatchContextLimit

	r := Run(context.Background(), req)[0]
	if want := []BatchSummary{{Batch: 2, Text: "B"}}; !slices.Equal(r.Summaries, want) {
		t.Errorf("Summaries = %+v, want %+v", r.Summaries, want)
	}
	if want := []string{"b/y.go"}; !slices.Equal(r.Reviewed, want) {
		t.Errorf("Reviewed = %q, want %q", r.Reviewed, want)
	}
}

func TestRun_RepairedAnswerSummary(t *testing.T) {
	f := &fake{}
	f.respond = func(n int, _ call) runner.RunResult {
		if n == 1 {
			return completed("Looks fine.")
		}
		return completed(summaryAnswer("R"))
	}
	r := Run(context.Background(), newRequest(t, f, patch("cmd/a.go", "x")))[0]
	if want := []BatchSummary{{Batch: 1, Text: "R"}}; !slices.Equal(r.Summaries, want) {
		t.Errorf("Summaries = %+v, want %+v", r.Summaries, want)
	}
}

func TestRun_NoSummaryIsFine(t *testing.T) {
	f := &fake{}
	f.respond = func(int, call) runner.RunResult { return completed(answer(t)) }
	r := Run(context.Background(), newRequest(t, f, patch("cmd/a.go", "x")))[0]
	if len(r.Summaries) != 0 {
		t.Errorf("Summaries = %+v, want none", r.Summaries)
	}
	if r.Verdict != NoBlockingIssues {
		t.Errorf("Verdict = %q, want %q", r.Verdict, NoBlockingIssues)
	}
	if want := []string{"cmd/a.go"}; !slices.Equal(r.Reviewed, want) {
		t.Errorf("Reviewed = %q, want %q", r.Reviewed, want)
	}
}
