package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateOpenCodeConfig(t *testing.T) {
	t.Setenv("OPENCODE_PROVIDER", "anthropic")
	t.Setenv("OPENCODE_MODEL", "claude-sonnet-4")
	t.Setenv("OPENCODE_API_KEY", "sk-test-123")

	configPath, cleanup, err := GenerateOpenCodeConfig(20, 100000, "test prompt", t.TempDir())
	if err != nil {
		t.Fatalf("GenerateOpenCodeConfig() error: %v", err)
	}
	defer cleanup()

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}

	var cfg openCodeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parsing config JSON: %v", err)
	}

	// Check model field
	if cfg.Model != "anthropic/claude-sonnet-4" {
		t.Errorf("model = %q, want %q", cfg.Model, "anthropic/claude-sonnet-4")
	}

	// Check provider options
	prov, ok := cfg.Provider["anthropic"]
	if !ok {
		t.Fatal("provider 'anthropic' missing")
	}
	if prov.Options.APIKey != "{env:OPENCODE_API_KEY}" {
		t.Errorf("options.apiKey = %q, want %q", prov.Options.APIKey, "{env:OPENCODE_API_KEY}")
	}

	// The skill prompt must live on the agent opencode actually runs
	if cfg.DefaultAgent != AgentName {
		t.Errorf("default_agent = %q, want %q", cfg.DefaultAgent, AgentName)
	}
	agent, ok := cfg.Agent[AgentName]
	if !ok {
		t.Fatalf("agent %q missing", AgentName)
	}
	if agent.Mode != "primary" {
		t.Errorf("agent.mode = %q, want %q", agent.Mode, "primary")
	}
	if agent.Prompt != "test prompt" {
		t.Errorf("agent.prompt = %q, want %q", agent.Prompt, "test prompt")
	}
	if agent.Steps != 20 {
		t.Errorf("agent.steps = %d, want %d", agent.Steps, 20)
	}
	if _, ok := cfg.Agent["default"]; ok {
		t.Error("agent 'default' should not be generated")
	}

	// Check permission field restricts edit/bash and unneeded tools
	for _, key := range []string{"edit", "bash", "task", "skill", "webfetch", "websearch"} {
		if cfg.Permission[key] != "deny" {
			t.Errorf("permission.%s = %v, want %q", key, cfg.Permission[key], "deny")
		}
	}
	for _, key := range []string{"glob", "grep", "list"} {
		if cfg.Permission[key] != "allow" {
			t.Errorf("permission.%s = %v, want %q", key, cfg.Permission[key], "allow")
		}
	}
	if _, ok := cfg.Permission["write"]; ok {
		t.Error("permission.write should not be set (write is gated by edit)")
	}

	read, ok := cfg.Permission["read"].(map[string]any)
	if !ok {
		t.Fatalf("permission.read = %v, want object", cfg.Permission["read"])
	}
	wantRead := map[string]string{
		"*":             "allow",
		"*.env":         "deny",
		"*.env.*":       "deny",
		"*.env.example": "allow",
	}
	for pattern, action := range wantRead {
		if read[pattern] != action {
			t.Errorf("permission.read[%q] = %v, want %q", pattern, read[pattern], action)
		}
	}
	if len(read) != len(wantRead) {
		t.Errorf("permission.read has %d rules, want %d", len(read), len(wantRead))
	}

	// opencode applies rules last-match-wins in file order
	order := []string{`"*": "allow"`, `"*.env": "deny"`, `"*.env.*": "deny"`, `"*.env.example": "allow"`}
	last := -1
	for _, rule := range order {
		idx := bytes.Index(data, []byte(rule))
		if idx <= last {
			t.Errorf("read rule %s out of order in generated JSON", rule)
		}
		last = idx
	}
}

func TestGenerateOpenCodeConfig_Instructions(t *testing.T) {
	t.Setenv("OPENCODE_PROVIDER", "anthropic")
	t.Setenv("OPENCODE_MODEL", "claude-sonnet-4")

	tests := []struct {
		name  string
		files []string
		want  string
	}{
		{"none", nil, ""},
		{"agents", []string{"AGENTS.md"}, "AGENTS.md"},
		{"claude", []string{"CLAUDE.md"}, "CLAUDE.md"},
		{"agents wins", []string{"AGENTS.md", "CLAUDE.md"}, "AGENTS.md"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := t.TempDir()
			for _, f := range tt.files {
				if err := os.WriteFile(filepath.Join(repo, f), []byte("# context"), 0644); err != nil {
					t.Fatal(err)
				}
			}

			configPath, cleanup, err := GenerateOpenCodeConfig(20, 100000, "", repo)
			if err != nil {
				t.Fatalf("GenerateOpenCodeConfig() error: %v", err)
			}
			defer cleanup()

			data, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatalf("reading config: %v", err)
			}
			var cfg openCodeConfig
			if err := json.Unmarshal(data, &cfg); err != nil {
				t.Fatalf("parsing config JSON: %v", err)
			}

			if tt.want == "" {
				if len(cfg.Instructions) != 0 {
					t.Errorf("instructions = %v, want none", cfg.Instructions)
				}
				return
			}
			want := filepath.Join(repo, tt.want)
			if len(cfg.Instructions) != 1 || cfg.Instructions[0] != want {
				t.Errorf("instructions = %v, want [%s]", cfg.Instructions, want)
			}
		})
	}
}

func TestGenerateOpenCodeConfig_WithBaseURL(t *testing.T) {
	t.Setenv("OPENCODE_PROVIDER", "openai")
	t.Setenv("OPENCODE_MODEL", "gpt-4o")
	t.Setenv("OPENCODE_API_KEY", "sk-test")
	t.Setenv("OPENCODE_BASE_URL", "http://localhost:4000/v1")

	configPath, cleanup, err := GenerateOpenCodeConfig(20, 100000, "test prompt", t.TempDir())
	if err != nil {
		t.Fatalf("GenerateOpenCodeConfig() error: %v", err)
	}
	defer cleanup()

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}

	var cfg openCodeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parsing config JSON: %v", err)
	}

	prov := cfg.Provider["openai"]
	if prov.Options.BaseURL != "http://localhost:4000/v1" {
		t.Errorf("options.baseURL = %q, want %q", prov.Options.BaseURL, "http://localhost:4000/v1")
	}
}

func TestGenerateOpenCodeConfig_Cleanup(t *testing.T) {
	t.Setenv("OPENCODE_PROVIDER", "anthropic")
	t.Setenv("OPENCODE_MODEL", "claude-sonnet-4")
	t.Setenv("OPENCODE_API_KEY", "sk-test")

	configPath, cleanup, err := GenerateOpenCodeConfig(10, 50000, "", t.TempDir())
	if err != nil {
		t.Fatalf("GenerateOpenCodeConfig() error: %v", err)
	}

	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("config file should exist: %v", err)
	}

	cleanup()

	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Error("config file should be deleted after cleanup")
	}
}

func TestGenerateOpenCodeConfig_NoProvider(t *testing.T) {
	t.Setenv("OPENCODE_PROVIDER", "")
	t.Setenv("OPENCODE_MODEL", "claude-sonnet-4")

	configPath, cleanup, err := GenerateOpenCodeConfig(20, 100000, "test prompt", t.TempDir())
	if err != nil {
		t.Fatalf("should not error when provider is empty: %v", err)
	}
	defer cleanup()
	if configPath != "" {
		t.Errorf("configPath = %q, want empty (use opencode's own config)", configPath)
	}
}

func TestGenerateOpenCodeConfig_ProviderWithoutModel(t *testing.T) {
	t.Setenv("OPENCODE_PROVIDER", "anthropic")
	t.Setenv("OPENCODE_MODEL", "")

	_, _, err := GenerateOpenCodeConfig(20, 100000, "test prompt", t.TempDir())
	if err == nil {
		t.Error("should error when provider is set but model is empty")
	}
}
