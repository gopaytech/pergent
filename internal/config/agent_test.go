package config

import (
	"strings"
	"testing"
	"time"
)

// clearCI makes sure no CI platform is detectable from the environment.
func clearCI(t *testing.T) {
	t.Helper()
	for _, k := range []string{"GITHUB_ACTIONS", "CI_MERGE_REQUEST_IID", "PERGENT_PLATFORM", "GITHUB_TOKEN", "GITLAB_TOKEN"} {
		t.Setenv(k, "")
	}
}

func TestResolveAgent_DefaultsWithoutPlatform(t *testing.T) {
	clearCI(t)

	cfg, err := ResolveAgent(Options{})
	if err != nil {
		t.Fatalf("ResolveAgent() error: %v (agent mode must not detect a platform)", err)
	}
	if cfg.MaxTurns != 20 || cfg.Concurrency != 4 || cfg.Timeout != 10*time.Minute || cfg.ContextLimit != 0 {
		t.Errorf("ResolveAgent() = %+v, want the review defaults", cfg)
	}
	if cfg.Platform != "" {
		t.Errorf("Platform = %q, want none", cfg.Platform)
	}
	if len(cfg.Warnings) != 0 {
		t.Errorf("Warnings = %v, want none", cfg.Warnings)
	}
}

func TestResolveAgent_FlagsBeatEnv(t *testing.T) {
	clearCI(t)
	t.Setenv("PERGENT_MAX_TURNS", "50")
	t.Setenv("PERGENT_CONCURRENCY", "8")
	t.Setenv("PERGENT_TIMEOUT", "3m")
	t.Setenv("PERGENT_CONTEXT_LIMIT", "1000")

	cfg, err := ResolveAgent(Options{})
	if err != nil {
		t.Fatalf("ResolveAgent() error: %v", err)
	}
	if cfg.MaxTurns != 50 || cfg.Concurrency != 8 || cfg.Timeout != 3*time.Minute || cfg.ContextLimit != 1000 {
		t.Errorf("ResolveAgent() = %+v, want the env values", cfg)
	}

	cfg, err = ResolveAgent(Options{MaxTurns: 5, Concurrency: 2, Timeout: time.Minute, ContextLimit: 99})
	if err != nil {
		t.Fatalf("ResolveAgent() error: %v", err)
	}
	if cfg.MaxTurns != 5 || cfg.Concurrency != 2 || cfg.Timeout != time.Minute || cfg.ContextLimit != 99 {
		t.Errorf("ResolveAgent() = %+v, want the flag values", cfg)
	}
}

func TestResolveAgent_Invalid(t *testing.T) {
	clearCI(t)
	tests := []struct {
		name string
		opts Options
		want string
	}{
		{"explicit concurrency zero", Options{Concurrency: 0, ConcurrencySet: true}, "--concurrency must be at least 1, got 0"},
		{"negative timeout", Options{Timeout: -time.Second}, "--timeout must be positive"},
		{"negative context limit", Options{ContextLimit: -1}, "--context-limit must be a positive number"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ResolveAgent(tt.opts)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ResolveAgent() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestResolveAgent_MaxTokensDeprecated(t *testing.T) {
	clearCI(t)

	cfg, err := ResolveAgent(Options{MaxTokens: 30000})
	if err != nil {
		t.Fatalf("ResolveAgent() error: %v", err)
	}
	if len(cfg.Warnings) != 1 || cfg.Warnings[0] != maxTokensDeprecation {
		t.Errorf("Warnings = %v, want the deprecation warning", cfg.Warnings)
	}
}
