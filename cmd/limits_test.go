package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// --context-limit and --output-limit reach the opencode config in local
// mode. It runs main() in a process of its own against a stub opencode that
// copies the config it is given.
func TestMain_LimitsInLocalMode(t *testing.T) {
	if root := os.Getenv("PERGENT_TEST_LIMITS_REPO"); root != "" {
		os.Args = []string{"pergent", "--local", "--skill", "code-review", "--repo-path", root,
			"--context-limit", "1000000", "--output-limit", "64000"}
		main()
		return
	}

	repo := t.TempDir()
	realRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "init", "-q")
	writeFile(t, repo, "a.go", "package a\n")
	base := commitAll(t, repo, "base")
	gitIn(t, repo, "update-ref", "refs/remotes/origin/main", base)
	writeFile(t, repo, "a.go", "package a\n\nfunc A() {}\n")
	commitAll(t, repo, "head")

	ndjson := `{"type":"step_start","part":{"type":"step-start"}}
{"type":"text","part":{"type":"text","text":"` + "```json\\n{\\\"findings\\\": []}\\n```" + `"}}
{"type":"step_finish","part":{"type":"step-finish","reason":"stop","tokens":{"total":24,"input":20,"output":4}}}
`
	copied := filepath.Join(t.TempDir(), "config.json")
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ncp \"$OPENCODE_CONFIG\" '" + copied + "'\ncat <<'EOF'\n" + ndjson + "EOF\n"
	if err := os.WriteFile(filepath.Join(bin, "opencode"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestMain_LimitsInLocalMode$")
	cmd.Env = append(os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"PERGENT_TEST_LIMITS_REPO="+realRepo,
		"OPENCODE_PROVIDER=anthropic",
		"OPENCODE_MODEL=claude-sonnet-4",
		"XDG_DATA_HOME="+t.TempDir(),
	)
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	if err := cmd.Run(); err != nil {
		t.Fatalf("main() failed: %v\nstdout:\n%s\nstderr:\n%s", err, so.String(), se.String())
	}

	data, err := os.ReadFile(copied)
	if err != nil {
		t.Fatalf("the stub copied no config: %v\nstderr:\n%s", err, se.String())
	}
	var cfg struct {
		Provider map[string]struct {
			Models map[string]struct {
				Limit map[string]int `json:"limit"`
			} `json:"models"`
		} `json:"provider"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	got := cfg.Provider["anthropic"].Models["claude-sonnet-4"].Limit
	if got["context"] != 1000000 || got["output"] != 64000 || len(got) != 2 {
		t.Errorf("limit = %v in %s, want context 1000000 and output 64000", got, strings.TrimSpace(string(data)))
	}
}
