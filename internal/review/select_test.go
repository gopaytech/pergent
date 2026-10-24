package review

import (
	"slices"
	"strings"
	"testing"

	"github.com/gopaytech/pergent/internal/diff"
	"github.com/gopaytech/pergent/internal/skill"
)

// patch returns a one-hunk diff of p whose new line is body.
func patch(p, body string) string {
	return "diff --git a/" + p + " b/" + p + "\n--- a/" + p + "\n+++ b/" + p + "\n@@ -1 +1 @@\n-old\n+" + body + "\n"
}

func deletedPatch(p string) string {
	return "diff --git a/" + p + " b/" + p + "\ndeleted file mode 100644\n--- a/" + p + "\n+++ /dev/null\n@@ -1 +0,0 @@\n-old\n"
}

func paths(fs []diff.FileDiff) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Path)
	}
	return out
}

func TestSelect(t *testing.T) {
	files := diff.Split(strings.Join([]string{
		patch("cmd/a.go", "x"),
		patch("manifests/app.yaml", "x"),
		patch("manifests/README.md", "x"),
		patch("go.sum", "x"),
		deletedPatch("manifests/old.yaml"),
	}, ""))

	tests := []struct {
		name        string
		skill       skill.Skill
		wantIn      []string
		wantSkipped []diff.Skip
	}{
		{
			name:   "paths and exclude",
			skill:  skill.Skill{Name: "s", Paths: []string{"**/manifests/**"}, Exclude: []string{"**/*.md"}},
			wantIn: []string{"manifests/app.yaml"},
			wantSkipped: []diff.Skip{
				{Path: "cmd/a.go", Rule: "out of scope"},
				{Path: "manifests/README.md", Rule: "excluded by skill"},
				{Path: "go.sum", Rule: "out of scope"},
				{Path: "manifests/old.yaml", Rule: "deleted"},
			},
		},
		{
			name:   "no paths skips lockfile",
			skill:  skill.Skill{Name: "s"},
			wantIn: []string{"cmd/a.go", "manifests/app.yaml", "manifests/README.md"},
			wantSkipped: []diff.Skip{
				{Path: "go.sum", Rule: "lockfile"},
				{Path: "manifests/old.yaml", Rule: "deleted"},
			},
		},
		{
			name:   "paths opt in to lockfile",
			skill:  skill.Skill{Name: "s", Paths: []string{"go.sum"}},
			wantIn: []string{"go.sum"},
			wantSkipped: []diff.Skip{
				{Path: "cmd/a.go", Rule: "out of scope"},
				{Path: "manifests/app.yaml", Rule: "out of scope"},
				{Path: "manifests/README.md", Rule: "out of scope"},
				{Path: "manifests/old.yaml", Rule: "deleted"},
			},
		},
		{
			name:   "deleted even with match-all paths",
			skill:  skill.Skill{Name: "s", Paths: []string{"**"}},
			wantIn: []string{"cmd/a.go", "manifests/app.yaml", "manifests/README.md", "go.sum"},
			wantSkipped: []diff.Skip{
				{Path: "manifests/old.yaml", Rule: "deleted"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in, skipped := Select(tt.skill, files, diff.Classifier{})
			if got := paths(in); !slices.Equal(got, tt.wantIn) {
				t.Errorf("in = %q, want %q", got, tt.wantIn)
			}
			if !slices.Equal(skipped, tt.wantSkipped) {
				t.Errorf("skipped = %+v, want %+v", skipped, tt.wantSkipped)
			}
		})
	}
}
