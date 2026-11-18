package runner

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"time"

	"github.com/gopaytech/pergent/internal/redact"
)

// MinOpencodeVersion is the oldest opencode pergent runs: the sandbox and the
// event format it relies on are opencode 2's. TestedOpencodeVersion is the one
// its end-to-end tests ran against; a newer one only gets a warning.
const (
	MinOpencodeVersion    = "2.3.2"
	TestedOpencodeVersion = "2.3.2"
)

const versionTimeout = 15 * time.Second

var versionRE = regexp.MustCompile(`v?(\d+)\.(\d+)\.(\d+)`)

// CheckOpencodeVersion runs `opencode --version` and refuses a version older
// than MinOpencodeVersion or one it cannot read. A newer version than the
// tested one returns a warning.
func CheckOpencodeVersion(ctx context.Context) (warning string, err error) {
	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	out, runErr := exec.CommandContext(ctx, "opencode", "--version").CombinedOutput()
	if runErr != nil {
		if errors.Is(runErr, exec.ErrNotFound) {
			return "", fmt.Errorf("opencode was not found on PATH; pergent needs opencode %s or later", MinOpencodeVersion)
		}
		if len(versionRE.Find(out)) == 0 {
			return "", fmt.Errorf("could not read the opencode version: opencode --version failed: %s%s",
				redact.String(runErr.Error()), versionSample(string(out)))
		}
	}
	return checkVersionOutput(string(out))
}

func versionSample(out string) string {
	s := redact.String(out)
	if len(s) > 80 {
		s = s[:80] + "..."
	}
	if s == "" {
		return ""
	}
	return fmt.Sprintf(" (printed %q)", s)
}

func parseVersion(v [4]string) [3]int {
	var n [3]int
	for i := range n {
		n[i], _ = strconv.Atoi(v[i+1])
	}
	return n
}

func versionLess(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// checkVersionOutput judges what `opencode --version` printed. Any pre-release
// suffix after the first x.y.z is ignored.
func checkVersionOutput(out string) (warning string, err error) {
	m := versionRE.FindStringSubmatch(out)
	if m == nil {
		return "", fmt.Errorf("could not read the opencode version%s; pergent needs opencode %s or later", versionSample(out), MinOpencodeVersion)
	}
	found := m[1] + "." + m[2] + "." + m[3]
	got := parseVersion([4]string(m[:4]))
	min := parseVersion([4]string(versionRE.FindStringSubmatch(MinOpencodeVersion)))
	if versionLess(got, min) {
		return "", fmt.Errorf("opencode %s is too old: pergent needs opencode %s or later", found, MinOpencodeVersion)
	}
	tested := parseVersion([4]string(versionRE.FindStringSubmatch(TestedOpencodeVersion)))
	if versionLess(tested, got) {
		return fmt.Sprintf("tested with opencode %s, found %s", TestedOpencodeVersion, found), nil
	}
	return "", nil
}
