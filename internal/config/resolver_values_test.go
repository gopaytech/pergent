package config

import (
	"strings"
	"testing"
)

// A value that can't be used is a config error that names the variable and
// the value, not a silent fall back to the default.
func TestResolve_BadEnvValuesAreErrors(t *testing.T) {
	tests := []struct {
		name, env, value string
		want             []string
	}{
		{"max turns text", "PERGENT_MAX_TURNS", "many", []string{"PERGENT_MAX_TURNS", `"many"`}},
		{"max turns padded", "PERGENT_MAX_TURNS", " 5", []string{"PERGENT_MAX_TURNS", `" 5"`}},
		{"concurrency text", "PERGENT_CONCURRENCY", "4x", []string{"PERGENT_CONCURRENCY", `"4x"`}},
		{"timeout text", "PERGENT_TIMEOUT", "ten minutes", []string{"PERGENT_TIMEOUT", `"ten minutes"`}},
		{"timeout no unit", "PERGENT_TIMEOUT", "600", []string{"PERGENT_TIMEOUT", `"600"`}},
		{"context limit text", "PERGENT_CONTEXT_LIMIT", "128k", []string{"PERGENT_CONTEXT_LIMIT", `"128k"`}},
		{"previous review text", "PERGENT_PREVIOUS_REVIEW", "yes", []string{"PERGENT_PREVIOUS_REVIEW", `"yes"`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupValidGitHub(t)
			t.Setenv(tt.env, tt.value)

			_, err := Resolve(Options{Skills: []string{"./skills/code-review.md"}, Platform: "github"})
			if err == nil {
				t.Fatalf("Resolve() with %s=%q passed, want a config error", tt.env, tt.value)
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to contain %q", err, want)
				}
			}

			if _, err := ResolveAgent(Options{}); tt.env != "PERGENT_PREVIOUS_REVIEW" && err == nil {
				t.Errorf("ResolveAgent() with %s=%q passed, want a config error", tt.env, tt.value)
			}
		})
	}
}

func TestResolve_EmptyEnvValuesMeanTheDefault(t *testing.T) {
	setupValidGitHub(t)
	for _, name := range []string{"PERGENT_MAX_TURNS", "PERGENT_CONCURRENCY", "PERGENT_TIMEOUT", "PERGENT_CONTEXT_LIMIT", "PERGENT_PREVIOUS_REVIEW"} {
		t.Setenv(name, "")
	}

	cfg, err := Resolve(Options{Skills: []string{"./skills/code-review.md"}, Platform: "github"})
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if cfg.MaxTurns != 20 || cfg.Concurrency != 4 || cfg.ContextLimit != 0 || cfg.PreviousReview {
		t.Errorf("cfg = %+v, want the defaults", cfg)
	}
}

// An explicit flag wins over the variable, so a bad variable is not read.
func TestResolve_FlagBeatsBadEnv(t *testing.T) {
	setupValidGitHub(t)
	t.Setenv("PERGENT_MAX_TURNS", "many")
	t.Setenv("PERGENT_CONCURRENCY", "many")
	t.Setenv("PERGENT_TIMEOUT", "soon")
	t.Setenv("PERGENT_CONTEXT_LIMIT", "lots")
	t.Setenv("PERGENT_PREVIOUS_REVIEW", "maybe")

	cfg, err := Resolve(Options{
		Skills:            []string{"./skills/code-review.md"},
		Platform:          "github",
		MaxTurns:          5,
		Concurrency:       2,
		ConcurrencySet:    true,
		Timeout:           60_000_000_000,
		ContextLimit:      1000,
		PreviousReview:    true,
		PreviousReviewSet: true,
	})
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if cfg.MaxTurns != 5 || cfg.Concurrency != 2 || cfg.ContextLimit != 1000 || !cfg.PreviousReview {
		t.Errorf("cfg = %+v, want the flag values", cfg)
	}
}

func TestResolve_MaxTurnsBelowOne(t *testing.T) {
	tests := []struct {
		name, env string
		flag      int
		want      string
	}{
		{"flag negative", "", -3, "--max-turns must be at least 1, got -3"},
		{"env negative", "-3", 0, "--max-turns must be at least 1, got -3"},
		{"env zero", "0", 0, "--max-turns must be at least 1, got 0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupValidGitHub(t)
			if tt.env != "" {
				t.Setenv("PERGENT_MAX_TURNS", tt.env)
			}
			_, err := Resolve(Options{Skills: []string{"./skills/code-review.md"}, Platform: "github", MaxTurns: tt.flag})
			if err == nil || err.Error() != tt.want {
				t.Fatalf("Resolve() error = %v, want %q", err, tt.want)
			}
		})
	}
}
