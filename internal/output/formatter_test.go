package output

import (
	"strings"
	"testing"

	"github.com/gopaytech/pergent/internal/runner"
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
