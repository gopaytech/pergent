package output

import (
	"strings"
	"testing"

	"github.com/gopaytech/pergent/internal/review"
)

func TestOutputPassesFileRules(t *testing.T) {
	const secret = "glpat-secretvalue"
	t.Setenv("GITLAB_TOKEN", secret)
	results := []review.SkillResult{{Skill: "s", Verdict: review.NoBlockingIssues, Batches: 1, BatchesFinished: 1,
		Reviewed:  []string{"a.go", secret + ".go"},
		FileRules: []review.FileRule{{Path: "a.go", Rule: "repo:**/*.go"}, {Path: secret + ".go", Rule: "repo:" + secret + "/**"}}}}
	c := FormatReviewWithinOptions(results, []string{"a.go"}, 0, Options{})
	if strings.Contains(c, secret) {
		t.Errorf("token leaked:\n%s", c)
	}
	if !strings.Contains(c, "Rules: repo `**/*.go` (1 file) · repo `[REDACTED]/**` (1 file)") {
		t.Errorf("no Rules line:\n%s", c)
	}
}

func TestOutputHeaderNote(t *testing.T) {
	results := []review.SkillResult{{Skill: "s", Verdict: review.NoBlockingIssues, Reviewed: []string{"a.go"}, Batches: 1, BatchesFinished: 1}}
	c := FormatReviewWithinOptions(results, []string{"a.go"}, 0, Options{HeaderNote: "Repository rules could not be read"})
	if !strings.Contains(c, "_Repository rules could not be read_") {
		t.Errorf("no header note:\n%s", c)
	}
}
