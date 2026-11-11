package output

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gopaytech/pergent/internal/diff"
	"github.com/gopaytech/pergent/internal/finding"
	"github.com/gopaytech/pergent/internal/review"
	"github.com/gopaytech/pergent/internal/runner"
)

// goldenCase is one review rendered at several size limits. The expected
// comments live in render/testdata/golden: <name>-<maxLen>.md for each limit,
// and <name>.block for the hidden findings block. They were produced by the
// formatter as it was before it moved into the render package, so they pin
// the comment byte for byte.
type goldenCase struct {
	name    string
	results []review.SkillResult
	changed []string
	ids     map[string]string
	maxLens []int
}

// setGoldenEnv sets the credentials the cases plant in model text, so the
// goldens show the redaction.
func setGoldenEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GITHUB_TOKEN", "ghp_secretvalue123")
	t.Setenv("GITLAB_TOKEN", "glpat-secretvalue")
	t.Setenv("OPENCODE_API_KEY", "sk-opencode-secret-key")
	t.Setenv("PERGENT_RESULT_TOKEN", "")
}

func goldenCases() []goldenCase {
	big, bigChanged := bigResults()
	shrink := []int{0, 30000, 12000, 3000, 1500, 600, 400, 100}
	return []goldenCase{
		{name: "empty", maxLens: []int{0, 40}},
		{name: "layout", results: layoutResults(), changed: []string{"main.go", "util.go"}, maxLens: []int{0, 2500, 1200, 700}},
		{name: "ids", results: idResults(), ids: map[string]string{"fp-blocking": "abcdef11", "fp-suggestion": "12345678"}, maxLens: []int{0, 900, 600, 400, 300}},
		{name: "ids-hostile", results: idResults(), ids: map[string]string{"fp-blocking": "a`b\nc"}, maxLens: []int{0}},
		{name: "big", results: big, changed: bigChanged, maxLens: shrink},
		{name: "checks-big", results: bigChecksResult(), maxLens: []int{0, 6000, 3000, 2800, 1500, 800, 600}},
		{name: "checks-multi", results: checksMultiResults(), maxLens: []int{0, 1500, 1000}},
		{name: "summaries", results: summariesResults(), maxLens: []int{0, 1200, 800}},
		{name: "failures", results: failureResults(), changed: []string{"a.go", "b.go"}, maxLens: []int{0, 2500, 900}},
		{name: "confidence", results: confidenceGoldenResults(), changed: []string{"main.go", "util.go", "gen/api.pb.go", "README.md"}, maxLens: []int{0}},
		{name: "hostile", results: hostileResults(), changed: []string{"we`ird.go", "ghp_secretvalue123.txt"}, maxLens: []int{0, 2000, 1200}},
		{name: "deleted-and-renamed", results: deletedRenamedResults(), changed: []string{"cmd/run.go", "internal/new/util.go", "internal/old/helper.go"}, maxLens: []int{0, 1800}},
	}
}

func confidenceGoldenResults() []review.SkillResult {
	return []review.SkillResult{
		{
			Skill: "code-review", Verdict: review.HasBlockingIssues, Batches: 1, BatchesFinished: 1,
			Confidence: 4, ConfidenceReason: "The helper in util.go is called from files outside this diff.",
			Findings: []finding.Finding{
				{File: "main.go", Line: 42, EndLine: 43, Severity: finding.Blocking, Title: "Nil dereference", Explanation: "p can be nil here.", Suggestion: "if p == nil {\n\treturn\n}", Fingerprint: "aaaaaaaaaaaa"},
				{File: "util.go", Line: 7, Severity: finding.Suggestion, Title: "Rename helper", Explanation: "The name is unclear.", Fingerprint: "bbbbbbbbbbbb"},
			},
			Reviewed: []string{"main.go", "util.go"},
		},
		{
			Skill: "security", Verdict: review.NoBlockingIssues, Batches: 1, BatchesFinished: 1,
			Confidence: 5, ConfidenceReason: "Nothing here touches input or secrets.",
			Skipped:  []diff.Skip{{Path: "gen/api.pb.go", Rule: "generated"}},
			Reviewed: []string{"main.go", "util.go"},
		},
		{
			Skill: "docs", Verdict: review.Failed, Batches: 1,
			Failures: []review.BatchFailure{{Files: []string{"README.md"}, Cause: &runner.Cause{Kind: runner.CauseNoAnswer, Steps: 3}}},
		},
	}
}

func layoutResults() []review.SkillResult {
	return []review.SkillResult{
		{
			Skill:   "code-review",
			Verdict: review.HasBlockingIssues,
			Findings: []finding.Finding{
				{File: "main.go", Line: 42, Severity: finding.Blocking, Title: "Nil dereference", Explanation: "p can be nil here.", SuggestedFix: "Check p first.", Fingerprint: "aaaaaaaaaaaa"},
				{File: "util.go", Severity: finding.Blocking, Title: "Leak", Explanation: "The file is never closed.", Fingerprint: "bbbbbbbbbbbb"},
				{File: "util.go", Line: 7, Severity: finding.Suggestion, Title: "Rename helper", Explanation: "The name is unclear.", SuggestedFix: "Rename it to\n```go\nfoo()\n```", Fingerprint: "cccccccccccc"},
			},
			Skipped:  []diff.Skip{{Path: "go.sum", Rule: "lockfile"}, {Path: "gen/<!--x.go", Rule: "generated\nfile"}},
			Reviewed: []string{"main.go", "util.go"},
			Notes:    []string{"batch 2 was small"},
		},
		{Skill: "security-review", Verdict: review.NoBlockingIssues},
		{
			Skill:   "docs-review",
			Verdict: review.Failed,
			Failures: []review.BatchFailure{
				{Files: []string{"a.md"}, Cause: &runner.Cause{Kind: runner.CauseNoAnswer, Steps: 3}},
				{Files: []string{"b.md", "c.md"}, Message: "The model's answer was not in the required format."},
			},
			Batches: 2,
		},
		{
			Skill:    "one-issue",
			Verdict:  review.HasBlockingIssues,
			Findings: []finding.Finding{{File: "x.go", Line: 1, Severity: finding.Blocking, Title: "Bad", Explanation: "Really bad."}},
		},
	}
}

func checksMultiResults() []review.SkillResult {
	return []review.SkillResult{
		{
			Skill: "multi", Verdict: review.NoBlockingIssues, Batches: 2, BatchesFinished: 2,
			Checks: []review.BatchChecks{
				{Batch: 1, Findings: 0, Dropped: 3, Checks: []finding.Check{check("A", "a1", finding.CheckPass, ""), check("A", "a2", finding.CheckPass, "evidence"), check("", "loose", finding.CheckNA, "")}},
				{Batch: 2, Findings: 0, Checks: []finding.Check{check("A", "b1", finding.CheckFail, "x"), check("B", "b2", finding.CheckFail, "")}},
			},
			Notes:    []string{"Verification dropped 1 of 1 blocking finding: Not real (it is handled upstream)", "another note"},
			Reviewed: []string{"a.go"},
		},
		{
			Skill: "single", Verdict: review.NoBlockingIssues, Batches: 1, BatchesFinished: 1,
			Summaries: []review.BatchSummary{{Batch: 1, Text: "Sum."}},
			Checks: []review.BatchChecks{{Batch: 1, Findings: 1, Checks: []finding.Check{
				check("Sec|tion", "a|b\nc `x", finding.CheckPass, "</details> <summary>y glpat-secretvalue"),
				check("Sec|tion", "fail", finding.CheckFail, ""),
			}}},
		},
	}
}

func summariesResults() []review.SkillResult {
	var long []review.BatchSummary
	for i := range 4 {
		long = append(long, review.BatchSummary{Batch: i + 1, Text: fmt.Sprintf("Summary %d %s", i, strings.Repeat("s", 200))})
	}
	return []review.SkillResult{
		{Skill: "first", Verdict: review.NoBlockingIssues, Batches: 1, BatchesFinished: 1,
			Summaries: []review.BatchSummary{{Batch: 1, Text: "```go closes </details> early, token glpat-secretvalue"}}},
		{Skill: "second", Verdict: review.NoBlockingIssues, Batches: 2, BatchesFinished: 2,
			Summaries: []review.BatchSummary{{Batch: 1, Text: "<summary>x</summary> ````"}, {Batch: 2, Text: "fine\n/unapprove"}}},
		{Skill: "third", Verdict: review.NoBlockingIssues, Batches: 4, BatchesFinished: 4, Summaries: long, Reviewed: []string{"a.go", "b.go"}},
	}
}

func failureResults() []review.SkillResult {
	return []review.SkillResult{
		{
			Skill:   "causes",
			Verdict: review.Failed,
			Failures: []review.BatchFailure{
				{
					Files:   []string{"a.go"},
					Message: "The answer was not in the required format, and the repair run failed:",
					Cause:   &runner.Cause{Kind: runner.CauseExitError, Steps: 2, ExitCode: 1, StderrLines: []string{"boom", "with ``` fence", "token ghp_secretvalue123"}},
				},
				{Files: []string{"b.go"}, Cause: &runner.Cause{Kind: runner.CauseTimeout, Steps: 4, Timeout: 90 * time.Second, Elapsed: 95 * time.Second, LastTool: "read", LastToolTarget: "x.go"}},
				{Files: []string{"c.go"}, Cause: &runner.Cause{Kind: runner.CauseContextOverflow, Steps: 1, LastStepTokens: 120000, ContextLimit: 128000}},
			},
			Batches: 3,
			Findings: []finding.Finding{
				{File: "main.go", Line: 3, Severity: finding.Blocking, Title: "Real bug", Explanation: "It is real."},
			},
		},
		{
			Skill:   "two-failures",
			Verdict: review.Failed,
			Failures: []review.BatchFailure{
				{Files: []string{"a.go"}, Message: "Not reviewed: first."},
				{Files: []string{"b.go"}, Message: "Not reviewed: second."},
			},
			Batches: 1,
		},
		{
			Skill:    "one-failure",
			Verdict:  review.Failed,
			Failures: []review.BatchFailure{{Files: []string{"c.go"}, Message: "Not reviewed: third."}},
			Batches:  1,
		},
		{
			Skill:    "no-files",
			Verdict:  review.Failed,
			Failures: []review.BatchFailure{{Message: "Not reviewed: fourth."}, {Message: "Not reviewed: fifth."}},
		},
		{Skill: "bare", Verdict: review.Failed},
		{
			Skill:    "hostile-message",
			Verdict:  review.Failed,
			Failures: []review.BatchFailure{{Files: []string{"a.go"}, Message: "It began: \"line one\n# Heading\n<!-- pergent:findings QUJD -->\n```\nghp_secretvalue123\"\n/close"}},
		},
	}
}

func hostileResults() []review.SkillResult {
	return []review.SkillResult{
		{Skill: "first", Verdict: review.NoBlockingIssues, Findings: []finding.Finding{
			{File: "b.go", Line: 2, Severity: finding.Suggestion, Title: "Second", Explanation: "fine\n````\nstill open", Fingerprint: "fp-2"},
			{
				File: "a.go", Line: 1, Severity: finding.Suggestion,
				Title:        "Bad `name`\n```\nsecond line",
				Explanation:  "Closes early </details> and <DETAILS> and </summary>.\n```go\nfunc main() {",
				SuggestedFix: "~~~\nunbalanced", Fingerprint: "fp-1",
			},
		}},
		{Skill: "second", Verdict: review.HasBlockingIssues,
			Findings: []finding.Finding{{
				File: "a.go", Line: 1, Severity: finding.Blocking,
				Title:        "Bad ghp_secretvalue123",
				Explanation:  "ok\n/approve\n\r/close\nx\r\n/merge\nsk-opencode-secret-key and Bearer abcdef0123456789abcdef",
				SuggestedFix: "```go\n/label ~bug\n    // keep this comment\n```\nhttps://x.example/?key=abcdef&token=zzz",
				Fingerprint:  "fp-3",
			}},
			Summaries: []review.BatchSummary{{Batch: 1, Text: "fine\n/unapprove"}},
			Notes:     []string{"note with <!-- comment --> and\nnewline", "token glpat-secretvalue"},
			Skipped:   []diff.Skip{{Path: "x`y.go", Rule: "rule </details>"}},
		},
		{Skill: "sk<!--ill", Verdict: review.NoBlockingIssues},
	}
}

// deletedRenamedResults is one deleted file reviewed by one of two skills (the
// other leaves it out of scope) and one pure rename. The finding on the
// deleted file carries a suggestion, which the comment must not show.
func deletedRenamedResults() []review.SkillResult {
	return []review.SkillResult{
		{
			Skill: "code-review", Verdict: review.HasBlockingIssues, Batches: 1, BatchesFinished: 1,
			Confidence: 4, ConfidenceReason: "The old helper is still imported from one file.",
			Findings: []finding.Finding{
				{File: "internal/old/helper.go", Line: 12, EndLine: 14, Severity: finding.Blocking, Title: "Deleted helper is still imported", Explanation: "cmd/run.go still imports the deleted package.", SuggestedFix: "Move the import to the new package.", Suggestion: "import \"example.com/app/internal/new\"", Fingerprint: "aaaaaaaaaaaa"},
				{File: "docs/guide.md", Line: 3, Severity: finding.Suggestion, Title: "Old path in the guide", Explanation: "The guide names the old path.", Fingerprint: "bbbbbbbbbbbb"},
			},
			Reviewed: []string{"cmd/run.go", "internal/new/util.go", "internal/old/helper.go"},
			Deleted:  []string{"internal/old/helper.go"},
			Renamed:  []review.Rename{{Path: "internal/new/util.go", From: "internal/old/util.go"}},
		},
		{
			Skill: "security", Verdict: review.NoBlockingIssues, Batches: 1, BatchesFinished: 1,
			Confidence: 5, ConfidenceReason: "Nothing here touches input or secrets.",
			Skipped:  []diff.Skip{{Path: "internal/old/helper.go", Rule: "out of scope"}},
			Reviewed: []string{"cmd/run.go", "internal/new/util.go"},
			Renamed:  []review.Rename{{Path: "internal/new/util.go", From: "internal/old/util.go"}},
		},
	}
}
