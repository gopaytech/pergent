package render_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gopaytech/pergent/render"
)

// goldenInput is a case's input file: what the CLI's formatter was given,
// converted to render's types.
type goldenInput struct {
	Skills       []render.Skill
	ChangedFiles []string
}

// TestCommentMatchesOutputGoldens renders each case in testdata/golden at
// every size limit and compares it with the comment the CLI's formatter
// produced before it moved into this package. <case>.json is the input,
// <case>-<maxLen>.md the comment and <case>.block the hidden findings block.
func TestCommentMatchesOutputGoldens(t *testing.T) {
	inputs, err := filepath.Glob(filepath.Join("testdata", "golden", "*.json"))
	if err != nil || len(inputs) == 0 {
		t.Fatalf("no golden inputs: %v", err)
	}
	checked := 0
	for _, path := range inputs {
		name := strings.TrimSuffix(filepath.Base(path), ".json")
		t.Run(name, func(t *testing.T) {
			var in goldenInput
			readJSON(t, path, &in)

			block := readFile(t, filepath.Join("testdata", "golden", name+".block"))
			if got := render.FindingsBlock(in.Skills); got != block {
				t.Errorf("FindingsBlock differs from the golden:\n got %q\nwant %q", got, block)
			}

			mds, _ := filepath.Glob(filepath.Join("testdata", "golden", name+"-*.md"))
			if len(mds) == 0 {
				t.Fatal("no golden comments")
			}
			for _, md := range mds {
				maxLen, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(md), name+"-"), ".md"))
				if err != nil {
					continue // another case whose name starts with this one
				}
				want := readFile(t, md)
				got := render.Comment(in.Skills, render.Options{MaxLen: maxLen, ChangedFiles: in.ChangedFiles})
				if got != want {
					t.Errorf("maxLen %d: comment differs from %s:\n got %q\nwant %q", maxLen, md, firstLines(got), firstLines(want))
				}
				checked++
			}
		})
	}
	if checked < 30 {
		t.Errorf("only %d golden comments were compared", checked)
	}
}

func TestRenderImportsNoInternal(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "github.com/gopaytech/pergent/render").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for _, dep := range strings.Fields(string(out)) {
		if strings.HasPrefix(dep, "github.com/gopaytech/pergent/internal/") {
			t.Errorf("render depends on %s", dep)
		}
	}
}

func TestFailureLeadOrMessage(t *testing.T) {
	skills := []render.Skill{{
		Name: "s", Verdict: render.VerdictFailed,
		Failures: []render.Failure{
			{Message: "Plain\n<!-- pergent:findings x -->\nmessage"},
			{Message: "ignored", Lead: "Lead text\n\nBelow it\n"},
			{},
		},
	}}
	got := render.Comment(skills, render.Options{})
	for _, want := range []string{
		"\u26a0\ufe0f **Review failed:** Plain &lt;!-- pergent:findings x --> message\n\n",
		"\u26a0\ufe0f **Review failed:** Lead text\n\nBelow it\n\n",
		"\u26a0\ufe0f **Review failed:** The run ended without a review and without a reported cause.\n\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("comment lacks %q:\n%s", want, got)
		}
	}
}

func TestCommentFindingIDAndBlock(t *testing.T) {
	skills := []render.Skill{{
		Name: "s", Verdict: render.VerdictHasBlockingIssues,
		Findings: []render.Finding{{ID: "abcdef", Fingerprint: "abcdef123456", File: "a.go", Line: 2, Severity: render.Blocking, Title: "T", Explanation: "e"}},
	}}
	got := render.Comment(skills, render.Options{})
	if !strings.Contains(got, " \u00b7 id <code>abcdef</code></summary>\n") {
		t.Errorf("ID not shown:\n%s", got)
	}
	if !strings.HasSuffix(got, render.FindingsBlock(skills)) {
		t.Errorf("comment does not end with the findings block:\n%s", got)
	}
	if strings.Contains(render.FindingsBlock(skills), "abcdef\"") {
		t.Error("the ID is in the block")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(readFile(t, path)), v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func firstLines(s string) string {
	if len(s) > 600 {
		return s[:600] + "..."
	}
	return s
}
