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

// TempPrefix starts the name of each of pergent's own temp directories (the
// batch attachments, the on-demand diffs). The external_directory rule allows
// reading them.
const TempPrefix = "pergent-"

// DataDirPrefix starts the name of each run's opencode data directory. It must
// not start with "pergent-": the external_directory rule for pergent's temp
// directories would then let a run read the auth.json and the session database
// inside it.
const DataDirPrefix = "opencode-data-pergent-"

// StateDirPrefix starts the name of each run's opencode state directory
// (XDG_STATE_HOME), which holds the shared server's service.json. The same
// rule as DataDirPrefix applies: it must not start with "pergent-".
const StateDirPrefix = "opencode-state-pergent-"

// PromptFileName is the file, next to opencode.json, that holds the agent's
// prompt. The config names it with a {file:} token.
const PromptFileName = "agent-prompt.md"

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
	Snapshot     bool                        `json:"snapshot"`
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
	// Permission repeats the top-level block: opencode 2 appends an agent's
	// rules after every global one and the last match wins.
	Permission map[string]any `json:"permission,omitempty"`
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
	// Linked are the directories of linked repositories the run may read
	// through external_directory. Only an impact run sets it; every other run
	// reads nothing outside the repository and pergent's own temp files.
	Linked []string
	// Model is the run's model; nil is the one the environment names. The
	// key is always {env:OPENCODE_API_KEY}: the caller sets that variable in
	// the run's own environment (runner.Options.Env).
	Model *Model
}

// Model is the model a run uses, as OPENCODE_PROVIDER, OPENCODE_MODEL,
// OPENCODE_BASE_URL and OPENCODE_NPM name it.
type Model struct {
	Provider, Model, BaseURL, NPM string
}

// EnvModel is the model the environment names.
func EnvModel() Model {
	return Model{Provider: os.Getenv("OPENCODE_PROVIDER"), Model: os.Getenv("OPENCODE_MODEL"),
		BaseURL: os.Getenv("OPENCODE_BASE_URL"), NPM: os.Getenv("OPENCODE_NPM")}
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

// linkedAllows are the external_directory patterns that let a run read the
// directory dir and everything below it. A directory that is not absolute, is
// the root, or has a wildcard character in its name gets none: the pattern
// would reach further than the one clone it is meant for.
func linkedAllows(dir string) []string {
	if dir == "" || !filepath.IsAbs(dir) || strings.ContainsAny(dir, "*?[]{}") {
		return nil
	}
	dir = filepath.Clean(dir)
	if dir == string(filepath.Separator) {
		return nil
	}
	return []string{dir, filepath.Join(dir, "*")}
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
	m := EnvModel()
	if o.Model != nil {
		m = *o.Model
	}
	provider, model := m.Provider, m.Model

	// If no provider set, skip config generation — opencode uses its own config
	if !GeneratesConfig() {
		noop := func() {}
		return "", noop, nil
	}
	if model == "" {
		return "", nil, fmt.Errorf("OPENCODE_MODEL is required when OPENCODE_PROVIDER is set")
	}

	fullModel := provider + "/" + model
	npm := m.NPM
	baseURL := m.BaseURL

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

	externalDirectory := map[string]string{
		"*": "deny",
		filepath.Join(os.TempDir(), TempPrefix+"*"):                                    "allow",
		filepath.Join(os.TempDir(), DataDirPrefix+"*", "opencode", "tool-output", "*"): "allow",
	}
	for _, dir := range o.Linked {
		for _, pattern := range linkedAllows(dir) {
			externalDirectory[pattern] = "allow"
		}
	}

	// An allow-list: "*" denies every tool, then only the read tools are
	// allowed, so a tool opencode adds later (opencode 2 added execute,
	// question and image generation) is denied without a change here.
	// Rules are last-match-wins. encoding/json sorts map keys, and "*" sorts
	// before letters, so "*" is written first and the specific allows after it,
	// which is the order these rules need. The read patterns inside "read" are
	// emitted as "*", "*.env", "*.env.*", "*.env.example" (then "*.key",
	// "*.pem", "*.tfvars" with DenySecretFiles) for the same reason.
	// The block is also set on the agent: opencode 2 appends agent rules after
	// the global ones, so a user's global config cannot loosen it.
	permission := map[string]any{
		"*":    "deny",
		"read": readRules,
		"glob": "allow",
		"grep": "allow",
		// "*": "deny" would also deny the files pergent attaches, the diffs and
		// the index it writes for runs (all in pergent-* temp directories), and
		// the <data>/opencode/tool-output folder where opencode saves a long tool
		// result and points the model at it, so those are allowed here. Only that
		// folder of the data directory is allowed. If a version moves it, that
		// read is denied. A denied call only fails that one tool call. An impact
		// run also reads its linked repositories' clones.
		"external_directory": externalDirectory,
		// Not a permission action in opencode 2; kept for compatibility.
		"doom_loop": "allow",
	}

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
				Permission:  permission,
			},
		},
		Permission: permission,
		// Auto-compaction summarises the conversation when the context fills
		// and then prompts the model to continue, which can end a run with no
		// review. With it off, an overflow ends the run with a
		// context overflow error, which pergent reports as the failure cause.
		Compaction: openCodeCompaction{Auto: false},
		// opencode 2 never starts language servers. Before it, the read tool
		// started them whatever the permissions said, and a server can be a
		// program from the reviewed repo (opencode prefers its
		// node_modules/.bin/biome), running with the LLM key in its
		// environment. The setting is kept in case a version turns them on.
		LSP: false,
		// Snapshots run git in the checkout after each step and write to its
		// object store; a review writes nothing there.
		Snapshot: false,
	}

	tmpDir, err := os.MkdirTemp("", "pergent-*")
	if err != nil {
		return "", nil, fmt.Errorf("creating temp dir: %w", err)
	}

	// opencode expands {env:VAR} and {file:path} over the whole config text, so
	// a prompt inside opencode.json would reach the model changed (a skill
	// body naming {env:GITLAB_TOKEN} would carry the token) or fail every run
	// (a stray {file:missing}). A {file:} token is replaced by the file's
	// content, which is not scanned again, so the prompt stays literal there.
	if skillPrompt != "" {
		promptPath := filepath.Join(tmpDir, PromptFileName)
		// A "}" would end the token early.
		if strings.Contains(promptPath, "}") {
			os.RemoveAll(tmpDir)
			return "", nil, fmt.Errorf("temp dir path %q contains a closing brace, which opencode's {file:} cannot take", tmpDir)
		}
		if err := os.WriteFile(promptPath, []byte(skillPrompt), 0600); err != nil {
			os.RemoveAll(tmpDir)
			return "", nil, fmt.Errorf("writing prompt: %w", err)
		}
		agent := cfg.Agent[AgentName]
		agent.Prompt = "{file:" + promptPath + "}"
		cfg.Agent[AgentName] = agent
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
