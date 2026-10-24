package output

import (
	"encoding/base64"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gopaytech/pergent/internal/diff"
	"github.com/gopaytech/pergent/internal/finding"
	"github.com/gopaytech/pergent/internal/review"
	"github.com/gopaytech/pergent/internal/runner"
)

func TestFormatComment_SingleSkill(t *testing.T) {
	results := []runner.RunResult{
		{
			SkillName: "code-review",
			Status:    runner.StatusCompleted,
			Output:    "- **[main.go:42]** Missing error handling",
		},
	}
	files := []string{"main.go"}

	comment := FormatComment(results, files)

	if !strings.Contains(comment, "<!-- pergent -->") {
		t.Error("missing pergent marker")
	}
	if !strings.Contains(comment, "## pergent review") {
		t.Error("missing header")
	}
	if !strings.Contains(comment, "**Skills:** code-review") {
		t.Error("missing skills list")
	}
	if !strings.Contains(comment, "`main.go`") {
		t.Error("missing file list")
	}
	if !strings.Contains(comment, "<!-- pergent:code-review -->") {
		t.Error("missing skill marker")
	}
	if !strings.Contains(comment, "Missing error handling") {
		t.Error("missing review content")
	}
}

func TestFormatComment_MultipleSkills(t *testing.T) {
	results := []runner.RunResult{
		{SkillName: "code-review", Status: runner.StatusCompleted, Output: "Looks good."},
		{SkillName: "security-review", Status: runner.StatusCompleted, Output: "No issues found."},
	}
	files := []string{"main.go", "handler.go"}

	comment := FormatComment(results, files)

	if !strings.Contains(comment, "**Skills:** code-review, security-review") {
		t.Error("missing combined skills list")
	}
	if !strings.Contains(comment, "<!-- pergent:code-review -->") {
		t.Error("missing code-review marker")
	}
	if !strings.Contains(comment, "<!-- pergent:security-review -->") {
		t.Error("missing security-review marker")
	}
}

func TestFormatComment_FailedRun(t *testing.T) {
	results := []runner.RunResult{
		{SkillName: "code-review", Status: runner.StatusCompleted, Output: "Looks good."},
		runner.Failed("security-review", runner.Cause{
			Kind:           runner.CauseTimeout,
			Steps:          9,
			Timeout:        10 * time.Minute,
			Elapsed:        10 * time.Minute,
			LastTool:       "read",
			LastToolTarget: "service/store.go",
		}),
	}

	comment := FormatComment(results, []string{"main.go"})

	want := "<!-- pergent:security-review -->\n### security-review\n\n" +
		"**Review failed:** Timed out after 10m0s (limit 10m0s) waiting for the model after step 9; the last tool call was `read` on `service/store.go`.\n" +
		"<!-- /pergent:security-review -->\n"
	if !strings.Contains(comment, want) {
		t.Errorf("comment =\n%s\nwant section\n%s", comment, want)
	}
	if !strings.Contains(comment, "Looks good.") {
		t.Error("the other skill's review is missing")
	}
	if strings.Contains(comment, "truncated") {
		t.Error("a failed run must not be presented as a truncated review")
	}
}

func TestFormatComment_FailedRunWithStderr(t *testing.T) {
	results := []runner.RunResult{
		runner.Failed("code-review", runner.Cause{
			Kind:        runner.CauseExitError,
			Steps:       2,
			ExitCode:    1,
			StderrLines: []string{"Error: Configuration is invalid", "  model: expected string"},
		}),
	}

	comment := FormatComment(results, nil)

	want := "**Review failed:** opencode exited with code 1 at step 2 without producing a review.\n\n" +
		"Last lines of opencode's stderr:\n\n```text\nError: Configuration is invalid\n  model: expected string\n```\n"
	if !strings.Contains(comment, want) {
		t.Errorf("comment =\n%s\nwant\n%s", comment, want)
	}
}

func TestFormatComment_StderrWithBackticksKeepsFence(t *testing.T) {
	text := FailureText(&runner.Cause{Kind: runner.CauseExitError, ExitCode: 1, StderrLines: []string{"```oops```"}})
	if !strings.Contains(text, "````text\n```oops```\n````") {
		t.Errorf("FailureText() = %q, want a fence longer than the backticks inside", text)
	}
}

func TestFormatComment_RedactsSecrets(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "sk-test-abcdef0123456789")
	t.Setenv("GITLAB_TOKEN", "glpat-0123456789abcdef")

	results := []runner.RunResult{
		runner.Failed("code-review", runner.Cause{
			Kind:         runner.CauseOpencodeError,
			ErrorName:    "ProviderAuthError",
			ErrorMessage: "Incorrect API key provided: sk-test-abcdef0123456789",
			StderrLines:  []string{"request: Authorization: Bearer 0123456789abcdefXYZ", "token glpat-0123456789abcdef"},
		}),
		{SkillName: "security-review", Status: runner.StatusCompleted, Output: "Found glpat-0123456789abcdef in config.yaml"},
	}

	comment := FormatComment(results, nil)

	for _, secret := range []string{"sk-test-abcdef0123456789", "glpat-0123456789abcdef", "0123456789abcdefXYZ"} {
		if strings.Contains(comment, secret) {
			t.Errorf("comment leaks %q:\n%s", secret, comment)
		}
	}
	if !strings.Contains(comment, "opencode reported ProviderAuthError: Incorrect API key provided: [REDACTED].") {
		t.Errorf("comment =\n%s\nwant the redacted error message", comment)
	}
}

func TestFailureText_NilCause(t *testing.T) {
	if got := FailureText(nil); !strings.HasPrefix(got, "**Review failed:**") {
		t.Errorf("FailureText(nil) = %q, want a failure line", got)
	}
}

func TestIsFailedSection(t *testing.T) {
	body := FormatComment([]runner.RunResult{
		runner.Failed("code-review", runner.Cause{Kind: runner.CauseNoAnswer, Steps: 3}),
		{SkillName: "security-review", Status: runner.StatusCompleted, Output: "Looks good."},
	}, nil)

	if !IsFailedSection(ExtractSkillSection(body, "code-review")) {
		t.Error("a failed skill's section should be detected as a failure")
	}
	if IsFailedSection(ExtractSkillSection(body, "security-review")) {
		t.Error("a review should not be detected as a failure")
	}
}

func TestExtractSkillSection_Found(t *testing.T) {
	body := "<!-- pergent -->\n## pergent review\n\n" +
		"<!-- pergent:code-review -->\n### code-review\n\nFinding A\n<!-- /pergent:code-review -->\n\n" +
		"<!-- pergent:security-review -->\n### security-review\n\nFinding B\n<!-- /pergent:security-review -->\n"

	section := ExtractSkillSection(body, "code-review")

	if !strings.Contains(section, "Finding A") {
		t.Errorf("section = %q, should contain code-review finding", section)
	}
	if strings.Contains(section, "Finding B") {
		t.Errorf("section = %q, should not contain other skill's finding", section)
	}
	if strings.Contains(section, "<!-- pergent:code-review -->") {
		t.Errorf("section = %q, should not include the opening marker", section)
	}
	if strings.Contains(section, "<!-- /pergent:code-review -->") {
		t.Errorf("section = %q, should not include the closing marker", section)
	}
}

func TestExtractSkillSection_Missing(t *testing.T) {
	body := "<!-- pergent -->\n## pergent review\n\n" +
		"<!-- pergent:code-review -->\n### code-review\n\nFinding A\n<!-- /pergent:code-review -->\n"

	if section := ExtractSkillSection(body, "security-review"); section != "" {
		t.Errorf("section = %q, want empty for missing skill", section)
	}
}

func TestExtractSkillSection_MissingEndMarker(t *testing.T) {
	body := "<!-- pergent:code-review -->\n### code-review\n\nFinding A\n"

	if section := ExtractSkillSection(body, "code-review"); section != "" {
		t.Errorf("section = %q, want empty when end marker is missing", section)
	}
}

func TestExtractSkillSection_EmptyBody(t *testing.T) {
	if section := ExtractSkillSection("", "code-review"); section != "" {
		t.Errorf("section = %q, want empty for empty body", section)
	}
}

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
		"<!-- pergent -->\n## pergent review",
		"<details><summary>Files changed (2)</summary>",
		"- `main.go`",
		"<!-- pergent:code-review -->",
		"### code-review",
		"\u274c **2 blocking issues**",
		"\u274c **1 blocking issue**\n",
		"\u2705 **No blocking issues**",
		"**`main.go:42`** \u2014 **Nil dereference**",
		"p can be nil here.",
		"_Suggested fix:_ Check p first.",
		"**`util.go`** \u2014 **Leak**",
		"<details><summary>Suggestions (1)</summary>",
		"**`util.go:7`** \u2014 **Rename helper**",
		"<details><summary>Skipped files (1)</summary>",
		"- `go.sum` \u2014 lockfile",
		"> batch 2 was small",
		"\u26a0\ufe0f Files `a.md`: **Review failed:**",
		"Files `b.md`, `c.md`:",
		"The model's answer was not in the required format.",
		"<!-- /pergent:code-review -->",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("comment is missing %q:\n%s", want, got)
		}
	}
	if !strings.HasPrefix(got, "<!-- pergent -->\n## pergent review") {
		t.Errorf("comment starts with %q", got[:40])
	}
	if strings.Contains(got, "_Suggested fix:_ \n") || strings.Count(got, "_Suggested fix:_") != 1 {
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

	if !strings.Contains(got, "\u26a0\ufe0f **Review failed:**") || !strings.Contains(got, "**Real bug**") {
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
	old := FormatComment([]runner.RunResult{{SkillName: "code-review", Status: runner.StatusCompleted, Output: "Looks good."}}, nil)
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
	if o, c := strings.Count(visible, "<details>"), strings.Count(visible, "</details>"); o != c {
		t.Errorf("%d <details> but %d </details>:\n%s", o, c, got)
	}
	// Walk the lines the way markdown does: every fence opened must close
	// before the following skill's section and the hidden line.
	open := ""
	for _, line := range strings.Split(visible, "\n") {
		if strings.HasPrefix(line, "<!-- /pergent:first -->") && open != "" {
			t.Errorf("fence %q is still open at the end of the first skill:\n%s", open, got)
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
	if !strings.Contains(visible, "\u2705 **No blocking issues**\n\n<!-- /pergent:second -->") {
		t.Errorf("the second skill's verdict is missing or displaced:\n%s", got)
	}
	if !strings.Contains(visible, "**Bad `name` ``` second line**") {
		t.Errorf("title was not collapsed to one line:\n%s", got)
	}
	if !strings.Contains(visible, "<details><summary>Suggestions (2)</summary>") {
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
