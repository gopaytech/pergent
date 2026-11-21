package result

import (
	"encoding/json"
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

func TestBuildWithoutPanelIsUnchanged(t *testing.T) {
	sr := review.SkillResult{Skill: "review", Verdict: review.NoBlockingIssues, Findings: []finding.Finding{
		{File: "x.go", Line: 1, Severity: finding.Blocking, Title: "t", Explanation: "e", Fingerprint: "aaaaaaaaaaaa"}}}
	b, _ := Encode(Build(testMeta(), []review.SkillResult{sr}))
	for _, k := range []string{`"panel"`, `"tokens_by_model"`, `"role"`} {
		if strings.Contains(string(b), k) {
			t.Fatalf("%s in a non-panel result: %s", k, b)
		}
	}
	// BuildPanel with no skill panel adds only the top-level panel.
	var m map[string]json.RawMessage
	b2, _ := Encode(BuildPanel(testMeta(), testPanel(), []review.SkillResult{sr}))
	_ = json.Unmarshal(b2, &m)
	if _, ok := m["panel"]; !ok {
		t.Fatal("top-level panel missing")
	}
}
