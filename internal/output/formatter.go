package output

import (
	"fmt"
	"strings"

	"github.com/gopaytech/pergent/internal/runner"
)

func FormatComment(results []runner.RunResult, changedFiles []string) string {
	var b strings.Builder

	b.WriteString("<!-- pergent -->\n")
	b.WriteString("## pergent review\n\n")

	// Skills list
	skillNames := make([]string, len(results))
	for i, r := range results {
		skillNames[i] = r.SkillName
	}
	fmt.Fprintf(&b, "**Skills:** %s\n", strings.Join(skillNames, ", "))

	// Files changed
	if len(changedFiles) > 0 {
		quoted := make([]string, len(changedFiles))
		for i, f := range changedFiles {
			quoted[i] = "`" + f + "`"
		}
		fmt.Fprintf(&b, "**Files changed:** %s\n", strings.Join(quoted, ", "))
	}

	b.WriteString("\n")

	// Per-skill sections
	for _, r := range results {
		fmt.Fprintf(&b, "<!-- pergent:%s -->\n", r.SkillName)
		fmt.Fprintf(&b, "### %s\n\n", r.SkillName)

		if r.Truncated {
			b.WriteString("> **Note:** This review was truncated due to timeout.\n\n")
		}

		b.WriteString(r.Output)
		b.WriteString("\n")

		fmt.Fprintf(&b, "<!-- /pergent:%s -->\n\n", r.SkillName)
	}

	return b.String()
}

// ExtractSkillSection returns the content between the per-skill markers
// that FormatComment writes (<!-- pergent:NAME --> ... <!-- /pergent:NAME -->).
// Returns "" when either marker is missing.
func ExtractSkillSection(body string, skillName string) string {
	start := fmt.Sprintf("<!-- pergent:%s -->", skillName)
	end := fmt.Sprintf("<!-- /pergent:%s -->", skillName)

	_, after, ok := strings.Cut(body, start)
	if !ok {
		return ""
	}
	section, _, ok := strings.Cut(after, end)
	if !ok {
		return ""
	}
	return strings.TrimSpace(section)
}
