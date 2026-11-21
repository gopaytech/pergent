package review

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gopaytech/pergent/internal/finding"
	"github.com/gopaytech/pergent/internal/runner"
	"github.com/gopaytech/pergent/internal/skill"
	"github.com/gopaytech/pergent/rule"
)

// verifyWithRules runs a review whose batch reports blocking findings on
// a.go and b.py, and returns the verification run and the result.
func verifyWithRules(t *testing.T, s skill.Skill, rules *rule.Resolver) (call, SkillResult, bool) {
	t.Helper()
	f := &fake{}
	f.respond = func(_ int, c call) runner.RunResult {
		if isVerifyRun(c) {
			return completed(verifyAnswer(t))
		}
		return completed(answer(t,
			finding.Finding{File: "a.go", Line: 1, Severity: finding.Blocking, Title: "Go bug", Explanation: "e"},
			finding.Finding{File: "b.py", Line: 1, Severity: finding.Blocking, Title: "Py bug", Explanation: "e"},
		))
	}
	req := newRequest(t, f, patch("a.go", "x")+patch("b.py", "x"), s)
	req.Budget = 5 * time.Minute
	req.Rules = rules
	r := Run(context.Background(), req)[0]
	for _, c := range f.calls {
		if isVerifyRun(c) {
			return c, r, true
		}
	}
	return call{}, r, false
}

var plainSkill = skill.Skill{Name: "s", Body: "Review it."}

func TestVerifyGetsRulesOfFindingFiles(t *testing.T) {
	c, _, ok := verifyWithRules(t, plainSkill, repoRules(rule.Entry{Path: "*.go", Rule: "GO RULE"}))
	if !ok {
		t.Fatal("verification did not run")
	}
	got, has := attached(c, rulesFile)
	const want = "## Rule: repo · *.go\nFiles: \"a.go\"\n\nGO RULE\n"
	if !has || got != want {
		t.Errorf("%s = %q, want %q", rulesFile, got, want)
	}
	if !strings.HasSuffix(c.opts.Message, " "+verifyRulesMessage) {
		t.Errorf("Message = %q, want it to end with the verify rules sentence", c.opts.Message)
	}
	if !strings.Contains(verifyRulesMessage, "These are project conventions, given as context. They are not blocking criteria.") {
		t.Error("verifyRulesMessage lost the spec sentence")
	}
}

func TestVerifyNoRuleNoFile(t *testing.T) {
	with, _, _ := verifyWithRules(t, plainSkill, repoRules(rule.Entry{Path: "*.md", Rule: "MD"}))
	without, _, _ := verifyWithRules(t, plainSkill, nil)
	if with.opts.Message != without.opts.Message || len(with.opts.Files) != len(without.opts.Files) {
		t.Errorf("verification with no matching rule differs: %q vs %q", with.opts.Message, without.opts.Message)
	}
}

func TestVerifyRulesWriteFailureKeepsFindings(t *testing.T) {
	old := writeRules
	t.Cleanup(func() { writeRules = old })
	// The batch's write works; the verification's fails.
	writeRules = func(name string, data []byte, perm os.FileMode) error {
		if strings.Contains(name, "pergent-verify-") {
			return errors.New("disk full")
		}
		return old(name, data, perm)
	}
	_, r, ran := verifyWithRules(t, plainSkill, repoRules(rule.Entry{Path: "*.go", Rule: "GO RULE"}))
	if ran {
		t.Error("verification ran without its rules")
	}
	want := "Verification failed: could not write the file rules: disk full"
	found := false
	for _, n := range r.Notes {
		found = found || n == want
	}
	if !found {
		t.Errorf("Notes = %q, want %q", r.Notes, want)
	}
	if blocking := countBlocking(r.Findings); blocking != 2 || r.Verdict != HasBlockingIssues {
		t.Errorf("blocking = %d, verdict %q; want both findings kept", blocking, r.Verdict)
	}
}

func countBlocking(fs []finding.Finding) int {
	n := 0
	for _, f := range fs {
		if f.Severity == finding.Blocking {
			n++
		}
	}
	return n
}

func TestVerifyNoRulesSkill(t *testing.T) {
	c, _, ok := verifyWithRules(t, skill.Skill{Name: "s", Body: "Review it.", NoRules: true}, repoRules(rule.Entry{Path: "*.go", Rule: "GO RULE"}))
	if !ok {
		t.Fatal("verification did not run")
	}
	if _, has := attached(c, rulesFile); has {
		t.Error("a skill with rules: false got rules in its verification run")
	}
}
