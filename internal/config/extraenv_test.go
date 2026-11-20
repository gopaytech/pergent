package config

import (
	"slices"
	"strings"
	"testing"
)

func TestExtraEnvNames(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    []string
		wantErr string
	}{
		{"empty", "", nil, ""},
		{"one", "OTEL_EXPORTER_OTLP_ENDPOINT", []string{"OTEL_EXPORTER_OTLP_ENDPOINT"}, ""},
		{"spaces are trimmed", " A , B_1 ,_c ", []string{"A", "B_1", "_c"}, ""},
		{"empty items are skipped", "A,,B,", []string{"A", "B"}, ""},
		{"a bad character", "A-B", nil, "A-B"},
		{"a leading digit", "1A", nil, "1A"},
		{"a glob", "AWS_*", nil, "AWS_*"},
		{"an assignment", "A=b", nil, "A=b"},
		{"OPENCODE_ name", "OPENCODE_CONFIG_DIR", nil, "OPENCODE_CONFIG_DIR"},
		{"PERGENT_ name", "PERGENT_X", nil, "PERGENT_X"},
		{"lower-case opencode_ name", "opencode_config_dir", nil, "opencode_config_dir"},
		{"mixed-case Pergent_ name", "Pergent_x", nil, "Pergent_x"},
		{"lower-case github_token", "github_token", nil, "github_token"},
		{"mixed-case Gitlab_Token", "Gitlab_Token", nil, "Gitlab_Token"},
		{"lower-case pwd", "pwd", nil, "pwd"},
		{"mixed-case Xdg_Data_Home", "Xdg_Data_Home", nil, "Xdg_Data_Home"},
		{"GITHUB_TOKEN", "GITHUB_TOKEN", nil, "GITHUB_TOKEN"},
		{"GITLAB_TOKEN", "A,GITLAB_TOKEN", nil, "GITLAB_TOKEN"},
		{"PWD", "PWD", nil, "PWD"},
		{"XDG_DATA_HOME", "XDG_DATA_HOME", nil, "XDG_DATA_HOME"},
		{"XDG_STATE_HOME", "XDG_STATE_HOME", nil, "XDG_STATE_HOME"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExtraEnvNames(tt.in)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) || !strings.Contains(err.Error(), "PERGENT_OPENCODE_ENV") {
					t.Fatalf("ExtraEnvNames(%q) error = %v, want one naming PERGENT_OPENCODE_ENV and %q", tt.in, err, tt.wantErr)
				}
				return
			}
			if err != nil || !slices.Equal(got, tt.want) {
				t.Errorf("ExtraEnvNames(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
			}
		})
	}
}

func TestResolve_BadExtraEnv(t *testing.T) {
	for _, local := range []bool{false, true} {
		setupValidGitHub(t)
		t.Setenv("PERGENT_OPENCODE_ENV", "OPENCODE_DB")
		opts := Options{Skills: []string{"./skills/code-review.md"}, Platform: "github"}
		if local {
			opts.Platform, opts.Local = "", true
		}
		if _, err := Resolve(opts); err == nil || !strings.Contains(err.Error(), "PERGENT_OPENCODE_ENV") {
			t.Errorf("Resolve(local=%v) error = %v, want one naming PERGENT_OPENCODE_ENV", local, err)
		}
	}
	// Own-config mode generates no config, so the value is not read.
	setupValidGitHub(t)
	t.Setenv("OPENCODE_PROVIDER", "")
	t.Setenv("PERGENT_USE_OPENCODE_CONFIG", "true")
	t.Setenv("PERGENT_OPENCODE_ENV", "OPENCODE_DB")
	if _, err := Resolve(Options{Skills: []string{"./skills/code-review.md"}, Platform: "github"}); err != nil {
		t.Errorf("own-config Resolve() error = %v, want none", err)
	}
}
