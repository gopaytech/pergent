// Package render builds pergent's review comment from a review's results.
//
// It is the one place that knows the comment's layout, so the CLI and other
// programs that show or update a review (a service re-rendering a stored
// result, for example) produce the same comment. It imports no other pergent
// package and redacts nothing: the caller removes credentials from the text
// it passes in.
package render

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Marker is the first line of every pergent comment; it finds the comment
// to update.
const Marker = "<!-- pergent -->"

// Severity says whether a finding blocks the change.
type Severity string

const (
	Blocking   Severity = "blocking"
	Suggestion Severity = "suggestion"
)

// The verdicts a Skill can have.
const (
	VerdictNoBlockingIssues  = "no_blocking_issues"
	VerdictHasBlockingIssues = "has_blocking_issues"
	VerdictFailed            = "failed"
)

// The statuses a Check can have.
const (
	CheckPass = "pass"
	CheckFail = "fail"
	CheckNA   = "n/a"
)

// Finding is one review comment about a file, and optionally a line of it.
// Any severity other than Suggestion counts as blocking.
type Finding struct {
	// ID is the finding's short ID; empty shows none. It is shown at the end of
	// the row's summary line as "id <code>…</code>", or inside the dismiss line
	// when Options.DismissCommand is set.
	ID           string
	Fingerprint  string
	File         string
	Line         int
	EndLine      int // last line of the range the finding covers; 0 means Line only
	Severity     Severity
	Title        string
	Explanation  string
	SuggestedFix string
	Suggestion   string // exact replacement text for the lines Line to EndLine; the comment shows an escaped copy
}

// Check is one item of a skill's checklist.
type Check struct {
	Section string // the checklist heading it belongs to; may be empty
	Name    string
	Status  string // CheckPass, CheckFail or CheckNA
	Detail  string // the evidence; may be empty
}

// BatchChecks is the checklist the model reported for one batch.
type BatchChecks struct {
	Batch    int // 1-based batch number
	Checks   []Check
	Dropped  int // valid checks beyond the per-answer limit
	Findings int // findings the batch's answer reported
}

// Summary is the model's summary of one batch.
type Summary struct {
	Batch int // 1-based batch number
	Text  string
}

// Failure records in-scope files that were not reviewed.
type Failure struct {
	Files   []string
	Message string
	// Lead is optional markdown for the part of the failure line after the
	// "Review failed:" prefix, followed by anything that goes below it (the
	// CLI puts the cause and the tail of opencode's stderr there). It is used
	// as is, so the caller makes it safe to embed. Empty means Message,
	// collapsed to one line, is shown.
	Lead string
}

// Skipped is a changed file a skill did not review, and the rule that
// skipped it.
type Skipped struct{ Path, Rule string }

// DismissedFinding is a finding someone set aside.
type DismissedFinding struct {
	Finding Finding
	By      string
	Reason  string
}

// Skill is one skill's review.
type Skill struct {
	Name    string
	Verdict string // VerdictNoBlockingIssues, VerdictHasBlockingIssues or VerdictFailed

	Findings []Finding
	Failures []Failure
	Skipped  []Skipped
	Reviewed []string
	Notes    []string

	// Confidence is the model's score for the review, 1 to 5; 0 means none.
	// SkillScore lowers it for the comment.
	Confidence       int
	ConfidenceReason string

	Batches         int
	BatchesFinished int
	Summaries       []Summary
	Checks          []BatchChecks

	// Dismissed holds the findings ApplyDismissals set aside.
	Dismissed []DismissedFinding
}

// Options changes how Comment renders.
type Options struct {
	// MaxLen is the platform's comment size limit in bytes; not positive
	// means no limit.
	MaxLen int
	// ChangedFiles are listed in the Files block, which also names the
	// skills that reviewed or skipped each file.
	ChangedFiles []string
	// Inline holds the fingerprints of the findings the caller posted as
	// inline comments on the diff. They are left out of the one findings list
	// and replaced by one note with their count; the headline, the meta line's
	// counts, the skills table and the hidden findings block still count them.
	Inline map[string]bool
	// Footer is markdown the caller trusts, such as a hint on how to dismiss
	// a finding. It is written after the files, before the hidden findings
	// block, in every layout, inside <sub>…</sub> on one line with Version, so
	// it must be one paragraph of inline Markdown: its whitespace, line breaks
	// included, is collapsed to single spaces. Empty writes nothing.
	Footer string
	// Commit is the reviewed commit, shown as its first 7 characters.
	Commit string
	// FileURL returns the web link for a finding's lines; nil, or an empty
	// result, means the location is plain text. GitHubFileURL and
	// GitLabFileURL build one.
	FileURL func(path string, line, endLine int) string
	// DismissCommand is the command prefix that dismisses a finding, such as
	// "@pergent-review dismiss"; empty means no dismiss line.
	DismissCommand string
	// MetaNote is the last part of the meta line, such as how many files this
	// review covered. The caller formats it; its whitespace, line breaks
	// included, is collapsed to single spaces. Empty adds nothing.
	MetaNote string
	// Version is shown in the footer after "pergent ".
	Version string
	// FindingsTitle replaces the "Findings" heading when non-empty.
	FindingsTitle string
	// FindingNote maps a finding's fingerprint to a one-line note shown at the
	// top of that finding's body, such as why it is not an inline comment.
	FindingNote map[string]string
}

// failedPrefix starts the report of a failed batch.
const failedPrefix = "**Review failed:**"

// noCauseMessage is shown for a failure with neither a message nor a lead.
const noCauseMessage = "The run ended without a review and without a reported cause."

// Notes Comment adds for what it left out.
const (
	hiddenDroppedNote       = "> Findings were too large to store for the next run; --previous-review will treat the next run as a first review."
	detailsDroppedNote      = "> Suggestion details omitted to fit the comment size limit."
	passedChecksDroppedNote = "> Passed and n/a checks omitted to fit the comment size limit."
	checkTablesDroppedNote  = "> Check tables omitted to fit the comment size limit."
	truncatedNote           = "\n\n> Comment truncated to fit the comment size limit.\n"
)

// layout says what render leaves out of the comment.
type layout struct {
	noHidden         bool
	suggestionTitles bool // suggestions without their explanation and fix
	noSuggestions    bool // also leaves out the dismissed findings
	checksFailOnly   bool // check tables keep only failed checks
	noCheckTables    bool // the failed checks table goes too
	// listLimit caps the files list; -1 means no cap.
	listLimit int
}

// Comment renders the skills as one markdown comment: a headline with the
// verdict over all skills (and the confidence score, when a skill has one), a
// meta line, the reason for the score, a skills table when there are several
// skills, the failures, then one list of findings across all skills, blocking
// ones first and open, then the dismissed findings, then the checks of all
// skills (the failed ones in a table, the rest collapsed), the notes, the
// list of files and the footer. A hidden last line carries the findings for
// the next run. The headline, meta line, reason, table and failures are kept
// by every shrink step below.
//
// A comment over o.MaxLen is shrunk in steps until it fits: the hidden
// findings block goes first, then the suggestions' explanations and fixes
// (their titles stay), then the suggestions and dismissed findings, then the
// passed and n/a checks, then the failed checks table (the meta line keeps
// the counts), then the file list is cut to 20 paths and then to a count.
// Blocking findings and failures are never dropped; if they alone are over the
// limit, the comment is cut at the limit with a note.
func Comment(skills []Skill, o Options) string {
	out := render(skills, o, layout{listLimit: -1})
	if o.MaxLen <= 0 || len(out) <= o.MaxLen {
		return out
	}
	for _, l := range []layout{
		{noHidden: true, listLimit: -1},
		{noHidden: true, suggestionTitles: true, listLimit: -1},
		{noHidden: true, noSuggestions: true, listLimit: -1},
		{noHidden: true, noSuggestions: true, checksFailOnly: true, listLimit: -1},
		{noHidden: true, noSuggestions: true, checksFailOnly: true, noCheckTables: true, listLimit: -1},
		{noHidden: true, noSuggestions: true, checksFailOnly: true, noCheckTables: true, listLimit: 20},
		{noHidden: true, noSuggestions: true, checksFailOnly: true, noCheckTables: true, listLimit: 0},
	} {
		out = render(skills, o, l)
		if len(out) <= o.MaxLen {
			return out
		}
	}
	keep := o.MaxLen - len(truncatedNote)
	if keep < 0 {
		return cutBytes(out, o.MaxLen)
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

func render(skills []Skill, o Options, l layout) string {
	var b strings.Builder

	b.WriteString(Marker + "\n")
	writeHeader(&b, skills, o)
	writeFindings(&b, skills, o, l)
	writeDismissedList(&b, skills, l)
	writeChecks(&b, skills, l)
	writeNotes(&b, skills)
	writeFiles(&b, skills, o, l)
	writeFooter(&b, o)

	out := neutralizeQuickActions(b.String())
	if l.noHidden {
		return out + hiddenDroppedNote + "\n"
	}
	return out + FindingsBlock(skills)
}

// findingsMarker starts the hidden line that carries a review's findings to
// the next run.
const findingsMarker = "<!-- pergent:findings "

// findingsBlockVersion is the format version of the hidden findings block.
const findingsBlockVersion = 1

// blockFinding is a finding as the hidden block stores it. Its fields and
// tags are the ones pergent's CLI reads back with --previous-review.
type blockFinding struct {
	File         string   `json:"file"`
	Line         int      `json:"line,omitempty"`
	EndLine      int      `json:"end_line,omitempty"`
	Severity     Severity `json:"severity"`
	Title        string   `json:"title"`
	Explanation  string   `json:"explanation"`
	SuggestedFix string   `json:"suggested_fix,omitempty"`
	Suggestion   string   `json:"suggestion,omitempty"`
	Fingerprint  string   `json:"fingerprint,omitempty"`
}

// blockConfidence is a skill's score as the hidden block stores it: the
// model's, before any cap.
type blockConfidence struct {
	Score  int    `json:"score"`
	Reason string `json:"reason,omitempty"`
}

type findingsBlock struct {
	Version    int                        `json:"version"`
	Skills     map[string][]blockFinding  `json:"skills"`
	Confidence map[string]blockConfidence `json:"confidence,omitempty"`
}

// FindingsBlock returns the hidden line that carries the findings to the next
// run, including its newline: the same bytes the comment ends with. Dismissed
// findings are not in it. It is empty if the block cannot be encoded.
func FindingsBlock(skills []Skill) string {
	block := findingsBlock{Version: findingsBlockVersion, Skills: make(map[string][]blockFinding, len(skills))}
	for _, s := range skills {
		fs := make([]blockFinding, len(s.Findings))
		for i, f := range s.Findings {
			fs[i] = blockFinding{
				File: f.File, Line: f.Line, EndLine: f.EndLine, Severity: f.Severity, Title: f.Title,
				Explanation: f.Explanation, SuggestedFix: f.SuggestedFix, Suggestion: f.Suggestion,
				Fingerprint: f.Fingerprint,
			}
		}
		block.Skills[s.Name] = fs
		if hasModelScore(s) {
			if block.Confidence == nil {
				block.Confidence = map[string]blockConfidence{}
			}
			block.Confidence[s.Name] = blockConfidence{Score: s.Confidence, Reason: s.ConfidenceReason}
		}
	}
	data, err := json.Marshal(block)
	if err != nil {
		return ""
	}
	return findingsMarker + base64.StdEncoding.EncodeToString(data) + " -->\n"
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

// writeFailure writes one failure line, and below it whatever the lead
// carries.
func writeFailure(b *strings.Builder, f Failure) {
	if f.Lead != "" {
		fmt.Fprintf(b, "%s %s", failedPrefix, f.Lead)
		if !strings.HasSuffix(f.Lead, "\n") {
			b.WriteString("\n")
		}
		return
	}
	line := oneLine(f.Message)
	if line == "" {
		line = noCauseMessage
	}
	fmt.Fprintf(b, "%s %s\n", failedPrefix, neutralizeComments(line))
}

// dismissedBlocking counts the dismissed findings that would have blocked.
func dismissedBlocking(ds []DismissedFinding) int {
	n := 0
	for _, d := range ds {
		if d.Finding.Severity != Suggestion {
			n++
		}
	}
	return n
}

// writeDismissed writes one line of the Dismissed list: the finding's title
// and ID, then who dismissed it and why. The reason is someone's free text,
// so it is collapsed to one line and "<" is escaped, which keeps it from
// opening HTML, a comment or the layout's collapsible blocks. "@" is broken
// up too, so re-rendering the comment doesn't mention people again.
func writeDismissed(b *strings.Builder, d DismissedFinding) {
	fmt.Fprintf(b, "- **%s**", neutralizeComments(oneLine(d.Finding.Title)))
	if d.Finding.ID != "" {
		fmt.Fprintf(b, " id %s", codeSpan(d.Finding.ID))
	}
	by, reason := mentionSafe(d.By), mentionSafe(d.Reason)
	switch {
	case by != "" && reason != "":
		fmt.Fprintf(b, " — dismissed by %s: %s\n", by, reason)
	case by != "":
		fmt.Fprintf(b, " — dismissed by %s\n", by)
	case reason != "":
		fmt.Fprintf(b, " — dismissed: %s\n", reason)
	default:
		b.WriteString(" — dismissed\n")
	}
}

// checkCell renders model text for a table cell: one line, "|" escaped so it
// can't add a column, "<" escaped so it can't open HTML, and backticks
// escaped when unbalanced so a code span can't run past the cell.
func checkCell(s string) string {
	s = oneLine(s)
	if strings.Count(s, "`")%2 == 1 {
		s = strings.ReplaceAll(s, "`", "\\`")
	}
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.ReplaceAll(s, "<", "&lt;")
}

// summaryText renders free text as plain text. It always follows a fixed
// prefix, so it never starts a line and can't open a code block, heading or
// HTML block; escaping "<" keeps any HTML in it, comments included, inert.
func summaryText(s string) string {
	return strings.ReplaceAll(oneLine(s), "<", "&lt;")
}

// mentionSafe is summaryText with a zero-width joiner after each "@", which
// keeps platforms from turning "@name" into a mention.
func mentionSafe(s string) string {
	return strings.ReplaceAll(summaryText(s), "@", "@\u200d")
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// longestBacktickRun returns the length of the longest run of backticks in s.
func longestBacktickRun(s string) int {
	longest, run := 0, 0
	for _, c := range s {
		if c == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return longest
}

// codeSpan wraps s in an inline code span whose backtick fence is longer than
// any backtick run inside s.
func codeSpan(s string) string {
	s = oneLine(s)
	longest := longestBacktickRun(s)
	fence := strings.Repeat("`", longest+1)
	if longest > 0 {
		return fence + " " + s + " " + fence
	}
	return fence + s + fence
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

// neutralizeQuickActions keeps model text from running commands as the bot
// that posts the comment: GitLab runs a quick action on any line that starts
// with "/" (after deleting every "\r"), and other platforms' bots react to
// slash commands the same way. Carriage returns are removed and a zero-width
// space goes before a "/" that starts a line; indented lines are left as is.
func neutralizeQuickActions(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r", ""), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "/") {
			lines[i] = "\u200b" + line
		}
	}
	return strings.Join(lines, "\n")
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

// modelText prepares multi-line model text for the comment. "\r" goes first:
// neutralizeQuickActions removes it from the whole comment later, which would
// rebuild a comment marker or a fence that the steps here had broken up.
func modelText(s string) string {
	return closeFences(neutralizeComments(strings.ReplaceAll(strings.TrimSpace(s), "\r", "")))
}
