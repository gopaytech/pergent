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
	if want := "> `b`: &lt;b>risky&lt;/b>\n\n"; !strings.Contains(got, want) {
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
		`<a href="https://github.com/o/r/blob/sha1/role.yaml#L17-L19"><code>role.yaml:17–19</code></a>`,
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
	if !strings.Contains(got, "</summary>\n\n_off the &lt;diff>_\nE\n\n") {
		t.Errorf("note not at the top of the body:\n%s", got)
	}
}

func TestSummariesNotShown(t *testing.T) {
	s := Skill{Name: "s", Verdict: VerdictNoBlockingIssues, Batches: 1,
		Summaries: []Summary{{Batch: 1, Text: "Reviewed the widget carefully."}}}
	got := Comment([]Skill{s}, Options{})
	if strings.Contains(got, "widget") || strings.Contains(got, "Summary") {
		t.Errorf("summary shown:\n%s", got)
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
	if !strings.Contains(out, detailsDroppedNote+"\n\n- 💡 <b>S</b> · <code>b.go:2</code>\n") || strings.Contains(out, "suggbody") || !strings.Contains(out, "blockbody") {
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
