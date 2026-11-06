package runner

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// hugeLine is a tool event with a line longer than the parser reads.
func hugeLine() string {
	return `{"type":"tool_use","part":{"tool":"read","state":{"status":"completed","output":"` + strings.Repeat("x", maxOutputLine+1024) + `"}}}`
}

// A line over the limit stops the parse. What came before it may look like an
// answer, but the stream went on, so it is not one.
func TestParseOutput_LineOverTheLimitIsNotAnAnswer(t *testing.T) {
	stream := `{"type":"step_start"}` + "\n" +
		`{"type":"text","part":{"type":"text","text":"early answer"}}` + "\n" +
		`{"type":"step_finish","part":{"reason":"stop"}}` + "\n" +
		hugeLine() + "\n" +
		`{"type":"step_start"}` + "\n" +
		`{"type":"error","error":{"name":"APIError","data":{"message":"later error"}}}` + "\n"

	got := ParseOutput([]byte(stream))
	if got.Answer != "" {
		t.Errorf("Answer = %q, want none: the stream went on past the line that stopped the parse", got.Answer)
	}
	if got.ReadErr == "" {
		t.Error("ReadErr is empty, want the reason the parse stopped")
	}
}

func TestClassify_LineOverTheLimitFails(t *testing.T) {
	stream := `{"type":"step_start"}` + "\n" +
		`{"type":"step_finish","part":{"reason":"tool-calls"}}` + "\n" +
		`{"type":"step_start"}` + "\n" +
		hugeLine() + "\n"

	a := attempt{transcript: ParseOutput([]byte(stream))}
	status, cause := classify(a, Options{}, time.Second)
	if status != StatusFailed || cause == nil {
		t.Fatalf("classify() = %s, %v, want a failure", status, cause)
	}
	msg := cause.Message()
	if !strings.Contains(msg, "line") || !strings.Contains(msg, "16 MB") || strings.Contains(msg, "middle of step") {
		t.Errorf("message = %q, want it to say a line over 16 MB stopped the parse, not that the output ended mid-step", msg)
	}
}

// A line over the parser's limit must not hide the events after it from the
// progress log, or the step limit is no longer enforced.
func TestProgressWriter_EventsAfterAHugeLineStillCount(t *testing.T) {
	var out bytes.Buffer
	steps := 0
	pw := &progressWriter{buf: &bytes.Buffer{}, w: &out, onStep: func(int) { steps++ }}

	stream := hugeLine() + "\n" + `{"type":"step_start"}` + "\n" + `{"type":"step_start"}` + "\n"
	pw.Write([]byte(stream))

	if steps != 2 {
		t.Errorf("onStep called %d times, want 2; output: %q", steps, out.String())
	}
}
