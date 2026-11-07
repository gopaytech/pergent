package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestGenerateOpenCodeConfig(t *testing.T) {
	t.Setenv("OPENCODE_PROVIDER", "anthropic")
	t.Setenv("OPENCODE_MODEL", "claude-sonnet-4")
	t.Setenv("OPENCODE_API_KEY", "sk-test-123")

	configPath, cleanup, err := GenerateOpenCodeConfig(20, 0, "test prompt", t.TempDir())
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
	promptPath := filepath.Join(filepath.Dir(configPath), "agent-prompt.md")
	if want := "{file:" + promptPath + "}"; agent.Prompt != want {
		t.Errorf("agent.prompt = %q, want %q", agent.Prompt, want)
	}
	if got, err := os.ReadFile(promptPath); err != nil || string(got) != "test prompt" {
		t.Errorf("prompt file = %q, %v, want %q", got, err, "test prompt")
	}
	if info, err := os.Stat(promptPath); err != nil || info.Mode().Perm() != 0600 {
		t.Errorf("prompt file mode = %v, %v, want 0600", info, err)
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

	// opencode's default for these is "ask", which `opencode run` rejects,
	// and a rejection ends the run with no answer.
	if cfg.Permission["doom_loop"] != "allow" {
		t.Errorf("permission.doom_loop = %v, want %q", cfg.Permission["doom_loop"], "allow")
	}
	external, ok := cfg.Permission["external_directory"].(map[string]any)
	if !ok {
		t.Fatalf("permission.external_directory = %v, want object", cfg.Permission["external_directory"])
	}
	attachments := filepath.Join(os.TempDir(), "pergent-*")
	toolOutput := filepath.Join(os.TempDir(), DataDirPrefix+"*", "opencode", "tool-output", "*")
	wantExternal := map[string]string{"*": "deny", attachments: "allow", toolOutput: "allow"}
	for pattern, action := range wantExternal {
		if external[pattern] != action {
			t.Errorf("permission.external_directory[%q] = %v, want %q", pattern, external[pattern], action)
		}
	}
	if len(external) != len(wantExternal) {
		t.Errorf("permission.external_directory has %d rules, want %d", len(external), len(wantExternal))
	}
	if bytes.Index(data, []byte(`"*": "deny"`)) > bytes.Index(data, []byte(`"`+attachments+`": "allow"`)) {
		t.Error(`external_directory "*" must come before the attachments rule (last match wins)`)
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

			configPath, cleanup, err := GenerateOpenCodeConfig(20, 0, "", repo)
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

	configPath, cleanup, err := GenerateOpenCodeConfig(20, 0, "test prompt", t.TempDir())
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

	configPath, cleanup, err := GenerateOpenCodeConfig(10, 0, "a prompt", t.TempDir())
	if err != nil {
		t.Fatalf("GenerateOpenCodeConfig() error: %v", err)
	}

	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("config file should exist: %v", err)
	}
	promptPath := filepath.Join(filepath.Dir(configPath), "agent-prompt.md")
	if _, err := os.Stat(promptPath); err != nil {
		t.Fatalf("prompt file should exist: %v", err)
	}

	cleanup()

	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Error("config file should be deleted after cleanup")
	}
	if _, err := os.Stat(promptPath); !os.IsNotExist(err) {
		t.Error("prompt file should be deleted after cleanup")
	}
}

// opencode expands {env:VAR} and {file:path} over the whole config text, so a
// prompt written into opencode.json would reach the model changed ({env:}
// replaced by a secret) or fail the run ({file:} of a missing file). The text
// is read from a file instead, and opencode does not scan a file's content
// again.
func TestGenerateOpenCodeConfig_PromptStaysLiteral(t *testing.T) {
	t.Setenv("OPENCODE_PROVIDER", "anthropic")
	t.Setenv("OPENCODE_MODEL", "claude-sonnet-4")
	prompt := "Never print {env:GITLAB_TOKEN}.\nSee {file:/nonexistent} and \"quotes\"."

	configPath, cleanup, err := GenerateOpenCodeConfig(20, 0, prompt, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"{env:GITLAB_TOKEN}", "{file:/nonexistent}"} {
		if bytes.Contains(raw, []byte(token)) {
			t.Errorf("config holds %q; opencode would expand it", token)
		}
	}
	got, err := os.ReadFile(filepath.Join(filepath.Dir(configPath), "agent-prompt.md"))
	if err != nil || string(got) != prompt {
		t.Errorf("prompt file = %q, %v, want the prompt unchanged", got, err)
	}
}

func TestGenerateOpenCodeConfig_EmptyPromptHasNoFile(t *testing.T) {
	t.Setenv("OPENCODE_PROVIDER", "anthropic")
	t.Setenv("OPENCODE_MODEL", "claude-sonnet-4")
	configPath, cleanup, err := GenerateOpenCodeConfig(20, 0, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	raw, _ := os.ReadFile(configPath)
	if bytes.Contains(raw, []byte(`"prompt"`)) {
		t.Errorf("config has a prompt for an empty prompt:\n%s", raw)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(configPath), "agent-prompt.md")); !os.IsNotExist(err) {
		t.Errorf("a prompt file exists for an empty prompt (stat error %v)", err)
	}
}

func TestGenerateOpenCodeConfig_NoProvider(t *testing.T) {
	t.Setenv("OPENCODE_PROVIDER", "")
	t.Setenv("OPENCODE_MODEL", "claude-sonnet-4")

	configPath, cleanup, err := GenerateOpenCodeConfig(20, 0, "test prompt", t.TempDir())
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

	_, _, err := GenerateOpenCodeConfig(20, 0, "test prompt", t.TempDir())
	if err == nil {
		t.Error("should error when provider is set but model is empty")
	}
}

// A language server is a program from the reviewed repo (opencode prefers its
// node_modules/.bin/biome), started by the read tool whatever the permissions
// say. Older opencode turns them on when lsp is unset, so false must be explicit.
func TestGenerateOpenCodeConfig_LSPOff(t *testing.T) {
	t.Setenv("OPENCODE_PROVIDER", "anthropic")
	t.Setenv("OPENCODE_MODEL", "claude-sonnet-4")

	for name, opts := range map[string]OpenCodeOptions{
		"default":  {MaxTurns: 20},
		"hardened": {MaxTurns: 20, ExplicitInstructions: true, DenySecretFiles: true},
	} {
		t.Run(name, func(t *testing.T) {
			path, cleanup, err := GenerateOpenCodeConfigWith(opts)
			if err != nil {
				t.Fatalf("GenerateOpenCodeConfigWith() error: %v", err)
			}
			defer cleanup()

			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading config: %v", err)
			}
			var raw map[string]any
			if err := json.Unmarshal(data, &raw); err != nil {
				t.Fatalf("parsing config JSON: %v", err)
			}
			if lsp, ok := raw["lsp"].(bool); !ok || lsp {
				t.Errorf("lsp = %v, want false", raw["lsp"])
			}
		})
	}
}

// external_directory is decided by the last matching rule. opencode's data
// directory holds auth.json and the session database, so a run may read only
// the tool output opencode itself points it at, not the rest.
func TestGenerateOpenCodeConfig_ExternalDirectory(t *testing.T) {
	t.Setenv("OPENCODE_PROVIDER", "anthropic")
	t.Setenv("OPENCODE_MODEL", "claude-sonnet-4")

	path, cleanup, err := GenerateOpenCodeConfigWith(OpenCodeOptions{MaxTurns: 20})
	if err != nil {
		t.Fatalf("GenerateOpenCodeConfigWith() error: %v", err)
	}
	defer cleanup()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	var raw struct {
		Permission struct {
			ExternalDirectory map[string]string `json:"external_directory"`
		} `json:"permission"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parsing config JSON: %v", err)
	}
	rules := raw.Permission.ExternalDirectory
	keys := make([]string, 0, len(rules))
	for k := range rules {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	allowed := func(dir string) bool {
		// opencode checks a file outside the repo as its directory plus "/*".
		pattern, got := filepath.Join(dir, "*"), false
		for _, k := range keys {
			if wildcardMatch(k, pattern) {
				got = rules[k] == "allow"
			}
		}
		return got
	}

	dataDir := filepath.Join(os.TempDir(), DataDirPrefix+"abc123")
	for dir, want := range map[string]bool{
		filepath.Join(os.TempDir(), "pergent-batch-abc123"): true,
		filepath.Join(os.TempDir(), "pergent-diffs-abc123"): true,
		filepath.Join(dataDir, "opencode", "tool-output"):   true,
		filepath.Join(dataDir, "opencode"):                  false,
		dataDir:                                             false,
		filepath.Join(os.TempDir(), "someone-else"):         false,
		"/etc": false,
	} {
		if got := allowed(dir); got != want {
			t.Errorf("allowed(%q) = %v, want %v", dir, got, want)
		}
	}
}

func TestGenerateOpenCodeConfig_LimitsAndCompaction(t *testing.T) {
	t.Setenv("OPENCODE_PROVIDER", "anthropic")
	t.Setenv("OPENCODE_MODEL", "claude-sonnet-4")

	tests := []struct {
		name         string
		contextLimit int
		wantLimit    bool
	}{
		{"model registry limits by default", 0, false},
		{"explicit context limit", 64000, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath, cleanup, err := GenerateOpenCodeConfig(20, tt.contextLimit, "", t.TempDir())
			if err != nil {
				t.Fatalf("GenerateOpenCodeConfig() error: %v", err)
			}
			defer cleanup()

			data, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatalf("reading config: %v", err)
			}
			var raw map[string]any
			if err := json.Unmarshal(data, &raw); err != nil {
				t.Fatalf("parsing config JSON: %v", err)
			}

			// opencode 1.19 reads compaction.auto; false turns auto-compaction off
			compaction, ok := raw["compaction"].(map[string]any)
			if !ok {
				t.Fatalf("compaction = %v, want an object", raw["compaction"])
			}
			if auto, ok := compaction["auto"].(bool); !ok || auto {
				t.Errorf("compaction.auto = %v, want false", compaction["auto"])
			}

			// The model must be declared even without a limit, or opencode can't
			// find models served from a custom base URL.
			provider := raw["provider"].(map[string]any)["anthropic"].(map[string]any)
			entry, ok := provider["models"].(map[string]any)["claude-sonnet-4"].(map[string]any)
			if !ok {
				t.Fatalf("provider.models = %v, want an entry for the model", provider["models"])
			}
			limit, hasLimit := entry["limit"].(map[string]any)
			if !tt.wantLimit {
				if hasLimit {
					t.Errorf("limit = %v, want none so opencode uses the model's real limits", limit)
				}
				return
			}
			if !hasLimit {
				t.Fatalf("model entry = %v, want a limit", entry)
			}
			if limit["context"] != float64(tt.contextLimit) {
				t.Errorf("limit.context = %v, want %d", limit["context"], tt.contextLimit)
			}
			// opencode's schema requires output; 0 means its default output cap
			if limit["output"] != float64(0) {
				t.Errorf("limit.output = %v, want 0", limit["output"])
			}
		})
	}
}

// generateWith writes a config via GenerateOpenCodeConfigWith and returns the
// raw JSON.
func generateWith(t *testing.T, o OpenCodeOptions) []byte {
	t.Helper()
	t.Setenv("OPENCODE_PROVIDER", "anthropic")
	t.Setenv("OPENCODE_MODEL", "claude-sonnet-4")

	configPath, cleanup, err := GenerateOpenCodeConfigWith(o)
	if err != nil {
		t.Fatalf("GenerateOpenCodeConfigWith() error: %v", err)
	}
	t.Cleanup(cleanup)

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	return data
}

func TestGenerateWith_ExplicitNoInstructions(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "AGENTS.md"), []byte("be nice"), 0644); err != nil {
		t.Fatal(err)
	}

	data := generateWith(t, OpenCodeOptions{
		MaxTurns:             20,
		Prompt:               "p",
		RepoPath:             repo,
		ExplicitInstructions: true,
	})

	var cfg openCodeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Instructions) != 0 {
		t.Errorf("instructions = %v, want none", cfg.Instructions)
	}
	if bytes.Contains(data, []byte("AGENTS.md")) {
		t.Error("config must not mention the repo's AGENTS.md")
	}
}

func TestGenerateWith_ExplicitInstructions(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "AGENTS.md"), []byte("be nice"), 0644); err != nil {
		t.Fatal(err)
	}

	data := generateWith(t, OpenCodeOptions{
		MaxTurns:             20,
		Prompt:               "p",
		RepoPath:             repo,
		ExplicitInstructions: true,
		Instructions:         []string{"/in/instructions.md"},
	})

	var cfg openCodeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Instructions) != 1 || cfg.Instructions[0] != "/in/instructions.md" {
		t.Errorf("instructions = %v, want [/in/instructions.md]", cfg.Instructions)
	}
}

func TestGenerateWith_DenySecretFiles(t *testing.T) {
	data := generateWith(t, OpenCodeOptions{
		MaxTurns:        20,
		Prompt:          "p",
		RepoPath:        t.TempDir(),
		DenySecretFiles: true,
	})

	var cfg openCodeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	read, ok := cfg.Permission["read"].(map[string]any)
	if !ok {
		t.Fatalf("permission.read = %T, want object", cfg.Permission["read"])
	}
	want := map[string]string{
		"*":             "allow",
		"*.env":         "deny",
		"*.env.*":       "deny",
		"*.env.example": "allow",
		"*.key":         "deny",
		"*.pem":         "deny",
		"*.tfvars":      "deny",
	}
	for k, v := range want {
		if read[k] != v {
			t.Errorf("permission.read[%q] = %v, want %q", k, read[k], v)
		}
	}
	if len(read) != len(want) {
		t.Errorf("permission.read has %d rules, want %d: %v", len(read), len(want), read)
	}

	// Rules are last-match-wins, so the order in the file matters: read the
	// keys back in document order.
	dec := json.NewDecoder(bytes.NewReader(data))
	var order []string
	inRead := false
	depth := 0
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch v := tok.(type) {
		case json.Delim:
			if v == '{' {
				depth++
			} else if v == '}' {
				if inRead && depth == 3 {
					inRead = false
				}
				depth--
			}
		case string:
			if depth == 2 && v == "read" {
				// next token is the opening brace of the read object
				if next, err := dec.Token(); err == nil {
					if d, ok := next.(json.Delim); ok && d == '{' {
						depth++
						inRead = true
					}
				}
			} else if inRead && depth == 3 {
				order = append(order, v)
				// skip the value
				if _, err := dec.Token(); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	wantOrder := []string{"*", "*.env", "*.env.*", "*.env.example", "*.key", "*.pem", "*.tfvars"}
	if len(order) != len(wantOrder) {
		t.Fatalf("read key order = %v, want %v", order, wantOrder)
	}
	for i := range wantOrder {
		if order[i] != wantOrder[i] {
			t.Fatalf("read key order = %v, want %v", order, wantOrder)
		}
	}
}

// goldenCIConfig is the exact output of GenerateOpenCodeConfig(20, 0,
// "test prompt", repo) for a repo with an AGENTS.md, captured before
// GenerateOpenCodeConfigWith was introduced, except that the prompt now lives
// in a file. %[1]s is the AGENTS.md path, %[2]s the pattern for pergent's temp
// directories, %[3]s the tool-output pattern, %[4]s the prompt file.
const goldenCIConfig = `{
  "$schema": "https://opencode.ai/config.json",
  "model": "anthropic/claude-sonnet-4",
  "default_agent": "pergent",
  "instructions": [
    "%s"
  ],
  "provider": {
    "anthropic": {
      "options": {
        "apiKey": "{env:OPENCODE_API_KEY}"
      },
      "models": {
        "claude-sonnet-4": {}
      }
    }
  },
  "agent": {
    "pergent": {
      "description": "pergent review agent",
      "mode": "primary",
      "model": "anthropic/claude-sonnet-4",
      "steps": 20,
      "prompt": "{file:%[4]s}"
    }
  },
  "permission": {
    "bash": "deny",
    "doom_loop": "allow",
    "edit": "deny",
    "external_directory": {
      "*": "deny",
      "%[3]s": "allow",
      "%[2]s": "allow"
    },
    "glob": "allow",
    "grep": "allow",
    "list": "allow",
    "read": {
      "*": "allow",
      "*.env": "deny",
      "*.env.*": "deny",
      "*.env.example": "allow"
    },
    "skill": "deny",
    "task": "deny",
    "webfetch": "deny",
    "websearch": "deny"
  },
  "compaction": {
    "auto": false
  },
  "lsp": false
}`

func TestGenerate_Unchanged(t *testing.T) {
	t.Setenv("OPENCODE_PROVIDER", "anthropic")
	t.Setenv("OPENCODE_MODEL", "claude-sonnet-4")

	repo := t.TempDir()
	agentsPath := filepath.Join(repo, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte("be nice"), 0644); err != nil {
		t.Fatal(err)
	}

	configPath, cleanup, err := GenerateOpenCodeConfig(20, 0, "test prompt", repo)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	toolOutput := filepath.Join(os.TempDir(), DataDirPrefix+"*", "opencode", "tool-output", "*")
	promptPath := filepath.Join(filepath.Dir(configPath), "agent-prompt.md")
	want := fmt.Sprintf(goldenCIConfig, agentsPath, filepath.Join(os.TempDir(), "pergent-*"), toolOutput, promptPath)
	if string(got) != want {
		t.Errorf("CI config changed.\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestReadDenied(t *testing.T) {
	tests := []struct {
		path       string
		deny, want bool
	}{
		{"app/main.go", true, false},
		{".env", false, true},
		{"cfg/prod.env", false, true},
		{"cfg/.env.local", false, true},
		{"cfg/.env.example", false, false},
		{"cfg/app.env.example", false, false},
		{"cfg/app.env.example.bak", false, true},
		{"keys/id.pem", false, false},
		{"keys/id.pem", true, true},
		{"keys/id.key", true, true},
		{"infra/prod.tfvars", true, true},
		{"infra/prod.tfvars", false, false},
		{"docs/pem.md", true, false},
	}
	for _, tt := range tests {
		if got := ReadDenied(tt.path, tt.deny); got != tt.want {
			t.Errorf("ReadDenied(%q, %v) = %v, want %v", tt.path, tt.deny, got, tt.want)
		}
	}
}

// ReadRules is the one source of the read patterns: the generated config
// carries exactly it.
func TestReadRules_AreTheGeneratedConfigsRules(t *testing.T) {
	t.Setenv("OPENCODE_PROVIDER", "anthropic")
	t.Setenv("OPENCODE_MODEL", "m")
	for _, deny := range []bool{false, true} {
		path, cleanup, err := GenerateOpenCodeConfigWith(OpenCodeOptions{Prompt: "p", DenySecretFiles: deny})
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		cleanup()
		if err != nil {
			t.Fatal(err)
		}
		var cfg struct {
			Permission struct {
				Read map[string]string `json:"read"`
			} `json:"permission"`
		}
		if err := json.Unmarshal(data, &cfg); err != nil {
			t.Fatal(err)
		}
		want := ReadRules(deny)
		if len(cfg.Permission.Read) != len(want) {
			t.Errorf("deny=%v: config has %d read rules, ReadRules has %d", deny, len(cfg.Permission.Read), len(want))
		}
		for k, v := range want {
			if cfg.Permission.Read[k] != v {
				t.Errorf("deny=%v: config read[%q] = %q, ReadRules says %q", deny, k, cfg.Permission.Read[k], v)
			}
		}
	}
}

// GeneratesConfig is true exactly when GenerateOpenCodeConfigWith writes a file.
func TestGeneratesConfig(t *testing.T) {
	for _, provider := range []string{"", "anthropic"} {
		t.Setenv("OPENCODE_PROVIDER", provider)
		t.Setenv("OPENCODE_MODEL", "m")
		path, cleanup, err := GenerateOpenCodeConfigWith(OpenCodeOptions{Prompt: "p"})
		if err != nil {
			t.Fatal(err)
		}
		cleanup()
		if got := GeneratesConfig(); got != (path != "") || got != (provider != "") {
			t.Errorf("provider %q: GeneratesConfig() = %v, config path %q", provider, got, path)
		}
	}
}

// wantNoLinkedConfig is the generated config of OpenCodeOptions{MaxTurns: 20}
// as it was before linked repositories existed, with %[1]s standing for
// os.TempDir(). A review with no linked repository must still get exactly it.
const wantNoLinkedConfig = `{
  "$schema": "https://opencode.ai/config.json",
  "model": "anthropic/claude-sonnet-4",
  "default_agent": "pergent",
  "provider": {
    "anthropic": {
      "options": {
        "apiKey": "{env:OPENCODE_API_KEY}"
      },
      "models": {
        "claude-sonnet-4": {}
      }
    }
  },
  "agent": {
    "pergent": {
      "description": "pergent review agent",
      "mode": "primary",
      "model": "anthropic/claude-sonnet-4",
      "steps": 20
    }
  },
  "permission": {
    "bash": "deny",
    "doom_loop": "allow",
    "edit": "deny",
    "external_directory": {
      "*": "deny",
      "%[1]s/opencode-data-pergent-*/opencode/tool-output/*": "allow",
      "%[1]s/pergent-*": "allow"
    },
    "glob": "allow",
    "grep": "allow",
    "list": "allow",
    "read": {
      "*": "allow",
      "*.env": "deny",
      "*.env.*": "deny",
      "*.env.example": "allow"
    },
    "skill": "deny",
    "task": "deny",
    "webfetch": "deny",
    "websearch": "deny"
  },
  "compaction": {
    "auto": false
  },
  "lsp": false
}`

func TestExternalDirectoryOnlyForLinked(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	externalDirectory := func(t *testing.T, data []byte) map[string]string {
		t.Helper()
		var cfg struct {
			Permission struct {
				External map[string]string `json:"external_directory"`
			} `json:"permission"`
		}
		if err := json.Unmarshal(data, &cfg); err != nil {
			t.Fatal(err)
		}
		return cfg.Permission.External
	}

	t.Run("without linked paths the config is byte-identical to before", func(t *testing.T) {
		want := fmt.Sprintf(wantNoLinkedConfig, os.TempDir())
		for name, o := range map[string]OpenCodeOptions{
			"nil":   {MaxTurns: 20},
			"empty": {MaxTurns: 20, Linked: []string{}},
		} {
			if got := string(generateWith(t, o)); got != want {
				t.Errorf("%s: config changed:\n%s\nwant:\n%s", name, got, want)
			}
		}
	})

	t.Run("linked paths are allowed and nothing else is", func(t *testing.T) {
		base := externalDirectory(t, generateWith(t, OpenCodeOptions{MaxTurns: 20}))
		got := externalDirectory(t, generateWith(t, OpenCodeOptions{
			MaxTurns: 20,
			Linked:   []string{"/work/linked-1", "/work/linked-2/"},
		}))
		want := map[string]string{
			"/work/linked-1":   "allow",
			"/work/linked-1/*": "allow",
			"/work/linked-2":   "allow",
			"/work/linked-2/*": "allow",
		}
		for k, v := range base {
			want[k] = v
		}
		if len(got) != len(want) {
			t.Errorf("external_directory = %v, want %v", got, want)
		}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("external_directory[%q] = %q, want %q (got %v)", k, got[k], v, got)
			}
		}
		if got["*"] != "deny" {
			t.Errorf(`external_directory["*"] = %q, want deny`, got["*"])
		}
	})

	t.Run("read rules and the rest of the config are untouched by linked paths", func(t *testing.T) {
		var a, b map[string]any
		if err := json.Unmarshal(generateWith(t, OpenCodeOptions{MaxTurns: 20, DenySecretFiles: true}), &a); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(generateWith(t, OpenCodeOptions{MaxTurns: 20, DenySecretFiles: true, Linked: []string{"/work/linked-1"}}), &b); err != nil {
			t.Fatal(err)
		}
		delete(a["permission"].(map[string]any), "external_directory")
		delete(b["permission"].(map[string]any), "external_directory")
		ab, _ := json.Marshal(a)
		bb, _ := json.Marshal(b)
		if !bytes.Equal(ab, bb) {
			t.Errorf("linked paths changed more than external_directory:\n%s\n%s", ab, bb)
		}
	})
}

// A linked path that could allow more than its own clone gets no allow.
func TestLinkedAllowsRefusesWidePaths(t *testing.T) {
	for _, dir := range []string{"", "/", "relative/linked-1", "/work/*", "/work/linked-?", "/work/{a,b}", "/work/[a]"} {
		if got := linkedAllows(dir); len(got) != 0 {
			t.Errorf("linkedAllows(%q) = %v, want none", dir, got)
		}
	}
	if got := linkedAllows("/work/linked-1/"); len(got) != 2 || got[0] != "/work/linked-1" || got[1] != "/work/linked-1/*" {
		t.Errorf("linkedAllows(/work/linked-1/) = %v", got)
	}
}
