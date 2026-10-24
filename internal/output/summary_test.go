package output

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gopaytech/pergent/internal/diff"
	"github.com/gopaytech/pergent/internal/review"
)

func TestFormatReview_SummaryUnderVerdict(t *testing.T) {
	got := FormatReview([]review.SkillResult{{
		Skill: "s", Verdict: review.NoBlockingIssues, Batches: 1, BatchesFinished: 1,
		Summaries: []review.BatchSummary{{Batch: 1, Text: "Checked a."}},
		Reviewed:  []string{"a.go"},
	}}, nil)

	verdict := strings.Index(got, "✅ **No blocking issues**")
	summary := strings.Index(got, "Checked a.")
	details := strings.Index(got, "<details>")
	if verdict < 0 || summary < verdict || details < summary {
		t.Errorf("want verdict, then summary, then the first <details> block:\n%s", got)
	}
}

func TestFormatReview_MultiBatchSummaries(t *testing.T) {
	got := FormatReview([]review.SkillResult{{
		Skill: "s", Verdict: review.NoBlockingIssues, Batches: 3, BatchesFinished: 3,
		Summaries: []review.BatchSummary{{Batch: 1, Text: "First."}, {Batch: 3, Text: "Third."}},
	}}, nil)

	first := strings.Index(got, "- **Batch 1 of 3:** First.")
	third := strings.Index(got, "- **Batch 3 of 3:** Third.")
	if first < 0 || third < first {
		t.Errorf("want batch 1 then batch 3 as bullets:\n%s", got)
	}
}

func TestFormatReview_ReviewedFiles(t *testing.T) {
	got := FormatReview([]review.SkillResult{{
		Skill: "s", Verdict: review.NoBlockingIssues,
		Reviewed: []string{"a.go", "b.go"},
		Skipped:  []diff.Skip{{Path: "go.sum", Rule: "lockfile"}},
	}}, nil)
	reviewed := strings.Index(got, "<details><summary>Reviewed files (2)</summary>")
	skipped := strings.Index(got, "<details><summary>Skipped files (1)</summary>")
	if reviewed < 0 || skipped < reviewed {
		t.Errorf("want Reviewed files before Skipped files:\n%s", got)
	}
	if !strings.Contains(got, "- `a.go`\n- `b.go`\n") {
		t.Errorf("reviewed files not listed:\n%s", got)
	}

	none := FormatReview([]review.SkillResult{{Skill: "s", Verdict: review.NoBlockingIssues}}, nil)
	if strings.Contains(none, "Reviewed files") {
		t.Errorf("a skill with no reviewed files shows the block:\n%s", none)
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
	if o, c := strings.Count(visible, "<details>"), strings.Count(visible, "</details>"); o != c {
		t.Errorf("%d <details> but %d </details>:\n%s", o, c, got)
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
	if !strings.Contains(visible, "✅ **No blocking issues**") || !strings.Contains(visible, "### second") {
		t.Errorf("layout broken:\n%s", got)
	}
}

func TestFormatReviewWithin_DropsSummariesBeforeLists(t *testing.T) {
	var summaries []review.BatchSummary
	for i := range 20 {
		summaries = append(summaries, review.BatchSummary{Batch: i + 1, Text: fmt.Sprintf("Summary %02d %s", i, strings.Repeat("s", 560))})
	}
	base := review.SkillResult{
		Skill: "s", Verdict: review.NoBlockingIssues, Batches: 20, BatchesFinished: 20,
		Reviewed: []string{"a.go", "b.go"},
	}
	withSummaries := base
	withSummaries.Summaries = summaries

	maxLen := len(FormatReview([]review.SkillResult{base}, nil)) + 300
	got := FormatReviewWithin([]review.SkillResult{withSummaries}, nil, maxLen)
	if len(got) > maxLen {
		t.Errorf("len = %d, want at most %d", len(got), maxLen)
	}
	if strings.Contains(got, "Summary 00") {
		t.Errorf("summaries kept:\n%s", got)
	}
	if !strings.Contains(got, "> Summaries omitted to fit the comment size limit.") {
		t.Errorf("no note about omitted summaries:\n%s", got)
	}
	if !strings.Contains(got, "- `a.go`\n- `b.go`\n") {
		t.Errorf("reviewed files should still be listed:\n%s", got)
	}
}

func TestFormatReview_SummaryStaysVisibleText(t *testing.T) {
	for _, text := range []string{"```go Checked a.go: fine.", "<pre> Checked a.go: fine.", "# Checked a.go: fine."} {
		got := FormatReview([]review.SkillResult{{
			Skill: "s", Verdict: review.NoBlockingIssues, Batches: 1, BatchesFinished: 1,
			Summaries: []review.BatchSummary{{Batch: 1, Text: text}},
			Reviewed:  []string{"a.go"},
		}}, nil)
		for _, line := range strings.Split(got, "\n") {
			if strings.Contains(line, "Checked a.go: fine.") {
				if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "<") || strings.HasPrefix(line, "#") {
					t.Errorf("summary %q starts a markdown block: %q", text, line)
				}
			}
		}
		if strings.Contains(got, "<pre>") {
			t.Errorf("raw <pre> reached the comment:\n%s", got)
		}
		if !strings.Contains(got, "Checked a.go: fine.") {
			t.Errorf("summary text missing:\n%s", got)
		}
	}
}
