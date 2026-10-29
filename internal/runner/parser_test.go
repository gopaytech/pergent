package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixtureReview = "## Review\n\n- **[service/handler.go:42]** The error from `Save` is discarded, so a failed write still returns 200.\n\nNo other issues found."

const compactionContinue = "Continue if you have next steps, or stop and ask for clarification if you are unsure how to proceed."

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	return data
}

func TestParseOutput_Fixtures(t *testing.T) {
	tests := []struct {
		fixture        string
		answer         string
		steps          int
		lastTokens     int
		finishReason   string
		inStep         bool
		compacted      bool
		errorName      string
		lastTool       string
		lastToolTarget string
	}{
		{fixture: "completed.ndjson", answer: fixtureReview, steps: 4, lastTokens: 45000, finishReason: "stop", lastTool: "read", lastToolTarget: "/builds/repo/service/handler.go"},
		{fixture: "compacted_no_answer.ndjson", steps: 5, lastTokens: 98000, finishReason: "stop", compacted: true, lastTool: "grep", lastToolTarget: "/builds/repo/service"},
		{fixture: "compacted_then_answer.ndjson", answer: fixtureReview, steps: 4, lastTokens: 8000, finishReason: "stop", compacted: true, lastTool: "grep", lastToolTarget: "/builds/repo/service"},
		{fixture: "context_overflow.ndjson", steps: 6, lastTokens: 156000, finishReason: "tool-calls", inStep: true, errorName: "ContextOverflowError", lastTool: "read", lastToolTarget: "/builds/repo/service/handler.go"},
		{fixture: "rate_limited.ndjson", steps: 2, lastTokens: 14000, finishReason: "tool-calls", inStep: true, errorName: "APIError", lastTool: "read", lastToolTarget: "/builds/repo/service/handler.go"},
		{fixture: "step_limit.ndjson", answer: "Maximum steps for this agent have been reached. So far I read service/handler.go. Remaining: check callers of Save.", steps: 3, lastTokens: 40000, finishReason: "stop", lastTool: "grep", lastToolTarget: "/builds/repo/service"},
		{fixture: "timeout.ndjson", steps: 3, lastTokens: 23000, finishReason: "tool-calls", inStep: true, lastTool: "read", lastToolTarget: "/builds/repo/service/store.go"},
		{fixture: "no_answer.ndjson", steps: 2, lastTokens: 23000, finishReason: "tool-calls", lastTool: "grep", lastToolTarget: "/builds/repo/service"},
		{fixture: "output_length.ndjson", steps: 2, lastTokens: 30000, finishReason: "length", lastTool: "read", lastToolTarget: "/builds/repo/service/handler.go"},
	}

	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			got := ParseOutput(readFixture(t, tt.fixture))

			if got.Answer != tt.answer {
				t.Errorf("Answer = %q, want %q", got.Answer, tt.answer)
			}
			if got.Steps != tt.steps {
				t.Errorf("Steps = %d, want %d", got.Steps, tt.steps)
			}
			if got.LastStepTokens != tt.lastTokens {
				t.Errorf("LastStepTokens = %d, want %d", got.LastStepTokens, tt.lastTokens)
			}
			if got.LastFinishReason != tt.finishReason {
				t.Errorf("LastFinishReason = %q, want %q", got.LastFinishReason, tt.finishReason)
			}
			if got.InStep != tt.inStep {
				t.Errorf("InStep = %v, want %v", got.InStep, tt.inStep)
			}
			if got.Compacted != tt.compacted {
				t.Errorf("Compacted = %v, want %v", got.Compacted, tt.compacted)
			}
			var errName string
			if e := got.LastError(); e != nil {
				errName = e.Name
			}
			if errName != tt.errorName {
				t.Errorf("LastError().Name = %q, want %q", errName, tt.errorName)
			}
			if got.LastTool != tt.lastTool || got.LastToolTarget != tt.lastToolTarget {
				t.Errorf("last tool = %q on %q, want %q on %q", got.LastTool, got.LastToolTarget, tt.lastTool, tt.lastToolTarget)
			}
		})
	}
}

// The bug this guards against: after compacting, opencode injects a synthetic
// user message, and posting the last text event posted that message as the review.
func TestParseOutput_NeverReturnsSyntheticText(t *testing.T) {
	for _, fixture := range []string{"compacted_no_answer.ndjson", "compacted_then_answer.ndjson", "real_compacted_no_answer.ndjson", "real_compacted_then_answer.ndjson"} {
		got := ParseOutput(readFixture(t, fixture))
		if strings.Contains(got.Answer, "Continue if you have next steps") {
			t.Errorf("%s: Answer is opencode's synthetic message: %q", fixture, got.Answer)
		}
	}
}

func TestParseOutput_CompactionPoint(t *testing.T) {
	got := ParseOutput(readFixture(t, "compacted_no_answer.ndjson"))
	// The last step before the synthetic message is opencode's summary; the
	// step before it is the one that filled the context.
	if got.CompactedAtTokens != 87000 || got.CompactedAtStep != 4 {
		t.Errorf("compacted at %d tokens / step %d, want 87000 / 4", got.CompactedAtTokens, got.CompactedAtStep)
	}
}

func TestParseOutput_SyntheticTextInsideStepIgnored(t *testing.T) {
	stdout := `{"type":"step_start","part":{"type":"step-start"}}
{"type":"text","part":{"type":"text","text":"` + compactionContinue + `","synthetic":true}}
{"type":"step_finish","part":{"type":"step-finish","reason":"stop","tokens":{"input":10,"output":5}}}
`
	if got := ParseOutput([]byte(stdout)); got.Answer != "" {
		t.Errorf("Answer = %q, want empty (synthetic text is not the model's)", got.Answer)
	}
}

func TestParseOutput_TextOutsideStepIgnored(t *testing.T) {
	// Text outside step_start/step_finish isn't from an assistant step.
	stdout := `{"type":"step_start","part":{"type":"step-start"}}
{"type":"text","part":{"type":"text","text":"Real answer"}}
{"type":"step_finish","part":{"type":"step-finish","reason":"stop","tokens":{"input":10,"output":5}}}
{"type":"text","part":{"type":"text","text":"Review the attached diff"}}
`
	if got := ParseOutput([]byte(stdout)); got.Answer != "" {
		t.Errorf("Answer = %q, want empty: a non-assistant message came after the answer", got.Answer)
	}
}

func TestParseOutput_NarrationBeforeToolCallIsNotAnswer(t *testing.T) {
	stdout := `{"type":"step_start","part":{"type":"step-start"}}
{"type":"text","part":{"type":"text","text":"Let me check..."}}
{"type":"tool_use","part":{"type":"tool","tool":"read","state":{"status":"completed","input":{"filePath":"a.go"}}}}
{"type":"step_finish","part":{"type":"step-finish","reason":"tool-calls","tokens":{"input":10,"output":5}}}
`
	if got := ParseOutput([]byte(stdout)); got.Answer != "" {
		t.Errorf("Answer = %q, want empty", got.Answer)
	}
}

func TestParseOutput_MultipleTextPartsInFinalStep(t *testing.T) {
	stdout := `{"type":"step_start","part":{"type":"step-start"}}
{"type":"text","part":{"type":"text","text":"## Review"}}
{"type":"reasoning","part":{"type":"reasoning","text":"thinking"}}
{"type":"text","part":{"type":"text","text":"- finding"}}
{"type":"step_finish","part":{"type":"step-finish","reason":"stop","tokens":{"input":10,"output":5}}}
`
	if got := ParseOutput([]byte(stdout)); got.Answer != "## Review\n\n- finding" {
		t.Errorf("Answer = %q, want both text parts", got.Answer)
	}
}

func TestParseOutput_TokensWithoutTotal(t *testing.T) {
	stdout := `{"type":"step_start","part":{"type":"step-start"}}
{"type":"step_finish","part":{"type":"step-finish","reason":"tool-calls","tokens":{"input":1000,"output":200,"reasoning":0,"cache":{"read":300,"write":40}}}}
`
	if got := ParseOutput([]byte(stdout)); got.LastStepTokens != 1540 {
		t.Errorf("LastStepTokens = %d, want 1540 (input+output+cache)", got.LastStepTokens)
	}
}

func TestParseOutput_ErrorEventFields(t *testing.T) {
	got := ParseOutput(readFixture(t, "rate_limited.ndjson"))
	e := got.LastError()
	if e == nil {
		t.Fatal("LastError() = nil, want the APIError")
	}
	if e.Message != "Rate limit exceeded: 429 Too Many Requests" || e.StatusCode != 429 || !e.IsRetryable {
		t.Errorf("error = %+v, want message, statusCode 429 and isRetryable", *e)
	}
}

func TestParseOutput_EmptyInput(t *testing.T) {
	got := ParseOutput([]byte(""))
	if got.Answer != "" || got.Steps != 0 || got.LastError() != nil {
		t.Errorf("ParseOutput(\"\") = %+v, want zero transcript", got)
	}
}

func TestParseOutput_SkipsNonJSON(t *testing.T) {
	stdout := `not json
{"type":"step_start","part":{"type":"step-start"}}
{"type":"text","part":{"type":"text","text":"Valid output"}}
also not json
{"type":"step_finish","part":{"type":"step-finish","reason":"stop"}}
`
	if got := ParseOutput([]byte(stdout)); got.Answer != "Valid output" {
		t.Errorf("Answer = %q, want %q", got.Answer, "Valid output")
	}
}

func TestParseOutput_WithThinkTags(t *testing.T) {
	stdout := `{"type":"step_start","part":{"type":"step-start"}}
{"type":"text","part":{"type":"text","text":"<think>\nLet me analyze this...\n</think>\n\nThe code looks good."}}
{"type":"step_finish","part":{"type":"step-finish","reason":"stop"}}
`
	if got := ParseOutput([]byte(stdout)); got.Answer != "The code looks good." {
		t.Errorf("Answer = %q, want %q", got.Answer, "The code looks good.")
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
		{"consecutive leading blocks", "<think>a</think>\n<think>b</think>\nend", "end"},
		{"leading whitespace before block", "\n  <think>a</think>\nResult", "Result"},
		{"unclosed tag", "<think>reasoning forever", ""},
		{"empty think", "<think></think>content", "content"},
		{"only think", "<think>just thinking</think>", ""},
		{"empty string", "", ""},
		// Tags after the answer starts are part of the review (e.g. quoting a
		// diff that contains the literal text) and must be kept verbatim.
		{"mid-text block kept", "before<think>\nlong\nreasoning\n</think>\nafter", "before<think>\nlong\nreasoning\n</think>\nafter"},
		{"mid-text block after leading block kept", "<think>a</think>middle<think>b</think>end", "middle<think>b</think>end"},
		{"mid-text unclosed tag kept", "Finding: the parser drops text after <think> with no closing tag.\n\nMore findings.", "Finding: the parser drops text after <think> with no closing tag.\n\nMore findings."},
		{"backtick-quoted tag kept", "Use `<think>` as the marker.\nNext point.", "Use `<think>` as the marker.\nNext point."},
		{"leading block then mid-text mention", "<think>plan</think>\nThe `<think>` handling looks fine.", "The `<think>` handling looks fine."},
		{"stray closing tag before block", "a</think>b<think>x</think>c", "a</think>b<think>x</think>c"},
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

func TestParseOutput_Usage(t *testing.T) {
	stdout := `{"type":"step_start","part":{"type":"step-start"}}
{"type":"step_finish","part":{"type":"step-finish","reason":"tool-calls","tokens":{"total":99,"input":10,"output":3,"cache":{"read":50,"write":1}}}}
{"type":"step_start","part":{"type":"step-start"}}
{"type":"step_finish","part":{"type":"step-finish","reason":"stop","tokens":{"total":200,"input":20,"output":5}}}
`
	got := ParseOutput([]byte(stdout))
	if want := (Usage{Input: 30, Output: 8}); got.Usage != want {
		t.Errorf("Usage = %+v, want %+v", got.Usage, want)
	}
}
