package result

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gopaytech/pergent/internal/finding"
	"github.com/gopaytech/pergent/internal/review"
	"github.com/gopaytech/pergent/internal/runner"
)

func testMeta() Meta {
	return Meta{
		PergentVersion: "0.1.0",
		Base:           "base111",
		Head:           "head222",
		Started:        time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Finished:       time.Date(2026, 1, 2, 3, 6, 5, 0, time.UTC),
		Budget:         10 * time.Minute,
	}
}

func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	return m
}

func TestAssignIDs(t *testing.T) {
	got := AssignIDs([]string{"abcdef111111", "abcdef222222", "123456777777"})
	want := map[string]string{
		"abcdef111111": "abcdef11",
		"abcdef222222": "abcdef22",
		"123456777777": "123456",
	}
	for fp, id := range want {
		if got[fp] != id {
			t.Errorf("AssignIDs[%q] = %q, want %q", fp, got[fp], id)
		}
	}

	single := AssignIDs([]string{"abcdef111111"})
	if single["abcdef111111"] != "abcdef" {
		t.Errorf("single fingerprint ID = %q, want abcdef", single["abcdef111111"])
	}

	// The same fingerprint twice is not a collision.
	dup := AssignIDs([]string{"abcdef111111", "abcdef111111"})
	if dup["abcdef111111"] != "abcdef" {
		t.Errorf("repeated fingerprint ID = %q, want abcdef", dup["abcdef111111"])
	}
}

func TestBuild_Shape(t *testing.T) {
	f := finding.Finding{
		File: "x.go", Line: 3, Severity: finding.Blocking,
		Title: "Nil deref", Explanation: "p may be nil", SuggestedFix: "check p",
		Fingerprint: "a1b2c3d4e5f6",
	}
	sr := review.SkillResult{
		Skill:    "code-review",
		Verdict:  review.HasBlockingIssues,
		Findings: []finding.Finding{f},
		Failures: []review.BatchFailure{{
			Files: []string{"a.go"},
			Cause: &runner.Cause{Kind: runner.CauseTimeout},
		}},
		Batches: 2, BatchesFinished: 1,
		Reviewed: []string{"x.go"},
		Usage:    runner.Usage{Input: 8, Output: 3},
		Duration: 2 * time.Second,
	}

	r := Build(testMeta(), []review.SkillResult{sr})
	b, err := Encode(r)
	if err != nil {
		t.Fatal(err)
	}
	m := decode(t, b)

	if m["schema_version"] != float64(1) || m["status"] != "completed" {
		t.Errorf("schema_version/status = %v/%v", m["schema_version"], m["status"])
	}
	if m["base"] != "base111" || m["head"] != "head222" || m["budget_seconds"] != float64(600) {
		t.Errorf("meta fields wrong: %v", m)
	}
	if m["started_at"] != "2026-01-02T03:04:05Z" {
		t.Errorf("started_at = %v", m["started_at"])
	}
	sk := m["skills"].([]any)[0].(map[string]any)
	if sk["name"] != "code-review" || sk["verdict"] != "has_blocking_issues" {
		t.Errorf("skill name/verdict = %v/%v", sk["name"], sk["verdict"])
	}
	if sk["duration_seconds"] != float64(2) {
		t.Errorf("duration_seconds = %v", sk["duration_seconds"])
	}
	tok := sk["tokens"].(map[string]any)
	if tok["input"] != float64(8) || tok["output"] != float64(3) {
		t.Errorf("tokens = %v", tok)
	}
	files := sk["files"].(map[string]any)
	nr := files["not_reviewed"].([]any)
	if len(nr) != 1 || nr[0].(map[string]any)["path"] != "a.go" {
		t.Errorf("not_reviewed = %v", nr)
	}
	if nr[0].(map[string]any)["cause"] == "" {
		t.Errorf("not_reviewed cause is empty")
	}
	if files["reviewed"].([]any)[0] != "x.go" {
		t.Errorf("reviewed = %v", files["reviewed"])
	}
	fails := sk["failures"].([]any)
	if len(fails) != 1 || fails[0].(map[string]any)["cause"].(map[string]any)["kind"] != "timeout" {
		t.Errorf("failures = %v", fails)
	}
	fd := sk["findings"].([]any)[0].(map[string]any)
	if fd["id"] != "a1b2c3" || fd["fingerprint"] != "a1b2c3d4e5f6" {
		t.Errorf("finding id/fingerprint = %v/%v", fd["id"], fd["fingerprint"])
	}
	if fd["suggested_fix"] != "check p" || fd["line"] != float64(3) {
		t.Errorf("finding = %v", fd)
	}
	if _, ok := m["error"]; ok {
		t.Errorf("completed result has an error key")
	}
	if sk["batches"] != float64(2) || sk["batches_finished"] != float64(1) {
		t.Errorf("batches = %v/%v", sk["batches"], sk["batches_finished"])
	}
}

func TestBuild_FindingsAndFilesAreArrays(t *testing.T) {
	r := Build(testMeta(), []review.SkillResult{{Skill: "s", Verdict: review.NoBlockingIssues}})
	b, _ := Encode(r)
	sk := decode(t, b)["skills"].([]any)[0].(map[string]any)
	for _, k := range []string{"findings", "failures", "notes"} {
		if _, ok := sk[k].([]any); !ok {
			t.Errorf("%s = %v, want an array", k, sk[k])
		}
	}
	files := sk["files"].(map[string]any)
	for _, k := range []string{"reviewed", "skipped", "not_reviewed"} {
		if _, ok := files[k].([]any); !ok {
			t.Errorf("files.%s = %v, want an array", k, files[k])
		}
	}
}

func TestErrorResult(t *testing.T) {
	r := ErrorResult(testMeta(), CodeCommitMissing, "commit abc is not in the checkout")
	b, err := Encode(r)
	if err != nil {
		t.Fatal(err)
	}
	m := decode(t, b)
	if m["status"] != "error" || m["schema_version"] != float64(1) {
		t.Errorf("status = %v", m["status"])
	}
	e := m["error"].(map[string]any)
	if e["code"] != "commit_missing" {
		t.Errorf("error.code = %v", e["code"])
	}
	c := m["comment"].(map[string]any)
	want := "⚠️ **pergent couldn't review this MR:** commit abc is not in the checkout"
	if c["markdown"] != want {
		t.Errorf("comment.markdown = %q, want %q", c["markdown"], want)
	}
	if _, ok := m["skills"]; ok {
		t.Errorf("error result has a skills key")
	}
}

func TestErrorResult_OneLineRedacted(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "glpat-secretvalue")
	r := ErrorResult(Meta{}, CodeInternal, "failed\n  with glpat-secretvalue\ttoken")
	if strings.ContainsAny(r.Comment.Markdown, "\n\t") {
		t.Errorf("comment is not one line: %q", r.Comment.Markdown)
	}
	b, _ := Encode(r)
	if bytes.Contains(b, []byte("glpat-secretvalue")) {
		t.Errorf("token leaked: %s", b)
	}
}

func TestErrorCodes(t *testing.T) {
	cases := map[ErrorCode]string{
		CodeUsage: "usage", CodeCheckoutMissing: "checkout_missing", CodeCommitMissing: "commit_missing",
		CodeHeadMismatch: "head_mismatch", CodeDiffFailed: "diff_failed", CodeSkillsInvalid: "skills_invalid",
		CodeUntrustedInstructions: "untrusted_instructions", CodeInternal: "internal",
	}
	for c, want := range cases {
		if string(c) != want {
			t.Errorf("code %q != %q", c, want)
		}
	}
}

func TestEncode_SizeCap(t *testing.T) {
	big := strings.Repeat("x", 11*1024*1024)
	sr := review.SkillResult{
		Skill:    "code-review",
		Verdict:  review.HasBlockingIssues,
		Findings: []finding.Finding{{File: "x.go", Severity: finding.Blocking, Title: "t", Explanation: "e", Fingerprint: "a1b2c3d4e5f6"}},
		Checks: []review.BatchChecks{{
			Batch:  1,
			Checks: []finding.Check{{Section: "s", Name: "n", Status: "pass", Detail: big}},
		}},
		Summaries: []review.BatchSummary{{Batch: 1, Text: "short summary"}},
	}
	r := Build(testMeta(), []review.SkillResult{sr})
	b, err := Encode(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > 10*1024*1024 {
		t.Fatalf("encoded %d bytes, over 10 MB", len(b))
	}
	sk := decode(t, b)["skills"].([]any)[0].(map[string]any)
	if _, ok := sk["checks"]; ok {
		t.Errorf("checks not dropped")
	}
	if len(sk["findings"].([]any)) != 1 {
		t.Errorf("findings lost")
	}
	if _, ok := sk["summaries"]; !ok {
		t.Errorf("summaries dropped though dropping checks was enough")
	}
	notes := sk["notes"].([]any)
	if len(notes) != 1 || notes[0] != "Checks omitted to fit the 10 MB result limit." {
		t.Errorf("notes = %v", notes)
	}
	// The caller's result is not changed.
	if len(r.Skills[0].Checks) == 0 {
		t.Errorf("Encode mutated the caller's result")
	}
}

func TestEncode_SizeCapDropsSummariesNext(t *testing.T) {
	big := strings.Repeat("y", 11*1024*1024)
	sr := review.SkillResult{
		Skill:     "s",
		Verdict:   review.NoBlockingIssues,
		Summaries: []review.BatchSummary{{Batch: 1, Text: big}},
		Checks:    []review.BatchChecks{{Batch: 1, Checks: []finding.Check{{Name: "n", Status: "pass"}}}},
	}
	b, err := Encode(Build(testMeta(), []review.SkillResult{sr}))
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > 10*1024*1024 {
		t.Fatalf("encoded %d bytes", len(b))
	}
	sk := decode(t, b)["skills"].([]any)[0].(map[string]any)
	if _, ok := sk["summaries"]; ok {
		t.Errorf("summaries not dropped")
	}
	if _, ok := sk["checks"]; ok {
		t.Errorf("checks not dropped")
	}
	notes := sk["notes"].([]any)
	if len(notes) != 2 || notes[1] != "Summaries omitted to fit the 10 MB result limit." {
		t.Errorf("notes = %v", notes)
	}
}

func TestEncode_Redacts(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "glpat-secretvalue")
	sr := review.SkillResult{
		Skill:   "s",
		Verdict: review.HasBlockingIssues,
		Findings: []finding.Finding{{
			File: "x.go", Severity: finding.Blocking, Title: "t",
			Explanation: "leaks glpat-secretvalue here", Fingerprint: "a1b2c3d4e5f6",
		}},
		Notes: []string{"note glpat-secretvalue"},
	}
	r := Build(testMeta(), []review.SkillResult{sr})
	r.Comment.Markdown = "comment glpat-secretvalue"
	b, err := Encode(r)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte("glpat-secretvalue")) {
		t.Errorf("token leaked: %s", b)
	}
}

func TestAssignIDs_WidensUntilUnique(t *testing.T) {
	// These share their first 10 characters.
	a, b := "abcdef1234aaaa", "abcdef1234bbbb"
	got := AssignIDs([]string{a, b, "zzzzzz000000"})
	if got[a] == got[b] {
		t.Fatalf("IDs not unique: %q", got[a])
	}
	if got[a] != "abcdef1234a" || got[b] != "abcdef1234b" {
		t.Errorf("IDs = %q, %q", got[a], got[b])
	}
	if got["zzzzzz000000"] != "zzzzzz" {
		t.Errorf("unrelated ID = %q", got["zzzzzz000000"])
	}
}

func TestAssignIDs_IdenticalShareID(t *testing.T) {
	got := AssignIDs([]string{"abcdef111111", "abcdef222222", "abcdef111111"})
	if got["abcdef111111"] != "abcdef11" || got["abcdef222222"] != "abcdef22" {
		t.Errorf("IDs = %v", got)
	}
}

func TestBuild_IDsUniqueAcrossSkills(t *testing.T) {
	mk := func(name, fp string) review.SkillResult {
		return review.SkillResult{Skill: name, Verdict: review.HasBlockingIssues,
			Findings: []finding.Finding{{File: "x.go", Severity: finding.Blocking, Title: name, Fingerprint: fp}}}
	}
	r := Build(testMeta(), []review.SkillResult{mk("one", "abcdef111111"), mk("two", "abcdef222222")})
	if id := r.Skills[0].Findings[0].ID; id != "abcdef11" {
		t.Errorf("first ID = %q", id)
	}
	if id := r.Skills[1].Findings[0].ID; id != "abcdef22" {
		t.Errorf("second ID = %q", id)
	}
}

func TestBuild_FailureFieldsArePlainOneLine(t *testing.T) {
	sr := review.SkillResult{
		Skill: "s", Verdict: review.Failed,
		Failures: []review.BatchFailure{{
			Files:   []string{"a.go", "b.go"},
			Message: "Batch 1\nof 2 failed.",
			Cause: &runner.Cause{
				Kind: runner.CauseExitError, ExitCode: 1,
				StderrLines: []string{"panic: boom", "goroutine 1"},
			},
		}},
	}
	b, err := Encode(Build(testMeta(), []review.SkillResult{sr}))
	if err != nil {
		t.Fatal(err)
	}
	sk := decode(t, b)["skills"].([]any)[0].(map[string]any)
	fail := sk["failures"].([]any)[0].(map[string]any)
	causeMsg := runner.Cause{Kind: runner.CauseExitError, ExitCode: 1}.Message()
	texts := []string{fail["message"].(string)}
	for _, nr := range sk["files"].(map[string]any)["not_reviewed"].([]any) {
		texts = append(texts, nr.(map[string]any)["cause"].(string))
	}
	if len(texts) != 3 {
		t.Fatalf("texts = %v", texts)
	}
	for _, s := range texts {
		if strings.Contains(s, "**") || strings.Contains(s, "\n") || strings.Contains(strings.ToLower(s), "stderr") || strings.Contains(s, "panic: boom") {
			t.Errorf("not plain one-line text: %q", s)
		}
		if !strings.Contains(s, "Batch 1 of 2 failed.") || !strings.Contains(s, causeMsg) {
			t.Errorf("missing lead or cause text: %q (cause %q)", s, causeMsg)
		}
	}
}

func TestBuild_FailureWithoutMessageOrCause(t *testing.T) {
	sr := review.SkillResult{Skill: "s", Verdict: review.Failed,
		Failures: []review.BatchFailure{{Files: []string{"a.go"}}}}
	r := Build(testMeta(), []review.SkillResult{sr})
	want := "The run ended without a review and without a reported cause."
	if r.Skills[0].Failures[0].Message != want || r.Skills[0].Files.NotReviewed[0].Cause != want {
		t.Errorf("got %q", r.Skills[0].Failures[0].Message)
	}
}

func TestEncode_RedactsEveryField(t *testing.T) {
	const secret = "glpat-secretvalue"
	t.Setenv("GITLAB_TOKEN", secret)
	sr := review.SkillResult{
		Skill: "s", Verdict: review.Failed,
		Failures: []review.BatchFailure{{
			Files:   []string{"a.go"},
			Message: "failed with " + secret,
			Cause:   &runner.Cause{Kind: runner.CauseOpencodeError, ErrorName: "E", ErrorMessage: "bad " + secret},
		}},
		Summaries: []review.BatchSummary{{Batch: 1, Text: "summary " + secret}},
		Checks: []review.BatchChecks{{Batch: 1, Checks: []finding.Check{
			{Section: "s", Name: "n", Status: "pass", Detail: "detail " + secret}}}},
	}
	r := Build(testMeta(), []review.SkillResult{sr})
	// Build output must be clean in the failure fields already, and Encode
	// must clean the rest.
	b, err := Encode(r)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte(secret)) {
		t.Errorf("token leaked: %s", b)
	}
	sk := decode(t, b)["skills"].([]any)[0].(map[string]any)
	if sk["failures"].([]any)[0].(map[string]any)["message"] == "" {
		t.Errorf("failure message empty")
	}
	// Encode redacts fields set after Build too.
	r.Skills[0].Failures[0].Cause.Message = "cause " + secret
	r.Skills[0].Files.NotReviewed[0].Cause = "nr " + secret
	b, _ = Encode(r)
	if bytes.Contains(b, []byte(secret)) {
		t.Errorf("token leaked after edit: %s", b)
	}
}
