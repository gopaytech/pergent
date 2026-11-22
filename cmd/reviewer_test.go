package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/gopaytech/pergent/internal/finding"
	"github.com/gopaytech/pergent/internal/result"
	"github.com/gopaytech/pergent/internal/review"
	"github.com/gopaytech/pergent/internal/runner"
)

// reviewerFind is a blocking finding of a reviewer.
var reviewerFind = result.Finding{Fingerprint: "fa1", File: "a.go", Line: 3, Severity: "blocking", Title: "Nil map write", Explanation: "e"}

// reviewSkill is a reviewer's review of skill "review" with the given findings.
func reviewSkill(fs ...result.Finding) result.Skill {
	s := result.Skill{Name: "review", Verdict: string(review.NoBlockingIssues), Findings: fs,
		Files: result.Files{Reviewed: []string{"a.go"}}, Batches: 1, BatchesFinished: 1}
	for _, f := range fs {
		if f.Severity == "blocking" {
			s.Verdict = string(review.HasBlockingIssues)
		}
	}
	return s
}

// reviewerCompleted is a completed reviewer result with the given skills.
func reviewerCompleted(skills ...result.Skill) result.Result {
	return result.Result{SchemaVersion: result.SchemaVersion, Status: result.StatusCompleted, Skills: skills}
}

// writeReviewer writes r, encoded, to name in a directory outside the checkout.
func writeReviewer(t *testing.T, name string, r result.Result) string {
	t.Helper()
	data, err := result.Encode(r)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// A result whose base or head differs is not a review of this change.
func TestReviewerMismatch(t *testing.T) {
	r := &result.Result{Base: "b1", Head: "h1"}
	if got := reviewerMismatch(r, "b1", "h1"); got != "" {
		t.Errorf("same change: %q", got)
	}
	if got := reviewerMismatch(r, "b2", "h1"); got != `base "b1", want "b2"` {
		t.Errorf("other base: %q", got)
	}
	if got := reviewerMismatch(r, "b1", "h2"); got != `head "h1", want "h2"` {
		t.Errorf("other head: %q", got)
	}
}

func TestReadReviewer(t *testing.T) {
	ok := writeReviewer(t, "ok.json", reviewerCompleted(reviewSkill(reviewerFind)))
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(t.TempDir(), "big.json")
	if err := os.WriteFile(big, bytes.Repeat([]byte(" "), result.MaxSize+1), 0o600); err != nil {
		t.Fatal(err)
	}
	odd := reviewerCompleted()
	odd.Status = "weird"
	oddPath := writeReviewer(t, "odd.json", odd)

	if r, reason := readReviewer(ok); r == nil || reason != "" || len(r.Skills) != 1 {
		t.Errorf("ok: r %v, reason %q", r, reason)
	}
	for _, c := range []struct{ path, want string }{
		{bad, "decoding"},
		{big, "over the 10 MB limit"},
		{oddPath, `status "weird"`},
		{t.TempDir(), "not a regular file"},
		{filepath.Join(t.TempDir(), "missing.json"), "no such file or directory"},
	} {
		if r, reason := readReviewer(c.path); r != nil || !strings.Contains(reason, c.want) {
			t.Errorf("%s: r %v, reason %q, want %q", c.path, r, reason, c.want)
		}
	}
}

// A FIFO is refused before it is opened: opening one blocks until a writer
// comes.
func TestReadReviewerFIFODoesNotBlock(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "reviewer.json")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	done := make(chan string, 1)
	go func() {
		_, reason := readReviewer(fifo)
		done <- reason
	}()
	select {
	case reason := <-done:
		if !strings.Contains(reason, "not a regular file") {
			t.Errorf("reason = %q", reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("readReviewer blocked on a FIFO")
	}
}

func TestReviewerSkills(t *testing.T) {
	s := reviewSkill(reviewerFind)
	s.Confidence, s.ConfidenceReason = 4, "small"
	s.Notes = []string{"a note"}
	s.Files.Deleted = []string{"a.go"}
	s.Files.Skipped = []result.Skipped{{Path: "go.sum", Rule: "lockfile"}}
	odd := reviewSkill()
	odd.Name, odd.Verdict = "odd", "maybe"
	unfinished := reviewSkill()
	unfinished.Name, unfinished.Batches = "unfinished", 2
	twice := reviewSkill()
	twice.Name = "twice"
	twiceFailed := twice
	twiceFailed.Verdict = string(review.Failed)
	got := reviewerSkills(&result.Result{Skills: []result.Skill{s, odd, unfinished, twice, twiceFailed, twiceFailed, twice}})
	res, ok := got["review"]
	if !ok || res.Skill != "review" || res.Verdict != review.HasBlockingIssues || len(res.Findings) != 1 {
		t.Fatalf("review = %+v", res)
	}
	f := res.Findings[0]
	if f.Fingerprint != "fa1" || f.File != "a.go" || f.Line != 3 || f.Severity != "blocking" || f.Title != "Nil map write" {
		t.Errorf("finding = %+v", f)
	}
	if res.Confidence != 4 || res.ConfidenceReason != "small" || !slices.Equal(res.Notes, []string{"a note"}) ||
		!slices.Equal(res.Reviewed, []string{"a.go"}) || !slices.Equal(res.Deleted, []string{"a.go"}) ||
		len(res.Skipped) != 1 || res.Batches != 1 || res.BatchesFinished != 1 {
		t.Errorf("review = %+v", res)
	}
	// A verdict pergent does not know can't be trusted: the skill is failed.
	if got["odd"].Verdict != review.Failed {
		t.Errorf("odd verdict = %s, want failed", got["odd"].Verdict)
	}
	// A batch that did not finish, with no failure reported, is no pass.
	if got["unfinished"].Verdict != review.Failed {
		t.Errorf("unfinished verdict = %s, want failed", got["unfinished"].Verdict)
	}
	// A skill the result holds twice can't be trusted, whichever came first.
	if got["twice"].Verdict != review.Failed {
		t.Errorf("twice verdict = %s, want failed", got["twice"].Verdict)
	}
	if again := reviewerSkills(&result.Result{Skills: []result.Skill{twiceFailed, twice}}); again["twice"].Verdict != review.Failed {
		t.Errorf("failed first: verdict = %s, want failed", again["twice"].Verdict)
	}
}

// TestReviewerSkillNotReviewedIsFailed: a file the reviewer did not review, with
// no failure and every batch finished, still makes the skill failed.
func TestReviewerSkillNotReviewedIsFailed(t *testing.T) {
	s := reviewSkill(reviewerFind)
	s.Files.NotReviewed = []result.NotReviewed{{Path: "a.go", Cause: "the batch failed"}}
	if got := reviewerSkill(s); got.Verdict != review.Failed {
		t.Errorf("verdict = %s, want failed", got.Verdict)
	}
}

const (
	judgeProvider = "anthropic"
	judgeModel    = "model-j"
	judgeKey      = "key-j-0123456789"
)

// reviewerFind2 is reviewer 2's report of the issue reviewerFind is about.
var reviewerFind2 = result.Finding{Fingerprint: "fb1", File: "a.go", Line: 3, Severity: "blocking", Title: "The map is nil", Explanation: "e"}

// multiRunEnv is the environment of a judge Job: the judge model in
// OPENCODE_*, its key, and a result token. The process environment gets the
// model too, for the generated opencode config.
func multiRunEnv(t *testing.T) func(string) string {
	t.Helper()
	t.Setenv("OPENCODE_PROVIDER", judgeProvider)
	t.Setenv("OPENCODE_MODEL", judgeModel)
	return agentEnv(map[string]string{
		"OPENCODE_PROVIDER":    judgeProvider,
		"OPENCODE_MODEL":       judgeModel,
		"OPENCODE_API_KEY":     judgeKey,
		"PERGENT_RESULT_TOKEN": "result-token",
	})
}

// reviewerFile writes a reviewer's result for the checkout's base and head,
// and returns its path.
type reviewerFile func(t *testing.T, base, head string) string

// reviewerOf is r as a reviewer's review of the checkout's change.
func reviewerOf(name string, r result.Result) reviewerFile {
	return func(t *testing.T, base, head string) string {
		r.Base, r.Head = base, head
		return writeReviewer(t, name, r)
	}
}

// staleReviewer is r as a reviewer's review of another head.
func staleReviewer(name string, r result.Result) reviewerFile {
	return func(t *testing.T, base, _ string) string {
		r.Base, r.Head = base, strings.Repeat("f", 40)
		return writeReviewer(t, name, r)
	}
}

// missingReviewer is a reviewer file that does not exist.
func missingReviewer(t *testing.T, _, _ string) string {
	return filepath.Join(t.TempDir(), "missing.json")
}

// multiRunCall is one run the fake saw: its attached file names and options.
type multiRunCall struct {
	files []string
	opts  runner.Options
}

// isJudge reports whether c is a judge run: it has a reviewer file.
func (c multiRunCall) isJudge() bool {
	return slices.ContainsFunc(c.files, func(f string) bool { return strings.HasPrefix(f, "reviewer-") })
}

// multiRuns answers a judge run (it has reviewer-<n>.json files) with judge,
// and a verification run (blocking-findings.json) with verify.
type multiRuns struct {
	judge, verify string
	// verifyCause, when set, makes each verification run fail with it.
	verifyCause *runner.Cause
	mu          sync.Mutex
	calls       []multiRunCall
}

func (m *multiRuns) run(_ context.Context, o runner.Options) runner.RunResult {
	call := multiRunCall{opts: o}
	for _, f := range o.Files {
		call.files = append(call.files, filepath.Base(f))
	}
	m.mu.Lock()
	m.calls = append(m.calls, call)
	m.mu.Unlock()
	if call.isJudge() {
		return runner.RunResult{Status: runner.StatusCompleted, Output: m.judge, Usage: runner.Usage{Input: 5, Output: 1}}
	}
	if m.verifyCause != nil {
		return runner.RunResult{Status: runner.StatusFailed, Cause: m.verifyCause}
	}
	return runner.RunResult{Status: runner.StatusCompleted, Output: m.verify, Usage: runner.Usage{Input: 5, Output: 1}}
}

// checkAnswer is a judge's or verification's answer with the given results.
func checkAnswer(t *testing.T, results ...map[string]any) string {
	t.Helper()
	if results == nil {
		results = []map[string]any{}
	}
	data, err := json.Marshal(map[string]any{"results": results})
	if err != nil {
		t.Fatal(err)
	}
	return "Checked.\n\n```json\n" + string(data) + "\n```\n"
}

// linkedAnswer is a judge's answer that 1.1 and 2.1 are the same real issue.
func linkedAnswer(t *testing.T) string {
	return checkAnswer(t,
		map[string]any{"id": "1.1", "real": true, "reason": "yes", "same_as": []string{"2.1"}},
		map[string]any{"id": "2.1", "real": true, "reason": "yes", "same_as": []string{"1.1"}})
}

// runMultiAgent runs `pergent agent --multi-review` with one --reviewer per
// file against a result server, and returns the exit code, the server and
// stderr.
func runMultiAgent(t *testing.T, env func(string) string, files []reviewerFile, runs *multiRuns, extra ...string) (int, *resultServer, string) {
	t.Helper()
	dir, base, head := agentRepo(t, nil)
	srv := newResultServer(t, http.StatusOK)
	args := []string{"--repo-path", dir, "--base", base, "--head", head, "--skills-dir", oneSkill(t),
		"--result-url", srv.URL, "--multi-review"}
	for _, f := range files {
		args = append(args, "--reviewer", f(t, base, head))
	}
	// A judge Job needs --mr-dismissals: the merge request has none here.
	if !slices.Contains(extra, "--no-judge") && !slices.Contains(extra, "--mr-dismissals") {
		args = append(args, "--mr-dismissals", noMRDismissals(t))
	}
	args = append(args, extra...)
	var stdout, stderr bytes.Buffer
	code := runAgent(context.Background(), args, env, &stdout, &stderr, runs.run)
	return code, srv, stderr.String()
}

// noMRDismissals is an --mr-dismissals file outside the checkout with no
// dismissals.
func noMRDismissals(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "mr-dismissals.json")
	if err := os.WriteFile(p, []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// checkRunEnv checks that each run gets no key of its own: opencode keeps
// the environment's OPENCODE_API_KEY, and the result token is kept from it.
// No key reaches the log or the result.
func checkRunEnv(t *testing.T, runs *multiRuns, srv *resultServer, stderr string) {
	t.Helper()
	for _, c := range runs.calls {
		if len(c.opts.Env) != 0 {
			t.Errorf("run %v: Env = %v, want none", c.files, c.opts.Env)
		}
		if !slices.Contains(c.opts.ExcludeEnv, "PERGENT_RESULT_TOKEN") || slices.Contains(c.opts.ExcludeEnv, "OPENCODE_API_KEY") {
			t.Errorf("run %v: ExcludeEnv = %v", c.files, c.opts.ExcludeEnv)
		}
	}
	if strings.Contains(stderr, judgeKey) {
		t.Error("the key reached the log")
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	for _, b := range srv.bodies {
		if bytes.Contains(b, []byte(judgeKey)) {
			t.Error("the key reached the result")
		}
	}
}

func TestRunAgentMultiReviewThree(t *testing.T) {
	runs := &multiRuns{judge: linkedAnswer(t)}
	files := []reviewerFile{
		reviewerOf("1.json", reviewerCompleted(reviewSkill(reviewerFind))),
		reviewerOf("2.json", reviewerCompleted(reviewSkill(reviewerFind2))),
		reviewerOf("3.json", reviewerCompleted(reviewSkill())),
	}
	code, srv, stderr := runMultiAgent(t, multiRunEnv(t), files, runs)
	if code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, stderr)
	}
	r := srv.only(t)
	m := r.MultiReview
	if m == nil {
		t.Fatal("no multi_review")
	}
	want := []result.MultiReviewer{{Number: 1, Usable: true}, {Number: 2, Usable: true}, {Number: 3, Usable: true}}
	if !slices.Equal(m.Reviewers, want) {
		t.Errorf("reviewers = %+v", m.Reviewers)
	}
	if m.Judge != (result.MultiJudge{Provider: judgeProvider, Model: judgeModel, Judged: true}) {
		t.Errorf("judge = %+v", m.Judge)
	}
	s, ok := skillNamed(r, "review")
	if !ok || len(s.Findings) != 1 || s.MultiReview == nil {
		t.Fatalf("skill = %+v", s)
	}
	if s.Verdict != "has_blocking_issues" || !slices.Equal(s.MultiReview.Reviewers, []int{1, 2, 3}) || s.MultiReview.Judge != review.JudgeOK {
		t.Errorf("skill verdict %s, multi_review %+v", s.Verdict, s.MultiReview)
	}
	f := s.Findings[0]
	if f.Severity != "blocking" || f.MultiReview == nil || !slices.Equal(f.MultiReview.FoundBy, []int{1, 2}) ||
		!slices.Equal(f.MultiReview.Also, []string{"fb1"}) {
		t.Errorf("finding = %+v, multi_review %+v", f, f.MultiReview)
	}
	header := "Multi review: reviewer 1, reviewer 2, reviewer 3 · judge " + judgeProvider + "/" + judgeModel
	if !strings.Contains(r.Comment.Markdown, header) {
		t.Errorf("comment lacks %q:\n%s", header, r.Comment.Markdown)
	}
	if len(runs.calls) != 1 || !runs.calls[0].isJudge() {
		t.Errorf("runs = %+v, want one judge run", runs.calls)
	}
	if !strings.Contains(stderr, "Merging the multi review (3 of 3 reviewers usable) for 1 skill(s)") {
		t.Errorf("stderr lacks the merge line:\n%s", stderr)
	}
	checkRunEnv(t, runs, srv, stderr)
}

func TestRunAgentMultiReviewMiddleReviewerUnusable(t *testing.T) {
	runs := &multiRuns{judge: checkAnswer(t,
		map[string]any{"id": "1.1", "real": true, "reason": "yes", "same_as": []string{"3.1"}},
		map[string]any{"id": "3.1", "real": true, "reason": "yes", "same_as": []string{"1.1"}})}
	files := []reviewerFile{
		reviewerOf("1.json", reviewerCompleted(reviewSkill(reviewerFind))),
		reviewerOf("2.json", result.ErrorResult(result.Meta{}, result.CodeInternal, "it broke")),
		reviewerOf("3.json", reviewerCompleted(reviewSkill(reviewerFind2))),
	}
	code, srv, stderr := runMultiAgent(t, multiRunEnv(t), files, runs)
	if code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, stderr)
	}
	r := srv.only(t)
	if r.MultiReview == nil || len(r.MultiReview.Reviewers) != 3 || r.MultiReview.Reviewers[1] != (result.MultiReviewer{Number: 2}) ||
		!r.MultiReview.Reviewers[0].Usable || !r.MultiReview.Reviewers[2].Usable {
		t.Fatalf("multi_review = %+v", r.MultiReview)
	}
	s, ok := skillNamed(r, "review")
	if !ok || s.MultiReview == nil || !slices.Equal(s.MultiReview.Reviewers, []int{1, 3}) || !slices.Equal(s.MultiReview.Failed, []int{2}) {
		t.Fatalf("skill multi_review = %+v", s.MultiReview)
	}
	if len(s.Findings) != 1 || !slices.Equal(s.Findings[0].MultiReview.FoundBy, []int{1, 3}) {
		t.Errorf("findings = %+v", s.Findings)
	}
	if !strings.Contains(stderr, "multi review: reviewer 2 can't be used: status error") {
		t.Errorf("stderr lacks why reviewer 2 can't be used:\n%s", stderr)
	}
	if strings.Contains(stderr, "it broke") || strings.Contains(string(srv.bodies[0]), "it broke") {
		t.Error("a reviewer's own error text reached the log or the result")
	}
	for _, want := range []string{"found by 2 of 2 reviewers (reviewer 1, reviewer 3)", "Multi review: reviewer 2 could not review this."} {
		if !strings.Contains(r.Comment.Markdown, want) {
			t.Errorf("comment lacks %q:\n%s", want, r.Comment.Markdown)
		}
	}
	if len(runs.calls) != 1 || !runs.calls[0].isJudge() || slices.Contains(runs.calls[0].files, "reviewer-2.json") {
		t.Errorf("runs = %+v, want one judge run without reviewer 2", runs.calls)
	}
	checkRunEnv(t, runs, srv, stderr)
}

// A reviewer's result for another head is not a review of this change: it
// can't be used, so it never makes the merge pass.
func TestRunAgentMultiReviewStaleReviewer(t *testing.T) {
	runs := &multiRuns{judge: checkAnswer(t,
		map[string]any{"id": "2.1", "real": true, "reason": "yes", "same_as": []string{"3.1"}},
		map[string]any{"id": "3.1", "real": true, "reason": "yes", "same_as": []string{"2.1"}})}
	files := []reviewerFile{
		staleReviewer("1.json", reviewerCompleted(reviewSkill())),
		reviewerOf("2.json", reviewerCompleted(reviewSkill(reviewerFind))),
		reviewerOf("3.json", reviewerCompleted(reviewSkill(reviewerFind2))),
	}
	code, srv, stderr := runMultiAgent(t, multiRunEnv(t), files, runs)
	if code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, stderr)
	}
	r := srv.only(t)
	if r.MultiReview == nil || r.MultiReview.Reviewers[0] != (result.MultiReviewer{Number: 1}) {
		t.Fatalf("multi_review = %+v", r.MultiReview)
	}
	s, ok := skillNamed(r, "review")
	if !ok || s.MultiReview == nil || !slices.Equal(s.MultiReview.Reviewers, []int{2, 3}) || !slices.Equal(s.MultiReview.Failed, []int{1}) {
		t.Fatalf("skill multi_review = %+v", s.MultiReview)
	}
	stale := `multi review: reviewer 1 can't be used: head "` + strings.Repeat("f", 40) + `", want "`
	if !strings.Contains(stderr, stale) {
		t.Errorf("stderr lacks %q:\n%s", stale, stderr)
	}
	if strings.Contains(string(srv.bodies[0]), strings.Repeat("f", 40)) {
		t.Error("the stale head reached the result")
	}
}

// With one usable reviewer, its blocking findings are verified on the judge
// model. A provider error of that run is the judge's.
func TestRunAgentMultiReviewOneUsableVerifies(t *testing.T) {
	files := []reviewerFile{
		missingReviewer,
		reviewerOf("2.json", reviewerCompleted(reviewSkill(reviewerFind2))),
	}
	t.Run("verified", func(t *testing.T) {
		runs := &multiRuns{verify: checkAnswer(t, map[string]any{"id": "1", "real": true, "reason": "yes"})}
		code, srv, stderr := runMultiAgent(t, multiRunEnv(t), files, runs)
		if code != 0 {
			t.Fatalf("exit code = %d; stderr:\n%s", code, stderr)
		}
		if len(runs.calls) != 1 || runs.calls[0].isJudge() || !slices.Contains(runs.calls[0].files, "blocking-findings.json") {
			t.Errorf("runs = %+v, want one verification run", runs.calls)
		}
		r := srv.only(t)
		s, ok := skillNamed(r, "review")
		if !ok || s.Verdict != "has_blocking_issues" || s.MultiReview == nil || !slices.Equal(s.MultiReview.Reviewers, []int{2}) {
			t.Errorf("skill = %+v, multi_review %+v", s, s.MultiReview)
		}
		if !strings.Contains(stderr, "multi review: reviewer 1 can't be used: ") || !strings.Contains(stderr, "no such file or directory") {
			t.Errorf("stderr lacks why reviewer 1 can't be read:\n%s", stderr)
		}
		checkRunEnv(t, runs, srv, stderr)
	})
	t.Run("quota", func(t *testing.T) {
		quota := &runner.Cause{Kind: runner.CauseOpencodeError, ProviderClass: runner.ClassQuota, ProviderStatus: 429}
		runs := &multiRuns{verifyCause: quota}
		code, srv, stderr := runMultiAgent(t, multiRunEnv(t), files, runs)
		if code != 0 {
			t.Fatalf("exit code = %d; stderr:\n%s", code, stderr)
		}
		r := srv.only(t)
		pe := r.ProviderError
		if pe == nil || pe.Role != result.RoleJudge || pe.Class != string(runner.ClassQuota) || pe.Status != 429 || pe.Model != judgeModel {
			t.Errorf("provider_error = %+v, want the judge's quota error", pe)
		}
		// A failed verification keeps the finding blocking.
		s, ok := skillNamed(r, "review")
		if !ok || s.Verdict != "has_blocking_issues" || len(s.Findings) != 1 {
			t.Errorf("skill = %+v", s)
		}
	})
}

func TestRunAgentMultiReviewNoJudge(t *testing.T) {
	// No OPENCODE_* at all: --no-judge runs no model.
	for _, k := range []string{"OPENCODE_PROVIDER", "OPENCODE_MODEL", "OPENCODE_API_KEY"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	env := func(k string) string {
		if k == "PERGENT_RESULT_TOKEN" {
			return "result-token"
		}
		return ""
	}
	runs := &multiRuns{}
	same := reviewerFind2
	same.Title = reviewerFind.Title
	files := []reviewerFile{
		reviewerOf("1.json", reviewerCompleted(reviewSkill(reviewerFind))),
		reviewerOf("2.json", reviewerCompleted(reviewSkill(same))),
	}
	code, srv, stderr := runMultiAgent(t, env, files, runs, "--no-judge")
	if code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, stderr)
	}
	if len(runs.calls) != 0 {
		t.Errorf("%d runs started", len(runs.calls))
	}
	r := srv.only(t)
	if r.MultiReview == nil || r.MultiReview.Judge != (result.MultiJudge{}) {
		t.Fatalf("multi_review = %+v", r.MultiReview)
	}
	s, ok := skillNamed(r, "review")
	if !ok || s.MultiReview == nil || !s.MultiReview.ExactMerge || s.Verdict != "has_blocking_issues" {
		t.Fatalf("skill = %+v, multi_review %+v", s, s.MultiReview)
	}
	if len(s.Findings) != 1 || !slices.Equal(s.Findings[0].MultiReview.FoundBy, []int{1, 2}) {
		t.Errorf("findings = %+v", s.Findings)
	}
	if !strings.Contains(r.Comment.Markdown, "Multi review: reviewer 1, reviewer 2") || strings.Contains(r.Comment.Markdown, "· judge") {
		t.Errorf("comment header wrong:\n%s", r.Comment.Markdown)
	}
}

func TestRunAgentMultiReviewNoUsableReviewer(t *testing.T) {
	runs := &multiRuns{}
	files := []reviewerFile{missingReviewer, missingReviewer}
	code, srv, stderr := runMultiAgent(t, multiRunEnv(t), files, runs)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr:\n%s", code, stderr)
	}
	if len(runs.calls) != 0 {
		t.Errorf("%d runs started", len(runs.calls))
	}
	r := srv.only(t)
	if r.Status != result.StatusError || r.Error == nil || r.Error.Code != result.CodeUsage || r.Error.Message != noUsableReviewer {
		t.Errorf("result = status %q error %+v", r.Status, r.Error)
	}
	for _, n := range []string{"1", "2"} {
		if !strings.Contains(stderr, "multi review: reviewer "+n+" can't be used: ") {
			t.Errorf("stderr lacks reviewer %s:\n%s", n, stderr)
		}
	}
}

// A loaded skill no reviewer reviewed fails; it is never dropped.
func TestRunAgentMultiReviewSkillInNoneFails(t *testing.T) {
	runs := &multiRuns{}
	other := reviewSkill()
	other.Name = "other"
	files := []reviewerFile{
		reviewerOf("1.json", reviewerCompleted(other)),
		reviewerOf("2.json", reviewerCompleted(other)),
	}
	code, srv, stderr := runMultiAgent(t, multiRunEnv(t), files, runs)
	if code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, stderr)
	}
	r := srv.only(t)
	s, ok := skillNamed(r, "review")
	if !ok || s.Verdict != "failed" || s.MultiReview == nil {
		t.Fatalf("skills = %+v", r.Skills)
	}
	if len(s.Failures) != 1 || s.Failures[0].Message != review.NoReviewerReview {
		t.Errorf("failures = %+v, want one that says why", s.Failures)
	}
	if !strings.Contains(r.Comment.Markdown, review.NoReviewerReview) {
		t.Errorf("the comment does not say why the skill failed:\n%s", r.Comment.Markdown)
	}
	if _, ok := skillNamed(r, "other"); ok {
		t.Error("a skill pergent did not load is in the result")
	}
	if len(runs.calls) != 0 {
		t.Errorf("%d runs started", len(runs.calls))
	}
}

// TestRunAgentMultiReviewAcceptsRules: --rules is a trusted input of the
// judge Job. The result reports the rule list, the judge run gets the rules
// of the files of the findings it judges, and a reviewer's files.rules is
// kept.
func TestRunAgentMultiReviewAcceptsRules(t *testing.T) {
	runs := &multiRuns{judge: linkedAnswer(t)}
	s1 := reviewSkill(reviewerFind)
	s1.Files.Rules = []result.FileRule{{Path: "a.go", Rule: "repo:*.go"}}
	files := []reviewerFile{
		reviewerOf("1.json", reviewerCompleted(s1)),
		reviewerOf("2.json", reviewerCompleted(reviewSkill(reviewerFind2))),
	}
	code, srv, stderr := runMultiAgent(t, multiRunEnv(t), files, runs, "--rules", rulesFileOutside(t, twoRules))
	if code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, stderr)
	}
	r := srv.only(t)
	if r.Rules == nil || !r.Rules.BuiltIn || r.Rules.Entries != 2+len(builtinNames()) {
		t.Errorf("rules = %+v, want built-ins on and 2 team entries", r.Rules)
	}
	if len(runs.calls) != 1 || !runs.calls[0].isJudge() || !slices.Contains(runs.calls[0].files, "file-rules.md") {
		t.Fatalf("runs = %+v, want one judge run with file-rules.md", runs.calls)
	}
	s, ok := skillNamed(r, "review")
	if !ok || !slices.Equal(s.Files.Rules, s1.Files.Rules) {
		t.Errorf("files.rules = %+v, want reviewer 1's", s.Files.Rules)
	}
}

// A judge link onto a finding a merge request dismissal covers is not
// joined: render will dismiss that finding, and a group with it would hide
// the other reviewer's blocking finding.
func TestRunAgentMultiReviewMRDismissalsNotJoined(t *testing.T) {
	runs := &multiRuns{judge: linkedAnswer(t)}
	files := []reviewerFile{
		reviewerOf("1.json", reviewerCompleted(reviewSkill(reviewerFind))),
		reviewerOf("2.json", reviewerCompleted(reviewSkill(reviewerFind2))),
	}
	ds := filepath.Join(t.TempDir(), "mr-dismissals.json")
	if err := os.WriteFile(ds, []byte(`[{"fingerprint": "fb1", "severity": "blocking"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	code, srv, stderr := runMultiAgent(t, multiRunEnv(t), files, runs, "--mr-dismissals", ds)
	if code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, stderr)
	}
	r := srv.only(t)
	s, ok := skillNamed(r, "review")
	if !ok || len(s.Findings) != 2 {
		t.Fatalf("skill = %+v, want 2 findings", s)
	}
	for _, f := range s.Findings {
		if f.MultiReview == nil || len(f.MultiReview.Also) != 0 || len(f.MultiReview.FoundBy) != 1 {
			t.Errorf("finding %s: multi_review %+v, want not joined", f.Fingerprint, f.MultiReview)
		}
	}
	if len(runs.calls) != 1 || !runs.calls[0].isJudge() {
		t.Errorf("runs = %+v, want one judge run", runs.calls)
	}
}

// A review's result has no multi_review key.
func TestRunAgentWithoutMultiReviewIsUnchanged(t *testing.T) {
	sandboxEnv(t)
	dir, base, head := agentRepo(t, nil)
	srv := newResultServer(t, http.StatusOK)
	f := finding.Finding{File: "a.go", Line: 3, Severity: finding.Suggestion, Title: "Document A", Explanation: "A is exported but has no doc comment."}
	args := []string{"--repo-path", dir, "--base", base, "--head", head, "--skills-dir", oneSkill(t), "--result-url", srv.URL}
	var stdout, stderr bytes.Buffer
	if code := runAgent(context.Background(), args, tokenEnv, &stdout, &stderr, answerRun(t, f)); code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, stderr.String())
	}
	srv.only(t)
	if bytes.Contains(srv.bodies[0], []byte(`"multi_review"`)) {
		t.Errorf("a review's result has multi_review:\n%s", srv.bodies[0])
	}
}
