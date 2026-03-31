package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type openCodeConfig struct {
	Model      string                       `json:"model"`
	Provider   map[string]openCodeProvider   `json:"provider"`
	Agent      map[string]openCodeAgent      `json:"agent"`
	Permission map[string]string             `json:"permission"`
}

type openCodeProvider struct {
	APIKey string                          `json:"apiKey"`
	Models map[string]openCodeModelLimits  `json:"models,omitempty"`
}

type openCodeModelLimits struct {
	Limit openCodeLimit `json:"limit"`
}

type openCodeLimit struct {
	Context int `json:"context"`
	Output  int `json:"output"`
}

type openCodeAgent struct {
	Description string `json:"description"`
	Model       string `json:"model"`
	Steps       int    `json:"steps"`
}

func GenerateOpenCodeConfig(maxTurns int, maxTokens int) (string, func(), error) {
	provider := os.Getenv("OPENCODE_PROVIDER")
	model := os.Getenv("OPENCODE_MODEL")

	if provider == "" {
		return "", nil, fmt.Errorf("OPENCODE_PROVIDER is required")
	}
	if model == "" {
		return "", nil, fmt.Errorf("OPENCODE_MODEL is required")
	}

	fullModel := provider + "/" + model

	cfg := openCodeConfig{
		Model: fullModel,
		Provider: map[string]openCodeProvider{
			provider: {
				APIKey: "{env:OPENCODE_API_KEY}",
				Models: map[string]openCodeModelLimits{
					model: {
						Limit: openCodeLimit{
							Context: maxTokens,
							Output:  10000,
						},
					},
				},
			},
		},
		Agent: map[string]openCodeAgent{
			"default": {
				Description: "pergent review agent",
				Model:       fullModel,
				Steps:       maxTurns,
			},
		},
		Permission: map[string]string{
			"read":  "allow",
			"write": "deny",
			"edit":  "deny",
			"bash":  "deny",
			"glob":  "allow",
			"grep":  "allow",
			"list":  "allow",
		},
	}

	tmpDir, err := os.MkdirTemp("", "pergent-*")
	if err != nil {
		return "", nil, fmt.Errorf("creating temp dir: %w", err)
	}

	configPath := filepath.Join(tmpDir, "opencode.json")
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		os.RemoveAll(tmpDir)
		return "", nil, fmt.Errorf("marshaling config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		os.RemoveAll(tmpDir)
		return "", nil, fmt.Errorf("writing config: %w", err)
	}

	cleanup := func() {
		os.RemoveAll(tmpDir)
	}

	return configPath, cleanup, nil
}
