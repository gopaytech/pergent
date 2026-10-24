package runner

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Status is how a skill run ended.
type Status string

const (
	// StatusCompleted means RunResult.Output holds the model's own final answer.
	StatusCompleted Status = "completed"
	// StatusFailed means there is no review; RunResult.Cause says why.
	StatusFailed Status = "failed"
)

// CauseKind identifies why a run failed.
type CauseKind string

const (
	CauseContextOverflow CauseKind = "context_overflow"
	CauseTimeout         CauseKind = "timeout"
	CauseOpencodeError   CauseKind = "opencode_error"
	CauseStepLimit       CauseKind = "step_limit"
	CauseExitError       CauseKind = "exit_error"
	CauseNoAnswer        CauseKind = "no_answer"
)

// Cause is a failed run's reason, with the numbers pergent could observe.
// Fields that don't apply to Kind are zero.
type Cause struct {
	Kind CauseKind

	// Steps is the number of model steps the run started.
	Steps int
	// StepLimit is the agent's step limit (0 when pergent doesn't set it).
	StepLimit int
	// LastStepTokens is the context size reported by the last finished step.
	LastStepTokens int
	// ContextLimit is the context limit pergent configured (0 means opencode
	// used the model's own limit, which pergent doesn't know).
	ContextLimit int

	// Error event from opencode (opencode_error and context_overflow).
	ErrorName    string
	ErrorMessage string
	StatusCode   int
	// OtherErrors are further error events, as "Name: message".
	OtherErrors []string
	// Attempts is how many times the run was started (more than 1 when
	// transient provider errors were retried).
	Attempts int

	// Timeout details. Timeout is the run's own deadline; Budget is the
	// review's budget (0 when the run was not part of a review). Elapsed is
	// the review's elapsed time, or the run's when there is no review.
	Timeout        time.Duration
	Budget         time.Duration
	Elapsed        time.Duration
	LastTool       string
	LastToolTarget string

	// Process exit details (exit_error).
	ExitCode    int
	ExitErr     string
	StderrLines []string

	// no_answer details.
	FinishReason      string
	EndedMidStep      bool
	Compacted         bool
	CompactedAtTokens int
	CompactedAtStep   int
}

// Message describes the cause in one or two sentences, suitable for the
// review comment and the CI log. It does not include StderrLines.
func (c Cause) Message() string {
	var msg string
	switch c.Kind {
	case CauseContextOverflow:
		// The request that overflows fails before its step starts, so Steps
		// counts the steps that came before it.
		if c.LastStepTokens > 0 {
			msg = fmt.Sprintf("The model's context window overflowed after %s: the last completed step used %s tokens (limit: %s).",
				plural(c.Steps, "step"), formatInt(c.LastStepTokens), c.contextLimitText())
		} else {
			msg = fmt.Sprintf("The model's context window overflowed on the first request: the skill prompt, diff and instructions alone exceed it (limit: %s).", c.contextLimitText())
		}
		if c.ErrorMessage != "" {
			msg += fmt.Sprintf(" %s: %s", c.ErrorName, ensurePeriod(c.ErrorMessage))
		}

	case CauseTimeout:
		var when string
		switch {
		case c.Steps == 0:
			when = "before the model's first step"
		case c.EndedMidStep:
			when = fmt.Sprintf("during step %d", c.Steps)
		default:
			when = fmt.Sprintf("waiting for the model after step %d", c.Steps)
		}
		limit := fmt.Sprintf("limit %s", roundDuration(c.Timeout))
		if c.Budget > 0 {
			limit = fmt.Sprintf("review budget %s", roundDuration(c.Budget))
		}
		msg = fmt.Sprintf("Timed out after %s (%s) %s", roundDuration(c.Elapsed), limit, when)
		if c.LastTool != "" {
			msg += fmt.Sprintf("; the last tool call was `%s`", c.LastTool)
			if c.LastToolTarget != "" {
				msg += fmt.Sprintf(" on `%s`", c.LastToolTarget)
			}
		}
		msg += "."

	case CauseOpencodeError:
		name := c.ErrorName
		if name == "" {
			name = "error"
		}
		if c.StatusCode != 0 {
			name += fmt.Sprintf(" (HTTP %d)", c.StatusCode)
		}
		msg = fmt.Sprintf("opencode reported %s: %s", name, ensurePeriod(c.ErrorMessage))
		for _, other := range c.OtherErrors {
			msg += fmt.Sprintf(" Then: %s", ensurePeriod(other))
		}
		if c.Attempts > 1 {
			msg += fmt.Sprintf(" Gave up after %d attempts.", c.Attempts)
		}

	case CauseStepLimit:
		msg = fmt.Sprintf("Reached the %d-step limit before the model finished the review (raise --max-turns to allow more steps).", c.StepLimit)

	case CauseExitError:
		if c.ExitCode > 0 {
			msg = fmt.Sprintf("opencode exited with code %d %s without producing a review.", c.ExitCode, c.atStep())
		} else {
			msg = fmt.Sprintf("opencode failed %s without producing a review: %s.", c.atStep(), c.ExitErr)
		}

	case CauseNoAnswer:
		switch {
		case c.Compacted:
			msg = fmt.Sprintf("opencode compacted the context at %s tokens after %s, and the run ended before the model produced a review.",
				formatInt(c.CompactedAtTokens), plural(c.CompactedAtStep, "step"))
		case c.FinishReason == "length":
			msg = fmt.Sprintf("The model's answer was cut off at its output token limit %s (finish reason: length).", c.atStep())
		case c.EndedMidStep:
			msg = fmt.Sprintf("opencode exited normally but its output ended in the middle of step %d, before the model produced a review.", c.Steps)
		case c.FinishReason != "":
			msg = fmt.Sprintf("opencode exited normally after %s, but the model did not produce a final answer (last step finished with reason %q).", plural(c.Steps, "step"), c.FinishReason)
		default:
			msg = fmt.Sprintf("opencode exited normally after %s, but the model did not produce a final answer.", plural(c.Steps, "step"))
		}
	default:
		msg = "The review failed for an unknown reason."
	}
	return msg
}

func (c Cause) atStep() string {
	if c.Steps == 0 {
		return "before the first model step finished"
	}
	return "at step " + fmt.Sprint(c.Steps)
}

func (c Cause) contextLimitText() string {
	if c.ContextLimit > 0 {
		return formatInt(c.ContextLimit) + " tokens, set by --context-limit"
	}
	return "the model's context window as known to opencode"
}

// transientPattern matches error messages of transient provider failures:
// rate limits, overload, and connection failures. It is only consulted when
// the error carries no HTTP status. opencode's own "Unexpected server error"
// is deliberately not matched: it is not a provider failure.
var transientPattern = regexp.MustCompile(`(?i)rate.?limit|too many requests|overloaded|service unavailable|bad gateway|gateway timeout|temporarily unavailable|fetch failed|network error|socket hang up|connection (reset|refused|closed|lost)|econnreset|econnrefused|etimedout|enotfound|eai_again|upstream connect|unable to connect`)

// Transient reports whether an opencode error event is a transient provider
// error (HTTP 429, 5xx, connection errors) that a new attempt may get past.
func (e EventError) Transient() bool {
	switch e.Name {
	case "ContextOverflowError", "ProviderAuthError", "MessageAbortedError", "ContentFilterError", "MessageOutputLengthError":
		return false
	}
	if e.StatusCode == 429 || e.StatusCode >= 500 {
		return true
	}
	if e.Name == "APIError" && e.IsRetryable {
		return true
	}
	if e.StatusCode >= 400 {
		return false
	}
	return transientPattern.MatchString(e.Message)
}

// attempt is one opencode process run.
type attempt struct {
	transcript Transcript
	timedOut   bool
	// exitCode is the process exit code; -1 when killed by a signal.
	exitCode int
	// runErr is the error from starting or waiting on the process, nil on a
	// clean exit.
	runErr      error
	stderrLines []string
}

// classify turns one attempt into a result. elapsed is the time since the
// review began (opts.ReviewStart), or since the run began outside a review.
func classify(a attempt, opts Options, elapsed time.Duration) (Status, *Cause) {
	t := a.transcript
	base := Cause{
		Steps:          t.Steps,
		StepLimit:      opts.StepLimit,
		LastStepTokens: t.LastStepTokens,
		ContextLimit:   opts.ContextLimit,
	}

	// opencode reports provider and session failures as error events and then
	// exits 1. They take precedence: the process exit adds nothing.
	if e := t.RootError(); e != nil {
		c := base
		c.ErrorName = e.Name
		c.ErrorMessage = e.Message
		c.StatusCode = e.StatusCode
		c.Kind = CauseOpencodeError
		if e.Name == "ContextOverflowError" {
			c.Kind = CauseContextOverflow
		}
		for _, other := range t.Errors {
			if other != *e {
				c.OtherErrors = append(c.OtherErrors, other.Name+": "+other.Message)
			}
		}
		return StatusFailed, &c
	}

	// On the last allowed step opencode tells the model to stop and summarise
	// what it did so far, so an answer from that step is not a finished review.
	if opts.StepLimit > 0 && t.Steps >= opts.StepLimit {
		c := base
		c.Kind = CauseStepLimit
		return StatusFailed, &c
	}

	// A final answer ends opencode's loop, so it is complete even if the
	// deadline hit while opencode was shutting down.
	if t.Answer != "" && (a.runErr == nil || a.timedOut) {
		return StatusCompleted, nil
	}

	if a.timedOut {
		c := base
		c.Kind = CauseTimeout
		c.Timeout = opts.Timeout
		c.Budget = opts.Budget
		c.Elapsed = elapsed
		c.LastTool = t.LastTool
		c.LastToolTarget = t.LastToolTarget
		c.EndedMidStep = t.InStep
		return StatusFailed, &c
	}

	if a.runErr != nil {
		c := base
		c.Kind = CauseExitError
		c.ExitCode = a.exitCode
		c.ExitErr = a.runErr.Error()
		c.StderrLines = a.stderrLines
		return StatusFailed, &c
	}

	c := base
	c.Kind = CauseNoAnswer
	c.FinishReason = t.LastFinishReason
	c.EndedMidStep = t.InStep
	c.Compacted = t.Compacted
	c.CompactedAtTokens = t.CompactedAtTokens
	c.CompactedAtStep = t.CompactedAtStep
	return StatusFailed, &c
}

func ensurePeriod(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "(no message)"
	}
	if strings.HasSuffix(s, ".") || strings.HasSuffix(s, "!") || strings.HasSuffix(s, "?") {
		return s
	}
	return s + "."
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// formatInt renders 96210 as "96,210".
func formatInt(n int) string {
	s := fmt.Sprint(n)
	if n < 0 {
		return "-" + formatInt(-n)
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func roundDuration(d time.Duration) time.Duration {
	return d.Round(time.Second)
}
