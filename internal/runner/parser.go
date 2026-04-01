package runner

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
)

// NDJSON event types from opencode --format json
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
}

type openCodeState struct {
	Status string         `json:"status"`
	Input  map[string]any `json:"input,omitempty"`
	Error  string         `json:"error,omitempty"`
}

type openCodeTokens struct {
	Total  int `json:"total"`
	Input  int `json:"input"`
	Output int `json:"output"`
}

type openCodeError struct {
	Name string           `json:"name"`
	Data *openCodeErrData `json:"data,omitempty"`
}

type openCodeErrData struct {
	Message string `json:"message"`
}

// ParseOutput extracts the final text output from opencode's NDJSON stream.
// It takes the last "type":"text" event's text content and strips <think> tags.
func ParseOutput(stdout []byte) (string, error) {
	var lastText string
	scanner := bufio.NewScanner(bytes.NewReader(stdout))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var event openCodeEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		if event.Part != nil && event.Part.Type == "text" {
			lastText = event.Part.Text
		}
	}
	return stripThinkTags(lastText), nil
}

// stripThinkTags removes <think>...</think> blocks from model output.
// Some models (DeepSeek, Qwen) wrap reasoning in these tags.
func stripThinkTags(s string) string {
	for {
		start := strings.Index(s, "<think>")
		if start == -1 {
			break
		}
		end := strings.Index(s, "</think>")
		if end == -1 {
			s = s[:start]
			break
		}
		s = s[:start] + s[end+len("</think>"):]
	}
	return strings.TrimSpace(s)
}
