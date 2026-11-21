package main

import (
	"slices"
	"testing"

	"github.com/gopaytech/pergent/internal/result"
)

// TestRunAgentPanelAcceptsRules: --rules is a trusted input of the merge
// step. The result reports the rule list, each judge run gets the rules of
// the files of the findings it judges, and a member's files.rules is kept.
func TestRunAgentPanelAcceptsRules(t *testing.T) {
	runs := &panelRuns{t: t, judge: map[string]string{
		panelModelB: checkAnswer(t, map[string]any{"id": "a1", "real": true, "reason": "yes", "same_as": "b1"}),
		panelModelA: checkAnswer(t, map[string]any{"id": "b1", "real": true, "reason": "yes", "same_as": "a1"}),
	}}
	sa := reviewSkill(panelFindA)
	sa.Files.Rules = []result.FileRule{{Path: "a.go", Rule: "repo:*.go"}}
	a := member("a.json", memberCompleted(sa))
	b := member("b.json", memberCompleted(reviewSkill(panelFindB)))

	code, srv, stderr := runPanelAgent(t, a, b, runs, "--rules", rulesFileOutside(t, twoRules))
	if code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, stderr)
	}
	r := srv.only(t)
	if r.Rules == nil || !r.Rules.BuiltIn || r.Rules.Entries != 2+len(builtinNames()) {
		t.Errorf("rules = %+v, want built-ins on and 2 team entries", r.Rules)
	}
	if len(runs.calls) != 2 {
		t.Fatalf("%d runs, want 2 judge runs", len(runs.calls))
	}
	for _, c := range runs.calls {
		if !slices.Contains(c.files, "file-rules.md") {
			t.Errorf("judge run on %s: files %v, want file-rules.md", c.model, c.files)
		}
	}
	s, ok := skillNamed(r, "review")
	if !ok {
		t.Fatalf("no review skill: %+v", r.Skills)
	}
	if !slices.Equal(s.Files.Rules, sa.Files.Rules) {
		t.Errorf("files.rules = %+v, want member A's", s.Files.Rules)
	}
}

// TestRunAgentPanelRulesUnsetWithoutFlag: a panel result reports the rule
// list even with no --rules, as a review does.
func TestRunAgentPanelRulesUnsetWithoutFlag(t *testing.T) {
	runs := &panelRuns{t: t, judge: map[string]string{
		panelModelB: checkAnswer(t), panelModelA: checkAnswer(t),
	}}
	a := member("a.json", memberCompleted(reviewSkill(panelFindA)))
	b := member("b.json", memberCompleted(reviewSkill(panelFindB)))
	code, srv, stderr := runPanelAgent(t, a, b, runs)
	if code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, stderr)
	}
	if r := srv.only(t); r.Rules == nil || r.Rules.Entries != len(builtinNames()) {
		t.Errorf("rules = %+v, want the built-ins only", r.Rules)
	}
}
