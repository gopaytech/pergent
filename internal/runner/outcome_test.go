package runner

import (
	"errors"
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
			message: "The model's context window overflowed after 6 steps: the last completed step used 156,000 tokens (limit: the model's context window as known to opencode). ContextOverflowError: prompt is too long: 201234 tokens > 200000 maximum.",
		},
		{
			name:    "context overflow with explicit limit",
			fixture: "context_overflow.ndjson",
			attempt: attempt{runErr: exit1, exitCode: 1},
			opts:    Options{Timeout: 10 * time.Minute, StepLimit: 20, ContextLimit: 160000},
			status:  StatusFailed,
			kind:    CauseContextOverflow,
			message: "The model's context window overflowed after 6 steps: the last completed step used 156,000 tokens (limit: 160,000 tokens, set by --context-limit). ContextOverflowError: prompt is too long: 201234 tokens > 200000 maximum.",
		},
		{
			// Captured from opencode 1.19.4 with compaction off.
			name:    "context overflow from a real run",
			fixture: "real_context_overflow.ndjson",
			attempt: attempt{runErr: exit1, exitCode: 1},
			status:  StatusFailed,
			kind:    CauseContextOverflow,
			message: "The model's context window overflowed after 1 step: the last completed step used 12,300 tokens (limit: the model's context window as known to opencode). ContextOverflowError: This model's maximum context length is 8192 tokens. However, your messages resulted in 9100 tokens.",
		},
		{
			name:    "provider error",
			fixture: "rate_limited.ndjson",
			attempt: attempt{runErr: exit1, exitCode: 1},
			status:  StatusFailed,
			kind:    CauseOpencodeError,
			message: "opencode reported APIError (HTTP 429): Rate limit exceeded: 429 Too Many Requests.",
		},
		{
			name:    "auth error",
			fixture: "auth_error.ndjson",
			attempt: attempt{runErr: exit1, exitCode: 1},
			status:  StatusFailed,
			kind:    CauseOpencodeError,
			message: "opencode reported ProviderAuthError: invalid x-api-key.",
		},
		{
			// Captured from opencode 1.19.4: the real error, then a generic one.
			name:    "first error is the cause",
			fixture: "model_not_found.ndjson",
			attempt: attempt{runErr: exit1, exitCode: 1},
			status:  StatusFailed,
			kind:    CauseOpencodeError,
			message: "opencode reported UnknownError: Model not found: openai/my-model. Then: UnknownError: Unexpected server error. Check server logs for details.",
		},
		{
			// Captured from opencode 1.19.4 with an unreachable base URL.
			name:    "connection refused",
			fixture: "connection_refused.ndjson",
			attempt: attempt{runErr: exit1, exitCode: 1},
			status:  StatusFailed,
			kind:    CauseOpencodeError,
			message: "opencode reported APIError: Cannot connect to API: ECONNREFUSED: Unable to connect. Is the computer able to access the url?",
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
	c := Cause{Kind: CauseContextOverflow, Steps: 0, ErrorName: "ContextOverflowError", ErrorMessage: "input is too long"}
	want := "The model's context window overflowed on the first request: the skill prompt, diff and instructions alone exceed it (limit: the model's context window as known to opencode). ContextOverflowError: input is too long."
	if got := c.Message(); got != want {
		t.Errorf("Message() =\n  %q\nwant\n  %q", got, want)
	}
}

func TestCauseMessage_Attempts(t *testing.T) {
	c := Cause{Kind: CauseOpencodeError, ErrorName: "APIError", StatusCode: 503, ErrorMessage: "Service Unavailable", Attempts: 4}
	want := "opencode reported APIError (HTTP 503): Service Unavailable. Gave up after 4 attempts."
	if got := c.Message(); got != want {
		t.Errorf("Message() = %q, want %q", got, want)
	}
}

func TestEventError_Transient(t *testing.T) {
	tests := []struct {
		err  EventError
		want bool
	}{
		{EventError{Name: "APIError", StatusCode: 429, Message: "Too Many Requests"}, true},
		{EventError{Name: "APIError", StatusCode: 500, Message: "Internal Server Error"}, true},
		{EventError{Name: "APIError", StatusCode: 529, Message: "Overloaded"}, true},
		{EventError{Name: "APIError", IsRetryable: true, Message: "Provider is overloaded"}, true},
		{EventError{Name: "APIError", StatusCode: 400, Message: "Invalid request: model not found"}, false},
		{EventError{Name: "APIError", StatusCode: 401, Message: "rate limit header missing"}, false},
		{EventError{Name: "UnknownError", Message: "fetch failed: ECONNRESET"}, true},
		{EventError{Name: "UnknownError", Message: "connect ECONNREFUSED 10.0.0.1:443"}, true},
		{EventError{Name: "UnknownError", Message: "getaddrinfo ENOTFOUND api.example.com"}, true},
		{EventError{Name: "UnknownError", Message: "Agent not found: \"pergent\""}, false},
		{EventError{Name: "UnknownError", Message: "Unexpected server error. Check server logs for details."}, false},
		{EventError{Name: "APIError", IsRetryable: true, Message: "Cannot connect to API: ECONNREFUSED: Unable to connect. Is the computer able to access the url?"}, true},
		{EventError{Name: "ContextOverflowError", Message: "prompt is too long"}, false},
		{EventError{Name: "ProviderAuthError", Message: "invalid api key"}, false},
		{EventError{Name: "MessageOutputLengthError"}, false},
	}
	for _, tt := range tests {
		if got := tt.err.Transient(); got != tt.want {
			t.Errorf("%+v.Transient() = %v, want %v", tt.err, got, tt.want)
		}
	}
}

func TestFormatInt(t *testing.T) {
	tests := map[int]string{0: "0", 999: "999", 1000: "1,000", 96210: "96,210", 1234567: "1,234,567", -4500: "-4,500"}
	for n, want := range tests {
		if got := formatInt(n); got != want {
			t.Errorf("formatInt(%d) = %q, want %q", n, got, want)
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
