package render_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gopaytech/pergent/render"
)

// The names are swapped on purpose: the test proves roles are read, not order.
var panelNames = &render.PanelNames{A: "openai/model-b", B: "anthropic/model-a"}

func panelBothSkill() []render.Skill {
	return []render.Skill{{
		Name: "code-review", Verdict: render.VerdictHasBlockingIssues, Confidence: 3,
		Reviewed: []string{"a.go", "b.go", "c.go", "d.go", "e.go"},
		Panel:    &render.SkillPanel{Mode: "both", JudgeFailed: []string{"a"}},
		Findings: []render.Finding{
			{ID: "f1", Fingerprint: "fp1", File: "a.go", Line: 1, Severity: render.Blocking, Title: "Both found this",
				Explanation: "Seen twice.", Panel: &render.FindingPanel{FoundBy: "both", Also: []string{"fpx"}}},
			{ID: "f2", Fingerprint: "fp2", File: "b.go", Line: 2, Severity: render.Blocking, Title: "Only A found this",
				Explanation: "Seen once.", Panel: &render.FindingPanel{FoundBy: "a"}},
			{ID: "f3", Fingerprint: "fp3", File: "c.go", Line: 3, Severity: render.Suggestion, Title: "B demoted this",
				Explanation: "Doubtful.", Panel: &render.FindingPanel{FoundBy: "a", DisagreedBy: "b", Reason: "@someone says <b>no</b> here"}},
			{ID: "f4", Fingerprint: "fp4", File: "d.go", Line: 4, Severity: render.Blocking, Title: "Mixed ratings",
				Explanation: "Unsure.", Panel: &render.FindingPanel{FoundBy: "a", RatedSuggestionBy: "b"}},
			{ID: "f5", Fingerprint: "fp5", File: "e.go", Line: 5, Severity: render.Blocking, Title: "Only B found this",
				Explanation: "Not checked.", Panel: &render.FindingPanel{FoundBy: "b", UnjudgedBy: "a"}},
		},
	}}
}

func panelAOnlySkill() []render.Skill {
	return []render.Skill{{
		Name: "code-review", Verdict: render.VerdictHasBlockingIssues, Confidence: 3,
		Reviewed: []string{"a.go"},
		Panel:    &render.SkillPanel{Mode: "a_only"},
		Findings: []render.Finding{
			{ID: "f1", Fingerprint: "fp1", File: "a.go", Line: 1, Severity: render.Blocking, Title: "Only A reviewed",
				Explanation: "Seen once.", Panel: &render.FindingPanel{FoundBy: "a"}},
		},
	}}
}

func checkPanelGolden(t *testing.T, name string, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *updateGoldens {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if want := readFile(t, path); got != want {
		t.Errorf("comment differs from %s:\n got:\n%s\nwant:\n%s", path, got, want)
	}
}

func TestCommentPanel(t *testing.T) {
	got := render.Comment(panelBothSkill(), render.Options{Panel: panelNames})
	checkPanelGolden(t, "panel-0.md", got)
}

func TestCommentPanelAOnly(t *testing.T) {
	got := render.Comment(panelAOnlySkill(), render.Options{Panel: panelNames})
	checkPanelGolden(t, "panel-aonly-0.md", got)
}

func TestCommentNoPanelNamesWritesNoPanel(t *testing.T) {
	for _, skills := range [][]render.Skill{panelBothSkill(), panelAOnlySkill()} {
		got := render.Comment(skills, render.Options{})
		if strings.Contains(got, "Panel:") || strings.Contains(got, "found by") ||
			strings.Contains(got, "disagreed") || strings.Contains(got, "rated it") {
			t.Errorf("panel text without Options.Panel:\n%s", got)
		}
	}
}

func TestPanelReasonIsInert(t *testing.T) {
	got := render.Comment(panelBothSkill(), render.Options{Panel: panelNames})
	if strings.Contains(got, "@someone") || strings.Contains(got, "<b>no") {
		t.Errorf("reason is not inert:\n%s", got)
	}
	if !strings.Contains(got, "@\u2060someone") && !strings.Contains(got, "@\u200bsomeone") && !strings.Contains(got, "@\u200dsomeone") {
		t.Errorf("no joiner after @:\n%s", got)
	}
	if !strings.Contains(got, "&lt;") && !strings.Contains(got, "<\u200b") && !strings.Contains(got, "<\u2060") && !strings.Contains(got, "<\u200d") {
		t.Errorf("< is not escaped:\n%s", got)
	}
}

func TestPanelReasonIsOneLineAndCapped(t *testing.T) {
	s := panelBothSkill()
	s[0].Findings[2].Panel.Reason = "first\nsecond " + strings.Repeat("x", 2000)
	got := render.Comment(s, render.Options{Panel: panelNames})
	for _, l := range strings.Split(got, "\n") {
		if strings.Contains(l, "disagreed:") && (strings.Count(l, "x") > 300 || !strings.HasSuffix(l, "_")) {
			t.Errorf("reason line too long or split: %q", l)
		}
	}
	if strings.Contains(got, "\nsecond") {
		t.Errorf("reason split across lines")
	}
}
