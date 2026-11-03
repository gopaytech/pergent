package main

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/gopaytech/pergent/internal/config"
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

func TestFailureLogLine(t *testing.T) {
	tests := []struct {
		f    review.BatchFailure
		want string
	}{
		{review.BatchFailure{Files: []string{"a.go", "b.go"}, Message: "Not reviewed: x."}, "skill s, files a.go, b.go: **Review failed:** Not reviewed: x."},
		{review.BatchFailure{Message: "Not reviewed: y."}, "skill s: **Review failed:** Not reviewed: y."},
	}
	for _, tt := range tests {
		if got := failureLogLine("s", tt.f); got != tt.want {
			t.Errorf("failureLogLine() = %q, want %q", got, tt.want)
		}
	}
}

func TestCommentLimit(t *testing.T) {
	for platform, want := range map[string]int{"github": 65536, "gitlab": 1000000, "": 0} {
		if got := commentLimit(platform); got != want {
			t.Errorf("commentLimit(%q) = %d, want %d", platform, got, want)
		}
	}
}

func TestCommentOptions(t *testing.T) {
	gh := config.Config{Platform: "github", GitHub: config.GitHubConfig{HeadSHA: "h1", WebURL: "https://github.com/o/r"}}
	o := commentOptions(gh)
	if o.Commit != "h1" || o.Version != "v"+version || o.FileURL == nil {
		t.Fatalf("github options = %+v", o)
	}
	if got := o.FileURL("a.go", 3, 0); got != "https://github.com/o/r/blob/h1/a.go?plain=1#L3" {
		t.Errorf("github link = %q", got)
	}

	gl := config.Config{Platform: "gitlab", GitLab: config.GitLabConfig{HeadSHA: "h2", WebURL: "https://gitlab.example.com/g/p"}}
	o = commentOptions(gl)
	if got := o.FileURL("a.go", 3, 0); got != "https://gitlab.example.com/g/p/-/blob/h2/a.go?plain=1#L3" {
		t.Errorf("gitlab link = %q", got)
	}

	// A missing commit or address means no link.
	gh.GitHub.HeadSHA = ""
	if o = commentOptions(gh); o.FileURL != nil {
		t.Error("link built without a commit")
	}

	// Local mode has no links and no commit.
	local := config.Config{Local: true, GitHub: gh.GitHub}
	if o = commentOptions(local); o.FileURL != nil || o.Commit != "" || o.Version != "v"+version {
		t.Errorf("local options = %+v", o)
	}
}
