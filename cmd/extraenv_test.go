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
