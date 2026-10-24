package main

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/gopaytech/pergent/internal/finding"
	"github.com/gopaytech/pergent/internal/output"
	"github.com/gopaytech/pergent/internal/review"
)

func TestPreviousFindings(t *testing.T) {
	body := output.FormatReview([]review.SkillResult{
		{Skill: "with-findings", Verdict: review.HasBlockingIssues, Findings: []finding.Finding{
			{File: "a.go", Line: 3, Severity: finding.Blocking, Title: "Bug", Explanation: "Broken.", Fingerprint: "abc"},
		}},
		{Skill: "no-findings", Verdict: review.NoBlockingIssues},
	}, []string{"a.go"})

	got := previousFindings(body)

	if len(got["with-findings"]) != 1 || got["with-findings"][0].Title != "Bug" {
		t.Errorf("previousFindings()[with-findings] = %+v, want the one finding", got["with-findings"])
	}
	if _, ok := got["no-findings"]; ok {
		t.Errorf("previousFindings() has an entry for a skill without findings: %+v", got)
	}
}

func TestPreviousFindings_OlderCommentIsAFirstReview(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	defer log.SetOutput(os.Stderr)

	old := "<!-- pergent -->\n## pergent review\n\n<!-- pergent:code-review -->\n### code-review\n\nLooks good.\n<!-- /pergent:code-review -->\n"
	if got := previousFindings(old); len(got) != 0 {
		t.Errorf("previousFindings(old comment) = %+v, want none", got)
	}
	if logs.Len() != 0 {
		t.Errorf("an older comment logged %q, want no warning", logs.String())
	}
}

func TestPreviousFindings_CorruptBlockWarnsAndContinues(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	defer log.SetOutput(os.Stderr)

	got := previousFindings("<!-- pergent -->\n<!-- pergent:findings !!!notbase64 -->\n")

	if len(got) != 0 {
		t.Errorf("previousFindings(corrupt) = %+v, want none", got)
	}
	if !strings.Contains(logs.String(), "warning:") {
		t.Errorf("log = %q, want a warning", logs.String())
	}
}
