package config

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// The tests here set the model through the environment, so none of them runs
// in parallel.

const (
	scanOut    = "/work/scan-out"
	scanSkills = "/work/skills"
)

func scanOptions(tools ...string) ScanOptions {
	return ScanOptions{
		MaxSteps:      600,
		SubagentSteps: 100,
		Prompt:        "scan the repository",
		Tools:         tools,
		OutputDir:     scanOut,
		SkillDir:      scanSkills,
	}
}

func setScanModel(t *testing.T) {
	t.Helper()
	t.Setenv("OPENCODE_PROVIDER", "anthropic")
	t.Setenv("OPENCODE_MODEL", "claude-sonnet-4")
	t.Setenv("OPENCODE_BASE_URL", "")
	t.Setenv("OPENCODE_NPM", "")
}

// scanConfig writes a scan config for o and returns it decoded.
func scanConfig(t *testing.T, o ScanOptions) map[string]any {
	t.Helper()
	setScanModel(t)
	path, cleanup, err := GenerateScanConfig(o)
	if err != nil {
		t.Fatalf("GenerateScanConfig() error: %v", err)
	}
	t.Cleanup(cleanup)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func scanAgents(t *testing.T, cfg map[string]any) map[string]map[string]any {
	t.Helper()
	raw, _ := cfg["agent"].(map[string]any)
	agents := map[string]map[string]any{}
	for name, a := range raw {
		m, ok := a.(map[string]any)
		if !ok {
			t.Fatalf("agent %q is %T", name, a)
		}
		agents[name] = m
	}
	return agents
}

func agentPermission(t *testing.T, agent map[string]any) map[string]any {
	t.Helper()
	p, ok := agent["permission"].(map[string]any)
	if !ok {
		t.Fatalf("agent has no permission block: %v", agent)
	}
	return p
}

func TestScanConfigAgents(t *testing.T) {
	cfg := scanConfig(t, scanOptions("bash", "write", "task"))
	agents := scanAgents(t, cfg)
	var names []string
	for name := range agents {
		names = append(names, name)
	}
	sort.Strings(names)
	if want := []string{"explore", "general", "pergent"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("agents = %v, want %v", names, want)
	}
	if cfg["default_agent"] != AgentName {
		t.Errorf("default_agent = %v, want %q", cfg["default_agent"], AgentName)
	}
	steps := map[string]float64{"pergent": 600, "general": 100, "explore": 100}
	modes := map[string]string{"pergent": "primary", "general": "subagent", "explore": "subagent"}
	for name, a := range agents {
		if a["steps"] != steps[name] {
			t.Errorf("%s.steps = %v, want %v", name, a["steps"], steps[name])
		}
		if a["mode"] != modes[name] {
			t.Errorf("%s.mode = %v, want %v", name, a["mode"], modes[name])
		}
		if a["model"] != cfg["model"] || a["model"] != "anthropic/claude-sonnet-4" {
			t.Errorf("%s.model = %v, want the scan model %v", name, a["model"], cfg["model"])
		}
	}
	// An empty description would replace opencode's own one, which the main
	// agent reads to pick a sub-agent.
	for _, name := range ScanSubagents {
		if _, ok := agents[name]["description"]; ok {
			t.Errorf("%s has a description; opencode's own one must stay", name)
		}
	}
	// A formatter runs a program after each edit, with the model key in its
	// environment.
	for _, key := range []string{"lsp", "snapshot", "formatter"} {
		if cfg[key] != false {
			t.Errorf("%s = %v, want false", key, cfg[key])
		}
	}
	if c, _ := cfg["compaction"].(map[string]any); c["auto"] != false {
		t.Errorf("compaction = %v, want auto false", cfg["compaction"])
	}
	prompt, _ := agents[AgentName]["prompt"].(string)
	if !strings.HasPrefix(prompt, "{file:") {
		t.Errorf("pergent.prompt = %q, want a {file:} token", prompt)
	}
}

// Every agent opencode may start for the scan carries the whole block, so
// opencode's own explore rules (shell, webfetch, websearch, every external
// directory) never apply.
func TestScanConfigSubagentsGetTheBlock(t *testing.T) {
	agents := scanAgents(t, scanConfig(t, scanOptions("bash", "write", "task")))
	without := func(p map[string]any) map[string]any {
		c := map[string]any{}
		for k, v := range p {
			if k != "subagent" {
				c[k] = v
			}
		}
		return c
	}
	main := agentPermission(t, agents[AgentName])
	for _, name := range ScanSubagents {
		p := agentPermission(t, agents[name])
		if !reflect.DeepEqual(without(p), without(main)) {
			t.Errorf("%s.permission = %v, want pergent's block %v", name, p, main)
		}
		if p["subagent"] != "deny" {
			t.Errorf(`%s.permission.subagent = %v, want "deny"`, name, p["subagent"])
		}
	}
	for name, a := range agents {
		p := agentPermission(t, a)
		if p["*"] != "deny" {
			t.Errorf(`%s.permission["*"] = %v, want "deny"`, name, p["*"])
		}
		for _, key := range []string{"webfetch", "websearch"} {
			if _, ok := p[key]; ok {
				t.Errorf("%s.permission has %q: %v", name, key, p[key])
			}
		}
		ext, _ := p["external_directory"].(map[string]any)
		if ext["*"] != "deny" {
			t.Errorf(`%s.permission.external_directory["*"] = %v, want "deny"`, name, ext["*"])
		}
		for _, pattern := range []string{scanSkills, scanSkills + "/*"} {
			if ext[pattern] != "allow" {
				t.Errorf("%s.permission.external_directory[%q] = %v, want allow", name, pattern, ext[pattern])
			}
		}
	}
	want := map[string]any{"*": "deny", "general": "allow", "explore": "allow"}
	if !reflect.DeepEqual(main["subagent"], want) {
		t.Errorf("pergent.permission.subagent = %v, want %v", main["subagent"], want)
	}
	if main["shell"] != "allow" {
		t.Errorf(`pergent.permission.shell = %v, want "allow"`, main["shell"])
	}
}

// The top-level block is appended to every agent, opencode's hidden ones too,
// so it grants no tool beyond the review's read-only ones.
func TestScanConfigTopLevelIsReadOnly(t *testing.T) {
	cfg := scanConfig(t, scanOptions("bash", "write", "task"))
	top, ok := cfg["permission"].(map[string]any)
	if !ok {
		t.Fatalf("no top-level permission: %v", cfg["permission"])
	}
	for _, key := range []string{"shell", "edit", "subagent"} {
		if _, ok := top[key]; ok {
			t.Errorf("top-level permission has %q: %v", key, top[key])
		}
	}
	data, err := json.Marshal(reviewPermission(true, nil))
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]any
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(top, want) {
		t.Errorf("top-level permission = %v, want the review block %v", top, want)
	}
}

func TestScanConfigEditOnlyOutput(t *testing.T) {
	agents := scanAgents(t, scanConfig(t, scanOptions("bash", "write", "task")))
	want := map[string]any{"*": "deny", scanOut + "/*": "allow"}
	for name, a := range agents {
		if got := agentPermission(t, a)["edit"]; !reflect.DeepEqual(got, want) {
			t.Errorf("%s.permission.edit = %v, want %v", name, got, want)
		}
	}
}

func TestScanConfigNoTask(t *testing.T) {
	cfg := scanConfig(t, scanOptions("bash"))
	agents := scanAgents(t, cfg)
	if len(agents) != 1 || agents[AgentName] == nil {
		t.Fatalf("agents = %v, want only %q", agents, AgentName)
	}
	p := agentPermission(t, agents[AgentName])
	for _, key := range []string{"subagent", "edit"} {
		if _, ok := p[key]; ok {
			t.Errorf("pergent.permission has %q without its tool: %v", key, p[key])
		}
	}
	if p["shell"] != "allow" {
		t.Errorf(`pergent.permission.shell = %v, want "allow"`, p["shell"])
	}

	// With no tool at all, the agent gets the read-only review block.
	agents = scanAgents(t, scanConfig(t, scanOptions()))
	p = agentPermission(t, agents[AgentName])
	for _, key := range []string{"shell", "edit", "subagent"} {
		if _, ok := p[key]; ok {
			t.Errorf("no tools: pergent.permission has %q: %v", key, p[key])
		}
	}
}

// opencode 2 calls the tools shell, edit and subagent. The manifest's v1
// names must never reach the config, and opencode's experimental options stay
// off.
func TestScanConfigActionNames(t *testing.T) {
	cfg := scanConfig(t, scanOptions("bash", "write", "task"))
	if _, ok := cfg["experimental"]; ok {
		t.Errorf("config has an experimental key: %v", cfg["experimental"])
	}
	var walk func(where string, v any)
	walk = func(where string, v any) {
		switch v := v.(type) {
		case map[string]any:
			for k, child := range v {
				switch k {
				case "bash", "write", "task", "experimental":
					t.Errorf("key %q at %s", k, where)
				}
				walk(where+"."+k, child)
			}
		case []any:
			for _, child := range v {
				walk(where+"[]", child)
			}
		}
	}
	walk("config", cfg)
}

func TestScanConfigRefusesBadDirs(t *testing.T) {
	setScanModel(t)
	bad := []string{"", "relative/out", "/", "/work/*", "/work/out-?", "/work/{env:OPENCODE_API_KEY}"}
	for _, dir := range bad {
		for _, field := range []string{"OutputDir", "SkillDir"} {
			o := scanOptions("bash", "write", "task")
			if field == "OutputDir" {
				o.OutputDir = dir
			} else {
				o.SkillDir = dir
			}
			if path, cleanup, err := GenerateScanConfig(o); err == nil {
				cleanup()
				t.Errorf("%s %q: got config %s, want an error", field, dir, path)
			}
		}
	}

	o := scanOptions("bash", "fetch")
	if path, cleanup, err := GenerateScanConfig(o); err == nil {
		cleanup()
		t.Errorf("unknown tool: got config %s, want an error", path)
	}

	// The review writes no config without a provider and lets opencode use
	// its own; a scan must never run on opencode's own permissions.
	t.Setenv("OPENCODE_PROVIDER", "")
	if path, cleanup, err := GenerateScanConfig(scanOptions("bash")); err == nil {
		cleanup()
		t.Errorf("no provider: got config %q, want an error", path)
	}
	t.Setenv("OPENCODE_PROVIDER", "anthropic")
	t.Setenv("OPENCODE_MODEL", "")
	if path, cleanup, err := GenerateScanConfig(scanOptions("bash")); err == nil {
		cleanup()
		t.Errorf("no model: got config %q, want an error", path)
	}
}
