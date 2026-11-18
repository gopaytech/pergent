package runner

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
