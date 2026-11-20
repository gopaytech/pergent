package main

import (
	"bytes"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/gopaytech/pergent/graph"
)

// graphifyOut copies a graphify fixture to <tmp>/graphify-out/graph.json
// and returns the folder.
func graphifyOut(t *testing.T, fixture string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "internal", "graphify", "testdata", fixture))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "graphify-out")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "graph.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestGraphMain_Convert(t *testing.T) {
	in := graphifyOut(t, "graph.json")
	out := filepath.Join(t.TempDir(), "g.json")
	var stderr bytes.Buffer
	if code := graphMain([]string{"convert", "--provider", "graphify", "--in", in, "--out", out}, &stderr); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr.String())
	}
	f, err := graph.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if f.Provider != "graphify" || len(f.Nodes) == 0 {
		t.Errorf("file = %+v", f)
	}
	fi, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
	if _, err := os.Stat(out + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("the .tmp file is left: %v", err)
	}
	if !strings.Contains(stderr.String(), "graph: wrote "+out+": ") || !strings.Contains(stderr.String(), " nodes, ") {
		t.Errorf("stderr = %q", stderr.String())
	}

	// --commit wins, and the default provider is graphify.
	const other = "fedcba9876543210fedcba9876543210fedcba98"
	stderr.Reset()
	if code := graphMain([]string{"convert", "--in", in, "--out", out, "--commit", other}, &stderr); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr.String())
	}
	if f, err := graph.ReadFile(out); err != nil || f.Commit != other {
		t.Errorf("commit = %v, %v; want %s", f, err, other)
	}
}

func TestGraphMain_Usage(t *testing.T) {
	in := graphifyOut(t, "graph.json")
	out := filepath.Join(t.TempDir(), "g.json")
	cases := map[string][]string{
		"no command":   {},
		"bad command":  {"nope"},
		"no --in":      {"convert", "--out", out},
		"no --out":     {"convert", "--in", in},
		"bad provider": {"convert", "--provider", "scip", "--in", in, "--out", out},
		"extra arg":    {"convert", "--in", in, "--out", out, "extra"},
		"unknown flag": {"convert", "--in", in, "--out", out, "--nope"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			var stderr bytes.Buffer
			if code := graphMain(args, &stderr); code != 2 {
				t.Fatalf("exit %d, want 2; stderr:\n%s", code, stderr.String())
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Errorf("--out was written")
			}
		})
	}
	var stderr bytes.Buffer
	graphMain([]string{"convert", "--provider", "scip", "--in", in, "--out", out}, &stderr)
	if !strings.Contains(stderr.String(), "graphify") {
		t.Errorf("an unknown provider does not list the known ones: %q", stderr.String())
	}
}

func TestGraphMain_ConvertFails(t *testing.T) {
	in := graphifyOut(t, "old.json")
	out := filepath.Join(t.TempDir(), "g.json")
	var stderr bytes.Buffer
	if code := graphMain([]string{"convert", "--in", in, "--out", out}, &stderr); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "schema_version") {
		t.Errorf("stderr = %q", stderr.String())
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("--out exists after a failed convert")
	}
	if _, err := os.Stat(out + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("--out.tmp exists after a failed convert")
	}

	// An --out that cannot be written is exit 1 too.
	in = graphifyOut(t, "graph.json")
	stderr.Reset()
	if code := graphMain([]string{"convert", "--in", in, "--out", filepath.Join(t.TempDir(), "no", "g.json")}, &stderr); code != 1 {
		t.Fatalf("exit %d, want 1; stderr:\n%s", code, stderr.String())
	}
}

// TestMain_GraphSubcommand runs main() in a child process: "pergent graph
// convert" works with no opencode on PATH and no LLM or platform setting.
func TestMain_GraphSubcommand(t *testing.T) {
	if args := os.Getenv("PERGENT_TEST_GRAPH_ARGS"); args != "" {
		os.Args = append([]string{"pergent", "graph"}, strings.Split(args, "\x1f")...)
		main()
		return
	}
	in := graphifyOut(t, "graph.json")
	out := filepath.Join(t.TempDir(), "g.json")
	cmd := exec.Command(os.Args[0], "-test.run=^TestMain_GraphSubcommand$")
	cmd.Env = []string{
		"PATH=",
		"HOME=" + t.TempDir(),
		"PERGENT_TEST_GRAPH_ARGS=" + strings.Join([]string{"convert", "--provider", "graphify", "--in", in, "--out", out}, "\x1f"),
	}
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	if err := cmd.Run(); err != nil {
		t.Fatalf("main() failed: %v\nstdout:\n%s\nstderr:\n%s", err, so.String(), se.String())
	}
	if _, err := graph.ReadFile(out); err != nil {
		t.Fatalf("no graph written: %v\nstderr:\n%s", err, se.String())
	}
}

func writeGraphFile(t *testing.T, f *graph.File) string {
	t.Helper()
	b, err := f.Encode()
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "graph.json")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// callGraph is a v1 graph where B in b.go (line 3) calls A in a.go (line 3).
func callGraph() *graph.File {
	return &graph.File{
		Format: graph.Format, Provider: "graphify", ProviderVersion: "0.9.112",
		Commit: "0123456789abcdef0123456789abcdef01234567", BuiltAt: "2026-11-19T10:00:00Z",
		Nodes: []graph.Node{
			{ID: "n1", Kind: graph.KindFunction, Name: "A", File: "a.go", Line: 3},
			{ID: "n2", Kind: graph.KindFunction, Name: "B", File: "b.go", Line: 3},
		},
		Edges: []graph.Edge{{From: "n2", To: "n1", Kind: graph.EdgeCalls}},
	}
}

func TestReadGraph(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	if g := readGraph("", logger); g != nil || buf.Len() != 0 {
		t.Errorf(`readGraph("") = %v, log %q; want nil and no log`, g, buf.String())
	}

	dir := t.TempDir()
	broken := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(broken, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	badField := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(badField, []byte(strings.Replace(string(mustEncode(t, callGraph())), `"name":"A"`, `"name":"a b"`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(dir, "big.json")
	if err := os.WriteFile(big, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(big, graph.MaxBytes+1); err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]string{"missing": filepath.Join(dir, "missing.json"), "broken": broken, "bad field": badField, "too big": big} {
		buf.Reset()
		if g := readGraph(p, logger); g != nil {
			t.Errorf("%s: readGraph gave a graph", name)
		}
		if !strings.HasPrefix(buf.String(), "graph: skipped: ") || strings.Count(buf.String(), "\n") != 1 {
			t.Errorf("%s: log = %q, want one graph: skipped line", name, buf.String())
		}
	}

	buf.Reset()
	g := readGraph(writeGraphFile(t, callGraph()), logger)
	if g == nil || len(g.Nodes) != 2 {
		t.Fatalf("readGraph = %v", g)
	}
	if want := "graph: commit 0123456789ab, 2 nodes, 1 edges\n"; buf.String() != want {
		t.Errorf("log = %q, want %q", buf.String(), want)
	}
}

func mustEncode(t *testing.T, f *graph.File) []byte {
	t.Helper()
	b, err := f.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestMain_GraphFlagInLocalMode runs main() in a child process in local
// mode. With no flag, a missing file and a broken file, everything opencode
// gets and the comment are the same; with a valid file the one run gets one
// more file, graph-context.md, last, and one more sentence.
func TestMain_GraphFlagInLocalMode(t *testing.T) {
	if root := os.Getenv("PERGENT_TEST_GRAPH_REPO"); root != "" {
		os.Args = append([]string{"pergent", "--local", "--skill", "code-review", "--repo-path", root},
			strings.Fields(os.Getenv("PERGENT_TEST_GRAPH_FLAGS"))...)
		main()
		return
	}

	repo := t.TempDir()
	gitIn(t, repo, "init", "-q")
	writeFile(t, repo, "a.go", "package a\n\nfunc A() {}\n")
	writeFile(t, repo, "b.go", "package a\n\nfunc B() { A() }\n")
	base := commitAll(t, repo, "base")
	gitIn(t, repo, "update-ref", "refs/remotes/origin/main", base)
	writeFile(t, repo, "a.go", "package a\n\nfunc A() { _ = 1 }\n")
	commitAll(t, repo, "head")

	ndjson := `{"type":"step_start","part":{"type":"step-start"}}
{"type":"text","part":{"type":"text","text":"` + "```json\\n{\\\"findings\\\": []}\\n```" + `"}}
{"type":"step_finish","part":{"type":"step-finish","reason":"stop","tokens":{"total":24,"input":20,"output":4}}}
`
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	// pergent clears the environment for opencode, so the capture folder is
	// written into the stub, not passed as a variable.
	capDir := t.TempDir()
	// The stub keeps its arguments, a copy of the config and the last file.
	script := "#!/bin/sh\n" +
		"for a in \"$@\"; do printf '%s\\n' \"$a\"; done > \"" + capDir + "/args\"\n" +
		"cp \"$OPENCODE_CONFIG\" \"" + capDir + "/config\"\n" +
		"last=''; prev=''; for a in \"$@\"; do if [ \"$prev\" = --file ]; then last=$a; fi; prev=$a; done\n" +
		"cp \"$last\" \"" + capDir + "/last\"\n" +
		"cat <<'EOF'\n" + ndjson + "EOF\n"
	if err := os.WriteFile(filepath.Join(bin, "opencode"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	tempRe := regexp.MustCompile(`(?:/private)?` + regexp.QuoteMeta(strings.TrimSuffix(os.TempDir(), "/")) + `/[^"\s]*`)
	type capture struct {
		stdout, stderr, config, last string
		args                         []string
	}
	run := func(t *testing.T, flags string, env ...string) capture {
		t.Helper()
		for _, f := range []string{"args", "config", "last"} {
			os.Remove(filepath.Join(capDir, f))
		}
		cmd := exec.Command(os.Args[0], "-test.run=^TestMain_GraphFlagInLocalMode$")
		cmd.Env = append(os.Environ(),
			"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
			"PERGENT_TEST_GRAPH_REPO="+repo,
			"PERGENT_TEST_GRAPH_FLAGS="+flags,
			"OPENCODE_PROVIDER=anthropic",
			"OPENCODE_MODEL=claude-sonnet-4",
		)
		cmd.Env = append(cmd.Env, env...)
		var so, se bytes.Buffer
		cmd.Stdout, cmd.Stderr = &so, &se
		if err := cmd.Run(); err != nil {
			t.Fatalf("main() failed: %v\nstdout:\n%s\nstderr:\n%s", err, so.String(), se.String())
		}
		c := capture{stdout: so.String(), stderr: se.String()}
		args, err := os.ReadFile(filepath.Join(capDir, "args"))
		if err != nil {
			t.Fatalf("opencode did not run: %v\nstderr:\n%s", err, se.String())
		}
		for _, a := range strings.Split(strings.TrimSuffix(string(args), "\n"), "\n") {
			if filepath.IsAbs(a) {
				a = filepath.Base(a)
			}
			c.args = append(c.args, a)
		}
		cfg, _ := os.ReadFile(filepath.Join(capDir, "config"))
		c.config = tempRe.ReplaceAllString(string(cfg), "<tmp>")
		last, _ := os.ReadFile(filepath.Join(capDir, "last"))
		c.last = string(last)
		return c
	}

	plain := run(t, "")
	if !strings.Contains(plain.stdout, "No blocking issues") {
		t.Fatalf("the plain run printed %q, want a review comment", plain.stdout)
	}
	dir := t.TempDir()
	broken := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(broken, []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]string{"missing": filepath.Join(dir, "missing.json"), "broken": broken} {
		t.Run(name, func(t *testing.T) {
			c := run(t, "--graph "+p)
			if c.stdout != plain.stdout || c.config != plain.config || !slices.Equal(c.args, plain.args) || c.last != plain.last {
				t.Errorf("the run differs from one with no flag:\nargs %q\nwant %q", c.args, plain.args)
			}
			if !strings.Contains(c.stderr, "graph: skipped: ") {
				t.Errorf("stderr has no graph: skipped line:\n%s", c.stderr)
			}
		})
	}
	// PERGENT_GRAPH names the file when --graph is not passed; a bad one is
	// skipped the same way.
	t.Run("broken from PERGENT_GRAPH", func(t *testing.T) {
		c := run(t, "", "PERGENT_GRAPH="+broken)
		if c.stdout != plain.stdout || !slices.Equal(c.args, plain.args) || !strings.Contains(c.stderr, "graph: skipped: ") {
			t.Errorf("the run differs from one with no graph, or logs no skip:\nargs %q\nstderr:\n%s", c.args, c.stderr)
		}
	})
	t.Run("valid from PERGENT_GRAPH", func(t *testing.T) {
		c := run(t, "", "PERGENT_GRAPH="+writeGraphFile(t, callGraph()))
		if !strings.Contains(c.last, "- B  b.go:3") || !strings.Contains(c.stderr, "graph: commit 0123456789ab, 2 nodes, 1 edges") {
			t.Errorf("graph-context.md = %q, want the caller of A; stderr:\n%s", c.last, c.stderr)
		}
	})
	t.Run("valid", func(t *testing.T) {
		c := run(t, "--graph "+writeGraphFile(t, callGraph()))
		if c.stdout != plain.stdout {
			t.Errorf("stdout differs:\n%s\nwant\n%s", c.stdout, plain.stdout)
		}
		if c.config != plain.config {
			t.Errorf("the config differs")
		}
		// plain: ... --file X --file Y -- message; valid: one more --file before "--".
		n := len(plain.args)
		if len(c.args) != n+2 || !slices.Equal(c.args[:n-2], plain.args[:n-2]) ||
			c.args[n-2] != "--file" || c.args[n-1] != "graph-context.md" || c.args[n] != "--" {
			t.Fatalf("args = %q, want the plain args with --file graph-context.md last", c.args)
		}
		if msg := c.args[n+1]; !strings.HasPrefix(msg, plain.args[n-1]+" ") || !strings.HasSuffix(msg, "read the code before you rely on it.") {
			t.Errorf("message = %q, want the plain message and the graph sentence", msg)
		}
		if !strings.Contains(c.last, "### A (function, line 3)") || !strings.Contains(c.last, "- B  b.go:3") {
			t.Errorf("graph-context.md = %q, want the caller of A", c.last)
		}
		if !strings.Contains(c.stderr, "graph: commit 0123456789ab, 2 nodes, 1 edges") {
			t.Errorf("stderr has no graph: commit line:\n%s", c.stderr)
		}
	})
}
