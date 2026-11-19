package runner

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
)

// maxOpened caps Transcript.Opened.
const maxOpened = 500

// maxOutputLine is the longest line of opencode's output pergent parses.
const maxOutputLine = 16 * 1024 * 1024

// NDJSON event types from `opencode run --format json` (opencode 2.x).
// Each line is {"type", "timestamp", "sessionID", ...}; the event types are
// step_start, step_finish, text, reasoning, tool_use (completed or failed tool
// calls only) and error.
type openCodeEvent struct {
	Type string        `json:"type"`
	Part *openCodePart `json:"part,omitempty"`
	// Error is decoded apart (readError), so a badly typed error value can't
	// hide that the line is an error event.
	Error json.RawMessage `json:"error,omitempty"`
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
	// Output is kept raw: a tool that sent a non-string value must not make
	// the whole event fail to decode.
	Output json.RawMessage `json:"output,omitempty"`
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

// unreadableError names an error event pergent could not read. Its name is
// fixed text: nothing of the line, which may hold provider text, is kept.
const unreadableError = "unreadable"

// readError decodes the "error" value of an error event: opencode 2's
// {type, message, status, retryAfterMs, response: {body}}. The 1.x shape
// ({name, data}) is not read. A field of the wrong type is left out; an
// error that is not an object at all is "unreadable", never dropped.
func readError(raw json.RawMessage) EventError {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return EventError{Name: unreadableError}
	}
	var e EventError
	json.Unmarshal(fields["type"], &e.Name)
	json.Unmarshal(fields["message"], &e.Message)
	json.Unmarshal(fields["status"], &e.StatusCode)
	var resp struct {
		Body json.RawMessage `json:"body"`
	}
	if json.Unmarshal(fields["response"], &resp) == nil {
		e.Body = bodyText(resp.Body)
	}
	if e.Name == "" && e.Message == "" && e.StatusCode == 0 && e.Body == "" {
		e.Name = unreadableError
	}
	return e
}

// EventError is an error event reported by opencode (a session.error).
type EventError struct {
	// Name is the error's type, e.g. "provider.internal" or "unknown".
	Name       string
	Message    string
	StatusCode int
	// Body is the provider's response body. It is read only to classify the
	// error (a context overflow can say so only there) and is never printed
	// or logged: it is provider text.
	Body string
}

// Usage is the tokens a run consumed, summed over every finished step.
// Input and Output are what opencode reports as input and output; Reasoning,
// CacheRead and CacheWrite are reported apart and never added to them.
type Usage struct {
	Input, Output                    int
	Reasoning, CacheRead, CacheWrite int
}

// Add returns u plus o.
func (u Usage) Add(o Usage) Usage {
	return Usage{
		Input: u.Input + o.Input, Output: u.Output + o.Output,
		Reasoning: u.Reasoning + o.Reasoning, CacheRead: u.CacheRead + o.CacheRead, CacheWrite: u.CacheWrite + o.CacheWrite,
	}
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
	// Opened are the paths that a completed read, grep or glob call worked on
	// (its path argument, or filePath), in order, at most maxOpened. opencode
	// 2 has no list tool: read lists a directory.
	// A call opencode refused (a denied read) ended in the error state and is
	// not counted. A call with no path argument is not counted either: it
	// worked on the working directory.
	Opened []string
	// Errors are opencode's error events, in order.
	Errors []EventError
	// ReadErr is why the parse stopped before the end of the output (a line
	// over maxOutputLine), empty when all of it was read. The output went on
	// past that point, so nothing in the transcript is trusted as an answer.
	ReadErr string
	// Usage sums part.tokens.input, output, reasoning and cache.read/write
	// over all step_finish events.
	Usage Usage
	// PerStep has one entry per step_start, in order: the step's tokens and
	// the tool calls reported while it was the last step started. At most
	// maxToolCalls calls are kept per attempt.
	PerStep []StepUsage
	// ToolTotals counts every tool call, kept in PerStep or not.
	ToolTotals ToolTotals
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
		if t.Errors[i].ContextOverflow() {
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
	// keptCalls counts the tool calls stored in PerStep.
	var keptCalls int

	scanner := bufio.NewScanner(bytes.NewReader(stdout))
	scanner.Buffer(make([]byte, 0, 64*1024), maxOutputLine)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var event openCodeEvent
		if err := json.Unmarshal(line, &event); err != nil {
			// A line that is not an event is skipped, but an error event
			// that can't be read still fails the run.
			var probe struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(line, &probe) == nil && probe.Type == "error" {
				t.Errors = append(t.Errors, EventError{Name: unreadableError})
			}
			continue
		}

		switch event.Type {
		case "step_start":
			t.Steps++
			t.PerStep = append(t.PerStep, StepUsage{})
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
					t.Usage.Reasoning += event.Part.Tokens.Reasoning
					t.Usage.CacheRead += event.Part.Tokens.Cache.Read
					t.Usage.CacheWrite += event.Part.Tokens.Cache.Write
					if n := len(t.PerStep); n > 0 {
						t.PerStep[n-1].Usage = Usage{
							Input: event.Part.Tokens.Input, Output: event.Part.Tokens.Output,
							Reasoning: event.Part.Tokens.Reasoning,
							CacheRead: event.Part.Tokens.Cache.Read, CacheWrite: event.Part.Tokens.Cache.Write,
						}
					}
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
			call := ToolCall{Tool: toolName(event.Part.Tool)}
			if st := event.Part.State; st != nil {
				switch st.Status {
				case "completed":
					call.Bytes = outputBytes(st.Output)
				case "error":
					call.Bytes = len(st.Error)
					call.Failed = true
				}
				if call.Tool != "other" {
					call.path = openedPath(st.Input)
				}
			}
			t.ToolTotals.add(call)
			if n := len(t.PerStep); n > 0 && keptCalls < maxToolCalls {
				t.PerStep[n-1].Tools = append(t.PerStep[n-1].Tools, call)
				keptCalls++
			}
			if event.Part.State != nil {
				t.LastToolTarget = toolTarget(event.Part.State.Input)
				if event.Part.State.Status == "completed" && len(t.Opened) < maxOpened {
					switch event.Part.Tool {
					case "read", "grep", "glob":
						if p := openedPath(event.Part.State.Input); p != "" {
							t.Opened = append(t.Opened, p)
						}
					}
				}
			}

		case "error":
			t.Errors = append(t.Errors, readError(event.Error))
		}
	}
	if err := scanner.Err(); err != nil {
		// The scanner stopped, but the stream went on. An answer found before
		// that point may be followed by more steps, errors or a new turn.
		t.ReadErr = err.Error()
		t.Answer = ""
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

// openedPath is the file or directory a read, grep or glob call was
// pointed at: its path or filePath argument, not its pattern.
func openedPath(input map[string]any) string {
	for _, key := range []string{"filePath", "path"} {
		if v, ok := input[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
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

// bodyText is a response body as text: a JSON string is unquoted, anything
// else is kept as it is.
func bodyText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

// outputBytes is the length of a tool result: the decoded string when it is a
// JSON string, else the length of the raw value.
func outputBytes(raw json.RawMessage) int {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return len(s)
	}
	return len(raw)
}
