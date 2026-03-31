package config

import (
	"encoding/json"
	"os"
	"testing"
)

func TestGenerateOpenCodeConfig(t *testing.T) {
	t.Setenv("OPENCODE_PROVIDER", "anthropic")
	t.Setenv("OPENCODE_MODEL", "claude-sonnet-4")
	t.Setenv("OPENCODE_API_KEY", "sk-test-123")

	configPath, cleanup, err := GenerateOpenCodeConfig(20, 100000, "test prompt")
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

	// Check permission field restricts write/bash
	if cfg.Permission["write"] != "deny" {
		t.Errorf("permission.write = %q, want %q", cfg.Permission["write"], "deny")
	}
	if cfg.Permission["edit"] != "deny" {
		t.Errorf("permission.edit = %q, want %q", cfg.Permission["edit"], "deny")
	}
	if cfg.Permission["bash"] != "deny" {
		t.Errorf("permission.bash = %q, want %q", cfg.Permission["bash"], "deny")
	}
	if cfg.Permission["read"] != "allow" {
		t.Errorf("permission.read = %q, want %q", cfg.Permission["read"], "allow")
	}
	if cfg.Permission["glob"] != "allow" {
		t.Errorf("permission.glob = %q, want %q", cfg.Permission["glob"], "allow")
	}
	if cfg.Permission["grep"] != "allow" {
		t.Errorf("permission.grep = %q, want %q", cfg.Permission["grep"], "allow")
	}
}

func TestGenerateOpenCodeConfig_WithBaseURL(t *testing.T) {
	t.Setenv("OPENCODE_PROVIDER", "openai")
	t.Setenv("OPENCODE_MODEL", "gpt-4o")
	t.Setenv("OPENCODE_API_KEY", "sk-test")
	t.Setenv("OPENCODE_BASE_URL", "http://localhost:4000/v1")

	configPath, cleanup, err := GenerateOpenCodeConfig(20, 100000, "test prompt")
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

	configPath, cleanup, err := GenerateOpenCodeConfig(10, 50000, "")
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

	configPath, cleanup, err := GenerateOpenCodeConfig(20, 100000, "test prompt")
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

	_, _, err := GenerateOpenCodeConfig(20, 100000, "test prompt")
	if err == nil {
		t.Error("should error when provider is set but model is empty")
	}
}
