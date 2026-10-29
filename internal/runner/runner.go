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
	"slices"
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
	// Usage is the tokens used, summed over all attempts.
	Usage Usage
}

// Options configures a skill run.
type Options struct {
	SkillName  string
	ConfigPath string
	// Files are attached to the message, in order, with --file.
	Files    []string
	Message  string
	RepoPath string
	// Timeout bounds the whole skill run, retries included.
	Timeout time.Duration
	// Budget is the whole review's time budget when the run is part of one
	// (0 otherwise); a timeout is then reported against it.
	Budget time.Duration
	// ReviewStart is when the review began; a timeout's elapsed time counts
	// from it when set, and from the run's start otherwise.
	ReviewStart time.Time
	// StepLimit is the agent step limit pergent configured (0 if unknown).
	StepLimit int
	// ContextLimit is the explicit context limit pergent configured (0 if none).
	ContextLimit int
	// ExcludeEnv names environment variables removed from opencode's
	// environment (e.g. secrets only pergent itself needs).
	ExcludeEnv []string
	// LogPrefix, when set, is written as "<prefix> " before every progress
	// line of the run.
	LogPrefix string
}

// logPrefix returns the text written before each progress line. It is spliced
// into format strings, so any % is doubled.
func (o Options) logPrefix() string {
	if o.LogPrefix == "" {
		return ""
	}
	return strings.ReplaceAll(o.LogPrefix, "%", "%%") + " "
}

// withoutEnv returns env without the variables named in exclude.
func withoutEnv(env, exclude []string) []string {
	if len(exclude) == 0 {
		return env
	}
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if !slices.Contains(exclude, name) {
			out = append(out, kv)
		}
	}
	return out
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

// BuildCommand returns the opencode run command for message, attaching each
// of files with --file.
func BuildCommand(ctx context.Context, configPath string, files []string, message, repoPath string) *exec.Cmd {
	args := []string{
		"run",
		"--format", "json",
	}
	if configPath != "" {
		args = append(args, "--agent", config.AgentName)
	}
	for _, f := range files {
		args = append(args, "--file", f)
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
	if !opts.ReviewStart.IsZero() {
		start = opts.ReviewStart
	}
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	prefix := opts.logPrefix()
	var usage Usage
	backoff := retryBackoff
	for n := 1; ; n++ {
		a := runAttempt(ctx, opts)
		usage = usage.Add(a.transcript.Usage)
		status, cause := classify(a, opts, time.Since(start))
		if status == StatusCompleted {
			return RunResult{SkillName: opts.SkillName, Status: status, Output: a.transcript.Answer, Usage: usage}
		}

		e := a.transcript.RootError()
		retry := cause.Kind == CauseOpencodeError && e != nil && e.Transient() && n <= maxRetries
		if retry {
			if deadline, _ := ctx.Deadline(); time.Until(deadline) <= backoff {
				fmt.Fprintf(os.Stderr, prefix+"  Transient error, but not enough time left to retry: %s\n", redact.String(cause.Message()))
				retry = false
			}
		}
		if !retry {
			cause.Attempts = n
			return RunResult{SkillName: opts.SkillName, Status: status, Cause: cause, Usage: usage}
		}

		fmt.Fprintf(os.Stderr, prefix+"  Transient error: %s\n"+prefix+"  Retrying in %s (attempt %d of %d)\n",
			redact.String(cause.Message()), backoff, n+1, maxRetries+1)
		select {
		case <-ctx.Done():
			cause.Attempts = n
			return RunResult{SkillName: opts.SkillName, Status: status, Cause: cause, Usage: usage}
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
	cmd := BuildCommand(runCtx, opts.ConfigPath, opts.Files, opts.Message, opts.RepoPath)

	cmd.Env = withoutEnv(cmd.Env, opts.ExcludeEnv)

	pw := &progressWriter{buf: &bytes.Buffer{}, w: os.Stderr, prefix: opts.logPrefix()}
	if opts.StepLimit > 0 {
		pw.onStep = func(step int) {
			if step == opts.StepLimit+1 {
				fmt.Fprintf(os.Stderr, opts.logPrefix()+"  Stopping opencode: step %d is past the %d-step limit\n", step, opts.StepLimit)
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
