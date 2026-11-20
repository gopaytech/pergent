package graph

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func readValid(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "valid.json"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// changed returns the valid file with one change applied to its decoded form.
func changed(t *testing.T, change func(m map[string]any)) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(readValid(t), &m); err != nil {
		t.Fatal(err)
	}
	change(m)
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func node(m map[string]any, i int) map[string]any {
	return m["nodes"].([]any)[i].(map[string]any)
}

func edge(m map[string]any, i int) map[string]any {
	return m["edges"].([]any)[i].(map[string]any)
}

func TestDecode_Valid(t *testing.T) {
	f, err := Decode(bytes.NewReader(readValid(t)))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if f.Format != Format || f.Provider != "graphify" || len(f.Nodes) != 8 || len(f.Edges) != 6 {
		t.Fatalf("unexpected file: %+v", f)
	}
	if f.Nodes[2].EndLine != 18 {
		t.Errorf("end_line = %d, want 18", f.Nodes[2].EndLine)
	}
	kinds := map[string]bool{}
	for _, e := range f.Edges {
		kinds[e.Kind] = true
	}
	for _, k := range []string{EdgeCalls, EdgeImports, EdgeContains, EdgeInherits} {
		if !kinds[k] {
			t.Errorf("fixture has no %s edge", k)
		}
	}
	out, err := f.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.HasSuffix(out, []byte("}\n")) {
		t.Errorf("Encode output does not end with a newline")
	}
	again, err := Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("Decode of Encode: %v", err)
	}
	if !reflect.DeepEqual(f, again) {
		t.Errorf("round trip changed the file:\n%+v\n%+v", f, again)
	}
}

func TestDecode_Refused(t *testing.T) {
	setNode := func(i int, k string, v any) func(m map[string]any) {
		return func(m map[string]any) { node(m, i)[k] = v }
	}
	setEdge := func(i int, k string, v any) func(m map[string]any) {
		return func(m map[string]any) { edge(m, i)[k] = v }
	}
	set := func(k string, v any) func(m map[string]any) {
		return func(m map[string]any) { m[k] = v }
	}
	cases := []struct {
		name   string
		input  []byte
		change func(m map[string]any)
		leak   string // text of the bad value that must not be in the error
	}{
		{name: "not JSON", input: []byte("nope")},
		{name: "empty", input: []byte("")},
		{name: "truncated", input: readValid(t)[:100]},
		{name: "trailing object", input: append(readValid(t), []byte("{}")...)},
		{name: "unknown top-level field", change: set("secret_field", "x"), leak: "secret_field"},
		{name: "unknown node field", change: setNode(0, "label", "x"), leak: "label"},
		{name: "unknown edge field", change: setEdge(0, "context", "x"), leak: "context"},
		{name: "other format", change: set("format", "pergent-graph/v2"), leak: "v2"},
		{name: "bad provider", change: set("provider", "Bad Provider"), leak: "Bad Provider"},
		{name: "bad provider version", change: set("provider_version", "1 2"), leak: "1 2"},
		{name: "short commit", change: set("commit", "abc123"), leak: "abc123"},
		{name: "upper-case commit", change: set("commit", "0123456789ABCDEF0123456789abcdef01234567"), leak: "ABCDEF"},
		{name: "bad built_at", change: set("built_at", "yesterday"), leak: "yesterday"},
		{name: "duplicate id", change: setNode(1, "id", "n1")},
		{name: "bad id", change: setNode(1, "id", "n 2"), leak: "n 2"},
		{name: "bad kind", change: setNode(1, "kind", "class"), leak: "class"},
		{name: "empty kind", change: setNode(1, "kind", "")},
		{name: "name with a space", change: setNode(2, "name", "a b"), leak: "a b"},
		{name: "name with a newline", change: setNode(2, "name", "a\nb"), leak: "a\nb"},
		{name: "name starts with a digit", change: setNode(2, "name", "1x")},
		{name: "file name not the base name", change: setNode(0, "name", "other.go"), leak: "other.go"},
		{name: "config key with a space", change: func(m map[string]any) {
			node(m, 7)["kind"] = KindConfigKey
			node(m, 7)["name"] = "a b"
			m["edges"] = m["edges"].([]any)[:5]
		}, leak: "a b"},
		{name: "absolute path", change: setNode(0, "file", "/server.go"), leak: "/server.go"},
		{name: "parent path", change: setNode(5, "file", "../client.go"), leak: "../client.go"},
		{name: "unclean path", change: setNode(5, "file", "a/../client.go"), leak: "a/../client.go"},
		{name: "dot path", change: setNode(5, "file", "./client.go"), leak: "./client.go"},
		{name: "backslash path", change: setNode(5, "file", `a\client.go`), leak: `a\client.go`},
		{name: "NUL in path", change: setNode(5, "file", "a\x00.go")},
		{name: "path is dot", change: setNode(5, "file", ".")},
		{name: "empty path", change: setNode(5, "file", "")},
		{name: "line 0", change: setNode(2, "line", 0)},
		{name: "end_line below line", change: setNode(2, "end_line", 5)},
		{name: "edge to a missing id", change: setEdge(0, "to", "n99"), leak: "n99"},
		{name: "unknown edge kind", change: setEdge(0, "kind", "uses"), leak: "uses"},
		{name: "calls from a file", change: setEdge(0, "from", "n1")},
		{name: "calls from a type", change: setEdge(0, "from", "n2")},
		{name: "calls to a file", change: setEdge(0, "to", "n5")},
		{name: "imports to a function", change: setEdge(2, "to", "n3")},
		{name: "imports from a type", change: setEdge(2, "from", "n2")},
		{name: "contains a file", change: setEdge(3, "to", "n5")},
		{name: "contains from a function", change: setEdge(3, "from", "n6")},
		{name: "inherits from a function", change: setEdge(5, "from", "n3")},
		{name: "inherits to a file", change: setEdge(5, "to", "n7")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := c.input
			if c.change != nil {
				in = changed(t, c.change)
			}
			_, err := Decode(bytes.NewReader(in))
			if err == nil {
				t.Fatal("Decode accepted it")
			}
			if !strings.HasPrefix(err.Error(), "graph: ") {
				t.Errorf("error %q does not start with graph: ", err)
			}
			if c.leak != "" && strings.Contains(err.Error(), c.leak) {
				t.Errorf("error %q holds the bad value %q", err, c.leak)
			}
		})
	}
}

func TestReadFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "g.json")
	if err := os.WriteFile(p, readValid(t), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := ReadFile(p)
	if err != nil || len(f.Nodes) != 8 {
		t.Fatalf("ReadFile = %v, %v", f, err)
	}
	if _, err := ReadFile(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Error("ReadFile of a missing file gave no error")
	}
}

func TestReadFile_TooBig(t *testing.T) {
	p := filepath.Join(t.TempDir(), "big.json")
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(p, MaxBytes+1); err != nil {
		t.Fatal(err)
	}
	_, err := ReadFile(p)
	if err == nil || !strings.Contains(err.Error(), "64 MiB") {
		t.Fatalf("ReadFile = %v, want an error naming 64 MiB", err)
	}
}

func TestDecode_TooBig(t *testing.T) {
	old := maxBytes
	maxBytes = 100
	defer func() { maxBytes = old }()
	_, err := Decode(bytes.NewReader(readValid(t)))
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("Decode = %v, want a size error", err)
	}
}

func TestEncode_TooBig(t *testing.T) {
	f, err := Decode(bytes.NewReader(readValid(t)))
	if err != nil {
		t.Fatal(err)
	}
	old := maxBytes
	maxBytes = 100
	defer func() { maxBytes = old }()
	if _, err := f.Encode(); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("Encode = %v, want a size error", err)
	}
}

func TestEncode_Invalid(t *testing.T) {
	f := &File{Format: "nope"}
	if _, err := f.Encode(); err == nil {
		t.Fatal("Encode of an invalid file gave no error")
	}
}

func TestValidName(t *testing.T) {
	cases := []struct {
		kind, name, file string
		want             bool
	}{
		{KindFunction, "Dispatch", "a.go", true},
		{KindFunction, "日本", "a.go", true},
		{KindFunction, "$x", "a.js", true},
		{KindFunction, "_", "a.go", true},
		{KindFunction, "1x", "a.go", false},
		{KindFunction, "a b", "a.go", false},
		{KindFunction, "a.b", "a.go", false},
		{KindFunction, "", "a.go", false},
		{KindFunction, strings.Repeat("a", 128), "a.go", true},
		{KindFunction, strings.Repeat("a", 129), "a.go", false},
		{KindMethod, "Run", "a.go", true},
		{KindType, "Server", "a.go", true},
		{KindFile, "b.go", "a/b.go", true},
		{KindFile, "a/b.go", "a/b.go", false},
		{KindConfigKey, "db.host-name_1", "c.yaml", true},
		{KindConfigKey, "a b", "c.yaml", false},
		{KindConfigKey, strings.Repeat("a", 129), "c.yaml", false},
		{"class", "X", "a.go", false},
	}
	for _, c := range cases {
		if got := ValidName(c.kind, c.name, c.file); got != c.want {
			t.Errorf("ValidName(%q, %q, %q) = %v, want %v", c.kind, c.name, c.file, got, c.want)
		}
	}
}

func TestValidPath(t *testing.T) {
	cases := []struct {
		p    string
		want bool
	}{
		{"a.go", true},
		{"a/b/c.go", true},
		{".github/workflows/x.yml", true},
		{"日本/a.go", true},
		{"", false},
		{".", false},
		{"..", false},
		{"../a.go", false},
		{"a/../b.go", false},
		{"./a.go", false},
		{"a//b.go", false},
		{"a/", false},
		{"/a.go", false},
		{`a\b.go`, false},
		{"a\x00.go", false},
		{"a\x1f.go", false},
		{"a\x7f.go", false},
		{"a\xff.go", false},
		{strings.Repeat("a", 4096), true},
		{strings.Repeat("a", 4097), false},
	}
	for _, c := range cases {
		if got := ValidPath(c.p); got != c.want {
			t.Errorf("ValidPath(%q) = %v, want %v", c.p, got, c.want)
		}
	}
}
