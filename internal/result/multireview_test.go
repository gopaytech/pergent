package result

import (
	"strings"
	"testing"

	"github.com/gopaytech/pergent/internal/finding"
	"github.com/gopaytech/pergent/internal/multireview"
	"github.com/gopaytech/pergent/internal/review"
	"github.com/gopaytech/pergent/internal/runner"
)

func multiMeta() Meta {
	m := testMeta()
	m.Provider, m.Model = "judgeprov", "judgemodel"
	return m
}

func multiSkill() review.SkillResult {
	f1 := finding.Finding{File: "x.go", Line: 1, Severity: finding.Blocking, Title: "one", Explanation: "e", Fingerprint: "aaaaaaaaaaaa"}
	f2 := finding.Finding{File: "y.go", Line: 2, Severity: finding.Suggestion, Title: "two", Explanation: "e", Fingerprint: "bbbbbbbbbbbb"}
	return review.SkillResult{
		Skill:    "review",
		Verdict:  review.NoBlockingIssues,
		Findings: []finding.Finding{f1, f2},
		MultiReview: &review.SkillMultiReview{
			Reviewers: []int{1, 3},
			Failed:    []int{2},
			Judge:     review.JudgeOK,
			Tags: []multireview.Tag{
				{FoundBy: []int{1, 3}, Also: []string{"cccccccccccc"}},
				{FoundBy: []int{3}, JudgeDisagreed: true, Reason: "not real", RatedSuggestionBy: []int{1}},
			},
		},
	}
}

func testMulti() MultiReview {
	return MultiReview{
		Reviewers: []MultiReviewer{{1, true}, {2, false}, {3, true}},
		Judge:     MultiJudge{Provider: "judgeprov", Model: "judgemodel", Judged: true},
	}
}

func encodeMap(t *testing.T, r Result) map[string]any {
	t.Helper()
	b, err := Encode(r)
	if err != nil {
		t.Fatal(err)
	}
	return decode(t, b)
}

func TestBuildMultiReviewFields(t *testing.T) {
	m := encodeMap(t, BuildMultiReview(multiMeta(), testMulti(), []review.SkillResult{multiSkill()}))
	top := m["multi_review"].(map[string]any)
	rs := top["reviewers"].([]any)
	if len(rs) != 3 || rs[1].(map[string]any)["number"] != float64(2) || rs[1].(map[string]any)["usable"] != false || rs[0].(map[string]any)["usable"] != true {
		t.Fatalf("reviewers = %v", rs)
	}
	j := top["judge"].(map[string]any)
	if j["provider"] != "judgeprov" || j["model"] != "judgemodel" || j["judged"] != true {
		t.Fatalf("judge = %v", j)
	}
	sk := m["skills"].([]any)[0].(map[string]any)
	sm := sk["multi_review"].(map[string]any)
	if got := sm["reviewers"].([]any); len(got) != 2 || got[0] != float64(1) || got[1] != float64(3) {
		t.Fatalf("skill reviewers = %v", got)
	}
	if got := sm["failed"].([]any); len(got) != 1 || got[0] != float64(2) || sm["judge"] != "ok" {
		t.Fatalf("skill multi_review = %v", sm)
	}
	fs := sk["findings"].([]any)
	f0 := fs[0].(map[string]any)["multi_review"].(map[string]any)
	if len(f0["found_by"].([]any)) != 2 || f0["also"].([]any)[0] != "cccccccccccc" {
		t.Fatalf("finding 0 = %v", f0)
	}
	f1 := fs[1].(map[string]any)["multi_review"].(map[string]any)
	if f1["judge_disagreed"] != true || f1["reason"] != "not real" || f1["rated_suggestion_by"].([]any)[0] != float64(1) {
		t.Fatalf("finding 1 = %v", f1)
	}
}

func TestBuildMultiReviewReviewersNeverNull(t *testing.T) {
	sr := review.SkillResult{Skill: "review", Verdict: review.Failed,
		MultiReview: &review.SkillMultiReview{Judge: review.JudgeNotRun}}
	r := BuildMultiReview(multiMeta(), MultiReview{}, []review.SkillResult{sr})
	b, err := Encode(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"multi_review":{"reviewers":[],"judge":"not_run"}`) {
		t.Fatalf("skill reviewers not []: %s", b)
	}
	if !strings.Contains(string(b), `"multi_review":{"reviewers":[],"judge":{"judged":false}}`) {
		t.Fatalf("top reviewers not []: %s", b)
	}
}

func TestBuildMultiReviewJudgeProviderError(t *testing.T) {
	sr := multiSkill()
	sr.MultiReview.Judge = review.JudgeFailed
	sr.MultiReview.Cause = &runner.Cause{Kind: runner.CauseOpencodeError, ProviderClass: runner.ClassQuota, ProviderStatus: 429}
	m := encodeMap(t, BuildMultiReview(multiMeta(), testMulti(), []review.SkillResult{sr}))
	top := m["provider_error"].(map[string]any)
	if top["class"] != "quota" || top["role"] != "judge" || top["provider"] != "judgeprov" || top["model"] != "judgemodel" {
		t.Fatalf("top = %v", top)
	}
	sk := m["skills"].([]any)[0].(map[string]any)
	pe := sk["multi_review"].(map[string]any)["judge_cause"].(map[string]any)["provider_error"].(map[string]any)
	if pe["class"] != "quota" || pe["role"] != "judge" || pe["provider"] != "judgeprov" || pe["model"] != "judgemodel" {
		t.Fatalf("judge provider_error = %v", pe)
	}
}

func TestBuildMultiReviewKeepsMoreSevereBatchError(t *testing.T) {
	sr := multiSkill()
	sr.Failures = []review.BatchFailure{{Files: []string{"x.go"}, Cause: &runner.Cause{Kind: runner.CauseOpencodeError, ProviderClass: runner.ClassAuth, ProviderStatus: 401}}}
	sr.MultiReview.Judge = review.JudgeFailed
	sr.MultiReview.Cause = &runner.Cause{Kind: runner.CauseOpencodeError, ProviderClass: runner.ClassQuota}
	m := encodeMap(t, BuildMultiReview(multiMeta(), testMulti(), []review.SkillResult{sr}))
	if top := m["provider_error"].(map[string]any); top["class"] != "auth" || top["role"] != nil {
		t.Fatalf("top = %v", top)
	}
}

func TestBuildMultiReviewNoJudge(t *testing.T) {
	mr := MultiReview{Reviewers: []MultiReviewer{{1, true}}}
	b, err := Encode(BuildMultiReview(multiMeta(), mr, []review.SkillResult{multiSkill()}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"judge":{"judged":false}`) {
		t.Fatalf("judge = %s", b)
	}
}

func TestBuildMultiReviewDoesNotChangeInputs(t *testing.T) {
	sr := multiSkill()
	mr := testMulti()
	r := BuildMultiReview(multiMeta(), mr, []review.SkillResult{sr})
	r.MultiReview.Reviewers[0].Number = 99
	r.Skills[0].MultiReview.Reviewers[0] = 99
	r.Skills[0].Findings[0].MultiReview.FoundBy[0] = 99
	r.Skills[0].Findings[0].MultiReview.Also[0] = "zzz"
	if mr.Reviewers[0].Number != 1 || sr.MultiReview.Reviewers[0] != 1 || sr.MultiReview.Tags[0].FoundBy[0] != 1 || sr.MultiReview.Tags[0].Also[0] != "cccccccccccc" {
		t.Fatal("an input was changed")
	}
}

func TestRedactKeepsMultiReviewFields(t *testing.T) {
	key := "pk-test-0123456789abcdef"
	t.Setenv("OPENCODE_API_KEY", key)
	sr := multiSkill()
	sr.MultiReview.Tags[0].Also = []string{"fp-" + key}
	sr.MultiReview.Tags[1].Reason = "leaked " + key
	sr.MultiReview.Judge = review.JudgeFailed
	sr.MultiReview.Exact = true
	sr.MultiReview.Cause = &runner.Cause{Kind: runner.CauseOpencodeError, ErrorName: "APIError", ErrorMessage: "bad key " + key,
		ProviderClass: runner.ClassAuth, ProviderStatus: 401}
	mr := testMulti()
	mr.Judge.Provider, mr.Judge.Model = "prov-"+key, "model-"+key
	meta := multiMeta()
	meta.Provider, meta.Model = "prov-"+key, "model-"+key
	b, err := Encode(BuildMultiReview(meta, mr, []review.SkillResult{sr}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), key) {
		t.Fatalf("key in result: %s", b)
	}
	if !strings.Contains(string(b), "[REDACTED]") {
		t.Fatalf("no redaction: %s", b)
	}
	m := decode(t, b)
	sk := m["skills"].([]any)[0].(map[string]any)
	sm := sk["multi_review"].(map[string]any)
	if sm["judge"] != "failed" || sm["exact_merge"] != true || len(sm["failed"].([]any)) != 1 || sm["judge_cause"] == nil {
		t.Fatalf("skill multi_review dropped fields: %v", sm)
	}
	fs := sk["findings"].([]any)
	f0 := fs[0].(map[string]any)["multi_review"].(map[string]any)
	if len(f0["found_by"].([]any)) != 2 || len(f0["also"].([]any)) != 1 {
		t.Fatalf("finding 0 dropped fields: %v", f0)
	}
	f1 := fs[1].(map[string]any)["multi_review"].(map[string]any)
	if f1["judge_disagreed"] != true || len(f1["rated_suggestion_by"].([]any)) != 1 || !strings.Contains(f1["reason"].(string), "leaked") {
		t.Fatalf("finding 1 dropped fields: %v", f1)
	}
	if m["provider_error"].(map[string]any)["role"] != "judge" {
		t.Fatalf("role dropped: %v", m["provider_error"])
	}
	// Unjudged is its own field.
	sr.MultiReview.Tags[0].Unjudged = true
	m2 := encodeMap(t, BuildMultiReview(multiMeta(), testMulti(), []review.SkillResult{sr}))
	u := m2["skills"].([]any)[0].(map[string]any)["findings"].([]any)[0].(map[string]any)["multi_review"].(map[string]any)
	if u["unjudged"] != true {
		t.Fatalf("unjudged dropped: %v", u)
	}
}

// noMultiReviewGolden is Build's output for the fixed input below, taken from
// the code before the multi review fields. A result without one must stay
// byte for byte.
const noMultiReviewGolden = `{"schema_version":1,"status":"completed","pergent_version":"0.1.0","base":"base111","head":"head222","started_at":"2026-01-02T03:04:05Z","finished_at":"2026-01-02T03:06:05Z","budget_seconds":600,"skills":[{"name":"review","verdict":"no_blocking_issues","findings":[{"id":"aaaaaa","fingerprint":"aaaaaaaaaaaa","file":"x.go","line":1,"severity":"blocking","title":"t","explanation":"e"}],"files":{"reviewed":[],"skipped":[],"not_reviewed":[{"path":"x.go","cause":"opencode reported error."}]},"failures":[{"files":["x.go"],"message":"opencode reported error.","cause":{"kind":"opencode_error","message":"opencode reported error.","provider_error":{"class":"quota","status":429}}}],"notes":[],"batches":0,"batches_finished":0,"duration_seconds":0,"tokens":{"input":0,"output":0,"reasoning":0,"cache_read":0,"cache_write":0}}],"provider_error":{"class":"quota","status":429},"comment":{"markdown":""}}`

func TestBuildWithoutMultiReviewIsUnchanged(t *testing.T) {
	sr := review.SkillResult{Skill: "review", Verdict: review.NoBlockingIssues, Findings: []finding.Finding{
		{File: "x.go", Line: 1, Severity: finding.Blocking, Title: "t", Explanation: "e", Fingerprint: "aaaaaaaaaaaa"}},
		Failures: []review.BatchFailure{{Files: []string{"x.go"}, Cause: &runner.Cause{Kind: runner.CauseOpencodeError, ProviderClass: runner.ClassQuota, ProviderStatus: 429}}}}
	b, err := Encode(Build(testMeta(), []review.SkillResult{sr}))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); got != noMultiReviewGolden {
		t.Fatalf("bytes changed:\n%s", got)
	}
	if strings.Contains(string(b), "multi_review") || strings.Contains(string(b), `"role"`) {
		t.Fatalf("multi review key in a plain result: %s", b)
	}
}
