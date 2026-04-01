package runner

import (
	"bytes"
	"strings"
	"testing"
)

func TestProgressWriter_StepStart(t *testing.T) {
	var out bytes.Buffer
	pw := &progressWriter{buf: &bytes.Buffer{}, w: &out}

	event := `{"type":"step_start","part":{"type":"step-start"}}` + "\n"
	pw.Write([]byte(event))

	if !strings.Contains(out.String(), "Step 1: starting...") {
		t.Errorf("output = %q, want step start message", out.String())
	}
}

func TestProgressWriter_StepFinishWithTokens(t *testing.T) {
	var out bytes.Buffer
	pw := &progressWriter{buf: &bytes.Buffer{}, step: 1, w: &out}

	event := `{"type":"step_finish","part":{"type":"step-finish","tokens":{"total":1000,"input":800,"output":200}}}` + "\n"
	pw.Write([]byte(event))

	if !strings.Contains(out.String(), "Step 1: done (200 tokens)") {
		t.Errorf("output = %q, want step finish with tokens", out.String())
	}
}

func TestProgressWriter_StepFinishWithoutTokens(t *testing.T) {
	var out bytes.Buffer
	pw := &progressWriter{buf: &bytes.Buffer{}, step: 1, w: &out}

	event := `{"type":"step_finish","part":{"type":"step-finish"}}` + "\n"
	pw.Write([]byte(event))

	if !strings.Contains(out.String(), "Step 1: done") {
		t.Errorf("output = %q, want step finish", out.String())
	}
}

func TestProgressWriter_ToolUse(t *testing.T) {
	var out bytes.Buffer
	pw := &progressWriter{buf: &bytes.Buffer{}, step: 1, w: &out}

	event := `{"type":"tool_use","part":{"tool":"read","state":{"status":"success"}}}` + "\n"
	pw.Write([]byte(event))

	if !strings.Contains(out.String(), "Step 1: tool read") {
		t.Errorf("output = %q, want tool use message", out.String())
	}
}

func TestProgressWriter_ToolUseError(t *testing.T) {
	var out bytes.Buffer
	pw := &progressWriter{buf: &bytes.Buffer{}, step: 1, w: &out}

	event := `{"type":"tool_use","part":{"tool":"skill","state":{"status":"error","error":"Skill not found"}}}` + "\n"
	pw.Write([]byte(event))

	got := out.String()
	if !strings.Contains(got, "tool skill (error:") {
		t.Errorf("output = %q, want tool error message", got)
	}
}

func TestProgressWriter_TextEvent(t *testing.T) {
	var out bytes.Buffer
	pw := &progressWriter{buf: &bytes.Buffer{}, step: 1, w: &out}

	event := `{"type":"text","part":{"type":"text","text":"Review output"}}` + "\n"
	pw.Write([]byte(event))

	if !strings.Contains(out.String(), "generating text...") {
		t.Errorf("output = %q, want text generation message", out.String())
	}
}

func TestProgressWriter_ErrorEvent(t *testing.T) {
	var out bytes.Buffer
	pw := &progressWriter{buf: &bytes.Buffer{}, w: &out}

	event := `{"type":"error","error":{"name":"APIError","data":{"message":"Rate limit exceeded"}}}` + "\n"
	pw.Write([]byte(event))

	if !strings.Contains(out.String(), "Error: Rate limit exceeded") {
		t.Errorf("output = %q, want error message", out.String())
	}
}

func TestProgressWriter_BuffersRawOutput(t *testing.T) {
	var out bytes.Buffer
	pw := &progressWriter{buf: &bytes.Buffer{}, w: &out}

	raw := `{"type":"text","part":{"type":"text","text":"hello"}}` + "\n"
	pw.Write([]byte(raw))

	if pw.buf.String() != raw {
		t.Errorf("buffer = %q, want raw input preserved", pw.buf.String())
	}
}

func TestProgressWriter_StepCounter(t *testing.T) {
	var out bytes.Buffer
	pw := &progressWriter{buf: &bytes.Buffer{}, w: &out}

	pw.Write([]byte(`{"type":"step_start","part":{"type":"step-start"}}` + "\n"))
	pw.Write([]byte(`{"type":"step_start","part":{"type":"step-start"}}` + "\n"))
	pw.Write([]byte(`{"type":"step_start","part":{"type":"step-start"}}` + "\n"))

	got := out.String()
	if !strings.Contains(got, "Step 1:") || !strings.Contains(got, "Step 2:") || !strings.Contains(got, "Step 3:") {
		t.Errorf("output = %q, want steps 1-3", got)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input string
		max   int
		want  string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is too long", 10, "this is to..."},
		{"", 5, ""},
	}

	for _, tt := range tests {
		got := truncate(tt.input, tt.max)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
		}
	}
}
