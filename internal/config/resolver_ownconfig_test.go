package config

import (
	"strings"
	"testing"
)

// Without OPENCODE_PROVIDER pergent writes no opencode config: no sandbox,
// project config on, the default agent, and the platform token in opencode's
// environment, so a change's own AGENTS.md or opencode.json applies. In CI an
// empty variable (an unset CI variable expands to "") must not slide into that
// by accident.

func platformSetups() map[string]func(*testing.T) {
	return map[string]func(*testing.T){
		"github": setupValidGitHub,
		"gitlab": func(t *testing.T) {
			setupValidGitLab(t)
			t.Setenv("CI_PROJECT_ID", "7")
		},
	}
}

func resolveOwnConfig(platform string) (Config, error) {
	return Resolve(Options{Skills: []string{"./skills/code-review.md"}, Platform: platform})
}

func TestResolve_PlatformNeedsProviderOrOptIn(t *testing.T) {
	for platform, setup := range platformSetups() {
		t.Run(platform, func(t *testing.T) {
			setup(t)
			t.Setenv("OPENCODE_PROVIDER", "")
			t.Setenv("PERGENT_USE_OPENCODE_CONFIG", "")

			_, err := resolveOwnConfig(platform)
			if err == nil {
				t.Fatal("Resolve() with an empty OPENCODE_PROVIDER passed, want a config error")
			}
			for _, want := range []string{"OPENCODE_PROVIDER", "PERGENT_USE_OPENCODE_CONFIG=true", "no sandbox"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to contain %q", err, want)
				}
			}
		})
	}
}

func TestResolve_ExplicitFalseIsNotTheOptIn(t *testing.T) {
	setupValidGitHub(t)
	t.Setenv("OPENCODE_PROVIDER", "")
	t.Setenv("PERGENT_USE_OPENCODE_CONFIG", "false")
	if _, err := resolveOwnConfig("github"); err == nil {
		t.Fatal("Resolve() with PERGENT_USE_OPENCODE_CONFIG=false passed, want a config error")
	}
}

func TestResolve_OptInRunsWithoutProviderAndWarns(t *testing.T) {
	for platform, setup := range platformSetups() {
		t.Run(platform, func(t *testing.T) {
			setup(t)
			t.Setenv("OPENCODE_PROVIDER", "")
			t.Setenv("OPENCODE_MODEL", "")
			t.Setenv("PERGENT_USE_OPENCODE_CONFIG", "true")

			cfg, err := resolveOwnConfig(platform)
			if err != nil {
				t.Fatalf("Resolve() error: %v", err)
			}
			if !cfg.UseOpenCodeConfig {
				t.Error("UseOpenCodeConfig = false, want true")
			}
			if len(cfg.Warnings) != 1 || !strings.Contains(cfg.Warnings[0], "no sandbox") {
				t.Errorf("Warnings = %q, want one that says there is no sandbox", cfg.Warnings)
			}
		})
	}
}

func TestResolve_ProviderSetNeedsNoOptIn(t *testing.T) {
	for platform, setup := range platformSetups() {
		t.Run(platform, func(t *testing.T) {
			setup(t)
			t.Setenv("OPENCODE_PROVIDER", "anthropic")
			t.Setenv("PERGENT_USE_OPENCODE_CONFIG", "")

			cfg, err := resolveOwnConfig(platform)
			if err != nil {
				t.Fatalf("Resolve() error: %v", err)
			}
			if cfg.UseOpenCodeConfig || len(cfg.Warnings) != 0 {
				t.Errorf("cfg = %+v, want no opt-in and no warnings", cfg)
			}
		})
	}
}

func TestResolve_BadOptInValueIsAnError(t *testing.T) {
	t.Run("platform", func(t *testing.T) {
		setupValidGitHub(t)
		t.Setenv("OPENCODE_PROVIDER", "anthropic")
		t.Setenv("PERGENT_USE_OPENCODE_CONFIG", "yes")
		_, err := resolveOwnConfig("github")
		if err == nil || !strings.Contains(err.Error(), "PERGENT_USE_OPENCODE_CONFIG") || !strings.Contains(err.Error(), `"yes"`) {
			t.Errorf("error = %v, want one that names PERGENT_USE_OPENCODE_CONFIG and %q", err, "yes")
		}
	})
	t.Run("local", func(t *testing.T) {
		t.Setenv("PERGENT_USE_OPENCODE_CONFIG", "yes")
		_, err := Resolve(Options{Skills: []string{"./skills/code-review.md"}, Local: true})
		if err == nil || !strings.Contains(err.Error(), "PERGENT_USE_OPENCODE_CONFIG") {
			t.Errorf("error = %v, want one that names PERGENT_USE_OPENCODE_CONFIG", err)
		}
	})
}

// --local is the user's own machine: no opt-in, no warning for an empty provider.
func TestResolve_LocalNeedsNoOptIn(t *testing.T) {
	t.Setenv("OPENCODE_PROVIDER", "")
	t.Setenv("OPENCODE_MODEL", "")
	t.Setenv("PERGENT_USE_OPENCODE_CONFIG", "")

	cfg, err := Resolve(Options{Skills: []string{"./skills/code-review.md"}, Local: true})
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if len(cfg.Warnings) != 0 {
		t.Errorf("Warnings = %q, want none", cfg.Warnings)
	}
}

// OPENCODE_MODEL with an empty OPENCODE_PROVIDER is an easy mistake: the model
// is ignored and opencode's own config decides.
func TestResolve_ModelWithoutProvider(t *testing.T) {
	t.Run("local warns", func(t *testing.T) {
		t.Setenv("OPENCODE_PROVIDER", "")
		t.Setenv("OPENCODE_MODEL", "claude-sonnet-4")
		cfg, err := Resolve(Options{Skills: []string{"./skills/code-review.md"}, Local: true})
		if err != nil {
			t.Fatalf("Resolve() error: %v", err)
		}
		if len(cfg.Warnings) != 1 || !strings.Contains(cfg.Warnings[0], "OPENCODE_MODEL") || !strings.Contains(cfg.Warnings[0], "OPENCODE_PROVIDER") {
			t.Errorf("Warnings = %q, want one about OPENCODE_MODEL without OPENCODE_PROVIDER", cfg.Warnings)
		}
	})
	t.Run("platform error names it", func(t *testing.T) {
		setupValidGitHub(t)
		t.Setenv("OPENCODE_PROVIDER", "")
		t.Setenv("OPENCODE_MODEL", "claude-sonnet-4")
		t.Setenv("PERGENT_USE_OPENCODE_CONFIG", "")
		_, err := resolveOwnConfig("github")
		if err == nil || !strings.Contains(err.Error(), "OPENCODE_MODEL is set") {
			t.Errorf("error = %v, want it to say OPENCODE_MODEL is set without OPENCODE_PROVIDER", err)
		}
	})
	t.Run("platform opt-in warns", func(t *testing.T) {
		setupValidGitHub(t)
		t.Setenv("OPENCODE_PROVIDER", "")
		t.Setenv("OPENCODE_MODEL", "claude-sonnet-4")
		t.Setenv("PERGENT_USE_OPENCODE_CONFIG", "true")
		cfg, err := resolveOwnConfig("github")
		if err != nil {
			t.Fatalf("Resolve() error: %v", err)
		}
		found := false
		for _, w := range cfg.Warnings {
			found = found || strings.Contains(w, "OPENCODE_MODEL")
		}
		if !found {
			t.Errorf("Warnings = %q, want one about OPENCODE_MODEL", cfg.Warnings)
		}
	})
}
