package runner

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestClassify_Fixtures(t *testing.T) {
	opts := Options{Timeout: 10 * time.Minute, StepLimit: 20}
	exit1 := errors.New("exit status 1")

	tests := []struct {
		name     string
		fixture  string
		attempt  attempt
		opts     Options
		status   Status
		kind     CauseKind
		message  string
		elapsed  time.Duration
		noStderr bool
	}{
		{
			name:    "completed",
			fixture: "completed.ndjson",
			status:  StatusCompleted,
		},
		{
			name:    "compaction then answer is still the model's answer",
			fixture: "compacted_then_answer.ndjson",
			status:  StatusCompleted,
		},
		{
			name:    "compaction without answer",
			fixture: "compacted_no_answer.ndjson",
			status:  StatusFailed,
			kind:    CauseNoAnswer,
			message: "opencode compacted the context at 87,000 tokens after 4 steps, and the run ended before the model produced a review.",
		},
		{
			// Captured from opencode 1.19.4 with compaction on: the run's last
			// text event is the synthetic "Continue if you have next steps..."
			name:    "compaction without answer from a real run",
			fixture: "real_compacted_no_answer.ndjson",
			status:  StatusFailed,
			kind:    CauseNoAnswer,
			message: "opencode compacted the context at 12,300 tokens after 1 step, and the run ended before the model produced a review.",
		},
		{
			name:    "compaction then answer from a real run",
			fixture: "real_compacted_then_answer.ndjson",
			status:  StatusCompleted,
		},
		{
			name:    "context overflow",
			fixture: "context_overflow.ndjson",
			attempt: attempt{runErr: exit1, exitCode: 1},
			status:  StatusFailed,
			kind:    CauseContextOverflow,
			message: "The model's context window overflowed after 6 steps: the last completed step used 156,000 tokens (limit: the model's context window as known to opencode). provider.invalid-request: prompt is too long: 201234 tokens > 200000 maximum.",
		},
		{
			name:    "context overflow with explicit limit",
			fixture: "context_overflow.ndjson",
			attempt: attempt{runErr: exit1, exitCode: 1},
			opts:    Options{Timeout: 10 * time.Minute, StepLimit: 20, ContextLimit: 160000},
			status:  StatusFailed,
			kind:    CauseContextOverflow,
			message: "The model's context window overflowed after 6 steps: the last completed step used 156,000 tokens (limit: 160,000 tokens, set by --context-limit). provider.invalid-request: prompt is too long: 201234 tokens > 200000 maximum.",
		},
		{
			// Shape of an opencode 2 context overflow error event.
			name:    "context overflow from a real run",
			fixture: "real_context_overflow.ndjson",
			attempt: attempt{runErr: exit1, exitCode: 1},
			status:  StatusFailed,
			kind:    CauseContextOverflow,
			message: "The model's context window overflowed after 1 step: the last completed step used 12,300 tokens (limit: the model's context window as known to opencode). provider.invalid-request: This model's maximum context length is 8192 tokens. However, your messages resulted in 9100 tokens.",
		},
		{
			name:    "replies cut at the max output",
			fixture: "output_limit.ndjson",
			attempt: attempt{runErr: exit1, exitCode: 1},
			status:  StatusFailed,
			kind:    CauseOpencodeError,
			message: OutputLimitReason + ".",
		},
		{
			name:    "provider error",
			fixture: "rate_limited.ndjson",
			attempt: attempt{runErr: exit1, exitCode: 1},
			status:  StatusFailed,
			kind:    CauseOpencodeError,
			message: "opencode reported provider.rate-limit (HTTP 429): Rate limit exceeded: 429 Too Many Requests.",
		},
		{
			name:    "auth error",
			fixture: "auth_error.ndjson",
			attempt: attempt{runErr: exit1, exitCode: 1},
			status:  StatusFailed,
			kind:    CauseOpencodeError,
			message: "opencode reported provider.auth (HTTP 401): invalid x-api-key.",
		},
		{
			// The real error, then a generic one.
			name:    "first error is the cause",
			fixture: "model_not_found.ndjson",
			attempt: attempt{runErr: exit1, exitCode: 1},
			status:  StatusFailed,
			kind:    CauseOpencodeError,
			message: "opencode reported unknown: Model not found: openai/my-model. Then: unknown: Unexpected server error. Check server logs for details.",
		},
		{
			// An unreachable base URL.
			name:    "connection refused",
			fixture: "connection_refused.ndjson",
			attempt: attempt{runErr: exit1, exitCode: 1},
			status:  StatusFailed,
			kind:    CauseOpencodeError,
			message: "opencode reported provider.transport: Cannot connect to API: ECONNREFUSED: Unable to connect. Is the computer able to access the url?",
		},
		{
			name:    "step limit",
			fixture: "step_limit.ndjson",
			opts:    Options{Timeout: 10 * time.Minute, StepLimit: 3},
			status:  StatusFailed,
			kind:    CauseStepLimit,
			message: "Reached the 3-step limit before the model finished the review (raise --max-turns to allow more steps).",
		},
		{
			name:    "answer before the step limit",
			fixture: "completed.ndjson",
			opts:    Options{Timeout: 10 * time.Minute, StepLimit: 5},
			status:  StatusCompleted,
		},
		{
			name:    "timeout",
			fixture: "timeout.ndjson",
			attempt: attempt{timedOut: true, runErr: errors.New("signal: killed"), exitCode: -1},
			elapsed: 10*time.Minute + 400*time.Millisecond,
			status:  StatusFailed,
			kind:    CauseTimeout,
			message: "Timed out after 10m0s (limit 10m0s) during step 3; the last tool call was `read` on `/builds/repo/service/store.go`.",
		},
		{
			name:    "timeout within a review budget",
			fixture: "timeout.ndjson",
			opts:    Options{Timeout: 9*time.Minute + 59987654321*time.Nanosecond, Budget: 10 * time.Minute, StepLimit: 20},
			attempt: attempt{timedOut: true, runErr: errors.New("signal: killed"), exitCode: -1},
			elapsed: 10*time.Minute + 400*time.Millisecond,
			status:  StatusFailed,
			kind:    CauseTimeout,
			message: "Timed out after 10m0s (review budget 10m0s) during step 3; the last tool call was `read` on `/builds/repo/service/store.go`.",
		},
		{
			name:    "exit non-zero without error event",
			fixture: "no_answer.ndjson",
			attempt: attempt{runErr: errors.New("exit status 2"), exitCode: 2, stderrLines: []string{"Error: Unexpected token in JSON"}},
			status:  StatusFailed,
			kind:    CauseExitError,
			message: "opencode exited with code 2 at step 2 without producing a review.",
		},
		{
			name:    "killed by a signal",
			fixture: "no_answer.ndjson",
			attempt: attempt{runErr: errors.New("signal: killed"), exitCode: -1},
			status:  StatusFailed,
			kind:    CauseExitError,
			message: "opencode failed at step 2 without producing a review: signal: killed.",
		},
		{
			// The review's context ended (SIGTERM, SIGINT) and opencode was
			// killed: the cause is the stop, not the kill.
			name:    "stopped by the review",
			fixture: "no_answer.ndjson",
			attempt: attempt{cancelled: "the review was cancelled", runErr: errors.New("signal: killed"), exitCode: -1},
			status:  StatusFailed,
			kind:    CauseCancelled,
			message: "Not finished: the review was cancelled; opencode was stopped at step 2.",
		},
		{
			name:    "stopped at the Job deadline before the first step",
			fixture: "connection_refused.ndjson",
			attempt: attempt{cancelled: "stopped at the Job deadline after the review budget ran out", runErr: errors.New("signal: killed"), exitCode: -1},
			status:  StatusFailed,
			kind:    CauseCancelled,
			message: "Not finished: stopped at the Job deadline after the review budget ran out; opencode was stopped before the first model step finished.",
		},
		{
			// Killing opencode can make it report an abort; the stop is the cause.
			name:    "stopped, with an abort error event",
			fixture: "rate_limited.ndjson",
			attempt: attempt{cancelled: "the review was cancelled", runErr: errors.New("signal: killed"), exitCode: -1},
			status:  StatusFailed,
			kind:    CauseCancelled,
			message: "Not finished: the review was cancelled; opencode was stopped at step 2.",
		},
		{
			// A final answer that arrived before the stop is a finished review.
			name:    "answer arrived as the review was stopped",
			fixture: "completed.ndjson",
			attempt: attempt{cancelled: "the review was cancelled"},
			status:  StatusCompleted,
		},
		{
			name:    "exit 0 without answer",
			fixture: "no_answer.ndjson",
			status:  StatusFailed,
			kind:    CauseNoAnswer,
			message: "opencode exited normally after 2 steps, but the model did not produce a final answer (last step finished with reason \"tool-calls\").",
		},
		{
			name:    "exit 0 mid-step",
			fixture: "timeout.ndjson",
			status:  StatusFailed,
			kind:    CauseNoAnswer,
			message: "opencode exited normally but its output ended in the middle of step 3, before the model produced a review.",
		},
		{
			name:    "answer cut off at output limit",
			fixture: "output_length.ndjson",
			status:  StatusFailed,
			kind:    CauseNoAnswer,
			message: "The model's answer was cut off at its output token limit at step 2 (finish reason: length).",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := tt.attempt
			a.transcript = ParseOutput(readFixture(t, tt.fixture))
			o := tt.opts
			if o.Timeout == 0 {
				o = opts
			}

			status, cause := classify(a, o, tt.elapsed)
			if status != tt.status {
				t.Fatalf("status = %q, want %q (cause %+v)", status, tt.status, cause)
			}
			if status == StatusCompleted {
				if cause != nil {
					t.Errorf("cause = %+v, want nil for a completed run", cause)
				}
				return
			}
			if cause.Kind != tt.kind {
				t.Errorf("kind = %q, want %q", cause.Kind, tt.kind)
			}
			if got := cause.Message(); got != tt.message {
				t.Errorf("message =\n  %q\nwant\n  %q", got, tt.message)
			}
		})
	}
}

func TestClassify_ExitErrorKeepsStderr(t *testing.T) {
	a := attempt{
		transcript:  ParseOutput(readFixture(t, "no_answer.ndjson")),
		runErr:      errors.New("exit status 2"),
		exitCode:    2,
		stderrLines: []string{"line one", "line two"},
	}
	_, cause := classify(a, Options{Timeout: time.Minute}, 0)
	if len(cause.StderrLines) != 2 {
		t.Errorf("StderrLines = %v, want the stderr tail", cause.StderrLines)
	}
}

func TestCauseMessage_NoStepFinished(t *testing.T) {
	c := Cause{Kind: CauseContextOverflow, Steps: 0, ErrorName: "provider.invalid-request", ErrorMessage: "input is too long"}
	want := "The model's context window overflowed on the first request: the skill prompt, diff and instructions alone exceed it (limit: the model's context window as known to opencode). provider.invalid-request: input is too long."
	if got := c.Message(); got != want {
		t.Errorf("Message() =\n  %q\nwant\n  %q", got, want)
	}
}

func TestCauseMessage_Attempts(t *testing.T) {
	c := Cause{Kind: CauseOpencodeError, ErrorName: "provider.internal", StatusCode: 503, ErrorMessage: "Service Unavailable", Attempts: 4}
	want := "opencode reported provider.internal (HTTP 503): Service Unavailable. Gave up after 4 attempts."
	if got := c.Message(); got != want {
		t.Errorf("Message() = %q, want %q", got, want)
	}
}

func TestEventError_Transient(t *testing.T) {
	tests := []struct {
		err  EventError
		want bool
	}{
		{EventError{Name: "provider.internal", StatusCode: 500, Message: "Internal Server Error"}, true},
		{EventError{Name: "output-limit", Message: "Response still truncated after two output token limit continuations"}, false},
		{EventError{Name: "provider.rate-limit", StatusCode: 429, Message: "Too Many Requests"}, true},
		{EventError{Name: "provider.internal", StatusCode: 529, Message: "Overloaded"}, true},
		{EventError{Name: "provider.transport", Message: "Cannot connect to API: ECONNREFUSED"}, true},
		{EventError{Name: "provider.timeout"}, true},
		{EventError{Name: "unknown", Message: "fetch failed"}, true},
		{EventError{Name: "unknown", Message: "fetch failed: ECONNRESET"}, true},
		{EventError{Name: "unknown", Message: "connect ECONNREFUSED 10.0.0.1:443"}, true},
		{EventError{Name: "unknown", Message: "getaddrinfo ENOTFOUND api.example.com"}, true},
		{EventError{Name: "provider.auth", StatusCode: 401, Message: "rate limit header missing"}, false},
		{EventError{Name: "provider.quota", StatusCode: 402}, false},
		{EventError{Name: "provider.content-filter"}, false},
		{EventError{Name: "provider.invalid-request", StatusCode: 400, Message: "Invalid request: model not found"}, false},
		{EventError{Name: "permission.rejected"}, false},
		{EventError{Name: "tool.execution"}, false},
		{EventError{Name: "aborted"}, false},
		{EventError{Name: "unknown", Message: ""}, false},
		{EventError{Name: "unknown", Message: "Agent not found: \"pergent\""}, false},
		{EventError{Name: "unknown", Message: "Unexpected server error. Check server logs for details."}, false},
		{EventError{Name: "something.new", Message: "fetch failed"}, true},
		// A provider error with no status is not judged by its message.
		{EventError{Name: "provider.invalid-output", Message: "fetch failed"}, false},
	}
	for _, tt := range tests {
		if got := tt.err.Transient(); got != tt.want {
			t.Errorf("%+v.Transient() = %v, want %v", tt.err, got, tt.want)
		}
	}
}

func TestEventError_ProviderError(t *testing.T) {
	if !(EventError{Name: "provider.internal"}).ProviderError() || (EventError{Name: "unknown"}).ProviderError() {
		t.Error("ProviderError() must be true for provider.* and false otherwise")
	}
}

func TestEventError_ContextOverflow(t *testing.T) {
	tests := []struct {
		name string
		err  EventError
		want bool
	}{
		{"message", EventError{Name: "provider.invalid-request", StatusCode: 400, Message: "This model's maximum context length is 1000 tokens. However, your messages resulted in 5000 tokens."}, true},
		{"body only", EventError{Name: "provider.invalid-request", StatusCode: 400, ResponseBody: `{"error":{"code":"context_length_exceeded"}}`}, true},
		{"no status", EventError{Name: "provider.invalid-request", Message: "prompt is too long"}, true},
		{"413", EventError{Name: "provider.invalid-request", StatusCode: 413, Message: "prompt is too long"}, true},
		{"429 is never overflow", EventError{Name: "provider.rate-limit", StatusCode: 429, Message: "too many tokens"}, false},
		{"500 is never overflow", EventError{Name: "provider.internal", StatusCode: 500, Message: "prompt is too long"}, false},
		{"other 400", EventError{Name: "provider.invalid-request", StatusCode: 400, Message: "invalid tool schema"}, false},
		{"excluded: rate limit", EventError{Name: "provider.invalid-request", StatusCode: 400, Message: "prompt is too long, rate limit"}, false},
		{"excluded: max_tokens", EventError{Name: "provider.invalid-request", StatusCode: 400, Message: "max_tokens must be less than 8192: too many tokens"}, false},
		{"excluded: please wait", EventError{Name: "provider.invalid-request", StatusCode: 400, Message: "Too many tokens, please wait before trying again."}, false},
		{"no body status line", EventError{Name: "provider.invalid-request", Message: "400 (no body)"}, true},
		{"empty", EventError{Name: "provider.invalid-request", StatusCode: 400}, false},
	}
	for _, tt := range tests {
		if got := tt.err.ContextOverflow(); got != tt.want {
			t.Errorf("%s: %+v.ContextOverflow() = %v, want %v", tt.name, tt.err, got, tt.want)
		}
	}
}

// Every pattern opencode v2.3.2 lists must compile in Go's RE2.
func TestOverflowPatternsCompile(t *testing.T) {
	if len(overflowPatterns) < 30 || len(overflowExclusions) != 5 {
		t.Errorf("got %d patterns and %d exclusions, want opencode v2.3.2's lists", len(overflowPatterns), len(overflowExclusions))
	}
}

func TestClassify_UnknownErrorTypeIsFailure(t *testing.T) {
	a := attempt{
		transcript: ParseOutput([]byte(`{"type":"error","error":{"type":"something.new"}}` + "\n")),
		runErr:     errors.New("exit status 1"), exitCode: 1,
	}
	status, cause := classify(a, Options{Timeout: time.Minute}, 0)
	if status != StatusFailed || cause.Kind != CauseOpencodeError {
		t.Fatalf("classify() = %v, %+v, want a failed opencode_error", status, cause)
	}
	msg := cause.Message()
	if !strings.Contains(msg, "something.new") || strings.Contains(msg, "(no message)") {
		t.Errorf("Message() = %q, want it to name the type and not say (no message)", msg)
	}
}

func TestClassify_ErrorBodyNeverPrinted(t *testing.T) {
	a := attempt{
		transcript: ParseOutput([]byte(`{"type":"error","error":{"type":"provider.invalid-request","message":"bad","status":400,"response":{"body":"SECRETBODY"}}}` + "\n")),
		runErr:     errors.New("exit status 1"), exitCode: 1,
	}
	_, cause := classify(a, Options{Timeout: time.Minute}, 0)
	if strings.Contains(fmt.Sprintf("%+v", cause), "SECRETBODY") {
		t.Errorf("cause = %+v, holds the response body", cause)
	}
}

func TestFormatInt(t *testing.T) {
	tests := map[int]string{0: "0", 999: "999", 1000: "1,000", 96210: "96,210", 1234567: "1,234,567", -4500: "-4,500"}
	for n, want := range tests {
		if got := FormatInt(n); got != want {
			t.Errorf("FormatInt(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestCauseMessage_TimeoutBetweenSteps(t *testing.T) {
	tests := []struct {
		cause Cause
		want  string
	}{
		{Cause{Kind: CauseTimeout, Steps: 4, Timeout: 15 * time.Second, Elapsed: 15 * time.Second, LastTool: "read", LastToolTarget: "app.go"},
			"Timed out after 15s (limit 15s) waiting for the model after step 4; the last tool call was `read` on `app.go`."},
		{Cause{Kind: CauseTimeout, Timeout: time.Minute, Elapsed: time.Minute},
			"Timed out after 1m0s (limit 1m0s) before the model's first step."},
		{Cause{Kind: CauseTimeout, Timeout: 9*time.Minute + 59987654321*time.Nanosecond, Elapsed: 10 * time.Minute},
			"Timed out after 10m0s (limit 10m0s) before the model's first step."},
		{Cause{Kind: CauseTimeout, Timeout: 4 * time.Minute, Budget: 10 * time.Minute, Elapsed: 10*time.Minute + 200*time.Millisecond, Steps: 3, EndedMidStep: true},
			"Timed out after 10m0s (review budget 10m0s) during step 3."},
	}
	for _, tt := range tests {
		if got := tt.cause.Message(); got != tt.want {
			t.Errorf("Message() = %q, want %q", got, tt.want)
		}
	}
}

// An error event that can't be read still fails the run, even after a normal
// answer and a clean exit.
func TestClassify_MalformedErrorEventIsFailure(t *testing.T) {
	answer := `{"type":"step_start","part":{"type":"step-start"}}` + "\n" +
		`{"type":"text","part":{"type":"text","text":"looks fine"}}` + "\n" +
		`{"type":"step_finish","part":{"type":"step-finish","reason":"stop"}}` + "\n"
	for name, line := range map[string]string{
		"error is a string":       `{"type":"error","error":"boom SECRETTEXT"}`,
		"error is null":           `{"type":"error","error":null}`,
		"no error key":            `{"type":"error"}`,
		"status is a string":      `{"type":"error","error":{"type":"provider.internal","message":"m","status":"500"}}`,
		"response is a string":    `{"type":"error","error":{"type":"provider.internal","message":"m","response":"oops"}}`,
		"line fails to unmarshal": `{"type":"error","part":"x","error":{"type":"unknown","message":"m"}}`,
		"event part mistyped":     `{"type":"error","part":5,"error":"boom"}`,
	} {
		t.Run(name, func(t *testing.T) {
			status, cause := classify(attempt{transcript: ParseOutput([]byte(answer + line + "\n"))}, Options{Timeout: time.Minute}, 0)
			if status != StatusFailed || cause == nil || cause.Kind != CauseOpencodeError {
				t.Fatalf("classify() = %v, %+v, want a failed opencode_error", status, cause)
			}
			if strings.Contains(cause.Message(), "SECRETTEXT") {
				t.Errorf("message = %q, holds text of the line", cause.Message())
			}
		})
	}
}

func TestParseOutput_NonErrorBadLineIsSkipped(t *testing.T) {
	got := ParseOutput([]byte(`{"type":"text","part":"x"` + "\n" + `not json` + "\n"))
	if len(got.Errors) != 0 {
		t.Errorf("Errors = %+v, want none: only an unreadable error event fails a run", got.Errors)
	}
}

// An error with a type and no message names the type once, not "X: X".
func TestCause_MessagelessErrorNamesTypeOnce(t *testing.T) {
	a := attempt{
		transcript: ParseOutput([]byte(`{"type":"error","error":{"type":"something.new"}}` + "\n" +
			`{"type":"error","error":{"type":"other.kind"}}` + "\n")),
		runErr: errors.New("exit status 1"), exitCode: 1,
	}
	_, cause := classify(a, Options{Timeout: time.Minute}, 0)
	msg := cause.Message()
	if !strings.HasPrefix(msg, "opencode reported something.new. Then: other.kind.") {
		t.Errorf("Message() = %q, want each type named once", msg)
	}
	if strings.Contains(msg, "something.new: something.new") || strings.Contains(msg, "other.kind: other.kind") {
		t.Errorf("Message() = %q, names a type twice", msg)
	}
}

func TestClassify_ProviderClassFromRootError(t *testing.T) {
	// opencode follows the real error with its own generic one.
	stdout := string(readFixture(t, "quota_openai.ndjson")) +
		`{"type":"error","error":{"type":"unknown","message":"Unexpected server error. Check server logs for details."}}` + "\n"
	a := attempt{transcript: ParseOutput([]byte(stdout)), runErr: errors.New("exit status 1"), exitCode: 1}
	_, cause := classify(a, Options{Timeout: time.Minute}, 0)
	if cause.Kind != CauseOpencodeError || cause.ProviderClass != ClassQuota || cause.ProviderStatus != 429 {
		t.Errorf("cause = %+v, want quota from the first error", cause)
	}

	a = attempt{transcript: ParseOutput(readFixture(t, "auth_error.ndjson")), runErr: errors.New("exit status 1"), exitCode: 1}
	if _, cause := classify(a, Options{Timeout: time.Minute}, 0); cause.ProviderClass != ClassAuth {
		t.Errorf("cause = %+v, want auth", cause)
	}
}

func TestCauseMessage_OutputLimit(t *testing.T) {
	tests := []struct {
		name    string
		cause   Cause
		limited bool
		message string
		prefix  string
	}{
		{
			name: "output limit",
			cause: Cause{Kind: CauseOpencodeError, ErrorName: "output-limit", ErrorMessage: "Response still truncated after two output token limit continuations",
				OtherErrors: []string{"unknown: x"}, Attempts: 2},
			limited: true,
			message: OutputLimitReason + ".",
		},
		{
			name:   "other opencode error",
			cause:  Cause{Kind: CauseOpencodeError, ErrorName: "provider.invalid-request"},
			prefix: "opencode reported provider.invalid-request",
		},
		{
			name:   "no answer",
			cause:  Cause{Kind: CauseNoAnswer, FinishReason: "length"},
			prefix: "The model's answer was cut off",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cause.OutputLimited(); got != tt.limited {
				t.Errorf("OutputLimited() = %v, want %v", got, tt.limited)
			}
			got := tt.cause.Message()
			if tt.message != "" && got != tt.message {
				t.Errorf("Message() = %q, want %q", got, tt.message)
			}
			if tt.prefix != "" && !strings.HasPrefix(got, tt.prefix) {
				t.Errorf("Message() = %q, want prefix %q", got, tt.prefix)
			}
		})
	}
}
