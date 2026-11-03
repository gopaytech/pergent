package output

import (
	"strings"
	"testing"

	"github.com/gopaytech/pergent/internal/diff"
	"github.com/gopaytech/pergent/internal/review"
)

// The model's summaries are no longer drawn in the comment.
func TestFormatReview_SummariesNotShown(t *testing.T) {
	got := FormatReview([]review.SkillResult{{
		Skill: "s", Verdict: review.NoBlockingIssues, Batches: 3, BatchesFinished: 3,
		Summaries: []review.BatchSummary{{Batch: 1, Text: "Checked a."}, {Batch: 3, Text: "Third."}},
		Reviewed:  []string{"a.go"},
	}}, nil)
	for _, text := range []string{"Checked a.", "Third.", "Summary", "Batch 1 of 3"} {
		if strings.Contains(got, text) {
			t.Errorf("summary text %q is shown:\n%s", text, got)
		}
	}
	if !strings.HasPrefix(got, "<!-- pergent -->\n### ✅ No blocking issues\n") {
		t.Errorf("headline missing:\n%s", got)
	}
}

func TestFormatReview_ReviewedFiles(t *testing.T) {
	got := FormatReview([]review.SkillResult{{
		Skill: "s", Verdict: review.NoBlockingIssues,
		Reviewed: []string{"a.go", "b.go"},
		Skipped:  []diff.Skip{{Path: "go.sum", Rule: "lockfile"}},
	}}, nil)
	if !strings.Contains(got, "<details><summary>Files (3)</summary>") {
		t.Errorf("want one Files block:\n%s", got)
	}
	if !strings.Contains(got, "- `a.go`\n- `b.go`\n- `go.sum` \u2014 skipped: lockfile\n") {
		t.Errorf("files not listed:\n%s", got)
	}

	none := FormatReview([]review.SkillResult{{Skill: "s", Verdict: review.NoBlockingIssues}}, nil)
	if strings.Contains(none, "Files (") {
		t.Errorf("a skill with no files shows the block:\n%s", none)
	}
}

func TestFormatReview_SummaryCannotBreakLayout(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "glpat-secretvalue")
	results := []review.SkillResult{
		{Skill: "first", Verdict: review.NoBlockingIssues, Batches: 1, BatchesFinished: 1,
			Summaries: []review.BatchSummary{{Batch: 1, Text: "```go closes </details> early, token glpat-secretvalue"}}},
		{Skill: "second", Verdict: review.NoBlockingIssues, Batches: 2, BatchesFinished: 2,
			Summaries: []review.BatchSummary{{Batch: 1, Text: "<summary>x</summary> ````"}, {Batch: 2, Text: "fine"}}},
	}
	got := FormatReview(results, nil)

	idx := strings.Index(got, "<!-- pergent:findings ")
	if idx < 0 {
		t.Fatalf("no hidden line:\n%s", got)
	}
	visible := got[:idx]
	if o, c := strings.Count(visible, "<details"), strings.Count(visible, "</details>"); o != c {
		t.Errorf("%d <details but %d </details>:\n%s", o, c, got)
	}
	if strings.Contains(strings.ToLower(visible), "<summary>x") {
		t.Errorf("summary tag from model text not neutralised:\n%s", got)
	}
	open := ""
	for _, line := range strings.Split(visible, "\n") {
		ch, n, bare := fenceLine(line)
		switch {
		case ch == 0:
		case open == "":
			open = strings.Repeat(string(ch), n)
		case open[0] == ch && n >= len(open) && bare:
			open = ""
		}
	}
	if open != "" {
		t.Errorf("fence %q is still open before the hidden line:\n%s", open, got)
	}
	if strings.Contains(got, "glpat-secretvalue") {
		t.Errorf("secret leaked:\n%s", got)
	}
	if !strings.Contains(visible, "### ✅ No blocking issues") || !strings.Contains(visible, "| `second` | ✅ No blocking issues | none |") {
		t.Errorf("layout broken:\n%s", got)
	}
}
