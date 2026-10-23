package output

import (
	"fmt"
	"strings"

	"github.com/gopaytech/pergent/internal/redact"
	"github.com/gopaytech/pergent/internal/runner"
)

// failedPrefix starts the section of a skill whose run failed.
const failedPrefix = "**Review failed:**"

// FormatComment renders the results as one markdown comment. Credentials are
// redacted from the whole comment, including the reviews themselves.
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

		if r.Status == runner.StatusCompleted {
			b.WriteString(r.Output)
			b.WriteString("\n")
		} else {
			writeFailure(&b, r.Cause)
		}

		fmt.Fprintf(&b, "<!-- /pergent:%s -->\n\n", r.SkillName)
	}

	return redact.String(b.String())
}

// FailureText renders a failed run's cause: a one-line reason, then the end of
// opencode's stderr when the process itself failed.
func FailureText(cause *runner.Cause) string {
	var b strings.Builder
	writeFailure(&b, cause)
	return redact.String(strings.TrimSpace(b.String()))
}

func writeFailure(b *strings.Builder, cause *runner.Cause) {
	if cause == nil {
		fmt.Fprintf(b, "%s The run ended without a review and without a reported cause.\n", failedPrefix)
		return
	}
	fmt.Fprintf(b, "%s %s\n", failedPrefix, cause.Message())
	if len(cause.StderrLines) > 0 {
		text := strings.Join(cause.StderrLines, "\n")
		fence := "```"
		for strings.Contains(text, fence) {
			fence += "`"
		}
		fmt.Fprintf(b, "\nLast lines of opencode's stderr:\n\n%stext\n%s\n%s\n", fence, text, fence)
	}
}

// IsFailedSection reports whether a skill section from a previous comment is
// a failure report rather than a review.
func IsFailedSection(section string) bool {
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return strings.HasPrefix(line, failedPrefix)
	}
	return false
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
