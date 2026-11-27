package config

import (
	"fmt"
	"path/filepath"
	"slices"
)

// ScanSubagents are the sub-agents a scan's main agent may start when the
// manifest asks for "task". They are opencode's two built-in sub-agents; the
// config gives each one the scan's permission block, so opencode's own rules
// for them (explore allows shell, webfetch, websearch and every external
// directory) never apply.
var ScanSubagents = []string{"general", "explore"}

// ScanOptions configures GenerateScanConfig.
type ScanOptions struct {
	// MaxSteps is the main agent's step limit; SubagentSteps is the limit of
	// each sub-agent.
	MaxSteps, SubagentSteps int
	// ContextLimit overrides the model's context window when > 0.
	ContextLimit int
	// Prompt is the main agent's system prompt.
	Prompt string
	// Tools are the manifest's tools: "bash", "write" and "task". Each one
	// turns on its opencode 2 action (shell, edit, subagent).
	Tools []string
	// OutputDir is the only directory the agents may edit in. It must be
	// absolute and outside the checkout: opencode names a file inside the
	// project by its relative path, which the edit rule does not match.
	OutputDir string
	// SkillDir is the skill's directory, which the agents may read.
	SkillDir string
}

// scanTools are the manifest's tool names. The config names the actions
// opencode 2 checks, never these.
var scanTools = []string{"bash", "write", "task"}

// GenerateScanConfig writes a temporary opencode.json for a security scan.
// Unlike a review, a scan always needs a model from the environment: without
// a config, opencode would run with its own permissions.
//
// Every agent gets the review's read-only block. The main agent and the
// sub-agents add only what the manifest's tools ask for:
//   - bash: shell.
//   - write: edit, inside OutputDir only.
//   - task: the main agent may start general and explore, and nothing else.
//     The sub-agents may start none.
//
// The top-level block stays the review's read-only one. opencode 2 appends it
// to every agent, its hidden ones (compaction, title, summary) too, so no
// tool reaches an agent the scan does not name.
func GenerateScanConfig(o ScanOptions) (path string, cleanup func(), err error) {
	m := EnvModel()
	if m.Provider == "" {
		return "", nil, fmt.Errorf("OPENCODE_PROVIDER is required for a scan")
	}
	if m.Model == "" {
		return "", nil, fmt.Errorf("OPENCODE_MODEL is required for a scan")
	}
	for _, tool := range o.Tools {
		if !slices.Contains(scanTools, tool) {
			return "", nil, fmt.Errorf("unknown scan tool %q", tool)
		}
	}
	outputDir, ok := safeDir(o.OutputDir)
	if !ok {
		return "", nil, fmt.Errorf("scan output directory %q must be absolute, not the root, and have no wildcard or brace", o.OutputDir)
	}
	skillAllows := linkedAllows(o.SkillDir)
	if skillAllows == nil {
		return "", nil, fmt.Errorf("scan skill directory %q must be absolute, not the root, and have no wildcard or brace", o.SkillDir)
	}

	has := func(tool string) bool { return slices.Contains(o.Tools, tool) }
	// block builds a new permission map on each call, so no two agents
	// share one.
	block := func(subagent any) map[string]any {
		p := reviewPermission(true, nil)
		ext := p["external_directory"].(map[string]string)
		for _, pattern := range skillAllows {
			ext[pattern] = "allow"
		}
		if has("bash") {
			p["shell"] = "allow"
		}
		if has("write") {
			// "*" sorts before "/", so encoding/json writes the deny first
			// and the allow wins for the output directory.
			p["edit"] = map[string]string{"*": "deny", filepath.Join(outputDir, "*"): "allow"}
		}
		if subagent != nil {
			p["subagent"] = subagent
		}
		return p
	}

	fullModel, providers := providerConfig(m, o.ContextLimit)
	var mainSubagent any
	if has("task") {
		allow := map[string]string{"*": "deny"}
		for _, name := range ScanSubagents {
			allow[name] = "allow"
		}
		mainSubagent = allow
	}
	agents := map[string]openCodeAgent{
		AgentName: {
			Description: "pergent security scan agent",
			Mode:        "primary",
			Model:       fullModel,
			Steps:       o.MaxSteps,
			Permission:  block(mainSubagent),
		},
	}
	if has("task") {
		for _, name := range ScanSubagents {
			// No description: opencode's own one stays, and the main agent
			// reads it to pick a sub-agent.
			agents[name] = openCodeAgent{
				Mode:       "subagent",
				Model:      fullModel,
				Steps:      o.SubagentSteps,
				Permission: block("deny"),
			}
		}
	}

	cfg := openCodeConfig{
		Schema:       "https://opencode.ai/config.json",
		Model:        fullModel,
		DefaultAgent: AgentName,
		Provider:     providers,
		Agent:        agents,
		Permission:   reviewPermission(true, nil),
		// The same reasons as for a review: compaction can end a run with no
		// result, language servers can be programs from the checkout, and
		// snapshots write to its git object store.
		Compaction: openCodeCompaction{Auto: false},
		LSP:        false,
		Snapshot:   false,
	}
	return writeOpenCodeConfig(cfg, o.Prompt)
}
