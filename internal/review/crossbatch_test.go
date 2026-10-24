package review

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gopaytech/pergent/internal/finding"
	"github.com/gopaytech/pergent/internal/runner"
	"github.com/gopaytech/pergent/internal/skill"
)

func wantCrossBatchMessage(n int) string {
	return fmt.Sprintf("This change was reviewed in %d batches. Attached are the list of every changed file and the findings from all batches. Look only for problems that span batches, such as conflicting changes between files in different batches. Report only new findings.", n)
}

func isCrossBatchCall(c call) bool {
	return strings.HasPrefix(c.opts.Message, "This change was reviewed in ")
}

func crossBatchRequest(t *testing.T, f *fake, crossBatch bool) (Request, []string) {
	t.Helper()
	diffText, paths := nBatchDiff(3)
	req := newRequest(t, f, diffText, skill.Skill{Name: "s", Body: "Review it.", CrossBatch: crossBatch})
	req.ContextLimit = twoBatchContextLimit
	req.Budget = 5 * time.Minute
	return req, paths
}

func TestCrossBatch_OffByDefault(t *testing.T) {
	f := &fake{}
	f.respond = func(_ int, c call) runner.RunResult { return completed(answer(t)) }
	req, _ := crossBatchRequest(t, f, false)

	r := Run(context.Background(), req)[0]
	if len(f.calls) != 3 {
		t.Errorf("runner called %d times, want 3 (one per batch, no cross-batch)", len(f.calls))
	}
	for _, c := range f.calls {
		if isCrossBatchCall(c) {
			t.Error("cross-batch ran although the skill did not ask for it")
		}
	}
	if len(r.Notes) != 0 {
		t.Errorf("Notes = %q, want none", r.Notes)
	}
}

func TestCrossBatch_NotRunForSingleBatch(t *testing.T) {
	f := &fake{}
	f.respond = func(_ int, c call) runner.RunResult { return completed(answer(t)) }
	req := newRequest(t, f, patch("a/x.go", "x"), skill.Skill{Name: "s", Body: "Review it.", CrossBatch: true})
	req.Budget = 5 * time.Minute

	Run(context.Background(), req)
	if len(f.calls) != 1 {
		t.Errorf("runner called %d times, want 1 (a single batch has nothing to span)", len(f.calls))
	}
}

func TestCrossBatch_AddsFindings(t *testing.T) {
	batchFinding := finding.Finding{File: "d0/f.go", Line: 1, Severity: finding.Suggestion, Title: "Batch finding", Explanation: "e"}
	newFinding := finding.Finding{File: "d1/f.go", Line: 2, Severity: finding.Suggestion, Title: "Spanning finding", Explanation: "e"}
	f := &fake{}
	f.respond = func(_ int, c call) runner.RunResult {
		if isCrossBatchCall(c) {
			// One repeat of a batch finding and one new finding.
			return completed(answer(t, batchFinding, newFinding))
		}
		if strings.Contains(c.contents[0], "d0/f.go") {
			return completed(answer(t, batchFinding))
		}
		return completed(answer(t))
	}
	req, paths := crossBatchRequest(t, f, true)

	r := Run(context.Background(), req)[0]

	var cross []call
	for _, c := range f.calls {
		if isCrossBatchCall(c) {
			cross = append(cross, c)
		}
	}
	if len(f.calls) != 4 || len(cross) != 1 {
		t.Fatalf("runner called %d times with %d cross-batch calls, want 4 and 1", len(f.calls), len(cross))
	}
	cc := cross[0]
	if cc.opts.Message != wantCrossBatchMessage(3) {
		t.Errorf("Message = %q, want %q", cc.opts.Message, wantCrossBatchMessage(3))
	}
	if len(cc.contents) != 2 {
		t.Fatalf("cross-batch attached %d files, want 2", len(cc.contents))
	}
	for _, p := range paths {
		if !strings.Contains(cc.contents[0], "- "+p+"\n") {
			t.Errorf("changed-file list %q is missing %s", cc.contents[0], p)
		}
	}
	var sent struct{ Findings []finding.Finding }
	if err := json.Unmarshal([]byte(cc.contents[1]), &sent); err != nil {
		t.Fatalf("second attachment is not JSON: %v\n%s", err, cc.contents[1])
	}
	if got, want := titles(sent.Findings), []string{"Batch finding"}; !slices.Equal(got, want) {
		t.Errorf("cross-batch was sent findings %q, want %q", got, want)
	}
	if got, want := titles(r.Findings), []string{"Batch finding", "Spanning finding"}; !slices.Equal(got, want) {
		t.Errorf("findings = %q, want %q (merged and deduped)", got, want)
	}
	for _, fi := range r.Findings {
		if fi.Fingerprint == "" {
			t.Errorf("finding %q has no fingerprint", fi.Title)
		}
	}
	if r.Verdict != NoBlockingIssues || len(r.Notes) != 0 {
		t.Errorf("Verdict = %q, Notes = %q, want no blocking issues and no notes", r.Verdict, r.Notes)
	}
	assertRemoved(t, allFiles(f.calls))
}

func TestCrossBatch_FindingsAreVerified(t *testing.T) {
	blocking := finding.Finding{File: "d1/f.go", Line: 2, Severity: finding.Blocking, Title: "Spanning bug", Explanation: "e"}
	fp := finding.Fingerprint("s", blocking.File, blocking.Title)
	f := &fake{}
	f.respond = func(_ int, c call) runner.RunResult {
		switch {
		case isCrossBatchCall(c):
			return completed(answer(t, blocking))
		case isVerifyCall(c):
			return completed(verifyAnswer(t, map[string]any{"fingerprint": fp, "real": false, "reason": "r"}))
		}
		return completed(answer(t))
	}
	req, _ := crossBatchRequest(t, f, true)

	r := Run(context.Background(), req)[0]
	var order []string
	for _, c := range f.calls {
		switch {
		case isCrossBatchCall(c):
			order = append(order, "cross")
		case isVerifyCall(c):
			order = append(order, "verify")
		}
	}
	if !slices.Equal(order, []string{"cross", "verify"}) {
		t.Errorf("run order = %q, want cross-batch then verification", order)
	}
	if len(r.Findings) != 0 || r.Verdict != NoBlockingIssues {
		t.Errorf("result = %+v, want the cross-batch finding verified away", r)
	}
}

func TestCrossBatch_SkippedWhenLowOnTime(t *testing.T) {
	f := &fake{}
	f.respond = func(_ int, c call) runner.RunResult { return completed(answer(t)) }
	req, _ := crossBatchRequest(t, f, true)
	req.Budget = verifyMinRemaining - time.Second

	r := Run(context.Background(), req)[0]
	for _, c := range f.calls {
		if isCrossBatchCall(c) {
			t.Error("cross-batch ran with less than verifyMinRemaining left")
		}
	}
	const want = "Cross-batch check skipped: less than 1m30s of the review budget was left."
	if !slices.Contains(r.Notes, want) {
		t.Errorf("Notes = %q, want it to contain %q", r.Notes, want)
	}
	if r.Verdict != NoBlockingIssues {
		t.Errorf("Verdict = %q, want %q (a skipped cross-batch never fails the skill)", r.Verdict, NoBlockingIssues)
	}
}

func TestCrossBatch_FailureIsANote(t *testing.T) {
	cause := &runner.Cause{Kind: runner.CauseNoAnswer}
	f := &fake{}
	f.respond = func(_ int, c call) runner.RunResult {
		if isCrossBatchCall(c) {
			return runner.RunResult{Status: runner.StatusFailed, Cause: cause}
		}
		return completed(answer(t))
	}
	req, _ := crossBatchRequest(t, f, true)

	r := Run(context.Background(), req)[0]
	want := "Cross-batch check failed: " + cause.Message()
	if !slices.Contains(r.Notes, want) {
		t.Errorf("Notes = %q, want it to contain %q", r.Notes, want)
	}
	if r.Verdict != NoBlockingIssues || len(r.Failures) != 0 {
		t.Errorf("Verdict = %q, Failures = %+v, want a note only", r.Verdict, r.Failures)
	}
}

func TestCrossBatch_BadAnswerIsANoteWithoutRepair(t *testing.T) {
	f := &fake{}
	f.respond = func(_ int, c call) runner.RunResult {
		if isCrossBatchCall(c) {
			return completed("Looks fine to me.")
		}
		return completed(answer(t))
	}
	req, _ := crossBatchRequest(t, f, true)

	r := Run(context.Background(), req)[0]
	if len(f.calls) != 4 {
		t.Errorf("runner called %d times, want 4 (no repair run)", len(f.calls))
	}
	found := false
	for _, n := range r.Notes {
		if strings.HasPrefix(n, "Cross-batch check failed: the answer was not in the required format (") &&
			strings.Contains(n, "Looks fine to me.") {
			found = true
		}
	}
	if !found {
		t.Errorf("Notes = %q, want a Cross-batch check failed note about the format with an excerpt", r.Notes)
	}
	if r.Verdict != NoBlockingIssues {
		t.Errorf("Verdict = %q, want %q", r.Verdict, NoBlockingIssues)
	}
}
