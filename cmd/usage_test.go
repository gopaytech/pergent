package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gopaytech/pergent/internal/result"
	"github.com/gopaytech/pergent/internal/review"
	"github.com/gopaytech/pergent/internal/runner"
)

func TestWriteUsageFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "u.json")
	results := []review.SkillResult{{
		Skill: "code-review",
		Runs: []review.RunUsage{{
			Label: "1/1",
			Steps: []runner.StepUsage{{Attempt: 1, Usage: runner.Usage{Input: 7, Output: 3}}},
		}},
	}}
	if err := writeUsageFile(path, results); err != nil {
		t.Fatalf("writeUsageFile: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc result.UsageDoc
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("the file is not a UsageDoc: %v\n%s", err, b)
	}
	if len(doc.Skills) != 1 || doc.Skills[0].Name != "code-review" || len(doc.Skills[0].Runs) != 1 || doc.Skills[0].Runs[0].Run != "1/1" {
		t.Errorf("doc = %+v, want skill code-review with run 1/1", doc)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", st.Mode().Perm())
	}
}

// --usage-file works in local mode, and without it nothing changes. It runs
// main() in a process of its own against a stub opencode.
func TestMain_UsageFileInLocalMode(t *testing.T) {
	if root := os.Getenv("PERGENT_TEST_USAGE_REPO"); root != "" {
		os.Args = append([]string{"pergent", "--local", "--skill", "code-review", "--repo-path", root},
			strings.Fields(os.Getenv("PERGENT_TEST_USAGE_ARGS"))...)
		main()
		return
	}

	repo := t.TempDir()
	gitIn(t, repo, "init", "-q")
	writeFile(t, repo, "a.go", "package a\n")
	base := commitAll(t, repo, "base")
	gitIn(t, repo, "update-ref", "refs/remotes/origin/main", base)
	writeFile(t, repo, "a.go", "package a\n\nfunc A() {}\n")
	commitAll(t, repo, "head")

	ndjson := `{"type":"step_start","part":{"type":"step-start"}}
{"type":"tool_use","part":{"tool":"read","state":{"status":"completed","input":{"filePath":"` + filepath.Join(repo, "a.go") + `"},"output":"x"}}}
{"type":"step_finish","part":{"type":"step-finish","reason":"tool-calls","tokens":{"total":15,"input":10,"output":5}}}
{"type":"step_start","part":{"type":"step-start"}}
{"type":"text","part":{"type":"text","text":"` + "```json\\n{\\\"findings\\\": []}\\n```" + `"}}
{"type":"step_finish","part":{"type":"step-finish","reason":"stop","tokens":{"total":24,"input":20,"output":4}}}
`
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ncat <<'EOF'\n" + ndjson + "EOF\n"
	if err := os.WriteFile(filepath.Join(bin, "opencode"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	run := func(t *testing.T, args string) (stdout, stderr string) {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.run=^TestMain_UsageFileInLocalMode$")
		cmd.Env = append(os.Environ(),
			"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
			"PERGENT_TEST_USAGE_REPO="+repo,
			"PERGENT_TEST_USAGE_ARGS="+args,
			"OPENCODE_PROVIDER=anthropic",
			"OPENCODE_MODEL=claude-sonnet-4",
		)
		var so, se bytes.Buffer
		cmd.Stdout, cmd.Stderr = &so, &se
		if err := cmd.Run(); err != nil {
			t.Fatalf("main() failed: %v\nstdout:\n%s\nstderr:\n%s", err, so.String(), se.String())
		}
		return so.String(), se.String()
	}

	plain, _ := run(t, "")
	if !strings.Contains(plain, "No blocking issues") {
		t.Fatalf("the plain run printed %q, want a review comment", plain)
	}

	t.Run("with the flag", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "u.json")
		stdout, _ := run(t, "--usage-file "+path)
		if stdout != plain {
			t.Errorf("stdout differs with --usage-file:\n%q\nwant\n%q", stdout, plain)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("no usage file: %v", err)
		}
		var doc result.UsageDoc
		if err := json.Unmarshal(b, &doc); err != nil {
			t.Fatal(err)
		}
		if len(doc.Skills) != 1 || doc.Skills[0].Name != "code-review" || len(doc.Skills[0].Runs) != 1 {
			t.Fatalf("doc = %s, want one skill code-review with one run", b)
		}
		r := doc.Skills[0].Runs[0]
		if r.Run != "1/1" || len(r.Steps) != 2 {
			t.Fatalf("run = %q with %d steps, want 1/1 with 2", r.Run, len(r.Steps))
		}
		want := result.ToolCall{Tool: "read", Target: "repository", Bytes: 1}
		if len(r.Steps[0].Tools) != 1 || r.Steps[0].Tools[0] != want {
			t.Errorf("step 1 tools = %+v, want [%+v]", r.Steps[0].Tools, want)
		}
		if r.ToolTotals.Read.Calls < 1 {
			t.Errorf("tool_totals.read.calls = %d, want at least 1", r.ToolTotals.Read.Calls)
		}
	})

	t.Run("a path that cannot be written", func(t *testing.T) {
		stdout, stderr := run(t, fmt.Sprintf("--usage-file %s", filepath.Join(t.TempDir(), "missing-dir", "u.json")))
		if stdout != plain {
			t.Errorf("stdout differs with a bad --usage-file:\n%q\nwant\n%q", stdout, plain)
		}
		if !strings.Contains(stderr, "warning: could not write the usage file") {
			t.Errorf("stderr = %q, want the warning", stderr)
		}
	})
}
