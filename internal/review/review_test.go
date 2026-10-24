package review

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gopaytech/pergent/internal/config"
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
	if c.opts.Timeout != 3*time.Minute || c.opts.SkillName != "s" || c.opts.RepoPath != req.RepoPath {
		t.Errorf("opts = %+v, want timeout 3m, skill s and the repo path", c.opts)
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
	if len(f.calls) != 1 || f.calls[0].opts.Timeout != 10*time.Minute {
		t.Errorf("calls = %+v, want one run with a 10m timeout", f.calls)
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
	for i, c := range f.calls {
		if want := batchMessage(i+1, 2); c.opts.Message != want {
			t.Errorf("call %d Message = %q, want %q", i+1, c.opts.Message, want)
		}
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
	for i, c := range f.calls {
		if want := batchMessage(i+1, 2) + prevMsg; c.opts.Message != want {
			t.Errorf("call %d Message = %q, want %q", i+1, c.opts.Message, want)
		}
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
