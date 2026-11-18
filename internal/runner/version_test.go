package runner

import (
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
