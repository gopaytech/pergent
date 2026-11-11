package render

import (
	"fmt"
	"strings"
)

// The middle and bottom of the comment: the summaries and checks, the list of
// files, the notes and the footer.

// checkIcons are the status marks of the check tables.
var checkIcons = map[string]string{
	CheckPass: "✅",
	CheckFail: "❌",
	CheckNA:   "➖",
}

// verificationDropPrefix starts the note the verification pass leaves when it
// drops findings; with checks, it is listed first among the notes, since a
// dropped finding can leave a failed check behind.
const verificationDropPrefix = "Verification dropped "

func isVerificationDropNote(n string) bool {
	return strings.HasPrefix(n, verificationDropPrefix)
}

// checkRow is one check of one skill, merged across the skill's batches.
type checkRow struct {
	skill string
	Check
	// matched is set on a failed row when a batch that failed it reported
	// findings; without one the check has no finding to explain it.
	matched bool
}

// checkRank orders statuses by how much they matter: failed beats n/a beats
// passed.
func checkRank(status string) int {
	switch status {
	case CheckFail:
		return 3
	case CheckNA:
		return 2
	case CheckPass:
		return 1
	}
	return 0
}

// mergeChecks returns every skill's checks as rows in order of first
// appearance. A check that several batches answered is one row, keyed by
// (skill, section, name): its status is the worst one and its detail comes
// from the first batch with that status.
func mergeChecks(skills []Skill) []checkRow {
	var rows []checkRow
	for _, s := range skills {
		type key struct{ section, name string }
		at := map[key]int{}
		for _, bc := range s.Checks {
			for _, c := range bc.Checks {
				k := key{c.Section, c.Name}
				i, seen := at[k]
				if !seen {
					i = len(rows)
					at[k] = i
					rows = append(rows, checkRow{skill: s.Name, Check: c})
				} else if checkRank(c.Status) > checkRank(rows[i].Status) {
					rows[i].Check = c
					rows[i].matched = false
				}
				if c.Status == CheckFail && rows[i].Status == CheckFail && bc.Findings > 0 {
					rows[i].matched = true
				}
			}
		}
	}
	return rows
}

// checkTotals counts merged rows: those that passed, those that passed or
// failed, and the n/a ones.
func checkTotals(rows []checkRow) (passed, total, na int) {
	for _, r := range rows {
		switch r.Status {
		case CheckPass:
			passed++
			total++
		case CheckFail:
			total++
		case CheckNA:
			na++
		}
	}
	return passed, total, na
}

// liveSummaries returns the skill's summaries that have text to show.
func liveSummaries(s Skill) []Summary {
	var out []Summary
	for _, x := range s.Summaries {
		if lineText(x.Text) != "" {
			out = append(out, x)
		}
	}
	return out
}

// singleSummary reports whether the skill's summary is one line of its own: one
// batch and one summary. Any other summaries are a line per batch.
func singleSummary(s Skill) bool { return s.Batches <= 1 && len(liveSummaries(s)) == 1 }

// writeSummary writes the Summary line of a review with one skill, one batch
// and one summary; every other summary is written by writeDetails. With
// l.noSummaries it writes the note about the cut, once, if there was any
// summary to cut anywhere. The text is model text and always follows a prefix
// of ours, because lineText does not break a "#", ">" or "-" at a line's start.
func writeSummary(b *strings.Builder, skills []Skill, l layout) {
	if l.noSummaries {
		for _, s := range skills {
			if len(liveSummaries(s)) > 0 {
				b.WriteString(summariesDroppedNote + "\n\n")
				return
			}
		}
		return
	}
	if len(skills) == 1 && singleSummary(skills[0]) {
		writeSkillSummary(b, skills[0], l.foldSummary)
	}
}

// writeSkillSummary writes one skill's summaries: "**Summary:** text" for one
// batch with one summary, otherwise a list with a line per batch that has one.
// A skill with no summary writes nothing, not an empty label. With fold it all
// sits in a <details> labelled Summary; the "**Summary:**" label stays inside,
// since model text must always follow a prefix of ours.
func writeSkillSummary(b *strings.Builder, s Skill, fold bool) {
	sums := liveSummaries(s)
	if len(sums) == 0 {
		return
	}
	if fold {
		b.WriteString("<details><summary>Summary</summary>\n\n")
		defer b.WriteString("</details>\n\n")
	}
	if singleSummary(s) {
		fmt.Fprintf(b, "**Summary:** %s\n\n", lineText(sums[0].Text))
		return
	}
	total := s.Batches
	for _, x := range sums {
		total = max(total, x.Batch)
	}
	for _, x := range sums {
		fmt.Fprintf(b, "- **Batch %d of %d:** %s\n", x.Batch, total, lineText(x.Text))
	}
	b.WriteString("\n")
}

// shownChecks returns the rows the layout shows: all of them, or only the
// failed ones with o.Short, a layout that leaves checks out and at the step
// that drops the passed and n/a checks, or none at the step that drops the
// tables.
func shownChecks(rows []checkRow, o Options, l layout) []checkRow {
	if l.noCheckTables {
		return nil
	}
	if !o.Short && !l.noChecks && !l.checksFailOnly {
		return rows
	}
	var failed []checkRow
	for _, r := range rows {
		if r.Status == CheckFail {
			failed = append(failed, r)
		}
	}
	return failed
}

// writeDetails writes what the summary line leaves: for one skill, a line per
// batch (when there is not a single summary) and its checks by area under
// "####" headings; for several skills, one "####" section per skill that has a
// summary or checks to show, with the checks under "#####" headings. Then one
// note if the size limit cut checks. A layout that leaves summaries out sets
// l.noSummaries, and one that leaves checks out sets l.noChecks.
func writeDetails(b *strings.Builder, skills []Skill, o Options, l layout) {
	var all []checkRow
	rowsOf := make([][]checkRow, len(skills))
	for i, s := range skills {
		rowsOf[i] = mergeChecks([]Skill{s})
		all = append(all, rowsOf[i]...)
	}
	if len(skills) == 1 {
		if !l.noSummaries && !singleSummary(skills[0]) {
			writeSkillSummary(b, skills[0], l.foldSummary)
		}
		writeChecks(b, rowsOf[0], shownChecks(rowsOf[0], o, l), l.foldChecks, "####")
	} else {
		for i, s := range skills {
			shown := shownChecks(rowsOf[i], o, l)
			summary := !l.noSummaries && len(liveSummaries(s)) > 0
			if !summary && len(shown) == 0 {
				continue
			}
			fmt.Fprintf(b, "#### %s\n\n", skillRef(s.Name))
			if summary {
				writeSkillSummary(b, s, l.foldSummary)
			}
			writeChecks(b, rowsOf[i], shown, l.foldChecks, "#####")
		}
	}
	writeChecksNote(b, all, o, l)
}

// writeChecksNote says what the size limit left out of the checks. With o.Short
// or a layout that leaves checks out the passed and n/a checks were never
// written, so their step leaves no note; the failed checks are written, so
// cutting them does.
func writeChecksNote(b *strings.Builder, rows []checkRow, o Options, l layout) {
	passed, total, na := checkTotals(rows)
	failed := total - passed
	quiet := o.Short || l.noChecks
	switch {
	case l.noCheckTables:
		if len(rows) > 0 && (!quiet || failed > 0) {
			b.WriteString(checkTablesDroppedNote + "\n\n")
		}
	case l.checksFailOnly && !quiet && passed+na > 0:
		b.WriteString(passedChecksDroppedNote + "\n\n")
	}
}

// checksLabel is the label of a block of checks, such as "Checks: 3 of 4
// passed, 1 n/a".
func checksLabel(rows []checkRow) string {
	passed, total, na := checkTotals(rows)
	var parts []string
	if total > 0 {
		parts = append(parts, fmt.Sprintf("%d of %d passed", passed, total))
	}
	if na > 0 {
		parts = append(parts, fmt.Sprintf("%d n/a", na))
	}
	return "Checks: " + strings.Join(parts, ", ")
}

// writeChecks writes one skill's shown checks by area. With fold the failed
// ones stay in view and the rest go in one <details> labelled with the skill's
// counts, over all its rows, still by area; with none left there is no fold.
func writeChecks(b *strings.Builder, all, shown []checkRow, fold bool, level string) {
	if !fold {
		writeAreas(b, shown, level)
		return
	}
	var failed, rest []checkRow
	for _, r := range shown {
		if r.Status == CheckFail {
			failed = append(failed, r)
		} else {
			rest = append(rest, r)
		}
	}
	writeAreas(b, failed, level)
	if len(rest) == 0 {
		return
	}
	fmt.Fprintf(b, "<details><summary>%s</summary>\n\n", checksLabel(all))
	writeAreas(b, rest, level)
	b.WriteString("</details>\n\n")
}

// otherChecksHeading names the table of checks with no area when named areas
// are shown beside it.
const otherChecksHeading = "Other checks"

// writeAreas writes checks as one table per area under a heading of the given
// level. Areas keep the order they first appear in. Checks with no area come
// last; beside named areas they get the heading "Other checks" (or they would
// read as part of the area above), alone they have none. That is decided on the
// rows shown, so an area dropped by Short or a size step leaves no such heading
// behind. The area is model text, so it is a line of text after the heading's
// prefix.
func writeAreas(b *strings.Builder, rows []checkRow, level string) {
	type area struct {
		heading string // already inert text; empty writes no heading
		rows    []checkRow
	}
	var areas []*area
	byName := map[string]*area{}
	var loose []checkRow
	for _, r := range rows {
		name := oneLine(r.Section)
		if name == "" {
			loose = append(loose, r)
			continue
		}
		a, ok := byName[name]
		if !ok {
			a = &area{heading: lineText(name)}
			byName[name] = a
			areas = append(areas, a)
		}
		a.rows = append(a.rows, r)
	}
	if len(loose) > 0 {
		other := &area{rows: loose}
		if len(areas) > 0 {
			other.heading = otherChecksHeading
		}
		areas = append(areas, other)
	}
	for _, a := range areas {
		if a.heading != "" {
			fmt.Fprintf(b, "%s %s\n\n", level, a.heading)
		}
		writeCheckTable(b, a.rows)
	}
}

// writeCheckTable writes the checks of one area as a table: the failed ones
// first, so one among many isn't missed, then the rest in their order.
func writeCheckTable(b *strings.Builder, rows []checkRow) {
	b.WriteString("| Check | Status | Detail |\n|---|---|---|\n")
	for _, fail := range []bool{true, false} {
		for _, r := range rows {
			if (r.Status == CheckFail) == fail {
				fmt.Fprintf(b, "| %s | %s | %s |\n", cellText(r.Name), checkIcons[r.Status], cellText(r.Detail))
			}
		}
	}
	b.WriteString("\n")
}

// writeNotes writes the notes about the checks (failed checks without a
// finding, checks over the limit, findings verification dropped), then the
// skills' other notes, as quotes. With several skills each names its skill.
func writeNotes(b *strings.Builder, skills []Skill) {
	var notes []string
	add := func(s Skill, n string) {
		prefix := ""
		if len(skills) > 1 {
			prefix = skillRef(s.Name) + ": "
		}
		notes = append(notes, prefix+lineText(n))
	}
	for _, s := range skills {
		if len(s.Checks) == 0 {
			continue
		}
		unmatched, dropped := 0, 0
		for _, r := range mergeChecks([]Skill{s}) {
			if r.Status == CheckFail && !r.matched {
				unmatched++
			}
		}
		for _, bc := range s.Checks {
			dropped += bc.Dropped
		}
		if unmatched == 1 {
			add(s, "1 failed check has no matching finding; the verdict is based on findings.")
		} else if unmatched > 1 {
			add(s, fmt.Sprintf("%d failed checks have no matching finding; the verdict is based on findings.", unmatched))
		}
		if dropped == 1 {
			add(s, "1 check beyond the first 60 per answer was not shown.")
		} else if dropped > 1 {
			add(s, fmt.Sprintf("%d checks beyond the first 60 per answer were not shown.", dropped))
		}
		for _, n := range s.Notes {
			if isVerificationDropNote(n) {
				add(s, n)
			}
		}
	}
	for _, s := range skills {
		for _, n := range s.Notes {
			if len(s.Checks) > 0 && isVerificationDropNote(n) {
				continue // listed with the check notes above
			}
			add(s, n)
		}
	}
	for _, n := range notes {
		fmt.Fprintf(b, "> %s\n", n)
	}
	if len(notes) > 0 {
		b.WriteString("\n")
	}
}

// fileRow is a path of the review and what each skill did with it.
type fileRow struct {
	path     string
	reviewed []int // indexes of the skills that reviewed it
	skipped  []fileSkip
	deleted  bool   // some skill reviewed it as deleted
	from     string // the old path, when some skill reviewed it as a rename
}

type fileSkip struct {
	skill int
	rule  string
}

// fileRows returns every path the review touched: the changed files, then
// those the skills reviewed or skipped that are not among them, each once.
func fileRows(skills []Skill, o Options) []fileRow {
	var rows []fileRow
	at := map[string]int{}
	row := func(path string) *fileRow {
		i, ok := at[path]
		if !ok {
			i = len(rows)
			at[path] = i
			rows = append(rows, fileRow{path: path})
		}
		return &rows[i]
	}
	for _, p := range o.ChangedFiles {
		row(p)
	}
	for i, s := range skills {
		for _, p := range s.Deleted {
			row(p).deleted = true
		}
		for _, rn := range s.Renamed {
			if r := row(rn.Path); r.from == "" {
				r.from = rn.From
			}
		}
		for _, p := range s.Reviewed {
			r := row(p)
			if n := len(r.reviewed); n == 0 || r.reviewed[n-1] != i {
				r.reviewed = append(r.reviewed, i)
			}
		}
		for _, sk := range s.Skipped {
			r := row(sk.Path)
			r.skipped = append(r.skipped, fileSkip{i, sk.Rule})
		}
	}
	return rows
}

// metaFileCount is the number of paths in the Files block.
func metaFileCount(skills []Skill, o Options) int { return len(fileRows(skills, o)) }

// fileMark is what follows a path's code span when a skill reviewed it as
// deleted or renamed. It shows once however many skills did.
func fileMark(r fileRow) string {
	var mark string
	if r.deleted {
		mark = " (deleted)"
	}
	if r.from != "" {
		mark += " (renamed from " + codeSpan(r.from) + ")"
	}
	return mark
}

// fileLine is what follows a path in the Files block: nothing when every skill
// reviewed it.
func fileLine(r fileRow, skills []Skill) string {
	rule := func(s string) string { return neutralizeComments(oneLine(s)) }
	if len(skills) == 1 {
		switch {
		case len(r.reviewed) > 0:
			return ""
		case len(r.skipped) > 0:
			if rl := rule(r.skipped[0].rule); rl != "" {
				return " — skipped: " + rl
			}
			return " — skipped"
		}
		return " — not reviewed"
	}
	if len(r.reviewed) == len(skills) {
		return ""
	}
	var parts []string
	if len(r.reviewed) > 0 {
		names := make([]string, len(r.reviewed))
		for i, k := range r.reviewed {
			names[i] = skillRef(skills[k].Name)
		}
		parts = append(parts, "reviewed by "+strings.Join(names, ", "))
	}
	if len(r.skipped) > 0 {
		names := make([]string, len(r.skipped))
		for i, sk := range r.skipped {
			names[i] = skillRef(skills[sk.skill].Name)
			if rl := rule(sk.rule); rl != "" {
				names[i] += " (" + rl + ")"
			}
		}
		parts = append(parts, "skipped by "+strings.Join(names, ", "))
	}
	if len(parts) == 0 {
		return " — not reviewed"
	}
	return " — " + strings.Join(parts, "; ")
}

// writeFiles writes every path of the review in one list, collapsed unless the
// layout opens it; with o.Short it writes nothing.
func writeFiles(b *strings.Builder, skills []Skill, o Options, l layout) {
	if o.Short {
		return
	}
	rows := fileRows(skills, o)
	if len(rows) == 0 {
		return
	}
	lines := make([]string, len(rows))
	for i, r := range rows {
		lines[i] = codeSpan(r.path) + fileMark(r) + fileLine(r, skills)
	}
	fmt.Fprintf(b, "<details%s><summary>Files (%d)</summary>\n\n", openAttr(l.openFiles), len(rows))
	writeList(b, lines, l.listLimit)
	b.WriteString("\n</details>\n\n")
}

// writeFooter writes the caller's footer and pergent's version in one small
// line.
func writeFooter(b *strings.Builder, o Options) {
	var parts []string
	if o.Footer != "" {
		parts = append(parts, oneLine(o.Footer))
	}
	if o.Version != "" {
		parts = append(parts, "pergent "+escapeSummary(o.Version))
	}
	if len(parts) > 0 {
		fmt.Fprintf(b, "<sub>%s</sub>\n\n", strings.Join(parts, " · "))
	}
}
