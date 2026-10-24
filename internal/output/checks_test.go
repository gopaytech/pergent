package output

import (
	"strings"
	"testing"

	"github.com/gopaytech/pergent/internal/finding"
	"github.com/gopaytech/pergent/internal/review"
)

func check(section, name string, status finding.CheckStatus, detail string) finding.Check {
	return finding.Check{Section: section, Name: name, Status: status, Detail: detail}
}

// inOrder fails the test unless each want appears in got after the previous one.
func inOrder(t *testing.T, got string, wants ...string) {
	t.Helper()
	pos := 0
	for _, w := range wants {
		i := strings.Index(got[pos:], w)
		if i < 0 {
			t.Fatalf("%q missing or out of order:\n%s", w, got)
		}
		pos += i + len(w)
	}
}

func TestFormatReview_ChecksSingleBatch(t *testing.T) {
	got := FormatReview([]review.SkillResult{{
		Skill: "s", Verdict: review.HasBlockingIssues, Batches: 1, BatchesFinished: 1,
		Summaries: []review.BatchSummary{{Batch: 1, Text: "Sum."}},
		Checks: []review.BatchChecks{{Batch: 1, Findings: 1, Checks: []finding.Check{
			check("A", "a1", finding.CheckPass, "d1"),
			check("A", "a2", finding.CheckNA, "d2"),
			check("B", "b1", finding.CheckFail, ""),
		}}},
		Findings: []finding.Finding{{File: "x.go", Line: 1, Severity: finding.Blocking, Title: "Broken thing", Explanation: "e"}},
	}}, nil)

	inOrder(t, got,
		"**Summary:** Sum.",
		"**Checks:** 1 passed · 1 failed · 1 n/a",
		"**A**", "| Check | Status | Detail |", "| a1 | ✅ | d1 |", "| a2 | ➖ | d2 |",
		"**B**", "| Check | Status | Detail |", "| b1 | ❌ |",
		"Broken thing",
	)
}

func TestFormatReview_ChecksCountOmitsZeros(t *testing.T) {
	got := FormatReview([]review.SkillResult{{
		Skill: "s", Verdict: review.NoBlockingIssues, Batches: 1, BatchesFinished: 1,
		Checks: []review.BatchChecks{{Batch: 1, Checks: []finding.Check{
			check("", "a", finding.CheckPass, ""), check("", "b", finding.CheckPass, ""), check("", "c", finding.CheckPass, ""),
		}}},
	}}, nil)
	if !strings.Contains(got, "**Checks:** 3 passed\n") {
		t.Errorf("want a count line with only passes:\n%s", got)
	}
	if strings.Contains(got, "failed") || strings.Contains(got, "n/a") {
		t.Errorf("zero counts shown:\n%s", got)
	}
}

func TestFormatReview_ChecksMultiBatch(t *testing.T) {
	got := FormatReview([]review.SkillResult{{
		Skill: "s", Verdict: review.NoBlockingIssues, Batches: 2, BatchesFinished: 2,
		Checks: []review.BatchChecks{
			{Batch: 1, Findings: 0, Checks: []finding.Check{check("A", "a1", finding.CheckPass, ""), check("A", "a2", finding.CheckPass, "")}},
			{Batch: 2, Findings: 1, Checks: []finding.Check{check("A", "b1", finding.CheckFail, "")}},
		},
	}}, nil)
	inOrder(t, got,
		"**Checks:** 2 passed · 1 failed",
		"<details><summary>Checks — batch 1 of 2 (2 passed)</summary>", "| a1 | ✅ |", "</details>",
		"<details open><summary>Checks — batch 2 of 2 (1 failed)</summary>", "| b1 | ❌ |", "</details>",
	)
	if o, c := strings.Count(got, "<details"), strings.Count(got, "</details>"); o != c {
		t.Errorf("%d <details but %d </details>:\n%s", o, c, got)
	}
}

func TestFormatReview_FailedCheckWithoutFinding(t *testing.T) {
	got := FormatReview([]review.SkillResult{{
		Skill: "s", Verdict: review.NoBlockingIssues, Batches: 1, BatchesFinished: 1,
		Checks: []review.BatchChecks{{Batch: 1, Findings: 0, Checks: []finding.Check{check("", "a", finding.CheckFail, "")}}},
	}}, nil)
	inOrder(t, got,
		"✅ **No blocking issues**",
		"| a | ❌ |",
		"> 1 failed check has no matching finding; the verdict is based on findings.",
	)
}

func TestFormatReview_VerificationNoteNextToChecks(t *testing.T) {
	note := "Verification dropped 1 of 1 blocking finding: Not real (it is handled upstream)"
	got := FormatReview([]review.SkillResult{{
		Skill: "s", Verdict: review.NoBlockingIssues, Batches: 1, BatchesFinished: 1,
		Checks:   []review.BatchChecks{{Batch: 1, Findings: 1, Checks: []finding.Check{check("", "a", finding.CheckFail, "")}}},
		Notes:    []string{note},
		Reviewed: []string{"a.go"},
	}}, nil)
	inOrder(t, got, "| a | ❌ |", "> "+note, "<details><summary>Reviewed files")
	if strings.Count(got, note) != 1 {
		t.Errorf("verification note shown %d times, want once:\n%s", strings.Count(got, note), got)
	}
}

func TestFormatReview_CheckCellsSafe(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "glpat-secretvalue")
	got := FormatReview([]review.SkillResult{{
		Skill: "s", Verdict: review.NoBlockingIssues, Batches: 1, BatchesFinished: 1,
		Checks: []review.BatchChecks{{Batch: 1, Findings: 1, Checks: []finding.Check{
			check("Sec|tion", "a|b\nc `x", finding.CheckPass, "</details> <summary>y glpat-secretvalue"),
		}}},
		Reviewed: []string{"a.go"},
	}}, nil)
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "| ") && !strings.HasPrefix(line, "| Check ") {
			if n := strings.Count(line, "|") - strings.Count(line, `\|`); n != 4 {
				t.Errorf("row %q has %d unescaped |, want 4", line, n)
			}
		}
	}
	if o, c := strings.Count(got, "<details"), strings.Count(got, "</details>"); o != c {
		t.Errorf("%d <details but %d </details>:\n%s", o, c, got)
	}
	if strings.Contains(got, "glpat-secretvalue") {
		t.Errorf("secret leaked:\n%s", got)
	}
}

func TestFormatReview_ChecksDroppedNote(t *testing.T) {
	got := FormatReview([]review.SkillResult{{
		Skill: "s", Verdict: review.NoBlockingIssues, Batches: 1, BatchesFinished: 1,
		Checks: []review.BatchChecks{{Batch: 1, Dropped: 140, Checks: []finding.Check{check("", "a", finding.CheckPass, "")}}},
	}}, nil)
	if !strings.Contains(got, "> 140 checks beyond the first 60 per answer were not shown.") {
		t.Errorf("no dropped-checks note:\n%s", got)
	}
}

func TestFormatReview_NoChecksUnchanged(t *testing.T) {
	got := FormatReview([]review.SkillResult{{
		Skill: "s", Verdict: review.NoBlockingIssues, Batches: 1, BatchesFinished: 1,
		Summaries: []review.BatchSummary{{Batch: 1, Text: "Sum."}},
		Reviewed:  []string{"a.go"},
	}}, nil)
	if strings.Contains(got, "**Checks:**") || strings.Contains(got, "| Check |") {
		t.Errorf("checks shown without checks:\n%s", got)
	}
	if !strings.Contains(got, "**Summary:** Sum.\n\n<details><summary>Reviewed files") {
		t.Errorf("layout changed for a result without checks:\n%s", got)
	}
}
