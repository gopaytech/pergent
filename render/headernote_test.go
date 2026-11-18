package render

import (
	"strings"
	"testing"
)

const testHeaderNote = "CI: test passed · cycode-scan failed (blocks approval)"

func headerNoteLine(t *testing.T, c string) int {
	t.Helper()
	i := strings.Index(c, "\n_"+testHeaderNote+"_\n")
	if i < 0 {
		t.Fatalf("header note missing:\n%s", c)
	}
	return i
}

func TestHeaderNoteWrittenAfterHeader(t *testing.T) {
	c := Comment([]Skill{layoutSkill("code-review")}, Options{HeaderNote: testHeaderNote})
	i := headerNoteLine(t, c)
	if i > strings.Index(c, "**Summary:**") {
		t.Fatal(c)
	}
	if !strings.Contains(c, "_"+testHeaderNote+"_\n\n") {
		t.Fatalf("note not followed by a blank line:\n%s", c)
	}
}

func TestHeaderNoteEmptyChangesNothing(t *testing.T) {
	skills := []Skill{layoutSkill("code-review")}
	if Comment(skills, Options{}) != Comment(skills, Options{HeaderNote: ""}) {
		t.Fatal("empty note changed the comment")
	}
}

func TestHeaderNoteIsInert(t *testing.T) {
	c := Comment([]Skill{layoutSkill("code-review")}, Options{HeaderNote: "# x\n@team y"})
	var line string
	for _, l := range strings.Split(c, "\n") {
		if strings.HasPrefix(l, "_") && strings.Contains(l, "team y") {
			line = l
		}
	}
	if line == "" || !strings.HasSuffix(line, "_") {
		t.Fatalf("no one-line note:\n%s", c)
	}
	if strings.Contains(c, "@team") {
		t.Fatalf("raw mention:\n%s", c)
	}
	if strings.Contains(c, "\n# x") || strings.Contains(c, "\n#x") {
		t.Fatalf("heading line:\n%s", c)
	}
	for _, l := range strings.Split(c, "\n") {
		if strings.Contains(l, "team y") && l != line {
			t.Fatalf("note spans lines:\n%s", c)
		}
	}
}

func TestHeaderNoteSurvivesShortLayoutAndLimit(t *testing.T) {
	skills, _ := bulkSkills()
	o := Options{HeaderNote: testHeaderNote}

	short := o
	short.Short = true
	headerNoteLine(t, Comment(skills, short))

	noSummary := o
	noSummary.Layout = lay(t, "findings:open")
	headerNoteLine(t, Comment(skills, noSummary))

	full := len(Comment(skills, o))
	for _, max := range []int{full - 1, full / 2, full / 4, 1500} {
		lim := o
		lim.MaxLen = max
		c := Comment(skills, lim)
		if len(c) > max {
			t.Fatalf("MaxLen %d: got %d bytes", max, len(c))
		}
		headerNoteLine(t, c)
	}
}

func TestHeaderNoteWithFailures(t *testing.T) {
	skills := []Skill{{Name: "a", Verdict: VerdictFailed, Failures: []Failure{{Message: "boom"}}}}
	c := Comment(skills, Options{HeaderNote: testHeaderNote})
	f := strings.Index(c, "boom")
	n := headerNoteLine(t, c)
	if f < 0 || n < f {
		t.Fatalf("note not after failures:\n%s", c)
	}
}
