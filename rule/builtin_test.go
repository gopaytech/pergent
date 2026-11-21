package rule

import (
	"io/fs"
	"path"
	"regexp"
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
	// open-code-review's texts in the order of its system_rules.json, less
	// scala and objc, which are over MaxRuleChars.
	want := []string{
		"properties", "mapper_dao_xml", "pom_xml", "build_gradle", "package_json", "cargo_toml", "move_toml", "move",
		"composer_json", "json", "github_workflows", "github_config", "yaml", "java", "go", "freemarker",
		"handlebars_mustache", "jinja", "pug", "ejs_liquid_njk", "haml_slim", "erb", "arkts", "astro",
		"ts_js", "kotlin", "fsharp", "csharp", "rust", "cpp", "c", "cuda_hip",
		"python", "ruby", "php", "protobuf", "po", "pot", "graphql", "prisma",
		"julia", "r", "terraform", "bicep", "dhall", "nix", "haskell", "nim",
		"swift", "nushell", "elm", "jsonnet", "zig", "thrift", "starlark", "capnp",
		"ocaml", "verilog", "vhdl", "matlab", "solidity", "vyper", "rego", "shader",
		"powershell", "clojure", "sql",
	}
	if !slices.Equal(names, want) {
		t.Errorf("built-in names in order = %v, want %v", names, want)
	}
}

// Each file type gets its own text, first match wins: a workflow is not a
// .github config file or a YAML file of any kind, a .tsx file is TS/JS, and
// scala, objc and a file of another type get nothing.
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
		"charts/pergent-sh/values.yaml": "built-in:yaml",
		".github/dependabot.yml":        "built-in:github_config",
		"Cargo.toml":                    "built-in:cargo_toml",
		"web/package.json":              "built-in:package_json",
		"analysis/model.R":              "built-in:r",
		"src/App.scala":                 "",
		"ios/View.mm":                   "",
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

// severityWords finds a sentence that says what blocks ("Treat security
// findings as blocking", "not blocking", "blocking findings"). A rule never
// blocks. "Blocking I/O" and Verilog's blocking assignments stay: they name
// what the code does, not a verdict.
var severityWords = regexp.MustCompile(`(?i)\bas (non-)?blocking\b|\b(non-)?blocking (finding|issue|suggestion|comment)s?\b|\bnot blocking\b|\btreat\b[^.\n]*\bblocking\b`)

func TestBuiltinTextsHaveNoBlocking(t *testing.T) {
	for _, s := range []string{
		"Treat security and correctness findings as blocking, and style suggestions as non-blocking.",
		"report as minor, not blocking",
		"into blocking findings.",
	} {
		if !severityWords.MatchString(s) {
			t.Errorf("severityWords misses %q", s)
		}
	}
	for _, s := range []string{"- Blocking I/O, waits", "Mixed blocking and non-blocking assignments"} {
		if severityWords.MatchString(s) {
			t.Errorf("severityWords matches %q", s)
		}
	}
	for _, e := range Builtin() {
		if m := severityWords.FindString(e.Rule); m != "" {
			t.Errorf("built-in %s says what is blocking: %q", e.Name, m)
		}
	}
}

func TestExpandBraces(t *testing.T) {
	cases := map[string][]string{
		"**/*.go":              {"**/*.go"},
		"**/*.{ts,js}":         {"**/*.ts", "**/*.js"},
		"**/{BUILD,WORKSPACE}": {"**/BUILD", "**/WORKSPACE"},
		"{a,b}/*.{x,y}":        {"a/*.x", "a/*.y", "b/*.x", "b/*.y"},
		"**/*.{go":             {"**/*.{go"},
	}
	for in, want := range cases {
		if got := expandBraces(in); !slices.Equal(got, want) {
			t.Errorf("expandBraces(%q) = %q, want %q", in, got, want)
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
