package review

import (
	"context"
	"encoding/json"
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
	pc := panelCall{model: c.Model, opts: o, contents: map[string]string{}}
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

func TestJudgeRejectedMessageNamesTheFile(t *testing.T) {
	if !strings.Contains(judgeRejectedMessage, rejectedFile+" is context") {
		t.Errorf("judgeRejectedMessage = %q", judgeRejectedMessage)
	}
}
