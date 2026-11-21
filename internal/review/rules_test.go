package review

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gopaytech/pergent/internal/finding"

	"github.com/gopaytech/pergent/internal/diff"
	"github.com/gopaytech/pergent/internal/runner"
	"github.com/gopaytech/pergent/internal/skill"
	"github.com/gopaytech/pergent/rule"
)

// repoRules is a resolver over team entries only (built-ins off), so a test
// does not depend on the built-in list.
func repoRules(entries ...rule.Entry) *rule.Resolver {
	for i := range entries {
		if entries[i].Source == "" {
			entries[i].Source = rule.SourceRepo
		}
	}
	return rule.NewResolver(entries, false)
}

func TestRuleCountsInBatchSizing(t *testing.T) {
	body := strings.Repeat("a", 1600)
	diffText := patch("a/1.go", body) + patch("a/2.go", body)
	ruleText := strings.Repeat("r", 1600)
	for _, tt := range []struct {
		name    string
		noRules bool
		want    int
	}{{"rules on", false, 2}, {"rules off", true, 1}} {
		t.Run(tt.name, func(t *testing.T) {
			f := &fake{respond: func(int, call) runner.RunResult { return completed(answer(t)) }}
			req := newRequest(t, f, diffText, skill.Skill{Name: "s", Body: "Review it.", NoRules: tt.noRules})
			req.ContextLimit = 4000 // per-batch budget of 1,000 tokens
			req.Rules = repoRules(rule.Entry{Path: "**/*.go", Rule: ruleText})
			r := Run(context.Background(), req)[0]
			if r.Batches != tt.want {
				t.Errorf("Batches = %d, want %d", r.Batches, tt.want)
			}
		})
	}
}

func TestRuleOversizeMessage(t *testing.T) {
	p := patch("a.go", strings.Repeat("z", 2400))
	ruleText := strings.Repeat("r", 2400)
	f := &fake{respond: func(int, call) runner.RunResult { return completed(answer(t)) }}
	req := newRequest(t, f, p)
	req.ContextLimit = 4000
	req.Rules = repoRules(rule.Entry{Path: "*.go", Rule: ruleText})
	r := Run(context.Background(), req)[0]
	n := diff.EstimateTokens(p) + diff.EstimateTokens(ruleText)
	want := fmt.Sprintf("Not reviewed: `a.go` — its diff and rule are about %s tokens, larger than the per-batch limit of 1,000.", runner.FormatInt(n))
	if len(r.Failures) != 1 || r.Failures[0].Message != want {
		t.Errorf("Failures = %+v, want %q", r.Failures, want)
	}
	if r.Verdict != Failed || len(f.calls) != 0 {
		t.Errorf("Verdict = %q, calls = %d; want failed and no run", r.Verdict, len(f.calls))
	}
}

// With CI results the batches are packed to a smaller budget, and a file
// over it that fits the full budget gets a batch of its own. Its rule counts
// there too.
func TestRuleCountsInFullBudgetFallback(t *testing.T) {
	p := patch("a.go", strings.Repeat("z", 2800)) // about 710 tokens
	for _, tt := range []struct {
		name    string
		rule    string
		batches int
	}{{"no rule", "", 1}, {"rule", strings.Repeat("r", 1600), 0}} {
		t.Run(tt.name, func(t *testing.T) {
			f := &fake{respond: func(int, call) runner.RunResult { return completed(answer(t)) }}
			req := newRequest(t, f, p)
			req.ContextLimit = 4000
			req.CIResults = strings.Repeat("c", 1600) // 400 tokens: packing budget 600
			if tt.rule != "" {
				req.Rules = repoRules(rule.Entry{Path: "*.go", Rule: tt.rule})
			}
			r := Run(context.Background(), req)[0]
			if r.Batches != tt.batches {
				t.Errorf("Batches = %d, want %d (failures %+v)", r.Batches, tt.batches, r.Failures)
			}
		})
	}
}

func apiAndBaseRules() *rule.Resolver {
	return repoRules(
		rule.Entry{Path: "internal/api/**/*.go", Rule: "API RULE\n"},
		rule.Entry{Path: "**/*.go", Rule: "BASE RULE", Source: rule.DashboardSource("base")},
	)
}

func TestBatchFileRulesSections(t *testing.T) {
	f := &fake{respond: func(int, call) runner.RunResult { return completed(answer(t)) }}
	diffText := patch("internal/api/user.go", "x") + patch("internal/api/order.go", "x") + patch("internal/store/cache.go", "x") + patch("README.md", "x")
	req := newRequest(t, f, diffText)
	req.Rules = apiAndBaseRules()
	req.Previous = map[string][]finding.Finding{"s": {}}
	Run(context.Background(), req)
	if len(f.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(f.calls))
	}
	c := f.calls[0]
	got, ok := attached(c, rulesFile)
	const want = "## Rule: repo · internal/api/**/*.go\n" +
		"Files: \"internal/api/order.go\", \"internal/api/user.go\"\n\nAPI RULE\n\n" +
		"## Rule: dashboard:base · **/*.go\n" +
		"Files: \"internal/store/cache.go\"\n\nBASE RULE\n"
	if !ok || got != want {
		t.Errorf("%s =\n%q\nwant\n%q", rulesFile, got, want)
	}
	if names := baseNames(c); !slices.Equal(names[:3], []string{"diff.patch", "changed-files.md", rulesFile}) {
		t.Errorf("files = %v, want file-rules.md right after changed-files.md", names)
	}
	msg := c.opts.Message
	if i, j := strings.Index(msg, rulesMessage), strings.Index(msg, previousMessage); i < 0 || j < 0 || i > j {
		t.Errorf("Message = %q, want the rules sentence before the previous-findings sentence", msg)
	}
	assertRemoved(t, allFiles(f.calls))
}

func TestBatchNoRuleNoFile(t *testing.T) {
	run := func(rules *rule.Resolver) call {
		f := &fake{respond: func(int, call) runner.RunResult { return completed(answer(t)) }}
		req := newRequest(t, f, patch("README.md", "x"))
		req.Rules = rules
		Run(context.Background(), req)
		return f.calls[0]
	}
	with, without := run(apiAndBaseRules()), run(nil)
	if with.opts.Message != without.opts.Message || !slices.Equal(baseNames(with), baseNames(without)) {
		t.Errorf("a batch with no rule differs:\n%q %v\n%q %v", with.opts.Message, baseNames(with), without.opts.Message, baseNames(without))
	}
}

func TestSkillNoRulesGetsNoFile(t *testing.T) {
	f := &fake{respond: func(int, call) runner.RunResult { return completed(answer(t)) }}
	req := newRequest(t, f, patch("a.go", "x"), skill.Skill{Name: "s", Body: "Review it.", NoRules: true})
	req.Rules = apiAndBaseRules()
	r := Run(context.Background(), req)[0]
	if _, ok := attached(f.calls[0], rulesFile); ok || strings.Contains(f.calls[0].opts.Message, rulesMessage) {
		t.Error("a skill with rules: false got file-rules.md")
	}
	if len(r.FileRules) != 0 {
		t.Errorf("FileRules = %+v, want none", r.FileRules)
	}
}

func TestRulesWriteFailureNotReviewed(t *testing.T) {
	old := writeRules
	t.Cleanup(func() { writeRules = old })
	writeRules = func(string, []byte, os.FileMode) error { return errors.New("disk full") }
	f := &fake{respond: func(int, call) runner.RunResult { return completed(answer(t)) }}
	req := newRequest(t, f, patch("a.go", "x"))
	req.Rules = apiAndBaseRules()
	r := Run(context.Background(), req)[0]
	want := BatchFailure{Files: []string{"a.go"}, Message: "Not reviewed: could not write the file rules: disk full"}
	if len(r.Failures) != 1 || r.Failures[0].Message != want.Message || !slices.Equal(r.Failures[0].Files, want.Files) {
		t.Errorf("Failures = %+v, want %+v", r.Failures, want)
	}
	if r.Verdict != Failed || len(f.calls) != 0 {
		t.Errorf("Verdict = %q, calls = %d; want failed and no run", r.Verdict, len(f.calls))
	}
}

func TestFileRulesQuotesPaths(t *testing.T) {
	e := rule.Entry{Path: "**", Rule: "R", Source: rule.SourceRepo}
	text, labels := fileRules([]string{"a\nb.go", "c`d.go"}, func(string) (rule.Entry, bool) { return e, true })
	want := "## Rule: repo · **\nFiles: \"a\\nb.go\", \"c`d.go\"\n\nR\n"
	if text != want {
		t.Errorf("fileRules =\n%q\nwant\n%q", text, want)
	}
	if labels["a\nb.go"] != "repo:**" || len(labels) != 2 {
		t.Errorf("labels = %v", labels)
	}
	if text, labels := fileRules([]string{"x.md"}, func(string) (rule.Entry, bool) { return rule.Entry{}, false }); text != "" || len(labels) != 0 {
		t.Errorf("no rule gave %q %v", text, labels)
	}
}

func TestSkillResultFileRules(t *testing.T) {
	f := &fake{}
	f.respond = func(_ int, c call) runner.RunResult {
		if strings.Contains(c.contents[0], "b/y.go") {
			return runner.RunResult{Status: runner.StatusFailed, Cause: &runner.Cause{Kind: runner.CauseNoAnswer}}
		}
		return completed(answer(t))
	}
	req := newRequest(t, f, twoBatchDiff())
	req.ContextLimit = twoBatchContextLimit
	req.Rules = repoRules(rule.Entry{Path: "**/*.go", Rule: "check"})
	r := Run(context.Background(), req)[0]
	want := []FileRule{{Path: "a/x.go", Rule: "repo:**/*.go"}}
	if !slices.Equal(r.FileRules, want) {
		t.Errorf("FileRules = %+v, want %+v", r.FileRules, want)
	}
}

func TestCrossBatchGetsNoRules(t *testing.T) {
	f := &fake{respond: func(int, call) runner.RunResult { return completed(answer(t)) }}
	req, _ := crossBatchRequest(t, f, true)
	req.Rules = repoRules(rule.Entry{Path: "**/*.go", Rule: "check"})
	Run(context.Background(), req)
	cross, batches := 0, 0
	for _, c := range f.calls {
		_, has := attached(c, rulesFile)
		switch {
		case isCrossBatchCall(c):
			cross++
			if has || strings.Contains(c.opts.Message, rulesMessage) {
				t.Error("the cross-batch run got the rules")
			}
		case has:
			batches++
		}
	}
	if cross != 1 || batches != 3 {
		t.Errorf("cross-batch runs = %d, batch runs with rules = %d; want 1 and 3", cross, batches)
	}
}

func TestImpactGetsNoRules(t *testing.T) {
	f := impactFake(t, answer(t), nil)
	req := impactRequest(t, f, patch("render/r.go", "x"), []LinkedRepo{{Name: "billing", Path: billingClone(t), CanBlock: true}})
	req.Rules = repoRules(rule.Entry{Path: "**/*.go", Rule: "check"})
	rs := Run(context.Background(), req)
	checks, _, _ := impactCalls(f)
	if len(checks) == 0 {
		t.Fatal("the impact check did not run")
	}
	for _, c := range checks {
		if _, has := attached(c, rulesFile); has || strings.Contains(c.opts.Message, rulesMessage) {
			t.Error("the impact check got the rules")
		}
	}
	if len(rs[len(rs)-1].FileRules) != 0 {
		t.Error("the impact result has file rules")
	}
}

func TestRepairGetsNoRules(t *testing.T) {
	f := &fake{}
	f.respond = func(n int, _ call) runner.RunResult {
		if n == 1 {
			return completed("not json")
		}
		return completed(answer(t))
	}
	req := newRequest(t, f, patch("a.go", "x"))
	req.Rules = repoRules(rule.Entry{Path: "*.go", Rule: "check"})
	Run(context.Background(), req)
	if len(f.calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(f.calls))
	}
	if names := baseNames(f.calls[1]); !slices.Equal(names, []string{"review-answer.md"}) {
		t.Errorf("repair files = %v, want only review-answer.md", names)
	}
}

// A rule only adds checks: the same answer gives the same verdict and
// severities with and without rules.
func TestRulesNeverChangeVerdict(t *testing.T) {
	run := func(rules *rule.Resolver) SkillResult {
		f := &fake{}
		f.respond = func(_ int, c call) runner.RunResult {
			if isVerifyRun(c) {
				return completed(verifyAnswer(t))
			}
			return completed(answer(t, blockingTwoAndSuggestion()...))
		}
		req := newRequest(t, f, patch("a/x.go", "x"))
		req.Budget = 5 * time.Minute
		req.Rules = rules
		return Run(context.Background(), req)[0]
	}
	with, without := run(repoRules(rule.Entry{Path: "**", Rule: "Nothing here is blocking."})), run(nil)
	if with.Verdict != without.Verdict || len(with.Findings) != len(without.Findings) {
		t.Fatalf("with rules %q %d findings, without %q %d", with.Verdict, len(with.Findings), without.Verdict, len(without.Findings))
	}
	for i := range with.Findings {
		if with.Findings[i].Severity != without.Findings[i].Severity {
			t.Errorf("finding %d: severity %q with rules, %q without", i, with.Findings[i].Severity, without.Findings[i].Severity)
		}
	}
}
