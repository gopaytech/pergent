package runner

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
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
{"type":"step_finish","part":{"type":"step-finish","reason":"tool-calls","tokens":{"total":99,"input":10,"output":3,"reasoning":4,"cache":{"read":50,"write":1}}}}
{"type":"step_start","part":{"type":"step-start"}}
{"type":"step_finish","part":{"type":"step-finish","reason":"stop","tokens":{"total":200,"input":20,"output":5}}}
`
	got := ParseOutput([]byte(stdout))
	if want := (Usage{Input: 30, Output: 8, Reasoning: 4, CacheRead: 50, CacheWrite: 1}); got.Usage != want {
		t.Errorf("Usage = %+v, want %+v", got.Usage, want)
	}
}

func TestParseOutput_OpenedPaths(t *testing.T) {
	ev := func(tool, status, input string) string {
		return `{"type":"tool_use","part":{"type":"tool","tool":"` + tool + `","state":{"status":"` + status + `","input":` + input + `}}}` + "\n"
	}
	stdout := `{"type":"step_start","part":{"type":"step-start"}}` + "\n" +
		ev("read", "completed", `{"filePath":"/work/linked-1/a.go"}`) +
		ev("read", "error", `{"filePath":"/work/linked-2/denied.go"}`) +
		ev("grep", "completed", `{"pattern":"/work/linked-3/x","path":"/work/linked-1/svc"}`) +
		ev("grep", "completed", `{"pattern":"/work/linked-3/x"}`) +
		ev("glob", "completed", `{"pattern":"**/*.go","path":"/work/linked-1"}`) +
		ev("list", "completed", `{"path":"/work/linked-1"}`) +
		ev("edit", "completed", `{"filePath":"/work/linked-1/b.go"}`) +
		ev("read", "completed", `{}`) +
		`{"type":"step_finish","part":{"type":"step-finish","reason":"stop","tokens":{"input":1,"output":1}}}` + "\n"
	got := ParseOutput([]byte(stdout)).Opened
	want := []string{"/work/linked-1/a.go", "/work/linked-1/svc", "/work/linked-1", "/work/linked-1"}
	if !slices.Equal(got, want) {
		t.Errorf("Opened = %q, want %q: only a completed read, grep, glob or list with a path", got, want)
	}
}

func TestParseOutput_OpenedIsCapped(t *testing.T) {
	var b strings.Builder
	for i := 0; i < maxOpened+50; i++ {
		b.WriteString(`{"type":"tool_use","part":{"tool":"read","state":{"status":"completed","input":{"filePath":"/x"}}}}` + "\n")
	}
	if got := len(ParseOutput([]byte(b.String())).Opened); got != maxOpened {
		t.Errorf("len(Opened) = %d, want %d", got, maxOpened)
	}
}

func TestRun_ReportsOpenedPaths(t *testing.T) {
	fakeOpencode(t, fakeAttempt{stdout: `{"type":"step_start","part":{"type":"step-start"}}
{"type":"tool_use","part":{"type":"tool","tool":"read","state":{"status":"completed","input":{"filePath":"/work/linked-1/a.go"}}}}
{"type":"step_finish","part":{"type":"step-finish","reason":"tool-calls","tokens":{"input":1,"output":1}}}
{"type":"step_start","part":{"type":"step-start"}}
{"type":"text","part":{"type":"text","text":"Done."}}
{"type":"step_finish","part":{"type":"step-finish","reason":"stop","tokens":{"input":1,"output":1}}}
`})
	got := Run(context.Background(), testOptions(time.Minute))
	if got.Status != StatusCompleted || !slices.Equal(got.Opened, []string{"/work/linked-1/a.go"}) {
		t.Errorf("Run() = %+v, want it to report the path it read", got)
	}
}

func usageEv(tool, status, field, val string) string {
	return `{"type":"tool_use","part":{"type":"tool","tool":"` + tool + `","state":{"status":"` + status + `","input":{"filePath":"/x"},"` + field + `":"` + val + `"}}}` + "\n"
}

const (
	usageStart = `{"type":"step_start","part":{"type":"step-start"}}` + "\n"
	usageStop  = `{"type":"step_finish","part":{"type":"step-finish","reason":"stop","tokens":{"input":1,"output":1}}}` + "\n"
)

func TestParseOutput_PerStep(t *testing.T) {
	stdout := usageStart +
		usageEv("read", "completed", "output", "abc") +
		usageEv("grep", "error", "error", "no") +
		`{"type":"step_finish","part":{"type":"step-finish","reason":"tool-calls","tokens":{"input":10,"output":3,"reasoning":4,"cache":{"read":50,"write":1}}}}` + "\n" +
		usageStart +
		`{"type":"text","part":{"type":"text","text":"Done."}}` + "\n" +
		`{"type":"step_finish","part":{"type":"step-finish","reason":"stop","tokens":{"input":20,"output":5}}}` + "\n"
	got := ParseOutput([]byte(stdout))
	if len(got.PerStep) != 2 {
		t.Fatalf("len(PerStep) = %d, want 2", len(got.PerStep))
	}
	if want := (Usage{Input: 10, Output: 3, Reasoning: 4, CacheRead: 50, CacheWrite: 1}); got.PerStep[0].Usage != want {
		t.Errorf("PerStep[0].Usage = %+v, want %+v", got.PerStep[0].Usage, want)
	}
	if want := (Usage{Input: 20, Output: 5}); got.PerStep[1].Usage != want {
		t.Errorf("PerStep[1].Usage = %+v, want %+v", got.PerStep[1].Usage, want)
	}
	tools := got.PerStep[0].Tools
	if len(tools) != 2 ||
		tools[0].Tool != "read" || tools[0].Bytes != 3 || tools[0].Failed ||
		tools[1].Tool != "grep" || tools[1].Bytes != 2 || !tools[1].Failed {
		t.Errorf("PerStep[0].Tools = %+v, want read (3 bytes) and failed grep (2 bytes)", tools)
	}
	if tools[0].path != "/x" {
		t.Errorf("path = %q, want /x", tools[0].path)
	}
	if len(got.PerStep[1].Tools) != 0 {
		t.Errorf("PerStep[1].Tools = %+v, want none", got.PerStep[1].Tools)
	}
}

func TestParseOutput_ToolTotals(t *testing.T) {
	stdout := usageStart +
		usageEv("read", "completed", "output", "abc") +
		usageEv("read", "completed", "output", "abcd") +
		usageEv("grep", "completed", "output", "a") +
		usageEv("glob", "completed", "output", "a") +
		usageEv("list", "completed", "output", "a") +
		usageEv("edit", "completed", "output", "a") +
		usageEv("invalid", "completed", "output", "a") +
		usageStop
	got := ParseOutput([]byte(stdout))
	if got.ToolTotals.Read != (ToolUse{2, 7}) {
		t.Errorf("Read = %+v, want {2 7}", got.ToolTotals.Read)
	}
	for name, u := range map[string]ToolUse{"Grep": got.ToolTotals.Grep, "Glob": got.ToolTotals.Glob, "List": got.ToolTotals.List} {
		if u.Calls != 1 {
			t.Errorf("%s.Calls = %d, want 1", name, u.Calls)
		}
	}
	if got.ToolTotals.Other.Calls != 2 {
		t.Errorf("Other.Calls = %d, want 2", got.ToolTotals.Other.Calls)
	}
	for _, c := range got.PerStep[0].Tools {
		if !slices.Contains([]string{"read", "grep", "glob", "list", "other"}, c.Tool) {
			t.Errorf("Tool = %q, not in the fixed set", c.Tool)
		}
	}
}

func TestParseOutput_ToolAfterStepFinishBelongsToThatStep(t *testing.T) {
	stdout := usageStart + usageStop + usageEv("read", "completed", "output", "ab") + usageStart + usageStop
	got := ParseOutput([]byte(stdout))
	if len(got.PerStep) != 2 || len(got.PerStep[0].Tools) != 1 || len(got.PerStep[1].Tools) != 0 {
		t.Errorf("PerStep = %+v, want the call in step 1", got.PerStep)
	}
}

func TestParseOutput_ToolBeforeAnyStep(t *testing.T) {
	got := ParseOutput([]byte(usageEv("read", "completed", "output", "ab") + usageStart + usageStop))
	if got.ToolTotals.Read.Calls != 1 {
		t.Errorf("Read.Calls = %d, want 1", got.ToolTotals.Read.Calls)
	}
	if len(got.PerStep) != 1 || len(got.PerStep[0].Tools) != 0 {
		t.Errorf("PerStep = %+v, want the call in no step", got.PerStep)
	}
}

func TestParseOutput_PerStepSumsToUsage(t *testing.T) {
	for _, f := range []string{"completed", "compacted_no_answer", "compacted_then_answer", "context_overflow", "rate_limited", "step_limit", "timeout", "no_answer", "output_length"} {
		t.Run(f, func(t *testing.T) {
			got := ParseOutput(readFixture(t, f+".ndjson"))
			if len(got.PerStep) != got.Steps {
				t.Errorf("len(PerStep) = %d, want Steps %d", len(got.PerStep), got.Steps)
			}
			var sum Usage
			for _, s := range got.PerStep {
				sum = sum.Add(s.Usage)
			}
			if sum != got.Usage {
				t.Errorf("sum of PerStep = %+v, want Usage %+v", sum, got.Usage)
			}
			if f == "timeout" && got.PerStep[len(got.PerStep)-1].Usage != (Usage{}) {
				t.Errorf("last step Usage = %+v, want zero", got.PerStep[len(got.PerStep)-1].Usage)
			}
		})
	}
}

func TestParseOutput_ToolCallsCapped(t *testing.T) {
	var b strings.Builder
	b.WriteString(usageStart)
	for i := 0; i < maxToolCalls+50; i++ {
		b.WriteString(usageEv("read", "completed", "output", "a"))
	}
	got := ParseOutput([]byte(b.String()))
	if n := len(got.PerStep[0].Tools); n != maxToolCalls {
		t.Errorf("kept %d calls, want %d", n, maxToolCalls)
	}
	if got.ToolTotals.Read.Calls != maxToolCalls+50 {
		t.Errorf("Read.Calls = %d, want %d", got.ToolTotals.Read.Calls, maxToolCalls+50)
	}
}
