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

const goldenDir = "../../render/testdata/golden"

// TestFormatMatchesGoldens checks that the CLI's formatter, which now
// delegates to the render package, still produces byte for byte the comments
// it produced before (see goldenCase). With -update-golden-inputs it also
// rewrites <case>.json, the same cases as render's own input, which
// render's TestCommentMatchesOutputGoldens renders.
func TestFormatMatchesGoldens(t *testing.T) {
	setGoldenEnv(t)
	for _, c := range goldenCases() {
		t.Run(c.name, func(t *testing.T) {
			for _, n := range c.maxLens {
				path := filepath.Join(goldenDir, fmt.Sprintf("%s-%d.md", c.name, n))
				want := mustRead(t, path)
				got := FormatReviewWithinOptions(c.results, c.changed, n, Options{IDs: c.ids})
				if got != want {
					t.Errorf("maxLen %d: comment differs from %s (got %d bytes, want %d)", n, path, len(got), len(want))
				}
			}
			if got, want := FindingsBlock(c.results), mustRead(t, filepath.Join(goldenDir, c.name+".block")); got != want {
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
				if err := os.WriteFile(filepath.Join(goldenDir, c.name+".json"), append(data, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
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
