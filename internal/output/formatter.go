package output

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/gopaytech/pergent/internal/finding"
	"github.com/gopaytech/pergent/internal/redact"
	"github.com/gopaytech/pergent/internal/review"
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
	writeFailureLead(b, "", cause, false)
}

// writeFailureLead writes a failure: the prefix, then lead (context to show
// before the cause), then the cause and the tail of opencode's stderr. The
// text is collapsed to one line. In markdown mode it also cannot open an HTML
// comment, so a model's excerpt can't hide or forge the findings block.
func writeFailureLead(b *strings.Builder, lead string, cause *runner.Cause, markdown bool) {
	lead = oneLine(lead)
	var line string
	switch {
	case cause != nil && lead != "":
		line = lead + " " + oneLine(cause.Message())
	case cause != nil:
		line = oneLine(cause.Message())
	case lead != "":
		line = lead
	default:
		line = "The run ended without a review and without a reported cause."
	}
	if markdown {
		line = neutralizeComments(redact.String(line))
	}
	fmt.Fprintf(b, "%s %s\n", failedPrefix, line)
	if cause != nil && len(cause.StderrLines) > 0 {
		text := strings.Join(cause.StderrLines, "\n")
		fence := "```"
		for strings.Contains(text, fence) {
			fence += "`"
		}
		fmt.Fprintf(b, "\nLast lines of opencode's stderr:\n\n%stext\n%s\n%s\n", fence, text, fence)
	}
}

// oneLine collapses all whitespace, newlines included, to single spaces.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// neutralizeComments keeps text from opening an HTML comment.
func neutralizeComments(s string) string {
	return strings.ReplaceAll(s, "<!--", "&lt;!--")
}

// BatchFailureText renders one failed batch as redacted text for the CI log:
// the failure line, then the end of opencode's stderr when there is one.
func BatchFailureText(f review.BatchFailure) string {
	var b strings.Builder
	writeFailureLead(&b, f.Message, f.Cause, false)
	return redact.String(strings.TrimSpace(b.String()))
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

// findingsMarker starts the hidden line that carries a review's findings to
// the next run.
const findingsMarker = "<!-- pergent:findings "

// findingsLineRe matches the hidden line. The real one is the last line of
// the comment, so ParsePreviousFindings uses the last match: text quoted from
// the reviewed code can't replace it.
var findingsLineRe = regexp.MustCompile(`(?m)^<!-- pergent:findings (\S+) -->[ \t]*$`)

// findingsBlockVersion is the format version of the hidden findings block.
const findingsBlockVersion = 1

type findingsBlock struct {
	Version int                          `json:"version"`
	Skills  map[string][]finding.Finding `json:"skills"`
}

// FormatReview renders the results as one markdown comment: for each skill a
// verdict line, the blocking findings in full, then collapsed suggestions and
// skipped files. A hidden last line carries the findings for the next run.
// Credentials are redacted from the whole comment and from the hidden block.
func FormatReview(results []review.SkillResult, changedFiles []string) string {
	var b strings.Builder

	b.WriteString("<!-- pergent -->\n")
	b.WriteString("## pergent review\n\n")

	if len(changedFiles) > 0 {
		fmt.Fprintf(&b, "<details><summary>Files changed (%d)</summary>\n\n", len(changedFiles))
		for _, f := range changedFiles {
			fmt.Fprintf(&b, "- %s\n", codeSpan(f))
		}
		b.WriteString("\n</details>\n\n")
	}

	block := findingsBlock{Version: findingsBlockVersion, Skills: make(map[string][]finding.Finding, len(results))}
	for _, r := range results {
		fs := redactFindings(r.Findings)
		block.Skills[r.Skill] = fs
		writeSkill(&b, r, fs)
	}

	out := redact.String(b.String())
	if data, err := json.Marshal(block); err == nil {
		out += findingsMarker + base64.StdEncoding.EncodeToString(data) + " -->\n"
	}
	return out
}

func writeSkill(b *strings.Builder, r review.SkillResult, fs []finding.Finding) {
	fmt.Fprintf(b, "<!-- pergent:%s -->\n", r.Skill)
	fmt.Fprintf(b, "### %s\n\n", r.Skill)

	var blocking, suggestions []finding.Finding
	for _, f := range fs {
		if f.Severity == finding.Suggestion {
			suggestions = append(suggestions, f)
		} else {
			blocking = append(blocking, f)
		}
	}

	switch {
	case len(r.Failures) > 0:
		for _, f := range r.Failures {
			b.WriteString("\u26a0\ufe0f ")
			if r.Batches > 1 && len(f.Files) > 0 {
				quoted := make([]string, len(f.Files))
				for i, file := range f.Files {
					quoted[i] = codeSpan(file)
				}
				fmt.Fprintf(b, "Files %s: ", strings.Join(quoted, ", "))
			}
			writeFailureLead(b, f.Message, f.Cause, true)
			b.WriteString("\n")
		}
	case r.Verdict == review.Failed:
		b.WriteString("\u26a0\ufe0f ")
		writeFailureLead(b, "", nil, true)
		b.WriteString("\n")
	}

	switch {
	case len(blocking) == 1:
		b.WriteString("\u274c **1 blocking issue**\n\n")
	case len(blocking) > 1:
		fmt.Fprintf(b, "\u274c **%d blocking issues**\n\n", len(blocking))
	case r.Verdict == review.NoBlockingIssues:
		b.WriteString("\u2705 **No blocking issues**\n\n")
	}

	for _, f := range blocking {
		writeFinding(b, f)
	}
	if len(suggestions) > 0 {
		fmt.Fprintf(b, "<details><summary>Suggestions (%d)</summary>\n\n", len(suggestions))
		for _, f := range suggestions {
			writeFinding(b, f)
		}
		b.WriteString("</details>\n\n")
	}
	if len(r.Skipped) > 0 {
		fmt.Fprintf(b, "<details><summary>Skipped files (%d)</summary>\n\n", len(r.Skipped))
		for _, s := range r.Skipped {
			fmt.Fprintf(b, "- %s \u2014 %s\n", codeSpan(s.Path), neutralizeComments(oneLine(s.Rule)))
		}
		b.WriteString("\n</details>\n\n")
	}
	for _, n := range r.Notes {
		fmt.Fprintf(b, "> %s\n", neutralizeComments(oneLine(n)))
	}
	if len(r.Notes) > 0 {
		b.WriteString("\n")
	}

	fmt.Fprintf(b, "<!-- /pergent:%s -->\n\n", r.Skill)
}

func writeFinding(b *strings.Builder, f finding.Finding) {
	where := f.File
	if f.Line > 0 {
		where = fmt.Sprintf("%s:%d", f.File, f.Line)
	}
	fmt.Fprintf(b, "**%s** \u2014 **%s**\n\n", codeSpan(where), neutralizeComments(oneLine(f.Title)))
	if f.Explanation != "" {
		fmt.Fprintf(b, "%s\n\n", neutralizeComments(strings.TrimSpace(f.Explanation)))
	}
	if f.SuggestedFix != "" {
		fmt.Fprintf(b, "_Suggested fix:_ %s\n\n", neutralizeComments(strings.TrimSpace(f.SuggestedFix)))
	}
}

// codeSpan wraps s in an inline code span whose backtick fence is longer than
// any backtick run inside s.
func codeSpan(s string) string {
	s = oneLine(s)
	longest, run := 0, 0
	for _, c := range s {
		if c == '`' {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", longest+1)
	if longest > 0 {
		return fence + " " + s + " " + fence
	}
	return fence + s + fence
}

// redactFindings returns a copy of fs with credentials removed from every
// text field, so neither the comment nor the hidden block carries them.
func redactFindings(fs []finding.Finding) []finding.Finding {
	out := make([]finding.Finding, len(fs))
	for i, f := range fs {
		f.File = redact.String(f.File)
		f.Title = redact.String(f.Title)
		f.Explanation = redact.String(f.Explanation)
		f.SuggestedFix = redact.String(f.SuggestedFix)
		f.Fingerprint = redact.String(f.Fingerprint)
		out[i] = f
	}
	return out
}

// ParsePreviousFindings reads the findings per skill from the hidden block of
// a previous pergent comment. It returns nil, nil when the comment has no
// block (for example, one posted by an older pergent), and an error when the
// block is there but cannot be read.
func ParsePreviousFindings(body string) (map[string][]finding.Finding, error) {
	matches := findingsLineRe.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil, nil
	}
	raw, err := base64.StdEncoding.DecodeString(matches[len(matches)-1][1])
	if err != nil {
		return nil, fmt.Errorf("decoding the findings block: %w", err)
	}
	var block findingsBlock
	if err := json.Unmarshal(raw, &block); err != nil {
		return nil, fmt.Errorf("parsing the findings block: %w", err)
	}
	if block.Version != findingsBlockVersion {
		return nil, fmt.Errorf("unsupported findings block version %d", block.Version)
	}
	return block.Skills, nil
}
