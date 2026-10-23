package output

import (
	"strings"
	"testing"
	"time"

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
