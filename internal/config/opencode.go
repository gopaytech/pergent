package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gopaytech/pergent/internal/checkout"
)

// AgentName is the opencode agent that carries the skill prompt and step limit.
// It is set as default_agent and passed via --agent; opencode would otherwise
// run its built-in "build" agent and ignore our agent definition.
const AgentName = "pergent"

// DataDirPrefix starts the name of each run's opencode data directory. It must
// not start with "pergent-": the external_directory rule for pergent's temp
// directories would then let a run read the auth.json and the session database
// inside it.
const DataDirPrefix = "opencode-data-pergent-"

type openCodeConfig struct {
	Schema       string                      `json:"$schema,omitempty"`
	Model        string                      `json:"model"`
	DefaultAgent string                      `json:"default_agent"`
	Instructions []string                    `json:"instructions,omitempty"`
	Provider     map[string]openCodeProvider `json:"provider"`
	Agent        map[string]openCodeAgent    `json:"agent"`
	Permission   map[string]any              `json:"permission"`
	Compaction   openCodeCompaction          `json:"compaction"`
	LSP          bool                        `json:"lsp"`
}

type openCodeCompaction struct {
	Auto bool `json:"auto"`
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
	Limit *openCodeLimit `json:"limit,omitempty"`
}

type openCodeLimit struct {
	Context int `json:"context"`
	// Output is required by opencode's schema; 0 makes opencode use its
	// default output cap.
	Output int `json:"output"`
}

type openCodeAgent struct {
	Description string `json:"description"`
	Mode        string `json:"mode"`
	Model       string `json:"model"`
	Steps       int    `json:"steps"`
	Prompt      string `json:"prompt,omitempty"`
}

// OpenCodeOptions configures GenerateOpenCodeConfigWith.
type OpenCodeOptions struct {
	// MaxTurns is the agent's step limit.
	MaxTurns int
	// ContextLimit overrides the model's context window when > 0; otherwise
	// opencode uses the limits from its model registry.
	ContextLimit int
	// Prompt is the agent's system prompt.
	Prompt string
	// RepoPath is where AGENTS.md/CLAUDE.md are looked up when
	// ExplicitInstructions is false.
	RepoPath string
	// ExplicitInstructions makes Instructions the only instruction files:
	// nothing is auto-discovered in RepoPath. It is for runs where the repo
	// contents are not trusted.
	ExplicitInstructions bool
	// Instructions are absolute paths of instruction files, used only when
	// ExplicitInstructions is set.
	Instructions []string
	// DenySecretFiles additionally denies reading *.pem, *.key and *.tfvars.
	DenySecretFiles bool
}

// ReadRules returns the generated config's read permission, pattern to
// "allow" or "deny". Rules are last-match-wins, in the order encoding/json
// writes the keys (sorted): "*", "*.env", "*.env.*", "*.env.example", then
// with denySecretFiles "*.key", "*.pem", "*.tfvars". The patterns don't
// overlap with the earlier ones, so the order only matters relative to "*".
func ReadRules(denySecretFiles bool) map[string]string {
	rules := map[string]string{
		"*":             "allow",
		"*.env":         "deny",
		"*.env.*":       "deny",
		"*.env.example": "allow",
	}
	if denySecretFiles {
		rules["*.key"] = "deny"
		rules["*.pem"] = "deny"
		rules["*.tfvars"] = "deny"
	}
	return rules
}

// ReadDenied reports whether the read rules deny reading path (a path in the
// repository). Like opencode, "*" matches any run of characters, "/"
// included, and the last matching rule in key order wins. This is the only
// guard for the review's diff files: they are named by index, so opencode's
// own read rules never see the path a diff came from. If this ever matched
// differently from opencode, a denied file's diff could be readable.
func ReadDenied(path string, denySecretFiles bool) bool {
	rules := ReadRules(denySecretFiles)
	keys := make([]string, 0, len(rules))
	for k := range rules {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	denied := false
	for _, k := range keys {
		if wildcardMatch(k, path) {
			denied = rules[k] == "deny"
		}
	}
	return denied
}

// wildcardMatch reports whether s matches pattern, where "*" matches any run
// of characters (possibly empty) and every other character matches itself.
func wildcardMatch(pattern, s string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == s
	}
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]
	last := parts[len(parts)-1]
	for _, p := range parts[1 : len(parts)-1] {
		i := strings.Index(s, p)
		if i < 0 {
			return false
		}
		s = s[i+len(p):]
	}
	return strings.HasSuffix(s, last)
}

// GeneratesConfig reports whether GenerateOpenCodeConfigWith writes a config
// in this environment. Without OPENCODE_PROVIDER it writes none, and opencode
// runs with its own config, none of the permissions below.
func GeneratesConfig() bool {
	return os.Getenv("OPENCODE_PROVIDER") != ""
}

// GenerateOpenCodeConfig writes a temporary opencode.json for one skill run.
// contextLimit overrides the model's context window when > 0; otherwise
// opencode uses the limits from its model registry.
func GenerateOpenCodeConfig(maxTurns int, contextLimit int, skillPrompt string, repoPath string) (string, func(), error) {
	return GenerateOpenCodeConfigWith(OpenCodeOptions{
		MaxTurns:     maxTurns,
		ContextLimit: contextLimit,
		Prompt:       skillPrompt,
		RepoPath:     repoPath,
	})
}

// GenerateOpenCodeConfigWith writes a temporary opencode.json as configured by o.
func GenerateOpenCodeConfigWith(o OpenCodeOptions) (string, func(), error) {
	maxTurns, contextLimit, skillPrompt := o.MaxTurns, o.ContextLimit, o.Prompt
	provider := os.Getenv("OPENCODE_PROVIDER")
	model := os.Getenv("OPENCODE_MODEL")

	// If no provider set, skip config generation — opencode uses its own config
	if !GeneratesConfig() {
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

	// The model is always declared: opencode doesn't know models served from a
	// custom base URL otherwise ("Model not found"). An entry without a limit
	// keeps the limits from opencode's model registry.
	modelEntry := openCodeModelLimits{}
	if contextLimit > 0 {
		modelEntry.Limit = &openCodeLimit{Context: contextLimit}
	}

	instructions := o.Instructions
	if !o.ExplicitInstructions {
		instructions = projectInstructions(o.RepoPath, o.DenySecretFiles)
	}

	readRules := ReadRules(o.DenySecretFiles)

	cfg := openCodeConfig{
		Schema:       "https://opencode.ai/config.json",
		Model:        fullModel,
		DefaultAgent: AgentName,
		Instructions: instructions,
		Provider: map[string]openCodeProvider{
			provider: {
				NPM: npm,
				Options: openCodeProviderOptions{
					APIKey:  "{env:OPENCODE_API_KEY}",
					BaseURL: baseURL,
				},
				Models: map[string]openCodeModelLimits{model: modelEntry},
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
		// patterns are emitted as "*", "*.env", "*.env.*", "*.env.example"
		// (then "*.key", "*.pem", "*.tfvars" with DenySecretFiles), which is
		// the order these rules need.
		Permission: map[string]any{
			"read": readRules,
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
			// opencode's default for these two is "ask". `opencode run` rejects
			// every ask, and a rejection ends the run with no answer; a deny
			// only fails that one tool call. The model may re-read the files
			// pergent attaches, and read the diffs and the index it writes for
			// runs, which all live in pergent-* temp directories. opencode saves
			// a long tool result in <data>/opencode/tool-output (so in 1.3.17 and
			// 1.19.4) and points the model at it; only that folder of the data
			// directory is allowed. If a version moves it, that read is denied.
			"external_directory": map[string]string{
				"*":                                      "deny",
				filepath.Join(os.TempDir(), "pergent-*"): "allow",
				filepath.Join(os.TempDir(), DataDirPrefix+"*", "opencode", "tool-output", "*"): "allow",
			},
			// A repeated tool call is bounded by the agent's step limit.
			"doom_loop": "allow",
		},
		// Auto-compaction summarises the conversation when the context fills
		// and then prompts the model to continue, which can end a run with no
		// review. With it off, an overflow ends the run with a
		// ContextOverflowError, which pergent reports as the failure cause.
		Compaction: openCodeCompaction{Auto: false},
		// The read tool starts language servers whatever the permissions say,
		// and a server can be a program from the reviewed repo (opencode prefers
		// its node_modules/.bin/biome), running with the LLM key in its
		// environment. opencode 1.19 leaves them off when this is unset, but
		// older versions turn them on, and so can a global config.
		LSP: false,
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
//
// A file may be a symlink, but only to a regular file inside the repo that
// the read rules (with denySecretFiles) allow. Any other link would put a
// file the agent must not read, or one outside the repo such as
// /proc/self/environ, in the model's context. Such a link is skipped with a
// warning and the next name is tried. The repo root itself may be reached
// through a symlink, so real paths are compared.
func projectInstructions(repoPath string, denySecretFiles bool) []string {
	if repoPath == "" {
		repoPath = "."
	}
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		return nil
	}
	root, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil
	}
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		path := filepath.Join(abs, name)
		linfo, err := os.Lstat(path)
		if err != nil {
			continue
		}
		isLink := linfo.Mode()&os.ModeSymlink != 0
		skip := func(why string) {
			if isLink {
				log.Printf("warning: not using %s as instructions: %s", path, why)
			}
		}
		real, err := filepath.EvalSymlinks(path)
		if err != nil {
			skip("the link does not resolve")
			continue
		}
		if info, err := os.Stat(real); err != nil || !info.Mode().IsRegular() {
			skip("it is not a link to a regular file")
			continue
		}
		if !checkout.Within(root, real) {
			skip("the link leaves the repository")
			continue
		}
		rel, err := filepath.Rel(root, real)
		if err != nil || ReadDenied(filepath.ToSlash(rel), denySecretFiles) {
			skip("the read rules deny its target")
			continue
		}
		return []string{path}
	}
	return nil
}
