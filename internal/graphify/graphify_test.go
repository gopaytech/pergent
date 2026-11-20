package graphify

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gopaytech/pergent/graph"
	"github.com/gopaytech/pergent/internal/config"
)

var fixedNow = func() time.Time { return time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC) }

// fixture returns testdata/graph.json decoded, plus one node in a file for
// each deny rule of the hardened read rules, as written and in upper case,
// each calling Dispatch. It returns the number of those nodes.
func fixture(t *testing.T) (map[string]any, int) {
	t.Helper()
	m := load(t, filepath.Join("testdata", "graph.json"))
	nodes := m["nodes"].([]any)
	links := m["links"].([]any)
	added := 0
	for k, v := range config.ReadRules(true) {
		if v != "deny" {
			continue
		}
		p := "deny/" + strings.ReplaceAll(k, "*", "x")
		for i, f := range []string{p, strings.ToUpper(p)} {
			id := fmt.Sprintf("deny_%s_%d", k, i)
			nodes = append(nodes, map[string]any{
				"id": id, "label": "Leak()", "file_type": "code",
				"source_file": f, "source_location": "L1",
			})
			links = append(links, map[string]any{
				"source": id, "target": "server_dispatch", "relation": "calls",
				"confidence": "EXTRACTED", "source_file": f, "source_location": "L2",
			})
			added++
		}
	}
	if added == 0 {
		t.Fatal("no deny rules found")
	}
	m["nodes"] = nodes
	m["links"] = links
	return m, added
}

func load(t *testing.T, p string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// writeDir writes m as graph.json in a new directory and returns it.
func writeDir(t *testing.T, m map[string]any) string {
	t.Helper()
	dir := t.TempDir()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "graph.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func convert(t *testing.T, m map[string]any, opts Options) (*graph.File, Stats) {
	t.Helper()
	if opts.Now == nil {
		opts.Now = fixedNow
	}
	f, st, err := Convert(writeDir(t, m), opts)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	return f, st
}

func nodeKeys(f *graph.File) []string {
	var out []string
	for _, n := range f.Nodes {
		out = append(out, fmt.Sprintf("%s %s %s %d", n.Kind, n.Name, n.File, n.Line))
	}
	sort.Strings(out)
	return out
}

func edgeKeys(f *graph.File) []string {
	byID := map[string]graph.Node{}
	for _, n := range f.Nodes {
		byID[n.ID] = n
	}
	var out []string
	for _, e := range f.Edges {
		out = append(out, byID[e.From].Name+" "+e.Kind+" "+byID[e.To].Name)
	}
	sort.Strings(out)
	return out
}

func equal(t *testing.T, what string, got, want []string) {
	t.Helper()
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s:\ngot:\n  %s\nwant:\n  %s", what, strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

func TestConvert_Kinds(t *testing.T) {
	m, _ := fixture(t)
	f, _ := convert(t, m, Options{})
	equal(t, "nodes", nodeKeys(f), []string{
		"file server.go server.go 1",
		"type Server server.go 3",
		"function Dispatch server.go 10",
		"method Run server.go 20",
		"file client.go client.go 1",
		"function Call client.go 5",
		"function Helper client.go 42",
		"file helper.go pkg/util/helper.go 1",
		"function do pkg/util/helper.go 3",
		"file config.yaml config.yaml 1",
		"file .ci.yml .ci.yml 1",
	})
	equal(t, "edges", edgeKeys(f), []string{
		"Run calls Dispatch",
		"Call calls Dispatch",
		"do calls Dispatch",
		"client.go imports server.go",
		"server.go contains Server",
		"server.go contains Dispatch",
		"Server contains Run",
		"client.go contains Call",
		"client.go contains Helper",
		"helper.go contains do",
	})
	if f.Format != graph.Format || f.Provider != "graphify" || f.ProviderVersion != "0.9.112" ||
		f.Commit != "0123456789abcdef0123456789abcdef01234567" || f.BuiltAt != "2026-11-19T10:00:00Z" {
		t.Errorf("header = %+v", f)
	}
}

func TestConvert_EdgeDirection(t *testing.T) {
	// _src/_tgt win over source/target: Call calls Dispatch, never the reverse.
	m, _ := fixture(t)
	f, _ := convert(t, m, Options{})
	for _, e := range edgeKeys(f) {
		if e == "Dispatch calls Call" {
			t.Fatal("the edge direction came from source/target, not _src/_tgt")
		}
	}
}

func TestConvert_NoFreeText(t *testing.T) {
	m, _ := fixture(t)
	f, _ := convert(t, m, Options{})
	out, err := f.Encode()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"MARKER", "docstring", "password", "Deployment", "has space", "community",
		"metadata", "label", "context", "SECRET_KEY_NAME", "go_pkg", "_callable", "server_dispatch", "http.Client"} {
		if strings.Contains(string(out), s) {
			t.Errorf("the output holds %q", s)
		}
	}
	checkStrings(t, out)
}

// checkStrings decodes a v1 file loosely and checks every string in it
// against the rule for its field.
func checkStrings(t *testing.T, out []byte) {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	header := map[string]bool{"format": true, "provider": true, "provider_version": true, "commit": true, "built_at": true}
	idRe := regexp.MustCompile(`^n[0-9]+$`)
	for k, v := range m {
		switch k {
		case "nodes":
			for _, x := range v.([]any) {
				n := x.(map[string]any)
				for nk, nv := range n {
					s, isString := nv.(string)
					switch nk {
					case "id":
						if !idRe.MatchString(s) {
							t.Errorf("node id %q", s)
						}
					case "kind":
						if !map[string]bool{"file": true, "function": true, "method": true, "type": true}[s] {
							t.Errorf("node kind %q", s)
						}
					case "name":
						if !graph.ValidName(n["kind"].(string), s, n["file"].(string)) {
							t.Errorf("node name %q", s)
						}
					case "file":
						if !graph.ValidPath(s) {
							t.Errorf("node file %q", s)
						}
					case "line", "end_line":
						if isString {
							t.Errorf("node %s is a string", nk)
						}
					default:
						t.Errorf("node field %q", nk)
					}
				}
			}
		case "edges":
			for _, x := range v.([]any) {
				for ek, ev := range x.(map[string]any) {
					s := ev.(string)
					switch ek {
					case "from", "to":
						if !idRe.MatchString(s) {
							t.Errorf("edge %s %q", ek, s)
						}
					case "kind":
						if !map[string]bool{"calls": true, "imports": true, "contains": true, "inherits": true}[s] {
							t.Errorf("edge kind %q", s)
						}
					default:
						t.Errorf("edge field %q", ek)
					}
				}
			}
		default:
			if !header[k] {
				t.Errorf("top-level field %q", k)
			}
		}
	}
}

func TestConvert_DropsDenied(t *testing.T) {
	m, added := fixture(t)
	f, st := convert(t, m, Options{})
	for _, n := range f.Nodes {
		if config.ReadDenied(n.File, true) || config.ReadDenied(strings.ToLower(n.File), true) {
			t.Errorf("denied file %q is in the output", n.File)
		}
		if strings.HasPrefix(strings.ToLower(n.File), "deny/") {
			t.Errorf("deny fixture file %q is in the output", n.File)
		}
	}
	if st.DeniedNodes != added {
		t.Errorf("DeniedNodes = %d, want %d", st.DeniedNodes, added)
	}
	if st.Nodes != len(f.Nodes) || st.Edges != len(f.Edges) {
		t.Errorf("Stats %+v do not match the output (%d nodes, %d edges)", st, len(f.Nodes), len(f.Edges))
	}
	links := len(m["links"].([]any))
	if st.Edges+st.DroppedEdges != links {
		t.Errorf("Stats edges %d + dropped %d != %d links", st.Edges, st.DroppedEdges, links)
	}
}

func TestConvert_VersionGate(t *testing.T) {
	cases := []struct {
		name   string
		change func(m map[string]any)
		want   string
	}{
		{"empty graph object", func(m map[string]any) { m["graph"] = map[string]any{} }, "schema_version is missing"},
		{"no graph key", func(m map[string]any) { delete(m, "graph") }, "schema_version is missing"},
		{"schema 2", func(m map[string]any) { m["graph"].(map[string]any)["schema_version"] = 2 }, "schema_version 2 is not supported"},
		{"schema as a string", func(m map[string]any) { m["graph"].(map[string]any)["schema_version"] = "1" }, "schema_version is not a number"},
		{"older graphify", func(m map[string]any) { m["graph"].(map[string]any)["graphify_version"] = "0.9.111" }, `graphify_version "0.9.111" is not supported`},
		{"no graphify version", func(m map[string]any) { delete(m["graph"].(map[string]any), "graphify_version") }, "graphify_version is missing"},
		{"raw no-cluster output", func(m map[string]any) {
			m["edges"] = m["links"]
			delete(m, "links")
			delete(m, "graph")
		}, "without --no-cluster"},
		{"not a graph", func(m map[string]any) {
			for k := range m {
				delete(m, k)
			}
		}, "not a graphify graph"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _ := fixture(t)
			c.change(m)
			_, _, err := Convert(writeDir(t, m), Options{Now: fixedNow})
			if err == nil || !strings.Contains(err.Error(), c.want) || !strings.HasPrefix(err.Error(), "graphify: graph.json: ") {
				t.Fatalf("Convert = %v, want an error with %q", err, c.want)
			}
		})
	}
	t.Run("old.json", func(t *testing.T) {
		dir := t.TempDir()
		b, err := os.ReadFile(filepath.Join("testdata", "old.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "graph.json"), b, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := Convert(dir, Options{}); err == nil || !strings.Contains(err.Error(), "schema_version is missing") {
			t.Fatalf("Convert = %v", err)
		}
	})
	t.Run("long version is cut", func(t *testing.T) {
		m, _ := fixture(t)
		m["graph"].(map[string]any)["graphify_version"] = strings.Repeat("9", 100)
		_, _, err := Convert(writeDir(t, m), Options{})
		if err == nil || strings.Contains(err.Error(), strings.Repeat("9", 33)) {
			t.Fatalf("Convert = %v, want the version cut to 32 bytes", err)
		}
	})
}

func TestConvert_Commit(t *testing.T) {
	const other = "fedcba9876543210fedcba9876543210fedcba98"
	m, _ := fixture(t)
	f, _ := convert(t, m, Options{Commit: other})
	if f.Commit != other {
		t.Errorf("Commit = %q, want the --commit value", f.Commit)
	}

	m, _ = fixture(t)
	delete(m, "built_at_commit")
	if _, _, err := Convert(writeDir(t, m), Options{}); err == nil || !strings.Contains(err.Error(), "no commit: pass --commit") {
		t.Errorf("no commit: Convert = %v", err)
	}
	f, _ = convert(t, m, Options{Commit: other})
	if f.Commit != other {
		t.Errorf("Commit = %q with no built_at_commit", f.Commit)
	}

	for _, bad := range []string{strings.ToUpper(other), "abc123", other + " "} {
		m, _ = fixture(t)
		if _, _, err := Convert(writeDir(t, m), Options{Commit: bad}); err == nil || !strings.Contains(err.Error(), "commit is not a full SHA") {
			t.Errorf("commit %q: Convert = %v", bad, err)
		}
	}
	m, _ = fixture(t)
	m["built_at_commit"] = "ABC"
	if _, _, err := Convert(writeDir(t, m), Options{}); err == nil || !strings.Contains(err.Error(), "commit is not a full SHA") {
		t.Errorf("bad built_at_commit: Convert = %v", err)
	}
}

func TestConvert_Deterministic(t *testing.T) {
	m, _ := fixture(t)
	f1, _ := convert(t, m, Options{})
	b1, err := f1.Encode()
	if err != nil {
		t.Fatal(err)
	}
	f2, _ := convert(t, m, Options{})
	b2, _ := f2.Encode()
	if string(b1) != string(b2) {
		t.Fatal("two converts gave different bytes")
	}
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 5; i++ {
		nodes := m["nodes"].([]any)
		links := m["links"].([]any)
		r.Shuffle(len(nodes), func(a, b int) { nodes[a], nodes[b] = nodes[b], nodes[a] })
		r.Shuffle(len(links), func(a, b int) { links[a], links[b] = links[b], links[a] })
		f3, _ := convert(t, m, Options{})
		b3, _ := f3.Encode()
		if string(b1) != string(b3) {
			t.Fatalf("shuffle %d gave different bytes", i)
		}
	}
}

func TestConvert_Limits(t *testing.T) {
	m, _ := fixture(t)
	dir := writeDir(t, m)
	old := maxInput
	maxInput = 100
	defer func() { maxInput = old }()
	if _, _, err := Convert(dir, Options{}); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("Convert = %v, want a size error", err)
	}
	if _, _, err := Convert(t.TempDir(), Options{}); err == nil {
		t.Fatal("Convert of a directory with no graph.json gave no error")
	}
}

// TestConvert_RealGraphify reads a cut of a real graphify 0.9.112 run
// (graphify extract --code-only, default output) over pergent's own code. It
// pins the shapes the hand-made fixture copies: networkx "links", file labels
// with a parent folder when two files share a base name, types found by a
// contains edge, methods by ".Name()", INFERRED calls, YAML keys.
func TestConvert_RealGraphify(t *testing.T) {
	m := load(t, filepath.Join("testdata", "real", "graph.json"))
	if _, ok := m["built_at_commit"]; ok {
		t.Fatal("the real fixture was expected to have no built_at_commit (graphify ran outside a git repo)")
	}
	if _, _, err := Convert(writeDir(t, m), Options{}); err == nil || !strings.Contains(err.Error(), "no commit: pass --commit") {
		t.Fatalf("Convert without --commit = %v", err)
	}
	f, st := convert(t, m, Options{Commit: "0123456789abcdef0123456789abcdef01234567"})
	nodes := strings.Join(nodeKeys(f), "\n")
	for _, want := range []string{
		"file usage.go internal/result/usage.go 1",
		"file usage.go internal/runner/usage.go 1",
		"file tail.go internal/runner/tail.go 1",
		"file .gitlab-ci.example.yml .gitlab-ci.example.yml 1",
		"type RedactedLineWriter internal/runner/tail.go 95",
		"method Flush internal/runner/tail.go 97",
		"function EncodeUsage internal/result/usage.go 114",
	} {
		if !strings.Contains(nodes, want) {
			t.Errorf("no node %q", want)
		}
	}
	for _, n := range f.Nodes {
		if n.File == ".gitlab-ci.example.yml" && n.Kind != graph.KindFile {
			t.Errorf("a YAML key was kept: %+v", n)
		}
	}
	edges := strings.Join(edgeKeys(f), "\n")
	for _, want := range []string{
		"EncodeUsage calls buildRuns",       // EXTRACTED
		"Flush calls writeLine",             // INFERRED, method to method
		"RedactedLineWriter contains Flush", // graphify "method"
		"usage.go imports redact.go",        // imports_from, file to file
	} {
		if !strings.Contains(edges, want) {
			t.Errorf("no edge %q", want)
		}
	}
	out, err := f.Encode()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"GITLAB_TOKEN", "OPENCODE_API_KEY", "go_pkg", "community", "parameter_type", "gopaytech"} {
		if strings.Contains(string(out), s) {
			t.Errorf("the output holds %q", s)
		}
	}
	checkStrings(t, out)
	if st.OtherNodes == 0 || st.BadNodes == 0 || st.DroppedEdges == 0 {
		t.Errorf("Stats = %+v, want some dropped", st)
	}
}
