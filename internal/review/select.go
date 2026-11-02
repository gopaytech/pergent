package review

import (
	"github.com/gopaytech/pergent/internal/diff"
	"github.com/gopaytech/pergent/internal/skill"
	"github.com/gopaytech/pergent/scope"
)

// Select splits files into the ones skill s reviews and the ones it skips,
// with the rule that skipped each. The rules and their order are
// scope.Decide's, applied to each file's default exclusion from c.
func Select(s skill.Skill, files []diff.FileDiff, c diff.Classifier) (in []diff.FileDiff, skipped []diff.Skip) {
	for _, f := range files {
		rule, optIn := c.Exclusion(f)
		if rule = scope.Decide(s.Paths, s.Exclude, f.Path, rule, optIn); rule != "" {
			skipped = append(skipped, diff.Skip{Path: f.Path, Rule: rule})
			continue
		}
		in = append(in, f)
	}
	return in, skipped
}
