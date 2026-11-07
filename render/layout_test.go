package render

import (
	"fmt"
	"strings"
	"testing"
)

func cleanSkill(name string, confidence int) Skill {
	return Skill{Name: name, Verdict: VerdictNoBlockingIssues, Confidence: confidence}
}

func TestHeadline(t *testing.T) {
	blockingSkill := Skill{
		Name: "s", Verdict: VerdictHasBlockingIssues, Confidence: 5,
		Findings: []Finding{{File: "a.go", Line: 1, Severity: Blocking, Title: "T"}},
	}
	for _, c := range []struct {
		name   string
		skills []Skill
		want   string
	}{
		{"clean", []Skill{cleanSkill("s", 4)}, Marker + "\n### ✅ No blocking issues · Confidence 4/5\n"},
		{"blocking capped", []Skill{blockingSkill}, Marker + "\n### ❌ 1 blocking issue · Confidence 3/5 (capped)\n"},
		{"failed", []Skill{{Name: "f", Verdict: VerdictFailed}, cleanSkill("c", 0)}, Marker + "\n### ⚠️ 1 skill failed\n"},
		{"two failed", []Skill{{Name: "f", Verdict: VerdictFailed}, {Name: "g", Verdict: VerdictFailed}}, Marker + "\n### ⚠️ 2 skills failed\n"},
		{"no skills", nil, Marker + "\n### No skills ran\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := Comment(c.skills, Options{})
			if !strings.HasPrefix(got, c.want) {
				t.Errorf("comment starts %q, want prefix %q", head(got), c.want)
			}
		})
	}
}

func TestHeadlineCounts(t *testing.T) {
	two := Skill{Name: "s", Verdict: VerdictHasBlockingIssues, Findings: []Finding{
		{File: "a.go", Severity: Blocking, Title: "A"}, {File: "b.go", Severity: Blocking, Title: "B"},
	}}
	got := Comment([]Skill{two, {Name: "f", Verdict: VerdictFailed}}, Options{})
	if !strings.Contains(got, "\n### ❌ 2 blocking issues\n") {
		t.Errorf("headline: %s", head(got))
	}
	if !strings.Contains(got, "2 blocking · 1 skill failed</sub>") {
		t.Errorf("a failed skill is not noted in the meta line: %s", head(got))
	}
	dismissed := Skill{Name: "s", Verdict: VerdictNoBlockingIssues, Dismissed: []DismissedFinding{
		{Finding: Finding{Severity: Blocking, Title: "D"}}, {Finding: Finding{Severity: Suggestion, Title: "E"}},
	}}
	got = Comment([]Skill{dismissed}, Options{})
	if !strings.Contains(got, "\n### ✅ No blocking issues (1 dismissed)\n") {
		t.Errorf("headline: %s", head(got))
	}
}

func TestHeadlineWithoutScoresHasNoConfidence(t *testing.T) {
	s := Skill{Name: "s", Verdict: VerdictNoBlockingIssues, Findings: []Finding{{File: "a.go", Severity: Suggestion, Title: "T"}}}
	got := Comment([]Skill{s}, Options{})
	if strings.Contains(got, "Confidence") || strings.Contains(got, "| Skill |") {
		t.Errorf("comment mentions confidence or a skills table:\n%s", got)
	}
}

func TestSkillsTable(t *testing.T) {
	got := Comment([]Skill{cleanSkill("a", 0), cleanSkill("b", 0)}, Options{})
	if !strings.Contains(got, "| Skill | Verdict | Findings |\n|---|---|---|\n") || strings.Contains(got, "Confidence") {
		t.Errorf("unscored table:\n%s", got)
	}
	got = Comment([]Skill{cleanSkill("a", 4), cleanSkill("b", 0)}, Options{})
	if !strings.Contains(got, "| Skill | Verdict | Confidence | Findings |\n") {
		t.Errorf("scored table:\n%s", got)
	}
}

func TestMetaLine(t *testing.T) {
	files := make([]string, 11)
	for i := range files {
		files[i] = fmt.Sprintf("f%d.go", i)
	}
	s := Skill{
		Name: "s", Verdict: VerdictNoBlockingIssues,
		Checks: []BatchChecks{{Batch: 1, Checks: []Check{
			{Name: "1", Status: CheckPass}, {Name: "2", Status: CheckPass}, {Name: "3", Status: CheckPass},
			{Name: "4", Status: CheckPass}, {Name: "5", Status: CheckPass}, {Name: "6", Status: CheckNA},
		}}},
		Findings: []Finding{
			{File: "a.go", Severity: Suggestion, Title: "A"}, {File: "b.go", Severity: Suggestion, Title: "B"}, {File: "c.go", Severity: Suggestion, Title: "C"},
		},
	}
	got := Comment([]Skill{s}, Options{Commit: "a1b2c3d4e5", ChangedFiles: files})
	want := "<sub>Commit `a1b2c3d` · 11 files · 5 of 5 checks passed · 3 suggestions</sub>\n\n"
	if !strings.Contains(got, want) {
		t.Errorf("meta line missing %q:\n%s", want, head(got))
	}
}

func TestMetaNote(t *testing.T) {
	s := Skill{Name: "s", Verdict: VerdictNoBlockingIssues, Reviewed: []string{"a.go", "b.go"},
		Findings: []Finding{{File: "a.go", Severity: Suggestion, Title: "A"}}}
	base := Options{Commit: "a1b2c3d4e5", ChangedFiles: []string{"a.go", "b.go"}}

	const note = "Re-reviewed 2 files · 28 carried over from `abc1234`"
	base.MetaNote = note
	got := Comment([]Skill{s}, base)
	if want := "<sub>Commit `a1b2c3d` · 2 files · 1 suggestion · " + note + "</sub>\n\n"; !strings.Contains(got, want) {
		t.Errorf("meta line missing %q:\n%s", want, head(got))
	}

	// Line breaks in the note collapse: the line stays one <sub> line.
	base.MetaNote = "Re-reviewed 2 files\n\n28 carried over"
	got = Comment([]Skill{s}, base)
	if want := "<sub>Commit `a1b2c3d` · 2 files · 1 suggestion · Re-reviewed 2 files 28 carried over</sub>\n\n"; !strings.Contains(got, want) {
		t.Errorf("meta line missing %q:\n%s", want, head(got))
	}

	// With no other part, the note makes the line.
	got = Comment(nil, Options{MetaNote: note})
	if want := "<sub>" + note + "</sub>\n\n"; !strings.Contains(got, want) {
		t.Errorf("meta line missing %q:\n%s", want, head(got))
	}
}

func TestMetaLineVariants(t *testing.T) {
	s := Skill{Name: "s", Verdict: VerdictNoBlockingIssues, Reviewed: []string{"a.go", "a.go", "b.go"},
		Checks: []BatchChecks{{Batch: 1, Checks: []Check{{Name: "1", Status: CheckPass}, {Name: "2", Status: CheckFail}}}}}
	got := Comment([]Skill{s}, Options{})
	if want := "<sub>2 files · 1 of 2 checks passed · no findings</sub>\n\n"; !strings.Contains(got, want) {
		t.Errorf("meta line missing %q:\n%s", want, head(got))
	}
	got = Comment([]Skill{{Name: "s", Verdict: VerdictNoBlockingIssues}}, Options{ChangedFiles: []string{"a.go"}})
	if want := "<sub>1 file · no findings</sub>\n\n"; !strings.Contains(got, want) {
		t.Errorf("meta line missing %q:\n%s", want, head(got))
	}
}

func TestReasonLine(t *testing.T) {
	one := Skill{Name: "a", Verdict: VerdictNoBlockingIssues, Confidence: 4, ConfidenceReason: "Safe @alice"}
	got := Comment([]Skill{one}, Options{})
	if want := "> Safe @\u200dalice\n\n"; !strings.Contains(got, want) {
		t.Errorf("reason missing %q:\n%s", want, got)
	}
	two := []Skill{
		{Name: "a", Verdict: VerdictNoBlockingIssues, Confidence: 5, ConfidenceReason: "fine"},
		{Name: "b", Verdict: VerdictNoBlockingIssues, Confidence: 2, ConfidenceReason: "<b>risky</b>"},
	}
	got = Comment(two, Options{})
	if want := "> `b`: <\u200db>risky<\u200d/b>\n\n"; !strings.Contains(got, want) {
		t.Errorf("reason missing %q:\n%s", want, got)
	}
}

func TestReasonLineCapped(t *testing.T) {
	f := []Finding{{File: "a.go", Severity: Blocking, Title: "T"}}
	got := Comment([]Skill{{Name: "a", Verdict: VerdictHasBlockingIssues, Confidence: 5, ConfidenceReason: "Looks wrong", Findings: f}}, Options{})
	if want := "> Looks wrong (capped: 1 blocking finding)\n\n"; !strings.Contains(got, want) {
		t.Errorf("reason missing %q:\n%s", want, got)
	}
	got = Comment([]Skill{{Name: "a", Verdict: VerdictHasBlockingIssues, Confidence: 5, Findings: f}}, Options{})
	if want := "> Capped: 1 blocking finding\n\n"; !strings.Contains(got, want) {
		t.Errorf("reason missing %q:\n%s", want, got)
	}
}

func twoSkillRows() []Skill {
	return []Skill{
		{Name: "alpha", Verdict: VerdictHasBlockingIssues, Findings: []Finding{
			{File: "b.go", Line: 1, Severity: Suggestion, Title: "Sug"},
			{File: "z.go", Line: 9, Severity: Blocking, Title: "Zed"},
		}},
		{Name: "beta", Verdict: VerdictHasBlockingIssues, Findings: []Finding{
			{File: "a.go", Line: 5, Severity: Blocking, Title: "Aye"},
		}},
	}
}

func TestFindingRowsOrderAndOpen(t *testing.T) {
	got := Comment(twoSkillRows(), Options{})
	rows := []string{
		"<details open><summary>❌ <b>Aye</b> · <code>a.go:5</code> · <code>beta</code></summary>",
		"<details open><summary>❌ <b>Zed</b> · <code>z.go:9</code> · <code>alpha</code></summary>",
		"<details><summary>💡 <b>Sug</b> · <code>b.go:1</code> · <code>alpha</code></summary>",
	}
	last := -1
	for _, r := range rows {
		i := strings.Index(got, r)
		if i < 0 {
			t.Fatalf("missing row %q:\n%s", r, got)
		}
		if i < last {
			t.Errorf("row %q out of order:\n%s", r, got)
		}
		last = i
	}
	if !strings.Contains(got, "#### Findings (3)\n\n") {
		t.Errorf("no findings heading:\n%s", got)
	}
}

func TestFindingRowLinkAndDismiss(t *testing.T) {
	s := Skill{Name: "s", Verdict: VerdictHasBlockingIssues, Findings: []Finding{
		{ID: "d9461c", File: "role.yaml", Line: 17, EndLine: 19, Severity: Blocking, Title: "T", Explanation: "E", SuggestedFix: "F"},
	}}
	got := Comment([]Skill{s}, Options{FileURL: GitHubFileURL("https://github.com/o/r", "sha1"), DismissCommand: "@bot dismiss"})
	for _, want := range []string{
		`<a href="https://github.com/o/r/blob/sha1/role.yaml?plain=1#L17-L19"><code>role.yaml:17–19</code></a>`,
		"Not a real problem? Reply `@bot dismiss d9461c <reason>`\n\n</details>\n\n",
		"</summary>\n\nE\n\n**Fix:** F\n\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("comment lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "id <code>") {
		t.Errorf("the ID is shown next to the dismiss command:\n%s", got)
	}
}

func TestFindingRowIDWithoutDismiss(t *testing.T) {
	s := Skill{Name: "s", Verdict: VerdictHasBlockingIssues, Findings: []Finding{
		{ID: "d9461c", File: "a.go", Severity: Blocking, Title: "T", Explanation: "E"},
	}}
	got := Comment([]Skill{s}, Options{})
	if !strings.Contains(got, " · id <code>d9461c</code></summary>") {
		t.Errorf("ID not labelled:\n%s", got)
	}
	if strings.Contains(got, "Not a real problem") {
		t.Errorf("dismiss line without a command:\n%s", got)
	}
}

func TestFindingRowMultiLineFix(t *testing.T) {
	s := Skill{Name: "s", Verdict: VerdictHasBlockingIssues, Findings: []Finding{
		{File: "a.go", Severity: Blocking, Title: "T", Explanation: "E", SuggestedFix: "one\ntwo"},
	}}
	got := Comment([]Skill{s}, Options{})
	if want := "**Fix:**\n\none\ntwo\n\n"; !strings.Contains(got, want) {
		t.Errorf("comment lacks %q:\n%s", want, got)
	}
}

func TestFindingTitleIsEscapedInSummary(t *testing.T) {
	s := Skill{Name: "s", Verdict: VerdictHasBlockingIssues, Findings: []Finding{
		{File: "a<b>.go", Line: 1, Severity: Blocking, Title: "x</summary></details><a href=\"https://e.vil\">&`y`", Explanation: "e"},
	}}
	got := Comment([]Skill{s}, Options{})
	var summary string
	for _, l := range strings.Split(got, "\n") {
		if strings.HasPrefix(l, "<details open><summary>") {
			summary = l
		}
	}
	if !strings.Contains(summary, "x&lt;/summary&gt;&lt;/details&gt;&lt;a href=&#34;https://e.vil&#34;&gt;&amp;") {
		t.Errorf("title not escaped in the summary line: %q", summary)
	}
	if strings.Contains(summary, "<a ") || strings.Contains(summary, "<b>.go") {
		t.Errorf("summary line has injected markup: %q", summary)
	}
	if opens, closes := strings.Count(got, "<details"), strings.Count(got, "</details>"); opens != closes {
		t.Errorf("%d <details, %d </details>:\n%s", opens, closes, got)
	}
}

func TestFindingsTitleAndNote(t *testing.T) {
	s := Skill{Name: "s", Verdict: VerdictHasBlockingIssues, Findings: []Finding{
		{Fingerprint: "fp1", File: "a.go", Severity: Blocking, Title: "T", Explanation: "E"},
	}}
	got := Comment([]Skill{s}, Options{FindingsTitle: "Not on the diff", FindingNote: map[string]string{"fp1": "off the <diff>"}})
	if !strings.Contains(got, "#### Not on the diff (1)\n\n") {
		t.Errorf("no custom heading:\n%s", got)
	}
	if !strings.Contains(got, "</summary>\n\n_off the <\u200ddiff>_\n\nE\n\n") {
		t.Errorf("note not at the top of the body:\n%s", got)
	}
}

func TestSummaryShown(t *testing.T) {
	s := Skill{Name: "s", Verdict: VerdictNoBlockingIssues, Batches: 1,
		Summaries: []Summary{{Batch: 1, Text: "Reviewed the widget carefully."}}}
	got := Comment([]Skill{s}, Options{})
	if !strings.Contains(got, "\n**Summary:** Reviewed the widget carefully.\n\n") {
		t.Errorf("summary not shown:\n%s", got)
	}
}

func TestInlineNoteAndNoFindingsSection(t *testing.T) {
	s := Skill{Name: "s", Verdict: VerdictHasBlockingIssues, Findings: []Finding{
		{Fingerprint: "fp1", File: "a.go", Severity: Blocking, Title: "T"},
	}}
	got := Comment([]Skill{s}, Options{Inline: map[string]bool{"fp1": true}})
	if !strings.Contains(got, "> 1 finding posted as inline comments on the diff.\n\n") || strings.Contains(got, "#### ") {
		t.Errorf("inline layout:\n%s", got)
	}
}

func TestSuggestionTitlesLayout(t *testing.T) {
	s := Skill{Name: "s", Verdict: VerdictHasBlockingIssues, Findings: []Finding{
		{File: "a.go", Line: 1, Severity: Blocking, Title: "B", Explanation: "blockbody"},
		{File: "b.go", Line: 2, Severity: Suggestion, Title: "S", Explanation: "suggbody"},
	}}
	out := render([]Skill{s}, Options{}, layout{noHidden: true, suggestionTitles: true, listLimit: -1})
	if !strings.Contains(out, detailsDroppedNote+"\n\n<ul>\n<li>💡 <b>S</b> · <code>b.go:2</code></li>\n</ul>\n\n") || strings.Contains(out, "suggbody") || !strings.Contains(out, "blockbody") {
		t.Errorf("layout:\n%s", out)
	}
	out = render([]Skill{s}, Options{}, layout{noHidden: true, noSuggestions: true, listLimit: -1})
	if !strings.Contains(out, "#### Findings (1)\n\n") {
		t.Errorf("the heading should count only the findings shown:\n%s", out)
	}
	if !strings.Contains(out, "> 1 suggestion omitted to fit the comment size limit.\n\n") || strings.Contains(out, "<b>S</b>") {
		t.Errorf("layout:\n%s", out)
	}
}

// head returns the start of a comment for an error message.
func head(s string) string {
	if len(s) > 600 {
		return s[:600] + "..."
	}
	return s
}

func TestSkillNameStaysInertInHeader(t *testing.T) {
	skills := []Skill{
		{Name: "a<!--x</details>", Verdict: VerdictFailed},
		{Name: "b", Verdict: VerdictNoBlockingIssues, Confidence: 3, ConfidenceReason: "r"},
	}
	got := Comment(skills, Options{})
	if strings.Count(got, "<!--") != 2 { // the marker and the findings block
		t.Errorf("a skill name opened an HTML comment:\n%s", got)
	}
	if strings.Contains(got, "</details>") {
		t.Errorf("a skill name closed a block:\n%s", got)
	}
}

func TestChecksFailedFirstInTheirArea(t *testing.T) {
	s := Skill{Name: "s", Verdict: VerdictNoBlockingIssues, Batches: 1, Checks: []BatchChecks{{Batch: 1, Checks: []Check{
		{Section: "Sec", Name: "good", Status: CheckPass, Detail: "ok"},
		{Section: "Sec", Name: "bad", Status: CheckFail, Detail: "broken"},
		{Name: "skip", Status: CheckNA},
	}}}}
	got := Comment([]Skill{s}, Options{})
	want := "\n#### Sec\n\n| Check | Status | Detail |\n|---|---|---|\n| bad | ❌ | broken |\n| good | ✅ | ok |\n\n" +
		"| Check | Status | Detail |\n|---|---|---|\n| skip | ➖ |  |\n\n"
	if !strings.Contains(got, want) {
		t.Errorf("checks:\n%s", got)
	}
}

func TestChecksMultiSkillAndBatch(t *testing.T) {
	a := Skill{Name: "a", Verdict: VerdictNoBlockingIssues, Batches: 2, Checks: []BatchChecks{
		{Batch: 1, Checks: []Check{{Name: "Tests", Status: CheckPass, Detail: "ok"}}},
		{Batch: 2, Checks: []Check{{Name: "Tests", Status: CheckFail, Detail: "missing"}}},
	}}
	b := Skill{Name: "b", Verdict: VerdictNoBlockingIssues, Batches: 1, Checks: []BatchChecks{
		{Batch: 1, Checks: []Check{{Name: "Tests", Status: CheckPass, Detail: "fine"}}},
	}}
	got := Comment([]Skill{a, b}, Options{})
	want := "\n#### `a`\n\n| Check | Status | Detail |\n|---|---|---|\n| Tests | ❌ | missing |\n\n" +
		"#### `b`\n\n| Check | Status | Detail |\n|---|---|---|\n| Tests | ✅ | fine |\n\n"
	if !strings.Contains(got, want) {
		t.Errorf("sections:\n%s", got)
	}
	if strings.Contains(got, "| ok |") {
		t.Errorf("a batch's own row is shown:\n%s", got)
	}
	// One number per fact: the meta line counts merged rows.
	if !strings.Contains(got, "1 of 2 checks passed") {
		t.Errorf("counts:\n%s", got)
	}
}

func TestChecksNotes(t *testing.T) {
	// Two batches fail the same check, neither with findings: one unmatched row.
	a := Skill{Name: "a", Verdict: VerdictNoBlockingIssues, Batches: 2, Notes: []string{"Verification dropped 1 finding", "plain note"},
		Checks: []BatchChecks{
			{Batch: 1, Dropped: 2, Checks: []Check{{Name: "T", Status: CheckFail}}},
			{Batch: 2, Checks: []Check{{Name: "T", Status: CheckFail}}},
		}}
	got := Comment([]Skill{a, cleanSkill("b", 0)}, Options{})
	want := "> `a`: 1 failed check has no matching finding; the verdict is based on findings.\n" +
		"> `a`: 2 checks beyond the first 60 per answer were not shown.\n" +
		"> `a`: Verification dropped 1 finding\n" +
		"> `a`: plain note\n\n"
	if !strings.Contains(got, want) {
		t.Errorf("notes missing:\n%s", got)
	}
}

func TestFilesBlock(t *testing.T) {
	s := Skill{Name: "s", Verdict: VerdictNoBlockingIssues, Reviewed: []string{"a.go"}, Skipped: []Skipped{{"go.sum", "lockfile"}}}
	got := Comment([]Skill{s}, Options{ChangedFiles: []string{"a.go", "go.sum", "x.md"}})
	for _, want := range []string{
		"<details><summary>Files (3)</summary>\n\n",
		"- `a.go`\n",
		"- `go.sum` — skipped: lockfile\n",
		"- `x.md` — not reviewed\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Reviewed files") || strings.Contains(got, "Skipped files") || strings.Contains(got, "Files changed") {
		t.Errorf("old blocks:\n%s", got)
	}

	a := Skill{Name: "a", Verdict: VerdictNoBlockingIssues, Reviewed: []string{"a.go"}}
	b := Skill{Name: "b", Verdict: VerdictNoBlockingIssues, Skipped: []Skipped{{"a.go", "generated"}}}
	got = Comment([]Skill{a, b}, Options{ChangedFiles: []string{"a.go"}})
	if want := "- `a.go` — reviewed by `a`; skipped by `b` (generated)\n"; !strings.Contains(got, want) {
		t.Errorf("lacks %q:\n%s", want, got)
	}
	// Reviewed by every skill: no suffix. Reviewed by none: not reviewed.
	c := Skill{Name: "c", Verdict: VerdictNoBlockingIssues, Reviewed: []string{"a.go"}}
	got = Comment([]Skill{a, c}, Options{ChangedFiles: []string{"a.go", "z.go"}})
	if !strings.Contains(got, "- `a.go`\n") || !strings.Contains(got, "- `z.go` — not reviewed\n") {
		t.Errorf("every skill / none:\n%s", got)
	}
}

func TestFilesBlockCountMatchesMeta(t *testing.T) {
	s := Skill{Name: "s", Verdict: VerdictNoBlockingIssues, Reviewed: []string{"a.go", "extra.go"}, Skipped: []Skipped{{"b.go", "lockfile"}}}
	got := Comment([]Skill{s}, Options{ChangedFiles: []string{"a.go", "a.go", "b.go"}})
	if !strings.Contains(got, "<sub>3 files · no findings</sub>") || !strings.Contains(got, "Files (3)</summary>") {
		t.Errorf("file counts differ:\n%s", got)
	}
}

func TestFilesBlockHostile(t *testing.T) {
	s := Skill{Name: "s", Verdict: VerdictNoBlockingIssues, Skipped: []Skipped{{"x.go", "rule </details>\n<!-- y"}}}
	got := Comment([]Skill{s}, Options{ChangedFiles: []string{"x.go"}})
	if opens, closes := strings.Count(got, "<details"), strings.Count(got, "</details>"); opens != closes {
		t.Errorf("%d <details, %d </details>:\n%s", opens, closes, got)
	}
	if strings.Count(got, "<!--") != 2 {
		t.Errorf("a rule opened an HTML comment:\n%s", got)
	}
}

func TestFooter(t *testing.T) {
	s := cleanSkill("s", 0)
	footer := "Run again: " + codeSpan("@bot review")
	got := Comment([]Skill{s}, Options{Footer: footer, Version: "v0.3.0"})
	want := "<sub>Run again: `@bot review` · pergent v0.3.0</sub>\n\n" + FindingsBlock([]Skill{s})
	if !strings.HasSuffix(got, want) {
		t.Errorf("footer:\n%s", got)
	}
	got = Comment([]Skill{s}, Options{Version: "v0.3.0"})
	if want := "<sub>pergent v0.3.0</sub>\n\n" + FindingsBlock([]Skill{s}); !strings.HasSuffix(got, want) {
		t.Errorf("version only:\n%s", got)
	}
	got = Comment([]Skill{s}, Options{Footer: "hint"})
	if want := "<sub>hint</sub>\n\n" + FindingsBlock([]Skill{s}); !strings.HasSuffix(got, want) {
		t.Errorf("footer only:\n%s", got)
	}
	if got = Comment([]Skill{s}, Options{}); strings.Contains(got, "pergent v") || strings.Count(got, "<sub>") != 1 {
		t.Errorf("empty footer drew something:\n%s", got)
	}
}

func bigFindings() Skill {
	s := Skill{Name: "s", Verdict: VerdictHasBlockingIssues}
	pad := strings.Repeat("x", 200)
	for i := 0; i < 30; i++ {
		s.Findings = append(s.Findings,
			Finding{File: fmt.Sprintf("b%02d.go", i), Line: 1, Severity: Blocking, Title: fmt.Sprintf("Block %02d", i), Explanation: fmt.Sprintf("body-%02d %s", i, pad)},
			Finding{File: fmt.Sprintf("s%02d.go", i), Line: 1, Severity: Suggestion, Title: fmt.Sprintf("Sug %02d", i), Explanation: fmt.Sprintf("sbody-%02d %s", i, pad)})
	}
	return s
}

func TestShrinkKeepsBlockingBodies(t *testing.T) {
	skills := []Skill{bigFindings()}
	limit := len(render(skills, Options{}, layout{noHidden: true, noSuggestions: true, listLimit: -1}))
	titles := len(render(skills, Options{}, layout{noHidden: true, suggestionTitles: true, listLimit: -1}))
	if titles <= limit {
		t.Fatalf("test setup: title-only layout (%d) fits the limit (%d)", titles, limit)
	}
	got := Comment(skills, Options{MaxLen: limit})
	if len(got) > limit {
		t.Fatalf("%d bytes over the limit %d", len(got), limit)
	}
	for i := 0; i < 30; i++ {
		if !strings.Contains(got, fmt.Sprintf("body-%02d ", i)) {
			t.Errorf("blocking body %d is gone", i)
		}
	}
	if n := strings.Count(got, "<details open><summary>❌"); n != 30 {
		t.Errorf("%d open blocking rows, want 30", n)
	}
	if !strings.Contains(got, "30 suggestions omitted") {
		t.Errorf("no omitted note:\n%s", head(got))
	}
}

func TestTitleOnlyRowsKeepTitleAsText(t *testing.T) {
	s := Skill{Name: "s", Verdict: VerdictNoBlockingIssues, Findings: []Finding{
		{ID: "abc123", File: "a.go", Line: 3, Severity: Suggestion, Title: "[x](https://e.vil) **b**", Explanation: strings.Repeat("e", 500)},
	}}
	skills := []Skill{s}
	full := len(render(skills, Options{}, layout{noHidden: true, listLimit: -1}))
	titles := render(skills, Options{}, layout{noHidden: true, suggestionTitles: true, listLimit: -1})
	if len(titles) >= full {
		t.Fatalf("test setup: %d >= %d", len(titles), full)
	}
	got := Comment(skills, Options{MaxLen: len(titles)})
	if !strings.Contains(got, detailsDroppedNote) || !strings.Contains(got, "<ul>\n<li>💡 <b>[x](https://e.vil) **b**</b>") {
		t.Fatalf("not the title-only step:\n%s", got)
	}
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "](https://e.vil)") && !strings.HasPrefix(line, "<li>") {
			t.Errorf("title text outside an <li>: %q", line)
		}
	}
	if !strings.Contains(got, "id <code>abc123</code></li>\n</ul>\n\n") {
		t.Errorf("list not closed:\n%s", got)
	}
}

func TestFooterIsOneLine(t *testing.T) {
	got := Comment([]Skill{cleanSkill("s", 0)}, Options{Footer: "a\n\nb", Version: "v1"})
	if !strings.Contains(got, "<sub>a b · pergent v1</sub>\n\n") {
		t.Errorf("footer not collapsed to one line:\n%s", got)
	}
}

func TestMetaLineAllFailedHasNoFindingCount(t *testing.T) {
	skills := []Skill{
		{Name: "a", Verdict: VerdictFailed, Failures: []Failure{{Message: "x"}}},
		{Name: "b", Verdict: VerdictFailed},
	}
	got := Comment(skills, Options{ChangedFiles: []string{"a.go"}})
	if strings.Contains(got, "no findings") || strings.Contains(got, "suggestion") {
		t.Errorf("a review where no skill finished counted findings:\n%s", head(got))
	}
	if !strings.Contains(got, "<sub>1 file</sub>\n\n") {
		t.Errorf("meta line missing:\n%s", head(got))
	}
}

func TestMetaLineSomeFailedKeepsFindingCount(t *testing.T) {
	skills := []Skill{
		{Name: "a", Verdict: VerdictFailed},
		cleanSkill("b", 0),
	}
	if got := Comment(skills, Options{}); !strings.Contains(got, "<sub>no findings</sub>") {
		t.Errorf("a finished skill's meta line lost its count:\n%s", head(got))
	}
}

func TestCapReasonNamesSkillWhenSeveral(t *testing.T) {
	f := []Finding{{File: "a.go", Severity: Blocking, Title: "T"}}
	skills := []Skill{
		{Name: "a", Verdict: VerdictHasBlockingIssues, Confidence: 5, Findings: f},
		{Name: "b", Verdict: VerdictNoBlockingIssues, Confidence: 5},
	}
	got := Comment(skills, Options{})
	if want := "> `a`: Capped: 1 blocking finding\n\n"; !strings.Contains(got, want) {
		t.Errorf("reason missing %q:\n%s", want, head(got))
	}
}

func TestLocationWithoutFileShowsNothing(t *testing.T) {
	calls := 0
	o := Options{FileURL: func(string, int, int) string { calls++; return "https://x/y" }}
	s := Skill{Name: "s", Verdict: VerdictHasBlockingIssues, Findings: []Finding{{Line: 4, Severity: Blocking, Title: "T", Explanation: "e"}}}
	got := Comment([]Skill{s}, o)
	if !strings.Contains(got, "<summary>❌ <b>T</b></summary>") {
		t.Errorf("row not just the title:\n%s", got)
	}
	if strings.Contains(got, "<a ") || strings.Contains(got, "<code></code>") || calls != 0 {
		t.Errorf("an empty file got a location (%d calls):\n%s", calls, got)
	}
}

func TestUntrustedFileURLWithWhitespaceIsNoLink(t *testing.T) {
	for _, bad := range []string{
		"https://x/a\n\n<!-- pergent:findings AAAA -->",
		"https://x/a b",
		"https://x/a\tb",
		"https://x/a\u0085b",
		"https://x/a\x00b",
	} {
		o := Options{FileURL: func(string, int, int) string { return bad }}
		s := Skill{Name: "s", Verdict: VerdictHasBlockingIssues, Findings: []Finding{{File: "a.go", Line: 4, Severity: Blocking, Title: "T"}}}
		got := Comment([]Skill{s}, o)
		if strings.Contains(got, "<a ") {
			t.Errorf("%q: a URL with whitespace became a link:\n%s", bad, got)
		}
		n := 0
		for _, line := range strings.Split(got, "\n") {
			if strings.HasPrefix(line, "<!-- pergent:findings") {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%q: %d findings-block lines, want 1:\n%s", bad, n, got)
		}
		if !strings.Contains(got, "<code>a.go:4</code>") {
			t.Errorf("%q: location lost:\n%s", bad, got)
		}
	}
}

// A finding's file is model text: an absolute path or one with a ".." segment
// could make a link that leads off the repository, so it is shown, not linked.
func TestFileURLSkipsAbsoluteAndParentPaths(t *testing.T) {
	o := Options{FileURL: GitHubFileURL("https://github.com/o/r", "abc1234")}
	for _, file := range []string{
		"../../../../evil/repo/blob/main/x.go", "a/../../b.go", "/etc/passwd", "..", "a/..", `a\..\b.go`, `..\b.go`, `\\host\share`,
	} {
		s := Skill{Name: "s", Verdict: VerdictHasBlockingIssues, Findings: []Finding{{File: file, Line: 1, Severity: Blocking, Title: "T"}}}
		got := Comment([]Skill{s}, o)
		if strings.Contains(got, "<a ") {
			t.Errorf("%q became a link:\n%s", file, got)
		}
		if want := "<code>" + escapeSummary(file) + ":1</code>"; !strings.Contains(got, want) {
			t.Errorf("%q: location text lost, want %s:\n%s", file, want, got)
		}
	}
	for _, file := range []string{"a.go", "dir/b.go", "dir/..b.go", "dir/a..b/c.go", ".github/ci.yml", "dir/.hidden"} {
		s := Skill{Name: "s", Verdict: VerdictHasBlockingIssues, Findings: []Finding{{File: file, Line: 1, Severity: Blocking, Title: "T"}}}
		if got := Comment([]Skill{s}, o); !strings.Contains(got, `<a href="https://github.com/o/r/blob/abc1234/`) {
			t.Errorf("%q is not linked:\n%s", file, got)
		}
	}
}

func TestNoteTextNeutralizesModelText(t *testing.T) {
	const j = joiner // what lineText puts inside live sequences
	cases := []struct {
		name, note string
		want       string   // the note as it appears in the comment
		banned     []string // must not appear anywhere in the comment
	}{
		{"mention", "Missing @bob", "Missing @" + j + "bob", []string{"@bob"}},
		{"decimal entity", "ping &#64;bob", "ping &" + j + "#64;bob", []string{"&#64;"}},
		{"hex entity", "ping &#x40;bob", "ping &" + j + "#x40;bob", []string{"&#x40;"}},
		{"named entity", "ping &commat;bob", "ping &" + j + "commat;bob", []string{"&commat;"}},
		{"image", "see ![x](http://e/x.png)", "see !" + j + "[x]" + j + "(http://e/x.png)", []string{"![", "](", "<img"}},
		{"link", "[click](http://e)", j + "[click]" + j + "(http://e)", []string{"](http://e)"}},
		{"wiki link", "[[click|http://e]]", j + "[" + j + "[click|" + j + "http://e]" + j + "]" + j, []string{"[[click"}},
		{"wiki link run", "[[[click|http://e]]]", j + "[" + j + "[" + j + "[click|" + j + "http://e]" + j + "]" + j + "]" + j, []string{"[[click", "[[["}},
		{"image wiki link", "![[click|http://e]]", "!" + j + "[" + j + "[click|" + j + "http://e]" + j + "]" + j, []string{"![", "[[click"}},
		{"html", "<img src=x>", "<" + j + "img src=x>", []string{"<img"}},
		{"comment", "<!-- x", "<" + j + "!-- x", []string{"<!-- x"}},
		{"details", "</details>", "<" + j + "/details>", []string{"</details>"}},
		{"newlines", "line one\n\n# line two", "line one # line two", []string{"\n# line two"}},
		{"plain", "Dropped `Foo.bar` (not real)", "Dropped `Foo.bar` (not real)", nil},
		// A code span is code: it is kept as written, with no entity, backslash or joiner.
		{"code angle", "Dropped `Map<String, Foo>`", "Dropped `Map<String, Foo>`", []string{"&lt;", "&gt;"}},
		{"code call", "Dropped `handlers[i](ctx)`", "Dropped `handlers[i](ctx)`", []string{`\(`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := Skill{Name: "a", Verdict: VerdictNoBlockingIssues, Notes: []string{tc.note}}
			got := Comment([]Skill{s}, Options{})
			if want := "> " + tc.want + "\n"; !strings.Contains(got, want) {
				t.Errorf("lacks %q:\n%s", want, got)
			}
			for _, banned := range tc.banned {
				if strings.Contains(got, banned) {
					t.Errorf("live %q in:\n%s", banned, got)
				}
			}
			// Apart from the joiner the note reads as written.
			if line := noteLine(got); strings.ReplaceAll(line, j, "") != oneLine(tc.note) {
				t.Errorf("note changed beyond the joiner: %q", line)
			}
		})
	}

	// With several skills the prefix is added outside the escaping.
	a := Skill{Name: "a", Verdict: VerdictNoBlockingIssues, Notes: []string{"ping @bob <b>"}}
	got := Comment([]Skill{a, cleanSkill("b", 0)}, Options{})
	if want := "> `a`: ping @" + j + "bob <" + j + "b>\n"; !strings.Contains(got, want) {
		t.Errorf("lacks %q:\n%s", want, got)
	}
}

// noteLine returns the text of the comment's first quote line.
func noteLine(comment string) string {
	for _, l := range strings.Split(comment, "\n") {
		if strings.HasPrefix(l, "> ") {
			return strings.TrimPrefix(l, "> ")
		}
	}
	return ""
}
