package output

import (
	"reflect"
	"strings"
	"testing"

	"github.com/gopaytech/pergent/internal/finding"
	"github.com/gopaytech/pergent/internal/review"
)

func idResults() []review.SkillResult {
	return []review.SkillResult{{
		Skill:   "code-review",
		Verdict: review.HasBlockingIssues,
		Findings: []finding.Finding{
			{File: "a.go", Line: 3, Severity: finding.Blocking, Title: "Nil deref", Explanation: "boom", Fingerprint: "fp-blocking"},
			{File: "b.go", Line: 9, Severity: finding.Suggestion, Title: "Rename", Explanation: "nit", Fingerprint: "fp-suggestion"},
			{File: "c.go", Severity: finding.Suggestion, Title: "No id", Fingerprint: "fp-unknown"},
		},
	}}
}

func TestFormat_IDsShown(t *testing.T) {
	ids := map[string]string{"fp-blocking": "abcdef11", "fp-suggestion": "12345678"}
	results := idResults()
	for i := range results[0].Findings[1:] {
		results[0].Findings[i+1].Explanation = strings.Repeat("long explanation ", 40)
	}

	out := FormatReviewWithinOptions(results, nil, 0, Options{IDs: ids})
	if !strings.Contains(out, "**Nil deref** `abcdef11`\n") {
		t.Errorf("blocking finding has no ID:\n%s", out)
	}
	if !strings.Contains(out, "**Rename** `12345678`\n") {
		t.Errorf("suggestion has no ID:\n%s", out)
	}
	if !strings.Contains(out, "**No id**\n") {
		t.Errorf("finding without an ID changed:\n%s", out)
	}

	// Every shrink step keeps the IDs: suggestions render title-only first.
	full := FormatReviewWithinOptions(results, nil, 0, Options{IDs: ids})
	noHidden := FormatReviewWithinOptions(results, nil, len(full)-1, Options{IDs: ids})
	if !strings.Contains(noHidden, hiddenDroppedNote) {
		t.Fatalf("expected the layout without the hidden block:\n%s", noHidden)
	}
	titleOnly := FormatReviewWithinOptions(results, nil, len(noHidden)-1, Options{IDs: ids})
	if !strings.Contains(titleOnly, detailsDroppedNote) {
		t.Fatalf("expected the title-only layout:\n%s", titleOnly)
	}
	for _, want := range []string{"**Nil deref** `abcdef11`", "**Rename** `12345678`"} {
		if !strings.Contains(titleOnly, want) {
			t.Errorf("title-only layout lacks %q:\n%s", want, titleOnly)
		}
	}
	// With no room for suggestions, the blocking ID stays.
	noSugg := FormatReviewWithinOptions(results, nil, len(titleOnly)-1, Options{IDs: ids})
	if !strings.Contains(noSugg, "**Nil deref** `abcdef11`") {
		t.Errorf("no-suggestions layout lacks the blocking ID:\n%s", noSugg)
	}
}

func TestFormat_IDCannotBreakLayout(t *testing.T) {
	out := FormatReviewWithinOptions(idResults(), nil, 0, Options{IDs: map[string]string{"fp-blocking": "a`b\nc"}})
	if !strings.Contains(out, "**Nil deref** ``"+" a`b c "+"``\n") {
		t.Errorf("ID not rendered as a single code span:\n%s", out)
	}
}

func TestFormat_NoIDsUnchanged(t *testing.T) {
	results, changed := bigResults()
	for _, maxLen := range []int{0, 1 << 30, 30000, 12000, 6000, 3000, 1500, 400} {
		want := FormatReviewWithin(results, changed, maxLen)
		for name, o := range map[string]Options{"zero": {}, "empty map": {IDs: map[string]string{}}} {
			if got := FormatReviewWithinOptions(results, changed, maxLen, o); got != want {
				t.Errorf("maxLen %d, %s: output differs from FormatReviewWithin", maxLen, name)
			}
		}
	}
	if strings.Contains(FormatReviewWithin(idResults(), nil, 0), "`abcdef11`") {
		t.Error("CI mode shows an ID")
	}
}

func TestFindingsBlock(t *testing.T) {
	results := idResults()
	results[0].Findings[0].Explanation = "token ghp_abcdefghijklmnopqrstuvwxyz0123456789 leaked"
	comment := FormatReview(results, []string{"a.go"})
	block := FindingsBlock(results)

	if !strings.HasPrefix(block, findingsMarker) || !strings.HasSuffix(block, " -->\n") || strings.Count(block, "\n") != 1 {
		t.Errorf("block is not one hidden line: %q", block)
	}
	if !strings.HasSuffix(comment, block) {
		t.Errorf("block is not the last line of the comment:\n%q\n%q", block, comment)
	}
	got, err := ParsePreviousFindings(block)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got["code-review"], redactFindings(results[0].Findings)) {
		t.Errorf("round trip = %+v", got["code-review"])
	}
	if strings.Contains(block, "ghp_") {
		t.Error("block is not redacted")
	}
}
