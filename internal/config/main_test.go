package config

import (
	"os"
	"strings"
	"testing"
)

// TestMain clears the CI variables Resolve auto-detects from, so a test sees
// only what it sets itself. Run inside GitHub Actions, GITHUB_ACTIONS and
// GITHUB_EVENT_PATH would otherwise make every test a GitHub pull request.
func TestMain(m *testing.M) {
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		for _, prefix := range []string{"GITHUB_", "GITLAB_", "CI_"} {
			if strings.HasPrefix(name, prefix) {
				os.Unsetenv(name)
			}
		}
	}
	os.Unsetenv("CI")
	os.Exit(m.Run())
}
