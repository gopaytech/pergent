package review

import (
	"github.com/gopaytech/pergent/internal/diff"
	"github.com/gopaytech/pergent/internal/skill"
	"github.com/gopaytech/pergent/rule"
)

// ruleFor is the rule a skill's runs use for path: none when the skill has
// rules off (rules: false) or the request has no resolver.
func (r *reviewer) ruleFor(s skill.Skill, path string) (rule.Entry, bool) {
	if s.NoRules {
		return rule.Entry{}, false
	}
	return r.req.Rules.Resolve(path)
}

// ruleExtra counts each file's rule in batch sizing: one copy of a rule per
// batch, however many of its files the batch has. Nil when the skill gets no
// rules.
func (r *reviewer) ruleExtra(s skill.Skill) diff.Extra {
	if s.NoRules || r.req.Rules == nil {
		return nil
	}
	return func(path string) (string, int) {
		e, ok := r.ruleFor(s, path)
		if !ok {
			return "", 0
		}
		return e.Label(), diff.EstimateTokens(e.Rule)
	}
}

// fileSize is a file's tokens in a batch: its diff, plus its rule when it has
// one (withRule).
func (r *reviewer) fileSize(s skill.Skill, f diff.FileDiff) (tokens int, withRule bool) {
	tokens = diff.EstimateTokens(f.Patch)
	if e, ok := r.ruleFor(s, f.Path); ok {
		return tokens + diff.EstimateTokens(e.Rule), true
	}
	return tokens, false
}
