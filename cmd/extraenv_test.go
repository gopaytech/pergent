package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// A bad PERGENT_OPENCODE_ENV is a config error before any review in CI mode.
func TestMain_BadExtraEnvIsConfigError(t *testing.T) {
	if os.Getenv("PERGENT_TEST_BADEXTRAENV") != "" {
		os.Args = []string{"pergent", "--platform", "gitlab", "--skill", "code-review", "--repo-path", t.TempDir()}
		main()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestMain_BadExtraEnvIsConfigError$")
	cmd.Env = append(os.Environ(),
		"PERGENT_TEST_BADEXTRAENV=1",
		"PERGENT_OPENCODE_ENV=GITLAB_TOKEN",
		"GITLAB_TOKEN=test-token",
		"CI_SERVER_URL=http://127.0.0.1:1",
		"CI_PROJECT_ID=1",
		"CI_MERGE_REQUEST_IID=1",
		"OPENCODE_PROVIDER=anthropic",
		"OPENCODE_MODEL=claude-sonnet-4",
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("main() succeeded, want a config error:\n%s", out)
	}
	if !strings.Contains(string(out), "config error") || !strings.Contains(string(out), "PERGENT_OPENCODE_ENV") {
		t.Errorf("output = %q, want a config error naming PERGENT_OPENCODE_ENV", out)
	}
}

// --test generates a config too, so it checks PERGENT_OPENCODE_ENV like a
// review does, before it runs opencode.
func TestMain_TestModeRefusesBadExtraEnv(t *testing.T) {
	if os.Getenv("PERGENT_TEST_BADEXTRAENV_TESTMODE") != "" {
		os.Args = []string{"pergent", "--test"}
		main()
		return
	}
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/opencode", []byte("#!/bin/sh\necho 2.3.2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestMain_TestModeRefusesBadExtraEnv$")
	cmd.Env = append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"PERGENT_TEST_BADEXTRAENV_TESTMODE=1",
		"PERGENT_TEST_REAL_VERSION_CHECK=1",
		"PERGENT_OPENCODE_ENV=GITHUB_TOKEN",
		"OPENCODE_PROVIDER=anthropic",
		"OPENCODE_MODEL=claude-sonnet-4",
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("--test succeeded, want a failure:\n%s", out)
	}
	if !strings.Contains(string(out), "PERGENT_OPENCODE_ENV") {
		t.Errorf("output = %q, want it to name PERGENT_OPENCODE_ENV", out)
	}
}
