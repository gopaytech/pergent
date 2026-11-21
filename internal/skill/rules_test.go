package skill

import "testing"

func TestSkillRulesDefaultOn(t *testing.T) {
	s, err := parse([]byte("---\nname: x\n---\nReview it."), "x")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if s.NoRules {
		t.Error("a skill with no rules field has rules off")
	}
	p, err := LoadPreset("code-review")
	if err != nil || p.NoRules {
		t.Errorf("preset code-review: NoRules = %v, err = %v", p.NoRules, err)
	}
}

func TestSkillRulesFalse(t *testing.T) {
	for value, want := range map[string]bool{"false": true, "true": false, "False": true, "TRUE": false} {
		s, err := parse([]byte("---\nname: x\nrules: "+value+"\n---\nReview it."), "x")
		if err != nil {
			t.Fatalf("rules: %s: %v", value, err)
		}
		if s.NoRules != want {
			t.Errorf("rules: %s: NoRules = %v, want %v", value, s.NoRules, want)
		}
	}
}

func TestSkillRulesNotBool(t *testing.T) {
	for _, value := range []string{"yes", `"false"`, "1", "[a]", "null"} {
		_, err := parse([]byte("---\nname: x\nrules: "+value+"\n---\nReview it."), "x")
		if err == nil || err.Error() != "rules must be true or false" {
			t.Errorf("rules: %s: err = %v, want %q", value, err, "rules must be true or false")
		}
	}
}
