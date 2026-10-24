package output

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

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
	return render(results, changedFiles, layout{listLimit: -1})
}

// Notes FormatReviewWithin adds for what it left out.
const (
	hiddenDroppedNote    = "> Findings were too large to store for the next run; --previous-review will treat the next run as a first review."
	detailsDroppedNote   = "> Suggestion details omitted to fit the comment size limit."
	summariesDroppedNote = "> Summaries omitted to fit the comment size limit."
	truncatedNote        = "\n\n> Comment truncated to fit the comment size limit.\n"
)

// layout says what render leaves out of the comment.
type layout struct {
	noHidden         bool
	suggestionTitles bool // suggestions without their explanation and fix
	noSuggestions    bool
	noSummaries      bool
	// listLimit caps the changed-files and skipped-files lists; -1 means no cap.
	listLimit int
}

// FormatReviewWithin is FormatReview for a platform that limits a comment to
// maxLen bytes (no limit when maxLen is not positive). A comment over the
// limit is shrunk in steps until it fits: the hidden findings block goes
// first, then the suggestions' explanations and fixes, then the suggestions,
// then the review summaries, then the file lists are cut to a count. Blocking findings and failures are
// never dropped; if they alone are over the limit, the comment is cut at the
// limit with a note.
func FormatReviewWithin(results []review.SkillResult, changedFiles []string, maxLen int) string {
	out := FormatReview(results, changedFiles)
	if maxLen <= 0 || len(out) <= maxLen {
		return out
	}
	for _, l := range []layout{
		{noHidden: true, listLimit: -1},
		{noHidden: true, suggestionTitles: true, listLimit: -1},
		{noHidden: true, noSuggestions: true, listLimit: -1},
		{noHidden: true, noSuggestions: true, noSummaries: true, listLimit: -1},
		{noHidden: true, noSuggestions: true, noSummaries: true, listLimit: 20},
		{noHidden: true, noSuggestions: true, noSummaries: true, listLimit: 0},
	} {
		out = render(results, changedFiles, l)
		if len(out) <= maxLen {
			return out
		}
	}
	keep := maxLen - len(truncatedNote)
	if keep < 0 {
		return cutBytes(out, maxLen)
	}
	out = cutBytes(out, keep)
	if i := strings.LastIndexByte(out, '\n'); i >= 0 {
		out = out[:i]
	}
	return out + truncatedNote
}

// cutBytes returns the longest prefix of s of at most n bytes that doesn't
// split a UTF-8 sequence.
func cutBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func render(results []review.SkillResult, changedFiles []string, l layout) string {
	var b strings.Builder

	b.WriteString("<!-- pergent -->\n")
	b.WriteString("## pergent review\n\n")

	if len(changedFiles) > 0 {
		fmt.Fprintf(&b, "<details><summary>Files changed (%d)</summary>\n\n", len(changedFiles))
		lines := make([]string, len(changedFiles))
		for i, f := range changedFiles {
			lines[i] = codeSpan(f)
		}
		writeList(&b, lines, l.listLimit)
		b.WriteString("\n</details>\n\n")
	}

	block := findingsBlock{Version: findingsBlockVersion, Skills: make(map[string][]finding.Finding, len(results))}
	for _, r := range results {
		fs := redactFindings(r.Findings)
		block.Skills[r.Skill] = fs
		writeSkill(&b, r, fs, l)
	}

	out := redact.String(b.String())
	if l.noHidden {
		return out + hiddenDroppedNote + "\n"
	}
	if data, err := json.Marshal(block); err == nil {
		out += findingsMarker + base64.StdEncoding.EncodeToString(data) + " -->\n"
	}
	return out
}

// writeList writes lines as a markdown list, at most limit of them (all when
// limit is negative) and then a count of the rest.
func writeList(b *strings.Builder, lines []string, limit int) {
	for i, line := range lines {
		if limit >= 0 && i == limit {
			fmt.Fprintf(b, "- and %d more not listed to fit the comment size limit\n", len(lines)-limit)
			return
		}
		fmt.Fprintf(b, "- %s\n", line)
	}
}

func writeSkill(b *strings.Builder, r review.SkillResult, fs []finding.Finding, l layout) {
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

	writeSummaries(b, r, l)
	for _, f := range blocking {
		writeFinding(b, f, false)
	}
	switch {
	case len(suggestions) == 0:
	case l.noSuggestions:
		fmt.Fprintf(b, "> %s omitted to fit the comment size limit.\n\n", plural(len(suggestions), "suggestion"))
	default:
		fmt.Fprintf(b, "<details><summary>Suggestions (%d)</summary>\n\n", len(suggestions))
		if l.suggestionTitles {
			b.WriteString(detailsDroppedNote + "\n\n")
		}
		for _, f := range suggestions {
			writeFinding(b, f, l.suggestionTitles)
		}
		b.WriteString("</details>\n\n")
	}
	if len(r.Reviewed) > 0 {
		fmt.Fprintf(b, "<details><summary>Reviewed files (%d)</summary>\n\n", len(r.Reviewed))
		lines := make([]string, len(r.Reviewed))
		for i, p := range r.Reviewed {
			lines[i] = codeSpan(p)
		}
		writeList(b, lines, l.listLimit)
		b.WriteString("\n</details>\n\n")
	}
	if len(r.Skipped) > 0 {
		fmt.Fprintf(b, "<details><summary>Skipped files (%d)</summary>\n\n", len(r.Skipped))
		lines := make([]string, len(r.Skipped))
		for i, s := range r.Skipped {
			lines[i] = fmt.Sprintf("%s \u2014 %s", codeSpan(s.Path), neutralizeComments(oneLine(s.Rule)))
		}
		writeList(b, lines, l.listLimit)
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

// writeSummaries writes the model's summaries under the verdict: one
// paragraph for a single-batch skill, one bullet per batch otherwise.
func writeSummaries(b *strings.Builder, r review.SkillResult, l layout) {
	if len(r.Summaries) == 0 {
		return
	}
	if l.noSummaries {
		b.WriteString(summariesDroppedNote + "\n\n")
		return
	}
	if r.Batches <= 1 && len(r.Summaries) == 1 {
		fmt.Fprintf(b, "**Summary:** %s\n\n", summaryText(r.Summaries[0].Text))
		return
	}
	for _, sum := range r.Summaries {
		fmt.Fprintf(b, "- **Batch %d of %d:** %s\n", sum.Batch, r.Batches, summaryText(sum.Text))
	}
	b.WriteString("\n")
}

// summaryText renders a summary as plain text. It always follows a fixed
// prefix, so it never starts a line and can't open a code block, heading or
// HTML block; escaping "<" keeps any HTML in it, comments included, inert.
func summaryText(s string) string {
	return strings.ReplaceAll(oneLine(s), "<", "&lt;")
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// writeFinding writes a finding; titleOnly leaves out its explanation and
// suggested fix.
func writeFinding(b *strings.Builder, f finding.Finding, titleOnly bool) {
	where := f.File
	if f.Line > 0 {
		where = fmt.Sprintf("%s:%d", f.File, f.Line)
	}
	fmt.Fprintf(b, "**%s** \u2014 **%s**\n\n", codeSpan(where), neutralizeComments(oneLine(f.Title)))
	if titleOnly {
		return
	}
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
