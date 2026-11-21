package rule

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

const exampleYAML = `rules:
  - path: "internal/api/**/*.go"
    rule: |
      Every handler that starts a database transaction must defer tx.Rollback().
  - path: "**/*.pb.go"
    off: true
`

const exampleJSON = `{"rules": [
  {"path": "internal/api/**/*.go", "rule": "Every handler that starts a database transaction must defer tx.Rollback().\n"},
  {"path": "**/*.pb.go", "off": true}
]}`

func TestLoadEntries(t *testing.T) {
	want := []Entry{
		{Path: "internal/api/**/*.go", Rule: "Every handler that starts a database transaction must defer tx.Rollback().\n", Source: "repo"},
		{Path: "**/*.pb.go", Off: true, Source: "repo"},
	}
	for name, data := range map[string]string{"yaml": exampleYAML, "json": exampleJSON} {
		got, err := Load([]byte(data), SourceRepo)
		if err != nil {
			t.Fatalf("%s: Load: %v", name, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: Load = %#v, want %#v", name, got, want)
		}
	}
}

func entries(n int) string {
	var b strings.Builder
	b.WriteString("rules:\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "  - path: \"f%d.go\"\n    rule: x\n", i)
	}
	return b.String()
}

func TestLoadErrors(t *testing.T) {
	long := strings.Repeat("é", MaxRuleChars+1)
	tests := []struct {
		name, source, data, want string
	}{
		{"unknown entry field", SourceRepo, "rules:\n  - path: a.go\n    rule: x\n  - paths: b.go\n    rule: y\n",
			`.pergent/rules.yaml: entry 2: unknown field "paths"`},
		{"unknown top-level key", SourceRepo, "rule:\n  - path: a.go\n",
			`.pergent/rules.yaml: unknown field "rule"`},
		{"both rule and off", SourceRepo, "rules:\n  - path: a.go\n    rule: x\n    off: true\n",
			`.pergent/rules.yaml: entry 1: has both rule and off: true; use one`},
		{"neither", SourceRepo, "rules:\n  - path: a.go\n",
			`.pergent/rules.yaml: entry 1: needs a rule or off: true`},
		{"off false without rule", SourceRepo, "rules:\n  - path: a.go\n    off: false\n",
			`.pergent/rules.yaml: entry 1: needs a rule or off: true`},
		{"blank rule", SourceRepo, "rules:\n  - path: a.go\n    rule: \"  \\n\"\n",
			`.pergent/rules.yaml: entry 1: needs a rule or off: true`},
		{"never matches", SourceRepo, "rules:\n  - path: a.go\n    rule: x\n  - path: b.go\n    rule: x\n  - path: api/\n    rule: x\n",
			`.pergent/rules.yaml: entry 3: path: pattern "api/" can never match a file: for a directory use "api/**"`},
		{"missing path", SourceRepo, "rules:\n  - rule: x\n",
			`.pergent/rules.yaml: entry 1: path: pattern is empty`},
		{"rule too long", SourceRepo, "rules:\n  - path: a.go\n    rule: " + long + "\n",
			`.pergent/rules.yaml: entry 1: rule is 16001 characters, over the limit of 16000`},
		{"too many entries", SourceRepo, entries(MaxEntries + 1),
			`.pergent/rules.yaml: 101 entries, over the limit of 100`},
		{"too big", SourceRepo, "rules: []\n" + strings.Repeat("#", MaxFileBytes+1-len("rules: []\n")),
			`.pergent/rules.yaml: 65537 bytes, over the limit of 65536 (64 KB)`},
		{"dashboard set named", DashboardSource("go-api"), "rules:\n  - path: a.go\n",
			`rule set "go-api": entry 1: needs a rule or off: true`},
		{"not a list", SourceRepo, "rules: a.go\n",
			`.pergent/rules.yaml: rules must be a list`},
		{"entry not a map", SourceRepo, "rules:\n  - a.go\n",
			`.pergent/rules.yaml: entry 1: must be a map with path and rule or off`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load([]byte(tt.data), tt.source)
			if err == nil {
				t.Fatalf("Load passed, want %q", tt.want)
			}
			if err.Error() != tt.want {
				t.Errorf("Load error\n got %q\nwant %q", err.Error(), tt.want)
			}
		})
	}
}

func TestLoadLimitsInclusive(t *testing.T) {
	if _, err := Load([]byte(entries(MaxEntries)), SourceRepo); err != nil {
		t.Errorf("100 entries: %v", err)
	}
	rule := strings.Repeat("é", MaxRuleChars)
	if _, err := Load([]byte("rules:\n  - path: a.go\n    rule: "+rule+"\n"), SourceRepo); err != nil {
		t.Errorf("16000 characters: %v", err)
	}
}

// The size check runs before parsing: a file over the limit is refused even
// when it is not YAML at all.
func TestLoadSizeBeforeParse(t *testing.T) {
	_, err := Load([]byte(strings.Repeat("{", MaxFileBytes+1)), SourceRepo)
	if err == nil || !strings.Contains(err.Error(), "over the limit of 65536") {
		t.Errorf("Load = %v, want the size error", err)
	}
}

func TestLoadOffNotBool(t *testing.T) {
	for _, v := range []string{"yes", `"true"`, "1"} {
		_, err := Load([]byte("rules:\n  - path: a.go\n    off: "+v+"\n"), SourceRepo)
		want := `.pergent/rules.yaml: entry 1: off must be true or false`
		if err == nil || err.Error() != want {
			t.Errorf("off: %s: Load = %v, want %q", v, err, want)
		}
	}
}

func TestLoadEmpty(t *testing.T) {
	for _, data := range []string{"", "rules:\n", "rules: []\n", "# only a comment\n"} {
		got, err := Load([]byte(data), SourceRepo)
		if err != nil || len(got) != 0 {
			t.Errorf("Load(%q) = %v, %v; want no entries and no error", data, got, err)
		}
	}
}

func TestLoadErrorNeverQuotesRule(t *testing.T) {
	_, err := Load([]byte("rules:\n  - path: a.go\n    rule: SECRET-TEXT\n    off: true\n"), SourceRepo)
	if err == nil || strings.Contains(err.Error(), "SECRET-TEXT") {
		t.Errorf("Load = %v, want an error that does not quote the rule", err)
	}
}

func TestListRoundTrip(t *testing.T) {
	in := []Entry{
		{Path: "a/**", Rule: "check a", Source: SourceRepo},
		{Path: "**/*.pb.go", Off: true, Source: SourceRepo},
		{Path: "*.go", Rule: "check go", Source: DashboardSource("a")},
	}
	data, err := EncodeList(in)
	if err != nil {
		t.Fatalf("EncodeList: %v", err)
	}
	got, err := DecodeList(data)
	if err != nil {
		t.Fatalf("DecodeList: %v", err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Errorf("round trip = %#v, want %#v", got, in)
	}
}

func TestDecodeListErrors(t *testing.T) {
	many := make([]string, MaxEntries+1)
	for i := range many {
		many[i] = fmt.Sprintf(`{"source":"repo","path":"f%d.go","rule":"x"}`, i)
	}
	tests := []struct{ name, data, want string }{
		{"built-in source", `[{"source":"built-in","path":"*.go","rule":"x"}]`,
			`rules list: entry 1: source "built-in" is not repo or dashboard:<set>`},
		{"bad set name", `[{"source":"dashboard:-x","path":"*.go","rule":"x"}]`,
			`rules list: entry 1: source "dashboard:-x" is not repo or dashboard:<set>`},
		{"empty set name", `[{"source":"dashboard:","path":"*.go","rule":"x"}]`,
			`rules list: entry 1: source "dashboard:" is not repo or dashboard:<set>`},
		{"no source", `[{"path":"*.go","rule":"x"}]`,
			`rules list: entry 1: source "" is not repo or dashboard:<set>`},
		{"bad entry", `[{"source":"repo","path":"*.go","rule":"x"},{"source":"repo","path":"api/","rule":"x"}]`,
			`rules list: entry 2: path: pattern "api/" can never match a file: for a directory use "api/**"`},
		{"unknown field", `[{"source":"repo","path":"*.go","rule":"x","paths":"y"}]`,
			`rules list: entry 1: unknown field "paths"`},
		{"too many of one source", "[" + strings.Join(many, ",") + "]",
			`rules list: 101 entries from repo, over the limit of 100`},
		{"too big", `[` + strings.Repeat(" ", MaxListBytes) + `]`,
			`rules list: 1048578 bytes, over the limit of 1048576 (1 MiB)`},
		{"not a list", `{"rules": []}`,
			`rules list: must be a JSON list of entries`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeList([]byte(tt.data))
			if err == nil || err.Error() != tt.want {
				t.Errorf("DecodeList error\n got %v\nwant %q", err, tt.want)
			}
		})
	}
}

func TestEncodeListRefusesBuiltIn(t *testing.T) {
	if _, err := EncodeList([]Entry{{Path: "*.go", Rule: "x", Source: SourceBuiltIn, Name: "go"}}); err == nil {
		t.Error("EncodeList took a built-in entry")
	}
}

func TestDecodeListEmpty(t *testing.T) {
	got, err := DecodeList([]byte("[]"))
	if err != nil || len(got) != 0 {
		t.Errorf("DecodeList([]) = %v, %v", got, err)
	}
}
