package result

import (
	"strings"
	"testing"

	"github.com/gopaytech/pergent/internal/finding"
	"github.com/gopaytech/pergent/internal/panel"
	"github.com/gopaytech/pergent/internal/review"
	"github.com/gopaytech/pergent/internal/runner"
)

func testPanel() Panel {
	return Panel{
		A:      PanelMember{Provider: "provA", Model: "modelA"},
		B:      PanelMember{Provider: "provB", Model: "modelB"},
		Mode:   "both",
		Judged: true,
	}
}

func panelSkill() review.SkillResult {
	f1 := finding.Finding{File: "x.go", Line: 1, Severity: finding.Blocking, Title: "one", Explanation: "e", Fingerprint: "aaaaaaaaaaaa"}
	f2 := finding.Finding{File: "y.go", Line: 2, Severity: finding.Suggestion, Title: "two", Explanation: "e", Fingerprint: "bbbbbbbbbbbb"}
	return review.SkillResult{
		Skill:    "review",
		Verdict:  review.NoBlockingIssues,
		Findings: []finding.Finding{f1, f2},
		Panel: &review.SkillPanel{
			Mode: "both",
			Tags: []panel.Tag{
				{FoundBy: panel.Both, Also: []string{"cccccccccccc"}},
				{FoundBy: panel.B, DisagreedBy: panel.A, Reason: "not real"},
			},
			UsageByRole: map[string]runner.Usage{"b": {Input: 2}, "a": {Input: 1}},
		},
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

func TestBuildPanelFields(t *testing.T) {
	m := encodeMap(t, BuildPanel(testMeta(), testPanel(), []review.SkillResult{panelSkill()}))
	p := m["panel"].(map[string]any)
	if p["mode"] != "both" || p["judged"] != true || p["a"].(map[string]any)["model"] != "modelA" {
		t.Fatalf("panel = %v", p)
	}
	sk := m["skills"].([]any)[0].(map[string]any)
	if sk["panel"].(map[string]any)["mode"] != "both" {
		t.Fatalf("skill panel = %v", sk["panel"])
	}
	fs := sk["findings"].([]any)
	f0 := fs[0].(map[string]any)["panel"].(map[string]any)
	if f0["found_by"] != "both" || f0["also"].([]any)[0] != "cccccccccccc" {
		t.Fatalf("finding 0 panel = %v", f0)
	}
	f1 := fs[1].(map[string]any)["panel"].(map[string]any)
	if f1["disagreed_by"] != "a" || f1["reason"] != "not real" {
		t.Fatalf("finding 1 panel = %v", f1)
	}
	tbm := sk["tokens_by_model"].([]any)
	if len(tbm) != 2 {
		t.Fatalf("tokens_by_model = %v", tbm)
	}
	a, b := tbm[0].(map[string]any), tbm[1].(map[string]any)
	if a["role"] != "a" || a["provider"] != "provA" || a["model"] != "modelA" || a["tokens"].(map[string]any)["input"] != float64(1) {
		t.Fatalf("A entry = %v", a)
	}
	if b["role"] != "b" || b["tokens"].(map[string]any)["input"] != float64(2) {
		t.Fatalf("B entry = %v", b)
	}
}

func TestBuildPanelJudgeProviderError(t *testing.T) {
	sr := panelSkill()
	sr.Panel.Judges = []review.JudgeRun{
		{Role: "b", Status: "failed", Cause: &runner.Cause{Kind: runner.CauseOpencodeError, ProviderClass: runner.ClassQuota, ProviderStatus: 429}},
		{Role: "a", Status: "ok"},
	}
	m := encodeMap(t, BuildPanel(testMeta(), testPanel(), []review.SkillResult{sr}))
	top := m["provider_error"].(map[string]any)
	if top["class"] != "quota" || top["role"] != "b" || top["provider"] != "provB" || top["model"] != "modelB" {
		t.Fatalf("top = %v", top)
	}
	sk := m["skills"].([]any)[0].(map[string]any)
	j := sk["panel"].(map[string]any)["judges"].([]any)[0].(map[string]any)
	pe := j["cause"].(map[string]any)["provider_error"].(map[string]any)
	if pe["class"] != "quota" || pe["provider"] != "provB" || pe["model"] != "modelB" {
		t.Fatalf("judge provider_error = %v", pe)
	}
}

func TestBuildPanelKeepsMoreSevereBatchError(t *testing.T) {
	sr := panelSkill()
	sr.Failures = []review.BatchFailure{{Files: []string{"x.go"}, Cause: &runner.Cause{Kind: runner.CauseOpencodeError, ProviderClass: runner.ClassAuth, ProviderStatus: 401}}}
	sr.Panel.Judges = []review.JudgeRun{{Role: "b", Status: "failed", Cause: &runner.Cause{Kind: runner.CauseOpencodeError, ProviderClass: runner.ClassQuota}}}
	m := encodeMap(t, BuildPanel(testMeta(), testPanel(), []review.SkillResult{sr}))
	top := m["provider_error"].(map[string]any)
	if top["class"] != "auth" || top["role"] != nil {
		t.Fatalf("top = %v", top)
	}
}

func TestRedactKeepsPanelFields(t *testing.T) {
	secret := "abcdefghijklmnopqrstuvwxyz0123456789"
	sr := panelSkill()
	sr.Panel.Tags[0].Also = []string{"Bearer " + secret}
	sr.Panel.Tags[1].Reason = "leaked Bearer " + secret
	sr.Panel.Judges = []review.JudgeRun{{Role: "b", Status: "failed", Cause: &runner.Cause{Kind: runner.CauseOpencodeError, ProviderClass: runner.ClassQuota}}}
	r := BuildPanel(testMeta(), testPanel(), []review.SkillResult{sr})
	b, err := Encode(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), secret) {
		t.Fatalf("secret in result: %s", b)
	}
	m := decode(t, b)
	sk := m["skills"].([]any)[0].(map[string]any)
	fs := sk["findings"].([]any)
	if fs[0].(map[string]any)["panel"].(map[string]any)["found_by"] != "both" || len(fs[0].(map[string]any)["panel"].(map[string]any)["also"].([]any)) != 1 {
		t.Fatalf("finding panel dropped: %v", fs[0])
	}
	if !strings.Contains(fs[1].(map[string]any)["panel"].(map[string]any)["reason"].(string), "leaked") {
		t.Fatalf("reason dropped: %v", fs[1])
	}
	if sk["panel"] == nil || sk["tokens_by_model"] == nil || m["panel"] == nil {
		t.Fatalf("panel parts dropped: %v", m)
	}
	if m["provider_error"].(map[string]any)["role"] != "b" {
		t.Fatalf("role dropped: %v", m["provider_error"])
	}
}

func TestRedactPanelNamesAndJudgeCause(t *testing.T) {
	key := "pk-test-0123456789abcdef"
	t.Setenv("PERGENT_PANEL_B_API_KEY", key)
	sr := panelSkill()
	sr.Panel.Judges = []review.JudgeRun{{Role: "b", Status: "failed", Cause: &runner.Cause{
		Kind: runner.CauseOpencodeError, ErrorName: "APIError", ErrorMessage: "bad key " + key,
		ProviderClass: runner.ClassAuth, ProviderStatus: 401}}}
	p := testPanel()
	p.A.Model = "model-" + key
	p.B.Provider = "prov-" + key
	p.B.Model = "model-" + key
	b, err := Encode(BuildPanel(testMeta(), p, []review.SkillResult{sr}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), key) {
		t.Fatalf("key in result: %s", b)
	}
	if !strings.Contains(string(b), "[REDACTED]") || !strings.Contains(string(b), `"tokens_by_model"`) {
		t.Fatalf("unexpected result: %s", b)
	}
}

// noPanelGolden is Build's output for the fixed input below, taken from the
// code before the panel fields. A non-panel result must stay byte for byte.
const noPanelGolden = `{"schema_version":1,"status":"completed","pergent_version":"0.1.0","base":"base111","head":"head222","started_at":"2026-01-02T03:04:05Z","finished_at":"2026-01-02T03:06:05Z","budget_seconds":600,"skills":[{"name":"review","verdict":"no_blocking_issues","findings":[{"id":"aaaaaa","fingerprint":"aaaaaaaaaaaa","file":"x.go","line":1,"severity":"blocking","title":"t","explanation":"e"}],"files":{"reviewed":[],"skipped":[],"not_reviewed":[{"path":"x.go","cause":"opencode reported error."}]},"failures":[{"files":["x.go"],"message":"opencode reported error.","cause":{"kind":"opencode_error","message":"opencode reported error.","provider_error":{"class":"quota","status":429}}}],"notes":[],"batches":0,"batches_finished":0,"duration_seconds":0,"tokens":{"input":0,"output":0,"reasoning":0,"cache_read":0,"cache_write":0}}],"provider_error":{"class":"quota","status":429},"comment":{"markdown":""}}`

func TestBuildWithoutPanelIsUnchanged(t *testing.T) {
	sr := review.SkillResult{Skill: "review", Verdict: review.NoBlockingIssues, Findings: []finding.Finding{
		{File: "x.go", Line: 1, Severity: finding.Blocking, Title: "t", Explanation: "e", Fingerprint: "aaaaaaaaaaaa"}},
		Failures: []review.BatchFailure{{Files: []string{"x.go"}, Cause: &runner.Cause{Kind: runner.CauseOpencodeError, ProviderClass: runner.ClassQuota, ProviderStatus: 429}}}}
	b, err := Encode(Build(testMeta(), []review.SkillResult{sr}))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); got != noPanelGolden {
		t.Fatalf("non-panel bytes changed:\n%s", got)
	}
}
