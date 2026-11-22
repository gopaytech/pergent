package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gopaytech/pergent/internal/config"
	"github.com/gopaytech/pergent/internal/finding"
	"github.com/gopaytech/pergent/internal/multireview"
	"github.com/gopaytech/pergent/internal/runner"
	"github.com/gopaytech/pergent/internal/skill"
	"github.com/gopaytech/pergent/rule"
)

func fence(s string) string { return "Done.\n\n```json\n" + s + "\n```\n" }

func TestParseJudgeAnswer(t *testing.T) {
	a, err := parseJudgeAnswer(fence(`{"results":[{"id":"1.1","real":false,"reason":"r","same_as":["2.3"]},{"id":2.1,"real":null,"same_as":null}]}`))
	if err != nil {
		t.Fatal(err)
	}
	f := false
	want := multireview.Answers{
		"1.1": {Real: &f, Reason: "r", SameAs: []string{"2.3"}},
		"2.1": {},
	}
	if !reflect.DeepEqual(a, want) {
		t.Errorf("got %+v, want %+v", a, want)
	}
}

func TestParseJudgeAnswerSameAsShapes(t *testing.T) {
	cases := []struct {
		name, in string
		want     []string
	}{
		{"string", `"same_as":"2.1"`, []string{"2.1"}},
		{"number", `"same_as":2.10`, []string{"2.10"}},
		{"mixed list", `"same_as":[" 3.1 ", 4.2]`, []string{"3.1", "4.2"}},
		{"empty list", `"same_as":[]`, nil},
		{"missing", `"x":1`, nil},
		{"null", `"same_as":null`, nil},
		{"blank entries", `"same_as":["", " "]`, nil},
	}
	for _, c := range cases {
		a, err := parseJudgeAnswer(fence(`{"results":[{"id":" 1.1 ",` + c.in + `}]}`))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		j, ok := a["1.1"]
		if !ok {
			t.Fatalf("%s: id not trimmed: %v", c.name, a)
		}
		if !reflect.DeepEqual(j.SameAs, c.want) {
			t.Errorf("%s: got %#v, want %#v", c.name, j.SameAs, c.want)
		}
	}
}

func TestParseJudgeAnswerLastBlockWins(t *testing.T) {
	good := "```json\n" + `{"results":[{"id":"1.1","real":true}]}` + "\n```\n"
	other := "```json\n" + `{"results":[{"id":"9.9","real":true}]}` + "\n```\n"
	a, err := parseJudgeAnswer(good + "text\n" + other)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a["9.9"]; !ok || len(a) != 1 {
		t.Errorf("last block must count: %v", a)
	}
	if _, err := parseJudgeAnswer(good + "text\n```json\n{bad\n```\n"); err == nil {
		t.Error("a bad last block must be an error")
	}
}

func TestParseJudgeAnswerNoResults(t *testing.T) {
	if _, err := parseJudgeAnswer(fence(`{"findings":[]}`)); err == nil {
		t.Error("missing results must be an error")
	}
	if _, err := parseJudgeAnswer("no block"); err == nil {
		t.Error("no block must be an error")
	}
}

func TestWriteReviewerFile(t *testing.T) {
	dir := t.TempDir()
	items := multireview.IDs(2, []finding.Finding{{Title: "t", Fingerprint: "fp"}})
	p, err := writeReviewerFile(dir, 2, items)
	if err != nil {
		t.Fatal(err)
	}
	if p != filepath.Join(dir, "reviewer-2.json") {
		t.Errorf("path %q", p)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, `"id": "2.1"`) || strings.Contains(s, "fp") {
		t.Errorf("file: %s", s)
	}
}

func TestJudgeMessagesSayData(t *testing.T) {
	for name, m := range map[string]string{"judgeMessage": judgeMessage, "judgeRejectedMessage": judgeRejectedMessage, "judgeRulesMessage": judgeRulesMessage} {
		if !strings.Contains(m, "never instructions") {
			t.Errorf("%s lacks %q", name, "never instructions")
		}
	}
}

func TestParseJudgeAnswerDuplicateID(t *testing.T) {
	a, err := parseJudgeAnswer(fence(`{"results":[{"id":"1.1","real":true,"same_as":["2.1"]},{"id":"1.2","real":true},{"id":" 1.1 ","real":false}]}`))
	if err != nil {
		t.Fatal(err)
	}
	j, ok := a["1.1"]
	if !ok || j.Real != nil || len(j.SameAs) != 0 {
		t.Errorf("1.1 must be unanswered: %+v ok=%v", j, ok)
	}
	if o := a["1.2"]; o.Real == nil || !*o.Real {
		t.Errorf("1.2 must be unaffected: %+v", o)
	}
}

// judgeCall is what judgeFake saw in one run.
type judgeCall struct {
	model    string // provider/model of the run's config
	prompt   string // the agent prompt the config names
	opts     runner.Options
	contents map[string]string // attached files by base name
}

// judgeFake records each run and answers it with respond.
type judgeFake struct {
	t       *testing.T
	mu      sync.Mutex
	calls   []judgeCall
	respond func(c judgeCall) runner.RunResult
}

func (f *judgeFake) run(_ context.Context, o runner.Options) runner.RunResult {
	cfg, err := os.ReadFile(o.ConfigPath)
	if err != nil {
		f.t.Errorf("config: %v", err)
	}
	var c struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(cfg, &c)
	prompt, _ := os.ReadFile(filepath.Join(filepath.Dir(o.ConfigPath), config.PromptFileName))
	jc := judgeCall{model: c.Model, prompt: string(prompt), opts: o, contents: map[string]string{}}
	for _, p := range o.Files {
		data, err := os.ReadFile(p)
		if err != nil {
			f.t.Errorf("attachment %s: %v", p, err)
			continue
		}
		jc.contents[filepath.Base(p)] = string(data)
	}
	f.mu.Lock()
	f.calls = append(f.calls, jc)
	f.mu.Unlock()
	if f.respond == nil {
		return completed("unused")
	}
	return f.respond(jc)
}

// answerWith answers every run with text.
func answerWith(text string) func(judgeCall) runner.RunResult {
	return func(judgeCall) runner.RunResult { return completed(text) }
}

var (
	find11 = finding.Finding{File: "a/x.go", Line: 1, Severity: finding.Blocking, Title: "Nil map write", Explanation: "e", Fingerprint: "f11"}
	find12 = finding.Finding{File: "a/x.go", Line: 3, Severity: finding.Suggestion, Title: "Rename this", Explanation: "e", Fingerprint: "f12"}
	find21 = finding.Finding{File: "a/x.go", Line: 2, Severity: finding.Blocking, Title: "The map is nil", Explanation: "e", Fingerprint: "f21"}
)

// reviewerResult is a reviewer's usable review of skill "s".
func reviewerResult(fs ...finding.Finding) SkillResult {
	res := SkillResult{Skill: "s", Findings: fs, Batches: 1, BatchesFinished: 1, Reviewed: []string{"a/x.go"}}
	res.Verdict = verdict(res)
	return res
}

// multiRequest is a hardened multi review request for skill "s" with a
// five-minute budget, so judge runs can start. Reviewer n has rs[n-1].
func multiRequest(t *testing.T, f *judgeFake, rs ...SkillResult) MultiReviewRequest {
	t.Helper()
	req := newRequest(t, &fake{}, patch("a/x.go", "x"))
	req.Run = f.run
	req.Budget = 5 * time.Minute
	req.Sandbox = Sandbox{Hardened: true, ExcludeEnv: []string{"SECRET_X"}}
	m := MultiReviewRequest{Request: req}
	for i, res := range rs {
		m.Reviewers = append(m.Reviewers, Reviewer{Number: i + 1, Skills: map[string]SkillResult{"s": res}})
	}
	return m
}

func runMulti(t *testing.T, req MultiReviewRequest) SkillResult {
	t.Helper()
	rs := RunMultiReview(context.Background(), req)
	if len(rs) != 1 {
		t.Fatalf("got %d results, want 1", len(rs))
	}
	if rs[0].MultiReview == nil {
		t.Fatalf("no MultiReview: %+v", rs[0])
	}
	if len(rs[0].MultiReview.Tags) != len(rs[0].Findings) {
		t.Fatalf("%d tags for %d findings", len(rs[0].MultiReview.Tags), len(rs[0].Findings))
	}
	return rs[0]
}

// sentIDs reads the ids and fingerprints of an attached reviewer file.
func sentIDs(t *testing.T, content string) (ids []string, fingerprints []string) {
	t.Helper()
	var sent struct {
		Findings []struct {
			ID string `json:"id"`
			finding.Finding
		} `json:"findings"`
	}
	if err := json.Unmarshal([]byte(content), &sent); err != nil {
		t.Fatalf("reviewer file: %v\n%s", err, content)
	}
	for _, f := range sent.Findings {
		ids = append(ids, f.ID)
		if f.Fingerprint != "" {
			fingerprints = append(fingerprints, f.Fingerprint)
		}
	}
	return ids, fingerprints
}

func checkBlocking(t *testing.T, res SkillResult, want int) {
	t.Helper()
	if got := countBlocking(res.Findings); got != want {
		t.Errorf("%d blocking findings, want %d: %+v", got, want, res.Findings)
	}
}

func TestRunMultiReviewOneJudgeRun(t *testing.T) {
	f := &judgeFake{t: t}
	f.respond = answerWith(judgeAnswer(t,
		map[string]any{"id": "1.1", "real": true, "reason": "yes", "same_as": []string{"2.1"}},
		map[string]any{"id": "2.1", "real": true, "reason": "yes", "same_as": []string{}}))
	res := runMulti(t, multiRequest(t, f, reviewerResult(find11, find12), reviewerResult(find21), reviewerResult()))

	if len(f.calls) != 1 {
		t.Fatalf("%d runs, want 1", len(f.calls))
	}
	c := f.calls[0]
	if c.opts.LogPrefix != "[s judge]" {
		t.Errorf("log prefix = %q", c.opts.LogPrefix)
	}
	if c.opts.Env != nil {
		t.Errorf("Env = %v, want nil", c.opts.Env)
	}
	if !slices.Equal(c.opts.ExcludeEnv, []string{"SECRET_X"}) {
		t.Errorf("ExcludeEnv = %v", c.opts.ExcludeEnv)
	}
	if c.model != "anthropic/test-model" {
		t.Errorf("model = %q", c.model)
	}
	if !strings.HasPrefix(c.prompt, judgePrompt) || c.opts.Message != judgeMessage {
		t.Errorf("prompt %q, message %q", c.prompt, c.opts.Message)
	}
	var names []string
	for _, p := range c.opts.Files {
		names = append(names, filepath.Base(p))
	}
	if !slices.Equal(names, []string{"reviewer-1.json", "reviewer-2.json", "reviewer-3.json"}) {
		t.Errorf("files = %v", names)
	}
	for name, want := range map[string][]string{"reviewer-1.json": {"1.1", "1.2"}, "reviewer-2.json": {"2.1"}, "reviewer-3.json": nil} {
		ids, fps := sentIDs(t, c.contents[name])
		if !slices.Equal(ids, want) || fps != nil {
			t.Errorf("%s: ids %v, fingerprints %v", name, ids, fps)
		}
	}

	if len(res.Findings) != 2 {
		t.Fatalf("findings = %+v", res.Findings)
	}
	tags := res.MultiReview.Tags
	if res.Findings[0].Title != find11.Title || res.Findings[0].Severity != finding.Blocking || !slices.Equal(tags[0].FoundBy, []int{1, 2}) {
		t.Errorf("first = %+v %+v", res.Findings[0], tags[0])
	}
	if !slices.Equal(tags[0].Also, []string{"f21"}) {
		t.Errorf("Also = %v", tags[0].Also)
	}
	if res.Findings[1].Title != find12.Title || res.Findings[1].Severity != finding.Suggestion || !slices.Equal(tags[1].FoundBy, []int{1}) {
		t.Errorf("second = %+v %+v", res.Findings[1], tags[1])
	}
	mr := res.MultiReview
	if !slices.Equal(mr.Reviewers, []int{1, 2, 3}) || len(mr.Failed) != 0 || mr.Judge != JudgeOK || mr.Cause != nil || mr.Exact {
		t.Errorf("multi review = %+v", mr)
	}
	if res.Verdict != HasBlockingIssues {
		t.Errorf("verdict = %s", res.Verdict)
	}
	if len(res.Runs) != 1 || res.Runs[0].Label != "judge" {
		t.Errorf("runs = %+v", res.Runs)
	}
}

func TestRunMultiReviewDemotes(t *testing.T) {
	f := &judgeFake{t: t}
	f.respond = answerWith(judgeAnswer(t, map[string]any{"id": "1.1", "real": false, "reason": "guarded", "same_as": nil}))
	res := runMulti(t, multiRequest(t, f, reviewerResult(find11), reviewerResult()))

	if len(f.calls) != 1 {
		t.Fatalf("%d runs, want 1", len(f.calls))
	}
	if len(res.Findings) != 1 || res.Findings[0].Severity != finding.Suggestion {
		t.Fatalf("findings = %+v", res.Findings)
	}
	if tag := res.MultiReview.Tags[0]; !tag.JudgeDisagreed || tag.Reason != "guarded" {
		t.Errorf("tag = %+v", tag)
	}
	if res.Verdict != NoBlockingIssues || res.MultiReview.Judge != JudgeOK {
		t.Errorf("verdict %s, judge %s", res.Verdict, res.MultiReview.Judge)
	}
}

func TestRunMultiReviewJudgeCannotDemoteAgreed(t *testing.T) {
	f := &judgeFake{t: t}
	f.respond = answerWith(judgeAnswer(t,
		map[string]any{"id": "1.1", "real": false, "reason": "no", "same_as": []string{"2.1"}},
		map[string]any{"id": "2.1", "real": false, "reason": "no", "same_as": []string{"1.1"}}))
	res := runMulti(t, multiRequest(t, f, reviewerResult(find11), reviewerResult(find21)))

	if len(res.Findings) != 1 || res.Findings[0].Severity != finding.Blocking {
		t.Fatalf("findings = %+v", res.Findings)
	}
	if tag := res.MultiReview.Tags[0]; tag.JudgeDisagreed || !slices.Equal(tag.FoundBy, []int{1, 2}) {
		t.Errorf("tag = %+v", tag)
	}
	if res.Verdict != HasBlockingIssues {
		t.Errorf("verdict = %s", res.Verdict)
	}
}

// exactPair is reviewer 1 with find11, and reviewer 2 with the same finding
// in other words plus find21: an exact merge has 2 findings, both blocking.
func exactPair(t *testing.T, f *judgeFake) MultiReviewRequest {
	same := find11
	same.Explanation, same.Fingerprint = "other words", "f22"
	return multiRequest(t, f, reviewerResult(find11), reviewerResult(same, find21))
}

func checkExact(t *testing.T, res SkillResult, judge string) {
	t.Helper()
	mr := res.MultiReview
	if !mr.Exact || mr.Judge != judge {
		t.Errorf("multi review = %+v, want exact with judge %q", mr, judge)
	}
	if got := titles(res.Findings); !slices.Equal(got, []string{find11.Title, find21.Title}) {
		t.Fatalf("titles = %v", got)
	}
	checkBlocking(t, res, 2)
	if !slices.Equal(mr.Tags[0].FoundBy, []int{1, 2}) || res.Verdict != HasBlockingIssues {
		t.Errorf("tag %+v, verdict %s", mr.Tags[0], res.Verdict)
	}
}

func TestRunMultiReviewJudgeFailsIsExact(t *testing.T) {
	quota := &runner.Cause{Kind: runner.CauseOpencodeError, ProviderClass: runner.ClassQuota, ProviderStatus: 429}
	f := &judgeFake{t: t}
	f.respond = func(judgeCall) runner.RunResult { return runner.RunResult{Status: runner.StatusFailed, Cause: quota} }
	res := runMulti(t, exactPair(t, f))

	checkExact(t, res, JudgeFailed)
	if res.MultiReview.Cause != quota {
		t.Errorf("cause = %+v", res.MultiReview.Cause)
	}
}

func TestRunMultiReviewBadAnswerIsExact(t *testing.T) {
	f := &judgeFake{t: t}
	f.respond = answerWith("no json here")
	res := runMulti(t, exactPair(t, f))

	checkExact(t, res, JudgeFailed)
	if res.MultiReview.Cause != nil {
		t.Errorf("cause = %+v", res.MultiReview.Cause)
	}
}

func TestRunMultiReviewLowBudgetIsExact(t *testing.T) {
	f := &judgeFake{t: t}
	req := exactPair(t, f)
	req.Budget = time.Minute
	res := runMulti(t, req)

	if len(f.calls) != 0 {
		t.Errorf("%d runs started", len(f.calls))
	}
	checkExact(t, res, JudgeSkipped)
}

func TestRunMultiReviewOverLimitIsExact(t *testing.T) {
	many := func(n, count int) []finding.Finding {
		var fs []finding.Finding
		for i := range count {
			fs = append(fs, finding.Finding{File: "a/x.go", Line: i + 1, Severity: finding.Blocking, Title: fmt.Sprintf("Bug %d of %d", i, n), Explanation: "e"})
		}
		return fs
	}
	for _, tc := range []struct {
		second int
		runs   int
		judge  string
	}{{100, 1, JudgeOK}, {101, 0, JudgeSkipped}} {
		f := &judgeFake{t: t}
		f.respond = answerWith(judgeAnswer(t))
		res := runMulti(t, multiRequest(t, f, reviewerResult(many(1, 100)...), reviewerResult(many(2, tc.second)...)))

		if len(f.calls) != tc.runs || res.MultiReview.Judge != tc.judge || res.MultiReview.Exact != (tc.runs == 0) {
			t.Errorf("%d findings: %d runs, multi review %+v", 100+tc.second, len(f.calls), res.MultiReview)
		}
		checkBlocking(t, res, 100+tc.second)
	}
}

func TestRunMultiReviewNothingToJudge(t *testing.T) {
	f := &judgeFake{t: t}
	res := runMulti(t, multiRequest(t, f, reviewerResult(), reviewerResult(find12)))

	if len(f.calls) != 0 {
		t.Errorf("%d runs started", len(f.calls))
	}
	mr := res.MultiReview
	if mr.Judge != JudgeNotRun || mr.Exact {
		t.Errorf("multi review = %+v", mr)
	}
	if len(res.Findings) != 1 || res.Findings[0].Severity != finding.Suggestion || !slices.Equal(mr.Tags[0].FoundBy, []int{2}) {
		t.Errorf("findings %+v, tags %+v", res.Findings, mr.Tags)
	}
	if res.Verdict != NoBlockingIssues {
		t.Errorf("verdict = %s", res.Verdict)
	}
}

func TestRunMultiReviewSuggestionsAreGrouped(t *testing.T) {
	other := find12
	other.Line, other.Title, other.Fingerprint = 4, "Pick a better name", "f22"
	f := &judgeFake{t: t}
	f.respond = answerWith(judgeAnswer(t, map[string]any{"id": "1.1", "real": nil, "same_as": []string{"2.1"}}))
	res := runMulti(t, multiRequest(t, f, reviewerResult(find12), reviewerResult(other)))

	if len(f.calls) != 1 {
		t.Fatalf("%d runs, want 1", len(f.calls))
	}
	if len(res.Findings) != 1 || res.Findings[0].Severity != finding.Suggestion || !slices.Equal(res.MultiReview.Tags[0].FoundBy, []int{1, 2}) {
		t.Errorf("findings %+v, tags %+v", res.Findings, res.MultiReview.Tags)
	}
}

func TestRunMultiReviewConfidenceIsLowest(t *testing.T) {
	for _, tc := range []struct {
		scores     []int
		want       int
		wantReason string
	}{
		{[]int{4, 2, 3}, 2, "r2"},
		{[]int{4, 0, 3}, 0, ""},
		{[]int{3, 2, 2}, 2, "r2"},
		{[]int{1, 1, 1}, 1, "r1"},
	} {
		var rs []SkillResult
		for i, score := range tc.scores {
			res := reviewerResult()
			res.Confidence = score
			if score > 0 {
				res.ConfidenceReason = fmt.Sprintf("r%d", i+1)
			}
			rs = append(rs, res)
		}
		res := runMulti(t, multiRequest(t, &judgeFake{t: t}, rs...))
		if res.Confidence != tc.want || res.ConfidenceReason != tc.wantReason {
			t.Errorf("%v: got %d %q, want %d %q", tc.scores, res.Confidence, res.ConfidenceReason, tc.want, tc.wantReason)
		}
	}
}

func TestRunMultiReviewBaseIsLowestUsable(t *testing.T) {
	r1 := SkillResult{Skill: "s", Verdict: Failed, Failures: []BatchFailure{{Message: "boom"}}, Notes: []string{"n1"}}
	r2 := reviewerResult(find11)
	r2.Batches, r2.BatchesFinished = 2, 2
	r2.Summaries = []BatchSummary{{Batch: 1, Text: "two"}}
	r2.Notes = []string{"n2", "shared"}
	r2.Usage, r2.Duration = runner.Usage{Input: 99}, time.Hour
	r3 := reviewerResult()
	r3.Summaries = []BatchSummary{{Batch: 1, Text: "three"}}
	r3.Notes = []string{"shared", "n3"}
	f := &judgeFake{t: t}
	f.respond = answerWith(judgeAnswer(t, map[string]any{"id": "2.1", "real": true, "same_as": nil}))
	res := runMulti(t, multiRequest(t, f, r1, r2, r3))

	if res.Batches != 2 || len(res.Summaries) != 1 || res.Summaries[0].Text != "two" || len(res.Failures) != 0 {
		t.Errorf("base = %+v", res)
	}
	if !slices.Equal(res.Notes, []string{"n2", "shared", "n3"}) {
		t.Errorf("notes = %v", res.Notes)
	}
	mr := res.MultiReview
	if !slices.Equal(mr.Reviewers, []int{2, 3}) || !slices.Equal(mr.Failed, []int{1}) {
		t.Errorf("multi review = %+v", mr)
	}
	if len(f.calls) != 1 {
		t.Fatalf("%d runs, want 1", len(f.calls))
	}
	if _, ok := f.calls[0].contents["reviewer-1.json"]; ok {
		t.Error("the unusable reviewer's file was attached")
	}
	if res.Usage.Input != 0 || res.Duration >= time.Hour || res.Verdict != HasBlockingIssues {
		t.Errorf("usage %+v, duration %s, verdict %s", res.Usage, res.Duration, res.Verdict)
	}
	// The input is not changed.
	if !slices.Equal(r2.Notes, []string{"n2", "shared"}) {
		t.Errorf("reviewer 2's notes changed: %v", r2.Notes)
	}
}

func TestRunMultiReviewRejectedAttached(t *testing.T) {
	f := &judgeFake{t: t}
	f.respond = answerWith(judgeAnswer(t))
	req := multiRequest(t, f, reviewerResult(find11), reviewerResult(find21))
	req.Dismissals = map[string][]finding.Rejected{"s": rejectedBoth}
	runMulti(t, req)

	if len(f.calls) != 1 {
		t.Fatalf("%d runs, want 1", len(f.calls))
	}
	c := f.calls[0]
	data, ok := c.contents[rejectedFile]
	if !ok {
		t.Fatalf("no %s in %v", rejectedFile, c.opts.Files)
	}
	var got struct{ Rejected []finding.Rejected }
	if err := json.Unmarshal([]byte(data), &got); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Rejected, rejectedBoth[:1]) {
		t.Errorf("rejected = %+v, want the blocking one only", got.Rejected)
	}
	if c.opts.Message != judgeMessage+" "+judgeRejectedMessage {
		t.Errorf("message = %q", c.opts.Message)
	}
}

// TestRunMultiReviewDismissedFindingLinksNothing: reviewer 2 reports a
// finding people dismissed as blocking, and the judge links reviewer 1's
// blocking finding to it. The link is ignored, so the dismissal cannot hide
// reviewer 1's finding.
func TestRunMultiReviewDismissedFindingLinksNothing(t *testing.T) {
	old := finding.Finding{File: "a/x.go", Line: 7, Severity: finding.Blocking, Title: "Old dismissed bug", Explanation: "e"}
	old.Fingerprint = finding.Fingerprint("s", old.File, old.Title)
	f := &judgeFake{t: t}
	f.respond = answerWith(judgeAnswer(t,
		map[string]any{"id": "1.1", "real": true, "reason": "yes", "same_as": []string{"2.1"}},
		map[string]any{"id": "2.1", "real": true, "reason": "yes", "same_as": []string{"1.1"}}))
	req := multiRequest(t, f, reviewerResult(find11), reviewerResult(old))
	req.Dismissals = map[string][]finding.Rejected{"s": {{File: old.File, Title: old.Title, Severity: string(finding.Blocking), Reason: "fine"}}}
	res := runMulti(t, req)

	if got := titles(res.Findings); !slices.Equal(got, []string{find11.Title, old.Title}) {
		t.Fatalf("titles = %v", got)
	}
	if tag := res.MultiReview.Tags[0]; tag.Also != nil || !slices.Equal(tag.FoundBy, []int{1}) {
		t.Errorf("tag = %+v", tag)
	}
}

func TestRunMultiReviewJudgeGetsRules(t *testing.T) {
	onPy := finding.Finding{File: "b/y.py", Line: 1, Severity: finding.Suggestion, Title: "Py style", Explanation: "e", Fingerprint: "fpy"}
	f := &judgeFake{t: t}
	f.respond = answerWith(judgeAnswer(t))
	req := multiRequest(t, f, reviewerResult(find11), reviewerResult(onPy))
	req.Rules = repoRules(rule.Entry{Path: "*.go", Rule: "GO RULE"}, rule.Entry{Path: "*.py", Rule: "PY RULE"})
	runMulti(t, req)

	if len(f.calls) != 1 {
		t.Fatalf("%d runs, want 1", len(f.calls))
	}
	c := f.calls[0]
	// Only the blocking findings' files: the suggestion's rule is left out.
	want := "## Rule: repo · *.go\nFiles: \"a/x.go\"\n\nGO RULE\n"
	if got, ok := c.contents[rulesFile]; !ok || got != want {
		t.Errorf("%s = %q, want %q", rulesFile, got, want)
	}
	if c.opts.Message != judgeMessage+" "+judgeRulesMessage {
		t.Errorf("message = %q", c.opts.Message)
	}
}

func TestRunMultiReviewJudgeRulesWriteFailureIsExact(t *testing.T) {
	old := writeRules
	t.Cleanup(func() { writeRules = old })
	writeRules = func(string, []byte, os.FileMode) error { return errors.New("disk full") }
	f := &judgeFake{t: t}
	req := exactPair(t, f)
	req.Rules = repoRules(rule.Entry{Path: "*.go", Rule: "GO RULE"})
	res := runMulti(t, req)

	if len(f.calls) != 0 {
		t.Errorf("the judge ran without the rules of the findings it judges")
	}
	checkExact(t, res, JudgeFailed)
}

func TestRunMultiReviewUsageIsTheJudgeRun(t *testing.T) {
	f := &judgeFake{t: t}
	f.respond = func(judgeCall) runner.RunResult {
		r := completed(judgeAnswer(t))
		r.Usage = runner.Usage{Input: 10}
		return r
	}
	r1, r2 := reviewerResult(find11), reviewerResult()
	r1.Usage, r2.Usage = runner.Usage{Input: 50}, runner.Usage{Input: 70}
	res := runMulti(t, multiRequest(t, f, r1, r2))

	if res.Usage.Input != 10 {
		t.Errorf("usage = %+v", res.Usage)
	}
	if len(res.Runs) != 1 || res.Runs[0].Usage.Input != 10 {
		t.Errorf("runs = %+v", res.Runs)
	}
}

// TestRunMultiReviewSkillsInOrder: each skill has its own judge run, and
// the results are in req.Skills order.
func TestRunMultiReviewSkillsInOrder(t *testing.T) {
	f := &judgeFake{t: t}
	f.respond = answerWith(judgeAnswer(t))
	req := multiRequest(t, f)
	req.Skills = []skill.Skill{{Name: "s", Body: "Review it."}, {Name: "t", Body: "Review it."}}
	req.Concurrency = 2
	for n := 1; n <= 2; n++ {
		req.Reviewers = append(req.Reviewers, Reviewer{Number: n, Skills: map[string]SkillResult{"s": reviewerResult(find11), "t": reviewerResult(find21)}})
	}
	rs := RunMultiReview(context.Background(), req)

	if len(rs) != 2 || rs[0].Skill != "s" || rs[1].Skill != "t" {
		t.Fatalf("results = %+v", rs)
	}
	prefixes := []string{f.calls[0].opts.LogPrefix, f.calls[1].opts.LogPrefix}
	slices.Sort(prefixes)
	if !slices.Equal(prefixes, []string{"[s judge]", "[t judge]"}) {
		t.Errorf("prefixes = %v", prefixes)
	}
	for _, res := range rs {
		if res.MultiReview == nil || res.MultiReview.Judge != JudgeOK || len(res.Runs) != 1 {
			t.Errorf("%s: %+v", res.Skill, res.MultiReview)
		}
	}
}

// judgeAnswer is a judge's answer with the given results.
func judgeAnswer(t *testing.T, results ...map[string]any) string {
	t.Helper()
	return verifyAnswer(t, results...)
}
