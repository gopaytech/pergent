package rule

import (
	"strings"
	"testing"
	"testing/fstest"
	"unicode/utf8"
)

func TestBuiltinLoads(t *testing.T) {
	got := Builtin()
	// PR 1 ships the engine with no built-in texts.
	if len(got) != 0 {
		t.Errorf("Builtin() has %d entries, want 0 until the texts ship", len(got))
	}
	for _, e := range got {
		if e.Source != SourceBuiltIn || e.Name == "" {
			t.Errorf("built-in %+v has no name or the wrong source", e)
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
