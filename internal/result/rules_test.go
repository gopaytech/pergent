package result

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/gopaytech/pergent/internal/review"
)

func TestFileRulesSurviveEncode(t *testing.T) {
	sr := review.SkillResult{Skill: "s", Verdict: review.NoBlockingIssues, Reviewed: []string{"a.go", "b.md"},
		FileRules: []review.FileRule{{Path: "a.go", Rule: "repo:**/*.go"}}}
	b, err := Encode(Build(testMeta(), []review.SkillResult{sr}))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Skills []struct {
			Files struct {
				Rules []FileRule `json:"rules"`
			} `json:"files"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	want := []FileRule{{Path: "a.go", Rule: "repo:**/*.go"}}
	if len(got.Skills) != 1 || len(got.Skills[0].Files.Rules) != 1 || got.Skills[0].Files.Rules[0] != want[0] {
		t.Errorf("files.rules = %+v, want %+v", got.Skills, want)
	}
}

func TestFileRulesRedacted(t *testing.T) {
	const secret = "glpat-secretvalue"
	t.Setenv("GITLAB_TOKEN", secret)
	sr := review.SkillResult{Skill: "s", Verdict: review.NoBlockingIssues, Reviewed: []string{secret + ".go"},
		FileRules: []review.FileRule{{Path: secret + ".go", Rule: "repo:" + secret + "/**"}}}
	b, err := Encode(Build(testMeta(), []review.SkillResult{sr}))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte(secret)) {
		t.Errorf("token leaked: %s", b)
	}
	if !bytes.Contains(b, []byte(`"rules":[{"path":"[REDACTED].go","rule":"repo:[REDACTED]/**"}]`)) {
		t.Errorf("files.rules not redacted in place: %s", b)
	}
}

func TestRulesObject(t *testing.T) {
	meta := testMeta()
	meta.Rules = &Rules{BuiltIn: true, Entries: 7}
	b, err := Encode(Build(meta, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(`"rules":{"built_in":true,"entries":7}`)) {
		t.Errorf("no rules object: %s", b)
	}
	b, err = Encode(Build(testMeta(), nil))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte(`"rules"`)) {
		t.Errorf("rules key with no rule support: %s", b)
	}
	b, err = Encode(ErrorResult(meta, CodeUsage, "bad"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte(`"rules"`)) {
		t.Errorf("an error result has a rules key: %s", b)
	}
}

func TestNoFileRulesNoKey(t *testing.T) {
	sr := review.SkillResult{Skill: "s", Verdict: review.NoBlockingIssues, Reviewed: []string{"a.go"}}
	b, err := Encode(Build(testMeta(), []review.SkillResult{sr}))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte(`"rules"`)) {
		t.Errorf("files.rules with no rule: %s", b)
	}
}

// A chosen set of built-ins is named in the rules object; without one the key
// is left out, so an older reader sees the same object.
func TestRulesObjectBuiltInNames(t *testing.T) {
	meta := testMeta()
	meta.Rules = &Rules{BuiltIn: true, Entries: 4, BuiltInNames: []string{"github_workflows", "yaml"}}
	r := Build(meta, nil)
	meta.Rules.BuiltInNames[0] = "changed"
	b, err := Encode(r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(`"rules":{"built_in":true,"entries":4,"built_in_names":["github_workflows","yaml"]}`)) {
		t.Errorf("no built_in_names, or it shares the caller's slice: %s", b)
	}
	meta.Rules = &Rules{BuiltIn: true, Entries: 4}
	if b, err = Encode(Build(meta, nil)); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte(`built_in_names`)) {
		t.Errorf("built_in_names with all built-ins: %s", b)
	}
}
