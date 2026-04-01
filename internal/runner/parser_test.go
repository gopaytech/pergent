package runner

import (
	"testing"
)

func TestParseOutput_TextEvents(t *testing.T) {
	stdout := `{"type":"message.part.updated","part":{"type":"thinking","text":"Let me analyze..."}}
{"type":"message.part.updated","part":{"type":"text","text":"## Review\n\n"}}
{"type":"message.part.updated","part":{"type":"text","text":"## Review\n\n- Bug found in line 42"}}
{"type":"step_finish","tokens":{"input":1234,"output":567}}
`
	result, err := ParseOutput([]byte(stdout))
	if err != nil {
		t.Fatalf("ParseOutput() error: %v", err)
	}
	expected := "## Review\n\n- Bug found in line 42"
	if result != expected {
		t.Errorf("result = %q, want %q", result, expected)
	}
}

func TestParseOutput_NoTextEvents(t *testing.T) {
	stdout := `{"type":"step_finish","tokens":{"input":100,"output":50}}
`
	result, err := ParseOutput([]byte(stdout))
	if err != nil {
		t.Fatalf("ParseOutput() error: %v", err)
	}
	if result != "" {
		t.Errorf("result = %q, want empty string", result)
	}
}

func TestParseOutput_EmptyInput(t *testing.T) {
	result, err := ParseOutput([]byte(""))
	if err != nil {
		t.Fatalf("ParseOutput() error: %v", err)
	}
	if result != "" {
		t.Errorf("result = %q, want empty string", result)
	}
}

func TestParseOutput_WithThinkTags(t *testing.T) {
	stdout := `{"type":"text","part":{"type":"text","text":"<think>\nLet me analyze this...\n</think>\n\nThe code looks good."}}
`
	result, err := ParseOutput([]byte(stdout))
	if err != nil {
		t.Fatalf("ParseOutput() error: %v", err)
	}
	expected := "The code looks good."
	if result != expected {
		t.Errorf("result = %q, want %q", result, expected)
	}
}

func TestStripThinkTags(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no tags", "Hello world", "Hello world"},
		{"simple think block", "<think>reasoning</think>\n\nResult", "Result"},
		{"multiple think blocks", "<think>a</think>middle<think>b</think>end", "middleend"},
		{"unclosed tag", "<think>reasoning forever", ""},
		{"empty think", "<think></think>content", "content"},
		{"only think", "<think>just thinking</think>", ""},
		{"nested content", "before<think>\nlong\nreasoning\n</think>\nafter", "before\nafter"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripThinkTags(tt.input)
			if got != tt.want {
				t.Errorf("stripThinkTags(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseOutput_SkipsNonJSON(t *testing.T) {
	stdout := `not json
{"type":"text","part":{"type":"text","text":"Valid output"}}
also not json
`
	result, err := ParseOutput([]byte(stdout))
	if err != nil {
		t.Fatalf("ParseOutput() error: %v", err)
	}
	if result != "Valid output" {
		t.Errorf("result = %q, want %q", result, "Valid output")
	}
}

func TestParseOutput_MultipleSessions(t *testing.T) {
	stdout := `{"type":"text","part":{"type":"text","text":"First response"}}
{"type":"step_finish","part":{"type":"step-finish"}}
{"type":"text","part":{"type":"text","text":"Second response"}}
{"type":"step_finish","part":{"type":"step-finish"}}
`
	result, err := ParseOutput([]byte(stdout))
	if err != nil {
		t.Fatalf("ParseOutput() error: %v", err)
	}
	if result != "Second response" {
		t.Errorf("result = %q, want %q (last text event)", result, "Second response")
	}
}
