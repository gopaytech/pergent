package output

import (
	"reflect"
	"strings"
	"testing"

	"github.com/gopaytech/pergent/internal/diff"
	"github.com/gopaytech/pergent/internal/finding"
	"github.com/gopaytech/pergent/internal/review"
	"github.com/gopaytech/pergent/internal/runner"
)

// fillStrings sets every plain string reachable from v to s. Named string
// types (verdicts, severities, statuses, cause kinds) are enums, not text,
// and are left alone.
func fillStrings(v reflect.Value, s string) {
	switch v.Kind() {
	case reflect.String:
		if v.Type() == reflect.TypeOf("") && v.CanSet() {
			v.SetString(s)
		}
	case reflect.Pointer:
		if !v.IsNil() {
			fillStrings(v.Elem(), s)
		}
	case reflect.Struct:
		for i := range v.NumField() {
			fillStrings(v.Field(i), s)
		}
	case reflect.Slice:
		for i := range v.Len() {
			fillStrings(v.Index(i), s)
		}
	}
}

// render redacts nothing, so every text field must be redacted on the way in.
func TestFormat_RedactsEveryTextField(t *testing.T) {
	const token = "sk-every-field-token-0123456789"
	t.Setenv("OPENCODE_API_KEY", token)

	text := "before " + token + " after"
	r := review.SkillResult{
		Skill:     "skill",
		Verdict:   review.HasBlockingIssues,
		Findings:  []finding.Finding{{Severity: finding.Blocking, Line: 1}, {Severity: finding.Suggestion, Line: 2}},
		Skipped:   []diff.Skip{{}},
		Failures:  []review.BatchFailure{{Files: []string{"a", "b"}, Cause: &runner.Cause{Kind: runner.CauseExitError, StderrLines: []string{"x"}, OtherErrors: []string{"y"}}}, {Files: []string{"c"}}},
		Notes:     []string{"n", "Verification dropped n"},
		Summaries: []review.BatchSummary{{Batch: 1}},
		Reviewed:  []string{"r"},
		Checks: []review.BatchChecks{{Batch: 1, Findings: 1, Checks: []finding.Check{
			{Status: finding.CheckPass}, {Status: finding.CheckFail}, {Status: finding.CheckNA},
		}}},
		Batches: 1,
	}
	fillStrings(reflect.ValueOf(&r).Elem(), text)
	changed := []string{text}

	if !strings.Contains(r.Skill, token) || !strings.Contains(r.Failures[0].Cause.StderrLines[0], token) || !strings.Contains(r.Checks[0].Checks[0].Detail, token) {
		t.Fatal("fillStrings did not reach the fields")
	}

	got := FormatReviewWithinOptions([]review.SkillResult{r}, changed, 0, Options{IDs: map[string]string{text: "abcdef"}})
	if strings.Contains(got, token) {
		t.Errorf("a credential reached the comment:\n%s", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Errorf("nothing was redacted; the fields were not rendered:\n%s", got)
	}
	if block := FindingsBlock([]review.SkillResult{r}); strings.Contains(block, token) {
		t.Errorf("a credential reached the findings block: %q", block)
	}
	shrunk := FormatReviewWithin([]review.SkillResult{r}, changed, 600)
	if strings.Contains(shrunk, token) {
		t.Errorf("a credential reached the shrunk comment:\n%s", shrunk)
	}
}

// A confidence reason and a suggestion are model text too.
func TestFormat_RedactsConfidenceReasonAndSuggestion(t *testing.T) {
	const token = "ghp_confidence_and_suggestion_token"
	t.Setenv("GITHUB_TOKEN", token)

	r := review.SkillResult{
		Skill: "skill", Verdict: review.HasBlockingIssues, Confidence: 4, ConfidenceReason: "because " + token,
		Findings: []finding.Finding{{File: "a.go", Line: 1, EndLine: 2, Severity: finding.Blocking, Title: "t", Explanation: "e", Suggestion: "key := \"" + token + "\""}},
	}
	got := FormatReviewWithinOptions([]review.SkillResult{r}, nil, 0, Options{})
	if strings.Contains(got, token) {
		t.Errorf("a credential reached the comment:\n%s", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Errorf("nothing was redacted; the fields were not rendered:\n%s", got)
	}
	if block := FindingsBlock([]review.SkillResult{r}); strings.Contains(block, token) {
		t.Errorf("a credential reached the findings block: %q", block)
	}
}
