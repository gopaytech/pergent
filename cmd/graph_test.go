package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
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
