package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/gopaytech/pergent/internal/result"
	"github.com/gopaytech/pergent/internal/review"
	"github.com/gopaytech/pergent/internal/runner"
)

const (
	panelModelA = "anthropic/model-a"
	panelModelB = "openai/model-b"
)

// panelRunEnv is the environment of a panel merge step: model A in
// OPENCODE_*, model B in PERGENT_PANEL_B_*, and a result token.
func panelRunEnv(t *testing.T) func(string) string {
	t.Helper()
	t.Setenv("OPENCODE_PROVIDER", "anthropic")
	t.Setenv("OPENCODE_MODEL", "model-a")
	return panelEnv(map[string]string{
		"OPENCODE_MODEL":          "model-a",
		"OPENCODE_API_KEY":        "key-a-0123456789",
		"PERGENT_PANEL_B_API_KEY": "key-b-0123456789",
		"PERGENT_RESULT_TOKEN":    "result-token",
	})
}

// reviewSkill is a member's review of skill "review" with the given findings.
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

// memberCompleted is a completed member result with the given skills.
func memberCompleted(skills ...result.Skill) result.Result {
	return result.Result{SchemaVersion: result.SchemaVersion, Status: result.StatusCompleted, Skills: skills}
}

// writeMember writes r, encoded, to name in a directory outside the checkout.
func writeMember(t *testing.T, name string, r result.Result) string {
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

// memberFile writes a member's result for the checkout's base and head,
// and returns its path.
type memberFile func(t *testing.T, base, head string) string

// member is r as a member's review of the checkout's change.
func member(name string, r result.Result) memberFile {
	return func(t *testing.T, base, head string) string {
		r.Base, r.Head = base, head
		return writeMember(t, name, r)
	}
}

// staleMember is r as a member's review of another head.
func staleMember(name string, r result.Result) memberFile {
	return func(t *testing.T, base, _ string) string {
		r.Base, r.Head = base, strings.Repeat("f", 40)
		return writeMember(t, name, r)
	}
}

// missingMember is a member file that does not exist.
func missingMember(t *testing.T, _, _ string) string {
	return filepath.Join(t.TempDir(), "missing.json")
}

var (
	panelFindA = result.Finding{Fingerprint: "fa1", File: "a.go", Line: 3, Severity: "blocking", Title: "Nil map write", Explanation: "e"}
	panelFindB = result.Finding{Fingerprint: "fb1", File: "a.go", Line: 3, Severity: "blocking", Title: "The map is nil", Explanation: "e"}
)

// panelRunCall is one run the fake saw: its model and attached file names.
type panelRunCall struct {
	model string
	files []string
	opts  runner.Options
}

// panelRuns answers each run by its model and kind: a judge run (it has
// their-findings.json) with judge[model], a verification run with verify.
type panelRuns struct {
	t      *testing.T
	judge  map[string]string
	verify string
	// verifyCause, when set, makes each verification run fail with it.
	verifyCause *runner.Cause
	mu          sync.Mutex
	calls       []panelRunCall
}

func (p *panelRuns) run(_ context.Context, o runner.Options) runner.RunResult {
	cfg, err := os.ReadFile(o.ConfigPath)
	if err != nil {
		p.t.Errorf("config: %v", err)
	}
	var c struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(cfg, &c)
	call := panelRunCall{model: c.Model, opts: o}
	for _, f := range o.Files {
		call.files = append(call.files, filepath.Base(f))
	}
	p.mu.Lock()
	p.calls = append(p.calls, call)
	p.mu.Unlock()
	out := p.verify
	if slices.Contains(call.files, "their-findings.json") {
		out = p.judge[c.Model]
	} else if p.verifyCause != nil {
		return runner.RunResult{Status: runner.StatusFailed, Cause: p.verifyCause}
	}
	return runner.RunResult{Status: runner.StatusCompleted, Output: out, Usage: runner.Usage{Input: 5, Output: 1}}
}

// checkAnswer is a judge's or verification's answer with the given results.
func checkAnswer(t *testing.T, results ...map[string]any) string {
	t.Helper()
	if results == nil {
		results = []map[string]any{}
	}
	data, err := json.Marshal(map[string]any{"results": results})
	if err != nil {
		t.Fatal(err)
	}
	return "Checked.\n\n```json\n" + string(data) + "\n```\n"
}

// runPanelAgent runs `pergent agent --panel` against a result server and
// returns the exit code, the delivered result and stderr.
func runPanelAgent(t *testing.T, a, b memberFile, runs *panelRuns, extra ...string) (int, *resultServer, string) {
	t.Helper()
	env := panelRunEnv(t)
	dir, base, head := agentRepo(t, nil)
	srv := newResultServer(t, http.StatusOK)
	args := append([]string{"--repo-path", dir, "--base", base, "--head", head, "--skills-dir", oneSkill(t),
		"--result-url", srv.URL, "--panel", "--panel-a", a(t, base, head), "--panel-b", b(t, base, head)}, extra...)
	var stdout, stderr bytes.Buffer
	code := runAgent(context.Background(), args, env, &stdout, &stderr, runs.run)
	return code, srv, stderr.String()
}

// checkKeys checks that each run got only its own model's key.
func checkKeys(t *testing.T, calls []panelRunCall) {
	t.Helper()
	for _, c := range calls {
		want := "OPENCODE_API_KEY=key-a-0123456789"
		if c.model == panelModelB {
			want = "OPENCODE_API_KEY=key-b-0123456789"
		}
		if !slices.Equal(c.opts.Env, []string{want}) {
			t.Errorf("run on %s: Env = %v", c.model, c.opts.Env)
		}
		for _, name := range []string{"PERGENT_PANEL_B_API_KEY", "OPENCODE_API_KEY", "PERGENT_RESULT_TOKEN"} {
			if !slices.Contains(c.opts.ExcludeEnv, name) {
				t.Errorf("run on %s: ExcludeEnv = %v, want %s", c.model, c.opts.ExcludeEnv, name)
			}
		}
	}
}

func TestRunAgentPanelBoth(t *testing.T) {
	runs := &panelRuns{t: t, judge: map[string]string{
		panelModelB: checkAnswer(t, map[string]any{"id": "a1", "real": true, "reason": "yes", "same_as": "b1"}),
		panelModelA: checkAnswer(t, map[string]any{"id": "b1", "real": true, "reason": "yes", "same_as": "a1"}),
	}}
	a := member("a.json", memberCompleted(reviewSkill(panelFindA)))
	b := member("b.json", memberCompleted(reviewSkill(panelFindB)))

	code, srv, stderr := runPanelAgent(t, a, b, runs)
	if code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, stderr)
	}
	r := srv.only(t)
	if r.Panel == nil || r.Panel.Mode != "both" || !r.Panel.Judged {
		t.Fatalf("panel = %+v", r.Panel)
	}
	if r.Panel.A != (result.PanelMember{Provider: "anthropic", Model: "model-a"}) || r.Panel.B != (result.PanelMember{Provider: "openai", Model: "model-b"}) {
		t.Errorf("panel members = %+v", r.Panel)
	}
	s, ok := skillNamed(r, "review")
	if !ok || len(s.Findings) != 1 {
		t.Fatalf("skill = %+v", s)
	}
	f := s.Findings[0]
	if f.Severity != "blocking" || f.Panel == nil || f.Panel.FoundBy != "both" || !slices.Equal(f.Panel.Also, []string{"fb1"}) {
		t.Errorf("finding = %+v, panel %+v", f, f.Panel)
	}
	if s.Verdict != "has_blocking_issues" || s.Panel == nil || s.Panel.Mode != "both" || len(s.Panel.Judges) != 2 {
		t.Errorf("skill verdict %s, panel %+v", s.Verdict, s.Panel)
	}
	if len(s.TokensByModel) != 2 || s.TokensByModel[0].Role != "a" || s.TokensByModel[1].Role != "b" ||
		s.TokensByModel[1].Model != "model-b" || s.TokensByModel[0].Tokens.Input != 5 {
		t.Errorf("tokens_by_model = %+v", s.TokensByModel)
	}
	if !strings.Contains(r.Comment.Markdown, "Panel: anthropic/model-a (A) + openai/model-b (B)") {
		t.Errorf("comment lacks the panel header:\n%s", r.Comment.Markdown)
	}
	if len(runs.calls) != 2 {
		t.Errorf("%d runs, want 2 judge runs", len(runs.calls))
	}
	checkKeys(t, runs.calls)
	for _, key := range []string{"key-a-0123456789", "key-b-0123456789"} {
		if strings.Contains(stderr, key) || bytes.Contains(srv.bodies[0], []byte(key)) {
			t.Errorf("%s reached the log or the result", key)
		}
	}
}

// checkOneMember checks a merge where only role's member can be used: one
// verification run on model, and the log line for the other member.
func checkOneMember(t *testing.T, code int, srv *resultServer, stderr string, runs *panelRuns, mode, model, logLine string) {
	t.Helper()
	if code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, stderr)
	}
	r := srv.only(t)
	if r.Panel == nil || r.Panel.Mode != mode || !r.Panel.Judged {
		t.Fatalf("panel = %+v", r.Panel)
	}
	s, ok := skillNamed(r, "review")
	if !ok || s.Panel == nil || s.Panel.Mode != mode || s.Verdict != "has_blocking_issues" {
		t.Errorf("skill = %+v, panel %+v", s, s.Panel)
	}
	if len(runs.calls) != 1 || runs.calls[0].model != model || !slices.Contains(runs.calls[0].files, "blocking-findings.json") {
		t.Errorf("runs = %+v, want one verification run on %s", runs.calls, model)
	}
	checkKeys(t, runs.calls)
	if !strings.Contains(stderr, logLine) {
		t.Errorf("stderr lacks %q:\n%s", logLine, stderr)
	}
}

func TestRunAgentPanelAOnly(t *testing.T) {
	runs := &panelRuns{t: t, verify: checkAnswer(t, map[string]any{"id": "1", "real": true, "reason": "yes"})}
	a := member("a.json", memberCompleted(reviewSkill(panelFindA)))
	b := member("b.json", result.ErrorResult(result.Meta{}, result.CodeInternal, "it broke"))

	code, srv, stderr := runPanelAgent(t, a, b, runs)
	checkOneMember(t, code, srv, stderr, runs, "a_only", panelModelA, "panel: member b can't be used: status error")
}

func TestRunAgentPanelBOnly(t *testing.T) {
	runs := &panelRuns{t: t, verify: checkAnswer(t, map[string]any{"id": "1", "real": true, "reason": "yes"})}
	a := missingMember
	b := member("b.json", memberCompleted(reviewSkill(panelFindB)))

	code, srv, stderr := runPanelAgent(t, a, b, runs)
	checkOneMember(t, code, srv, stderr, runs, "b_only", panelModelB, "panel: member a can't be used: ")
	if !strings.Contains(stderr, "no such file or directory") {
		t.Errorf("stderr lacks why a can't be read:\n%s", stderr)
	}
}

// TestRunAgentPanelOneMemberVerifyProviderError: the one usable member's
// verification fails on a provider error. The result's provider_error names
// that member's role and model, so the caller can fall back along its chain.
func TestRunAgentPanelOneMemberVerifyProviderError(t *testing.T) {
	quota := &runner.Cause{Kind: runner.CauseOpencodeError, ProviderClass: runner.ClassQuota, ProviderStatus: 429}
	runs := &panelRuns{t: t, verifyCause: quota}
	a := missingMember
	b := member("b.json", memberCompleted(reviewSkill(panelFindB)))

	code, srv, stderr := runPanelAgent(t, a, b, runs)
	if code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, stderr)
	}
	r := srv.only(t)
	pe := r.ProviderError
	if pe == nil || pe.Role != "b" || pe.Class != string(runner.ClassQuota) || pe.Status != 429 || pe.Model != "model-b" {
		t.Errorf("provider_error = %+v, want b's quota error", pe)
	}
	s, ok := skillNamed(r, "review")
	if !ok || s.Panel == nil || s.Panel.Mode != "b_only" || len(s.Panel.Judges) != 1 {
		t.Fatalf("skill panel = %+v", s.Panel)
	}
	if j := s.Panel.Judges[0]; j.Role != "b" || j.Status != "failed" || j.Cause == nil || j.Cause.ProviderError == nil {
		t.Errorf("judge = %+v", j)
	}
	// A failed verification keeps the finding blocking.
	if s.Verdict != "has_blocking_issues" || len(s.Findings) != 1 {
		t.Errorf("skill = %+v", s)
	}
}

func TestRunAgentPanelNoJudge(t *testing.T) {
	runs := &panelRuns{t: t}
	a := member("a.json", memberCompleted(reviewSkill(panelFindA)))
	b := member("b.json", memberCompleted(reviewSkill(panelFindB)))

	code, srv, stderr := runPanelAgent(t, a, b, runs, "--panel-no-judge")
	if code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, stderr)
	}
	if len(runs.calls) != 0 {
		t.Errorf("%d runs started", len(runs.calls))
	}
	r := srv.only(t)
	if r.Panel == nil || r.Panel.Mode != "both" || r.Panel.Judged {
		t.Fatalf("panel = %+v", r.Panel)
	}
	s, ok := skillNamed(r, "review")
	if !ok || s.Panel == nil || !s.Panel.ExactMerge {
		t.Errorf("skill = %+v, panel %+v", s, s.Panel)
	}
}

func TestRunAgentPanelNoUsableMember(t *testing.T) {
	runs := &panelRuns{t: t}
	a := member("a.json", result.ErrorResult(result.Meta{}, result.CodeInternal, "it broke"))
	other := memberCompleted(reviewSkill(panelFindB))
	other.SchemaVersion = 2
	b := member("b.json", other)

	code, srv, stderr := runPanelAgent(t, a, b, runs)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr:\n%s", code, stderr)
	}
	if len(runs.calls) != 0 {
		t.Errorf("%d runs started", len(runs.calls))
	}
	r := srv.only(t)
	if r.Status != result.StatusError || r.Error == nil || r.Error.Code != result.CodeUsage ||
		r.Error.Message != "--panel needs at least one usable member result" {
		t.Errorf("result = status %q error %+v", r.Status, r.Error)
	}
	for _, want := range []string{"panel: member a can't be used: status error", "panel: member b can't be used: schema_version 2, want 1"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
	if strings.Contains(stderr, "it broke") || strings.Contains(string(srv.bodies[0]), "it broke") {
		t.Error("a member's own error text reached the log or the result")
	}
}

func TestRunAgentPanelSkillFailedInB(t *testing.T) {
	runs := &panelRuns{t: t, verify: checkAnswer(t, map[string]any{"id": "1", "real": true, "reason": "yes"})}
	a := member("a.json", memberCompleted(reviewSkill(panelFindA)))
	failed := reviewSkill()
	failed.Verdict = string(review.Failed)
	failed.Failures = []result.Failure{{Files: []string{"a.go"}, Message: "b broke"}}
	b := member("b.json", memberCompleted(failed))

	code, srv, stderr := runPanelAgent(t, a, b, runs)
	if code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, stderr)
	}
	r := srv.only(t)
	if r.Panel == nil || r.Panel.Mode != "both" {
		t.Fatalf("panel = %+v, want both members usable", r.Panel)
	}
	s, ok := skillNamed(r, "review")
	if !ok || s.Panel == nil || s.Panel.Mode != "a_only" {
		t.Fatalf("skill = %+v", s)
	}
	if s.Verdict != "has_blocking_issues" || len(s.Failures) != 0 || len(s.Findings) != 1 || s.Findings[0].Panel.FoundBy != "a" {
		t.Errorf("skill verdict %s, failures %+v, findings %+v", s.Verdict, s.Failures, s.Findings)
	}
	if len(runs.calls) != 1 || runs.calls[0].model != panelModelA || !slices.Contains(runs.calls[0].files, "blocking-findings.json") {
		t.Errorf("runs = %+v, want one verification run on model A", runs.calls)
	}
	checkKeys(t, runs.calls)
}

// A loaded skill that neither member reviewed fails; it is never dropped.
func TestRunAgentPanelSkillInNeitherFails(t *testing.T) {
	runs := &panelRuns{t: t}
	other := reviewSkill()
	other.Name = "other"
	a := member("a.json", memberCompleted(other))
	b := member("b.json", memberCompleted(other))

	code, srv, stderr := runPanelAgent(t, a, b, runs)
	if code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, stderr)
	}
	r := srv.only(t)
	s, ok := skillNamed(r, "review")
	if !ok || s.Verdict != "failed" || s.Panel == nil || s.Panel.Mode != "none" {
		t.Errorf("skills = %+v", r.Skills)
	}
	if len(s.Failures) != 1 || s.Failures[0].Message != review.NoMemberReview {
		t.Errorf("failures = %+v, want one that says why", s.Failures)
	}
	if !strings.Contains(r.Comment.Markdown, review.NoMemberReview) {
		t.Errorf("the comment does not say why the skill failed:\n%s", r.Comment.Markdown)
	}
	if _, ok := skillNamed(r, "other"); ok {
		t.Error("a skill P did not load is in the result")
	}
}

// A member's result for another head is not a review of this change: it
// can't be used, so it never makes the merge pass.
func TestRunAgentPanelStaleMemberIsUnusable(t *testing.T) {
	stale := `can't be used: head "` + strings.Repeat("f", 40) + `", want "`
	t.Run("one", func(t *testing.T) {
		runs := &panelRuns{t: t, verify: checkAnswer(t, map[string]any{"id": "1", "real": true, "reason": "yes"})}
		a := member("a.json", memberCompleted(reviewSkill(panelFindA)))
		b := staleMember("b.json", memberCompleted(reviewSkill()))

		code, srv, stderr := runPanelAgent(t, a, b, runs)
		checkOneMember(t, code, srv, stderr, runs, "a_only", panelModelA, "panel: member b "+stale)
	})
	t.Run("both", func(t *testing.T) {
		runs := &panelRuns{t: t}
		a := staleMember("a.json", memberCompleted(reviewSkill()))
		b := staleMember("b.json", memberCompleted(reviewSkill()))

		code, srv, stderr := runPanelAgent(t, a, b, runs)
		if code != 2 {
			t.Fatalf("exit code = %d, want 2; stderr:\n%s", code, stderr)
		}
		r := srv.only(t)
		if r.Error == nil || r.Error.Code != result.CodeUsage || r.Error.Message != noUsableMember {
			t.Errorf("error = %+v", r.Error)
		}
		if strings.Contains(string(srv.bodies[0]), strings.Repeat("f", 40)) {
			t.Error("the stale head reached the result")
		}
		for _, role := range []string{"a", "b"} {
			if !strings.Contains(stderr, "panel: member "+role+" "+stale) {
				t.Errorf("stderr lacks the stale head of %s:\n%s", role, stderr)
			}
		}
	})
}

// A member whose base differs is not a review of this change either.
func TestMemberMismatch(t *testing.T) {
	r := &result.Result{Base: "b1", Head: "h1"}
	if got := memberMismatch(r, "b1", "h1"); got != "" {
		t.Errorf("same change: %q", got)
	}
	if got := memberMismatch(r, "b2", "h1"); got != `base "b1", want "b2"` {
		t.Errorf("other base: %q", got)
	}
	if got := memberMismatch(r, "b1", "h2"); got != `head "h1", want "h2"` {
		t.Errorf("other head: %q", got)
	}
}

func TestReadMember(t *testing.T) {
	ok := writeMember(t, "ok.json", memberCompleted(reviewSkill(panelFindA)))
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(t.TempDir(), "big.json")
	if err := os.WriteFile(big, bytes.Repeat([]byte(" "), result.MaxSize+1), 0o600); err != nil {
		t.Fatal(err)
	}
	odd := memberCompleted()
	odd.Status = "weird"
	oddPath := writeMember(t, "odd.json", odd)

	if r, reason := readMember(ok); r == nil || reason != "" || len(r.Skills) != 1 {
		t.Errorf("ok: r %v, reason %q", r, reason)
	}
	for _, c := range []struct{ path, want string }{
		{bad, "decoding"},
		{big, "over the 10 MB limit"},
		{oddPath, `status "weird"`},
		{t.TempDir(), "not a regular file"},
		{filepath.Join(t.TempDir(), "missing.json"), "no such file or directory"},
	} {
		if r, reason := readMember(c.path); r != nil || !strings.Contains(reason, c.want) {
			t.Errorf("%s: r %v, reason %q, want %q", c.path, r, reason, c.want)
		}
	}
}

// A FIFO is refused before it is opened: opening one blocks until a writer
// comes.
func TestReadMemberFIFODoesNotBlock(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "member.json")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	done := make(chan string, 1)
	go func() {
		_, reason := readMember(fifo)
		done <- reason
	}()
	select {
	case reason := <-done:
		if !strings.Contains(reason, "not a regular file") {
			t.Errorf("reason = %q", reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("readMember blocked on a FIFO")
	}
}

func TestMemberSkills(t *testing.T) {
	s := reviewSkill(panelFindA)
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
	got := memberSkills(&result.Result{Skills: []result.Skill{s, odd, unfinished, twice, twiceFailed, twiceFailed, twice}})
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
	if again := memberSkills(&result.Result{Skills: []result.Skill{twiceFailed, twice}}); again["twice"].Verdict != review.Failed {
		t.Errorf("failed first: verdict = %s, want failed", again["twice"].Verdict)
	}
}

// TestMemberSkillNotReviewedIsFailed: a file the member did not review, with
// no failure and every batch finished, still makes the skill failed.
func TestMemberSkillNotReviewedIsFailed(t *testing.T) {
	s := reviewSkill(panelFindA)
	s.Files.NotReviewed = []result.NotReviewed{{Path: "a.go", Cause: "the batch failed"}}
	if got := memberSkill(s); got.Verdict != review.Failed {
		t.Errorf("verdict = %s, want failed", got.Verdict)
	}
}
