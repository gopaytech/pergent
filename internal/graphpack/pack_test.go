package graphpack

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/gopaytech/pergent/graph"
	"github.com/gopaytech/pergent/internal/diff"
)

var update = flag.Bool("update", false, "rewrite testdata/pack.golden.md")

const commit = "0123456789abcdef0123456789abcdef01234567"

// builder makes a graph.File by hand.
type builder struct {
	f   graph.File
	ids map[string]string
}

func newBuilder() *builder {
	return &builder{
		f:   graph.File{Format: graph.Format, Provider: "graphify", ProviderVersion: "0.9.112", Commit: commit, BuiltAt: "2026-11-19T10:00:00Z"},
		ids: map[string]string{},
	}
}

// node adds a node; key is how the test names it.
func (b *builder) node(key, kind, name, file string, line, end int) *builder {
	id := "n" + strconv.Itoa(len(b.f.Nodes)+1)
	b.ids[key] = id
	b.f.Nodes = append(b.f.Nodes, graph.Node{ID: id, Kind: kind, Name: name, File: file, Line: line, EndLine: end})
	return b
}

func (b *builder) file(p string) *builder {
	return b.node(p, graph.KindFile, filepath.Base(p), p, 1, 0)
}

func (b *builder) edge(from, kind, to string) *builder {
	f, ok1 := b.ids[from]
	t, ok2 := b.ids[to]
	if !ok1 || !ok2 {
		panic("unknown node " + from + " or " + to)
	}
	b.f.Edges = append(b.f.Edges, graph.Edge{From: f, To: t, Kind: kind})
	return b
}

func (b *builder) index(t *testing.T) *Index {
	t.Helper()
	if err := b.f.Validate(); err != nil {
		t.Fatalf("test graph: %v", err)
	}
	return NewIndex(&b.f)
}

// goldenGraph: server.go holds Server (type), Dispatch and Server.Run;
// client.go imports server.go and Call calls Dispatch, and so does
// BenchmarkDispatch in bench_test.go; Dispatch calls helper in util.go and
// 5 functions in many.go.
func goldenGraph() *builder {
	b := newBuilder().
		file("server.go").file("client.go").file("util.go").file("many.go").file("bench_test.go").
		node("Server", graph.KindType, "Server", "server.go", 3, 0).
		node("Dispatch", graph.KindFunction, "Dispatch", "server.go", 10, 0).
		node("Run", graph.KindMethod, "Run", "server.go", 20, 0).
		node("Call", graph.KindFunction, "Call", "client.go", 5, 0).
		node("helper", graph.KindFunction, "helper", "util.go", 3, 0).
		node("BenchmarkDispatch", graph.KindFunction, "BenchmarkDispatch", "bench_test.go", 8, 0).
		edge("server.go", graph.EdgeContains, "Server").
		edge("server.go", graph.EdgeContains, "Dispatch").
		edge("Server", graph.EdgeContains, "Run").
		edge("Run", graph.EdgeCalls, "Dispatch").
		edge("Call", graph.EdgeCalls, "Dispatch").
		edge("BenchmarkDispatch", graph.EdgeCalls, "Dispatch").
		edge("client.go", graph.EdgeImports, "server.go").
		edge("Dispatch", graph.EdgeCalls, "helper")
	for i := 1; i <= 5; i++ {
		k := fmt.Sprintf("m%d", i)
		b.node(k, graph.KindFunction, k, "many.go", i*10, 0).edge("Dispatch", graph.EdgeCalls, k)
	}
	return b
}

// patchAt is a patch that removes old line n.
func patchAt(n int) string {
	return fmt.Sprintf("@@ -%d,1 +%d,0 @@\n-x\n", n, n)
}

func TestBuild_Golden(t *testing.T) {
	b := goldenGraph()
	ix := b.index(t)
	// A budget that fits callers, importers and the first callee only.
	old := maxBytes
	maxBytes = len(headerText(commit)) + len(cutReserve) +
		len("\n## server.go\n") + len("\n### Dispatch (function, line 10)\n") +
		len("\nCalled by:\n") + len("- Call  client.go:5\n") + len("- Server.Run  server.go:20\n") +
		len("- BenchmarkDispatch  bench_test.go:8\n") +
		len("\nImported by:\n") + len("- client.go\n") +
		len("\nCalls:\n") + len("- m1  many.go:10\n")
	defer func() { maxBytes = old }()

	p := ix.Build([]Change{{Path: "server.go", Patch: patchAt(12)}}, false)
	golden := filepath.Join("testdata", "pack.golden.md")
	if *update {
		if err := os.WriteFile(golden, []byte(p.Text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if p.Text != string(want) {
		t.Errorf("pack differs from %s:\n%s", golden, p.Text)
	}
	if p.Symbols != 1 || p.Entries != 5 || p.Cut != 5 {
		t.Errorf("Pack = %d symbols, %d entries, %d cut; want 1, 5, 5", p.Symbols, p.Entries, p.Cut)
	}
}

func TestBuild_Full(t *testing.T) {
	ix := goldenGraph().index(t)
	p := ix.Build([]Change{{Path: "server.go", Patch: patchAt(12)}}, false)
	for _, want := range []string{"- Call  client.go:5\n", "- Server.Run  server.go:20\n", "Imported by:\n- client.go\n", "- helper  util.go:3\n", "- m5  many.go:50\n"} {
		if !strings.Contains(p.Text, want) {
			t.Errorf("no %q in:\n%s", want, p.Text)
		}
	}
	if strings.Contains(p.Text, "left out") || p.Cut != 0 {
		t.Errorf("a cut line with nothing cut:\n%s", p.Text)
	}
	if !strings.Contains(p.Text, "at commit "+commit+".") {
		t.Errorf("the header does not name the commit")
	}
}

func TestBuild_SymbolLookup(t *testing.T) {
	b := newBuilder().file("a.go").file("c.go").
		node("Outer", graph.KindFunction, "Outer", "a.go", 10, 40).
		node("Inner", graph.KindFunction, "Inner", "a.go", 15, 20).
		node("After", graph.KindFunction, "After", "a.go", 50, 0).
		node("T", graph.KindType, "T", "a.go", 60, 0).
		node("M", graph.KindMethod, "M", "a.go", 60, 0)
	for _, k := range []string{"Outer", "Inner", "After", "T", "M"} {
		b.node("c"+k, graph.KindFunction, "c"+k, "c.go", len(b.f.Nodes), 0).edge("c"+k, graph.EdgeCalls, k)
	}
	b.edge("c.go", graph.EdgeImports, "a.go")
	ix := b.index(t)
	cases := []struct {
		line int
		want string // the symbol heading, or "" for none
	}{
		{17, "### Inner (function, line 15)"},
		{30, "### Outer (function, line 10)"},
		{45, ""}, // past Outer's end, before After: no symbol has it
		{55, "### After (function, line 50)"},
		{61, "### M (method, line 60)"},
		{5, ""},
	}
	for _, c := range cases {
		p := ix.Build([]Change{{Path: "a.go", Patch: patchAt(c.line)}}, false)
		got := regexp.MustCompile(`(?m)^### .*$`).FindAllString(p.Text, -1)
		if c.want == "" {
			if len(got) != 0 {
				t.Errorf("line %d: symbols %v, want none", c.line, got)
			}
		} else if len(got) != 1 || got[0] != c.want {
			t.Errorf("line %d: symbols %v, want %q", c.line, got, c.want)
		}
		if !strings.Contains(p.Text, "Imported by:\n- c.go\n") {
			t.Errorf("line %d: the file's importers are missing:\n%s", c.line, p.Text)
		}
	}
}

func TestBuild_RenameUsesOldPath(t *testing.T) {
	ix := goldenGraph().index(t)
	p := ix.Build([]Change{{Path: "new.go", OldPath: "server.go", Patch: patchAt(12)}}, false)
	if !strings.Contains(p.Text, "### Dispatch") {
		t.Errorf("a rename did not use OldPath:\n%s", p.Text)
	}
	p = ix.Build([]Change{{Path: "new.go", Patch: patchAt(12)}}, false)
	if p.Text != "" {
		t.Errorf("a file the graph does not know gave a pack:\n%s", p.Text)
	}
}

func TestBuild_DeletedFile(t *testing.T) {
	ix := goldenGraph().index(t)
	var lines []string
	for i := 0; i < 25; i++ {
		lines = append(lines, "-x")
	}
	p := ix.Build([]Change{{Path: "server.go", Patch: "@@ -1,25 +0,0 @@\n" + strings.Join(lines, "\n") + "\n"}}, false)
	for _, want := range []string{"### Dispatch (function, line 10)", "### Server.Run (method, line 20)"} {
		if !strings.Contains(p.Text, want) {
			t.Errorf("no %q in:\n%s", want, p.Text)
		}
	}
	// The type Server has no callers or callees: it is not listed.
	if strings.Contains(p.Text, "### Server ") {
		t.Errorf("a symbol with nothing to say is listed:\n%s", p.Text)
	}
}

func TestBuild_Caps(t *testing.T) {
	b := newBuilder().file("a.go").file("b.go").
		node("F", graph.KindFunction, "F", "a.go", 1, 0)
	for i := 1; i <= 15; i++ {
		b.node(fmt.Sprintf("in%02d", i), graph.KindFunction, fmt.Sprintf("in%02d", i), "b.go", i, 0).edge(fmt.Sprintf("in%02d", i), graph.EdgeCalls, "F")
		b.node(fmt.Sprintf("out%02d", i), graph.KindFunction, fmt.Sprintf("out%02d", i), "b.go", 100+i, 0).edge("F", graph.EdgeCalls, fmt.Sprintf("out%02d", i))
	}
	p := b.index(t).Build([]Change{{Path: "a.go", Patch: patchAt(1)}}, false)
	if n := strings.Count(p.Text, "  b.go:"); n != 20 {
		t.Errorf("%d entries, want 20", n)
	}
	if !strings.Contains(p.Text, "in10  b.go:10") || strings.Contains(p.Text, "in11  b.go:11") {
		t.Errorf("the caller cap is not the first 10 in order:\n%s", p.Text)
	}
	if !strings.HasSuffix(p.Text, "\n10 more entries were left out to keep this file short.\n") || p.Cut != 10 {
		t.Errorf("Cut = %d, text ends:\n%s", p.Cut, p.Text[len(p.Text)-80:])
	}
	if !strings.Contains(p.Text, "- in10  b.go:10\n\n5 more callers were left out.\n\nCalls:\n") {
		t.Errorf("no caller note after the 10th caller:\n%s", p.Text)
	}
	if strings.Contains(p.Text, "more callees") || strings.Count(p.Text, "callers were left out") != 1 {
		t.Errorf("a note for the callees, or more than one note:\n%s", p.Text)
	}
}

// testCallers gives F in a.go real callers in b.go and test callers in
// a_test.go, which sorts before b.go.
func testCallers(t *testing.T, real, tests int) *Index {
	t.Helper()
	b := newBuilder().file("a.go").file("a_test.go").file("b.go").
		node("F", graph.KindFunction, "F", "a.go", 1, 0)
	for i := 1; i <= tests; i++ {
		k := fmt.Sprintf("TestF%02d", i)
		b.node(k, graph.KindFunction, k, "a_test.go", i, 0).edge(k, graph.EdgeCalls, "F")
	}
	for i := 1; i <= real; i++ {
		k := fmt.Sprintf("use%02d", i)
		b.node(k, graph.KindFunction, k, "b.go", i, 0).edge(k, graph.EdgeCalls, "F")
	}
	return b.index(t)
}

func TestBuild_TestCallersLast(t *testing.T) {
	// 12 test callers sort first by file: before, they took all 10 slots
	// and no real caller was listed.
	p := testCallers(t, 3, 12).Build([]Change{{Path: "a.go", Patch: patchAt(1)}}, false)
	var want strings.Builder
	want.WriteString("\nCalled by:\n- use01  b.go:1\n- use02  b.go:2\n- use03  b.go:3\n")
	for i := 1; i <= 7; i++ {
		fmt.Fprintf(&want, "- TestF%02d  a_test.go:%d\n", i, i)
	}
	want.WriteString("\n5 more callers were left out (5 in test files).\n")
	if !strings.Contains(p.Text, want.String()) {
		t.Errorf("real callers are not first, or the note is wrong:\n%s\nwant a part:\n%s", p.Text, want.String())
	}
	if p.Entries != 10 || p.Cut != 5 {
		t.Errorf("Entries %d, Cut %d; want 10, 5", p.Entries, p.Cut)
	}
}

func TestBuild_CallerNote(t *testing.T) {
	cases := []struct {
		real, tests int
		note        string // "" for none
	}{
		{12, 2, "4 more callers were left out (2 in test files)."},
		{12, 0, "2 more callers were left out."},
		{0, 11, "1 more caller was left out (1 in test files)."},
		{1, 10, "1 more caller was left out (1 in test files)."},
		{4, 6, ""},
		{0, 3, ""},
	}
	for _, c := range cases {
		p := testCallers(t, c.real, c.tests).Build([]Change{{Path: "a.go", Patch: patchAt(1)}}, false)
		got := regexp.MustCompile(`(?m)^\d+ more caller.*$`).FindAllString(p.Text, -1)
		if c.note == "" && len(got) != 0 || c.note != "" && (len(got) != 1 || got[0] != c.note) {
			t.Errorf("%d real, %d tests: notes %q, want %q:\n%s", c.real, c.tests, got, c.note, p.Text)
		}
		if n := strings.Count(p.Text, "\n- "); n != min(c.real+c.tests, MaxPerSymbol) {
			t.Errorf("%d real, %d tests: %d callers listed", c.real, c.tests, n)
		}
	}
}

func TestBuild_CallerNoteFitsBudget(t *testing.T) {
	b := newBuilder().file("a.go").file("a_test.go").file("c.go")
	var patch strings.Builder
	for s := 1; s <= 5; s++ {
		k := fmt.Sprintf("S%d", s)
		b.node(k, graph.KindFunction, k, "a.go", s*10, 0)
		for c := 1; c <= 12; c++ {
			ck, file := fmt.Sprintf("%s_c%02d", k, c), "c.go"
			if c > 8 {
				file = "a_test.go"
			}
			b.node(ck, graph.KindFunction, ck, file, s*100+c, 0).edge(ck, graph.EdgeCalls, k)
		}
		fmt.Fprintf(&patch, "@@ -%d,1 +%d,0 @@\n-x\n", s*10, s*10)
	}
	ix := b.index(t)
	changes := []Change{{Path: "a.go", Patch: patch.String()}}
	old := maxBytes
	defer func() { maxBytes = old }()
	full := len(ix.Build(changes, false).Text)
	notes := 0
	for maxBytes = len(headerText(commit)) + len(cutReserve); maxBytes <= full+len(cutReserve); maxBytes++ {
		p := ix.Build(changes, false)
		if len(p.Text) > maxBytes {
			t.Fatalf("maxBytes %d: the hint is %d bytes:\n%s", maxBytes, len(p.Text), p.Text)
		}
		notes += strings.Count(p.Text, "callers were left out")
	}
	if notes == 0 {
		t.Fatal("no budget kept a caller note")
	}
}

func TestBuild_Budget(t *testing.T) {
	b := newBuilder().file("a.go").file("c.go")
	var patch strings.Builder
	for s := 1; s <= 300; s++ {
		k := fmt.Sprintf("S%03d", s)
		b.node(k, graph.KindFunction, k, "a.go", s*10, 0)
		for c := 1; c <= 10; c++ {
			ck := fmt.Sprintf("%s_caller%02d", k, c)
			b.node(ck, graph.KindFunction, ck, "c.go", s*100+c, 0).edge(ck, graph.EdgeCalls, k)
		}
		b.node(k+"_callee", graph.KindFunction, k+"_callee", "c.go", 100000+s, 0).edge(k, graph.EdgeCalls, k+"_callee")
		fmt.Fprintf(&patch, "@@ -%d,1 +%d,0 @@\n-x\n", s*10, s*10)
	}
	p := b.index(t).Build([]Change{{Path: "a.go", Patch: patch.String()}}, false)
	if got := diff.EstimateTokens(p.Text); got > MaxTokens {
		t.Errorf("EstimateTokens = %d, over %d", got, MaxTokens)
	}
	if strings.Contains(p.Text, "_callee") || strings.Contains(p.Text, "Calls:") {
		t.Errorf("a callee was kept before every caller fit")
	}
	if total := 300*10 + 300; p.Entries+p.Cut != total {
		t.Errorf("Entries %d + Cut %d != %d", p.Entries, p.Cut, total)
	}
	if n := strings.Count(p.Text, "\n- "); n != p.Entries {
		t.Errorf("%d entry lines, Entries = %d", n, p.Entries)
	}
	if !strings.HasSuffix(p.Text, fmt.Sprintf("\n%d more entries were left out to keep this file short.\n", p.Cut)) {
		t.Errorf("the cut line does not say %d", p.Cut)
	}
}

func TestBuild_Deterministic(t *testing.T) {
	b := goldenGraph()
	changes := []Change{{Path: "server.go", Patch: patchAt(12)}, {Path: "client.go", Patch: patchAt(5)}}
	want := b.index(t).Build(changes, false).Text
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 5; i++ {
		r.Shuffle(len(b.f.Nodes), func(x, y int) { b.f.Nodes[x], b.f.Nodes[y] = b.f.Nodes[y], b.f.Nodes[x] })
		r.Shuffle(len(b.f.Edges), func(x, y int) { b.f.Edges[x], b.f.Edges[y] = b.f.Edges[y], b.f.Edges[x] })
		r.Shuffle(len(changes), func(x, y int) { changes[x], changes[y] = changes[y], changes[x] })
		if got := NewIndex(&b.f).Build(changes, false).Text; got != want {
			t.Fatalf("shuffle %d changed the pack:\n%s\nwant:\n%s", i, got, want)
		}
	}
	if !strings.Contains(want, "## client.go") || strings.Index(want, "## client.go") > strings.Index(want, "## server.go") {
		t.Errorf("files are not in path order:\n%s", want)
	}
}

func TestBuild_Denied(t *testing.T) {
	b := newBuilder().file("a.go").file("x.env").file("X.ENV").file("k.pem").
		node("F", graph.KindFunction, "F", "a.go", 1, 0).
		node("e1", graph.KindFunction, "e1", "x.env", 2, 0).
		node("e2", graph.KindFunction, "e2", "X.ENV", 2, 0).
		node("p", graph.KindFunction, "p", "k.pem", 2, 0).
		edge("e1", graph.EdgeCalls, "F").edge("e2", graph.EdgeCalls, "F").edge("p", graph.EdgeCalls, "F").
		edge("x.env", graph.EdgeImports, "a.go").edge("k.pem", graph.EdgeImports, "a.go")
	ix := b.index(t)
	soft := ix.Build([]Change{{Path: "a.go", Patch: patchAt(1)}}, false)
	hard := ix.Build([]Change{{Path: "a.go", Patch: patchAt(1)}}, true)
	for _, p := range []Pack{soft, hard} {
		if strings.Contains(p.Text, "env") || strings.Contains(p.Text, "ENV") {
			t.Errorf("a .env entry is listed:\n%s", p.Text)
		}
	}
	if !strings.Contains(soft.Text, "- p  k.pem:2") || !strings.Contains(soft.Text, "- k.pem\n") {
		t.Errorf("k.pem is missing without hardened:\n%s", soft.Text)
	}
	if hard.Text != "" {
		t.Errorf("hardened pack lists k.pem:\n%s", hard.Text)
	}
	// A changed file the rules deny gets no section.
	if p := ix.Build([]Change{{Path: "k.pem", Patch: patchAt(2)}}, true); p.Text != "" {
		t.Errorf("a denied changed file has a section:\n%s", p.Text)
	}
}

func TestBuild_NothingToSay(t *testing.T) {
	b := newBuilder().file("a.go").node("F", graph.KindFunction, "F", "a.go", 1, 0)
	p := b.index(t).Build([]Change{{Path: "a.go", Patch: patchAt(1)}}, false)
	if p.Text != "" || p.Entries != 0 {
		t.Errorf("Pack = %+v, want empty", p)
	}
	if p := NewIndex(&graph.File{}).Build(nil, false); p.Text != "" {
		t.Errorf("an empty graph gave %q", p.Text)
	}
}
