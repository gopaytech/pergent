package render

import (
	"strings"
	"testing"
)

func deletedSkill(name string, findings ...Finding) Skill {
	return Skill{
		Name: name, Verdict: VerdictHasBlockingIssues, Findings: findings,
		Reviewed: []string{"old.go"}, Deleted: []string{"old.go"},
	}
}

func TestDeletedFindingHasNoLink(t *testing.T) {
	o := Options{FileURL: GitHubFileURL("https://github.com/o/r", "abc123")}
	cases := []struct {
		name string
		f    Finding
		want string
	}{
		{"line", Finding{File: "old.go", Line: 3}, "<code>old.go (deleted), line 3</code>"},
		{"range", Finding{File: "old.go", Line: 3, EndLine: 5}, "<code>old.go (deleted), lines 3–5</code>"},
		{"no line", Finding{File: "old.go"}, "<code>old.go (deleted)</code>"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			tt.f.Severity, tt.f.Title, tt.f.Explanation = Blocking, "Still used", "e"
			out := Comment([]Skill{deletedSkill("s", tt.f)}, o)
			if !strings.Contains(out, tt.want) {
				t.Errorf("missing %q:\n%s", tt.want, out)
			}
			if strings.Contains(out, "<a ") {
				t.Errorf("a finding on a deleted file got a link:\n%s", out)
			}
		})
	}
}

func TestOtherFindingKeepsItsLink(t *testing.T) {
	o := Options{FileURL: GitHubFileURL("https://github.com/o/r", "abc123")}
	f := Finding{File: "main.go", Line: 7, Severity: Blocking, Title: "Still used", Explanation: "e"}
	s := deletedSkill("s", f)
	out := Comment([]Skill{s}, o)
	want := `<a href="https://github.com/o/r/blob/abc123/main.go?plain=1#L7"><code>main.go:7</code></a>`
	if !strings.Contains(out, want) {
		t.Errorf("a finding on a file that was not deleted lost its link:\n%s", out)
	}
}

// A file deleted in one skill's review counts for every skill's findings on it.
func TestDeletedFindingFromAnotherSkill(t *testing.T) {
	f := Finding{File: "old.go", Line: 3, Severity: Blocking, Title: "T", Explanation: "e"}
	other := Skill{Name: "other", Verdict: VerdictHasBlockingIssues, Findings: []Finding{f}}
	out := Comment([]Skill{deletedSkill("s"), other}, Options{})
	if !strings.Contains(out, "<code>old.go (deleted), line 3</code>") {
		t.Errorf("finding of the other skill not marked:\n%s", out)
	}
}

func TestDeletedFindingHasNoSuggestionBlock(t *testing.T) {
	f := Finding{File: "old.go", Line: 3, Severity: Blocking, Title: "Still used",
		SuggestedFix: "Remove the import.", Suggestion: "x := 1"}
	out := Comment([]Skill{deletedSkill("s", f)}, Options{})
	if strings.Contains(out, "Suggested change") || strings.Contains(out, "x := 1") {
		t.Errorf("a finding on a deleted file shows a suggestion block:\n%s", out)
	}
	if !strings.Contains(out, "**Fix:** Remove the import.") {
		t.Errorf("the fix prose is gone:\n%s", out)
	}
	// Another file keeps its block.
	g := Finding{File: "main.go", Line: 3, Severity: Blocking, Title: "T", Suggestion: "y := 2"}
	out = Comment([]Skill{deletedSkill("s", f, g)}, Options{})
	if !strings.Contains(out, "_Suggested change, replacing line 3:_\n\n```\ny := 2\n```") || strings.Contains(out, "x := 1") {
		t.Errorf("suggestion blocks wrong:\n%s", out)
	}
}

func TestFilesListMarks(t *testing.T) {
	s := Skill{
		Name: "s", Verdict: VerdictNoBlockingIssues,
		Reviewed: []string{"old.go", "new.go", "keep.go"},
		Deleted:  []string{"old.go"},
		Renamed:  []Rename{{Path: "new.go", From: "was.go"}},
	}
	out := Comment([]Skill{s}, Options{ChangedFiles: []string{"old.go", "new.go", "keep.go"}})
	for _, want := range []string{"- `old.go` (deleted)\n", "- `new.go` (renamed from `was.go`)\n", "- `keep.go`\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestFilesListMarksBeforeSkipSuffix(t *testing.T) {
	a := Skill{Name: "a", Verdict: VerdictNoBlockingIssues, Reviewed: []string{"old.go", "new.go"},
		Deleted: []string{"old.go"}, Renamed: []Rename{{Path: "new.go", From: "was.go"}}}
	b := Skill{Name: "b", Verdict: VerdictNoBlockingIssues,
		Skipped: []Skipped{{Path: "old.go", Rule: "out of scope"}, {Path: "new.go", Rule: "out of scope"}}}
	out := Comment([]Skill{a, b}, Options{})
	for _, want := range []string{
		"- `old.go` (deleted) — reviewed by `a`; skipped by `b` (out of scope)\n",
		"- `new.go` (renamed from `was.go`) — reviewed by `a`; skipped by `b` (out of scope)\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	// Single skill: the mark comes before the skip suffix too.
	one := Skill{Name: "s", Verdict: VerdictNoBlockingIssues, Deleted: []string{"old.go"},
		Skipped: []Skipped{{Path: "old.go", Rule: "deleted"}}}
	out = Comment([]Skill{one}, Options{})
	if !strings.Contains(out, "- `old.go` (deleted) — skipped: deleted\n") {
		t.Errorf("single skill:\n%s", out)
	}
}

func TestFilesListShortHasNoMarks(t *testing.T) {
	s := deletedSkill("s")
	out := Comment([]Skill{s}, Options{Short: true})
	if strings.Contains(out, "(deleted)") {
		t.Errorf("Short shows a mark:\n%s", out)
	}
}

func TestDeletedMarksEscapePaths(t *testing.T) {
	nasty := "we`ird|<b>\nx.go"
	old := "ol`d <i>|\n.go"
	f := Finding{File: nasty, Line: 2, Severity: Blocking, Title: "T", Explanation: "e"}
	s := Skill{
		Name: "s", Verdict: VerdictHasBlockingIssues, Findings: []Finding{f},
		Reviewed: []string{nasty, "n.go"}, Deleted: []string{nasty},
		Renamed: []Rename{{Path: "n.go", From: old}},
	}
	out := Comment([]Skill{s}, Options{FileURL: GitHubFileURL("https://github.com/o/r", "abc")})
	if !strings.Contains(out, "<code>we`ird|&lt;b&gt; x.go (deleted), line 2</code>") {
		t.Errorf("finding location not escaped:\n%s", out)
	}
	if !strings.Contains(out, "- ``` we`ird|<b> x.go ``` (deleted)\n") && !strings.Contains(out, "- `` we`ird|<b> x.go `` (deleted)\n") {
		t.Errorf("Files entry not a code span with the mark after it:\n%s", out)
	}
	if !strings.Contains(out, "(renamed from `` ol`d <i>| .go ``)") {
		t.Errorf("rename source not a code span:\n%s", out)
	}
	if strings.Contains(out, "\n.go") || strings.Contains(out, "\nx.go") {
		t.Errorf("a newline in a path broke a line:\n%s", out)
	}
}

// A rename-only skill has files reviewed and no confidence score.
func TestRenameOnlySkillHeader(t *testing.T) {
	s := Skill{Name: "s", Verdict: VerdictNoBlockingIssues, Reviewed: []string{"new.go"},
		Renamed: []Rename{{Path: "new.go", From: "was.go"}}}
	out := Comment([]Skill{s}, Options{})
	if strings.Contains(out, "nothing reviewed") || strings.Contains(out, "Nothing reviewed") || strings.Contains(out, "error") {
		t.Errorf("rename-only skill shows as an error:\n%s", out)
	}
}
