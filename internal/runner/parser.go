package runner

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
)

// NDJSON event types from `opencode run --format json` (opencode 1.19.x).
// Each line is {"type", "timestamp", "sessionID", ...}; the event types are
// step_start, step_finish, text, reasoning, tool_use (completed or failed tool
// calls only) and error.
type openCodeEvent struct {
	Type  string         `json:"type"`
	Part  *openCodePart  `json:"part,omitempty"`
	Error *openCodeError `json:"error,omitempty"`
}

type openCodePart struct {
	Type   string          `json:"type"`
	Text   string          `json:"text"`
	Tool   string          `json:"tool"`
	Reason string          `json:"reason"`
	State  *openCodeState  `json:"state,omitempty"`
	Tokens *openCodeTokens `json:"tokens,omitempty"`
	// Synthetic marks text opencode wrote itself rather than the model, e.g.
	// the "Continue if you have next steps..." message it injects as a user
	// message after compacting the context.
	Synthetic bool           `json:"synthetic,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type openCodeState struct {
	Status string         `json:"status"`
	Input  map[string]any `json:"input,omitempty"`
	Error  string         `json:"error,omitempty"`
}

type openCodeTokens struct {
	Total     int `json:"total"`
	Input     int `json:"input"`
	Output    int `json:"output"`
	Reasoning int `json:"reasoning"`
	Cache     struct {
		Read  int `json:"read"`
		Write int `json:"write"`
	} `json:"cache"`
}

// used returns the tokens the step occupied in the context window, counted the
// same way opencode does for its overflow check.
func (t openCodeTokens) used() int {
	if t.Total > 0 {
		return t.Total
	}
	return t.Input + t.Output + t.Cache.Read + t.Cache.Write
}

type openCodeError struct {
	Name string           `json:"name"`
	Data *openCodeErrData `json:"data,omitempty"`
}

type openCodeErrData struct {
	Message     string `json:"message"`
	StatusCode  int    `json:"statusCode,omitempty"`
	IsRetryable bool   `json:"isRetryable,omitempty"`
}

// EventError is an error event reported by opencode (a session.error).
type EventError struct {
	Name        string
	Message     string
	StatusCode  int
	IsRetryable bool
}

// Usage is the tokens a run consumed: the sum of the input and output tokens
// of every finished step (cache tokens and totals are not counted).
type Usage struct {
	Input, Output int
}

// Add returns u plus o.
func (u Usage) Add(o Usage) Usage {
	return Usage{Input: u.Input + o.Input, Output: u.Output + o.Output}
}

// Transcript is what pergent can observe about one opencode run.
type Transcript struct {
	// Answer is the model's final answer: the text of the last step, when that
	// step finished on its own (not to call a tool) and nothing followed it.
	// Empty when the run produced no answer.
	Answer string
	// Steps counts the model steps started.
	Steps int
	// LastStepTokens is the context size reported by the last finished step.
	LastStepTokens int
	// LastFinishReason is the finish reason of the last finished step
	// ("stop", "tool-calls", "length", ...).
	LastFinishReason string
	// InStep is true when the stream ended between step_start and step_finish.
	InStep bool
	// Compacted is true when opencode compacted the context during the run.
	Compacted bool
	// CompactedAtTokens and CompactedAtStep describe the step that filled the
	// context, i.e. the one before opencode's summary step.
	CompactedAtTokens int
	CompactedAtStep   int
	// LastTool and LastToolTarget describe the last tool call opencode
	// reported, e.g. "read" and a file path.
	LastTool       string
	LastToolTarget string
	// Errors are opencode's error events, in order.
	Errors []EventError
	// Usage sums part.tokens.input and part.tokens.output over all
	// step_finish events.
	Usage Usage
}

// LastError returns the last error event, or nil.
func (t Transcript) LastError() *EventError {
	if len(t.Errors) == 0 {
		return nil
	}
	return &t.Errors[len(t.Errors)-1]
}

// RootError returns the error event that best explains the failure: a context
// overflow if there is one, otherwise the first error. opencode can follow
// the real error with a generic one ("Unexpected server error").
func (t Transcript) RootError() *EventError {
	for i := range t.Errors {
		if t.Errors[i].Name == "ContextOverflowError" {
			return &t.Errors[i]
		}
	}
	if len(t.Errors) == 0 {
		return nil
	}
	return &t.Errors[0]
}

// ParseOutput reads opencode's NDJSON stream.
//
// Only assistant text counts as an answer. opencode wraps every model call in
// step_start/step_finish, so text outside a step was written by someone else
// (the user prompt, or opencode itself), and synthetic text is opencode's own.
// Seeing either means a new turn started, so any earlier answer is dropped.
// Text from a step that ended to call a tool is the model narrating its work
// ("Let me check..."), not an answer.
func ParseOutput(stdout []byte) Transcript {
	var t Transcript
	var stepText []string
	// prevStepTokens is the context size of the step before the last one.
	// When opencode compacts, the last step is its summary, so this is the
	// size that triggered compaction.
	var prevStepTokens int

	scanner := bufio.NewScanner(bytes.NewReader(stdout))
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var event openCodeEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}

		switch event.Type {
		case "step_start":
			t.Steps++
			t.InStep = true
			stepText = nil
			t.Answer = ""

		case "step_finish":
			t.InStep = false
			if event.Part != nil {
				t.LastFinishReason = event.Part.Reason
				if event.Part.Tokens != nil {
					t.Usage.Input += event.Part.Tokens.Input
					t.Usage.Output += event.Part.Tokens.Output
					prevStepTokens = t.LastStepTokens
					t.LastStepTokens = event.Part.Tokens.used()
				}
			}
			if isFinalReason(t.LastFinishReason) {
				t.Answer = stripThinkTags(strings.Join(stepText, "\n\n"))
			} else {
				t.Answer = ""
			}
			stepText = nil

		case "text":
			if event.Part == nil || event.Part.Type != "text" {
				continue
			}
			if event.Part.Synthetic || !t.InStep {
				if event.Part.Synthetic && event.Part.Metadata["compaction_continue"] == true && !t.Compacted {
					t.Compacted = true
					t.CompactedAtTokens = prevStepTokens
					t.CompactedAtStep = max(t.Steps-1, 0)
				}
				t.Answer = ""
				continue
			}
			stepText = append(stepText, event.Part.Text)

		case "tool_use":
			if event.Part == nil {
				continue
			}
			t.LastTool = event.Part.Tool
			t.LastToolTarget = ""
			if event.Part.State != nil {
				t.LastToolTarget = toolTarget(event.Part.State.Input)
			}

		case "error":
			if event.Error == nil {
				continue
			}
			e := EventError{Name: event.Error.Name}
			if event.Error.Data != nil {
				e.Message = event.Error.Data.Message
				e.StatusCode = event.Error.Data.StatusCode
				e.IsRetryable = event.Error.Data.IsRetryable
			}
			t.Errors = append(t.Errors, e)
		}
	}
	return t
}

// isFinalReason reports whether a step that finished with this reason ended
// the model's turn. opencode itself keeps looping on "tool-calls" and
// "unknown"; "length", "content-filter" and "error" mean the answer was cut
// off or blocked.
func isFinalReason(reason string) bool {
	switch reason {
	case "", "tool-calls", "unknown", "length", "content-filter", "error":
		return false
	}
	return true
}

// toolTarget picks the file, directory or pattern a tool call worked on.
func toolTarget(input map[string]any) string {
	for _, key := range []string{"filePath", "path", "pattern"} {
		if v, ok := input[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// stripThinkTags removes leading <think>...</think> blocks from model output.
// Some models (DeepSeek, Qwen) emit their reasoning in these tags before the
// answer. Only blocks at the start are stripped: tags later in the text belong
// to the review itself (e.g. quoting a diff that contains the literal text)
// and are kept. A leading block that is never closed means the model stopped
// mid-reasoning, so there is no answer to return.
func stripThinkTags(s string) string {
	for {
		s = strings.TrimSpace(s)
		rest, ok := strings.CutPrefix(s, "<think>")
		if !ok {
			return s
		}
		_, after, closed := strings.Cut(rest, "</think>")
		if !closed {
			return ""
		}
		s = after
	}
}
