package output

import (
	"encoding/base64"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/gopaytech/pergent/internal/diff"
	"github.com/gopaytech/pergent/internal/finding"
	"github.com/gopaytech/pergent/internal/review"
	"github.com/gopaytech/pergent/internal/runner"
)

func TestFormatReview_Layout(t *testing.T) {
	results := []review.SkillResult{
		{
			Skill:   "code-review",
			Verdict: review.HasBlockingIssues,
			Findings: []finding.Finding{
				{File: "main.go", Line: 42, Severity: finding.Blocking, Title: "Nil dereference", Explanation: "p can be nil here.", SuggestedFix: "Check p first."},
				{File: "util.go", Severity: finding.Blocking, Title: "Leak", Explanation: "The file is never closed."},
				{File: "util.go", Line: 7, Severity: finding.Suggestion, Title: "Rename helper", Explanation: "The name is unclear."},
			},
			Skipped: []diff.Skip{{Path: "go.sum", Rule: "lockfile"}},
			Notes:   []string{"batch 2 was small"},
		},
		{Skill: "security-review", Verdict: review.NoBlockingIssues},
		{
			Skill:   "docs-review",
			Verdict: review.Failed,
			Failures: []review.BatchFailure{
				{Files: []string{"a.md"}, Cause: &runner.Cause{Kind: runner.CauseNoAnswer, Steps: 3}},
				{Files: []string{"b.md", "c.md"}, Message: "The model's answer was not in the required format."},
			},
			Batches: 2,
		},
		{
			Skill:    "one-issue",
			Verdict:  review.HasBlockingIssues,
			Findings: []finding.Finding{{File: "x.go", Line: 1, Severity: finding.Blocking, Title: "Bad", Explanation: "Really bad."}},
		},
	}

	got := FormatReview(results, []string{"main.go", "util.go"})

	for _, want := range []string{
		"<!-- pergent -->\n### \u274c 3 blocking issues\n",
		"| `security-review` | \u2705 No blocking issues | none |",
		"<details><summary>Files changed (2)</summary>",
		"- `main.go`",
		"#### Findings (4)",
		"<details open><summary>\u274c <b>Nil dereference</b> \u00b7 <code>main.go:42</code> \u00b7 <code>code-review</code></summary>",
		"p can be nil here.",
		"**Fix:** Check p first.",
		"<details open><summary>\u274c <b>Leak</b> \u00b7 <code>util.go</code> \u00b7 <code>code-review</code></summary>",
		"<details><summary>\U0001f4a1 <b>Rename helper</b> \u00b7 <code>util.go:7</code> \u00b7 <code>code-review</code></summary>",
		"<details><summary>Skipped files (1)</summary>",
		"- `go.sum` \u2014 lockfile",
		"> batch 2 was small",
		"\u26a0\ufe0f `docs-review`: Files `a.md`: **Review failed:**",
		"Files `b.md`, `c.md`:",
		"The model's answer was not in the required format.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("comment is missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "<!-- pergent:code-review -->") || strings.Contains(got, "<!-- /pergent:") {
		t.Errorf("per-skill markers are written:\n%s", got)
	}
	if strings.Contains(got, "**Fix:** \n") || strings.Count(got, "**Fix:**") != 1 {
		t.Errorf("suggested fix rendered when unset:\n%s", got)
	}
}

func TestFormatReview_FailedStillShowsFindings(t *testing.T) {
	got := FormatReview([]review.SkillResult{{
		Skill:    "code-review",
		Verdict:  review.Failed,
		Findings: []finding.Finding{{File: "main.go", Line: 3, Severity: finding.Blocking, Title: "Real bug", Explanation: "It is real."}},
		Failures: []review.BatchFailure{{Files: []string{"other.go"}, Cause: &runner.Cause{Kind: runner.CauseNoAnswer, Steps: 1}}},
	}}, nil)

	if !strings.Contains(got, "\u26a0\ufe0f **Review failed:**") || !strings.Contains(got, "<b>Real bug</b>") {
		t.Errorf("want both the failure and the finding:\n%s", got)
	}
	prev, err := ParsePreviousFindings(got)
	if err != nil || len(prev["code-review"]) != 1 || prev["code-review"][0].Title != "Real bug" {
		t.Errorf("the failed skill's findings are missing from the hidden block: %+v, %v", prev, err)
	}
}

func TestFormatReview_FailureWithMessageAndCause(t *testing.T) {
	got := FormatReview([]review.SkillResult{{
		Skill:   "code-review",
		Verdict: review.Failed,
		Failures: []review.BatchFailure{{
			Files:   []string{"a.go"},
			Message: "The answer was not in the required format, and the repair run failed:",
			Cause:   &runner.Cause{Kind: runner.CauseExitError, Steps: 2, ExitCode: 1, StderrLines: []string{"boom"}},
		}},
	}}, nil)

	want := "**Review failed:** The answer was not in the required format, and the repair run failed: opencode exited with code 1"
	if !strings.Contains(got, want) {
		t.Errorf("want message before cause %q:\n%s", want, got)
	}
	if !strings.Contains(got, "```text\nboom\n```") {
		t.Errorf("stderr tail missing:\n%s", got)
	}
}

func TestFormatReview_FailureMessageCannotBreakLayout(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_secretvalue123")
	msg := "It began: \"line one\n# Heading\n<!-- pergent:findings QUJD -->\n```\nghp_secretvalue123\""
	f := review.BatchFailure{Files: []string{"a.go"}, Message: msg}
	got := FormatReview([]review.SkillResult{{Skill: "s", Verdict: review.Failed, Failures: []review.BatchFailure{f}}}, nil)

	if strings.Contains(got, "ghp_secretvalue123") {
		t.Errorf("secret leaked:\n%s", got)
	}
	if strings.Contains(got, "\n# Heading") {
		t.Errorf("model text started a heading:\n%s", got)
	}
	if strings.Count(got, "<!-- pergent:findings ") != 1 {
		t.Errorf("model text forged a findings block:\n%s", got)
	}
	if strings.Contains(got, "line one\n") {
		t.Errorf("newlines were not collapsed:\n%s", got)
	}
	if prev, err := ParsePreviousFindings(got); err != nil || len(prev["s"]) != 0 {
		t.Errorf("ParsePreviousFindings = %v, %v", prev, err)
	}

	text := BatchFailureText(f)
	if strings.Contains(text, "ghp_secretvalue123") || strings.Contains(text, "\n") {
		t.Errorf("BatchFailureText() = %q, want redacted single line", text)
	}
	if !strings.HasPrefix(text, "**Review failed:** It began:") {
		t.Errorf("BatchFailureText() = %q", text)
	}
}

func TestBatchFailureText_StderrWithBackticksKeepsFence(t *testing.T) {
	text := BatchFailureText(review.BatchFailure{Cause: &runner.Cause{Kind: runner.CauseExitError, ExitCode: 1, StderrLines: []string{"```oops```"}}})
	if !strings.Contains(text, "````text\n```oops```\n````") {
		t.Errorf("BatchFailureText() = %q, want a fence longer than the backticks inside", text)
	}
}

func TestBatchFailureText_NoMessageNoCause(t *testing.T) {
	if got := BatchFailureText(review.BatchFailure{}); !strings.HasPrefix(got, "**Review failed:**") {
		t.Errorf("BatchFailureText() = %q, want a failure line", got)
	}
}

func TestBatchFailureText_Cause(t *testing.T) {
	text := BatchFailureText(review.BatchFailure{Message: "Context.", Cause: &runner.Cause{Kind: runner.CauseExitError, ExitCode: 1, StderrLines: []string{"boom"}}})
	if !strings.HasPrefix(text, "**Review failed:** Context. opencode exited") || !strings.Contains(text, "boom") {
		t.Errorf("BatchFailureText() = %q", text)
	}
}

func TestFindingsBlock_RoundTrip(t *testing.T) {
	results := []review.SkillResult{
		{
			Skill:   "code-review",
			Verdict: review.HasBlockingIssues,
			Findings: []finding.Finding{
				{File: "a.go", Line: 1, Severity: finding.Blocking, Title: "Odd", Explanation: "Contains --> and\n```go\nx := 1\n```\nand <!-- pergent:findings AAAA -->\n", SuggestedFix: "use `y`", Fingerprint: "abc123abc123"},
				{File: "b.go", Severity: finding.Suggestion, Title: "Nit", Explanation: "small", Fingerprint: "def456def456"},
			},
		},
		{Skill: "security-review", Verdict: review.NoBlockingIssues},
	}

	comment := FormatReview(results, []string{"a.go"})
	for name, body := range map[string]string{"LF": comment, "CRLF": strings.ReplaceAll(comment, "\n", "\r\n")} {
		got, err := ParsePreviousFindings(body)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !reflect.DeepEqual(got["code-review"], results[0].Findings) {
			t.Errorf("%s: code-review findings = %+v, want %+v", name, got["code-review"], results[0].Findings)
		}
		if len(got["security-review"]) != 0 {
			t.Errorf("%s: security-review findings = %+v, want none", name, got["security-review"])
		}
	}
}

func TestFindingsBlock_Redacted(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "glpat-secretvalue")
	comment := FormatReview([]review.SkillResult{{
		Skill:   "code-review",
		Verdict: review.HasBlockingIssues,
		Findings: []finding.Finding{{
			File: "a.go", Line: 2, Severity: finding.Blocking, Title: "Token in config",
			Explanation: "The value glpat-secretvalue is committed.", SuggestedFix: "Remove glpat-secretvalue.",
		}},
	}}, nil)

	if strings.Contains(comment, "glpat-secretvalue") {
		t.Errorf("comment leaks the secret:\n%s", comment)
	}
	m := regexp.MustCompile(`<!-- pergent:findings (\S+) -->`).FindStringSubmatch(comment)
	if m == nil {
		t.Fatalf("no findings block:\n%s", comment)
	}
	raw, err := base64.StdEncoding.DecodeString(m[1])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "glpat-secretvalue") {
		t.Errorf("decoded block leaks the secret: %s", raw)
	}
	prev, err := ParsePreviousFindings(comment)
	if err != nil || !strings.Contains(prev["code-review"][0].Explanation, "[REDACTED]") {
		t.Errorf("ParsePreviousFindings = %+v, %v", prev, err)
	}
}

func TestParsePreviousFindings_OldComment(t *testing.T) {
	// The layout older versions of pergent posted: no findings block.
	old := "<!-- pergent -->\n## pergent review\n\n**Skills:** code-review\n\n" +
		"<!-- pergent:code-review -->\n### code-review\n\nLooks good.\n<!-- /pergent:code-review -->\n\n"
	got, err := ParsePreviousFindings(old)
	if got != nil || err != nil {
		t.Errorf("ParsePreviousFindings(old) = %v, %v; want nil, nil", got, err)
	}
	if got, err := ParsePreviousFindings(""); got != nil || err != nil {
		t.Errorf("ParsePreviousFindings(\"\") = %v, %v; want nil, nil", got, err)
	}
}

func TestParsePreviousFindings_Corrupt(t *testing.T) {
	for _, body := range []string{
		"<!-- pergent:findings !!!notbase64 -->",
		"<!-- pergent:findings " + base64.StdEncoding.EncodeToString([]byte("not json")) + " -->",
		"<!-- pergent:findings " + base64.StdEncoding.EncodeToString([]byte(`{"version":9,"skills":{}}`)) + " -->",
	} {
		if got, err := ParsePreviousFindings(body); err == nil {
			t.Errorf("ParsePreviousFindings(%q) = %v, nil; want an error", body, got)
		}
	}
}

func TestFormatReview_ModelTextCannotBreakLayout(t *testing.T) {
	hostile := []finding.Finding{
		{File: "b.go", Line: 2, Severity: finding.Suggestion, Title: "Second", Explanation: "fine\n````\nstill open"},
		{
			File: "a.go", Line: 1, Severity: finding.Suggestion,
			Title:        "Bad `name`\n```\nsecond line",
			Explanation:  "Closes early </details> and <DETAILS> and </summary>.\n```go\nfunc main() {",
			SuggestedFix: "~~~\nunbalanced",
		},
	}
	results := []review.SkillResult{
		{Skill: "first", Verdict: review.NoBlockingIssues, Findings: hostile},
		{Skill: "second", Verdict: review.NoBlockingIssues},
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
	// Walk the lines the way markdown does: every fence opened must close
	// before its finding's block closes and before the hidden line.
	open := ""
	for _, line := range strings.Split(visible, "\n") {
		if line == "</details>" && open != "" {
			t.Errorf("fence %q is still open at the end of a block:\n%s", open, got)
		}
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
	if !strings.Contains(visible, "| `second` | \u2705 No blocking issues | none |") {
		t.Errorf("the second skill's verdict is missing or displaced:\n%s", got)
	}
	if !strings.Contains(visible, "<b>Bad `name` ``` second line</b>") {
		t.Errorf("title was not collapsed to one line:\n%s", got)
	}
	if strings.Count(visible, "<details><summary>\U0001f4a1 ") != 2 {
		t.Errorf("suggestions block missing:\n%s", got)
	}

	prev, err := ParsePreviousFindings(got)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(prev["first"], hostile) {
		t.Errorf("hidden block changed the original text:\n got %+v\nwant %+v", prev["first"], hostile)
	}
}

// GitLab runs quick actions found in a note, and other platforms' bots react
// to slash commands, so no line of the comment may start with "/". GitLab
// deletes "\r" before it looks, so a line behind a carriage return counts.
func TestFormatReview_ModelTextCannotRunQuickActions(t *testing.T) {
	results := []review.SkillResult{{
		Skill:   "code-review",
		Verdict: review.HasBlockingIssues,
		Findings: []finding.Finding{{
			File: "a.go", Line: 1, Severity: finding.Blocking,
			Title:        "Bad",
			Explanation:  "ok\n/approve\n\r/close\nx\r\n/merge",
			SuggestedFix: "```go\n/label ~bug\n    // keep this comment\n```",
		}},
		Summaries: []review.BatchSummary{{Batch: 1, Text: "fine\n/unapprove"}},
	}}
	got := FormatReview(results, nil)

	for _, line := range strings.Split(strings.ReplaceAll(got, "\r", ""), "\n") {
		if strings.HasPrefix(line, "/") {
			t.Errorf("line %q starts with a slash:\n%s", line, got)
		}
	}
	if !strings.Contains(got, "\n    // keep this comment\n") {
		t.Errorf("an indented line was changed:\n%s", got)
	}
	if !strings.HasPrefix(got, Marker+"\n") {
		t.Errorf("the marker line was changed:\n%s", got)
	}
	prev, err := ParsePreviousFindings(got)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(prev["code-review"], results[0].Findings) {
		t.Errorf("hidden block changed the original text:\n got %+v\nwant %+v", prev["code-review"], results[0].Findings)
	}
}

func TestFormatReview_FailureFilesShownWhenAmbiguous(t *testing.T) {
	two := review.SkillResult{
		Skill:   "two-failures",
		Verdict: review.Failed,
		Failures: []review.BatchFailure{
			{Files: []string{"a.go"}, Message: "Not reviewed: first."},
			{Files: []string{"b.go"}, Message: "Not reviewed: second."},
		},
		Batches: 1,
	}
	one := review.SkillResult{
		Skill:    "one-failure",
		Verdict:  review.Failed,
		Failures: []review.BatchFailure{{Files: []string{"c.go"}, Message: "Not reviewed: third."}},
		Batches:  1,
	}
	noFiles := review.SkillResult{
		Skill:    "no-files",
		Verdict:  review.Failed,
		Failures: []review.BatchFailure{{Message: "Not reviewed: fourth."}, {Message: "Not reviewed: fifth."}},
	}
	got := FormatReview([]review.SkillResult{two, one, noFiles}, nil)

	for _, want := range []string{
		"\u26a0\ufe0f `two-failures`: Files `a.go`: **Review failed:** Not reviewed: first.",
		"\u26a0\ufe0f `two-failures`: Files `b.go`: **Review failed:** Not reviewed: second.",
		"\u26a0\ufe0f `one-failure`: **Review failed:** Not reviewed: third.",
		"\u26a0\ufe0f `no-files`: **Review failed:** Not reviewed: fourth.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("comment is missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "`c.go`:") {
		t.Errorf("a single failure in a single-batch skill lists its files:\n%s", got)
	}
}

// bigResults returns a review whose comment is large mostly because of its
// suggestions, the hidden findings block and its skipped-files list.
func bigResults() ([]review.SkillResult, []string) {
	fs := []finding.Finding{
		{File: "main.go", Line: 1, Severity: finding.Blocking, Title: "Blocking one", Explanation: "First blocking explanation."},
		{File: "main.go", Line: 2, Severity: finding.Blocking, Title: "Blocking two", Explanation: "Second blocking explanation."},
	}
	for i := range 100 {
		fs = append(fs, finding.Finding{
			File: fmt.Sprintf("pkg/file%03d.go", i), Line: i + 1, Severity: finding.Suggestion,
			Title:       fmt.Sprintf("Suggestion %03d %s", i, strings.Repeat("t", 60)),
			Explanation: "Detail " + strings.Repeat("e", 300), SuggestedFix: "Fix " + strings.Repeat("f", 50),
		})
	}
	var skipped []diff.Skip
	var changed []string
	for i := range 100 {
		p := fmt.Sprintf("vendor/module/path/generated%03d.go", i)
		skipped = append(skipped, diff.Skip{Path: p, Rule: "vendored"})
		changed = append(changed, p)
	}
	return []review.SkillResult{
		{Skill: "code-review", Verdict: review.HasBlockingIssues, Findings: fs, Skipped: skipped},
		{Skill: "docs-review", Verdict: review.Failed, Failures: []review.BatchFailure{{Files: []string{"a.md"}, Message: "Not reviewed: the failure stays."}}},
	}, changed
}

const (
	wantHiddenNote      = "> Findings were too large to store for the next run; --previous-review will treat the next run as a first review."
	wantDetailsNote     = "> Suggestion details omitted to fit the comment size limit."
	wantSuggestionsNote = "> 100 suggestions omitted to fit the comment size limit."
)

func TestFormatReviewWithin_NoLimitIsFormatReview(t *testing.T) {
	results, changed := bigResults()
	full := FormatReview(results, changed)
	if got := FormatReviewWithin(results, changed, 0); got != full {
		t.Error("FormatReviewWithin(0) differs from FormatReview")
	}
	if got := FormatReviewWithin(results, changed, len(full)); got != full {
		t.Error("FormatReviewWithin(len(full)) changed a comment that fits")
	}
}

func TestFormatReviewWithin_ShrinksInSteps(t *testing.T) {
	results, changed := bigResults()
	full := FormatReview(results, changed)

	tests := []struct {
		name    string
		maxLen  int
		want    []string
		notWant []string
	}{
		{
			name:    "drops the hidden block first",
			maxLen:  len(full) - 1,
			want:    []string{wantHiddenNote, "Detail eee", "**Fix:** Fix fff", "- `vendor/module/path/generated099.go`"},
			notWant: []string{findingsMarker, wantDetailsNote, wantSuggestionsNote},
		},
		{
			name:    "then the suggestions' details",
			maxLen:  30000,
			want:    []string{wantHiddenNote, wantDetailsNote, "<b>Suggestion 099 ttt", "- `vendor/module/path/generated099.go`"},
			notWant: []string{findingsMarker, "Detail eee", "Fix fff", wantSuggestionsNote},
		},
		{
			name:    "then the suggestions",
			maxLen:  12000,
			want:    []string{wantHiddenNote, wantSuggestionsNote, "- `vendor/module/path/generated099.go`"},
			notWant: []string{findingsMarker, "Suggestion 000", "\U0001f4a1"},
		},
		{
			name:    "then the file lists",
			maxLen:  4000,
			want:    []string{wantHiddenNote, wantSuggestionsNote, "Skipped files (100)", "more not listed to fit the comment size limit"},
			notWant: []string{findingsMarker, "Suggestion 000", "generated099.go"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatReviewWithin(results, changed, tt.maxLen)
			if len(got) > tt.maxLen {
				t.Errorf("len = %d, want at most %d", len(got), tt.maxLen)
			}
			for _, w := range append(tt.want, "<b>Blocking one</b>", "First blocking explanation.", "<b>Blocking two</b>", "Not reviewed: the failure stays.") {
				if !strings.Contains(got, w) {
					t.Errorf("comment is missing %q:\n%s", w, got)
				}
			}
			for _, nw := range tt.notWant {
				if strings.Contains(got, nw) {
					t.Errorf("comment still contains %q:\n%s", nw, got)
				}
			}
			if prev, err := ParsePreviousFindings(got); prev != nil || err != nil {
				t.Errorf("ParsePreviousFindings = %v, %v; want nil, nil (a first review next time)", prev, err)
			}
		})
	}
}

func TestFormatReviewWithin_LastResortStaysWithinLimit(t *testing.T) {
	results, changed := bigResults()
	got := FormatReviewWithin(results, changed, 600)
	if len(got) > 600 {
		t.Errorf("len = %d, want at most 600", len(got))
	}
	if !strings.HasPrefix(got, "<!-- pergent -->\n") || !strings.Contains(got, "> Comment truncated to fit the comment size limit.") {
		t.Errorf("want the marker kept and a truncation note:\n%s", got)
	}
}

func confidenceResults() []review.SkillResult {
	return []review.SkillResult{{
		Skill: "code-review", Verdict: review.HasBlockingIssues, Confidence: 4, ConfidenceReason: "R",
		Findings: []finding.Finding{
			{File: "main.go", Line: 42, EndLine: 43, Severity: finding.Blocking, Title: "Nil dereference", Explanation: "p can be nil.", Suggestion: "x", Fingerprint: "aaaaaaaaaaaa"},
		},
	}}
}

func TestFormatReview_ConfidenceAndSuggestion(t *testing.T) {
	got := FormatReview(confidenceResults(), []string{"main.go"})
	for _, want := range []string{"Confidence 3/5 (capped)", "(capped: 1 blocking finding)", "_Suggested change, replacing lines 42–43:_"} {
		if !strings.Contains(got, want) {
			t.Errorf("comment lacks %q:\n%s", want, got)
		}
	}
}

func TestParsePreviousFindings_KeepsSuggestion(t *testing.T) {
	prev, err := ParsePreviousFindings(FormatReview(confidenceResults(), []string{"main.go"}))
	if err != nil {
		t.Fatal(err)
	}
	fs := prev["code-review"]
	if len(fs) != 1 || fs[0].EndLine != 43 || fs[0].Suggestion != "x" {
		t.Errorf("previous findings = %+v, want EndLine 43 and Suggestion x", fs)
	}
}
