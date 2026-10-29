package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/gopaytech/pergent/internal/finding"
	"github.com/gopaytech/pergent/internal/result"
	"github.com/gopaytech/pergent/internal/runner"
)

// sandboxEnv sets the opencode settings the hardened review needs to
// generate its config; without them every run fails closed.
func sandboxEnv(t *testing.T) {
	t.Helper()
	t.Setenv("OPENCODE_PROVIDER", "anthropic")
	t.Setenv("OPENCODE_MODEL", "some-model")
}

// answerRun is a RunFunc whose every run answers with the given findings.
func answerRun(t *testing.T, fs ...finding.Finding) func(context.Context, runner.Options) runner.RunResult {
	t.Helper()
	if fs == nil {
		fs = []finding.Finding{}
	}
	data, err := json.Marshal(map[string]any{"findings": fs})
	if err != nil {
		t.Fatal(err)
	}
	out := "Looked at the change.\n\n```json\n" + string(data) + "\n```\n"
	return func(context.Context, runner.Options) runner.RunResult {
		return runner.RunResult{Status: runner.StatusCompleted, Output: out}
	}
}

// resultServer records the bodies POSTed to it and replies with status.
type resultServer struct {
	*httptest.Server
	mu     sync.Mutex
	bodies [][]byte
	auth   []string
}

func newResultServer(t *testing.T, status int) *resultServer {
	t.Helper()
	s := &resultServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.bodies = append(s.bodies, body)
		s.auth = append(s.auth, r.Header.Get("Authorization"))
		s.mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(s.Close)
	return s
}

// only returns the one body the server received, decoded.
func (s *resultServer) only(t *testing.T) result.Result {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.bodies) != 1 {
		t.Fatalf("server received %d bodies, want 1", len(s.bodies))
	}
	var r result.Result
	if err := json.Unmarshal(s.bodies[0], &r); err != nil {
		t.Fatalf("body is not a result: %v\n%s", err, s.bodies[0])
	}
	return r
}

var tokenEnv = agentEnv(map[string]string{"PERGENT_RESULT_TOKEN": "result-token"})

// decodeOnly decodes data as exactly one JSON result and nothing after it.
func decodeOnly(t *testing.T, data []byte) result.Result {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	var r result.Result
	if err := dec.Decode(&r); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, data)
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("stdout has more after the result (err %v): %s", err, data)
	}
	return r
}

func TestRunAgent_StdoutIsOnlyJSON(t *testing.T) {
	sandboxEnv(t)
	dir, base, head := agentRepo(t, nil)
	var stdout, stderr bytes.Buffer

	code := runAgent(context.Background(), agentArgs(dir, base, head, oneSkill(t)), agentEnv(nil), &stdout, &stderr, answerRun(t))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr.String())
	}
	r := decodeOnly(t, stdout.Bytes())
	if r.Status != result.StatusCompleted {
		t.Errorf("status = %q, want %q", r.Status, result.StatusCompleted)
	}
	if r.Base != base || r.Head != head || r.StartedAt == "" || r.FinishedAt == "" || r.BudgetSeconds != 600 {
		t.Errorf("result meta = base %q head %q started %q finished %q budget %d", r.Base, r.Head, r.StartedAt, r.FinishedAt, r.BudgetSeconds)
	}
	if !strings.HasPrefix(stderr.String(), "Reviewing 1 changed file(s) with 1 skill(s)") {
		t.Errorf("stderr = %q, want the progress lines without timestamps, as in CI mode", stderr.String())
	}
}

func TestRunAgent_HTTPCompleted(t *testing.T) {
	sandboxEnv(t)
	dir, base, head := agentRepo(t, nil)
	srv := newResultServer(t, http.StatusOK)
	f := finding.Finding{File: "a.go", Line: 3, Severity: finding.Suggestion, Title: "Document A", Explanation: "A is exported but has no doc comment."}
	answer := answerRun(t, f)
	var mu sync.Mutex
	var opts []runner.Options
	run := func(ctx context.Context, o runner.Options) runner.RunResult {
		mu.Lock()
		opts = append(opts, o)
		mu.Unlock()
		return answer(ctx, o)
	}
	args := []string{"--repo-path", dir, "--base", base, "--head", head, "--skills-dir", oneSkill(t), "--result-url", srv.URL}
	var stdout, stderr bytes.Buffer

	code := runAgent(context.Background(), args, tokenEnv, &stdout, &stderr, run)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want nothing without --result -", stdout.String())
	}
	r := srv.only(t)
	if srv.auth[0] != "Bearer result-token" {
		t.Errorf("Authorization = %q, want the bearer token", srv.auth[0])
	}
	if r.SchemaVersion != 1 || r.Status != result.StatusCompleted {
		t.Fatalf("schema_version %d status %q, want 1 completed", r.SchemaVersion, r.Status)
	}
	if len(r.Skills) != 1 || len(r.Skills[0].Findings) != 1 {
		t.Fatalf("skills = %+v, want one with one finding", r.Skills)
	}
	id := r.Skills[0].Findings[0].ID
	if id == "" {
		t.Fatal("finding has no id")
	}
	if !strings.Contains(r.Comment.Markdown, "`"+id+"`") {
		t.Errorf("comment.markdown does not show the finding's id %q:\n%s", id, r.Comment.Markdown)
	}
	if r.Comment.FindingsBlock == "" || !strings.HasSuffix(r.Comment.Markdown, r.Comment.FindingsBlock) {
		t.Errorf("findings_block %q is not the comment's last line", r.Comment.FindingsBlock)
	}
	for _, o := range opts {
		for _, name := range []string{"PERGENT_RESULT_TOKEN", "GITHUB_TOKEN", "GITLAB_TOKEN"} {
			if !slices.Contains(o.ExcludeEnv, name) {
				t.Errorf("run ExcludeEnv = %v, want %s kept from opencode", o.ExcludeEnv, name)
			}
		}
		if o.LogPrefix == "" {
			t.Error("run has no log prefix: the review is not hardened")
		}
	}
}

func TestRunAgent_ErrorResultDelivered(t *testing.T) {
	dir, base, head := agentRepo(t, nil)
	gitIn(t, dir, "checkout", "-q", base)
	srv := newResultServer(t, http.StatusOK)
	args := []string{"--repo-path", dir, "--base", base, "--head", head, "--skills-dir", oneSkill(t), "--result-url", srv.URL}
	var stdout, stderr bytes.Buffer

	code := runAgent(context.Background(), args, tokenEnv, &stdout, &stderr, answerRun(t))
	if code != 3 {
		t.Fatalf("exit code = %d, want 3; stderr:\n%s", code, stderr.String())
	}
	r := srv.only(t)
	if r.Status != result.StatusError || r.Error == nil || r.Error.Code != result.CodeHeadMismatch {
		t.Errorf("result = status %q error %+v, want an error result with code %s", r.Status, r.Error, result.CodeHeadMismatch)
	}
	if r.Base != base || r.Head != head {
		t.Errorf("result base %q head %q, want the given commits", r.Base, r.Head)
	}
}

func TestRunAgent_UsageExit(t *testing.T) {
	dir, base, head := agentRepo(t, nil)
	var stdout, stderr bytes.Buffer

	code := runAgent(context.Background(), agentArgs(dir, base, head, oneSkill(t), "--local"), agentEnv(nil), &stdout, &stderr, answerRun(t))
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	r := decodeOnly(t, stdout.Bytes())
	if r.Error == nil || r.Error.Code != result.CodeUsage {
		t.Errorf("result error = %+v, want code %s", r.Error, result.CodeUsage)
	}
	if !strings.Contains(stderr.String(), "--local") {
		t.Errorf("stderr = %q, want the usage error", stderr.String())
	}
}

func TestRunAgent_UsageWithoutTarget(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runAgent(context.Background(), []string{"--local"}, agentEnv(nil), &stdout, &stderr, answerRun(t))
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want nothing without a target", stdout.String())
	}
}

// A usage error found while preparing (here duplicate skill names) is still
// a usage error: exit 2 with the error result delivered.
func TestRunAgent_PrepareUsageExit(t *testing.T) {
	dir, base, head := agentRepo(t, nil)
	skills := skillsDir(t, map[string]string{
		"a.md": "---\nname: review\n---\nOne.\n",
		"b.md": "---\nname: review\n---\nTwo.\n",
	})
	var stdout, stderr bytes.Buffer

	code := runAgent(context.Background(), agentArgs(dir, base, head, skills), agentEnv(nil), &stdout, &stderr, answerRun(t))
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr:\n%s", code, stderr.String())
	}
	if r := decodeOnly(t, stdout.Bytes()); r.Error == nil || r.Error.Code != result.CodeUsage {
		t.Errorf("result error = %+v, want code %s", r.Error, result.CodeUsage)
	}
}

func TestRunAgent_DeliveryFailure(t *testing.T) {
	sandboxEnv(t)
	dir, base, head := agentRepo(t, nil)
	srv := newResultServer(t, http.StatusUnauthorized)
	args := []string{"--repo-path", dir, "--base", base, "--head", head, "--skills-dir", oneSkill(t), "--result-url", srv.URL}
	var stdout, stderr bytes.Buffer

	code := runAgent(context.Background(), args, tokenEnv, &stdout, &stderr, answerRun(t))
	if code != 4 {
		t.Fatalf("exit code = %d, want 4; stderr:\n%s", code, stderr.String())
	}
	if strings.Contains(stderr.String(), "result-token") {
		t.Error("stderr shows the result token")
	}
}

// A prepare error whose error result can't be delivered exits 4.
func TestRunAgent_ErrorResultDeliveryFailure(t *testing.T) {
	dir, base, head := agentRepo(t, nil)
	gitIn(t, dir, "checkout", "-q", base)
	srv := newResultServer(t, http.StatusUnauthorized)
	args := []string{"--repo-path", dir, "--base", base, "--head", head, "--skills-dir", oneSkill(t), "--result-url", srv.URL}
	var stdout, stderr bytes.Buffer

	if code := runAgent(context.Background(), args, tokenEnv, &stdout, &stderr, answerRun(t)); code != 4 {
		t.Fatalf("exit code = %d, want 4; stderr:\n%s", code, stderr.String())
	}
}

func TestRunAgent_AllSkillsFailedIsExit0(t *testing.T) {
	sandboxEnv(t)
	dir, base, head := agentRepo(t, nil)
	run := func(context.Context, runner.Options) runner.RunResult {
		return runner.RunResult{Status: runner.StatusFailed, Cause: &runner.Cause{Kind: runner.CauseNoAnswer}}
	}
	var stdout, stderr bytes.Buffer

	code := runAgent(context.Background(), agentArgs(dir, base, head, oneSkill(t)), agentEnv(nil), &stdout, &stderr, run)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr.String())
	}
	r := decodeOnly(t, stdout.Bytes())
	if r.Status != result.StatusCompleted || len(r.Skills) != 1 || r.Skills[0].Verdict != "failed" {
		t.Errorf("result = status %q skills %+v, want completed with one failed skill", r.Status, r.Skills)
	}
}

// cancelledMessage is the failure of a skill whose one batch never started
// because a signal cancelled the review. review falls back to "the review was
// cancelled (<cause>)" for a cause without StopReason.
const cancelledMessage = "Not reviewed: the review was cancelled; 0 of 1 batches finished."

func TestRunAgent_CancelReason(t *testing.T) {
	sandboxEnv(t)
	dir, base, head := agentRepo(t, nil)
	srv := newResultServer(t, http.StatusOK)
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(signalCause(time.Second, 10*time.Minute))
	args := []string{"--repo-path", dir, "--base", base, "--head", head, "--skills-dir", oneSkill(t), "--result-url", srv.URL}
	var stdout, stderr bytes.Buffer

	code := runAgent(ctx, args, tokenEnv, &stdout, &stderr, answerRun(t))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr.String())
	}
	r := srv.only(t)
	if len(r.Skills) != 1 || len(r.Skills[0].Failures) != 1 {
		t.Fatalf("skills = %+v, want one with one failure", r.Skills)
	}
	if msg := r.Skills[0].Failures[0].Message; msg != cancelledMessage {
		t.Errorf("failure message = %q, want %q", msg, cancelledMessage)
	}
}

// A real SIGTERM during a run cancels the review, and the result is still
// delivered.
func TestRunAgent_SIGTERM(t *testing.T) {
	sandboxEnv(t)
	dir, base, head := agentRepo(t, nil)
	srv := newResultServer(t, http.StatusOK)
	skills := skillsDir(t, map[string]string{
		"a.md": "---\nname: first\n---\nOne.\n",
		"b.md": "---\nname: second\n---\nTwo.\n",
	})
	var once sync.Once
	run := func(ctx context.Context, _ runner.Options) runner.RunResult {
		once.Do(func() {
			if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
				t.Errorf("sending SIGTERM: %v", err)
			}
		})
		select {
		case <-ctx.Done():
		case <-time.After(10 * time.Second):
			t.Error("the run's context was not cancelled after SIGTERM")
		}
		return runner.RunResult{Status: runner.StatusFailed, Cause: &runner.Cause{Kind: runner.CauseTimeout}}
	}
	args := []string{"--repo-path", dir, "--base", base, "--head", head, "--skills-dir", skills, "--result-url", srv.URL, "--concurrency", "1"}
	var stdout, stderr bytes.Buffer

	code := runAgent(context.Background(), args, tokenEnv, &stdout, &stderr, run)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr.String())
	}
	r := srv.only(t)
	var messages []string
	for _, s := range r.Skills {
		if s.Verdict != "failed" {
			t.Errorf("skill %s verdict = %q, want failed", s.Name, s.Verdict)
		}
		for _, f := range s.Failures {
			messages = append(messages, f.Message)
		}
	}
	if !slices.Contains(messages, cancelledMessage) {
		t.Errorf("failure messages = %q, want %q", messages, cancelledMessage)
	}
}

func TestSignalCause(t *testing.T) {
	for _, tt := range []struct {
		elapsed time.Duration
		want    string
	}{
		{time.Minute, "the review was cancelled"},
		{10 * time.Minute, "stopped at the Job deadline after the review budget ran out"},
		{11 * time.Minute, "stopped at the Job deadline after the review budget ran out"},
	} {
		if got := signalCause(tt.elapsed, 10*time.Minute).StopReason(); got != tt.want {
			t.Errorf("signalCause(%s, 10m).StopReason() = %q, want %q", tt.elapsed, got, tt.want)
		}
	}
}

func TestReviewClock(t *testing.T) {
	var c reviewClock
	if got := c.stopCause().StopReason(); got != cancelledReason {
		t.Errorf("before the review started: %q, want %q", got, cancelledReason)
	}
	c.begin(0)
	if got := c.stopCause().StopReason(); got != deadlineReason {
		t.Errorf("after a zero budget: %q, want %q", got, deadlineReason)
	}
	c.begin(10 * time.Minute)
	if got := c.stopCause().StopReason(); got != cancelledReason {
		t.Errorf("within the budget: %q, want %q", got, cancelledReason)
	}
}

// A result over 10 MB even without checks and summaries becomes an internal
// error result.
func TestRunAgent_OversizeResult(t *testing.T) {
	sandboxEnv(t)
	dir, base, head := agentRepo(t, nil)
	// JSON writes each "<" as \u003c, six bytes, so the finding alone is over
	// the limit while the test handles a sixth of the data (it is slow under
	// -race otherwise).
	f := finding.Finding{File: "a.go", Line: 3, Severity: finding.Suggestion, Title: "Huge",
		Explanation: strings.Repeat("<", result.MaxSize/6+1)}
	var stdout, stderr bytes.Buffer

	code := runAgent(context.Background(), agentArgs(dir, base, head, oneSkill(t)), agentEnv(nil), &stdout, &stderr, answerRun(t, f))
	if code != 3 {
		t.Fatalf("exit code = %d, want 3; stderr:\n%.2000s", code, stderr.String())
	}
	r := decodeOnly(t, stdout.Bytes())
	if r.Status != result.StatusError || r.Error == nil || r.Error.Code != result.CodeInternal || r.Error.Message != oversizeMessage {
		t.Errorf("result = status %q error %+v, want %s %q", r.Status, r.Error, result.CodeInternal, oversizeMessage)
	}
}

// A signal that arrives while a normal-mode HTTP delivery hangs still ends
// the run within the short deadline, as a failed delivery.
func TestRunAgent_SignalDuringHungDelivery(t *testing.T) {
	sandboxEnv(t)
	dir, base, head := agentRepo(t, nil)
	old := result.ShortDeadline
	result.ShortDeadline = 500 * time.Millisecond
	t.Cleanup(func() { result.ShortDeadline = old })
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
			t.Errorf("sending SIGTERM: %v", err)
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })
	args := []string{"--repo-path", dir, "--base", base, "--head", head, "--skills-dir", oneSkill(t), "--result-url", srv.URL}
	var stdout, stderr bytes.Buffer

	start := time.Now()
	code := runAgent(context.Background(), args, tokenEnv, &stdout, &stderr, answerRun(t))
	if code != 4 {
		t.Fatalf("exit code = %d, want 4; stderr:\n%s", code, stderr.String())
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("runAgent took %s, want it to stop delivering at the short deadline", elapsed)
	}
}

// prepareAgent's log lines go to runAgent's stderr, not the global logger.
func TestRunAgent_PrepareLogsToStderr(t *testing.T) {
	sandboxEnv(t)
	dir, base, head := agentRepo(t, func(dir string) {
		writeFile(t, dir, "sub/AGENTS.md", "Planted.\n")
	})
	global := captureLog(t)
	args := agentArgs(dir, base, head, oneSkill(t), "--scrub-instructions", "--previous", filepath.Join(t.TempDir(), "missing.json"))
	var stdout, stderr bytes.Buffer

	if code := runAgent(context.Background(), args, agentEnv(nil), &stdout, &stderr, answerRun(t)); code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr.String())
	}
	for _, want := range []string{`--scrub-instructions: deleted "sub/AGENTS.md"`, "warning: ignoring --previous"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr = %q, want %q", stderr.String(), want)
		}
	}
	if global.Len() != 0 {
		t.Errorf("the global logger got %q, want nothing", global.String())
	}
}

func TestRunAgent_GeneratedFileReviewed(t *testing.T) {
	sandboxEnv(t)
	dir, base, head := agentRepo(t, func(dir string) {
		writeFile(t, dir, "gen.go", "// Code generated by a tool. DO NOT EDIT.\npackage a\n")
	})
	var stdout, stderr bytes.Buffer

	code := runAgent(context.Background(), agentArgs(dir, base, head, oneSkill(t)), agentEnv(nil), &stdout, &stderr, answerRun(t))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr.String())
	}
	r := decodeOnly(t, stdout.Bytes())
	if len(r.Skills) != 1 || !slices.Contains(r.Skills[0].Files.Reviewed, "gen.go") {
		t.Errorf("skills = %+v, want gen.go reviewed", r.Skills)
	}
}
