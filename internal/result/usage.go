package result

import (
	"encoding/json"

	"github.com/gopaytech/pergent/internal/redact"
	"github.com/gopaytech/pergent/internal/review"
	"github.com/gopaytech/pergent/internal/runner"
)

// Run is what one opencode run of a skill used. It holds counts, pergent's own
// labels and file names, and tool classes: no paths and no model text.
type Run struct {
	Run         string       `json:"run"`
	PromptBytes int          `json:"prompt_bytes"`
	Attachments []Attachment `json:"attachments"`
	Tokens      Tokens       `json:"tokens"`
	ToolTotals  ToolTotals   `json:"tool_totals"`
	Steps       []Step       `json:"steps"`
}

// Attachment is one file attached to a run.
type Attachment struct {
	Name  string `json:"name"`
	Bytes int    `json:"bytes"`
}

// ToolTotals counts the tool calls of a run, per tool.
type ToolTotals struct {
	Read  ToolUse `json:"read"`
	Grep  ToolUse `json:"grep"`
	Glob  ToolUse `json:"glob"`
	List  ToolUse `json:"list"`
	Other ToolUse `json:"other"`
}

// ToolUse is the number of calls of one tool and the bytes they returned.
type ToolUse struct {
	Calls int `json:"calls"`
	Bytes int `json:"bytes"`
}

// Step is one model step of a run.
type Step struct {
	Attempt int        `json:"attempt"`
	Tokens  Tokens     `json:"tokens"`
	Tools   []ToolCall `json:"tools"`
}

// ToolCall is one tool call. Target is a class, never a path.
type ToolCall struct {
	Tool   string `json:"tool"`
	Target string `json:"target,omitempty"`
	Bytes  int    `json:"bytes"`
	Failed bool   `json:"failed,omitempty"`
}

// UsageDoc is the file --usage-file writes.
type UsageDoc struct {
	Skills []SkillUsage `json:"skills"`
}

// SkillUsage is one skill's tokens and runs.
type SkillUsage struct {
	Name   string `json:"name"`
	Tokens Tokens `json:"tokens"`
	Runs   []Run  `json:"runs"`
}

func tokensOf(u runner.Usage) Tokens {
	return Tokens{Input: u.Input, Output: u.Output, Reasoning: u.Reasoning,
		CacheRead: u.CacheRead, CacheWrite: u.CacheWrite}
}

func toolUse(u runner.ToolUse) ToolUse { return ToolUse{Calls: u.Calls, Bytes: u.Bytes} }

// buildRuns converts a skill's runs. It returns nil for no runs, so a skill
// that started none has no runs key in the agent result.
func buildRuns(runs []review.RunUsage) []Run {
	if len(runs) == 0 {
		return nil
	}
	out := make([]Run, len(runs))
	for i, r := range runs {
		run := Run{
			Run:         r.Label,
			PromptBytes: r.PromptBytes,
			Attachments: make([]Attachment, len(r.Attachments)),
			Tokens:      tokensOf(r.Usage),
			ToolTotals: ToolTotals{
				Read: toolUse(r.ToolTotals.Read), Grep: toolUse(r.ToolTotals.Grep),
				Glob: toolUse(r.ToolTotals.Glob), List: toolUse(r.ToolTotals.List),
				Other: toolUse(r.ToolTotals.Other),
			},
			Steps: make([]Step, len(r.Steps)),
		}
		for j, a := range r.Attachments {
			run.Attachments[j] = Attachment{Name: a.Name, Bytes: a.Bytes}
		}
		for j, s := range r.Steps {
			step := Step{Attempt: s.Attempt, Tokens: tokensOf(s.Usage), Tools: make([]ToolCall, len(s.Tools))}
			for k, c := range s.Tools {
				step.Tools[k] = ToolCall{Tool: c.Tool, Target: c.Target, Bytes: c.Bytes, Failed: c.Failed}
			}
			run.Steps[j] = step
		}
		out[i] = run
	}
	return out
}

// EncodeUsage builds UsageDoc from the skills' results and encodes it as
// indented JSON that ends in a newline.
func EncodeUsage(skills []review.SkillResult) ([]byte, error) {
	doc := UsageDoc{Skills: make([]SkillUsage, len(skills))}
	for i, s := range skills {
		runs := buildRuns(s.Runs)
		if runs == nil {
			runs = []Run{}
		}
		doc.Skills[i] = SkillUsage{Name: redact.String(s.Skill), Tokens: tokensOf(s.Usage), Runs: runs}
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
