package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/gopaytech/pergent/graph"
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
	if !strings.Contains(r.Comment.Markdown, "id <code>"+id+"</code>") {
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

func TestRunAgent_CarriedFilesInResult(t *testing.T) {
	sandboxEnv(t)
	dir, base, head := agentRepo(t, func(dir string) {
		writeFile(t, dir, "b.go", "package a\n")
	})
	carried := filepath.Join(t.TempDir(), "carried.json")
	writeFile(t, filepath.Dir(carried), "carried.json", `{"review": ["a.go"]}`)
	var stdout, stderr bytes.Buffer

	code := runAgent(context.Background(), agentArgs(dir, base, head, oneSkill(t), "--carried", carried), agentEnv(nil), &stdout, &stderr, answerRun(t))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr.String())
	}
	r := decodeOnly(t, stdout.Bytes())
	if len(r.Skills) != 1 || !slices.Equal(r.Skills[0].Files.Carried, []string{"a.go"}) || !slices.Equal(r.Skills[0].Files.Reviewed, []string{"b.go"}) {
		t.Errorf("skills = %+v, want a.go carried and b.go reviewed", r.Skills)
	}
}

func TestRunAgent_DismissalsLogged(t *testing.T) {
	sandboxEnv(t)
	dir, base, head := agentRepo(t, nil)
	dismissals := filepath.Join(t.TempDir(), "dismissals.json")
	writeFile(t, filepath.Dir(dismissals), "dismissals.json", `{
  "security": [
    {"file": "a.go", "title": "One", "severity": "blocking", "reason": "r"},
    {"file": "a.go", "title": "Two", "severity": "suggestion", "reason": "r"}
  ],
  "review": [{"file": "a.go", "title": "Three", "severity": "blocking", "reason": "r"}],
  "quiet": []
}`)
	var stdout, stderr bytes.Buffer

	code := runAgent(context.Background(), agentArgs(dir, base, head, oneSkill(t), "--dismissals", dismissals), agentEnv(nil), &stdout, &stderr, answerRun(t))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "dismissals: review=1 security=2\n") {
		t.Errorf("stderr = %q, want a dismissals line with skills sorted by name", stderr.String())
	}
	// The result has no field for them: not even those of the skill that ran.
	// The run's attachment list names pergent's own file, rejected-findings.json:
	// that name is allowed, any other "rejected" would be a leak.
	out := strings.ReplaceAll(stdout.String(), "rejected-findings.json", "")
	for _, s := range []string{"dismiss", "rejected", "Three", `"r"`} {
		if strings.Contains(out, s) {
			t.Errorf("result contains %q, want no dismissals in it:\n%s", s, stdout.String())
		}
	}
}

func TestRunAgent_DismissalsReachTheRun(t *testing.T) {
	sandboxEnv(t)
	dir, base, head := agentRepo(t, nil)
	dismissals := filepath.Join(t.TempDir(), "dismissals.json")
	writeFile(t, filepath.Dir(dismissals), "dismissals.json", `{
  "review": [{"file": "a.go", "title": "Three", "severity": "suggestion", "reason": "Why", "actor": "someone"}],
  "other": [{"file": "a.go", "title": "Elsewhere", "severity": "suggestion", "reason": "r"}]
}`)
	var stdout, stderr bytes.Buffer

	// The run's files are removed once it returns, so read them inside it.
	var attached []string
	inner := answerRun(t)
	run := func(ctx context.Context, opts runner.Options) runner.RunResult {
		for _, p := range opts.Files {
			if filepath.Base(p) == "rejected-findings.json" {
				data, err := os.ReadFile(p)
				if err != nil {
					t.Error(err)
				}
				attached = append(attached, string(data))
			}
		}
		return inner(ctx, opts)
	}

	code := runAgent(context.Background(), agentArgs(dir, base, head, oneSkill(t), "--dismissals", dismissals), agentEnv(nil), &stdout, &stderr, run)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr.String())
	}
	if len(attached) != 1 {
		t.Fatalf("%d runs had rejected-findings.json, want 1", len(attached))
	}
	var got struct {
		Rejected []finding.Rejected `json:"rejected"`
	}
	if err := json.Unmarshal([]byte(attached[0]), &got); err != nil {
		t.Fatal(err)
	}
	if want := []finding.Rejected{{File: "a.go", Title: "Three", Severity: "suggestion", Reason: "Why"}}; !slices.Equal(got.Rejected, want) {
		t.Errorf("rejected = %+v, want only the running skill's entry", got.Rejected)
	}
	if strings.Contains(attached[0], "actor") || strings.Contains(attached[0], "someone") {
		t.Errorf("rejected-findings.json = %s, want fields that are not part of an entry left out", attached[0])
	}
}

func TestRunAgent_DescriptionReachesTheRun(t *testing.T) {
	sandboxEnv(t)
	dir, base, head := agentRepo(t, nil)
	path := filepath.Join(t.TempDir(), "change.md")
	writeFile(t, filepath.Dir(path), "change.md", "Fix the limiter")
	var stdout, stderr bytes.Buffer

	// The run's files are removed once it returns, so read them inside it.
	var attached []string
	inner := answerRun(t)
	run := func(ctx context.Context, opts runner.Options) runner.RunResult {
		for _, p := range opts.Files {
			if filepath.Base(p) == "change-description.md" {
				data, err := os.ReadFile(p)
				if err != nil {
					t.Error(err)
				}
				attached = append(attached, string(data))
			}
		}
		return inner(ctx, opts)
	}

	code := runAgent(context.Background(), agentArgs(dir, base, head, oneSkill(t), "--change-description", path), agentEnv(nil), &stdout, &stderr, run)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr.String())
	}
	if !slices.Equal(attached, []string{"Fix the limiter"}) {
		t.Errorf("attached = %q, want the description on the one run", attached)
	}
}

// impactRun answers every run with no findings and records which runs were the
// impact check, told apart by the log prefix a hardened review gives them.
type impactRun struct {
	mu     sync.Mutex
	checks int
	total  int
}

func (r *impactRun) run(t *testing.T) func(context.Context, runner.Options) runner.RunResult {
	inner := answerRun(t)
	return func(ctx context.Context, o runner.Options) runner.RunResult {
		r.mu.Lock()
		r.total++
		if strings.HasPrefix(o.LogPrefix, "[impact ") {
			r.checks++
		}
		r.mu.Unlock()
		res := inner(ctx, o)
		if strings.HasPrefix(o.LogPrefix, "[impact ") {
			// The check read a clone: the message lists them as "name" at "path".
			for _, m := range regexp.MustCompile(`" at ("(?:[^"\\]|\\.)*")`).FindAllStringSubmatch(o.Message, -1) {
				if p, err := strconv.Unquote(m[1]); err == nil {
					res.Opened = append(res.Opened, filepath.Join(p, "main.go"))
				}
			}
		}
		return res
	}
}

func skillNamed(r result.Result, name string) (result.Skill, bool) {
	for _, s := range r.Skills {
		if s.Name == name {
			return s, true
		}
	}
	return result.Skill{}, false
}

func TestAgentRunsImpact(t *testing.T) {
	sandboxEnv(t)
	dir, base, head := agentRepo(t, nil)
	clone := t.TempDir()
	writeFile(t, clone, "main.go", "package main\n")
	writeFile(t, clone, ".env", "KEY=1\n")
	linked := writeLinkedFile(t, `[{"name":"billing","path":`+jsonString(clone)+`,"watched":["a.go"],"can_block":true}]`)
	var stdout, stderr bytes.Buffer
	var fake impactRun

	code := runAgent(context.Background(), agentArgs(dir, base, head, oneSkill(t), "--linked-repos", linked), agentEnv(nil), &stdout, &stderr, fake.run(t))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr.String())
	}
	r := decodeOnly(t, stdout.Bytes())
	if len(r.Skills) != 2 || r.Skills[0].Name != "review" || r.Skills[1].Name != "impact" {
		t.Fatalf("skills = %+v, want review then impact", r.Skills)
	}
	imp := r.Skills[1]
	if imp.Verdict != "no_blocking_issues" || len(imp.Files.Reviewed) != 0 || len(imp.Files.NotReviewed) != 0 {
		t.Errorf("impact = %+v, want clean with no files", imp)
	}
	if !slices.Contains(imp.Notes, "Impact: checked billing, nothing found") {
		t.Errorf("notes = %q, want the clean note", imp.Notes)
	}
	if fake.checks != 1 {
		t.Errorf("%d impact runs, want 1", fake.checks)
	}
	if _, err := os.Stat(filepath.Join(clone, ".env")); !os.IsNotExist(err) {
		t.Errorf(".env is still in the clone: the scrub did not run before the review")
	}
	if !strings.Contains(r.Comment.Markdown, "impact") {
		t.Errorf("the comment does not show impact:\n%s", r.Comment.Markdown)
	}
}

func TestAgentBadLinkedFileFailsImpact(t *testing.T) {
	sandboxEnv(t)
	dir, base, head := agentRepo(t, nil)
	linked := writeLinkedFile(t, `{"name":`)
	var stdout, stderr bytes.Buffer
	var fake impactRun

	code := runAgent(context.Background(), agentArgs(dir, base, head, oneSkill(t), "--linked-repos", linked), agentEnv(nil), &stdout, &stderr, fake.run(t))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr.String())
	}
	r := decodeOnly(t, stdout.Bytes())
	review, ok := skillNamed(r, "review")
	imp, ok2 := skillNamed(r, "impact")
	if !ok || !ok2 || len(r.Skills) != 2 {
		t.Fatalf("skills = %+v, want review and impact", r.Skills)
	}
	if review.Verdict != "no_blocking_issues" {
		t.Errorf("review = %q, want the normal skill to run", review.Verdict)
	}
	if imp.Verdict != "failed" || len(imp.Failures) != 1 || len(imp.Failures[0].Files) != 0 || len(imp.Files.NotReviewed) != 0 {
		t.Errorf("impact = %+v, want failed with one failure and no file", imp)
	}
	if fake.checks != 0 {
		t.Errorf("%d impact runs, want none", fake.checks)
	}
}

// A refused path is a failed link too, not an absent one.
func TestAgentRefusedLinkedPathFailsImpact(t *testing.T) {
	sandboxEnv(t)
	dir, base, head := agentRepo(t, nil)
	linked := writeLinkedFile(t, `[{"name":"billing","path":"relative/clone"}]`)
	var stdout, stderr bytes.Buffer
	var fake impactRun

	code := runAgent(context.Background(), agentArgs(dir, base, head, oneSkill(t), "--linked-repos", linked), agentEnv(nil), &stdout, &stderr, fake.run(t))
	r := decodeOnly(t, stdout.Bytes())
	imp, ok := skillNamed(r, "impact")
	if code != 0 || !ok || imp.Verdict != "failed" || !strings.Contains(imp.Failures[0].Message, "billing") {
		t.Errorf("exit %d, impact = %+v (%v), want failed naming billing", code, imp, ok)
	}
}

func TestAgentNoLinkedReposUnchanged(t *testing.T) {
	sandboxEnv(t)
	dir, base, head := agentRepo(t, nil)
	var stdout, stderr bytes.Buffer
	var fake impactRun

	code := runAgent(context.Background(), agentArgs(dir, base, head, oneSkill(t)), agentEnv(nil), &stdout, &stderr, fake.run(t))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr.String())
	}
	r := decodeOnly(t, stdout.Bytes())
	if _, has := skillNamed(r, "impact"); has || len(r.Skills) != 1 || fake.total != 1 {
		t.Errorf("skills = %+v, runs = %d, want the one skill and one run", r.Skills, fake.total)
	}
}

// agentGraph is a v1 graph of agentRepo's base: B in b.go and P in k.pem
// call A in a.go, which holds line 1.
func agentGraph(t *testing.T) string {
	t.Helper()
	g := &graph.File{
		Format: graph.Format, Provider: "graphify", ProviderVersion: "0.9.112",
		Commit: "0123456789abcdef0123456789abcdef01234567", BuiltAt: "2026-11-19T10:00:00Z",
		Nodes: []graph.Node{
			{ID: "n1", Kind: graph.KindFunction, Name: "A", File: "a.go", Line: 1},
			{ID: "n2", Kind: graph.KindFunction, Name: "B", File: "b.go", Line: 1},
			{ID: "n3", Kind: graph.KindFunction, Name: "P", File: "k.pem", Line: 2},
		},
		Edges: []graph.Edge{{From: "n2", To: "n1", Kind: graph.EdgeCalls}, {From: "n3", To: "n1", Kind: graph.EdgeCalls}},
	}
	return writeGraphFile(t, g)
}

func TestPrepareAgent_GraphMissing(t *testing.T) {
	sandboxEnv(t)
	dir, base, head := agentRepo(t, nil)
	in := mustParse(t, agentArgs(dir, base, head, oneSkill(t), "--graph", filepath.Join(t.TempDir(), "missing.json")))
	var buf bytes.Buffer
	if e := prepareAgent(&in, log.New(&buf, "", 0)); e != nil {
		t.Fatalf("prepareAgent() error = %+v, want none", e)
	}
	if in.Graph != nil {
		t.Errorf("Graph = %+v, want nil", in.Graph)
	}
	if !strings.Contains(buf.String(), "graph: skipped: ") {
		t.Errorf("log = %q, want graph: skipped", buf.String())
	}
}

func TestRunAgent_GraphReachesTheRun(t *testing.T) {
	sandboxEnv(t)
	dir, base, head := agentRepo(t, nil)
	var stdout, stderr bytes.Buffer
	var hints []string
	inner := answerRun(t)
	run := func(ctx context.Context, opts runner.Options) runner.RunResult {
		for _, p := range opts.Files {
			if filepath.Base(p) == "graph-context.md" {
				data, err := os.ReadFile(p)
				if err != nil {
					t.Error(err)
				}
				hints = append(hints, string(data))
			}
		}
		return inner(ctx, opts)
	}
	code := runAgent(context.Background(), agentArgs(dir, base, head, oneSkill(t), "--graph", agentGraph(t)), agentEnv(nil), &stdout, &stderr, run)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr.String())
	}
	if len(hints) != 1 {
		t.Fatalf("%d runs had graph-context.md, want 1", len(hints))
	}
	if !strings.Contains(hints[0], "- B  b.go:1") {
		t.Errorf("hint = %q, want the caller B", hints[0])
	}
	if strings.Contains(hints[0], "k.pem") {
		t.Errorf("hint = %q, lists a file the hardened rules deny", hints[0])
	}
}

func TestRunAgent_GraphBrokenSameResult(t *testing.T) {
	sandboxEnv(t)
	dir, base, head := agentRepo(t, nil)
	broken := filepath.Join(t.TempDir(), "broken.json")
	writeFile(t, filepath.Dir(broken), "broken.json", "{nope")
	resultOf := func(extra ...string) map[string]any {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if code := runAgent(context.Background(), agentArgs(dir, base, head, oneSkill(t), extra...), agentEnv(nil), &stdout, &stderr, answerRun(t)); code != 0 {
			t.Fatalf("exit code = %d; stderr:\n%s", code, stderr.String())
		}
		var m map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		clearTimes(m)
		return m
	}
	plain := resultOf()
	if got := resultOf("--graph", broken); !reflect.DeepEqual(got, plain) {
		t.Errorf("result with a broken --graph:\n%v\nwant\n%v", got, plain)
	}
}

// clearTimes removes the fields that hold a time or a duration, at any depth.
func clearTimes(v any) {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			if strings.HasSuffix(k, "_at") || strings.HasSuffix(k, "_seconds") || k == "duration_ms" {
				delete(x, k)
				continue
			}
			clearTimes(e)
		}
	case []any:
		for _, e := range x {
			clearTimes(e)
		}
	}
}

func TestRunAgent_NoSkillVerifyReachesReview(t *testing.T) {
	sandboxEnv(t)
	dir, base, head := agentRepo(t, nil)
	var stdout, stderr bytes.Buffer
	var mu sync.Mutex
	var prefixes []string
	inner := answerRun(t, finding.Finding{File: "a.go", Line: 3, Severity: finding.Blocking, Title: "Bad", Explanation: "x"})
	run := func(ctx context.Context, o runner.Options) runner.RunResult {
		mu.Lock()
		prefixes = append(prefixes, o.LogPrefix)
		mu.Unlock()
		return inner(ctx, o)
	}

	code := runAgent(context.Background(), agentArgs(dir, base, head, oneSkill(t), "--no-skill-verify"), agentEnv(nil), &stdout, &stderr, run)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr.String())
	}
	for _, p := range prefixes {
		if strings.HasSuffix(p, " verify]") {
			t.Errorf("a verification run started: %v", prefixes)
		}
	}
	r := decodeOnly(t, stdout.Bytes())
	s, ok := skillNamed(r, "review")
	if !ok || s.Verdict != "has_blocking_issues" {
		t.Errorf("skill = %+v, want has_blocking_issues", s)
	}
}
