package config

import (
	"strings"
	"testing"
)

func TestResolve_CheckDeleted(t *testing.T) {
	tests := []struct {
		name string
		env  string
		opts Options
		want bool
	}{
		{"default is off", "", Options{}, false},
		{"env false turns it off", "false", Options{}, false},
		{"env true", "true", Options{}, true},
		{"flag on", "", Options{CheckDeleted: true, CheckDeletedSet: true}, true},
		{"flag off", "", Options{CheckDeleted: false, CheckDeletedSet: true}, false},
		{"flag off beats env true", "true", Options{CheckDeleted: false, CheckDeletedSet: true}, false},
		{"flag on beats env false", "false", Options{CheckDeleted: true, CheckDeletedSet: true}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupValidGitHub(t)
			t.Setenv("PERGENT_CHECK_DELETED", tt.env)
			opts := tt.opts
			opts.Skills, opts.Platform = []string{"./skills/code-review.md"}, "github"

			cfg, err := Resolve(opts)
			if err != nil {
				t.Fatalf("Resolve() error: %v", err)
			}
			if cfg.CheckDeleted != tt.want {
				t.Errorf("Resolve() CheckDeleted = %v, want %v", cfg.CheckDeleted, tt.want)
			}

			// Agent mode resolves it the same way.
			agent, err := ResolveAgent(tt.opts)
			if err != nil {
				t.Fatalf("ResolveAgent() error: %v", err)
			}
			if agent.CheckDeleted != tt.want {
				t.Errorf("ResolveAgent() CheckDeleted = %v, want %v", agent.CheckDeleted, tt.want)
			}
		})
	}
}

func TestResolve_CheckDeletedLocalMode(t *testing.T) {
	t.Setenv("OPENCODE_PROVIDER", "anthropic")
	t.Setenv("PERGENT_CHECK_DELETED", "true")
	cfg, err := Resolve(Options{Skills: []string{"./skills/code-review.md"}, Local: true})
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if !cfg.CheckDeleted {
		t.Error("CheckDeleted = false, want PERGENT_CHECK_DELETED=true honoured with --local")
	}
}

func TestResolve_CheckDeletedBadEnv(t *testing.T) {
	setupValidGitHub(t)
	t.Setenv("PERGENT_CHECK_DELETED", "maybe")

	_, err := Resolve(Options{Skills: []string{"./skills/code-review.md"}, Platform: "github"})
	if err == nil {
		t.Fatal("Resolve() with PERGENT_CHECK_DELETED=maybe passed, want a config error")
	}
	for _, want := range []string{"PERGENT_CHECK_DELETED", `"maybe"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err, want)
		}
	}
	if _, err := ResolveAgent(Options{}); err == nil {
		t.Error("ResolveAgent() with PERGENT_CHECK_DELETED=maybe passed, want a config error")
	}

	// An explicit flag wins, so the bad variable is not read.
	if _, err := Resolve(Options{Skills: []string{"./skills/code-review.md"}, Platform: "github", CheckDeleted: true, CheckDeletedSet: true}); err != nil {
		t.Errorf("Resolve() with the flag set error: %v, want the variable unread", err)
	}
}
