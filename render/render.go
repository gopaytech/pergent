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
	ID           string // short ID shown after the title; empty shows none
	Fingerprint  string
	File         string
	Line         int
	Severity     Severity
	Title        string
	Explanation  string
	SuggestedFix string
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
	// ChangedFiles are listed at the top of the comment.
	ChangedFiles []string
}

// failedPrefix starts the report of a failed batch.
const failedPrefix = "**Review failed:**"

// noCauseMessage is shown for a failure with neither a message nor a lead.
const noCauseMessage = "The run ended without a review and without a reported cause."

// Notes Comment adds for what it left out.
const (
	hiddenDroppedNote       = "> Findings were too large to store for the next run; --previous-review will treat the next run as a first review."
	detailsDroppedNote      = "> Suggestion details omitted to fit the comment size limit."
	summariesDroppedNote    = "> Summaries omitted to fit the comment size limit."
	passedChecksDroppedNote = "> Passed and n/a checks omitted to fit the comment size limit."
	checkTablesDroppedNote  = "> Check tables omitted to fit the comment size limit."
	truncatedNote           = "\n\n> Comment truncated to fit the comment size limit.\n"
)

// layout says what render leaves out of the comment.
type layout struct {
	noHidden         bool
	suggestionTitles bool // suggestions without their explanation and fix
	noSuggestions    bool // also leaves out the dismissed findings
	noSummaries      bool
	checksFailOnly   bool // check tables keep only failed checks
	noCheckTables    bool // only the checks count line is kept
	// listLimit caps the changed-files and skipped-files lists; -1 means no cap.
	listLimit int
}

// Comment renders the skills as one markdown comment: for each skill a
// verdict line, the blocking findings in full, then collapsed suggestions,
// dismissed findings and skipped files. A hidden last line carries the
// findings for the next run.
//
// A comment over o.MaxLen is shrunk in steps until it fits: the hidden
// findings block goes first, then the suggestions' explanations and fixes,
// then the suggestions and dismissed findings, then the passed and n/a
// checks, then the check tables (keeping the count line), then the review
// summaries, then the file lists are cut to a count. Blocking findings and
// failures are never dropped; if they alone are over the limit, the comment
// is cut at the limit with a note.
func Comment(skills []Skill, o Options) string {
	out := render(skills, o.ChangedFiles, layout{listLimit: -1})
	if o.MaxLen <= 0 || len(out) <= o.MaxLen {
		return out
	}
	for _, l := range []layout{
		{noHidden: true, listLimit: -1},
		{noHidden: true, suggestionTitles: true, listLimit: -1},
		{noHidden: true, noSuggestions: true, listLimit: -1},
		{noHidden: true, noSuggestions: true, checksFailOnly: true, listLimit: -1},
		{noHidden: true, noSuggestions: true, checksFailOnly: true, noCheckTables: true, listLimit: -1},
		{noHidden: true, noSuggestions: true, checksFailOnly: true, noCheckTables: true, noSummaries: true, listLimit: -1},
		{noHidden: true, noSuggestions: true, checksFailOnly: true, noCheckTables: true, noSummaries: true, listLimit: 20},
		{noHidden: true, noSuggestions: true, checksFailOnly: true, noCheckTables: true, noSummaries: true, listLimit: 0},
	} {
		out = render(skills, o.ChangedFiles, l)
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

func render(skills []Skill, changedFiles []string, l layout) string {
	var b strings.Builder

	b.WriteString(Marker + "\n")
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

	for _, s := range skills {
		writeSkill(&b, s, l)
	}

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
	Severity     Severity `json:"severity"`
	Title        string   `json:"title"`
	Explanation  string   `json:"explanation"`
	SuggestedFix string   `json:"suggested_fix,omitempty"`
	Fingerprint  string   `json:"fingerprint,omitempty"`
}

type findingsBlock struct {
	Version int                       `json:"version"`
	Skills  map[string][]blockFinding `json:"skills"`
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
				File: f.File, Line: f.Line, Severity: f.Severity, Title: f.Title,
				Explanation: f.Explanation, SuggestedFix: f.SuggestedFix, Fingerprint: f.Fingerprint,
			}
		}
		block.Skills[s.Name] = fs
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

func writeSkill(b *strings.Builder, r Skill, l layout) {
	fmt.Fprintf(b, "<!-- pergent:%s -->\n", r.Name)
	fmt.Fprintf(b, "### %s\n\n", r.Name)

	var blocking, suggestions []Finding
	for _, f := range r.Findings {
		if f.Severity == Suggestion {
			suggestions = append(suggestions, f)
		} else {
			blocking = append(blocking, f)
		}
	}

	switch {
	case len(r.Failures) > 0:
		for _, f := range r.Failures {
			b.WriteString("⚠️ ")
			// Name the files when the reader can't tell which ones failed: the
			// skill had several batches or there are several failures.
			if len(f.Files) > 0 && (r.Batches > 1 || len(r.Failures) > 1) {
				quoted := make([]string, len(f.Files))
				for i, file := range f.Files {
					quoted[i] = codeSpan(file)
				}
				fmt.Fprintf(b, "Files %s: ", strings.Join(quoted, ", "))
			}
			writeFailure(b, f)
			b.WriteString("\n")
		}
	case r.Verdict == VerdictFailed:
		b.WriteString("⚠️ ")
		writeFailure(b, Failure{})
		b.WriteString("\n")
	}

	switch {
	case len(blocking) == 1:
		b.WriteString("❌ **1 blocking issue**\n\n")
	case len(blocking) > 1:
		fmt.Fprintf(b, "❌ **%d blocking issues**\n\n", len(blocking))
	case r.Verdict == VerdictNoBlockingIssues:
		b.WriteString("✅ **No blocking issues**")
		if n := dismissedBlocking(r.Dismissed); n > 0 {
			fmt.Fprintf(b, " (%d dismissed)", n)
		}
		b.WriteString("\n\n")
	}

	writeSummaries(b, r, l)
	writeChecks(b, r, l)
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
	switch {
	case len(r.Dismissed) == 0:
	case l.noSuggestions:
		fmt.Fprintf(b, "> %s omitted to fit the comment size limit.\n\n", plural(len(r.Dismissed), "dismissed finding"))
	default:
		fmt.Fprintf(b, "<details><summary>Dismissed (%d)</summary>\n\n", len(r.Dismissed))
		for _, d := range r.Dismissed {
			writeDismissed(b, d)
		}
		b.WriteString("\n</details>\n\n")
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
			lines[i] = fmt.Sprintf("%s — %s", codeSpan(s.Path), neutralizeComments(oneLine(s.Rule)))
		}
		writeList(b, lines, l.listLimit)
		b.WriteString("\n</details>\n\n")
	}
	var notes []string
	for _, n := range r.Notes {
		if len(r.Checks) > 0 && isVerificationDropNote(n) {
			continue // shown next to the checks
		}
		notes = append(notes, n)
	}
	for _, n := range notes {
		fmt.Fprintf(b, "> %s\n", neutralizeComments(oneLine(n)))
	}
	if len(notes) > 0 {
		b.WriteString("\n")
	}

	fmt.Fprintf(b, "<!-- /pergent:%s -->\n\n", r.Name)
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
		fmt.Fprintf(b, " %s", codeSpan(d.Finding.ID))
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

// writeSummaries writes the model's summaries under the verdict: one
// paragraph for a single-batch skill, one bullet per batch otherwise.
func writeSummaries(b *strings.Builder, r Skill, l layout) {
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

// verificationDropPrefix starts the note the verification pass leaves when it
// drops findings; with checks, it is shown next to them, since a dropped
// finding can leave a failed check behind.
const verificationDropPrefix = "Verification dropped "

func isVerificationDropNote(n string) bool {
	return strings.HasPrefix(n, verificationDropPrefix)
}

var checkIcons = map[string]string{
	CheckPass: "✅",
	CheckFail: "❌",
	CheckNA:   "➖",
}

// checkCounts counts checks by status.
type checkCounts struct{ pass, fail, na int }

func countChecks(cs []Check) checkCounts {
	var c checkCounts
	for _, ch := range cs {
		switch ch.Status {
		case CheckPass:
			c.pass++
		case CheckFail:
			c.fail++
		case CheckNA:
			c.na++
		}
	}
	return c
}

func (c checkCounts) add(o checkCounts) checkCounts {
	return checkCounts{c.pass + o.pass, c.fail + o.fail, c.na + o.na}
}

// text lists the non-zero counts, e.g. "14 passed, 1 failed".
func (c checkCounts) text(sep string) string {
	var parts []string
	if c.pass > 0 {
		parts = append(parts, fmt.Sprintf("%d passed", c.pass))
	}
	if c.fail > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", c.fail))
	}
	if c.na > 0 {
		parts = append(parts, fmt.Sprintf("%d n/a", c.na))
	}
	return strings.Join(parts, sep)
}

// writeChecks writes the skill's checklist results: a count line, then the
// checks as one table per section. A single-batch skill shows the tables
// inline; otherwise each batch's tables go in a collapsed block that starts
// open when the batch has a failed check. Notes about failed checks without
// findings, checks over the limit, and findings verification dropped follow.
func writeChecks(b *strings.Builder, r Skill, l layout) {
	if len(r.Checks) == 0 {
		return
	}
	var total checkCounts
	unmatched, dropped := 0, 0
	for _, bc := range r.Checks {
		c := countChecks(bc.Checks)
		total = total.add(c)
		if bc.Findings == 0 {
			unmatched += c.fail
		}
		dropped += bc.Dropped
	}
	if counts := total.text(" · "); counts != "" {
		fmt.Fprintf(b, "**Checks:** %s\n\n", counts)
	}

	switch {
	case l.noCheckTables:
		b.WriteString(checkTablesDroppedNote + "\n\n")
	case r.Batches <= 1 && len(r.Checks) == 1:
		writeCheckTables(b, shownChecks(r.Checks[0].Checks, l))
	default:
		for _, bc := range r.Checks {
			c := countChecks(bc.Checks)
			shown := shownChecks(bc.Checks, l)
			if len(shown) == 0 {
				continue
			}
			open := ""
			if c.fail > 0 {
				open = " open"
			}
			fmt.Fprintf(b, "<details%s><summary>Checks — batch %d of %d (%s)</summary>\n\n", open, bc.Batch, r.Batches, c.text(", "))
			writeCheckTables(b, shown)
			b.WriteString("</details>\n\n")
		}
	}
	if l.checksFailOnly && !l.noCheckTables && total.pass+total.na > 0 {
		b.WriteString(passedChecksDroppedNote + "\n\n")
	}

	var notes []string
	if unmatched == 1 {
		notes = append(notes, "1 failed check has no matching finding; the verdict is based on findings.")
	} else if unmatched > 1 {
		notes = append(notes, fmt.Sprintf("%d failed checks have no matching finding; the verdict is based on findings.", unmatched))
	}
	if dropped == 1 {
		notes = append(notes, "1 check beyond the first 60 per answer was not shown.")
	} else if dropped > 1 {
		notes = append(notes, fmt.Sprintf("%d checks beyond the first 60 per answer were not shown.", dropped))
	}
	for _, n := range r.Notes {
		if isVerificationDropNote(n) {
			notes = append(notes, n)
		}
	}
	for _, n := range notes {
		fmt.Fprintf(b, "> %s\n", neutralizeComments(oneLine(n)))
	}
	if len(notes) > 0 {
		b.WriteString("\n")
	}
}

// shownChecks returns the checks the layout keeps: all of them, or only the
// failed ones once the comment had to shrink.
func shownChecks(cs []Check, l layout) []Check {
	if !l.checksFailOnly {
		return cs
	}
	var failed []Check
	for _, c := range cs {
		if c.Status == CheckFail {
			failed = append(failed, c)
		}
	}
	return failed
}

// writeCheckTables writes checks as one table per section, sections in order
// of first appearance and checks without a section last, with no heading.
func writeCheckTables(b *strings.Builder, cs []Check) {
	var order []string
	groups := map[string][]Check{}
	for _, c := range cs {
		if _, seen := groups[c.Section]; !seen && c.Section != "" {
			order = append(order, c.Section)
		}
		groups[c.Section] = append(groups[c.Section], c)
	}
	if len(groups[""]) > 0 {
		order = append(order, "")
	}
	for _, section := range order {
		if section != "" {
			fmt.Fprintf(b, "**%s**\n\n", checkCell(section))
		}
		b.WriteString("| Check | Status | Detail |\n|---|---|---|\n")
		for _, c := range groups[section] {
			fmt.Fprintf(b, "| %s | %s | %s |\n", checkCell(c.Name), checkIcons[c.Status], checkCell(c.Detail))
		}
		b.WriteString("\n")
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

// writeFinding writes a finding; titleOnly leaves out its explanation and
// suggested fix. A finding with an ID shows it after the title.
func writeFinding(b *strings.Builder, f Finding, titleOnly bool) {
	where := f.File
	if f.Line > 0 {
		where = fmt.Sprintf("%s:%d", f.File, f.Line)
	}
	idText := ""
	if f.ID != "" {
		idText = " " + codeSpan(f.ID)
	}
	fmt.Fprintf(b, "**%s** — **%s**%s\n\n", codeSpan(where), neutralizeComments(oneLine(f.Title)), idText)
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

// modelText prepares multi-line model text for the comment.
func modelText(s string) string {
	return closeFences(neutralizeComments(strings.TrimSpace(s)))
}
