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
	// Lead is optional text for the part of the failure line after the
	// "Review failed:" prefix, followed by anything that goes below it (the
	// CLI puts the cause and the tail of opencode's stderr there). It can
	// carry file names and model or process output, so it is treated as model
	// text: its fenced code and code spans are kept, the rest is made inert.
	// Empty means Message, collapsed to one line, is shown.
	Lead string
}

// Skipped is a changed file a skill did not review, and the rule that
// skipped it. The rule is caller-owned text, not model text: it is not
// escaped beyond comments and layout tags.
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
	// skills that reviewed or skipped each file. With Short they are only
	// counted in the meta line, not listed.
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
	// included, is collapsed to single spaces. Empty adds nothing. Caller-owned
	// text, not escaped: it must not carry model text.
	MetaNote string
	// Short leaves out the Files list and the passed and n/a checks, with no
	// note, since nothing was cut for size. Everything else is as without it:
	// the meta line keeps its file count and "N of M checks passed", and the
	// summaries, the failed checks (an area with none has no heading), the
	// findings, the Dismissed list and the hidden findings block stay. The
	// size-limit steps still apply after it.
	Short bool
	// Version is shown in the footer after "pergent ".
	Version string
	// FindingsTitle replaces the "Findings" heading when non-empty. Caller-owned
	// text, not escaped.
	FindingsTitle string
	// FindingNote maps a finding's fingerprint to a one-line note shown at the
	// top of that finding's body, such as why it is not an inline comment. It
	// is shown as text, so it may carry model or file names.
	FindingNote map[string]string
	// Layout orders the comment's blocks (summary, findings, dismissed, checks,
	// files), says which start open or collapsed, and leaves out any block it
	// does not list. Nil, empty or not valid (see ParseLayout) means
	// DefaultLayout. It changes only what the comment shows, never a verdict,
	// score or count: the headline, meta line, failures, notes, footer and
	// hidden findings block are the same in every layout, blocking findings
	// are always open and a failed check is always shown. Short wins over it.
	Layout []Block
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
	summariesDroppedNote    = "> Summaries omitted to fit the comment size limit."
	truncatedNote           = "\n\n> Comment truncated to fit the comment size limit.\n"
)

// layout says what render leaves out of the comment.
type layout struct {
	noHidden         bool
	suggestionTitles bool // suggestions without their explanation and fix
	noSuggestions    bool // also leaves out the dismissed findings
	checksFailOnly   bool // check tables keep only failed checks
	noCheckTables    bool // the failed checks go too
	noSummaries      bool // the summaries go, last of all
	// What Options.Layout asks for, set by render and never by a size step.
	foldSummary   bool // summaries sit in a <details> labelled Summary
	foldChecks    bool // passed and n/a checks sit in a <details> per skill
	noChecks      bool // like Short: only the failed checks are written
	openFindings  bool // suggestions start open
	openDismissed bool
	openFiles     bool
	// listLimit caps the files list; -1 means no cap.
	listLimit int
}

// Comment renders the skills as one markdown comment: a headline with the
// verdict over all skills (and the confidence score, when a skill has one), a
// meta line, the reason for a score below 5 or capped, a skills table when
// there are several skills and the failures. Then the summary (one skill with
// one batch and one summary), one list of findings across all skills, blocking
// ones first and open, the dismissed findings, and the details: a line per
// batch for one skill with several batches, a section per skill when there are
// several (its summary, then its checks), and the checks by area, each area a
// heading with a small table, failed checks first. Last come the list of
// files, the notes and the footer. A hidden last line carries the findings for
// the next run. That is the default order; o.Layout can change the order of the
// summary, findings, dismissed, checks and files blocks, fold or open them, or
// leave one out (see Options.Layout). The headline, meta line, reason, table and failures are kept
// by every shrink step below.
//
// A comment over o.MaxLen is shrunk in steps until it fits: the hidden
// findings block goes first, then the suggestions' explanations and fixes
// (their titles stay), then the suggestions and dismissed findings, then the
// passed and n/a checks (an area left with none loses its heading), then the
// failed checks (the meta line keeps the counts), then the file list is cut to
// 20 paths and then to a count, and last the summaries. Blocking findings and
// failures are never dropped; if they alone are over the limit, the comment is
// cut at the limit with a note. With o.Short the file list and the passed and
// n/a checks are not written at all, so their steps change nothing and leave
// no note.
func Comment(skills []Skill, o Options) string {
	out := render(skills, o, layout{listLimit: -1})
	if o.MaxLen <= 0 || len(out) <= o.MaxLen {
		return out
	}
	for _, l := range shrinkSteps {
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

// shrinkSteps are the layouts Comment tries, in order, when the comment is
// over the size limit. Each one keeps what the one before it left out.
var shrinkSteps = []layout{
	{noHidden: true, listLimit: -1},
	{noHidden: true, suggestionTitles: true, listLimit: -1},
	{noHidden: true, noSuggestions: true, listLimit: -1},
	{noHidden: true, noSuggestions: true, checksFailOnly: true, listLimit: -1},
	{noHidden: true, noSuggestions: true, checksFailOnly: true, noCheckTables: true, listLimit: -1},
	{noHidden: true, noSuggestions: true, checksFailOnly: true, noCheckTables: true, listLimit: 20},
	{noHidden: true, noSuggestions: true, checksFailOnly: true, noCheckTables: true, listLimit: 0},
	{noHidden: true, noSuggestions: true, checksFailOnly: true, noCheckTables: true, listLimit: 0, noSummaries: true},
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

	blocks := resolveLayout(o.Layout)
	state := func(name string) string { s, _ := blockState(blocks, name); return s }
	_, hasSummary := blockState(blocks, BlockSummary)
	_, hasChecks := blockState(blocks, BlockChecks)
	l.noSummaries = l.noSummaries || !hasSummary // a block left out is not written, and says nothing
	l.noChecks = !hasChecks
	l.foldSummary = state(BlockSummary) == StateCollapsed
	l.foldChecks = state(BlockChecks) == StateCollapsed
	l.openFindings = state(BlockFindings) == StateOpen
	l.openDismissed = state(BlockDismissed) == StateOpen
	l.openFiles = state(BlockFiles) == StateOpen
	// The batch list, the skill sections and the failed checks sit at checks,
	// else at summary after any single summary, else right after findings.
	detailsAt := BlockFindings
	switch {
	case hasChecks:
		detailsAt = BlockChecks
	case hasSummary:
		detailsAt = BlockSummary
	}

	b.WriteString(Marker + "\n")
	writeHeader(&b, skills, o)
	for _, bl := range blocks {
		switch bl.Name {
		case BlockSummary:
			writeSummary(&b, skills, l)
		case BlockFindings:
			writeFindings(&b, skills, o, l)
		case BlockDismissed:
			writeDismissedList(&b, skills, l)
		case BlockFiles:
			writeFiles(&b, skills, o, l)
		}
		if bl.Name == detailsAt {
			writeDetails(&b, skills, o, l)
		}
	}
	writeNotes(&b, skills)
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
// carries. head is what the line starts with. The message is model or file
// text and is shown as one line of text. The lead is Markdown the caller built
// from such text (a file name, the model's answer, opencode's output), so it
// goes through blockText, which keeps its fenced code and spans and breaks
// the rest. A lead that starts with a fence follows the prefix on the same
// line, so it starts below it.
func writeFailure(b *strings.Builder, head string, f Failure) {
	if f.Lead != "" {
		lead := neutralizeComments(blockText(f.Lead))
		sep := " "
		if strings.HasPrefix(lead, "```") {
			sep = "\n\n"
		}
		fmt.Fprintf(b, "%s%s%s%s\n", head, failedPrefix, sep, lead)
		return
	}
	line := lineText(f.Message)
	if line == "" {
		line = noCauseMessage
	}
	fmt.Fprintf(b, "%s%s %s\n", head, failedPrefix, line)
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
// and ID, then who dismissed it and why. The title is model text and the
// reason someone's free text; both are shown as lineText does, so they can't
// open HTML, a comment or the layout's collapsible blocks, and re-rendering
// the comment doesn't mention people again.
func writeDismissed(b *strings.Builder, d DismissedFinding) {
	title, text := wrappedText, lineText
	if strings.Contains(d.Finding.Title+d.By+d.Reason, "$") {
		title, text = plainText, plainText // one output line: see lineSpans
	}
	fmt.Fprintf(b, "- **%s**", title(d.Finding.Title))
	if d.Finding.ID != "" {
		fmt.Fprintf(b, " id %s", codeSpan(d.Finding.ID))
	}
	by, reason := text(d.By), text(d.Reason)
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

// ownCell renders pergent's own text for a table cell: one line, "|" escaped
// so it can't add a column, "<" escaped so it can't open HTML, and backticks
// escaped when unbalanced so a code span can't run past the cell. Model text
// goes through cellText.
func ownCell(s string) string {
	s = oneLine(s)
	if strings.Count(s, "`")%2 == 1 {
		s = strings.ReplaceAll(s, "`", "\\`")
	}
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.ReplaceAll(s, "<", "&lt;")
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
// any backtick run inside s. A blank s gives a span of one space.
func codeSpan(s string) string {
	s = oneLine(s)
	if s == "" {
		return "` `" // a bare pair of backticks is not a span, and would pair with the next one
	}
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

// modelText prepares multi-line model text for the comment: blockText, then
// neutralizeComments, which still matters for the code that blockText keeps.
func modelText(s string) string { return neutralizeComments(blockText(s)) }
