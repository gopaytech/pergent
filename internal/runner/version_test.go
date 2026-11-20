package runner

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCheckVersionOutput(t *testing.T) {
	tests := []struct {
		name, out string
		warnHas   []string
		errHas    []string
	}{
		{name: "tested", out: "opencode v2.3.2\n"},
		{name: "newer", out: "opencode v2.5.0", warnHas: []string{"tested with opencode 2.3.2, found 2.5.0"}},
		{name: "prerelease", out: "Update available\nopencode v2.4.0-beta.2", warnHas: []string{"2.4.0"}},
		{name: "old 1.x", out: "1.19.4", errHas: []string{"2.3.2 or later", "1.19.4"}},
		{name: "patch below", out: "opencode v2.3.1", errHas: []string{"2.3.2 or later", "2.3.1"}},
		{name: "minor below", out: "opencode v2.2.9", errHas: []string{"2.3.2 or later", "2.2.9"}},
		{name: "empty", out: "", errHas: []string{"could not read the opencode version"}},
		{name: "no number", out: "opencode", errHas: []string{"could not read the opencode version"}},
		{name: "address first", out: "listening on 10.0.0.1\nopencode v2.3.2\n"},
		{name: "update notice first", out: "update available 2.5.0\nopencode 2.3.1\n", errHas: []string{"2.3.2 or later", "2.3.1"}},
		{name: "bare after a notice", out: "update available: 2.5.0\n", warnHas: []string{"found 2.5.0"}},
		{name: "prerelease of the floor", out: "opencode v2.3.2-beta.1", errHas: []string{"2.3.2 or later", "2.3.2-beta.1"}},
		{name: "bare prerelease of the floor", out: "2.3.2-rc.1\n", errHas: []string{"2.3.2 or later"}},
		{name: "build metadata", out: "opencode v2.3.2+abc\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warn, err := checkVersionOutput(tt.out)
			if len(tt.errHas) > 0 {
				if err == nil {
					t.Fatalf("no error, want one with %v", tt.errHas)
				}
				for _, s := range tt.errHas {
					if !strings.Contains(err.Error(), s) {
						t.Errorf("error %q lacks %q", err, s)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("error %v", err)
			}
			if len(tt.warnHas) == 0 && warn != "" {
				t.Errorf("warning %q, want none", warn)
			}
			for _, s := range tt.warnHas {
				if !strings.Contains(warn, s) {
					t.Errorf("warning %q lacks %q", warn, s)
				}
			}
		})
	}
}

func TestCheckVersionOutput_SampleIsRedactedAndShort(t *testing.T) {
	_, err := checkVersionOutput("Bearer abcdefghijklmnopqrstuvwxyz0123456789 " + strings.Repeat("x", 500))
	if err == nil || strings.Contains(err.Error(), "abcdefghijklmnopqrstuvwxyz") || len(err.Error()) > 300 {
		t.Errorf("error = %v, want a short redacted sample", err)
	}
}

// A failing `opencode --version` is refused even when its output holds a
// version: a broken binary must not pass the check before any review.
func TestCheckOpencodeVersion_FailingRunIsRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake opencode is a shell script")
	}
	bin := t.TempDir()
	script := "#!/bin/sh\necho 'opencode 2.3.2'\necho 'error: please update to 2.5.0' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "opencode"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	if _, err := CheckOpencodeVersion(context.Background()); err == nil || !strings.Contains(err.Error(), "opencode --version failed") {
		t.Errorf("CheckOpencodeVersion() error = %v, want the failed run refused", err)
	}
}
