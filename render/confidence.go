package render

import (
	"fmt"
	"strings"
)

// Score is a skill's confidence after the caps. Value is 1 to 5, or 0 when
// the skill was not scored. Capped says why Value is below the model's score,
// and is empty when it is not capped.
type Score struct {
	Value  int
	Capped string
}

// The highest score a skill keeps while it has a blocking finding, and while
// a changed file went unreviewed.
const (
	blockingCap = 3
	skippedCap  = 4
)

// unreviewedRules are the skips that leave a changed file unreviewed.
var unreviewedRules = map[string]bool{
	"deleted": true, "binary": true, "vendored": true, "lockfile": true, "generated": true,
}

// SkillScore returns the skill's score: the model's, lowered by the first cap
// that applies. A skill whose verdict is neither no blocking issues nor has
// blocking issues (a failed one, say), and one the model did not score, has
// none. Findings in s.Dismissed don't count, so ApplyDismissals first restores
// the model's score.
func SkillScore(s Skill) Score {
	if (s.Verdict != VerdictNoBlockingIssues && s.Verdict != VerdictHasBlockingIssues) || s.Confidence < 1 || s.Confidence > 5 {
		return Score{}
	}
	blocking, _ := countFindings(s.Findings)
	if blocking > 0 && s.Confidence > blockingCap {
		return Score{blockingCap, plural(blocking, "blocking finding")}
	}
	unreviewed := 0
	for _, sk := range s.Skipped {
		if unreviewedRules[sk.Rule] {
			unreviewed++
		}
	}
	if unreviewed > 0 && s.Confidence > skippedCap {
		return Score{skippedCap, plural(unreviewed, "skipped file") + " not reviewed"}
	}
	return Score{s.Confidence, ""}
}

// ReviewScore returns the lowest scored skill's score and its index, the
// first on a tie. It returns (Score{}, -1) when no skill is scored.
func ReviewScore(skills []Skill) (Score, int) {
	best, at := Score{}, -1
	for i, s := range skills {
		if sc := SkillScore(s); sc.Value > 0 && (at < 0 || sc.Value < best.Value) {
			best, at = sc, i
		}
	}
	return best, at
}

// countFindings counts findings by severity.
func countFindings(fs []Finding) (blocking, suggestions int) {
	for _, f := range fs {
		if f.Severity == Suggestion {
			suggestions++
		} else {
			blocking++
		}
	}
	return blocking, suggestions
}

// anyModelScore reports whether any skill carries a model score; without one
// the comment does not mention confidence.
func anyModelScore(skills []Skill) bool {
	for _, s := range skills {
		if hasModelScore(s) {
			return true
		}
	}
	return false
}

func hasModelScore(s Skill) bool { return s.Confidence >= 1 && s.Confidence <= 5 }

// writeSkillsTable writes one row per skill; the Confidence column only when
// some skill carries a model score.
func writeSkillsTable(b *strings.Builder, skills []Skill) {
	scored := anyModelScore(skills)
	if scored {
		b.WriteString("| Skill | Verdict | Confidence | Findings |\n|---|---|---|---|\n")
	} else {
		b.WriteString("| Skill | Verdict | Findings |\n|---|---|---|\n")
	}
	for _, s := range skills {
		fmt.Fprintf(b, "| %s | %s |", ownCell(codeSpan(s.Name)), verdictCell(s.Verdict))
		if scored {
			fmt.Fprintf(b, " %s |", confidenceCell(SkillScore(s)))
		}
		fmt.Fprintf(b, " %s |\n", findingsCell(s.Findings))
	}
	b.WriteString("\n")
}

func verdictCell(verdict string) string {
	switch verdict {
	case VerdictNoBlockingIssues:
		return "✅ No blocking issues"
	case VerdictHasBlockingIssues:
		return "❌ Has blocking issues"
	}
	return "⚠️ Failed"
}

func confidenceCell(sc Score) string {
	switch {
	case sc.Value == 0:
		return "not scored"
	case sc.Capped != "":
		return fmt.Sprintf("%d/5 (capped)", sc.Value)
	}
	return fmt.Sprintf("%d/5", sc.Value)
}

func findingsCell(fs []Finding) string {
	blocking, suggestions := countFindings(fs)
	var parts []string
	if blocking > 0 {
		parts = append(parts, fmt.Sprintf("%d blocking", blocking))
	}
	if suggestions > 0 {
		parts = append(parts, plural(suggestions, "suggestion"))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, " · ")
}
