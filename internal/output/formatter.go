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

// failedPrefix starts the report of a failed batch.
const failedPrefix = "**Review failed:**"

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

// layoutTagRe matches the HTML tags the comment layout is built from.
var layoutTagRe = regexp.MustCompile(`(?i)<(/?(?:details|summary)\b)`)

// neutralizeComments keeps model text from opening an HTML comment or closing
// or opening one of the comment's collapsible blocks.
func neutralizeComments(s string) string {
	s = strings.ReplaceAll(s, "<!--", "&lt;!--")
	return layoutTagRe.ReplaceAllString(s, "&lt;$1")
}

// fenceLine parses a line as a code fence: its marker character, the length
// of the marker run and whether anything but whitespace follows it.
func fenceLine(line string) (ch byte, n int, bare bool) {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 || len(trimmed) < 3 || (trimmed[0] != '`' && trimmed[0] != '~') {
		return 0, 0, false
	}
	ch = trimmed[0]
	for n < len(trimmed) && trimmed[n] == ch {
		n++
	}
	if n < 3 {
		return 0, 0, false
	}
	rest := trimmed[n:]
	if ch == '`' && strings.Contains(rest, "`") {
		return 0, 0, false
	}
	return ch, n, strings.TrimSpace(rest) == ""
}

// closeFences appends the closing fence for a code fence the text leaves
// open, so nothing after the text is swallowed by it.
func closeFences(s string) string {
	var open byte
	var openLen int
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimRight(line, "\r")
		ch, n, bare := fenceLine(line)
		switch {
		case ch == 0:
		case open == 0:
			open, openLen = ch, n
		case ch == open && n >= openLen && bare:
			open = 0
		}
	}
	if open == 0 {
		return s
	}
	return strings.TrimRight(s, "\n") + "\n" + strings.Repeat(string(open), openLen)
}

// modelText prepares multi-line model text for the comment.
func modelText(s string) string {
	return closeFences(neutralizeComments(strings.TrimSpace(s)))
}

// BatchFailureText renders one failed batch as redacted text for the CI log:
// the failure line, then the end of opencode's stderr when there is one.
func BatchFailureText(f review.BatchFailure) string {
	var b strings.Builder
	writeFailureLead(&b, f.Message, f.Cause, false)
	return redact.String(strings.TrimSpace(b.String()))
}

// findingsMarker starts the hidden line that carries a review's findings to
// the next run.
const findingsMarker = "<!-- pergent:findings "

// findingsLineRe matches the hidden line. The real one is the last line of
// the comment, so ParsePreviousFindings uses the last match: text quoted from
// the reviewed code can't replace it.
var findingsLineRe = regexp.MustCompile(`(?m)^<!-- pergent:findings (\S+) -->[ \t\r]*$`)

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
			// Name the files when the reader can't tell which ones failed: the
			// skill had several batches or there are several failures.
			if len(f.Files) > 0 && (r.Batches > 1 || len(r.Failures) > 1) {
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
		fmt.Fprintf(b, "%s\n\n", modelText(f.Explanation))
	}
	if f.SuggestedFix != "" {
		fix := modelText(f.SuggestedFix)
		sep := " "
		if strings.Contains(fix, "\n") {
			sep = "\n\n"
		}
		fmt.Fprintf(b, "_Suggested fix:_%s%s\n\n", sep, fix)
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
