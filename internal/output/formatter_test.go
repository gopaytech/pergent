package output

import (
	"strings"
	"testing"

	"github.com/zufardhiyaulhaq/pergent/internal/runner"
)

func TestFormatComment_SingleSkill(t *testing.T) {
	results := []runner.RunResult{
		{
			SkillName: "code-review",
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
		{SkillName: "code-review", Output: "Looks good."},
		{SkillName: "security-review", Output: "No issues found."},
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

func TestFormatComment_TruncatedOutput(t *testing.T) {
	results := []runner.RunResult{
		{
			SkillName: "code-review",
			Output:    "Partial review...",
			Truncated: true,
		},
	}

	comment := FormatComment(results, []string{"main.go"})

	if !strings.Contains(comment, "truncated") {
		t.Error("missing truncation notice")
	}
}
