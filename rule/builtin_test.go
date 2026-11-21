package rule

import (
	"io/fs"
	"path"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"unicode/utf8"
)

func TestBuiltinLoads(t *testing.T) {
	got := Builtin()
	var names []string
	for _, e := range got {
		if e.Source != SourceBuiltIn || e.Name == "" || e.Off {
			t.Errorf("built-in %+v has no name, the wrong source, or is off", e)
		}
		if !slices.Contains(names, e.Name) {
			names = append(names, e.Name)
		}
	}
	want := []string{"github_workflows", "go", "ts_js", "python", "terraform"}
	if !slices.Equal(names, want) {
		t.Errorf("built-in names in order = %v, want %v", names, want)
	}
}

// Each file type gets its own text: a workflow is not a YAML file of any
// kind, a .tsx file is TS/JS, and a file of another type gets nothing.
func TestBuiltinMatches(t *testing.T) {
	r := NewResolver(nil, true)
	cases := map[string]string{
		".github/workflows/ci.yml":      "built-in:github_workflows",
		".github/workflows/rel.yaml":    "built-in:github_workflows",
		"internal/review/review.go":     "built-in:go",
		"web/src/pages/Review.tsx":      "built-in:ts_js",
		"web/vite.config.mts":           "built-in:ts_js",
		"scripts/check.py":              "built-in:python",
		"infra/main.tf":                 "built-in:terraform",
		"infra/prod.tfvars":             "built-in:terraform",
		"charts/pergent-sh/values.yaml": "",
		"README.md":                     "",
	}
	for p, want := range cases {
		e, ok := r.Resolve(p)
		got := ""
		if ok {
			got = e.Label()
		}
		if got != want {
			t.Errorf("%s gets %q, want %q", p, got, want)
		}
	}
}

// Every built-in text names its source on its first line, and every .md in
// the folder is used, so a text can't ship without its notice or be left out.
func TestBuiltinTextsNameTheirSource(t *testing.T) {
	used := map[string]bool{}
	for _, e := range Builtin() {
		used[e.Name+".md"] = true
		first, _, _ := strings.Cut(e.Rule, "\n")
		if !strings.HasPrefix(first, "<!-- ") || !strings.HasSuffix(first, " -->") || !strings.Contains(first, "open-code-review") {
			t.Errorf("built-in %s: first line %q does not name its source", e.Name, first)
		}
	}
	files, err := fs.Glob(builtinFS, "builtin/*.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if !used[path.Base(f)] {
			t.Errorf("%s is not in builtin.yaml", f)
		}
	}
	for _, f := range []string{"builtin/LICENSE", "builtin/NOTICE"} {
		if _, err := fs.Stat(builtinFS, f); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
}

func TestBuiltinTextsHaveNoBlocking(t *testing.T) {
	for _, e := range Builtin() {
		if strings.Contains(strings.ToLower(e.Rule), "blocking") {
			t.Errorf("built-in %s says what is blocking", e.Name)
		}
	}
}

func TestBuiltinTextsWithinLimits(t *testing.T) {
	for _, e := range Builtin() {
		if n := utf8.RuneCountInString(e.Rule); n > MaxRuleChars {
			t.Errorf("built-in %s is %d characters", e.Name, n)
		}
	}
}

func TestLoadBuiltinFS(t *testing.T) {
	fsys := fstest.MapFS{
		"builtin/builtin.yaml": {Data: []byte("rules:\n  - path: \"*.go\"\n    rule_file: go.md\n  - path: \"**/*.pb.go\"\n    off: true\n")},
		"builtin/go.md":        {Data: []byte("Check Go.\n")},
	}
	got, err := loadBuiltin(fsys)
	if err != nil {
		t.Fatalf("loadBuiltin: %v", err)
	}
	want := []Entry{
		{Path: "*.go", Rule: "Check Go.\n", Source: SourceBuiltIn, Name: "go"},
		{Path: "**/*.pb.go", Off: true, Source: SourceBuiltIn},
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("loadBuiltin = %+v, want %+v", got, want)
	}
}

func TestLoadBuiltinErrors(t *testing.T) {
	tests := map[string]fstest.MapFS{
		"missing file": {"builtin/builtin.yaml": {Data: []byte("rules:\n  - path: \"*.go\"\n    rule_file: go.md\n")}},
		"rule given":   {"builtin/builtin.yaml": {Data: []byte("rules:\n  - path: \"*.go\"\n    rule: x\n")}},
		"bad path":     {"builtin/builtin.yaml": {Data: []byte("rules:\n  - path: \"api/\"\n    rule_file: go.md\n")}, "builtin/go.md": {Data: []byte("x")}},
		"not md":       {"builtin/builtin.yaml": {Data: []byte("rules:\n  - path: \"*.go\"\n    rule_file: go.txt\n")}, "builtin/go.txt": {Data: []byte("x")}},
		"too long": {"builtin/builtin.yaml": {Data: []byte("rules:\n  - path: \"*.go\"\n    rule_file: go.md\n")},
			"builtin/go.md": {Data: []byte(strings.Repeat("x", MaxRuleChars+1))}},
	}
	for name, fsys := range tests {
		if _, err := loadBuiltin(fsys); err == nil {
			t.Errorf("%s: loadBuiltin passed", name)
		} else if !strings.HasPrefix(err.Error(), "builtin.yaml: ") {
			t.Errorf("%s: error %q does not name builtin.yaml", name, err)
		}
	}
}
