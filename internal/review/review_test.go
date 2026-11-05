package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gopaytech/pergent/internal/config"
	"github.com/gopaytech/pergent/internal/diff"
	"github.com/gopaytech/pergent/internal/finding"
	"github.com/gopaytech/pergent/internal/runner"
	"github.com/gopaytech/pergent/internal/skill"
)

// call is what the fake runner saw in one run. Contents holds each attached
// file's contents, read while the run was in progress.
type call struct {
	opts     runner.Options
	contents []string
}

// fake is a RunFunc that records its calls and answers with respond.
type fake struct {
	mu      sync.Mutex
	calls   []call
	respond func(n int, c call) runner.RunResult
}

func (f *fake) run(_ context.Context, opts runner.Options) runner.RunResult {
	c := call{opts: opts}
	for _, p := range opts.Files {
		data, err := os.ReadFile(p)
		if err != nil {
			c.contents = append(c.contents, "<unreadable: "+err.Error()+">")
			continue
		}
		c.contents = append(c.contents, string(data))
	}
	f.mu.Lock()
	f.calls = append(f.calls, c)
	n := len(f.calls)
	f.mu.Unlock()
	return f.respond(n, c)
}

func completed(output string) runner.RunResult {
	return runner.RunResult{Status: runner.StatusCompleted, Output: output}
}

// answer returns a model answer ending in the required json block.
func answer(t *testing.T, fs ...finding.Finding) string {
	t.Helper()
	if fs == nil {
		fs = []finding.Finding{}
	}
	data, err := json.Marshal(map[string]any{"findings": fs})
	if err != nil {
		t.Fatal(err)
	}
	return "Some notes.\n\n```json\n" + string(data) + "\n```\n"
}

// scored returns a model answer with no findings and the given confidence.
func scored(t *testing.T, score int, reason string) string {
	t.Helper()
	data, err := json.Marshal(map[string]any{"confidence": score, "confidence_reason": reason, "findings": []finding.Finding{}})
	if err != nil {
		t.Fatal(err)
	}
	return "```json\n" + string(data) + "\n```\n"
}

func newRequest(t *testing.T, f *fake, diffText string, skills ...skill.Skill) Request {
	t.Helper()
	t.Setenv("OPENCODE_PROVIDER", "")
	if len(skills) == 0 {
		skills = []skill.Skill{{Name: "s", Body: "Review it."}}
	}
	return Request{
		Skills:   skills,
		Diff:     diffText,
		RepoPath: t.TempDir(),
		Budget:   time.Minute,
		MaxTurns: 20,
		Run:      f.run,
	}
}

// twoBatchDiff has one file in each of two directories, sized so that a
// ContextLimit of twoBatchContextLimit puts them in separate batches.
func twoBatchDiff() string {
	return patch("a/x.go", strings.Repeat("a", 200)) + patch("b/y.go", strings.Repeat("b", 200))
}

const twoBatchContextLimit = 400 // per-batch budget of 100 tokens

func batchMessage(n, m int) string {
	return "Review the attached diff. It is batch " + strconv.Itoa(n) + " of " + strconv.Itoa(m) +
		" of this change; the attached list shows every changed file. Review only the files in this batch's diff, and read any file in the repository you need."
}

// sortedMessages returns the calls' messages in sorted order, since batches
// run concurrently.
func sortedMessages(calls []call) []string {
	var out []string
	for _, c := range calls {
		out = append(out, c.opts.Message)
	}
	slices.Sort(out)
	return out
}

func allFiles(calls []call) []string {
	var out []string
	for _, c := range calls {
		out = append(out, c.opts.Files...)
	}
	return out
}

func assertRemoved(t *testing.T, files []string) {
	t.Helper()
	for _, p := range files {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("temp file %s still exists after Run (stat err %v)", p, err)
		}
	}
}

func TestRun_NoFilesInScope(t *testing.T) {
	f := &fake{respond: func(int, call) runner.RunResult { return completed("") }}
	req := newRequest(t, f, patch("cmd/a.go", "x"), skill.Skill{Name: "s", Paths: []string{"docs/**"}})

	got := Run(context.Background(), req)
	if len(got) != 1 {
		t.Fatalf("got %d results, want 1", len(got))
	}
	r := got[0]
	if r.Verdict != NoBlockingIssues {
		t.Errorf("Verdict = %q, want %q", r.Verdict, NoBlockingIssues)
	}
	if !slices.Contains(r.Notes, "no files in scope") {
		t.Errorf("Notes = %q, want it to contain %q", r.Notes, "no files in scope")
	}
	if len(f.calls) != 0 {
		t.Errorf("runner called %d times, want 0", len(f.calls))
	}
	if len(r.Skipped) != 1 || r.Skipped[0].Rule != "out of scope" {
		t.Errorf("Skipped = %+v, want cmd/a.go out of scope", r.Skipped)
	}
}

func TestRun_ConfidenceIsLowestBatch(t *testing.T) {
	f := &fake{respond: func(_ int, c call) runner.RunResult {
		if strings.Contains(c.opts.Message, "batch 1 of 2") {
			return completed(scored(t, 4, "Minor naming."))
		}
		return completed(scored(t, 2, "Retry loop never ends."))
	}}
	req := newRequest(t, f, twoBatchDiff())
	req.ContextLimit = twoBatchContextLimit
	res := Run(context.Background(), req)[0]
	if res.Confidence != 2 || res.ConfidenceReason != "Retry loop never ends." {
		t.Errorf("got (%d, %q), want (2, %q)", res.Confidence, res.ConfidenceReason, "Retry loop never ends.")
	}
}

func TestRun_ConfidenceUnscoredWhenABatchHasNone(t *testing.T) {
	f := &fake{respond: func(_ int, c call) runner.RunResult {
		if strings.Contains(c.opts.Message, "batch 1 of 2") {
			return completed(scored(t, 4, "Minor naming."))
		}
		return completed(answer(t))
	}}
	req := newRequest(t, f, twoBatchDiff())
	req.ContextLimit = twoBatchContextLimit
	res := Run(context.Background(), req)[0]
	if res.Confidence != 0 || res.ConfidenceReason != "" {
		t.Errorf("got (%d, %q), want (0, \"\")", res.Confidence, res.ConfidenceReason)
	}
}

func TestRun_ConfidenceUnscoredWhenNoFilesInScope(t *testing.T) {
	f := &fake{respond: func(int, call) runner.RunResult { return completed("") }}
	req := newRequest(t, f, patch("cmd/a.go", "x"), skill.Skill{Name: "s", Paths: []string{"docs/**"}})
	res := Run(context.Background(), req)[0]
	if res.Confidence != 0 || res.ConfidenceReason != "" {
		t.Errorf("got (%d, %q), want (0, \"\")", res.Confidence, res.ConfidenceReason)
	}
}

func TestRun_SingleBatch(t *testing.T) {
	f := &fake{}
	f.respond = func(int, call) runner.RunResult { return completed(answer(t)) }
	diffText := patch("cmd/a.go", "x") + patch("go.sum", "x")
	req := newRequest(t, f, diffText)
	req.Budget = 3 * time.Minute

	r := Run(context.Background(), req)[0]
	if len(f.calls) != 1 {
		t.Fatalf("runner called %d times, want 1", len(f.calls))
	}
	c := f.calls[0]
	if c.opts.Message != "Review the attached diff." {
		t.Errorf("Message = %q, want the single-batch message", c.opts.Message)
	}
	if c.opts.Timeout <= 3*time.Minute-time.Second || c.opts.Timeout > 3*time.Minute || c.opts.SkillName != "s" || c.opts.RepoPath != req.RepoPath {
		t.Errorf("opts = %+v, want timeout of about 3m, skill s and the repo path", c.opts)
	}
	if c.opts.Budget != 3*time.Minute || time.Since(c.opts.ReviewStart) > 10*time.Second || time.Until(c.opts.ReviewStart) > 0 {
		t.Errorf("opts Budget = %v, ReviewStart = %v, want the review's budget and its start time", c.opts.Budget, c.opts.ReviewStart)
	}
	if len(c.opts.Files) != 2 {
		t.Fatalf("Files = %q, want the batch diff and the changed-file list", c.opts.Files)
	}
	if !strings.HasSuffix(c.opts.Files[0], ".patch") || c.contents[0] != patch("cmd/a.go", "x") {
		t.Errorf("first file %s = %q, want the in-scope patch", c.opts.Files[0], c.contents[0])
	}
	if !strings.HasSuffix(c.opts.Files[1], ".md") || c.contents[1] != "- cmd/a.go\n- go.sum\n" {
		t.Errorf("second file %s = %q, want every changed file listed", c.opts.Files[1], c.contents[1])
	}
	if r.Verdict != NoBlockingIssues || r.Batches != 1 || r.BatchesFinished != 1 {
		t.Errorf("result = %+v, want no blocking issues and 1 of 1 batches finished", r)
	}
	assertRemoved(t, allFiles(f.calls))
}

func TestRun_DefaultBudget(t *testing.T) {
	f := &fake{}
	f.respond = func(int, call) runner.RunResult { return completed(answer(t)) }
	req := newRequest(t, f, patch("cmd/a.go", "x"))
	req.Budget = 0

	Run(context.Background(), req)
	if len(f.calls) != 1 || f.calls[0].opts.Timeout <= 10*time.Minute-time.Second || f.calls[0].opts.Timeout > 10*time.Minute {
		t.Errorf("calls = %+v, want one run with a timeout of about 10m", f.calls)
	}
}

func TestRun_Verdicts(t *testing.T) {
	blocking := finding.Finding{File: "cmd/a.go", Line: 3, Severity: finding.Blocking, Title: "Nil dereference", Explanation: "e"}
	suggestion := finding.Finding{File: "cmd/a.go", Severity: finding.Suggestion, Title: "Rename variable", Explanation: "e"}
	tests := []struct {
		name     string
		findings []finding.Finding
		want     Verdict
	}{
		{"blocking", []finding.Finding{suggestion, blocking}, HasBlockingIssues},
		{"suggestions only", []finding.Finding{suggestion}, NoBlockingIssues},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fake{}
			f.respond = func(int, call) runner.RunResult { return completed(answer(t, tt.findings...)) }
			r := Run(context.Background(), newRequest(t, f, patch("cmd/a.go", "x")))[0]
			if r.Verdict != tt.want {
				t.Errorf("Verdict = %q, want %q", r.Verdict, tt.want)
			}
			if len(r.Findings) != len(tt.findings) {
				t.Fatalf("Findings = %+v, want %d", r.Findings, len(tt.findings))
			}
			for _, fd := range r.Findings {
				if len(fd.Fingerprint) != 12 {
					t.Errorf("finding %q fingerprint = %q, want 12 chars", fd.Title, fd.Fingerprint)
				}
				if want := finding.Fingerprint("s", fd.File, fd.Title); fd.Fingerprint != want {
					t.Errorf("finding %q fingerprint = %q, want %q", fd.Title, fd.Fingerprint, want)
				}
			}
		})
	}
}

func TestRun_DedupsFindings(t *testing.T) {
	dup := finding.Finding{File: "a/x.go", Line: 1, Severity: finding.Suggestion, Title: "Same", Explanation: "e"}
	f := &fake{}
	f.respond = func(int, call) runner.RunResult { return completed(answer(t, dup, dup)) }
	r := Run(context.Background(), newRequest(t, f, patch("a/x.go", "x")))[0]
	if len(r.Findings) != 1 {
		t.Errorf("Findings = %+v, want the duplicate dropped", r.Findings)
	}
}

func TestRun_BatchFailureMakesSkillFailed(t *testing.T) {
	ok := finding.Finding{File: "b/y.go", Line: 1, Severity: finding.Suggestion, Title: "Tidy up", Explanation: "e"}
	f := &fake{}
	f.respond = func(_ int, c call) runner.RunResult {
		if strings.Contains(c.contents[0], "a/x.go") {
			return runner.RunResult{Status: runner.StatusFailed, Cause: &runner.Cause{Kind: runner.CauseTimeout, Timeout: time.Minute}}
		}
		return completed(answer(t, ok))
	}
	req := newRequest(t, f, twoBatchDiff())
	req.ContextLimit = twoBatchContextLimit

	r := Run(context.Background(), req)[0]
	if len(f.calls) != 2 {
		t.Fatalf("runner called %d times, want 2", len(f.calls))
	}
	if got, want := sortedMessages(f.calls), []string{batchMessage(1, 2), batchMessage(2, 2)}; !slices.Equal(got, want) {
		t.Errorf("Messages = %q, want %q", got, want)
	}
	for i, c := range f.calls {
		if c.contents[1] != "- a/x.go\n- b/y.go\n" {
			t.Errorf("call %d changed-file list = %q, want both files", i+1, c.contents[1])
		}
	}
	if r.Verdict != Failed {
		t.Errorf("Verdict = %q, want %q", r.Verdict, Failed)
	}
	if len(r.Findings) != 1 || r.Findings[0].Title != "Tidy up" {
		t.Errorf("Findings = %+v, want the other batch's finding kept", r.Findings)
	}
	if len(r.Failures) != 1 || !slices.Equal(r.Failures[0].Files, []string{"a/x.go"}) || r.Failures[0].Cause == nil || r.Failures[0].Cause.Kind != runner.CauseTimeout {
		t.Errorf("Failures = %+v, want a/x.go with the runner's cause", r.Failures)
	}
	if r.Batches != 2 || r.BatchesFinished != 1 {
		t.Errorf("Batches = %d, BatchesFinished = %d, want 2 and 1", r.Batches, r.BatchesFinished)
	}
	assertRemoved(t, allFiles(f.calls))
}

func TestRun_OversizeFileMakesSkillFailed(t *testing.T) {
	f := &fake{}
	f.respond = func(int, call) runner.RunResult { return completed(answer(t)) }
	diffText := patch("big/huge.go", strings.Repeat("z", 2000)) + patch("small/s.go", "x")
	req := newRequest(t, f, diffText)
	req.ContextLimit = twoBatchContextLimit

	r := Run(context.Background(), req)[0]
	if r.Verdict != Failed {
		t.Errorf("Verdict = %q, want %q", r.Verdict, Failed)
	}
	if len(r.Failures) != 1 {
		t.Fatalf("Failures = %+v, want one", r.Failures)
	}
	fl := r.Failures[0]
	if !slices.Equal(fl.Files, []string{"big/huge.go"}) || fl.Cause != nil || !strings.Contains(fl.Message, "larger than the per-batch limit") {
		t.Errorf("failure = %+v, want big/huge.go larger than the per-batch limit", fl)
	}
	if len(f.calls) != 1 || strings.Contains(f.calls[0].contents[0], "huge.go") {
		t.Errorf("calls = %+v, want only the small file reviewed", f.calls)
	}
	if r.Batches != 1 || r.BatchesFinished != 1 {
		t.Errorf("Batches = %d, BatchesFinished = %d, want 1 and 1", r.Batches, r.BatchesFinished)
	}
}

func TestRun_RepairsBadFormatOnce(t *testing.T) {
	const badMsg = "The attached review is not in the required output format (no json code block found in the answer). Rewrite it in the required format without changing its findings."

	t.Run("repair succeeds", func(t *testing.T) {
		bad := "The code looks fine, one nit in cmd/a.go."
		fixed := finding.Finding{File: "cmd/a.go", Severity: finding.Suggestion, Title: "Nit", Explanation: "e"}
		f := &fake{}
		f.respond = func(n int, _ call) runner.RunResult {
			if n == 1 {
				return completed(bad)
			}
			return completed(answer(t, fixed))
		}
		r := Run(context.Background(), newRequest(t, f, patch("cmd/a.go", "x")))[0]
		if len(f.calls) != 2 {
			t.Fatalf("runner called %d times, want 2", len(f.calls))
		}
		c := f.calls[1]
		if c.opts.Message != badMsg {
			t.Errorf("repair Message = %q, want %q", c.opts.Message, badMsg)
		}
		if len(c.opts.Files) != 1 || !strings.HasSuffix(c.opts.Files[0], ".md") || c.contents[0] != bad {
			t.Errorf("repair Files = %q (%q), want the bad answer as one .md file", c.opts.Files, c.contents)
		}
		if r.Verdict != NoBlockingIssues || len(r.Findings) != 1 || r.Findings[0].Title != "Nit" {
			t.Errorf("result = %+v, want the repaired findings", r)
		}
		if r.BatchesFinished != 1 || len(r.Failures) != 0 {
			t.Errorf("result = %+v, want the batch finished with no failures", r)
		}
		assertRemoved(t, allFiles(f.calls))
	})

	t.Run("repair fails", func(t *testing.T) {
		bad := strings.Repeat("a", 150) + strings.Repeat("b", 150)
		f := &fake{}
		f.respond = func(int, call) runner.RunResult { return completed(bad) }
		r := Run(context.Background(), newRequest(t, f, patch("cmd/a.go", "x")))[0]
		if len(f.calls) != 2 {
			t.Fatalf("runner called %d times, want 2 (one repair only)", len(f.calls))
		}
		if r.Verdict != Failed || len(r.Findings) != 0 || r.BatchesFinished != 0 {
			t.Errorf("result = %+v, want failed with no findings", r)
		}
		if len(r.Failures) != 1 {
			t.Fatalf("Failures = %+v, want one", r.Failures)
		}
		msg := r.Failures[0].Message
		if !strings.Contains(msg, "not in the required format") || !strings.Contains(msg, bad[:200]) || strings.Contains(msg, bad[:201]) {
			t.Errorf("Message = %q, want the format cause and the first 200 chars of the answer", msg)
		}
		if !slices.Equal(r.Failures[0].Files, []string{"cmd/a.go"}) {
			t.Errorf("failure Files = %q, want cmd/a.go", r.Failures[0].Files)
		}
	})

	t.Run("repair run fails", func(t *testing.T) {
		f := &fake{}
		f.respond = func(n int, _ call) runner.RunResult {
			if n == 1 {
				return completed("no block")
			}
			return runner.RunResult{Status: runner.StatusFailed, Cause: &runner.Cause{Kind: runner.CauseNoAnswer}}
		}
		r := Run(context.Background(), newRequest(t, f, patch("cmd/a.go", "x")))[0]
		if r.Verdict != Failed || len(r.Failures) != 1 || r.Failures[0].Cause == nil || r.Failures[0].Cause.Kind != runner.CauseNoAnswer {
			t.Errorf("result = %+v, want failed with the repair run's cause", r)
		}
	})
}

func TestRun_RepairRunFailureKeepsFormatContext(t *testing.T) {
	f := &fake{}
	f.respond = func(n int, _ call) runner.RunResult {
		if n == 1 {
			return completed("no block")
		}
		return runner.RunResult{Status: runner.StatusFailed, Cause: &runner.Cause{Kind: runner.CauseNoAnswer}}
	}
	r := Run(context.Background(), newRequest(t, f, patch("cmd/a.go", "x")))[0]
	if len(r.Failures) != 1 {
		t.Fatalf("Failures = %+v, want one", r.Failures)
	}
	fl := r.Failures[0]
	const want = "The model's answer was not in the required format (no json code block found in the answer), and the repair run failed:"
	if fl.Message != want {
		t.Errorf("Message = %q, want %q", fl.Message, want)
	}
	if fl.Cause == nil || fl.Cause.Kind != runner.CauseNoAnswer {
		t.Errorf("Cause = %+v, want the repair run's cause", fl.Cause)
	}
}

func TestRun_PreviousFindingsAttached(t *testing.T) {
	const prevFP = "abcdefabcdef"
	prev := []finding.Finding{{File: "a/x.go", Line: 1, Severity: finding.Blocking, Title: "Old title", Explanation: "e", Fingerprint: prevFP}}
	kept := finding.Finding{File: "a/x.go", Line: 1, Severity: finding.Blocking, Title: "Reworded title", Explanation: "e", Fingerprint: prevFP}
	fresh := finding.Finding{File: "b/y.go", Line: 1, Severity: finding.Suggestion, Title: "New one", Explanation: "e", Fingerprint: "unknown12345"}

	f := &fake{}
	f.respond = func(_ int, c call) runner.RunResult {
		if strings.Contains(c.contents[0], "a/x.go") {
			return completed(answer(t, kept))
		}
		return completed(answer(t, fresh))
	}
	req := newRequest(t, f, twoBatchDiff())
	req.ContextLimit = twoBatchContextLimit
	req.Previous = map[string][]finding.Finding{"s": prev}

	r := Run(context.Background(), req)[0]
	if len(f.calls) != 2 {
		t.Fatalf("runner called %d times, want 2", len(f.calls))
	}
	const prevMsg = " Also attached are the findings from your previous review of an earlier revision, as JSON. Stay consistent with them: focus on what changed since, don't re-litigate unchanged code, drop findings the new changes resolve, and reuse a finding's fingerprint when it still applies."
	if got, want := sortedMessages(f.calls), []string{batchMessage(1, 2) + prevMsg, batchMessage(2, 2) + prevMsg}; !slices.Equal(got, want) {
		t.Errorf("Messages = %q, want %q", got, want)
	}
	for i, c := range f.calls {
		if !strings.Contains(c.opts.Message, "previous review") {
			t.Errorf("call %d Message doesn't mention the previous review", i+1)
		}
		if len(c.opts.Files) != 3 || !strings.HasSuffix(c.opts.Files[2], ".json") {
			t.Fatalf("call %d Files = %q, want the diff, the file list and a JSON file", i+1, c.opts.Files)
		}
		var got struct {
			Findings []finding.Finding `json:"findings"`
		}
		if err := json.Unmarshal([]byte(c.contents[2]), &got); err != nil || !slices.Equal(got.Findings, prev) {
			t.Errorf("call %d previous findings = %q (err %v), want %+v", i+1, c.contents[2], err, prev)
		}
	}

	byTitle := map[string]string{}
	for _, fd := range r.Findings {
		byTitle[fd.Title] = fd.Fingerprint
	}
	if byTitle["Reworded title"] != prevFP {
		t.Errorf("reused fingerprint = %q, want %q", byTitle["Reworded title"], prevFP)
	}
	if want := finding.Fingerprint("s", "b/y.go", "New one"); byTitle["New one"] != want {
		t.Errorf("unknown fingerprint replaced by %q, want %q", byTitle["New one"], want)
	}
	assertRemoved(t, allFiles(f.calls))
}

func TestCarriedFilesLeftOut(t *testing.T) {
	f := &fake{}
	f.respond = func(int, call) runner.RunResult { return completed(answer(t)) }
	req := newRequest(t, f, patch("a.go", "x")+patch("b.go", "y"))
	req.Carried = map[string][]string{"s": {"b.go"}}

	r := Run(context.Background(), req)[0]
	if len(f.calls) != 1 {
		t.Fatalf("runner called %d times, want 1", len(f.calls))
	}
	c := f.calls[0]
	if c.contents[0] != patch("a.go", "x") {
		t.Errorf("diff.patch = %q, want a.go only", c.contents[0])
	}
	if c.contents[1] != "- a.go\n- b.go\n" {
		t.Errorf("changed-files.md = %q, want both files listed", c.contents[1])
	}
	if !strings.Contains(c.opts.Message, carriedMessage) {
		t.Errorf("Message = %q, want it to contain the carried message", c.opts.Message)
	}
	if !slices.Equal(r.Carried, []string{"b.go"}) || !slices.Equal(r.Reviewed, []string{"a.go"}) {
		t.Errorf("Carried = %q, Reviewed = %q, want [b.go] and [a.go]", r.Carried, r.Reviewed)
	}
	assertRemoved(t, allFiles(f.calls))
}

func TestAllCarriedStartsNoRun(t *testing.T) {
	f := &fake{respond: func(int, call) runner.RunResult { return completed("") }}
	req := newRequest(t, f, patch("a.go", "x")+patch("b.go", "y"))
	req.Carried = map[string][]string{"s": {"a.go", "b.go"}}

	r := Run(context.Background(), req)[0]
	if len(f.calls) != 0 {
		t.Errorf("runner called %d times, want 0", len(f.calls))
	}
	if r.Verdict != NoBlockingIssues || len(r.Reviewed) != 0 || !slices.Equal(r.Carried, []string{"a.go", "b.go"}) {
		t.Errorf("result = %+v, want no blocking issues, nothing reviewed and both files carried", r)
	}
	if slices.Contains(r.Notes, noFilesNote) {
		t.Errorf("Notes = %q, want no %q note when files were carried", r.Notes, noFilesNote)
	}
}

func TestCarriedPathOutOfScopeIgnored(t *testing.T) {
	f := &fake{}
	f.respond = func(int, call) runner.RunResult { return completed(answer(t)) }
	req := newRequest(t, f, patch("a.go", "x")+patch("b.md", "y"), skill.Skill{Name: "s", Paths: []string{"**/*.go"}})
	req.Carried = map[string][]string{"s": {"b.md", "gone.go"}}

	r := Run(context.Background(), req)[0]
	if len(r.Carried) != 0 {
		t.Errorf("Carried = %q, want none: neither path is a file the skill reviews", r.Carried)
	}
	if strings.Contains(f.calls[0].opts.Message, carriedMessage) {
		t.Errorf("Message = %q, want no carried message", f.calls[0].opts.Message)
	}
	if !slices.Equal(r.Reviewed, []string{"a.go"}) {
		t.Errorf("Reviewed = %q, want [a.go]", r.Reviewed)
	}
}

func TestPreviousDropsCarriedFindings(t *testing.T) {
	onA := finding.Finding{File: "a.go", Line: 1, Severity: finding.Suggestion, Title: "On a", Explanation: "e", Fingerprint: "aaaaaaaaaaaa"}
	onB := finding.Finding{File: "b.go", Line: 1, Severity: finding.Suggestion, Title: "On b", Explanation: "e", Fingerprint: "bbbbbbbbbbbb"}
	f := &fake{}
	f.respond = func(int, call) runner.RunResult { return completed(answer(t)) }
	req := newRequest(t, f, patch("a.go", "x")+patch("b.go", "y"))
	req.Carried = map[string][]string{"s": {"b.go"}}
	req.Previous = map[string][]finding.Finding{"s": {onA, onB}}

	Run(context.Background(), req)
	if len(f.calls) != 1 || len(f.calls[0].contents) != 3 {
		t.Fatalf("calls = %+v, want one run with the previous findings attached", f.calls)
	}
	var got struct {
		Findings []finding.Finding `json:"findings"`
	}
	if err := json.Unmarshal([]byte(f.calls[0].contents[2]), &got); err != nil || !slices.Equal(got.Findings, []finding.Finding{onA}) {
		t.Errorf("previous findings = %q (err %v), want only the one on a.go", f.calls[0].contents[2], err)
	}
}

func TestPreviousCarriedFindingsEdgeCases(t *testing.T) {
	at := func(file string) finding.Finding {
		return finding.Finding{File: file, Line: 1, Severity: finding.Suggestion, Title: "On " + file, Explanation: "e", Fingerprint: "fp-" + file}
	}
	attached := func(t *testing.T, c call) []finding.Finding {
		t.Helper()
		var got struct {
			Findings []finding.Finding `json:"findings"`
		}
		if len(c.contents) != 3 {
			t.Fatalf("Files = %q, want the previous findings attached", c.opts.Files)
		}
		if err := json.Unmarshal([]byte(c.contents[2]), &got); err != nil {
			t.Fatal(err)
		}
		return got.Findings
	}

	t.Run("out-of-scope carried path keeps its findings", func(t *testing.T) {
		f := &fake{}
		f.respond = func(int, call) runner.RunResult { return completed(answer(t)) }
		req := newRequest(t, f, patch("a.go", "x")+patch("b.go", "y")+patch("c.md", "z"), skill.Skill{Name: "s", Paths: []string{"**/*.go"}})
		req.Carried = map[string][]string{"s": {"b.go", "c.md"}}
		req.Previous = map[string][]finding.Finding{"s": {at("a.go"), at("b.go"), at("c.md")}}

		Run(context.Background(), req)
		if got, want := attached(t, f.calls[0]), []finding.Finding{at("a.go"), at("c.md")}; !slices.Equal(got, want) {
			t.Errorf("previous findings = %+v, want only b.go's dropped", got)
		}
	})

	t.Run("nothing left attaches nothing", func(t *testing.T) {
		f := &fake{}
		f.respond = func(int, call) runner.RunResult { return completed(answer(t)) }
		req := newRequest(t, f, patch("a.go", "x")+patch("b.go", "y"))
		req.Carried = map[string][]string{"s": {"b.go"}}
		req.Previous = map[string][]finding.Finding{"s": {at("b.go")}}

		Run(context.Background(), req)
		c := f.calls[0]
		if len(c.opts.Files) != 2 {
			t.Errorf("Files = %q, want only the diff and the file list", c.opts.Files)
		}
		if strings.Contains(c.opts.Message, "previous review") {
			t.Errorf("Message = %q, want no previous-review message", c.opts.Message)
		}
	})
}

func TestRun_ResultsInSkillOrder(t *testing.T) {
	f := &fake{}
	f.respond = func(int, call) runner.RunResult { return completed(answer(t)) }
	req := newRequest(t, f, patch("cmd/a.go", "x"),
		skill.Skill{Name: "first"}, skill.Skill{Name: "second", Paths: []string{"docs/**"}}, skill.Skill{Name: "third"})

	got := Run(context.Background(), req)
	var names []string
	for _, r := range got {
		names = append(names, r.Skill)
	}
	if want := []string{"first", "second", "third"}; !slices.Equal(names, want) {
		t.Errorf("skills = %q, want %q", names, want)
	}
}

func TestRun_GeneratesConfigPerRun(t *testing.T) {
	s := skill.Skill{Name: "s", Body: "Check the manifests."}
	var configs []string
	f := &fake{}
	f.respond = func(_ int, c call) runner.RunResult {
		data, err := os.ReadFile(c.opts.ConfigPath)
		if err != nil {
			t.Errorf("reading config %q: %v", c.opts.ConfigPath, err)
		}
		var cfg struct {
			Agent map[string]struct {
				Prompt string `json:"prompt"`
			} `json:"agent"`
		}
		json.Unmarshal(data, &cfg)
		if got := cfg.Agent[config.AgentName].Prompt; got != s.Prompt() {
			t.Errorf("agent prompt = %q, want the skill's Prompt()", got)
		}
		configs = append(configs, c.opts.ConfigPath)
		return completed(answer(t))
	}
	req := newRequest(t, f, patch("cmd/a.go", "x"), s)
	t.Setenv("OPENCODE_PROVIDER", "anthropic")
	t.Setenv("OPENCODE_MODEL", "test-model")
	req.ContextLimit = 100000

	Run(context.Background(), req)
	if len(f.calls) != 1 {
		t.Fatalf("runner called %d times, want 1", len(f.calls))
	}
	if o := f.calls[0].opts; o.StepLimit != 20 || o.ContextLimit != 100000 {
		t.Errorf("StepLimit = %d, ContextLimit = %d, want 20 and 100000", o.StepLimit, o.ContextLimit)
	}
	assertRemoved(t, configs)
}

func TestRun_UnsplittableDiffFailsEverySkill(t *testing.T) {
	f := &fake{respond: func(int, call) runner.RunResult { return completed(answer(t)) }}
	req := newRequest(t, f, "--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-old\n+new\n",
		skill.Skill{Name: "one", Body: "Review it."}, skill.Skill{Name: "two", Body: "Review it."})

	got := Run(context.Background(), req)
	if len(f.calls) != 0 {
		t.Errorf("runner called %d times, want 0", len(f.calls))
	}
	const want = "Not reviewed: pergent could not split the diff into files (unrecognised diff format)."
	for _, r := range got {
		if r.Verdict != Failed {
			t.Errorf("%s: Verdict = %q, want %q", r.Skill, r.Verdict, Failed)
		}
		if len(r.Failures) != 1 || r.Failures[0].Message != want {
			t.Errorf("%s: Failures = %+v, want one with message %q", r.Skill, r.Failures, want)
		}
		if len(r.Notes) != 0 {
			t.Errorf("%s: Notes = %q, want none", r.Skill, r.Notes)
		}
	}
}

func TestRun_EmptyDiffIsNoFilesInScope(t *testing.T) {
	f := &fake{respond: func(int, call) runner.RunResult { return completed(answer(t)) }}
	r := Run(context.Background(), newRequest(t, f, " \n"))[0]
	if r.Verdict != NoBlockingIssues || len(r.Failures) != 0 {
		t.Errorf("result = %+v, want no blocking issues and no failures for an empty diff", r)
	}
}

func TestRun_FileWithoutPlatformDiffIsNotReviewed(t *testing.T) {
	missing := "diff --git a/big.go b/big.go\n" + diff.NotReviewedHeader + "GitLab did not return this file's diff (too large)\n"
	f := &fake{respond: func(int, call) runner.RunResult { return completed(answer(t)) }}
	req := newRequest(t, f, patch("a/x.go", "x")+missing)

	r := Run(context.Background(), req)[0]
	if r.Verdict != Failed {
		t.Errorf("Verdict = %q, want %q", r.Verdict, Failed)
	}
	want := BatchFailure{Files: []string{"big.go"}, Message: "Not reviewed: `big.go` — GitLab did not return this file's diff (too large)."}
	if len(r.Failures) != 1 || !slices.Equal(r.Failures[0].Files, want.Files) || r.Failures[0].Message != want.Message {
		t.Errorf("Failures = %+v, want [%+v]", r.Failures, want)
	}
	if len(r.Skipped) != 0 {
		t.Errorf("Skipped = %+v, want none (the file was not skipped)", r.Skipped)
	}
	if len(f.calls) != 1 || strings.Contains(f.calls[0].contents[0], "big.go") {
		t.Errorf("want one run whose diff leaves out big.go; calls = %d", len(f.calls))
	}
}

func TestRun_LockfileWithoutPlatformDiffIsStillSkipped(t *testing.T) {
	missing := "diff --git a/go.sum b/go.sum\n" + diff.NotReviewedHeader + "GitLab did not return this file's diff (too large)\n"
	f := &fake{respond: func(int, call) runner.RunResult { return completed(answer(t)) }}
	r := Run(context.Background(), newRequest(t, f, patch("a/x.go", "x")+missing))[0]
	if r.Verdict != NoBlockingIssues || len(r.Failures) != 0 {
		t.Errorf("result = %+v, want a pass with go.sum skipped as a lockfile", r)
	}
	if len(r.Skipped) != 1 || r.Skipped[0].Rule != "lockfile" {
		t.Errorf("Skipped = %+v, want go.sum skipped as a lockfile", r.Skipped)
	}
}

func TestRun_OversizeMessageNamesTheFile(t *testing.T) {
	f := &fake{respond: func(int, call) runner.RunResult { return completed(answer(t)) }}
	req := newRequest(t, f, patch("big/huge.go", strings.Repeat("z", 10000)))
	req.ContextLimit = 8000 // per-batch budget of 2,000 tokens

	r := Run(context.Background(), req)[0]
	const want = "Not reviewed: `big/huge.go` \u2014 its diff is about 2,524 tokens, larger than the per-batch limit of 2,000."
	if len(r.Failures) != 1 || r.Failures[0].Message != want {
		t.Errorf("Failures = %+v, want one with message %q", r.Failures, want)
	}
	if r.Batches != 0 || len(f.calls) != 0 {
		t.Errorf("Batches = %d, calls = %d, want no batch and no run", r.Batches, len(f.calls))
	}
}

var rejectedBoth = []finding.Rejected{
	{File: "a/x.go", Title: "Missing nil check", Severity: "blocking", Reason: "The caller never passes nil."},
	{File: "a/y.go", Title: "Rename this variable", Severity: "suggestion", Reason: "Matches the package style."},
}

// rejectedContents returns the rejected-findings.json a run was given.
func rejectedContents(t *testing.T, c call) (rejected []finding.Rejected, ok bool) {
	t.Helper()
	for i, p := range c.opts.Files {
		if filepath.Base(p) != rejectedFile {
			continue
		}
		var got struct {
			Rejected []finding.Rejected `json:"rejected"`
		}
		if err := json.Unmarshal([]byte(c.contents[i]), &got); err != nil {
			t.Fatalf("%s = %q: %v", rejectedFile, c.contents[i], err)
		}
		return got.Rejected, true
	}
	return nil, false
}

func TestDismissalsAttachedToBatch(t *testing.T) {
	f := &fake{}
	f.respond = func(int, call) runner.RunResult { return completed(answer(t)) }
	req := newRequest(t, f, patch("a/x.go", "x"))
	req.Dismissals = map[string][]finding.Rejected{"s": rejectedBoth}

	Run(context.Background(), req)
	if len(f.calls) != 1 {
		t.Fatalf("runner called %d times, want 1", len(f.calls))
	}
	c := f.calls[0]
	got, ok := rejectedContents(t, c)
	if !ok || !slices.Equal(got, rejectedBoth) {
		t.Errorf("rejected = %+v (attached %v), want %+v", got, ok, rejectedBoth)
	}
	if !strings.Contains(c.opts.Message, rejectedMessage) {
		t.Errorf("Message = %q, want it to contain the rejected message", c.opts.Message)
	}
	assertRemoved(t, allFiles(f.calls))
}

func TestNoDismissalsNoAttachment(t *testing.T) {
	cases := map[string]map[string][]finding.Rejected{
		"nil":            nil,
		"empty list":     {"s": {}},
		"other skill":    {"other": rejectedBoth},
		"other and none": {"other": rejectedBoth, "s": nil},
	}
	for name, dismissals := range cases {
		t.Run(name, func(t *testing.T) {
			f := &fake{}
			f.respond = func(int, call) runner.RunResult { return completed(answer(t)) }
			req := newRequest(t, f, patch("a/x.go", "x"))
			req.Dismissals = dismissals

			Run(context.Background(), req)
			if len(f.calls) != 1 {
				t.Fatalf("runner called %d times, want 1", len(f.calls))
			}
			c := f.calls[0]
			if _, ok := rejectedContents(t, c); ok || len(c.opts.Files) != 2 {
				t.Errorf("Files = %q, want only the diff and the file list", c.opts.Files)
			}
			if c.opts.Message != singleBatchMessage {
				t.Errorf("Message = %q, want %q", c.opts.Message, singleBatchMessage)
			}
		})
	}
}

func TestDismissalsAttachedToCrossBatch(t *testing.T) {
	f := &fake{}
	f.respond = func(int, call) runner.RunResult { return completed(answer(t)) }
	req, _ := crossBatchRequest(t, f, true)
	req.Dismissals = map[string][]finding.Rejected{"s": rejectedBoth}

	Run(context.Background(), req)
	var cross []call
	for _, c := range f.calls {
		if isCrossBatchCall(c) {
			cross = append(cross, c)
		}
	}
	if len(cross) != 1 {
		t.Fatalf("%d cross-batch calls, want 1", len(cross))
	}
	got, ok := rejectedContents(t, cross[0])
	if !ok || !slices.Equal(got, rejectedBoth) {
		t.Errorf("rejected = %+v (attached %v), want %+v", got, ok, rejectedBoth)
	}
	if want := wantCrossBatchMessage(3) + " " + rejectedMessage; cross[0].opts.Message != want {
		t.Errorf("Message = %q, want %q", cross[0].opts.Message, want)
	}
	assertRemoved(t, allFiles(f.calls))
}

// isVerifyRun reports whether the run was the verification run, with or
// without the rejected-findings text.
func isVerifyRun(c call) bool { return strings.HasPrefix(c.opts.Message, verifyMessage) }

func verifyDismissalsRun(t *testing.T, rejected []finding.Rejected) call {
	t.Helper()
	f := &fake{}
	f.respond = func(_ int, c call) runner.RunResult {
		if isVerifyRun(c) {
			return completed(verifyAnswer(t))
		}
		return completed(answer(t, blockingTwoAndSuggestion()...))
	}
	req := newRequest(t, f, patch("a/x.go", "x"))
	req.Budget = 5 * time.Minute
	req.Dismissals = map[string][]finding.Rejected{"s": rejected}

	Run(context.Background(), req)
	for _, c := range f.calls {
		if isVerifyRun(c) {
			return c
		}
	}
	t.Fatal("verification did not run")
	return call{}
}

func TestVerificationGetsOnlyBlockingDismissals(t *testing.T) {
	c := verifyDismissalsRun(t, rejectedBoth)

	got, ok := rejectedContents(t, c)
	if want := rejectedBoth[:1]; !ok || !slices.Equal(got, want) {
		t.Errorf("rejected = %+v (attached %v), want %+v", got, ok, want)
	}
	if !strings.HasSuffix(c.opts.Message, "Judge only the findings in blocking-findings.json; rejected-findings.json is context.") {
		t.Errorf("Message = %q, want it to end by naming the file to judge", c.opts.Message)
	}
	if want := verifyMessage + " " + rejectedVerifyMessage; c.opts.Message != want {
		t.Errorf("Message = %q, want %q", c.opts.Message, want)
	}
	if len(c.opts.Files) != 2 {
		t.Errorf("Files = %q, want the findings and the rejected findings", c.opts.Files)
	}
}

func TestVerificationSkipsSuggestionOnlyDismissals(t *testing.T) {
	// Any severity other than exactly "blocking" is not blocking.
	c := verifyDismissalsRun(t, []finding.Rejected{
		rejectedBoth[1],
		{File: "a/x.go", Title: "Odd", Severity: "Blocking", Reason: "r"},
		{File: "a/x.go", Title: "Odder", Severity: "", Reason: "r"},
	})

	if _, ok := rejectedContents(t, c); ok || len(c.opts.Files) != 1 {
		t.Errorf("Files = %q, want only the findings", c.opts.Files)
	}
	if c.opts.Message != verifyMessage {
		t.Errorf("Message = %q, want %q", c.opts.Message, verifyMessage)
	}
}

func TestRepairGetsNoDismissals(t *testing.T) {
	f := &fake{}
	f.respond = func(n int, _ call) runner.RunResult {
		if n == 1 {
			return completed("The code looks fine.")
		}
		return completed(answer(t))
	}
	req := newRequest(t, f, patch("a/x.go", "x"), skill.Skill{Name: "s", Body: "Review it."})
	req.Dismissals = map[string][]finding.Rejected{"s": rejectedBoth}

	Run(context.Background(), req)
	if len(f.calls) != 2 {
		t.Fatalf("runner called %d times, want 2 (review and repair)", len(f.calls))
	}
	if _, ok := rejectedContents(t, f.calls[1]); ok || len(f.calls[1].opts.Files) != 1 {
		t.Errorf("repair Files = %q, want only the bad answer", f.calls[1].opts.Files)
	}
	if strings.Contains(f.calls[1].opts.Message, "rejected") {
		t.Errorf("repair Message = %q, want no mention of rejected findings", f.calls[1].opts.Message)
	}
}

func TestDismissalsAttachedToEveryBatch(t *testing.T) {
	f := &fake{}
	f.respond = func(int, call) runner.RunResult { return completed(answer(t)) }
	req := newRequest(t, f, twoBatchDiff())
	req.ContextLimit = twoBatchContextLimit
	req.Dismissals = map[string][]finding.Rejected{"s": rejectedBoth}

	Run(context.Background(), req)
	if len(f.calls) != 2 {
		t.Fatalf("runner called %d times, want 2", len(f.calls))
	}
	for i, c := range f.calls {
		if got, ok := rejectedContents(t, c); !ok || !slices.Equal(got, rejectedBoth) {
			t.Errorf("call %d rejected = %+v (attached %v), want %+v", i+1, got, ok, rejectedBoth)
		}
	}
}

func TestDismissalsAttachmentFailureDoesNotFailTheReview(t *testing.T) {
	orig := writeRejected
	writeRejected = func(string, []byte, os.FileMode) error { return errors.New("disk full") }
	t.Cleanup(func() { writeRejected = orig })

	f := &fake{}
	f.respond = func(_ int, c call) runner.RunResult {
		switch {
		case isVerifyRun(c):
			return completed(verifyAnswer(t))
		case isCrossBatchCall(c):
			return completed(answer(t))
		}
		return completed(answer(t, finding.Finding{File: "a/x.go", Line: 1, Severity: finding.Blocking, Title: "A bug", Explanation: "e"}))
	}
	req, _ := crossBatchRequest(t, f, true)
	req.Dismissals = map[string][]finding.Rejected{"s": rejectedBoth}

	r := Run(context.Background(), req)[0]
	// Three batches, the cross-batch run and the verification run all ran.
	if len(f.calls) != 5 {
		t.Fatalf("runner called %d times, want 5", len(f.calls))
	}
	for i, c := range f.calls {
		if _, ok := rejectedContents(t, c); ok {
			t.Errorf("call %d has %s, want none", i+1, rejectedFile)
		}
		if strings.Contains(c.opts.Message, "rejected") {
			t.Errorf("call %d Message = %q, want no text about rejected findings", i+1, c.opts.Message)
		}
	}
	if r.Verdict != HasBlockingIssues || len(r.Failures) != 0 {
		t.Errorf("verdict = %s, failures = %+v, want has_blocking_issues and none", r.Verdict, r.Failures)
	}
	n := 0
	for _, note := range r.Notes {
		if note == dismissalsNote {
			n++
		}
	}
	if n != 1 || len(r.Notes) != 1 {
		t.Errorf("Notes = %q, want exactly one %q", r.Notes, dismissalsNote)
	}
	assertRemoved(t, allFiles(f.calls))
}

// leftOutAnswer returns a model answer with no findings that reports left_out.
func leftOutAnswer(t *testing.T, leftOut ...finding.LeftOut) string {
	t.Helper()
	data, err := json.Marshal(map[string]any{"findings": []finding.Finding{}, "left_out": leftOut})
	if err != nil {
		t.Fatal(err)
	}
	return "```json\n" + string(data) + "\n```\n"
}

// leftOutByRun answers each batch run with its entry list, by batch number,
// and the cross-batch run with cross.
func leftOutByRun(t *testing.T, batches map[int][]finding.LeftOut, cross []finding.LeftOut) *fake {
	t.Helper()
	f := &fake{}
	f.respond = func(_ int, c call) runner.RunResult {
		if isCrossBatchCall(c) {
			return completed(leftOutAnswer(t, cross...))
		}
		for n, entries := range batches {
			if strings.HasPrefix(c.opts.Message, batchMessage(n, 3)) {
				return completed(leftOutAnswer(t, entries...))
			}
		}
		t.Errorf("unexpected run with message %q", c.opts.Message)
		return completed(answer(t))
	}
	return f
}

func TestLeftOutNote(t *testing.T) {
	f := &fake{}
	f.respond = func(int, call) runner.RunResult {
		return completed(leftOutAnswer(t, finding.LeftOut{Title: "A", Rejected: "B"}))
	}
	req := newRequest(t, f, patch("a/x.go", "x"))
	req.Dismissals = map[string][]finding.Rejected{"s": rejectedBoth}

	r := Run(context.Background(), req)[0]
	if want := []string{"Left out because of past dismissals: A (B)"}; !slices.Equal(r.Notes, want) {
		t.Errorf("Notes = %q, want %q", r.Notes, want)
	}
	if r.Verdict != NoBlockingIssues {
		t.Errorf("Verdict = %q, want %q", r.Verdict, NoBlockingIssues)
	}
}

func TestLeftOutNoteAcrossBatches(t *testing.T) {
	f := leftOutByRun(t,
		map[int][]finding.LeftOut{
			1: {{Title: "A", Rejected: "B"}, {Title: "C", Rejected: "D"}},
			2: {{Title: "C", Rejected: "D"}, {Title: "E", Rejected: "F"}},
			3: nil,
		},
		[]finding.LeftOut{{Title: "A", Rejected: "B"}, {Title: "G", Rejected: "H"}})
	req, _ := crossBatchRequest(t, f, true)
	req.Dismissals = map[string][]finding.Rejected{"s": rejectedBoth}

	r := Run(context.Background(), req)[0]
	if len(f.calls) != 4 {
		t.Fatalf("runner called %d times, want 4 (three batches and the cross-batch run)", len(f.calls))
	}
	want := "Left out because of past dismissals: A (B); C (D); E (F); G (H)"
	if len(r.Notes) != 1 || r.Notes[0] != want {
		t.Errorf("Notes = %q, want one note %q", r.Notes, want)
	}
}

func TestLeftOutNoteBesideOtherNotes(t *testing.T) {
	f := &fake{}
	f.respond = func(_ int, c call) runner.RunResult {
		if isCrossBatchCall(c) {
			return completed("Looks fine to me.")
		}
		return completed(leftOutAnswer(t, finding.LeftOut{Title: "A", Rejected: "B"}))
	}
	req, _ := crossBatchRequest(t, f, true)
	req.Dismissals = map[string][]finding.Rejected{"s": rejectedBoth}

	r := Run(context.Background(), req)[0]
	if len(r.Notes) != 2 || !strings.HasPrefix(r.Notes[0], "Cross-batch check failed") || r.Notes[1] != "Left out because of past dismissals: A (B)" {
		t.Errorf("Notes = %q, want the cross-batch note and then the left-out note", r.Notes)
	}
	if r.Verdict != NoBlockingIssues {
		t.Errorf("Verdict = %q, want %q: a note never changes it", r.Verdict, NoBlockingIssues)
	}
}

func TestLeftOutNoteWithoutRejected(t *testing.T) {
	f := &fake{}
	f.respond = func(int, call) runner.RunResult {
		return completed(leftOutAnswer(t, finding.LeftOut{Title: "A"}))
	}
	req := newRequest(t, f, patch("a/x.go", "x"))
	req.Dismissals = map[string][]finding.Rejected{"s": rejectedBoth}

	r := Run(context.Background(), req)[0]
	if want := "Left out because of past dismissals: A"; len(r.Notes) != 1 || r.Notes[0] != want {
		t.Errorf("Notes = %q, want one note %q", r.Notes, want)
	}
}

func TestLeftOutIgnoredWithoutAttachment(t *testing.T) {
	f := &fake{}
	f.respond = func(int, call) runner.RunResult {
		return completed(leftOutAnswer(t, finding.LeftOut{Title: "A", Rejected: "B"}))
	}
	req := newRequest(t, f, patch("a/x.go", "x"))
	req.Dismissals = map[string][]finding.Rejected{"other": rejectedBoth}

	r := Run(context.Background(), req)[0]
	if len(r.Notes) != 0 {
		t.Errorf("Notes = %q, want none: the skill had no past dismissals", r.Notes)
	}
}

func TestLeftOutNeverRepairs(t *testing.T) {
	f := &fake{}
	f.respond = func(int, call) runner.RunResult {
		return completed("```json\n{\"findings\": [], \"left_out\": \"A\"}\n```\n")
	}
	req := newRequest(t, f, patch("a/x.go", "x"))
	req.Dismissals = map[string][]finding.Rejected{"s": rejectedBoth}

	r := Run(context.Background(), req)[0]
	if len(f.calls) != 1 {
		t.Errorf("runner called %d times, want 1 (no repair run)", len(f.calls))
	}
	if r.Verdict != NoBlockingIssues || len(r.Failures) != 0 || len(r.Notes) != 0 {
		t.Errorf("result = %+v, want a pass with no failures and no notes", r)
	}
}

func TestLeftOutNoteCapped(t *testing.T) {
	entries := func(from, to int) []finding.LeftOut {
		var out []finding.LeftOut
		for i := from; i < to; i++ {
			out = append(out, finding.LeftOut{Title: fmt.Sprintf("T%d", i), Rejected: fmt.Sprintf("R%d", i)})
		}
		return out
	}
	want := func() string {
		var parts []string
		for _, e := range entries(0, 20) {
			parts = append(parts, e.Title+" ("+e.Rejected+")")
		}
		return "Left out because of past dismissals: " + strings.Join(parts, "; ") + " and 5 more"
	}()
	for name, batches := range map[string]map[int][]finding.LeftOut{
		"across batches": {1: entries(0, 12), 2: entries(12, 25), 3: nil},
		"one answer":     {1: entries(0, 25), 2: nil, 3: nil},
	} {
		t.Run(name, func(t *testing.T) {
			f := leftOutByRun(t, batches, nil)
			req, _ := crossBatchRequest(t, f, false)
			req.Dismissals = map[string][]finding.Rejected{"s": rejectedBoth}

			r := Run(context.Background(), req)[0]
			if len(r.Notes) != 1 || r.Notes[0] != want {
				t.Errorf("Notes = %q, want one note %q", r.Notes, want)
			}
		})
	}
}

func TestLeftOutFromRepairedAnswer(t *testing.T) {
	f := &fake{}
	f.respond = func(n int, _ call) runner.RunResult {
		if n == 1 {
			return completed("The code looks fine.")
		}
		return completed(leftOutAnswer(t, finding.LeftOut{Title: "A", Rejected: "B"}))
	}
	req := newRequest(t, f, patch("a/x.go", "x"))
	req.Dismissals = map[string][]finding.Rejected{"s": rejectedBoth}

	r := Run(context.Background(), req)[0]
	if len(f.calls) != 2 {
		t.Fatalf("runner called %d times, want 2 (review and repair)", len(f.calls))
	}
	if want := "Left out because of past dismissals: A (B)"; len(r.Notes) != 1 || r.Notes[0] != want {
		t.Errorf("Notes = %q, want one note %q", r.Notes, want)
	}
}

func TestLeftOutFromFailedBatchIgnored(t *testing.T) {
	f := &fake{}
	f.respond = func(_ int, c call) runner.RunResult {
		switch {
		case strings.HasPrefix(c.opts.Message, batchMessage(1, 3)):
			return completed(leftOutAnswer(t, finding.LeftOut{Title: "A", Rejected: "B"}))
		case strings.HasPrefix(c.opts.Message, batchMessage(2, 3)):
			// The failed run's text is never an answer.
			return runner.RunResult{
				Status: runner.StatusFailed,
				Cause:  &runner.Cause{Kind: runner.CauseTimeout, Timeout: time.Minute},
				Output: leftOutAnswer(t, finding.LeftOut{Title: "C", Rejected: "D"}),
			}
		}
		return completed(answer(t))
	}
	req, _ := crossBatchRequest(t, f, false)
	req.Dismissals = map[string][]finding.Rejected{"s": rejectedBoth}

	r := Run(context.Background(), req)[0]
	if r.Verdict != Failed {
		t.Errorf("Verdict = %q, want %q", r.Verdict, Failed)
	}
	if want := "Left out because of past dismissals: A (B)"; len(r.Notes) != 1 || r.Notes[0] != want {
		t.Errorf("Notes = %q, want one note %q", r.Notes, want)
	}
}
