package render_test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/gopaytech/pergent/render"
)

var updateGoldens = flag.Bool("update-goldens", false, "rewrite render/testdata/golden/pergentsh-0.md")

// TestCommentWithHostedServiceOptions pins, byte for byte, the comment a
// service that re-renders stored results gets: file links, a dismiss command,
// a findings title, a finding note, a footer, a version and a commit.
func TestCommentWithHostedServiceOptions(t *testing.T) {
	skills := []render.Skill{{
		Name: "code-review", Verdict: render.VerdictHasBlockingIssues,
		Confidence: 4, ConfidenceReason: "The token check is clear; the rest is small.",
		Reviewed: []string{"internal/auth/token.go", "README.md"},
		Findings: []render.Finding{
			{
				ID: "a1b2c3", Fingerprint: "a1b2c3d4e5f6", File: "internal/auth/token.go", Line: 42, EndLine: 44,
				Severity: render.Blocking, Title: "Expired tokens are accepted",
				Explanation:  "The expiry is compared with `>` where `<` is meant, so an expired token passes.",
				SuggestedFix: "Reject the token when `exp` is before now.",
				Suggestion:   "\tif claims.Exp < now {\n\t\treturn ErrExpired\n\t}",
			},
			{
				ID: "d4e5f6", Fingerprint: "d4e5f6a1b2c3", File: "README.md", Line: 17,
				Severity: render.Suggestion, Title: "Document the token lifetime",
				Explanation: "The README does not say how long a token is valid.",
			},
		},
	}}
	got := render.Comment(skills, render.Options{
		ChangedFiles:   []string{"internal/auth/token.go", "README.md"},
		FileURL:        render.GitHubFileURL("https://github.com/owner/repo", "0123456789abcdef"),
		DismissCommand: "@pergent-review dismiss",
		FindingsTitle:  "Not on the diff",
		FindingNote:    map[string]string{"d4e5f6a1b2c3": "Line 17 is not part of the diff, so it could not be posted inline."},
		Footer:         "Run again: `@pergent-review review`",
		Version:        "v0.3.0",
		Commit:         "0123456789abcdef",
	})

	path := filepath.Join("testdata", "golden", "pergentsh-0.md")
	if *updateGoldens {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if want := readFile(t, path); got != want {
		t.Errorf("comment differs from %s:\n got:\n%s\nwant:\n%s", path, got, want)
	}
}
