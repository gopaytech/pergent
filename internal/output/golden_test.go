package output

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gopaytech/pergent/internal/redact"
)

var updateInputs = flag.Bool("update-golden-inputs", false, "rewrite the render inputs in render/testdata/golden")

var updateGoldens = flag.Bool("update-goldens", false, "rewrite the expected comments and blocks in render/testdata/golden")

const goldenDir = "../../render/testdata/golden"

// TestFormatMatchesGoldens checks that the CLI's formatter, which now
// delegates to the render package, still produces byte for byte the comments
// it produced before (see goldenCase). With -update-golden-inputs it also
// rewrites <case>.json, the same cases as render's own input, which
// render's TestCommentMatchesOutputGoldens renders. With -update-goldens it
// writes the expected <case>-<n>.md and <case>.block files instead of
// comparing; check the diff of every file it changes.
func TestFormatMatchesGoldens(t *testing.T) {
	setGoldenEnv(t)
	for _, c := range goldenCases() {
		t.Run(c.name, func(t *testing.T) {
			for _, n := range c.maxLens {
				path := filepath.Join(goldenDir, fmt.Sprintf("%s-%d.md", c.name, n))
				got := FormatReviewWithinOptions(c.results, c.changed, n, Options{IDs: c.ids})
				if *updateGoldens {
					mustWrite(t, path, got)
					continue
				}
				want := mustRead(t, path)
				if got != want {
					t.Errorf("maxLen %d: comment differs from %s (got %d bytes, want %d)", n, path, len(got), len(want))
				}
			}
			if *updateGoldens {
				mustWrite(t, filepath.Join(goldenDir, c.name+".block"), FindingsBlock(c.results))
			} else if got, want := FindingsBlock(c.results), mustRead(t, filepath.Join(goldenDir, c.name+".block")); got != want {
				t.Errorf("FindingsBlock differs from the golden:\n got %q\nwant %q", got, want)
			}
			if *updateInputs {
				changed := make([]string, len(c.changed))
				for i, f := range c.changed {
					changed[i] = redact.String(f)
				}
				in := struct {
					Skills       any
					ChangedFiles []string
				}{toRender(c.results, c.ids), changed}
				data, err := json.MarshalIndent(in, "", " ")
				if err != nil {
					t.Fatal(err)
				}
				mustWrite(t, filepath.Join(goldenDir, c.name+".json"), string(data)+"\n")
			}
		})
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
