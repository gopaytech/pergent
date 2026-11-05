package render

import (
	"fmt"
	"strings"
)

// The bottom of the comment: the checks across all skills, the notes, the
// list of files and the footer.

// checkIcons are the status marks of the check tables.
var checkIcons = map[string]string{
	CheckPass: "✅",
	CheckFail: "❌",
	CheckNA:   "➖",
}

// verificationDropPrefix starts the note the verification pass leaves when it
// drops findings; with checks, it is shown next to them, since a dropped
// finding can leave a failed check behind.
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

// writeChecks writes the failed checks of all skills as one table, then the
// passed and n/a ones in a collapsed block, as the layout allows.
func writeChecks(b *strings.Builder, skills []Skill, l layout) {
	rows := mergeChecks(skills)
	if len(rows) == 0 {
		return
	}
	var failed, other []checkRow
	for _, r := range rows {
		if r.Status == CheckFail {
			failed = append(failed, r)
		} else {
			other = append(other, r)
		}
	}
	if l.noCheckTables {
		b.WriteString(checkTablesDroppedNote + "\n\n")
		return
	}
	withSkill := len(skills) > 1
	if len(failed) > 0 {
		fmt.Fprintf(b, "**Failed checks (%d)**\n\n", len(failed))
		writeCheckTable(b, failed, withSkill)
	}
	switch {
	case len(other) == 0:
	case l.checksFailOnly:
		b.WriteString(passedChecksDroppedNote + "\n\n")
	default:
		passed, total, na := checkTotals(rows)
		var parts []string
		if total > 0 {
			parts = append(parts, fmt.Sprintf("%d of %d passed", passed, total))
		}
		if na > 0 {
			parts = append(parts, fmt.Sprintf("%d n/a", na))
		}
		fmt.Fprintf(b, "<details><summary>Checks: %s</summary>\n\n", strings.Join(parts, ", "))
		writeCheckTable(b, other, withSkill)
		b.WriteString("</details>\n\n")
	}
}

// writeCheckTable writes checks as one table, with the skill in a first column
// when the review has several.
func writeCheckTable(b *strings.Builder, rows []checkRow, withSkill bool) {
	if withSkill {
		b.WriteString("| Skill | Area | Check | Status | Detail |\n|---|---|---|---|---|\n")
	} else {
		b.WriteString("| Area | Check | Status | Detail |\n|---|---|---|---|\n")
	}
	for _, r := range rows {
		if withSkill {
			fmt.Fprintf(b, "| %s ", checkCell(skillRef(r.skill)))
		}
		fmt.Fprintf(b, "| %s | %s | %s | %s |\n", checkCell(r.Section), checkCell(r.Name), checkIcons[r.Status], checkCell(r.Detail))
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
		notes = append(notes, prefix+noteText(n))
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
				continue // shown with the checks
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

// noteTextReplacer makes model text in a note inert by putting a zero-width
// joiner inside every sequence that does something: "@" and "&" (a mention,
// also as "&#64;" or "&commat;"), "<" (HTML), "![" (an image), "](" (a link)
// and "[[" (a wiki link on GitLab). A joiner, not an entity or a backslash,
// because those show up literally inside a code span; backticks stay, so a
// plain note may use code spans. ">" is inert once "<" is broken.
var noteTextReplacer = strings.NewReplacer(
	"@", "@\u200d",
	"&", "&\u200d",
	"<", "<\u200d",
	"![", "!\u200d[",
	"](", "]\u200d(",
)

// noteText renders a note on one line as text only. The caller puts a fixed
// pergent prefix before it (a quote marker, and the skill name with several
// skills), so it never starts a block.
func noteText(n string) string {
	n = noteTextReplacer.Replace(oneLine(n))
	// A replacer pass doesn't overlap, so "[[[" keeps a "[[" after one pass.
	for strings.Contains(n, "[[") {
		n = strings.ReplaceAll(n, "[[", "[\u200d[")
	}
	return n
}

// fileRow is a path of the review and what each skill did with it.
type fileRow struct {
	path     string
	reviewed []int // indexes of the skills that reviewed it
	skipped  []fileSkip
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

// writeFiles writes every path of the review in one collapsed list.
func writeFiles(b *strings.Builder, skills []Skill, o Options, l layout) {
	rows := fileRows(skills, o)
	if len(rows) == 0 {
		return
	}
	lines := make([]string, len(rows))
	for i, r := range rows {
		lines[i] = codeSpan(r.path) + fileLine(r, skills)
	}
	fmt.Fprintf(b, "<details><summary>Files (%d)</summary>\n\n", len(rows))
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
