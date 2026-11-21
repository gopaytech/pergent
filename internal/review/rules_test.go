package review

import (
	"context"
	"fmt"
	"strings"
	"testing"

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
