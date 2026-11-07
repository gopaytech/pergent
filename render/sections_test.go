package render

import (
	"fmt"
	"strings"
	"testing"
)

// mustIndex returns the index of sub in s, failing the test when it is missing.
func mustIndex(t *testing.T, s, sub string) int {
	t.Helper()
	i := strings.Index(s, sub)
	if i < 0 {
		t.Fatalf("comment lacks %q:\n%s", sub, s)
	}
	return i
}

// inOrder fails unless every part is in the comment, each after the one before.
func inOrder(t *testing.T, s string, parts ...string) {
	t.Helper()
	prev, prevPart := -1, ""
	for _, p := range parts {
		i := strings.Index(s, p)
		if i < 0 {
			t.Fatalf("comment lacks %q:\n%s", p, s)
		}
		if i <= prev {
			t.Fatalf("%q is not after %q:\n%s", p, prevPart, s)
		}
		prev, prevPart = i, p
	}
}

func checksOf(batch int, cs ...Check) BatchChecks { return BatchChecks{Batch: batch, Checks: cs} }

func chk(section, name, status string) Check {
	return Check{Section: section, Name: name, Status: status, Detail: name + " detail"}
}

// summarySkill is one skill with a finding, a failure, a summary and checks.
func summarySkill() Skill {
	return Skill{
		Name: "code-review", Verdict: VerdictNoBlockingIssues,
		Batches: 1, BatchesFinished: 1,
		Summaries: []Summary{{Batch: 1, Text: "Checked the handler."}},
		Findings:  []Finding{{File: "a.go", Line: 3, Severity: Suggestion, Title: "Rename it"}},
		Failures:  []Failure{{Message: "A batch failed."}},
		Checks:    []BatchChecks{checksOf(1, chk("Plan", "Plan read", CheckPass))},
	}
}

func TestSummaryOneBatch(t *testing.T) {
	got := Comment([]Skill{summarySkill()}, Options{})
	inOrder(t, got, "**Review failed:** A batch failed.", "**Summary:** Checked the handler.\n\n", "#### Findings")
}

func TestSummaryBatchList(t *testing.T) {
	s := summarySkill()
	s.Batches, s.BatchesFinished = 3, 3
	s.Summaries = []Summary{{Batch: 1, Text: "First."}, {Batch: 3, Text: "Third."}}
	got := Comment([]Skill{s}, Options{})
	inOrder(t, got, "#### Findings", "Rename it", "- **Batch 1 of 3:** First.\n- **Batch 3 of 3:** Third.\n\n", "#### Plan")
	if strings.Contains(got, "**Summary:**") {
		t.Errorf("a batch list also has a Summary line:\n%s", got)
	}
}

func TestBatchesThreeOneSummary(t *testing.T) {
	s := summarySkill()
	s.Batches, s.BatchesFinished = 3, 1
	got := Comment([]Skill{s}, Options{})
	if !strings.Contains(got, "- **Batch 1 of 3:** Checked the handler.\n") || strings.Contains(got, "**Summary:**") {
		t.Errorf("one summary of three batches is not a batch list:\n%s", got)
	}
}

func TestNoSummaryWritesNothing(t *testing.T) {
	s := summarySkill()
	s.Summaries = nil
	if got := Comment([]Skill{s}, Options{}); strings.Contains(got, "Summary") || strings.Contains(got, "Batch 1 of") {
		t.Errorf("no summary, yet the comment names one:\n%s", got)
	}
	s.Summaries = []Summary{{Batch: 1, Text: " \n\t "}}
	if got := Comment([]Skill{s}, Options{}); strings.Contains(got, "Summary") {
		t.Errorf("a blank summary writes an empty label:\n%s", got)
	}
}

func TestSummaryIsInert(t *testing.T) {
	text := "# x\n> y @alice <img src=a> [l](u)"
	for _, batches := range []int{1, 3} {
		s := summarySkill()
		s.Batches = batches
		s.Summaries = []Summary{{Batch: 1, Text: text}}
		got := Comment([]Skill{s}, Options{})
		prefix := "**Summary:** "
		if batches > 1 {
			prefix = "- **Batch 1 of 3:** "
		}
		i := mustIndex(t, got, prefix)
		line := got[i:]
		if j := strings.Index(line, "\n"); j >= 0 {
			line = line[:j]
		}
		if rest := got[i+len(line):]; !strings.HasPrefix(rest, "\n") || strings.HasPrefix(rest, "\n"+"# x") || strings.HasPrefix(rest, "\n> y") {
			t.Errorf("summary spills past its line:\n%q", got[i:])
		}
		for _, bad := range []string{"@alice", "<img", "](u)"} {
			if strings.Contains(line, bad) {
				t.Errorf("summary line holds a live %q: %q", bad, line)
			}
		}
		for _, l := range strings.Split(got, "\n") {
			if strings.HasPrefix(l, "# x") || strings.HasPrefix(l, "> y") || strings.HasPrefix(l, "y ") {
				t.Errorf("a line of the comment starts with model text: %q", l)
			}
		}
	}
}

func TestAreaHeadingIsInert(t *testing.T) {
	s := Skill{Name: "s", Verdict: VerdictNoBlockingIssues, Checks: []BatchChecks{
		checksOf(1, chk("# x\n@bob <b>", "c", CheckPass)),
	}}
	got := Comment([]Skill{s}, Options{})
	want := "#### # x @" + joiner + "bob <" + joiner + "b>\n"
	if !strings.Contains(got, "\n"+want) {
		t.Errorf("heading is not %q:\n%s", want, got)
	}
	for _, bad := range []string{"@bob", "<b>"} {
		if strings.Contains(got, bad) {
			t.Errorf("comment holds a live %q:\n%s", bad, got)
		}
	}
	for _, l := range strings.Split(got, "\n") {
		if strings.HasPrefix(l, "@") || strings.HasPrefix(l, "# x") {
			t.Errorf("a line starts with model text: %q", l)
		}
	}
}

func TestReasonOnlyBelowFive(t *testing.T) {
	skill := func(score int, findings ...Finding) Skill {
		v := VerdictNoBlockingIssues
		if len(findings) > 0 {
			v = VerdictHasBlockingIssues
		}
		return Skill{Name: "s", Verdict: v, Confidence: score, ConfidenceReason: "The reason.", Findings: findings}
	}
	blocking := Finding{File: "a.go", Line: 1, Severity: Blocking, Title: "T"}
	for _, c := range []struct {
		name string
		s    Skill
		want bool
	}{
		{"five", skill(5), false},
		{"four", skill(4), true},
		{"capped", skill(5, blocking), true},
		{"unscored", skill(0), false},
	} {
		got := Comment([]Skill{c.s}, Options{})
		if has := strings.Contains(got, "\n> The reason."); has != c.want {
			t.Errorf("%s: reason shown = %v, want %v:\n%s", c.name, has, c.want, got)
		}
	}
}

func TestChecksByArea(t *testing.T) {
	s := Skill{Name: "s", Verdict: VerdictNoBlockingIssues, Checks: []BatchChecks{
		checksOf(1, chk("Plan", "Plan read", CheckPass), chk("Plan", "Plan sound", CheckFail)),
	}}
	got := Comment([]Skill{s}, Options{})
	inOrder(t, got, "\n#### Plan\n\n| Check | Status | Detail |\n|---|---|---|\n", "| Plan sound | ❌ | Plan sound detail |")
	for _, banned := range []string{"<summary>Checks:", "Failed checks (", "| Area |", "| Skill |"} {
		if strings.Contains(got, banned) {
			t.Errorf("comment holds %q:\n%s", banned, got)
		}
	}
}

func TestFailedFirstInArea(t *testing.T) {
	s := Skill{Name: "s", Verdict: VerdictNoBlockingIssues, Checks: []BatchChecks{
		checksOf(1,
			chk("B", "b-pass", CheckPass),
			chk("A", "a-pass", CheckPass), chk("A", "a-fail", CheckFail), chk("A", "a-na", CheckNA),
			chk("B", "b-fail", CheckFail)),
	}}
	got := Comment([]Skill{s}, Options{})
	inOrder(t, got, "#### B", "| b-fail ", "| b-pass ", "#### A", "| a-fail ", "| a-pass ", "| a-na ")
}

func TestChecksWithNoAreaLast(t *testing.T) {
	mk := func(cs ...Check) []Skill {
		return []Skill{{Name: "s", Verdict: VerdictNoBlockingIssues, Checks: []BatchChecks{checksOf(1, cs...)}}}
	}
	t.Run("beside a named area", func(t *testing.T) {
		got := Comment(mk(chk("", "loose", CheckPass), chk("A", "a-pass", CheckPass), chk("", "loose-fail", CheckFail)), Options{})
		inOrder(t, got, "\n#### A\n\n", "| a-pass ", "\n#### Other checks\n\n| Check | Status | Detail |", "| loose-fail ", "| loose ")
		if n := strings.Count(got, "| Check | Status | Detail |"); n != 2 {
			t.Errorf("%d tables, want 2 (one per group):\n%s", n, got)
		}
		if n := strings.Count(got, "\n####"); n != 2 {
			t.Errorf("%d headings, want 2:\n%s", n, got)
		}
	})
	t.Run("alone", func(t *testing.T) {
		got := Comment(mk(chk("", "loose", CheckPass), chk("  ", "blank", CheckFail)), Options{})
		if strings.Contains(got, "Other checks") || strings.Contains(got, "\n####") {
			t.Errorf("checks with no area alone have a heading:\n%s", got)
		}
		if n := strings.Count(got, "| Check | Status | Detail |"); n != 1 {
			t.Errorf("%d tables, want 1:\n%s", n, got)
		}
	})
	t.Run("in a skill section", func(t *testing.T) {
		got := Comment(append(mk(chk("A", "a1", CheckPass), chk("", "loose", CheckPass)), cleanSkill("t", 0)), Options{})
		inOrder(t, got, "\n#### `s`\n\n##### A\n\n", "\n##### Other checks\n\n", "| loose ")
	})
	cs := []Check{chk("A", "a-pass", CheckPass), chk("B", "b-fail", CheckFail), chk("", "loose-fail", CheckFail)}
	t.Run("Short keeps it beside a shown area", func(t *testing.T) {
		got := Comment(mk(cs...), Options{Short: true})
		inOrder(t, got, "\n#### B\n", "| b-fail ", "\n#### Other checks\n", "| loose-fail ")
	})
	// The named areas that are left are not the ones that make the heading.
	only := []Check{chk("A", "a-pass", CheckPass), chk("", "loose-fail", CheckFail)}
	t.Run("Short drops it with the last named area", func(t *testing.T) {
		got := Comment(mk(only...), Options{Short: true})
		if strings.Contains(got, "Other checks") || strings.Contains(got, "#### A") || !strings.Contains(got, "| loose-fail ") {
			t.Errorf("heading left alone by Short:\n%s", got)
		}
	})
	t.Run("step 4 drops it with the last named area", func(t *testing.T) {
		got := render(mk(only...), Options{}, shrinkSteps[3])
		if strings.Contains(got, "Other checks") || strings.Contains(got, "#### A") || !strings.Contains(got, "| loose-fail ") {
			t.Errorf("heading left alone by step 4:\n%s", got)
		}
	})
}

func TestBatchesStayOneRow(t *testing.T) {
	s := Skill{Name: "s", Verdict: VerdictNoBlockingIssues, Batches: 2, Checks: []BatchChecks{
		checksOf(1, chk("A", "same", CheckPass)),
		{Batch: 2, Findings: 1, Checks: []Check{{Section: "A", Name: "same", Status: CheckFail, Detail: "second"}}},
	}}
	got := Comment([]Skill{s}, Options{})
	if n := strings.Count(got, "| same |"); n != 1 {
		t.Fatalf("%d rows for one check:\n%s", n, got)
	}
	if !strings.Contains(got, "| same | ❌ | second |") {
		t.Errorf("the row is not the worst status:\n%s", got)
	}
	if !strings.Contains(got, "0 of 1 checks passed") {
		t.Errorf("the meta line changed:\n%s", got)
	}
}

func TestSkillSections(t *testing.T) {
	mk := func(name string) Skill {
		return Skill{
			Name: name, Verdict: VerdictNoBlockingIssues, Batches: 1,
			Summaries: []Summary{{Batch: 1, Text: name + " summary."}},
			Checks:    []BatchChecks{checksOf(1, chk("Plan", name+"-check", CheckPass))},
		}
	}
	bare := Skill{Name: "gamma", Verdict: VerdictNoBlockingIssues}
	got := Comment([]Skill{mk("beta"), mk("alpha"), bare}, Options{})
	inOrder(t, got,
		"\n#### `beta`\n\n**Summary:** beta summary.\n\n##### Plan\n\n| Check | Status | Detail |", "| beta-check ",
		"\n#### `alpha`\n\n**Summary:** alpha summary.\n\n##### Plan\n\n", "| alpha-check ")
	if strings.Contains(got, "\n#### `gamma`") {
		t.Errorf("a skill with nothing to show has a section:\n%s", got)
	}
	// The single-skill Summary line is not written above the findings.
	if strings.Index(got, "**Summary:**") < strings.Index(got, "| Skill |") {
		t.Errorf("a summary is above the skills table:\n%s", got)
	}
}

func TestSkillSectionBatchList(t *testing.T) {
	a := Skill{Name: "a", Verdict: VerdictNoBlockingIssues, Batches: 2, Summaries: []Summary{{Batch: 1, Text: "one"}, {Batch: 2, Text: "two"}}}
	b := cleanSkill("b", 0)
	got := Comment([]Skill{a, b}, Options{})
	inOrder(t, got, "\n#### `a`\n\n- **Batch 1 of 2:** one\n- **Batch 2 of 2:** two\n\n")
}

func TestNotesAfterFiles(t *testing.T) {
	s := Skill{Name: "s", Verdict: VerdictNoBlockingIssues, Reviewed: []string{"a.go"}, Checks: []BatchChecks{
		checksOf(1, chk("A", "broken", CheckFail)),
	}}
	got := Comment([]Skill{s}, Options{ChangedFiles: []string{"a.go"}, Version: "v9"})
	inOrder(t, got, "| broken ", "<summary>Files (1)</summary>", "> 1 failed check has no matching finding", "pergent v9", "<!-- pergent:findings ")
}

func TestChecksLabel(t *testing.T) {
	rows := []checkRow{{Check: Check{Status: CheckPass}}, {Check: Check{Status: CheckPass}}, {Check: Check{Status: CheckFail}}, {Check: Check{Status: CheckNA}}}
	if got, want := checksLabel(rows), "Checks: 2 of 3 passed, 1 n/a"; got != want {
		t.Errorf("label = %q, want %q", got, want)
	}
	if got, want := checksLabel(rows[3:]), "Checks: 1 n/a"; got != want {
		t.Errorf("label = %q, want %q", got, want)
	}
}

// sizedSkill is a review that holds every part the size limit cuts: a blocking
// finding, a suggestion, a summary, passed and failed checks and many files.
func sizedSkill() (Skill, Options) {
	files := make([]string, 120)
	for i := range files {
		files[i] = fmt.Sprintf("pkg/file%03d.go", i)
	}
	pad := strings.Repeat("padding ", 40)
	s := Skill{
		Name: "s", Verdict: VerdictHasBlockingIssues, Batches: 1, BatchesFinished: 1, Reviewed: files,
		Summaries: []Summary{{Batch: 1, Text: "KEEPSUMMARY " + pad}},
		Findings: []Finding{
			{File: "pkg/file001.go", Line: 1, Severity: Blocking, Title: "BLOCKTITLE", Explanation: "e"},
			{File: "pkg/file002.go", Line: 2, Severity: Suggestion, Title: "A suggestion", Explanation: pad},
		},
		Checks: []BatchChecks{{Batch: 1, Findings: 1, Checks: []Check{
			{Section: "Tests", Name: "t-pass", Status: CheckPass, Detail: pad},
			{Section: "Safety", Name: "s-fail", Status: CheckFail, Detail: pad},
		}}},
	}
	return s, Options{ChangedFiles: files}
}

func TestSummariesCutLast(t *testing.T) {
	s, o := sizedSkill()
	full := len(Comment([]Skill{s}, o))
	var noted, fileCutKept bool
	for maxLen := full; maxLen > 300; maxLen -= 3 {
		o.MaxLen = maxLen
		got := Comment([]Skill{s}, o)
		if strings.Contains(got, "Comment truncated") {
			continue
		}
		hasSummary := strings.Contains(got, "KEEPSUMMARY")
		hasNote := strings.Contains(got, summariesDroppedNote)
		if hasSummary == hasNote {
			t.Fatalf("maxLen %d: summary present = %v, note present = %v:\n%s", maxLen, hasSummary, hasNote, got)
		}
		if !strings.Contains(got, "BLOCKTITLE") {
			t.Fatalf("maxLen %d: the blocking finding is gone:\n%s", maxLen, got)
		}
		if hasNote {
			noted = true
			if !strings.Contains(got, "more not listed to fit") {
				t.Errorf("maxLen %d: summaries were cut before the file list was a count:\n%s", maxLen, got)
			}
		}
		if hasSummary && strings.Contains(got, "more not listed to fit") {
			fileCutKept = true
		}
	}
	if !noted || !fileCutKept {
		t.Errorf("sweep saw note = %v and a cut file list with the summary kept = %v", noted, fileCutKept)
	}
}

func TestEmptyAreaLosesItsHeading(t *testing.T) {
	s, o := sizedSkill()
	full := render([]Skill{s}, o, layout{listLimit: -1})
	inOrder(t, full, "#### Tests", "t-pass", "#### Safety", "s-fail")
	got := render([]Skill{s}, o, shrinkSteps[3])
	if strings.Contains(got, "#### Tests") || strings.Contains(got, "t-pass") {
		t.Errorf("step 4 keeps an all-passed area:\n%s", got)
	}
	inOrder(t, got, "#### Safety", "s-fail", passedChecksDroppedNote)
}

func TestEmptySectionIsNotWritten(t *testing.T) {
	onlyChecks := Skill{Name: "onlychecks", Verdict: VerdictNoBlockingIssues, Checks: []BatchChecks{checksOf(1, chk("A", "a1", CheckFail))}}
	withSummary := Skill{Name: "withsummary", Verdict: VerdictNoBlockingIssues, Batches: 1, Summaries: []Summary{{Batch: 1, Text: "kept"}}}
	skills := []Skill{onlyChecks, withSummary}
	if got := render(skills, Options{}, layout{listLimit: -1}); !strings.Contains(got, "\n#### `onlychecks`\n") {
		t.Fatalf("the section is missing before the cut:\n%s", got)
	}
	got := render(skills, Options{}, shrinkSteps[4])
	if strings.Contains(got, "`onlychecks`\n") || strings.Contains(got, "a1") {
		t.Errorf("step 5 keeps an empty section:\n%s", got)
	}
	inOrder(t, got, "\n#### `withsummary`\n\n**Summary:** kept\n", checkTablesDroppedNote)
}

func TestShortKeepsSummaryAndFailedChecks(t *testing.T) {
	s := Skill{
		Name: "s", Verdict: VerdictNoBlockingIssues, Batches: 1,
		Summaries: []Summary{{Batch: 1, Text: "Short summary."}},
		Checks: []BatchChecks{{Batch: 1, Findings: 1, Checks: []Check{
			chk("Tests", "Unit tests ran", CheckPass), chk("Docs", "Docs updated", CheckNA), chk("Safety", "No panics", CheckFail),
		}}},
	}
	got := Comment([]Skill{s}, Options{Short: true, ChangedFiles: []string{"a.go"}})
	inOrder(t, got, "**Summary:** Short summary.", "#### Safety", "| No panics ")
	for _, banned := range []string{"Unit tests ran", "Docs updated", "#### Tests", "#### Docs", "omitted", "Files ("} {
		if strings.Contains(got, banned) {
			t.Errorf("Short comment holds %q:\n%s", banned, got)
		}
	}
	// A section with nothing left is not written either.
	other := Skill{Name: "other", Verdict: VerdictNoBlockingIssues, Checks: []BatchChecks{checksOf(1, chk("Tests", "x-pass", CheckPass))}}
	got = Comment([]Skill{s, other}, Options{Short: true})
	if strings.Contains(got, "\n#### `other`") || strings.Contains(got, "x-pass") {
		t.Errorf("Short writes an empty section:\n%s", got)
	}
	if !strings.Contains(got, "\n#### `s`\n\n**Summary:** Short summary.") {
		t.Errorf("Short lost the section that has something:\n%s", got)
	}
}
