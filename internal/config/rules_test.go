package config

import (
	"strings"
	"testing"
)

func TestNoBuiltInRulesResolve(t *testing.T) {
	tests := []struct {
		name string
		env  string
		opts Options
		want bool
	}{
		{"default keeps them on", "", Options{}, false},
		{"env true", "true", Options{}, true},
		{"flag", "", Options{NoBuiltInRules: true, NoBuiltInRulesSet: true}, true},
		{"flag off beats env true", "true", Options{NoBuiltInRules: false, NoBuiltInRulesSet: true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupValidGitHub(t)
			t.Setenv("PERGENT_NO_BUILT_IN_RULES", tt.env)
			opts := tt.opts
			opts.Skills, opts.Platform = []string{"./skills/code-review.md"}, "github"
			cfg, err := Resolve(opts)
			if err != nil {
				t.Fatalf("Resolve() error: %v", err)
			}
			if cfg.NoBuiltInRules != tt.want {
				t.Errorf("Resolve() NoBuiltInRules = %v, want %v", cfg.NoBuiltInRules, tt.want)
			}
			agent, err := ResolveAgent(tt.opts)
			if err != nil {
				t.Fatalf("ResolveAgent() error: %v", err)
			}
			if agent.NoBuiltInRules != tt.want {
				t.Errorf("ResolveAgent() NoBuiltInRules = %v, want %v", agent.NoBuiltInRules, tt.want)
			}
		})
	}
}

func TestNoBuiltInRulesBadEnv(t *testing.T) {
	t.Setenv("PERGENT_NO_BUILT_IN_RULES", "maybe")
	_, err := ResolveAgent(Options{})
	if err == nil || !strings.Contains(err.Error(), "PERGENT_NO_BUILT_IN_RULES") {
		t.Errorf("ResolveAgent() = %v, want a config error naming PERGENT_NO_BUILT_IN_RULES", err)
	}
}
