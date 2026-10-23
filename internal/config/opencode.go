package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// AgentName is the opencode agent that carries the skill prompt and step limit.
// It is set as default_agent and passed via --agent; opencode would otherwise
// run its built-in "build" agent and ignore our agent definition.
const AgentName = "pergent"

type openCodeConfig struct {
	Schema       string                      `json:"$schema,omitempty"`
	Model        string                      `json:"model"`
	DefaultAgent string                      `json:"default_agent"`
	Instructions []string                    `json:"instructions,omitempty"`
	Provider     map[string]openCodeProvider `json:"provider"`
	Agent        map[string]openCodeAgent    `json:"agent"`
	Permission   map[string]any              `json:"permission"`
}

type openCodeProvider struct {
	NPM     string                         `json:"npm,omitempty"`
	Options openCodeProviderOptions        `json:"options"`
	Models  map[string]openCodeModelLimits `json:"models,omitempty"`
}

type openCodeProviderOptions struct {
	APIKey  string `json:"apiKey"`
	BaseURL string `json:"baseURL,omitempty"`
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
	Mode        string `json:"mode"`
	Model       string `json:"model"`
	Steps       int    `json:"steps"`
	Prompt      string `json:"prompt,omitempty"`
}

func GenerateOpenCodeConfig(maxTurns int, maxTokens int, skillPrompt string, repoPath string) (string, func(), error) {
	provider := os.Getenv("OPENCODE_PROVIDER")
	model := os.Getenv("OPENCODE_MODEL")

	// If no provider set, skip config generation — opencode uses its own config
	if provider == "" {
		noop := func() {}
		return "", noop, nil
	}
	if model == "" {
		return "", nil, fmt.Errorf("OPENCODE_MODEL is required when OPENCODE_PROVIDER is set")
	}

	fullModel := provider + "/" + model
	npm := os.Getenv("OPENCODE_NPM")
	baseURL := os.Getenv("OPENCODE_BASE_URL")

	// Default to openai-compatible SDK when using a custom base URL
	// This forces /chat/completions instead of /responses
	if npm == "" && baseURL != "" {
		npm = "@ai-sdk/openai-compatible"
	}

	// Cap output tokens to fit within context
	outputTokens := 10000
	if maxTokens < 20000 {
		outputTokens = maxTokens / 4
	}

	cfg := openCodeConfig{
		Schema:       "https://opencode.ai/config.json",
		Model:        fullModel,
		DefaultAgent: AgentName,
		Instructions: projectInstructions(repoPath),
		Provider: map[string]openCodeProvider{
			provider: {
				NPM: npm,
				Options: openCodeProviderOptions{
					APIKey:  "{env:OPENCODE_API_KEY}",
					BaseURL: baseURL,
				},
				Models: map[string]openCodeModelLimits{
					model: {
						Limit: openCodeLimit{
							Context: maxTokens,
							Output:  outputTokens,
						},
					},
				},
			},
		},
		Agent: map[string]openCodeAgent{
			AgentName: {
				Description: "pergent review agent",
				Mode:        "primary",
				Model:       fullModel,
				Steps:       maxTurns,
				Prompt:      skillPrompt,
			},
		},
		// Rules are last-match-wins. encoding/json sorts map keys, so the read
		// patterns are emitted as "*", "*.env", "*.env.*", "*.env.example",
		// which is the order these rules need.
		Permission: map[string]any{
			"read": map[string]string{
				"*":             "allow",
				"*.env":         "deny",
				"*.env.*":       "deny",
				"*.env.example": "allow",
			},
			// edit also gates the write and apply_patch tools
			"edit":      "deny",
			"bash":      "deny",
			"glob":      "allow",
			"grep":      "allow",
			"list":      "allow",
			"task":      "deny",
			"skill":     "deny",
			"webfetch":  "deny",
			"websearch": "deny",
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

// projectInstructions returns the repo's AGENTS.md (or else CLAUDE.md) as an
// absolute path. The subprocess runs with OPENCODE_DISABLE_PROJECT_CONFIG, which
// also stops opencode from finding these files itself, so they are passed
// explicitly. Like opencode, only the first match is used.
func projectInstructions(repoPath string) []string {
	if repoPath == "" {
		repoPath = "."
	}
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		path, err := filepath.Abs(filepath.Join(repoPath, name))
		if err != nil {
			continue
		}
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return []string{path}
		}
	}
	return nil
}
