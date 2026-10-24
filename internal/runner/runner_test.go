package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gopaytech/pergent/internal/config"
)

func TestBuildCommand(t *testing.T) {
	ctx := context.Background()
	cmd := BuildCommand(ctx, "/tmp/config.json", []string{"/tmp/diff.patch"}, "Review the attached diff", ".")

	found := false
	for _, env := range cmd.Env {
		if env == "OPENCODE_CONFIG=/tmp/config.json" {
			found = true
		}
	}
	if !found {
		t.Error("OPENCODE_CONFIG not set in command env")
	}
	if !slices.Contains(cmd.Env, "OPENCODE_DISABLE_PROJECT_CONFIG=true") {
		t.Error("OPENCODE_DISABLE_PROJECT_CONFIG not set in command env")
	}

	args := cmd.Args
	if args[0] != "opencode" {
		t.Errorf("command = %q, want %q", args[0], "opencode")
	}

	hasRun := false
	hasDiffFile := false
	hasFormat := false
	hasMessage := false
	hasAgent := false
	for i, arg := range args {
		if arg == "--agent" && i+1 < len(args) && args[i+1] == config.AgentName {
			hasAgent = true
		}
		if arg == "run" {
			hasRun = true
		}
		if arg == "--file" && i+1 < len(args) && args[i+1] == "/tmp/diff.patch" {
			hasDiffFile = true
		}
		if arg == "--format" && i+1 < len(args) && args[i+1] == "json" {
			hasFormat = true
		}
		if arg == "Review the attached diff" {
			hasMessage = true
		}
	}
	if !hasRun {
		t.Error("missing 'run' subcommand")
	}
	if !hasDiffFile {
		t.Error("missing --file flag for diff")
	}
	if !hasFormat {
		t.Error("missing --format json flag")
	}
	if !hasMessage {
		t.Error("missing message argument")
	}
	if !hasAgent {
		t.Errorf("missing --agent %s flag", config.AgentName)
	}
}

func TestBuildCommand_NoDiffFile(t *testing.T) {
	ctx := context.Background()
	cmd := BuildCommand(ctx, "/tmp/config.json", nil, "Say hello", ".")

	hasFile := false
	for _, arg := range cmd.Args {
		if arg == "--file" {
			hasFile = true
		}
	}
	if hasFile {
		t.Error("should not have --file flag when there are no files")
	}
}

func TestBuildCommand_NoConfig(t *testing.T) {
	ctx := context.Background()
	cmd := BuildCommand(ctx, "", []string{"/tmp/diff.patch"}, "Review", ".")

	for _, env := range cmd.Env {
		if env == "OPENCODE_CONFIG=" {
			t.Error("should not set empty OPENCODE_CONFIG")
		}
		if env == "OPENCODE_DISABLE_PROJECT_CONFIG=true" {
			t.Error("should not disable project config when using opencode's own config")
		}
	}
	if slices.Contains(cmd.Args, "--agent") {
		t.Error("should not pass --agent when using opencode's own config")
	}
}

func TestBuildCommand_MultipleFiles(t *testing.T) {
	ctx := context.Background()
	files := []string{"/tmp/diff.patch", "/tmp/changed-files.md", "/tmp/previous-findings.json"}
	cmd := BuildCommand(ctx, "/tmp/config.json", files, "Review the attached diff", ".")

	var got []string
	for i, arg := range cmd.Args {
		if arg == "--file" && i+1 < len(cmd.Args) {
			got = append(got, cmd.Args[i+1])
		}
	}
	if !slices.Equal(got, files) {
		t.Errorf("--file values = %q, want %q in order", got, files)
	}
	if n := len(cmd.Args); n < 2 || cmd.Args[n-2] != "--" || cmd.Args[n-1] != "Review the attached diff" {
		t.Errorf("args = %q, want the message last, after --", cmd.Args)
	}
}

// fakeOpencode puts an `opencode` script on PATH that replays fixtures. Call n
// of the script uses attempts[n-1] (the last entry repeats). It returns the
// file that counts how often the script ran.
type fakeAttempt struct {
	fixture string // testdata file to print on stdout
	stderr  string
	code    int
	sleep   string // seconds to hang after printing, to trigger the timeout
}

func fakeOpencode(t *testing.T, attempts ...fakeAttempt) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake opencode is a shell script")
	}
	dir := t.TempDir()
	for i, a := range attempts {
		prefix := filepath.Join(dir, fmt.Sprint(i+1))
		if a.fixture != "" {
			data, err := os.ReadFile(filepath.Join("testdata", a.fixture))
			if err != nil {
				t.Fatal(err)
			}
			os.WriteFile(prefix+".out", data, 0644)
		}
		os.WriteFile(prefix+".err", []byte(a.stderr), 0644)
		os.WriteFile(prefix+".code", []byte(fmt.Sprint(a.code)), 0644)
		if a.sleep != "" {
			os.WriteFile(prefix+".sleep", []byte(a.sleep), 0644)
		}
	}
	script := fmt.Sprintf(`#!/bin/sh
dir=%q
last=%d
n=$(cat "$dir/count" 2>/dev/null || echo 0)
n=$((n+1))
echo "$n" > "$dir/count"
[ "$n" -gt "$last" ] && n=$last
[ -f "$dir/$n.out" ] && cat "$dir/$n.out"
cat "$dir/$n.err" >&2
[ -f "$dir/$n.sleep" ] && exec sleep "$(cat "$dir/$n.sleep")"
exit "$(cat "$dir/$n.code")"
`, dir, len(attempts))
	bin := filepath.Join(dir, "bin")
	os.Mkdir(bin, 0755)
	if err := os.WriteFile(filepath.Join(bin, "opencode"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return filepath.Join(dir, "count")
}

func runCount(t *testing.T, countFile string) int {
	t.Helper()
	data, err := os.ReadFile(countFile)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return n
}

func testOptions(timeout time.Duration) Options {
	return Options{SkillName: "review", ConfigPath: "/tmp/config.json", Message: "Review", RepoPath: ".", Timeout: timeout, StepLimit: 20}
}

func shortBackoff(t *testing.T) {
	t.Helper()
	old := retryBackoff
	retryBackoff = 10 * time.Millisecond
	t.Cleanup(func() { retryBackoff = old })
}

func TestRun_Completed(t *testing.T) {
	fakeOpencode(t, fakeAttempt{fixture: "completed.ndjson"})

	got := Run(context.Background(), testOptions(time.Minute))
	if got.Status != StatusCompleted || got.Output != fixtureReview {
		t.Errorf("Run() = %+v, want the completed review", got)
	}
}

func TestRun_CompactedWithoutAnswerFails(t *testing.T) {
	fakeOpencode(t, fakeAttempt{fixture: "compacted_no_answer.ndjson"})

	got := Run(context.Background(), testOptions(time.Minute))
	if got.Status != StatusFailed || got.Cause.Kind != CauseNoAnswer || !got.Cause.Compacted {
		t.Fatalf("Run() = %+v, want a failed run caused by compaction", got)
	}
	if got.Output != "" {
		t.Errorf("Output = %q, want empty: nothing from a failed run is a review", got.Output)
	}
}

func TestRun_Timeout(t *testing.T) {
	fakeOpencode(t, fakeAttempt{fixture: "timeout.ndjson", sleep: "30"})

	start := time.Now()
	got := Run(context.Background(), testOptions(2*time.Second))
	if time.Since(start) > 15*time.Second {
		t.Errorf("Run() took %s, want it to stop soon after the timeout", time.Since(start))
	}
	if got.Status != StatusFailed || got.Cause.Kind != CauseTimeout {
		t.Fatalf("Run() = %+v, want a timeout", got)
	}
	c := got.Cause
	if c.Steps != 3 || c.LastTool != "read" || !strings.HasSuffix(c.LastToolTarget, "service/store.go") || c.Timeout != 2*time.Second {
		t.Errorf("cause = %+v, want step 3, last tool read on service/store.go, timeout 2s", c)
	}
	if got.Output != "" {
		t.Errorf("Output = %q, want empty: in-progress text is not a review", got.Output)
	}
}

func TestRun_ExitErrorReportsStderr(t *testing.T) {
	stderr := "\x1b[91mError\x1b[0m: something broke\n  at startup (index.js:1)\n"
	fakeOpencode(t, fakeAttempt{fixture: "no_answer.ndjson", stderr: stderr, code: 3})

	got := Run(context.Background(), testOptions(time.Minute))
	if got.Status != StatusFailed || got.Cause.Kind != CauseExitError || got.Cause.ExitCode != 3 {
		t.Fatalf("Run() = %+v, want exit code 3", got)
	}
	want := []string{"Error: something broke", "  at startup (index.js:1)"}
	if !slices.Equal(got.Cause.StderrLines, want) {
		t.Errorf("StderrLines = %q, want %q", got.Cause.StderrLines, want)
	}
}

func TestRun_RetriesTransientErrors(t *testing.T) {
	shortBackoff(t)
	count := fakeOpencode(t,
		fakeAttempt{fixture: "rate_limited.ndjson", code: 1},
		fakeAttempt{fixture: "rate_limited.ndjson", code: 1},
		fakeAttempt{fixture: "completed.ndjson"},
	)

	got := Run(context.Background(), testOptions(time.Minute))
	if got.Status != StatusCompleted || got.Output != fixtureReview {
		t.Errorf("Run() = %+v, want the review from the third attempt", got)
	}
	if n := runCount(t, count); n != 3 {
		t.Errorf("opencode ran %d times, want 3", n)
	}
}

func TestRun_RetriesExhausted(t *testing.T) {
	shortBackoff(t)
	count := fakeOpencode(t, fakeAttempt{fixture: "rate_limited.ndjson", code: 1})

	got := Run(context.Background(), testOptions(time.Minute))
	if got.Status != StatusFailed || got.Cause.Kind != CauseOpencodeError {
		t.Fatalf("Run() = %+v, want the provider error", got)
	}
	if n := runCount(t, count); n != maxRetries+1 {
		t.Errorf("opencode ran %d times, want %d", n, maxRetries+1)
	}
	want := "opencode reported APIError (HTTP 429): Rate limit exceeded: 429 Too Many Requests. Gave up after 4 attempts."
	if msg := got.Cause.Message(); msg != want {
		t.Errorf("message = %q, want %q", msg, want)
	}
}

func TestRun_NoRetryWithoutTimeLeft(t *testing.T) {
	old := retryBackoff
	retryBackoff = time.Hour
	t.Cleanup(func() { retryBackoff = old })
	count := fakeOpencode(t, fakeAttempt{fixture: "rate_limited.ndjson", code: 1})

	got := Run(context.Background(), testOptions(time.Minute))
	if got.Status != StatusFailed || got.Cause.Kind != CauseOpencodeError {
		t.Fatalf("Run() = %+v, want the provider error", got)
	}
	if n := runCount(t, count); n != 1 {
		t.Errorf("opencode ran %d times, want 1: the backoff doesn't fit in the timeout", n)
	}
}

func TestRun_NoRetryForPermanentErrors(t *testing.T) {
	shortBackoff(t)
	for _, fixture := range []string{"auth_error.ndjson", "context_overflow.ndjson"} {
		t.Run(fixture, func(t *testing.T) {
			count := fakeOpencode(t, fakeAttempt{fixture: fixture, code: 1})

			got := Run(context.Background(), testOptions(time.Minute))
			if got.Status != StatusFailed {
				t.Fatalf("Run() = %+v, want failed", got)
			}
			if n := runCount(t, count); n != 1 {
				t.Errorf("opencode ran %d times, want 1", n)
			}
		})
	}
}

func TestRelativeTo(t *testing.T) {
	repo := t.TempDir()
	tests := []struct{ path, want string }{
		{filepath.Join(repo, "service", "store.go"), filepath.Join("service", "store.go")},
		{"/elsewhere/file.go", "/elsewhere/file.go"},
		{"relative/file.go", "relative/file.go"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := relativeTo(repo, tt.path); got != tt.want {
			t.Errorf("relativeTo(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestRun_StopsPastStepLimit(t *testing.T) {
	// opencode asks the model to stop on the last step but doesn't enforce it.
	fakeOpencode(t, fakeAttempt{fixture: "runaway.ndjson", sleep: "30"})
	opts := testOptions(time.Minute)
	opts.StepLimit = 3

	start := time.Now()
	got := Run(context.Background(), opts)
	if time.Since(start) > 15*time.Second {
		t.Errorf("Run() took %s, want it stopped at the step limit, not the timeout", time.Since(start))
	}
	if got.Status != StatusFailed || got.Cause.Kind != CauseStepLimit || got.Cause.StepLimit != 3 {
		t.Errorf("Run() = %+v, want a step-limit failure", got)
	}
}

func TestRun_RetryUsesRootError(t *testing.T) {
	shortBackoff(t)
	// The generic follow-up error must not make a permanent error look transient.
	count := fakeOpencode(t, fakeAttempt{fixture: "model_not_found.ndjson", code: 1})

	got := Run(context.Background(), testOptions(time.Minute))
	if got.Status != StatusFailed || got.Cause.ErrorMessage != "Model not found: openai/my-model." {
		t.Fatalf("Run() = %+v, want the model-not-found error", got)
	}
	if n := runCount(t, count); n != 1 {
		t.Errorf("opencode ran %d times, want 1", n)
	}
}

func TestRun_TimeoutElapsedCountsFromReviewStart(t *testing.T) {
	fakeOpencode(t, fakeAttempt{fixture: "timeout.ndjson", sleep: "30"})

	opts := testOptions(2 * time.Second)
	opts.Budget = 10 * time.Second
	opts.ReviewStart = time.Now().Add(-8 * time.Second)
	got := Run(context.Background(), opts)
	if got.Status != StatusFailed || got.Cause.Kind != CauseTimeout {
		t.Fatalf("Run() = %+v, want a timeout", got)
	}
	c := got.Cause
	if c.Budget != 10*time.Second || c.Elapsed < 10*time.Second || c.Elapsed > 20*time.Second {
		t.Errorf("cause = %+v, want budget 10s and the review's elapsed time (about 10s)", c)
	}
	if !strings.HasPrefix(c.Message(), "Timed out after 10s (review budget 10s) ") {
		t.Errorf("Message() = %q, want it to state the review budget", c.Message())
	}
}
