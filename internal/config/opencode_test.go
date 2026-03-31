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

	configPath, cleanup, err := GenerateOpenCodeConfig(20, 100000)
	if err != nil {
		t.Fatalf("GenerateOpenCodeConfig() error: %v", err)
	}
	defer cleanup()

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parsing config JSON: %v", err)
	}

	// Check model field
	model, ok := raw["model"].(string)
	if !ok || model != "anthropic/claude-sonnet-4" {
		t.Errorf("model = %q, want %q", model, "anthropic/claude-sonnet-4")
	}

	// Check permission field restricts write/bash
	perm, ok := raw["permission"].(map[string]interface{})
	if !ok {
		t.Fatal("permission field missing")
	}
	if perm["write"] != "deny" {
		t.Errorf("permission.write = %q, want %q", perm["write"], "deny")
	}
	if perm["edit"] != "deny" {
		t.Errorf("permission.edit = %q, want %q", perm["edit"], "deny")
	}
	if perm["bash"] != "deny" {
		t.Errorf("permission.bash = %q, want %q", perm["bash"], "deny")
	}
	if perm["read"] != "allow" {
		t.Errorf("permission.read = %q, want %q", perm["read"], "allow")
	}
	if perm["glob"] != "allow" {
		t.Errorf("permission.glob = %q, want %q", perm["glob"], "allow")
	}
	if perm["grep"] != "allow" {
		t.Errorf("permission.grep = %q, want %q", perm["grep"], "allow")
	}
}

func TestGenerateOpenCodeConfig_Cleanup(t *testing.T) {
	t.Setenv("OPENCODE_PROVIDER", "anthropic")
	t.Setenv("OPENCODE_MODEL", "claude-sonnet-4")
	t.Setenv("OPENCODE_API_KEY", "sk-test")

	configPath, cleanup, err := GenerateOpenCodeConfig(10, 50000)
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

func TestGenerateOpenCodeConfig_MissingProvider(t *testing.T) {
	t.Setenv("OPENCODE_PROVIDER", "")
	t.Setenv("OPENCODE_MODEL", "claude-sonnet-4")

	_, _, err := GenerateOpenCodeConfig(20, 100000)
	if err == nil {
		t.Error("should error when OPENCODE_PROVIDER is not set")
	}
}
