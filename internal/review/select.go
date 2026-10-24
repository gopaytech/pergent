package review

import (
	"github.com/gopaytech/pergent/internal/diff"
	"github.com/gopaytech/pergent/internal/skill"
)

// Select splits files into the ones skill s reviews and the ones it skips,
// with the rule that skipped each. The rules apply in order:
//   - a default exclusion that paths can't override (deleted, binary, ...);
//   - "out of scope" when s has paths and none matches;
//   - "excluded by skill" when one of s's exclude patterns matches;
//   - a default exclusion that paths can override (lockfile, vendored,
//     generated), unless one of s's paths matches the file.
func Select(s skill.Skill, files []diff.FileDiff, c diff.Classifier) (in []diff.FileDiff, skipped []diff.Skip) {
	for _, f := range files {
		rule, optIn := c.Exclusion(f)
		if rule != "" && !optIn {
			skipped = append(skipped, diff.Skip{Path: f.Path, Rule: rule})
			continue
		}
		inPaths := matchAny(s.Paths, f.Path)
		if len(s.Paths) > 0 && !inPaths {
			skipped = append(skipped, diff.Skip{Path: f.Path, Rule: "out of scope"})
			continue
		}
		if matchAny(s.Exclude, f.Path) {
			skipped = append(skipped, diff.Skip{Path: f.Path, Rule: "excluded by skill"})
			continue
		}
		if rule != "" && !inPaths {
			skipped = append(skipped, diff.Skip{Path: f.Path, Rule: rule})
			continue
		}
		in = append(in, f)
	}
	return in, skipped
}

func matchAny(patterns []string, p string) bool {
	for _, pattern := range patterns {
		if diff.Match(pattern, p) {
			return true
		}
	}
	return false
}
