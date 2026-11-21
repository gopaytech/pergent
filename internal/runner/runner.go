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
	"syscall"
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
	// Opened are the paths the completing attempt's successful read, grep
	// and glob calls worked on (see Transcript.Opened).
	Opened []string
	// Steps are every attempt's steps in order, Attempt counted from 1.
	Steps []StepUsage
	// ToolTotals counts the tool calls of every attempt.
	ToolTotals ToolTotals
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
	// Env are NAME=value entries added to opencode's environment last, after
	// ExcludeEnv is removed: a review panel's run on its second model gets
	// that model's key as OPENCODE_API_KEY here. Never logged.
	Env []string
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

// allowedEnvNames are the inherited variables a generated-config run keeps:
// process basics, proxies, certificates, config and cache directories, and
// pergent's own key variable.
var allowedEnvNames = []string{
	"PATH", "HOME", "USER", "USERNAME", "LOGNAME", "SHELL", "TMPDIR", "TMP", "TEMP", "LANG", "TZ", "TERM",
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "no_proxy", "all_proxy",
	"SSL_CERT_FILE", "SSL_CERT_DIR", "NODE_EXTRA_CA_CERTS", "NODE_USE_SYSTEM_CA", "NODE_USE_ENV_PROXY", "NODE_TLS_REJECT_UNAUTHORIZED",
	"XDG_CONFIG_HOME", "XDG_CACHE_HOME",
	"OPENCODE_API_KEY",
}

// CloudEnvPrefixes are the cloud-provider prefixes a run keeps, so a provider
// that signs in with the caller's cloud credentials still can. A review
// panel's run on model B removes them: they are model A's.
var CloudEnvPrefixes = []string{"AWS_", "GOOGLE_", "VERTEX_", "AZURE_", "CLOUDFLARE_"}

// allowedEnvPrefixes are the cloud-provider and locale prefixes kept as well.
var allowedEnvPrefixes = append([]string{"LC_"}, CloudEnvPrefixes...)

// allowedEnv returns the entries of env a generated-config run keeps: the
// names above, the prefixes, and the names in extra.
func allowedEnv(env, extra []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if slices.Contains(allowedEnvNames, name) || slices.Contains(extra, name) ||
			slices.ContainsFunc(allowedEnvPrefixes, func(p string) bool { return strings.HasPrefix(name, p) }) {
			out = append(out, kv)
		}
	}
	return out
}

// maxRetries is how many times a run that failed on a transient provider
// error that is not the provider's (a connection that failed, say) is started
// again.
const maxRetries = 3

// maxProviderRetries is the same for a provider error. opencode already
// retried it with backoff before reporting it (a 500 took 12 requests), so one
// more start covers a short outage and no more.
const maxProviderRetries = 1

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
		// A private server: a plain run attaches to a shared one registered
		// by `opencode service start`, which ignores OPENCODE_CONFIG.
		"--standalone",
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
	// opencode starts children (language servers, tools). Put them in one
	// process group and kill the group, or a killed run leaves them running
	// and holding opencode's pipes.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		// The group id is opencode's pid (Setpgid).
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		if err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}

	env := os.Environ()
	if configPath != "" {
		// A generated config runs opencode on an allow-list, not on pergent's
		// whole environment minus a strip list: a CI job's environment holds
		// deploy tokens, cloud credentials and other secrets that a change
		// under review (through a prompt-injected tool call or a plugin) could
		// reach, and a list of names to remove misses the next one. Reasons
		// for the names pergent does not pass, found in opencode 2.3.2:
		//   - OPENCODE_CONFIG_CONTENT, OPENCODE_CLI_CONFIG_CONTENT: inline
		//     config merged over the file, so it could loosen the permissions.
		//   - OPENCODE_CONFIG_DIR: replaces the global config dir and loads it
		//     as a config directory, so its plugins run with the model key in
		//     the env.
		//   - OPENCODE_FLAGS: experimental flags that can add unvetted tools.
		//   - OPENCODE_DB: parallel runs would share one SQLite file and reach
		//     the user's real credentials, undoing the per-run data dir.
		//   - OPENCODE_TEST_HOME, OPENCODE_SIMULATE: test hooks.
		//   - OPENCODE_DISABLE_PROJECT_CONFIG, OPENCODE_CONFIG_PROJECT_DISABLE,
		//     OPENCODE_DISABLE_CLAUDE_CODE, OPENCODE_DISABLE_AUTOUPDATE: set
		//     below, so a caller's "false" cannot win.
		//   - GITHUB_TOKEN, GITLAB_TOKEN: opencode has no use for a platform
		//     token, which a change under review could otherwise reach.
		//   - OTEL_*: opencode 2 exports telemetry when these are set, and the
		//     traces can include prompts and repository text. Opt in with
		//     PERGENT_OPENCODE_ENV.
		extra, _ := config.ExtraEnvNames(os.Getenv(config.ExtraEnvVar)) // validated at startup; a bad value passes nothing extra
		env = allowedEnv(env, extra)
		// opencode reads the new project-config name first and the legacy one
		// only when it is unset, so both are set, in case a later version
		// drops either. The project config (opencode.json, .opencode/ agents
		// and plugins) would otherwise override our permissions or run code
		// with the LLM key in the environment.
		env = append(env,
			"OPENCODE_CONFIG="+configPath,
			"OPENCODE_DISABLE_PROJECT_CONFIG=true",
			"OPENCODE_CONFIG_PROJECT_DISABLE=true",
			// opencode 2 also reads CLAUDE.md files (the checkout's and
			// ~/.claude/CLAUDE.md), which agent mode never loads.
			"OPENCODE_DISABLE_CLAUDE_CODE=true",
			// No update check (a network call), and no upgrade that could
			// replace the binary in the middle of a review.
			"OPENCODE_DISABLE_AUTOUPDATE=true",
		)
	}
	// opencode 2 takes its project directory from $PWD, not the process's
	// working directory. The inherited PWD is the caller's, so without this a
	// review run from another directory would read that one instead of the
	// checkout.
	if repoPath != "" {
		if abs, err := filepath.Abs(repoPath); err == nil {
			env = append(withoutEnv(env, []string{"PWD"}), "PWD="+abs)
		}
	}
	cmd.Env = env

	return cmd
}

// Run runs one skill and always returns a result: a completed review or a
// failure with its cause. Transient errors are retried with exponential
// backoff while the timeout allows: up to maxProviderRetries times for a
// provider error, maxRetries times for any other.
func Run(ctx context.Context, opts Options) RunResult {
	start := time.Now()
	if !opts.ReviewStart.IsZero() {
		start = opts.ReviewStart
	}
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	prefix := opts.logPrefix()
	var usage Usage
	var steps []StepUsage
	var totals ToolTotals
	backoff := retryBackoff
	for n := 1; ; n++ {
		a := runAttempt(ctx, opts)
		usage = usage.Add(a.transcript.Usage)
		for _, s := range a.transcript.PerStep {
			s.Attempt = n
			steps = append(steps, s)
		}
		totals = totals.Add(a.transcript.ToolTotals)
		status, cause := classify(a, opts, time.Since(start))
		if status == StatusCompleted {
			return RunResult{SkillName: opts.SkillName, Status: status, Output: a.transcript.Answer, Usage: usage, Opened: a.transcript.Opened, Steps: steps, ToolTotals: totals}
		}

		e := a.transcript.RootError()
		limit := maxRetries
		if e != nil && e.ProviderError() {
			limit = maxProviderRetries
		}
		// A quota, billing or auth error is not started again: opencode
		// already waited out its own retries, and pergent.sh moves the review
		// to another model for these.
		retry := cause.Kind == CauseOpencodeError && e != nil && e.Transient() && !Classify(*e).Fallback() && n <= limit
		if retry {
			if deadline, _ := ctx.Deadline(); time.Until(deadline) <= backoff {
				fmt.Fprintf(os.Stderr, prefix+"  Transient error, but not enough time left to retry: %s\n", redact.String(cause.Message()))
				retry = false
			}
		}
		if !retry {
			cause.Attempts = n
			return RunResult{SkillName: opts.SkillName, Status: status, Cause: cause, Usage: usage, Steps: steps, ToolTotals: totals}
		}

		fmt.Fprintf(os.Stderr, prefix+"  Transient error: %s\n"+prefix+"  Retrying in %s (attempt %d of %d)\n",
			redact.String(cause.Message()), backoff, n+1, limit+1)
		select {
		case <-ctx.Done():
			cause.Attempts = n
			return RunResult{SkillName: opts.SkillName, Status: status, Cause: cause, Usage: usage, Steps: steps, ToolTotals: totals}
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

	dataDir, err := newDataDir()
	if err != nil {
		return attempt{runErr: err}
	}
	defer os.RemoveAll(dataDir)
	// A state directory of its own keeps the run from finding a shared
	// server's service.json, besides --standalone.
	stateDir, err := newStateDir()
	if err != nil {
		return attempt{runErr: err}
	}
	defer os.RemoveAll(stateDir)
	cmd.Env = append(withoutEnv(cmd.Env, append([]string{"XDG_DATA_HOME", "XDG_STATE_HOME"}, opts.ExcludeEnv...)),
		"XDG_DATA_HOME="+dataDir, "XDG_STATE_HOME="+stateDir)
	cmd.Env = append(cmd.Env, opts.Env...)

	pw := &progressWriter{buf: &bytes.Buffer{}, w: os.Stderr, prefix: opts.logPrefix()}
	if opts.StepLimit > 0 {
		pw.onStep = func(step int) {
			if step == opts.StepLimit+1 {
				fmt.Fprintf(os.Stderr, opts.logPrefix()+"  Stopping opencode: step %d is past the %d-step limit\n", step, opts.StepLimit)
				stop()
			}
		}
	}
	// opencode prints a provider error after its own retries; one waiting
	// will not fix (quota, billing, auth) ends the run, whatever opencode
	// does next. The log names the class only, never the provider's text.
	pw.onError = func(e EventError) {
		if c := Classify(e); c.Fallback() {
			fmt.Fprintf(os.Stderr, opts.logPrefix()+"  Stopping opencode: the provider reported a %s error\n", c)
			stop()
		}
	}
	// opencode's stderr reaches pergent's redacted and prefixed per line; the
	// tail kept for a failure is raw and is redacted where it is rendered.
	passthrough := &stderrWriter{w: os.Stderr, prefix: opts.LogPrefix}
	stderr := &tailBuffer{max: 64 * 1024}
	cmd.Stdout = pw
	cmd.Stderr = io.MultiWriter(passthrough, stderr)

	err = cmd.Run()
	passthrough.Flush()

	if debugBase := os.Getenv("PERGENT_DEBUG_OUTPUT"); debugBase != "" {
		if path, writeErr := writeDebugOutput(debugBase, pw.buf.Bytes()); writeErr != nil {
			fmt.Fprintf(os.Stderr, opts.logPrefix()+"  Warning: could not write debug output: %s\n", redact.String(writeErr.Error()))
		} else {
			fmt.Fprintf(os.Stderr, opts.logPrefix()+"  Debug output: %s\n", strings.ReplaceAll(path, "%", "%%"))
		}
	}

	a := attempt{
		transcript:  ParseOutput(pw.buf.Bytes()),
		timedOut:    errors.Is(ctx.Err(), context.DeadlineExceeded),
		runErr:      err,
		stderrLines: stderr.lastLines(stderrTailLines),
	}
	// A deadline is a timeout; any other end of the caller's context (a
	// signal, a cancel) is a stop that says why.
	if ctx.Err() != nil && !a.timedOut {
		a.cancelled = stopReason(ctx)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		a.exitCode = exitErr.ExitCode()
	}
	a.transcript.LastToolTarget = relativeTo(opts.RepoPath, a.transcript.LastToolTarget)
	// opencode resolves its working directory, so the model may read a
	// path by its real form. Classify by both forms; opts.RepoPath itself
	// stays as given.
	repoReal, tmpReal := realPath(opts.RepoPath), realPath(os.TempDir())
	for i := range a.transcript.PerStep {
		calls := a.transcript.PerStep[i].Tools
		for j := range calls {
			if calls[j].Tool == "other" {
				continue
			}
			t := pathTarget(opts.RepoPath, os.TempDir(), calls[j].path)
			if t == TargetOutside && (repoReal != opts.RepoPath || tmpReal != os.TempDir()) {
				t = pathTarget(repoReal, tmpReal, calls[j].path)
			}
			calls[j].Target = t
		}
	}
	return a
}

// stopReasoner is a context cause that says, as a phrase, why the run was
// stopped, e.g. "the review was cancelled". The review package's callers set
// one (agent mode does after SIGTERM).
type stopReasoner interface {
	StopReason() string
}

// stopReason says why ctx ended: the phrase its cause gives, or a plain
// cancellation.
func stopReason(ctx context.Context) string {
	var sr stopReasoner
	if errors.As(context.Cause(ctx), &sr) {
		return sr.StopReason()
	}
	return "the review was cancelled"
}

// newDataDir returns a new, empty directory for one opencode run's
// XDG_DATA_HOME. opencode keeps its SQLite database there and creates it on
// first start; runs started at once in a shared, fresh directory (a new CI
// container) race on that and the losers exit 1 before the first step. The
// user's auth.json is copied in, and opencode 2 imports it into the new
// database on start, so a login made with opencode 1.x still works. A login
// made with opencode 2 lives only in the user's own opencode.db and does not
// reach a run: keys must come from OPENCODE_API_KEY (generated config) or the
// provider's env or config (opencode's own config). `opencode auth export`
// prints a list that the import does not read.
func newDataDir() (string, error) {
	dir, err := os.MkdirTemp("", config.DataDirPrefix)
	if err != nil {
		return "", fmt.Errorf("creating opencode's data directory: %w", err)
	}
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "share")
	}
	auth, err := os.ReadFile(filepath.Join(base, "opencode", "auth.json"))
	if err != nil {
		return dir, nil
	}
	err = os.Mkdir(filepath.Join(dir, "opencode"), 0o700)
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, "opencode", "auth.json"), auth, 0o600)
	}
	if err != nil {
		os.RemoveAll(dir)
		return "", fmt.Errorf("copying opencode's auth.json: %w", err)
	}
	return dir, nil
}

// newStateDir returns a new, empty directory for one opencode run's
// XDG_STATE_HOME, where opencode registers a shared server. Nothing is copied
// in: the run must not find the user's.
func newStateDir() (string, error) {
	dir, err := os.MkdirTemp("", config.StateDirPrefix)
	if err != nil {
		return "", fmt.Errorf("creating opencode's state directory: %w", err)
	}
	return dir, nil
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
