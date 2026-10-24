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

func TestProgressWriter_SyntheticText(t *testing.T) {
	var out bytes.Buffer
	pw := &progressWriter{buf: &bytes.Buffer{}, step: 4, w: &out}

	event := `{"type":"text","part":{"type":"text","text":"Continue if you have next steps","synthetic":true}}` + "\n"
	pw.Write([]byte(event))

	got := out.String()
	if strings.Contains(got, "generating text") || !strings.Contains(got, "opencode added its own message") {
		t.Errorf("output = %q, want synthetic text labelled as opencode's", got)
	}
}

func TestProgressWriter_ErrorRedacted(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "sk-test-abcdef0123456789")
	var out bytes.Buffer
	pw := &progressWriter{buf: &bytes.Buffer{}, w: &out}

	pw.Write([]byte(`{"type":"error","error":{"name":"ProviderAuthError","data":{"message":"bad key sk-test-abcdef0123456789"}}}` + "\n"))

	if strings.Contains(out.String(), "sk-test-abcdef0123456789") {
		t.Errorf("output = %q, leaks the API key", out.String())
	}
}

func TestProgressWriter_LineSplitAcrossWrites(t *testing.T) {
	var out bytes.Buffer
	var steps []int
	pw := &progressWriter{buf: &bytes.Buffer{}, w: &out, onStep: func(n int) { steps = append(steps, n) }}

	line := `{"type":"step_start","part":{"type":"step-start"}}` + "\n"
	pw.Write([]byte(line[:20]))
	pw.Write([]byte(line[20:] + line[:10]))
	pw.Write([]byte(line[10:]))

	if len(steps) != 2 || steps[1] != 2 {
		t.Errorf("onStep calls = %v, want [1 2]", steps)
	}
	if pw.buf.String() != line+line {
		t.Errorf("buffer = %q, want both raw lines", pw.buf.String())
	}
}

func TestProgressWriter_KeyStraddlingTheCutIsRedacted(t *testing.T) {
	const key = "sk-test-abcdef0123456789"
	t.Setenv("OPENCODE_API_KEY", key)
	var out bytes.Buffer
	pw := &progressWriter{buf: &bytes.Buffer{}, w: &out}

	// The key starts 10 bytes before the 120-byte cut.
	msg := strings.Repeat("x", 110) + key
	pw.Write([]byte(`{"type":"error","error":{"name":"ProviderAuthError","data":{"message":"` + msg + `"}}}` + "\n"))

	if got := out.String(); strings.Contains(got, key[:10]) {
		t.Errorf("output = %q, prints part of the API key", got)
	}
}

func TestProgressWriter_ToolErrorRedacted(t *testing.T) {
	const key = "sk-test-abcdef0123456789"
	t.Setenv("OPENCODE_API_KEY", key)
	var out bytes.Buffer
	pw := &progressWriter{buf: &bytes.Buffer{}, step: 1, w: &out}

	pw.Write([]byte(`{"type":"tool_use","part":{"tool":"read","state":{"status":"error","error":"cannot read ` + strings.Repeat("y", 64) + key + `"}}}` + "\n"))

	if got := out.String(); strings.Contains(got, key[:4]) {
		t.Errorf("output = %q, prints part of the API key", got)
	}
}
