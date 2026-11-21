package review

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gopaytech/pergent/internal/config"
	"github.com/gopaytech/pergent/internal/finding"
	"github.com/gopaytech/pergent/internal/panel"
	"github.com/gopaytech/pergent/internal/runner"
	"github.com/gopaytech/pergent/internal/skill"
	"github.com/gopaytech/pergent/render"
	"github.com/gopaytech/pergent/rule"
)

func TestParseJudgeAnswer(t *testing.T) {
	text := "done\n```json\n" + `{"results":[{"id":"a1","real":false,"reason":"r","same_as":"b2"},{"id":2,"real":null,"same_as":null}]}` + "\n```\n"
	got, err := parseJudgeAnswer(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d answers: %v", len(got), got)
	}
	a1 := got["a1"]
	if a1.Real == nil || *a1.Real || a1.Reason != "r" || a1.SameAs != "b2" {
		t.Errorf("a1 = %+v", a1)
	}
	two, ok := got["2"]
	if !ok || two.Real != nil || two.Reason != "" || two.SameAs != "" {
		t.Errorf("2 = %+v, ok=%v", two, ok)
	}
}

func TestParseJudgeAnswerLastBlockWins(t *testing.T) {
	good := "```json\n" + `{"results":[{"id":"a1","real":true}]}` + "\n```\n"
	other := "```json\n" + `{"results":[{"id":"a1","real":false}]}` + "\n```\n"
	got, err := parseJudgeAnswer(good + "then\n" + other)
	if err != nil {
		t.Fatal(err)
	}
	if r := got["a1"].Real; r == nil || *r {
		t.Errorf("last block should win: %+v", got)
	}
	if _, err := parseJudgeAnswer(good + "then\n```json\n{bad\n```\n"); err == nil {
		t.Error("a bad last block must be an error, not the earlier block")
	}
}

func TestParseJudgeAnswerNoResults(t *testing.T) {
	if _, err := parseJudgeAnswer("```json\n{\"findings\":[]}\n```\n"); err == nil {
		t.Error("want error")
	}
	if _, err := parseJudgeAnswer("no block"); err == nil {
		t.Error("want error for no block")
	}
}

// panelCall is what the fake runner saw in one judge run.
type panelCall struct {
	model    string // provider/model of the run's config
	prompt   string // the agent prompt the config names
	opts     runner.Options
	contents map[string]string // attached files by base name
}

// panelFake answers each run by its model, and records the runs.
type panelFake struct {
	t       *testing.T
	mu      sync.Mutex
	calls   []panelCall
	respond func(c panelCall) runner.RunResult
}

func (f *panelFake) run(_ context.Context, o runner.Options) runner.RunResult {
	cfg, err := os.ReadFile(o.ConfigPath)
	if err != nil {
		f.t.Errorf("config: %v", err)
	}
	var c struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(cfg, &c)
	prompt, _ := os.ReadFile(filepath.Join(filepath.Dir(o.ConfigPath), config.PromptFileName))
	pc := panelCall{model: c.Model, prompt: string(prompt), opts: o, contents: map[string]string{}}
	for _, p := range o.Files {
		data, err := os.ReadFile(p)
		if err != nil {
			f.t.Errorf("attachment %s: %v", p, err)
			continue
		}
		pc.contents[filepath.Base(p)] = string(data)
	}
	f.mu.Lock()
	f.calls = append(f.calls, pc)
	f.mu.Unlock()
	return f.respond(pc)
}

func (f *panelFake) on(model string) []panelCall {
	var out []panelCall
	for _, c := range f.calls {
		if c.model == model {
			out = append(out, c)
		}
	}
	return out
}

const (
	modelA = "anthropic/model-a"
	modelB = "openai/model-b"
)

// judgeAnswer is a judge's answer with the given results.
func judgeAnswer(t *testing.T, results ...map[string]any) string {
	t.Helper()
	return verifyAnswer(t, results...)
}

// byModel answers a run on model A with a and a run on model B with b.
func byModel(a, b string) func(panelCall) runner.RunResult {
	return func(c panelCall) runner.RunResult {
		if c.model == modelA {
			return completed(a)
		}
		return completed(b)
	}
}

var (
	findA1 = finding.Finding{File: "a/x.go", Line: 1, Severity: finding.Blocking, Title: "Nil map write", Explanation: "e", Fingerprint: "fa1"}
	findA2 = finding.Finding{File: "a/x.go", Line: 3, Severity: finding.Suggestion, Title: "Rename this", Explanation: "e", Fingerprint: "fa2"}
	findB1 = finding.Finding{File: "a/x.go", Line: 2, Severity: finding.Blocking, Title: "The map is nil", Explanation: "e", Fingerprint: "fb1"}
)

func memberResult(fs ...finding.Finding) SkillResult {
	res := SkillResult{Skill: "s", Findings: fs, Batches: 1, BatchesFinished: 1, Reviewed: []string{"a/x.go"}}
	res.Verdict = verdict(res)
	return res
}

// panelRequest is a hardened panel request for skill "s" with a five-minute
// budget, so judge runs can start.
func panelRequest(t *testing.T, f *panelFake, a, b SkillResult) PanelRequest {
	t.Helper()
	req := newRequest(t, &fake{}, patch("a/x.go", "x"))
	req.Run = f.run
	req.Budget = 5 * time.Minute
	req.Sandbox = Sandbox{Hardened: true}
	return PanelRequest{
		Request: req,
		A:       Member{Role: panel.A, Model: config.Model{Provider: "anthropic", Model: "model-a"}, Key: "key-a", Skills: map[string]SkillResult{"s": a}},
		B:       Member{Role: panel.B, Model: config.Model{Provider: "openai", Model: "model-b"}, Key: "key-b", Skills: map[string]SkillResult{"s": b}},
	}
}

func runPanel(t *testing.T, req PanelRequest) SkillResult {
	t.Helper()
	rs := RunPanel(context.Background(), req)
	if len(rs) != 1 {
		t.Fatalf("got %d results, want 1", len(rs))
	}
	if rs[0].Panel == nil {
		t.Fatalf("no Panel: %+v", rs[0])
	}
	return rs[0]
}

// sentIDs reads the ids and fingerprints of an attached findings file.
func sentIDs(t *testing.T, content string) (ids []string, fingerprints []string) {
	t.Helper()
	var sent struct {
		Findings []struct {
			ID string `json:"id"`
			finding.Finding
		} `json:"findings"`
	}
	if err := json.Unmarshal([]byte(content), &sent); err != nil {
		t.Fatalf("findings file: %v\n%s", err, content)
	}
	for _, f := range sent.Findings {
		ids = append(ids, f.ID)
		if f.Fingerprint != "" {
			fingerprints = append(fingerprints, f.Fingerprint)
		}
	}
	return ids, fingerprints
}

func TestRunPanelBothJudges(t *testing.T) {
	f := &panelFake{t: t}
	f.respond = byModel(
		judgeAnswer(t, map[string]any{"id": "b1", "real": true, "reason": "yes", "same_as": "a1"}),
		judgeAnswer(t, map[string]any{"id": "a1", "real": true, "reason": "yes", "same_as": "b1"}, map[string]any{"id": "a2", "real": nil, "same_as": nil}),
	)
	res := runPanel(t, panelRequest(t, f, memberResult(findA1, findA2), memberResult(findB1)))

	if len(res.Findings) != 2 || len(res.Panel.Tags) != 2 {
		t.Fatalf("findings %+v, tags %+v", res.Findings, res.Panel.Tags)
	}
	if res.Findings[0].Title != findA1.Title || res.Findings[0].Severity != finding.Blocking || res.Panel.Tags[0].FoundBy != panel.Both {
		t.Errorf("first = %+v %+v", res.Findings[0], res.Panel.Tags[0])
	}
	if !slices.Equal(res.Panel.Tags[0].Also, []string{"fb1"}) {
		t.Errorf("Also = %v", res.Panel.Tags[0].Also)
	}
	if res.Findings[1].Title != findA2.Title || res.Findings[1].Severity != finding.Suggestion || res.Panel.Tags[1].FoundBy != panel.A {
		t.Errorf("second = %+v %+v", res.Findings[1], res.Panel.Tags[1])
	}
	if res.Verdict != HasBlockingIssues {
		t.Errorf("verdict = %s", res.Verdict)
	}
	if res.Panel.Mode != PanelBoth || res.Panel.Exact {
		t.Errorf("panel = %+v", res.Panel)
	}
	want := []JudgeRun{{Role: panel.B, Status: JudgeOK}, {Role: panel.A, Status: JudgeOK}}
	if !slices.EqualFunc(res.Panel.Judges, want, func(a, b JudgeRun) bool { return a.Role == b.Role && a.Status == b.Status && a.Cause == nil }) {
		t.Errorf("judges = %+v", res.Panel.Judges)
	}
	if len(f.calls) != 2 {
		t.Fatalf("%d runs, want 2", len(f.calls))
	}
	for model, key := range map[string]string{modelA: "key-a", modelB: "key-b"} {
		cs := f.on(model)
		if len(cs) != 1 {
			t.Fatalf("%d runs on %s", len(cs), model)
		}
		c := cs[0]
		if !slices.Equal(c.opts.Env, []string{"OPENCODE_API_KEY=" + key}) {
			t.Errorf("%s Env = %v", model, c.opts.Env)
		}
		if c.opts.Message != judgeMessage {
			t.Errorf("%s message = %q", model, c.opts.Message)
		}
		// A hardened judge run sees neither key in the environment it passes
		// on: its own is set in Env.
		for _, name := range []string{"OPENCODE_API_KEY", "PERGENT_PANEL_B_API_KEY"} {
			if !slices.Contains(c.opts.ExcludeEnv, name) {
				t.Errorf("%s ExcludeEnv = %v, want %s", model, c.opts.ExcludeEnv, name)
			}
		}
	}
	// B judges A's findings: theirs are A's, by id, with no fingerprint.
	c := f.on(modelB)[0]
	ids, fps := sentIDs(t, c.contents[theirFile])
	if !slices.Equal(ids, []string{"a1", "a2"}) || fps != nil {
		t.Errorf("B's %s: ids %v, fingerprints %v", theirFile, ids, fps)
	}
	if ids, _ := sentIDs(t, c.contents[ownFile]); !slices.Equal(ids, []string{"b1"}) {
		t.Errorf("B's %s: ids %v", ownFile, ids)
	}
	if ids, _ := sentIDs(t, f.on(modelA)[0].contents[theirFile]); !slices.Equal(ids, []string{"b1"}) {
		t.Errorf("A's %s: ids %v", theirFile, ids)
	}
	if c.opts.LogPrefix != "[s judge b]" {
		t.Errorf("log prefix = %q", c.opts.LogPrefix)
	}
}

func TestRunPanelDemotes(t *testing.T) {
	f := &panelFake{t: t}
	f.respond = byModel("unused", judgeAnswer(t, map[string]any{"id": "a1", "real": false, "reason": "guarded", "same_as": nil}))
	res := runPanel(t, panelRequest(t, f, memberResult(findA1), memberResult()))

	if len(res.Findings) != 1 || res.Findings[0].Severity != finding.Suggestion {
		t.Fatalf("findings = %+v", res.Findings)
	}
	if tag := res.Panel.Tags[0]; tag.DisagreedBy != panel.B || tag.Reason != "guarded" {
		t.Errorf("tag = %+v", tag)
	}
	if res.Verdict != NoBlockingIssues {
		t.Errorf("verdict = %s", res.Verdict)
	}
	// B has no findings: A has nothing to judge.
	if len(f.on(modelA)) != 0 {
		t.Errorf("a run started on model A")
	}
	if j := res.Panel.Judges; len(j) != 2 || j[0].Status != JudgeOK || j[1].Status != JudgeNotRun {
		t.Errorf("judges = %+v", j)
	}
}

func TestRunPanelJudgeFailsKeepsBlocking(t *testing.T) {
	quota := &runner.Cause{Kind: runner.CauseOpencodeError, ProviderClass: runner.ClassQuota, ProviderStatus: 429}
	other := finding.Finding{File: "a/x.go", Line: 9, Severity: finding.Suggestion, Title: "Unrelated", Explanation: "e"}
	f := &panelFake{t: t}
	f.respond = func(c panelCall) runner.RunResult {
		if c.model == modelB {
			return runner.RunResult{Status: runner.StatusFailed, Cause: quota}
		}
		return completed(judgeAnswer(t, map[string]any{"id": "b1", "real": nil, "same_as": nil}))
	}
	res := runPanel(t, panelRequest(t, f, memberResult(findA1), memberResult(other)))

	if len(res.Findings) != 2 || res.Findings[0].Title != findA1.Title || res.Findings[0].Severity != finding.Blocking {
		t.Fatalf("findings = %+v", res.Findings)
	}
	if res.Panel.Tags[0].UnjudgedBy != panel.B {
		t.Errorf("tag = %+v", res.Panel.Tags[0])
	}
	if res.Verdict != HasBlockingIssues {
		t.Errorf("verdict = %s", res.Verdict)
	}
	if j := res.Panel.Judges; len(j) != 2 || j[0].Role != panel.B || j[0].Status != JudgeFailed || j[0].Cause != quota || j[1].Status != JudgeOK {
		t.Errorf("judges = %+v", j)
	}
}

func TestRunPanelBadAnswerKeepsBlocking(t *testing.T) {
	f := &panelFake{t: t}
	f.respond = byModel("unused", "no json here")
	res := runPanel(t, panelRequest(t, f, memberResult(findA1), memberResult()))

	if len(res.Findings) != 1 || res.Findings[0].Severity != finding.Blocking || res.Verdict != HasBlockingIssues {
		t.Fatalf("findings %+v, verdict %s", res.Findings, res.Verdict)
	}
	if j := res.Panel.Judges[0]; j.Status != JudgeFailed || j.Cause != nil {
		t.Errorf("judge = %+v", j)
	}
}

func TestRunPanelLowBudgetSkipsKeepsBlocking(t *testing.T) {
	f := &panelFake{t: t}
	f.respond = byModel("unused", "unused")
	req := panelRequest(t, f, memberResult(findA1), memberResult(findB1))
	req.Budget = time.Minute
	res := runPanel(t, req)

	if len(f.calls) != 0 {
		t.Errorf("%d runs started", len(f.calls))
	}
	if j := res.Panel.Judges; len(j) != 2 || j[0].Status != JudgeSkipped || j[1].Status != JudgeSkipped {
		t.Errorf("judges = %+v", j)
	}
	if !res.Panel.Exact || res.Verdict != HasBlockingIssues || len(res.Findings) != 2 {
		t.Errorf("panel %+v, verdict %s, findings %+v", res.Panel, res.Verdict, res.Findings)
	}
}

func TestRunPanelBothJudgesFailIsExact(t *testing.T) {
	same := finding.Finding{File: findA1.File, Line: findA1.Line, Severity: finding.Blocking, Title: findA1.Title, Explanation: "other words", Fingerprint: "fb1"}
	f := &panelFake{t: t}
	f.respond = func(panelCall) runner.RunResult { return runner.RunResult{Status: runner.StatusFailed} }
	res := runPanel(t, panelRequest(t, f, memberResult(findA1), memberResult(same, findB1)))

	if !res.Panel.Exact {
		t.Error("not Exact")
	}
	if got := titles(res.Findings); !slices.Equal(got, []string{findA1.Title, findB1.Title}) {
		t.Fatalf("titles = %v", got)
	}
	for _, f := range res.Findings {
		if f.Severity != finding.Blocking {
			t.Errorf("%s is %s", f.Title, f.Severity)
		}
	}
	if res.Panel.Tags[0].FoundBy != panel.Both || res.Verdict != HasBlockingIssues {
		t.Errorf("tag %+v, verdict %s", res.Panel.Tags[0], res.Verdict)
	}
	if j := res.Panel.Judges; len(j) != 2 || j[0].Status != JudgeFailed || j[1].Status != JudgeFailed {
		t.Errorf("judges = %+v", j)
	}
}

func TestRunPanelConfidenceIsLower(t *testing.T) {
	cases := []struct {
		a, b       int
		want       int
		wantReason string
	}{
		{4, 2, 2, "rb"},
		{2, 4, 2, "ra"},
		{3, 3, 3, "ra"},
		{4, 0, 0, ""},
		{0, 4, 0, ""},
	}
	for _, tc := range cases {
		f := &panelFake{t: t}
		f.respond = byModel("unused", "unused")
		a, b := memberResult(), memberResult()
		a.Confidence, a.ConfidenceReason = tc.a, "ra"
		b.Confidence, b.ConfidenceReason = tc.b, "rb"
		if tc.a == 0 {
			a.ConfidenceReason = ""
		}
		if tc.b == 0 {
			b.ConfidenceReason = ""
		}
		res := runPanel(t, panelRequest(t, f, a, b))
		if res.Confidence != tc.want || res.ConfidenceReason != tc.wantReason {
			t.Errorf("A %d, B %d: got %d %q, want %d %q", tc.a, tc.b, res.Confidence, res.ConfidenceReason, tc.want, tc.wantReason)
		}
	}
}

func TestRunPanelNothingToJudge(t *testing.T) {
	f := &panelFake{t: t}
	f.respond = byModel("unused", "unused")
	res := runPanel(t, panelRequest(t, f, memberResult(), memberResult(findA2)))

	if len(f.calls) != 0 {
		t.Errorf("%d runs started", len(f.calls))
	}
	if j := res.Panel.Judges; len(j) != 2 || j[0].Status != JudgeNotRun || j[1].Status != JudgeNotRun {
		t.Errorf("judges = %+v", j)
	}
	if len(res.Findings) != 1 || res.Findings[0].Severity != finding.Suggestion || res.Panel.Tags[0].FoundBy != panel.B {
		t.Errorf("findings %+v, tags %+v", res.Findings, res.Panel.Tags)
	}
	if res.Panel.Exact || res.Verdict != NoBlockingIssues {
		t.Errorf("panel %+v, verdict %s", res.Panel, res.Verdict)
	}
}

func TestRunPanelUsageByRole(t *testing.T) {
	f := &panelFake{t: t}
	ok := judgeAnswer(t)
	f.respond = func(c panelCall) runner.RunResult {
		r := completed(ok)
		r.Usage = runner.Usage{Input: 20}
		if c.model == modelA {
			r.Usage = runner.Usage{Input: 10}
		}
		return r
	}
	res := runPanel(t, panelRequest(t, f, memberResult(findA1, findA2), memberResult(findB1)))

	if got := res.Panel.UsageByRole; got[panel.A].Input != 10 || got[panel.B].Input != 20 {
		t.Errorf("UsageByRole = %+v", got)
	}
	if res.Usage.Input != 30 {
		t.Errorf("Usage = %+v", res.Usage)
	}
	if len(res.Runs) != 2 || res.Runs[0].Label != "judge a" || res.Runs[0].Role != panel.A || res.Runs[1].Role != panel.B {
		t.Errorf("runs = %+v", res.Runs)
	}
}

func TestRunPanelRejectedAttached(t *testing.T) {
	f := &panelFake{t: t}
	f.respond = byModel(judgeAnswer(t), judgeAnswer(t))
	req := panelRequest(t, f, memberResult(findA1, findA2), memberResult(findB1))
	req.Dismissals = map[string][]finding.Rejected{"s": rejectedBoth}
	runPanel(t, req)

	if len(f.calls) != 2 {
		t.Fatalf("%d runs, want 2", len(f.calls))
	}
	for _, c := range f.calls {
		data, ok := c.contents[rejectedFile]
		if !ok {
			t.Errorf("%s: no %s in %v", c.model, rejectedFile, c.opts.Files)
			continue
		}
		var got struct{ Rejected []finding.Rejected }
		if err := json.Unmarshal([]byte(data), &got); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got.Rejected, rejectedBoth[:1]) {
			t.Errorf("%s: rejected = %+v, want the blocking one only", c.model, got.Rejected)
		}
		if c.opts.Message != judgeMessage+" "+judgeRejectedMessage {
			t.Errorf("%s: message = %q", c.model, c.opts.Message)
		}
	}
}

// TestRunPanelDismissedFindingLinksNothing: B reports b1 again, which people
// dismissed as blocking, and a judge links A's own blocking a1 to it. The
// link is dropped, so the dismissal of b1 cannot hide a1.
func TestRunPanelDismissedFindingLinksNothing(t *testing.T) {
	dismissedB := finding.Finding{File: "a/x.go", Line: 7, Severity: finding.Blocking, Title: "Old dismissed bug", Explanation: "e"}
	dismissedB.Fingerprint = finding.Fingerprint("s", dismissedB.File, dismissedB.Title)
	for name, answers := range map[string][2]string{
		// B's judge links a1 to its own b1.
		"of a": {judgeAnswer(t, map[string]any{"id": "b1", "real": true, "reason": "yes", "same_as": nil}),
			judgeAnswer(t, map[string]any{"id": "a1", "real": true, "reason": "yes", "same_as": "b1"})},
		// A's judge links b1 to its own a1.
		"of b": {judgeAnswer(t, map[string]any{"id": "b1", "real": true, "reason": "yes", "same_as": "a1"}),
			judgeAnswer(t, map[string]any{"id": "a1", "real": true, "reason": "yes", "same_as": nil})},
	} {
		t.Run(name, func(t *testing.T) {
			f := &panelFake{t: t}
			f.respond = byModel(answers[0], answers[1])
			req := panelRequest(t, f, memberResult(findA1), memberResult(dismissedB))
			req.Dismissals = map[string][]finding.Rejected{"s": {{File: dismissedB.File, Title: dismissedB.Title, Severity: string(finding.Blocking), Reason: "fine"}}}
			res := runPanel(t, req)

			s := render.Skill{Name: "s", Verdict: render.VerdictHasBlockingIssues}
			for i, f := range res.Findings {
				tag := res.Panel.Tags[i]
				s.Findings = append(s.Findings, render.Finding{Fingerprint: f.Fingerprint, Title: f.Title, Severity: render.Severity(f.Severity),
					Panel: &render.FindingPanel{FoundBy: tag.FoundBy, Also: tag.Also}})
			}
			out := render.ApplyDismissals([]render.Skill{s}, []render.Dismissal{{Fingerprint: dismissedB.Fingerprint, Severity: "blocking"}})[0]
			if len(out.Findings) != 1 || out.Findings[0].Title != findA1.Title || out.Findings[0].Severity != render.Blocking {
				t.Errorf("kept = %+v, want a1 blocking", out.Findings)
			}
			if len(out.Dismissed) != 1 || out.Dismissed[0].Finding.Title != dismissedB.Title {
				t.Errorf("dismissed = %+v, want b1 alone", out.Dismissed)
			}
			if out.Verdict != render.VerdictHasBlockingIssues {
				t.Errorf("verdict = %s", out.Verdict)
			}
		})
	}
}

func TestJudgeRejectedMessageNamesTheFile(t *testing.T) {
	if !strings.Contains(judgeRejectedMessage, rejectedFile+" is context") {
		t.Errorf("judgeRejectedMessage = %q", judgeRejectedMessage)
	}
}

// checkOneMemberVerify checks that a panel with one usable member, m, ran one
// verification on m's model with m's key, and dropped the finding it judged
// not real.
func checkOneMemberVerify(t *testing.T, f *panelFake, res SkillResult, m Member, model, mode string) {
	t.Helper()
	if len(f.calls) != 1 {
		t.Fatalf("%d runs, want 1", len(f.calls))
	}
	c := f.calls[0]
	if c.model != model {
		t.Errorf("run on %s, want %s", c.model, model)
	}
	if !slices.Equal(c.opts.Env, []string{"OPENCODE_API_KEY=" + m.Key}) {
		t.Errorf("Env = %v", c.opts.Env)
	}
	if !strings.HasPrefix(c.opts.Message, verifyMessage) || !strings.HasPrefix(c.prompt, verifyPrompt) {
		t.Errorf("not a verification run: message %q", c.opts.Message)
	}
	if _, ok := c.contents["blocking-findings.json"]; !ok {
		t.Errorf("no blocking-findings.json in %v", c.opts.Files)
	}
	if len(res.Findings) != 0 || len(res.Panel.Tags) != 0 {
		t.Errorf("findings %+v, tags %+v", res.Findings, res.Panel.Tags)
	}
	want := "Verification dropped 1 of 1 blocking finding: " + findA1.Title + " (guarded)"
	if !slices.Contains(res.Notes, want) {
		t.Errorf("notes = %q, want %q", res.Notes, want)
	}
	if res.Panel.Mode != mode || res.Panel.Exact || res.Panel.Judges != nil {
		t.Errorf("panel = %+v", res.Panel)
	}
	if res.Verdict != NoBlockingIssues {
		t.Errorf("verdict = %s", res.Verdict)
	}
	if u := res.Panel.UsageByRole[m.Role]; u.Input != 7 || res.Usage.Input != 7 || len(res.Panel.UsageByRole) != 1 {
		t.Errorf("usage %+v, by role %+v", res.Usage, res.Panel.UsageByRole)
	}
	if len(res.Runs) != 1 || res.Runs[0].Label != "verify" || res.Runs[0].Role != m.Role {
		t.Errorf("runs = %+v", res.Runs)
	}
}

func dropFirst(t *testing.T) func(panelCall) runner.RunResult {
	return func(panelCall) runner.RunResult {
		r := completed(verifyAnswer(t, map[string]any{"id": "1", "real": false, "reason": "guarded"}))
		r.Usage = runner.Usage{Input: 7}
		return r
	}
}

func failedResult() SkillResult {
	return SkillResult{Skill: "s", Verdict: Failed, Failures: []BatchFailure{{Message: "b broke"}}}
}

func TestRunPanelAOnlyVerifiesWithA(t *testing.T) {
	f := &panelFake{t: t, respond: dropFirst(t)}
	req := panelRequest(t, f, memberResult(findA1), failedResult())
	res := runPanel(t, req)
	checkOneMemberVerify(t, f, res, req.A, modelA, PanelAOnly)
}

func TestRunPanelBOnlyVerifiesWithB(t *testing.T) {
	f := &panelFake{t: t, respond: dropFirst(t)}
	req := panelRequest(t, f, failedResult(), memberResult(findA1))
	res := runPanel(t, req)
	checkOneMemberVerify(t, f, res, req.B, modelB, PanelBOnly)
}

func TestRunPanelOneMemberTagsWhatIsKept(t *testing.T) {
	f := &panelFake{t: t, respond: byModel(verifyAnswer(t), "unused")}
	req := panelRequest(t, f, memberResult(findA1, findA2), failedResult())
	res := runPanel(t, req)
	if len(res.Findings) != 2 || len(res.Panel.Tags) != 2 {
		t.Fatalf("findings %+v, tags %+v", res.Findings, res.Panel.Tags)
	}
	for _, tag := range res.Panel.Tags {
		if tag.FoundBy != panel.A {
			t.Errorf("tag = %+v", tag)
		}
	}
	if res.Verdict != HasBlockingIssues {
		t.Errorf("verdict = %s", res.Verdict)
	}
}

func TestRunPanelNoneFails(t *testing.T) {
	f := &panelFake{t: t, respond: byModel("unused", "unused")}
	a := failedResult()
	a.Failures = []BatchFailure{{Message: "a broke"}}
	res := runPanel(t, panelRequest(t, f, a, failedResult()))
	if len(f.calls) != 0 {
		t.Errorf("%d runs started", len(f.calls))
	}
	if res.Verdict != Failed || len(res.Failures) != 1 || res.Failures[0].Message != "a broke" {
		t.Errorf("verdict %s, failures %+v", res.Verdict, res.Failures)
	}
	if res.Panel.Mode != PanelNone {
		t.Errorf("panel = %+v", res.Panel)
	}
}

func TestRunPanelNoneFailsWithBsFailures(t *testing.T) {
	f := &panelFake{t: t, respond: byModel("unused", "unused")}
	req := panelRequest(t, f, SkillResult{}, failedResult())
	delete(req.A.Skills, "s")
	res := runPanel(t, req)
	if res.Verdict != Failed || len(res.Failures) != 1 || res.Failures[0].Message != "b broke" || res.Panel.Mode != PanelNone {
		t.Errorf("verdict %s, failures %+v, panel %+v", res.Verdict, res.Failures, res.Panel)
	}
}

func TestRunPanelMissingMemberIsAOnly(t *testing.T) {
	f := &panelFake{t: t, respond: byModel(verifyAnswer(t), "unused")}
	req := panelRequest(t, f, memberResult(findA1), SkillResult{})
	req.Skills = append(req.Skills, skill.Skill{Name: "t", Body: "Review it."})
	t2 := memberResult(findA2)
	t2.Skill = "t"
	req.A.Skills["t"] = t2
	req.B.Skills = nil
	rs := RunPanel(context.Background(), req)
	if len(rs) != 2 {
		t.Fatalf("%d results", len(rs))
	}
	for _, res := range rs {
		if res.Panel == nil || res.Panel.Mode != PanelAOnly {
			t.Errorf("%s: panel = %+v", res.Skill, res.Panel)
		}
	}
	if len(f.on(modelB)) != 0 {
		t.Error("a run started on model B")
	}
}

func TestRunPanelNoJudgeRunsNothing(t *testing.T) {
	f := &panelFake{t: t, respond: byModel("unused", "unused")}
	req := panelRequest(t, f, memberResult(findA1), memberResult(findB1))
	req.NoJudge = true
	res := runPanel(t, req)
	if len(f.calls) != 0 {
		t.Errorf("%d runs started", len(f.calls))
	}
	if !res.Panel.Exact || res.Panel.Mode != PanelBoth {
		t.Errorf("panel = %+v", res.Panel)
	}
	if j := res.Panel.Judges; len(j) != 2 || j[0].Status != JudgeSkipped || j[1].Status != JudgeSkipped {
		t.Errorf("judges = %+v", j)
	}
	if res.Verdict != HasBlockingIssues || len(res.Findings) != 2 {
		t.Errorf("verdict %s, findings %+v", res.Verdict, res.Findings)
	}
}

func TestRunPanelNoJudgeOneMemberRunsNothing(t *testing.T) {
	f := &panelFake{t: t, respond: byModel("unused", "unused")}
	req := panelRequest(t, f, memberResult(findA1), failedResult())
	req.NoJudge = true
	res := runPanel(t, req)
	if len(f.calls) != 0 {
		t.Errorf("%d runs started", len(f.calls))
	}
	if !res.Panel.Exact || res.Panel.Mode != PanelAOnly {
		t.Errorf("panel = %+v", res.Panel)
	}
	for _, n := range res.Notes {
		if strings.HasPrefix(n, "Verification") {
			t.Errorf("note %q", n)
		}
	}
	if res.Verdict != HasBlockingIssues || len(res.Findings) != 1 || res.Panel.Tags[0].FoundBy != panel.A {
		t.Errorf("verdict %s, findings %+v, tags %+v", res.Verdict, res.Findings, res.Panel.Tags)
	}
}

// impactResult is a member's impact check with the given findings.
func impactResult(fs ...finding.Finding) SkillResult {
	res := memberResult(fs...)
	res.Skill = ImpactSkill
	res.Verdict = verdict(res)
	return res
}

func TestRunPanelImpactIsExactMerged(t *testing.T) {
	same := findA1
	same.Explanation, same.Fingerprint = "other words", "fb1"
	f := &panelFake{t: t, respond: byModel("unused", "unused")}
	req := panelRequest(t, f, memberResult(), memberResult())
	req.A.Skills[ImpactSkill] = impactResult(findA1, findA2)
	req.B.Skills[ImpactSkill] = impactResult(same, findB1)
	rs := RunPanel(context.Background(), req)
	if len(rs) != 2 || rs[0].Skill != "s" || rs[1].Skill != ImpactSkill {
		t.Fatalf("results = %+v", rs)
	}
	res := rs[1]
	if len(f.calls) != 0 {
		t.Errorf("%d runs started", len(f.calls))
	}
	if res.Panel == nil || res.Panel.Mode != PanelBoth || res.Panel.Exact || res.Panel.Judges != nil {
		t.Fatalf("panel = %+v", res.Panel)
	}
	if got := titles(res.Findings); !slices.Equal(got, []string{findA1.Title, findA2.Title, findB1.Title}) {
		t.Fatalf("titles = %v", got)
	}
	if tags := res.Panel.Tags; len(tags) != 3 || tags[0].FoundBy != panel.Both || tags[1].FoundBy != panel.A || tags[2].FoundBy != panel.B {
		t.Errorf("tags = %+v", tags)
	}
	if res.Verdict != HasBlockingIssues || res.Findings[0].Severity != finding.Blocking {
		t.Errorf("verdict %s, findings %+v", res.Verdict, res.Findings)
	}
}

func TestRunPanelImpactOneMemberIsNotVerified(t *testing.T) {
	f := &panelFake{t: t, respond: byModel("unused", "unused")}
	req := panelRequest(t, f, memberResult(), memberResult())
	req.B.Skills[ImpactSkill] = impactResult(findB1)
	fail := failedResult()
	fail.Skill = ImpactSkill
	req.A.Skills[ImpactSkill] = fail
	rs := RunPanel(context.Background(), req)
	if len(rs) != 2 {
		t.Fatalf("results = %+v", rs)
	}
	res := rs[1]
	if len(f.calls) != 0 {
		t.Errorf("%d runs started", len(f.calls))
	}
	if res.Skill != ImpactSkill || res.Panel == nil || res.Panel.Mode != PanelBOnly || res.Panel.Exact {
		t.Fatalf("result %+v", res)
	}
	if res.Verdict != HasBlockingIssues || len(res.Panel.Tags) != 1 || res.Panel.Tags[0].FoundBy != panel.B {
		t.Errorf("verdict %s, tags %+v", res.Verdict, res.Panel.Tags)
	}
}

func TestRunPanelImpactBothFailed(t *testing.T) {
	f := &panelFake{t: t, respond: byModel("unused", "unused")}
	req := panelRequest(t, f, memberResult(), memberResult())
	a, b := failedResult(), failedResult()
	a.Skill, b.Skill = ImpactSkill, ImpactSkill
	a.Failures = []BatchFailure{{Message: "a broke"}}
	req.A.Skills[ImpactSkill], req.B.Skills[ImpactSkill] = a, b
	rs := RunPanel(context.Background(), req)
	if len(rs) != 2 {
		t.Fatalf("results = %+v", rs)
	}
	res := rs[1]
	if res.Skill != ImpactSkill || res.Verdict != Failed || len(res.Failures) != 1 || res.Failures[0].Message != "a broke" || res.Panel.Mode != PanelNone {
		t.Errorf("result %+v", res)
	}
}

// A loaded skill named impact is a normal panel skill: it is judged, and no
// second impact result is added.
func TestRunPanelLoadedImpactSkillIsJudged(t *testing.T) {
	f := &panelFake{t: t, respond: byModel(judgeAnswer(t), judgeAnswer(t))}
	req := panelRequest(t, f, memberResult(), memberResult())
	req.Skills = []skill.Skill{{Name: ImpactSkill, Body: "Review it."}}
	req.A.Skills = map[string]SkillResult{ImpactSkill: impactResult(findA1)}
	req.B.Skills = map[string]SkillResult{ImpactSkill: impactResult(findB1)}
	res := runPanel(t, req)
	if len(f.calls) != 2 || len(res.Panel.Judges) != 2 {
		t.Errorf("%d runs, panel %+v", len(f.calls), res.Panel)
	}
}

// A loaded skill neither member reviewed fails: it never drops out of the
// merge, which would read as a pass.
func TestRunPanelSkillInNeitherFails(t *testing.T) {
	f := &panelFake{t: t, respond: byModel(judgeAnswer(t), judgeAnswer(t))}
	req := panelRequest(t, f, memberResult(), memberResult())
	req.Skills = append(req.Skills, skill.Skill{Name: "t", Body: "Review it."})
	rs := RunPanel(context.Background(), req)
	if len(rs) != 2 || rs[0].Skill != "s" || rs[1].Skill != "t" {
		t.Fatalf("results = %+v", rs)
	}
	res := rs[1]
	if res.Verdict != Failed || res.Panel == nil || res.Panel.Mode != PanelNone {
		t.Errorf("result %+v", res)
	}
	if len(res.Failures) != 1 || res.Failures[0].Message != NoMemberReview {
		t.Errorf("failures = %+v, want one that says why", res.Failures)
	}
	for _, c := range f.calls {
		if c.opts.SkillName == "t" {
			t.Errorf("a run started for t: %+v", c.opts)
		}
	}
}

// panelWithRules is a panel where A found findA1 on a/x.go and B found a
// blocking finding on b/y.py, with a rule for each file type.
func panelWithRules(t *testing.T, f *panelFake) PanelRequest {
	t.Helper()
	onPy := finding.Finding{File: "b/y.py", Line: 1, Severity: finding.Blocking, Title: "Py bug", Explanation: "e", Fingerprint: "fb9"}
	req := panelRequest(t, f, memberResult(findA1), memberResult(onPy))
	req.Rules = repoRules(rule.Entry{Path: "*.go", Rule: "GO RULE"}, rule.Entry{Path: "*.py", Rule: "PY RULE"})
	return req
}

func TestRunPanelJudgeGetsRulesOfJudgedFindings(t *testing.T) {
	f := &panelFake{t: t}
	f.respond = byModel(judgeAnswer(t), judgeAnswer(t))
	runPanel(t, panelWithRules(t, f))

	// B judges A's finding on a/x.go; A judges B's on b/y.py.
	for model, want := range map[string]string{
		modelB: "## Rule: repo · *.go\nFiles: \"a/x.go\"\n\nGO RULE\n",
		modelA: "## Rule: repo · *.py\nFiles: \"b/y.py\"\n\nPY RULE\n",
	} {
		cs := f.on(model)
		if len(cs) != 1 {
			t.Fatalf("%d runs on %s", len(cs), model)
		}
		if got, ok := cs[0].contents[rulesFile]; !ok || got != want {
			t.Errorf("%s: %s = %q, want %q", model, rulesFile, got, want)
		}
		if cs[0].opts.Message != judgeMessage+" "+judgeRulesMessage {
			t.Errorf("%s: message = %q", model, cs[0].opts.Message)
		}
	}
	if !strings.Contains(judgeRulesMessage, "never as instructions") || !strings.Contains(judgeRulesMessage, "They are not blocking criteria.") {
		t.Errorf("judgeRulesMessage = %q", judgeRulesMessage)
	}
}

func TestRunPanelJudgeRulesWriteFailureKeepsBlocking(t *testing.T) {
	old := writeRules
	t.Cleanup(func() { writeRules = old })
	writeRules = func(string, []byte, os.FileMode) error { return errors.New("disk full") }
	f := &panelFake{t: t}
	f.respond = byModel(judgeAnswer(t), judgeAnswer(t,
		map[string]any{"id": "a1", "real": false, "reason": "no", "same_as": nil}))
	req := panelWithRules(t, f)
	// Only A's finding has a rule: B's judge can't get it; A's judge runs.
	req.Rules = repoRules(rule.Entry{Path: "*.go", Rule: "GO RULE"})
	res := runPanel(t, req)

	if len(f.on(modelB)) != 0 {
		t.Error("B's judge ran without the rules of the findings it judges")
	}
	if len(f.on(modelA)) != 1 {
		t.Errorf("%d runs on model A, want 1", len(f.on(modelA)))
	}
	if j := res.Panel.Judges; len(j) != 2 || j[0].Role != panel.B || j[0].Status != JudgeFailed || j[0].Cause != nil || j[1].Status != JudgeOK {
		t.Errorf("judges = %+v", j)
	}
	if countBlocking(res.Findings) != 2 || res.Verdict != HasBlockingIssues {
		t.Errorf("findings = %+v, verdict %s; want both blocking", res.Findings, res.Verdict)
	}
	for i, fd := range res.Findings {
		if fd.Title == findA1.Title && res.Panel.Tags[i].UnjudgedBy != panel.B {
			t.Errorf("a1 tag = %+v, want unjudged by B", res.Panel.Tags[i])
		}
	}
}
