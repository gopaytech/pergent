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
	"testing"

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
	mu     sync.Mutex
	calls  []panelRunCall
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
func runPanelAgent(t *testing.T, a, b string, runs *panelRuns, extra ...string) (int, *resultServer, string) {
	t.Helper()
	env := panelRunEnv(t)
	dir, base, head := agentRepo(t, nil)
	srv := newResultServer(t, http.StatusOK)
	args := append([]string{"--repo-path", dir, "--base", base, "--head", head, "--skills-dir", oneSkill(t),
		"--result-url", srv.URL, "--panel", "--panel-a", a, "--panel-b", b}, extra...)
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
	a := writeMember(t, "a.json", memberCompleted(reviewSkill(panelFindA)))
	b := writeMember(t, "b.json", memberCompleted(reviewSkill(panelFindB)))

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
	a := writeMember(t, "a.json", memberCompleted(reviewSkill(panelFindA)))
	b := writeMember(t, "b.json", result.ErrorResult(result.Meta{}, result.CodeInternal, "it broke"))

	code, srv, stderr := runPanelAgent(t, a, b, runs)
	checkOneMember(t, code, srv, stderr, runs, "a_only", panelModelA, "panel: member b can't be used: status error")
}

func TestRunAgentPanelBOnly(t *testing.T) {
	runs := &panelRuns{t: t, verify: checkAnswer(t, map[string]any{"id": "1", "real": true, "reason": "yes"})}
	a := filepath.Join(t.TempDir(), "missing.json")
	b := writeMember(t, "b.json", memberCompleted(reviewSkill(panelFindB)))

	code, srv, stderr := runPanelAgent(t, a, b, runs)
	checkOneMember(t, code, srv, stderr, runs, "b_only", panelModelB, "panel: member a can't be used: ")
	if !strings.Contains(stderr, "no such file or directory") {
		t.Errorf("stderr lacks why a can't be read:\n%s", stderr)
	}
}

func TestRunAgentPanelNoJudge(t *testing.T) {
	runs := &panelRuns{t: t}
	a := writeMember(t, "a.json", memberCompleted(reviewSkill(panelFindA)))
	b := writeMember(t, "b.json", memberCompleted(reviewSkill(panelFindB)))

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
	a := writeMember(t, "a.json", result.ErrorResult(result.Meta{}, result.CodeInternal, "it broke"))
	other := memberCompleted(reviewSkill(panelFindB))
	other.SchemaVersion = 2
	b := writeMember(t, "b.json", other)

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
	a := writeMember(t, "a.json", memberCompleted(reviewSkill(panelFindA)))
	failed := reviewSkill()
	failed.Verdict = string(review.Failed)
	failed.Failures = []result.Failure{{Files: []string{"a.go"}, Message: "b broke"}}
	b := writeMember(t, "b.json", memberCompleted(failed))

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
	a := writeMember(t, "a.json", memberCompleted(other))
	b := writeMember(t, "b.json", memberCompleted(other))

	code, srv, stderr := runPanelAgent(t, a, b, runs)
	if code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, stderr)
	}
	r := srv.only(t)
	s, ok := skillNamed(r, "review")
	if !ok || s.Verdict != "failed" || s.Panel == nil || s.Panel.Mode != "none" {
		t.Errorf("skills = %+v", r.Skills)
	}
	if _, ok := skillNamed(r, "other"); ok {
		t.Error("a skill P did not load is in the result")
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
	} {
		if r, reason := readMember(c.path); r != nil || !strings.Contains(reason, c.want) {
			t.Errorf("%s: r %v, reason %q, want %q", c.path, r, reason, c.want)
		}
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
	got := memberSkills(&result.Result{Skills: []result.Skill{s, odd}})
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
}
