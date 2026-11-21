package render

import (
	"fmt"
	"strings"
	"testing"
)

func goFiles(n int, prefix string) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%s%d.go", prefix, i)
	}
	return out
}

func withRules(name string, paths []string, rule string) Skill {
	s := Skill{Name: name, Verdict: VerdictNoBlockingIssues, Findings: []Finding{}, Reviewed: paths, Batches: 1, BatchesFinished: 1}
	for _, p := range paths {
		s.FileRules = append(s.FileRules, FileRule{Path: p, Rule: rule})
	}
	return s
}

// filesBlock is the comment's Files block.
func filesBlock(t *testing.T, c string) string {
	t.Helper()
	i := strings.Index(c, "<summary>Files (")
	if i < 0 {
		t.Fatalf("no Files block in:\n%s", c)
	}
	j := strings.Index(c[i:], "</details>")
	return c[i : i+j+len("</details>")]
}

func TestRulesLine(t *testing.T) {
	a := withRules("a", goFiles(12, "pkg/"), "built-in:go")
	b := withRules("b", goFiles(3, "internal/api/"), "repo:internal/api/**/*.go")
	// A file listed under two skills counts once.
	b.Reviewed = append(b.Reviewed, "pkg/0.go")
	b.FileRules = append(b.FileRules, FileRule{Path: "pkg/0.go", Rule: "built-in:go"})
	block := filesBlock(t, Comment([]Skill{a, b}, Options{}))
	want := "\nRules: built-in:go (12 files) · repo `internal/api/**/*.go` (3 files)\n\n</details>"
	if !strings.HasSuffix(block, want) {
		t.Errorf("Files block ends\n%q\nwant suffix\n%q", block[max(0, len(block)-200):], want)
	}
}

func TestRulesLineOrderAndForms(t *testing.T) {
	s := withRules("s", []string{"a.go"}, "dashboard:base:**/*.go")
	s.Reviewed = append(s.Reviewed, "b.tf", "c.tf")
	s.FileRules = append(s.FileRules, FileRule{Path: "b.tf", Rule: "repo:*.tf"}, FileRule{Path: "c.tf", Rule: "repo:*.tf"})
	block := filesBlock(t, Comment([]Skill{s}, Options{}))
	want := "Rules: repo `*.tf` (2 files) · dashboard:base `**/*.go` (1 file)"
	if !strings.Contains(block, want) {
		t.Errorf("Files block:\n%s\nwant %q", block, want)
	}
}

func TestNoRulesNoLine(t *testing.T) {
	s := Skill{Name: "s", Verdict: VerdictNoBlockingIssues, Findings: []Finding{}, Reviewed: []string{"a.go"}, Batches: 1, BatchesFinished: 1}
	if c := Comment([]Skill{s}, Options{}); strings.Contains(c, "Rules:") {
		t.Errorf("a review with no rule has a Rules line:\n%s", c)
	}
}

func TestRulesLineEscapesGlob(t *testing.T) {
	s := withRules("s", []string{"a.go"}, "repo:a`b/**")
	block := filesBlock(t, Comment([]Skill{s}, Options{}))
	if !strings.Contains(block, "repo `` a`b/** `` (1 file)") {
		t.Errorf("glob not in a safe code span:\n%s", block)
	}
	odd := withRules("s", []string{"a.go"}, "dashboard:_x_:*.go")
	if block := filesBlock(t, Comment([]Skill{odd}, Options{})); !strings.Contains(block, "dashboard:`_x_` `*.go` (1 file)") {
		t.Errorf("set name with markdown characters not in a code span:\n%s", block)
	}
	unknown := withRules("s", []string{"a.go"}, "<b>new</b>")
	if block := filesBlock(t, Comment([]Skill{unknown}, Options{})); !strings.Contains(block, "Rules: `<b>new</b>` (1 file)") {
		t.Errorf("an unknown label is not in a code span:\n%s", block)
	}
}

func TestRulesLineShort(t *testing.T) {
	s := withRules("s", []string{"a.go"}, "repo:*.go")
	if c := Comment([]Skill{s}, Options{Short: true}); strings.Contains(c, "Rules:") {
		t.Errorf("Short has a Rules line:\n%s", c)
	}
}
