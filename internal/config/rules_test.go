package config

import (
	"slices"
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

func TestBuiltInRulesResolve(t *testing.T) {
	tests := []struct {
		name string
		env  string
		opts Options
		want []string
	}{
		{"unset loads all", "", Options{}, nil},
		{"env", " go, yaml,,ts_js ", Options{}, []string{"go", "yaml", "ts_js"}},
		{"env of empty items is unset", " , ", Options{}, nil},
		{"flag", "", Options{BuiltInRules: "go,yaml", BuiltInRulesSet: true}, []string{"go", "yaml"}},
		{"flag beats env", "go", Options{BuiltInRules: "ts_js", BuiltInRulesSet: true}, []string{"ts_js"}},
		{"empty flag beats env and is unset", "go", Options{BuiltInRules: "", BuiltInRulesSet: true}, nil},
		{"no-built-in-rules false is no conflict", "go", Options{NoBuiltInRules: false, NoBuiltInRulesSet: true}, []string{"go"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupValidGitHub(t)
			t.Setenv("PERGENT_NO_BUILT_IN_RULES", "")
			t.Setenv("PERGENT_BUILT_IN_RULES", tt.env)
			opts := tt.opts
			opts.Skills, opts.Platform = []string{"./skills/code-review.md"}, "github"
			cfg, err := Resolve(opts)
			if err != nil {
				t.Fatalf("Resolve() error: %v", err)
			}
			if !slices.Equal(cfg.BuiltInRules, tt.want) || (tt.want == nil) != (cfg.BuiltInRules == nil) {
				t.Errorf("Resolve() BuiltInRules = %q, want %q", cfg.BuiltInRules, tt.want)
			}
			agent, err := ResolveAgent(tt.opts)
			if err != nil {
				t.Fatalf("ResolveAgent() error: %v", err)
			}
			if !slices.Equal(agent.BuiltInRules, tt.want) || (tt.want == nil) != (agent.BuiltInRules == nil) {
				t.Errorf("ResolveAgent() BuiltInRules = %q, want %q", agent.BuiltInRules, tt.want)
			}
		})
	}
}

// Choosing built-ins and turning them all off can't both hold.
func TestBuiltInRulesConflict(t *testing.T) {
	tests := []struct {
		name, env, noEnv string
		opts             Options
	}{
		{"flags", "", "", Options{BuiltInRules: "go", BuiltInRulesSet: true, NoBuiltInRules: true, NoBuiltInRulesSet: true}},
		{"env", "go", "true", Options{}},
		{"flag and env", "", "true", Options{BuiltInRules: "go", BuiltInRulesSet: true}},
		{"env and flag", "go", "", Options{NoBuiltInRules: true, NoBuiltInRulesSet: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PERGENT_BUILT_IN_RULES", tt.env)
			t.Setenv("PERGENT_NO_BUILT_IN_RULES", tt.noEnv)
			_, err := ResolveAgent(tt.opts)
			if err == nil || !strings.Contains(err.Error(), "--built-in-rules") || !strings.Contains(err.Error(), "--no-built-in-rules") {
				t.Errorf("ResolveAgent() = %v, want an error naming both flags", err)
			}
		})
	}
}
