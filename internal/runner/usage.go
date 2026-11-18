package runner

import (
	"path/filepath"
	"strings"

	"github.com/gopaytech/pergent/internal/config"
)

// Where a read, grep, glob or list call worked.
const (
	TargetRepository = "repository"  // inside Options.RepoPath, or relative, or no path (the working directory)
	TargetPergent    = "pergent"     // inside <temp>/pergent-*: attachments, the on-demand diffs and their index, the prompt
	TargetToolOutput = "tool_output" // inside <temp>/opencode-data-pergent-*/opencode/tool-output: a long tool result opencode saved
	TargetOutside    = "outside"     // anything else, linked clones included
)

// ToolUse counts the calls of one tool and the bytes of their results.
type ToolUse struct{ Calls, Bytes int }

// ToolTotals counts every tool call of a run, per tool.
type ToolTotals struct{ Read, Grep, Glob, List, Other ToolUse }

// Add returns the sum of two totals.
func (t ToolTotals) Add(o ToolTotals) ToolTotals {
	add := func(a, b ToolUse) ToolUse { return ToolUse{a.Calls + b.Calls, a.Bytes + b.Bytes} }
	return ToolTotals{
		Read: add(t.Read, o.Read), Grep: add(t.Grep, o.Grep), Glob: add(t.Glob, o.Glob),
		List: add(t.List, o.List), Other: add(t.Other, o.Other),
	}
}

// ToolCall is one tool call of a step.
type ToolCall struct {
	Tool   string // "read", "grep", "glob", "list" or "other"
	Target string // set in Task 2; "" until then and for "other"
	Bytes  int    // len(state.output) when completed, len(state.error) when failed
	Failed bool   // state.status was "error"
	path   string // filePath or path argument, for Task 2; never exported
}

// StepUsage is one model step: its tokens (zero when it never finished)
// and the tool calls reported while it was the last step started.
type StepUsage struct {
	Attempt int // set by Run (Task 2); 0 from ParseOutput
	Usage   Usage
	Tools   []ToolCall
}

// maxToolCalls is the number of tool calls kept per attempt in PerStep.
const maxToolCalls = 500

// toolName maps a tool name to the fixed set pergent reports.
func toolName(tool string) string {
	switch tool {
	case "read", "grep", "glob", "list":
		return tool
	}
	return "other"
}

// add counts one call.
func (t *ToolTotals) add(c ToolCall) {
	var u *ToolUse
	switch c.Tool {
	case "read":
		u = &t.Read
	case "grep":
		u = &t.Grep
	case "glob":
		u = &t.Glob
	case "list":
		u = &t.List
	default:
		u = &t.Other
	}
	u.Calls++
	u.Bytes += c.Bytes
}

// pathTarget sorts the path a read, grep, glob or list call worked on.
// A relative path is joined to repoPath. The path is cleaned first, and so
// is tempDir. When the repository and a pergent directory both hold the
// path, the deeper one wins, and the repository wins a tie.
func pathTarget(repoPath, tempDir, path string) string {
	if abs, err := filepath.Abs(repoPath); err == nil {
		repoPath = abs
	}
	repoPath = filepath.Clean(repoPath)
	if path == "" {
		return TargetRepository
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(repoPath, path)
	}
	path = filepath.Clean(path)
	tempDir = filepath.Clean(tempDir)

	repoDepth := -1
	if within(repoPath, path) {
		repoDepth = len(repoPath)
	}
	if rel, ok := strings.CutPrefix(path, tempDir+string(filepath.Separator)); ok {
		first, _, _ := strings.Cut(rel, string(filepath.Separator))
		if strings.HasPrefix(first, config.TempPrefix) && len(tempDir)+1+len(first) > repoDepth {
			return TargetPergent
		}
		if strings.HasPrefix(first, config.DataDirPrefix) &&
			within(filepath.Join(tempDir, first, "opencode", "tool-output"), path) &&
			len(tempDir)+1+len(first) > repoDepth {
			return TargetToolOutput
		}
	}
	if repoDepth >= 0 {
		return TargetRepository
	}
	return TargetOutside
}

// within reports whether path is root or inside it. Both are clean.
func within(root, path string) bool {
	return path == root || strings.HasPrefix(path, strings.TrimSuffix(root, string(filepath.Separator))+string(filepath.Separator))
}
