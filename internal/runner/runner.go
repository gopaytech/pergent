package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gopaytech/pergent/internal/config"
	"github.com/gopaytech/pergent/internal/redact"
)

// RunResult is the outcome of one skill run: either the model's final answer
// (StatusCompleted, Output set) or a failure (StatusFailed, Cause set).
type RunResult struct {
	SkillName string
	Status    Status
	Output    string
	Cause     *Cause
}

// Failed builds a failed result for errors that happen around the run itself.
func Failed(skillName string, cause Cause) RunResult {
	return RunResult{SkillName: skillName, Status: StatusFailed, Cause: &cause}
}

// Options configures a skill run.
type Options struct {
	SkillName      string
	ConfigPath     string
	DiffFile       string
	PrevReviewFile string
	Message        string
	RepoPath       string
	// Timeout bounds the whole skill run, retries included.
	Timeout time.Duration
	// StepLimit is the agent step limit pergent configured (0 if unknown).
	StepLimit int
	// ContextLimit is the explicit context limit pergent configured (0 if none).
	ContextLimit int
}

// maxRetries is how many times a run that failed on a transient provider
// error is started again. opencode already retries such errors itself
// before reporting them, so these retries cover longer outages.
const maxRetries = 3

// retryBackoff is the wait before the first retry; it doubles each time.
var retryBackoff = 5 * time.Second

// stderrTailLines is how many lines of opencode's stderr a failure reports.
const stderrTailLines = 10

// waitDelay bounds how long pergent waits for opencode's output pipes to
// close after the process is killed (child processes may hold them open).
const waitDelay = 10 * time.Second

func BuildCommand(ctx context.Context, configPath string, diffFile string, prevReviewFile string, message string, repoPath string) *exec.Cmd {
	args := []string{
		"run",
		"--format", "json",
	}
	if configPath != "" {
		args = append(args, "--agent", config.AgentName)
	}
	if diffFile != "" {
		args = append(args, "--file", diffFile)
	}
	if prevReviewFile != "" {
		args = append(args, "--file", prevReviewFile)
	}
	args = append(args, "--", message)

	cmd := exec.CommandContext(ctx, "opencode", args...)
	cmd.Dir = repoPath
	cmd.WaitDelay = waitDelay

	env := os.Environ()
	if configPath != "" {
		// Ignore the reviewed repo's own opencode.json and .opencode/ (agents,
		// plugins), which would otherwise override our permissions or run code
		// with the platform token and LLM key in the environment.
		env = append(env,
			"OPENCODE_CONFIG="+configPath,
			"OPENCODE_DISABLE_PROJECT_CONFIG=true",
		)
	}
	cmd.Env = env

	return cmd
}

// Run runs one skill and always returns a result: a completed review or a
// failure with its cause. Transient provider errors are retried up to
// maxRetries times with exponential backoff while the timeout allows.
func Run(ctx context.Context, opts Options) RunResult {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	backoff := retryBackoff
	for n := 1; ; n++ {
		a := runAttempt(ctx, opts)
		status, cause := classify(a, opts, time.Since(start))
		if status == StatusCompleted {
			return RunResult{SkillName: opts.SkillName, Status: status, Output: a.transcript.Answer}
		}

		e := a.transcript.RootError()
		retry := cause.Kind == CauseOpencodeError && e != nil && e.Transient() && n <= maxRetries
		if retry {
			if deadline, _ := ctx.Deadline(); time.Until(deadline) <= backoff {
				fmt.Fprintf(os.Stderr, "  Transient error, but not enough time left to retry: %s\n", redact.String(cause.Message()))
				retry = false
			}
		}
		if !retry {
			cause.Attempts = n
			return RunResult{SkillName: opts.SkillName, Status: status, Cause: cause}
		}

		fmt.Fprintf(os.Stderr, "  Transient error: %s\n  Retrying in %s (attempt %d of %d)\n",
			redact.String(cause.Message()), backoff, n+1, maxRetries+1)
		select {
		case <-ctx.Done():
			cause.Attempts = n
			return RunResult{SkillName: opts.SkillName, Status: status, Cause: cause}
		case <-time.After(backoff):
		}
		backoff *= 2
	}
}

// runAttempt starts opencode once and collects what it reported.
func runAttempt(ctx context.Context, opts Options) attempt {
	// runCtx also stops opencode when the model goes past the step limit:
	// opencode tells the model to stop on the last step but doesn't enforce
	// it, so a model that keeps calling tools would run until the timeout.
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	cmd := BuildCommand(runCtx, opts.ConfigPath, opts.DiffFile, opts.PrevReviewFile, opts.Message, opts.RepoPath)

	pw := &progressWriter{buf: &bytes.Buffer{}, w: os.Stderr}
	if opts.StepLimit > 0 {
		pw.onStep = func(step int) {
			if step == opts.StepLimit+1 {
				fmt.Fprintf(os.Stderr, "  Stopping opencode: step %d is past the %d-step limit\n", step, opts.StepLimit)
				stop()
			}
		}
	}
	stderr := &tailBuffer{max: 64 * 1024}
	cmd.Stdout = pw
	cmd.Stderr = io.MultiWriter(os.Stderr, stderr)

	err := cmd.Run()

	if debugFile := os.Getenv("PERGENT_DEBUG_OUTPUT"); debugFile != "" {
		if writeErr := os.WriteFile(debugFile, pw.buf.Bytes(), 0644); writeErr != nil {
			fmt.Fprintf(os.Stderr, "warning: could not write debug output: %v\n", writeErr)
		}
	}

	a := attempt{
		transcript:  ParseOutput(pw.buf.Bytes()),
		timedOut:    errors.Is(ctx.Err(), context.DeadlineExceeded),
		runErr:      err,
		stderrLines: stderr.lastLines(stderrTailLines),
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		a.exitCode = exitErr.ExitCode()
	}
	a.transcript.LastToolTarget = relativeTo(opts.RepoPath, a.transcript.LastToolTarget)
	return a
}

// relativeTo shortens an absolute path inside the repo to a repo-relative one.
func relativeTo(repoPath, path string) string {
	if path == "" || !filepath.IsAbs(path) {
		return path
	}
	root, err := filepath.Abs(repoPath)
	if err != nil {
		return path
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	return rel
}
