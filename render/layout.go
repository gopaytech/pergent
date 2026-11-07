package render

import (
	"fmt"
	"html"
	"slices"
	"sort"
	"strings"
	"unicode"
)

// The top of the comment: the headline, the meta line, the reason for the
// confidence score, the skills table and the failures. Then the findings, one
// list across all skills.

// failedSkill reports whether the skill did not finish its review.
func failedSkill(s Skill) bool { return s.Verdict == VerdictFailed || len(s.Failures) > 0 }

// reviewCounts counts, over every skill, the findings by severity (inline ones
// included, like the verdict), the blocking findings someone dismissed and
// the failed skills.
func reviewCounts(skills []Skill) (blocking, suggestions, dismissedBlock, failed int) {
	for _, s := range skills {
		b, g := countFindings(s.Findings)
		blocking, suggestions = blocking+b, suggestions+g
		dismissedBlock += dismissedBlocking(s.Dismissed)
		if failedSkill(s) {
			failed++
		}
	}
	return blocking, suggestions, dismissedBlock, failed
}

// writeHeader writes everything above the findings list.
func writeHeader(b *strings.Builder, skills []Skill, o Options) {
	blocking, suggestions, dismissed, failed := reviewCounts(skills)
	writeHeadline(b, skills, blocking, dismissed, failed)
	writeMeta(b, skills, o, blocking, suggestions, failed)
	writeReason(b, skills)
	if len(skills) > 1 {
		writeSkillsTable(b, skills)
	}
	writeFailures(b, skills)
}

// writeHeadline writes "### <icon> <verdict>[ · Confidence n/5]".
func writeHeadline(b *strings.Builder, skills []Skill, blocking, dismissed, failed int) {
	if len(skills) == 0 {
		b.WriteString("### No skills ran\n\n")
		return
	}
	switch {
	case blocking > 0:
		fmt.Fprintf(b, "### ❌ %s", plural(blocking, "blocking issue"))
	case failed > 0:
		fmt.Fprintf(b, "### ⚠️ %s failed", plural(failed, "skill"))
	default:
		b.WriteString("### ✅ No blocking issues")
		if dismissed > 0 {
			fmt.Fprintf(b, " (%d dismissed)", dismissed)
		}
	}
	if anyModelScore(skills) {
		score, _ := ReviewScore(skills)
		switch {
		case score.Value == 0:
			b.WriteString(" · Confidence not scored")
		case score.Capped != "":
			fmt.Fprintf(b, " · Confidence %d/5 (capped)", score.Value)
		default:
			fmt.Fprintf(b, " · Confidence %d/5", score.Value)
		}
	}
	b.WriteString("\n\n")
}

// metaCheckCounts returns the checks that passed and all that passed or
// failed, counting a check the batches of a skill answered once. n/a checks
// count in neither. They are the numbers of the checks block.
func metaCheckCounts(skills []Skill) (passed, total int) {
	passed, total, _ = checkTotals(mergeChecks(skills))
	return passed, total
}

// writeMeta writes the small line under the headline; a part with nothing to
// say is left out, and so is the line when every part is.
func writeMeta(b *strings.Builder, skills []Skill, o Options, blocking, suggestions, failed int) {
	var parts []string
	if o.Commit != "" {
		parts = append(parts, "Commit "+codeSpan(cutBytes(o.Commit, 7)))
	}
	if n := metaFileCount(skills, o); n > 0 {
		parts = append(parts, plural(n, "file"))
	}
	if passed, total := metaCheckCounts(skills); total > 0 {
		parts = append(parts, fmt.Sprintf("%d of %d checks passed", passed, total))
	}
	var counts []string
	if blocking > 0 {
		counts = append(counts, fmt.Sprintf("%d blocking", blocking))
	}
	if suggestions > 0 {
		counts = append(counts, plural(suggestions, "suggestion"))
	}
	switch {
	case len(counts) > 0:
		parts = append(parts, strings.Join(counts, " · "))
	case len(skills) > failed:
		// Not when every skill failed: nothing was reviewed to have no findings.
		parts = append(parts, "no findings")
	}
	if blocking > 0 && failed > 0 {
		parts = append(parts, plural(failed, "skill")+" failed")
	}
	if o.MetaNote != "" {
		parts = append(parts, oneLine(o.MetaNote))
	}
	if len(parts) > 0 {
		fmt.Fprintf(b, "<sub>%s</sub>\n\n", strings.Join(parts, " · "))
	}
}

// skillRef names a skill in a line of text; a skill name with an HTML comment
// or layout tag in it stays inert.
func skillRef(name string) string { return neutralizeComments(codeSpan(name)) }

// writeReason writes the model's reason for the review's confidence score as
// a quote, with the skill it came from when there are several and why the
// score was capped. A score of 5 that is not capped has none.
func writeReason(b *strings.Builder, skills []Skill) {
	score, at := ReviewScore(skills)
	if at < 0 || score.Value >= 5 && score.Capped == "" {
		return // at 5 of 5 the reason says nothing a reader needs
	}
	reason := lineText(skills[at].ConfidenceReason)
	switch {
	case reason == "" && score.Capped == "":
		return
	case reason == "":
		prefix := ""
		if len(skills) > 1 {
			prefix = skillRef(skills[at].Name) + ": "
		}
		fmt.Fprintf(b, "> %sCapped: %s\n\n", prefix, score.Capped)
		return
	}
	b.WriteString("> ")
	if len(skills) > 1 {
		b.WriteString(skillRef(skills[at].Name) + ": ")
	}
	b.WriteString(reason)
	if score.Capped != "" {
		fmt.Fprintf(b, " (capped: %s)", score.Capped)
	}
	b.WriteString("\n\n")
}

// writeFailures writes one line per failure, naming the skill when there are
// several.
func writeFailures(b *strings.Builder, skills []Skill) {
	for _, r := range skills {
		prefix := ""
		if len(skills) > 1 {
			prefix = skillRef(r.Name) + ": "
		}
		switch {
		case len(r.Failures) > 0:
			for _, f := range r.Failures {
				head := "⚠️ " + prefix
				// Name the files when the reader can't tell which ones failed: the
				// skill had several batches or there are several failures.
				if len(f.Files) > 0 && (r.Batches > 1 || len(r.Failures) > 1) {
					quoted := make([]string, len(f.Files))
					for i, file := range f.Files {
						quoted[i] = codeSpan(file)
					}
					head += fmt.Sprintf("Files %s: ", strings.Join(quoted, ", "))
				}
				writeFailure(b, head, f)
				b.WriteString("\n")
			}
		case r.Verdict == VerdictFailed:
			writeFailure(b, "⚠️ "+prefix, Failure{})
			b.WriteString("\n")
		}
	}
}

// findingRow is a finding to list, with the skill that reported it.
type findingRow struct {
	Finding
	skill string
}

// writeFindings writes the inline note and the findings list: every finding
// the caller did not post inline, blocking first, then by file and line.
func writeFindings(b *strings.Builder, skills []Skill, o Options, l layout) {
	var rows []findingRow
	inline := 0
	for _, s := range skills {
		for _, f := range s.Findings {
			if f.Fingerprint != "" && o.Inline[f.Fingerprint] {
				inline++
				continue
			}
			rows = append(rows, findingRow{f, s.Name})
		}
	}
	if inline > 0 {
		fmt.Fprintf(b, "> %s posted as inline comments on the diff.\n\n", plural(inline, "finding"))
	}
	if len(rows) == 0 {
		return
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, c := rows[i], rows[j]
		if ab, cb := a.Severity != Suggestion, c.Severity != Suggestion; ab != cb {
			return ab
		}
		if a.File != c.File {
			return a.File < c.File
		}
		return a.Line < c.Line
	})

	title := o.FindingsTitle
	if title == "" {
		title = "Findings"
	}
	many := len(skills) > 1
	var blocking, suggestions []findingRow
	for _, r := range rows {
		if r.Severity == Suggestion {
			suggestions = append(suggestions, r)
		} else {
			blocking = append(blocking, r)
		}
	}
	// The heading counts the findings shown; omitted suggestions get a note.
	shown := len(rows)
	if l.noSuggestions {
		shown = len(blocking)
	}
	if shown > 0 {
		fmt.Fprintf(b, "#### %s (%d)\n\n", neutralizeComments(oneLine(title)), shown)
	}
	for _, r := range blocking {
		writeFindingRow(b, r, o, many, false)
	}
	switch {
	case len(suggestions) == 0:
	case l.noSuggestions:
		fmt.Fprintf(b, "> %s omitted to fit the comment size limit.\n\n", plural(len(suggestions), "suggestion"))
	case l.suggestionTitles:
		// One HTML block with no blank line inside, so Markdown in a title
		// stays text.
		b.WriteString(detailsDroppedNote + "\n\n<ul>\n")
		for _, r := range suggestions {
			fmt.Fprintf(b, "<li>💡 <b>%s</b>%s", findingTitleHTML(r.Title), locationPart(r.Finding, o))
			if many {
				fmt.Fprintf(b, " · <code>%s</code>", escapeSummary(r.skill))
			}
			// There is no dismiss line here, so the ID stays to be quoted.
			if r.ID != "" {
				fmt.Fprintf(b, " · id <code>%s</code>", escapeSummary(r.ID))
			}
			b.WriteString("</li>\n")
		}
		b.WriteString("</ul>\n\n")
	default:
		for _, r := range suggestions {
			writeFindingRow(b, r, o, many, l.openFindings)
		}
	}
}

// escapeSummary prepares model text for a line of inline HTML: platforms
// don't render Markdown there, so it is shown as plain text.
func escapeSummary(s string) string { return html.EscapeString(oneLine(s)) }

// findingTitleHTML is escapeSummary for a finding's title, which also puts a
// zero-width joiner after each "@": the title is model text, and a platform
// turns "@name" into a mention even inside inline HTML.
func findingTitleHTML(s string) string {
	return strings.ReplaceAll(escapeSummary(s), "@", "@"+joiner)
}

// locationHTML is the finding's location as <code>, linked to the file when
// the caller gave o.FileURL.
func locationHTML(f Finding, o Options) string {
	if f.File == "" {
		return ""
	}
	code := "<code>" + escapeSummary(location(f)) + "</code>"
	if o.FileURL == nil {
		return code
	}
	if !repoPath(f.File) {
		return code
	}
	if u := o.FileURL(f.File, f.Line, f.EndLine); safeLinkURL(u) {
		return `<a href="` + html.EscapeString(u) + `">` + code + "</a>"
	}
	return code
}

// repoPath reports whether a finding's file can be linked: it is a path inside
// the repository, not an absolute one and with no ".." segment, which could
// make the link lead anywhere on the host. The file is model text.
func repoPath(file string) bool {
	if strings.HasPrefix(file, "/") || strings.HasPrefix(file, `\`) {
		return false
	}
	return !slices.Contains(strings.FieldsFunc(file, func(r rune) bool { return r == '/' || r == '\\' }), "..")
}

// safeLinkURL reports whether a URL from Options.FileURL can go into a link:
// it is not empty and has no whitespace or control character, so a hostile
// result can't carry a line break into the comment.
func safeLinkURL(u string) bool {
	return u != "" && !strings.ContainsFunc(u, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

// locationPart is " · <location>" for a summary line, or nothing for a
// finding with no file.
func locationPart(f Finding, o Options) string {
	if loc := locationHTML(f, o); loc != "" {
		return " · " + loc
	}
	return ""
}

// writeFindingRow writes one finding as a collapsible block. The summary line
// carries the title, location, skill (when there are several) and ID; blocking
// findings start open, and so do suggestions with openAll. The title,
// location and skill are model or file text in a line of HTML, so they are
// escaped; the body is Markdown and is guarded like any model text.
//
// The suggested change is display text: "<!--", "<details" and "<summary" are
// escaped and a line starting with "/" gets a zero-width space, so a caller
// that needs the exact text to apply it must use Finding.Suggestion itself.
func writeFindingRow(b *strings.Builder, r findingRow, o Options, withSkill, openAll bool) {
	f := r.Finding
	icon, open := "💡", ""
	if openAll {
		open = " open"
	}
	if f.Severity != Suggestion {
		icon, open = "❌", " open"
	}
	fmt.Fprintf(b, "<details%s><summary>%s <b>%s</b>%s", open, icon, findingTitleHTML(f.Title), locationPart(f, o))
	if withSkill {
		fmt.Fprintf(b, " · <code>%s</code>", escapeSummary(r.skill))
	}
	if f.ID != "" && o.DismissCommand == "" {
		fmt.Fprintf(b, " · id <code>%s</code>", escapeSummary(f.ID))
	}
	b.WriteString("</summary>\n\n")

	// A blank line between them: a note and an explanation in one paragraph
	// would share their backticks and definitions.
	var lead []string
	if note := o.FindingNote[f.Fingerprint]; note != "" {
		lead = append(lead, "_"+wrappedText(note)+"_")
	}
	if f.Explanation != "" {
		lead = append(lead, modelText(f.Explanation))
	}
	if len(lead) > 0 {
		b.WriteString(strings.Join(lead, "\n\n") + "\n\n")
	}
	if f.SuggestedFix != "" {
		fix := modelText(f.SuggestedFix)
		sep := " "
		if strings.Contains(fix, "\n") {
			sep = "\n\n"
		}
		fmt.Fprintf(b, "**Fix:**%s%s\n\n", sep, fix)
	}
	if f.Suggestion != "" {
		lines := fmt.Sprintf("line %d", f.Line)
		if f.EndLine > 0 && f.EndLine != f.Line {
			lines = fmt.Sprintf("lines %d–%d", f.Line, f.EndLine)
		}
		// "\r" goes first: neutralizeQuickActions removes it later and could join
		// two backtick runs into one longer than the fence.
		text := neutralizeComments(strings.ReplaceAll(f.Suggestion, "\r", ""))
		fence := strings.Repeat("`", max(3, longestBacktickRun(text)+1))
		fmt.Fprintf(b, "_Suggested change, replacing %s:_\n\n%s\n%s\n%s\n\n", lines, fence, text, fence)
	}
	if f.ID != "" && o.DismissCommand != "" {
		fmt.Fprintf(b, "Not a real problem? Reply %s\n\n", codeSpan(o.DismissCommand+" "+f.ID+" <reason>"))
	}
	b.WriteString("</details>\n\n")
}

// writeDismissedList writes the findings people set aside, across all skills.
func writeDismissedList(b *strings.Builder, skills []Skill, l layout) {
	var ds []DismissedFinding
	for _, s := range skills {
		ds = append(ds, s.Dismissed...)
	}
	switch {
	case len(ds) == 0:
	case l.noSuggestions:
		fmt.Fprintf(b, "> %s omitted to fit the comment size limit.\n\n", plural(len(ds), "dismissed finding"))
	default:
		fmt.Fprintf(b, "<details%s><summary>Dismissed (%d)</summary>\n\n", openAttr(l.openDismissed), len(ds))
		for _, d := range ds {
			writeDismissed(b, d)
		}
		b.WriteString("\n</details>\n\n")
	}
}

// openAttr is the attribute that makes a <details> start open.
func openAttr(open bool) string {
	if open {
		return " open"
	}
	return ""
}
