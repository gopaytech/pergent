package review

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gopaytech/pergent/internal/config"
	"github.com/gopaytech/pergent/internal/finding"
	"github.com/gopaytech/pergent/internal/runner"
)

func verifyAnswer(t *testing.T, results ...map[string]any) string {
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

// isVerifyCall reports whether the run was the verification run.
func isVerifyCall(c call) bool { return c.opts.Message == verifyMessage }

func titles(fs []finding.Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Title)
	}
	return out
}

func blockingTwoAndSuggestion() []finding.Finding {
	return []finding.Finding{
		{File: "a/x.go", Line: 1, Severity: finding.Blocking, Title: "First bug", Explanation: "e"},
		{File: "a/x.go", Line: 2, Severity: finding.Blocking, Title: "Second bug", Explanation: "e"},
		{File: "a/x.go", Line: 3, Severity: finding.Suggestion, Title: "A suggestion", Explanation: "e"},
	}
}

func TestVerify_DropsFalseFindings(t *testing.T) {
	fp1 := finding.Fingerprint("s", "a/x.go", "First bug")
	f := &fake{}
	f.respond = func(_ int, c call) runner.RunResult {
		if isVerifyCall(c) {
			return completed(verifyAnswer(t, map[string]any{"fingerprint": fp1, "real": false, "reason": "r"}))
		}
		return completed(answer(t, blockingTwoAndSuggestion()...))
	}
	req := newRequest(t, f, patch("a/x.go", "x"))
	req.Budget = 5 * time.Minute

	r := Run(context.Background(), req)[0]

	var verifyCalls []call
	for _, c := range f.calls {
		if isVerifyCall(c) {
			verifyCalls = append(verifyCalls, c)
		}
	}
	if len(verifyCalls) != 1 {
		t.Fatalf("verification ran %d times, want 1", len(verifyCalls))
	}
	vc := verifyCalls[0]
	if len(vc.contents) != 1 {
		t.Fatalf("verification attached %d files, want 1", len(vc.contents))
	}
	var sent struct{ Findings []finding.Finding }
	if err := json.Unmarshal([]byte(vc.contents[0]), &sent); err != nil {
		t.Fatalf("attached file is not JSON: %v\n%s", err, vc.contents[0])
	}
	if got, want := titles(sent.Findings), []string{"First bug", "Second bug"}; !slices.Equal(got, want) {
		t.Errorf("verification was sent %q, want only the blocking findings %q", got, want)
	}
	if got, want := titles(r.Findings), []string{"Second bug", "A suggestion"}; !slices.Equal(got, want) {
		t.Errorf("findings = %q, want %q", got, want)
	}
	if r.Verdict != HasBlockingIssues {
		t.Errorf("Verdict = %q, want %q", r.Verdict, HasBlockingIssues)
	}
	if len(r.Notes) != 0 {
		t.Errorf("Notes = %q, want none", r.Notes)
	}
}

func TestVerify_VerdictComputedAfterVerification(t *testing.T) {
	fp := finding.Fingerprint("s", "a/x.go", "Only bug")
	f := &fake{}
	f.respond = func(_ int, c call) runner.RunResult {
		if isVerifyCall(c) {
			return completed(verifyAnswer(t, map[string]any{"fingerprint": fp, "real": false, "reason": "r"}))
		}
		return completed(answer(t,
			finding.Finding{File: "a/x.go", Line: 1, Severity: finding.Blocking, Title: "Only bug", Explanation: "e"},
			finding.Finding{File: "a/x.go", Line: 2, Severity: finding.Suggestion, Title: "A suggestion", Explanation: "e"}))
	}
	req := newRequest(t, f, patch("a/x.go", "x"))
	req.Budget = 5 * time.Minute

	r := Run(context.Background(), req)[0]
	if r.Verdict != NoBlockingIssues {
		t.Errorf("Verdict = %q, want %q", r.Verdict, NoBlockingIssues)
	}
	if got, want := titles(r.Findings), []string{"A suggestion"}; !slices.Equal(got, want) {
		t.Errorf("findings = %q, want %q", got, want)
	}
}

func TestVerify_UsesVerifyPrompt(t *testing.T) {
	var prompts []string
	f := &fake{}
	f.respond = func(_ int, c call) runner.RunResult {
		if isVerifyCall(c) {
			data, err := os.ReadFile(c.opts.ConfigPath)
			if err != nil {
				t.Errorf("reading config %q: %v", c.opts.ConfigPath, err)
			}
			var cfg struct {
				Agent map[string]struct {
					Prompt string `json:"prompt"`
				} `json:"agent"`
			}
			json.Unmarshal(data, &cfg)
			prompts = append(prompts, cfg.Agent[config.AgentName].Prompt)
			return completed(verifyAnswer(t))
		}
		return completed(answer(t, blockingTwoAndSuggestion()...))
	}
	req := newRequest(t, f, patch("a/x.go", "x"))
	t.Setenv("OPENCODE_PROVIDER", "anthropic")
	t.Setenv("OPENCODE_MODEL", "test-model")
	req.Budget = 5 * time.Minute

	r := Run(context.Background(), req)[0]
	if len(prompts) != 1 {
		t.Fatalf("verification ran %d times, want 1", len(prompts))
	}
	if prompts[0] != verifyPrompt {
		t.Errorf("verification agent prompt = %q, want the verify prompt", prompts[0])
	}
	if !strings.HasPrefix(verifyPrompt, "You are checking another reviewer's blocking findings on a code change.") ||
		!strings.HasSuffix(verifyPrompt, "```json\n{\"results\": [{\"fingerprint\": \"abc123def456\", \"real\": true, \"reason\": \"One sentence\"}]}\n```\n") {
		t.Errorf("verifyPrompt = %q, does not match the required copy", verifyPrompt)
	}
	if len(r.Findings) != 3 {
		t.Errorf("findings = %q, want all 3 kept (missing from results)", titles(r.Findings))
	}
}

func TestVerify_SkippedWhenLowOnTime(t *testing.T) {
	f := &fake{}
	f.respond = func(_ int, c call) runner.RunResult {
		return completed(answer(t, blockingTwoAndSuggestion()...))
	}
	req := newRequest(t, f, patch("a/x.go", "x"))
	req.Budget = verifyMinRemaining - time.Second

	r := Run(context.Background(), req)[0]
	for _, c := range f.calls {
		if isVerifyCall(c) {
			t.Error("verification ran with less than verifyMinRemaining left")
		}
	}
	const want = "Verification skipped: less than 1m30s of the review budget was left; blocking findings are unverified."
	if !slices.Contains(r.Notes, want) {
		t.Errorf("Notes = %q, want it to contain %q", r.Notes, want)
	}
	if len(r.Findings) != 3 || r.Verdict != HasBlockingIssues {
		t.Errorf("result = %+v, want all findings kept", r)
	}
}

func TestVerify_FailureKeepsFindings(t *testing.T) {
	cause := &runner.Cause{Kind: runner.CauseNoAnswer}
	f := &fake{}
	f.respond = func(_ int, c call) runner.RunResult {
		if isVerifyCall(c) {
			return runner.RunResult{Status: runner.StatusFailed, Cause: cause}
		}
		return completed(answer(t, blockingTwoAndSuggestion()...))
	}
	req := newRequest(t, f, patch("a/x.go", "x"))
	req.Budget = 5 * time.Minute

	r := Run(context.Background(), req)[0]
	want := "Verification failed: " + cause.Message()
	if !slices.Contains(r.Notes, want) {
		t.Errorf("Notes = %q, want it to contain %q", r.Notes, want)
	}
	if len(r.Findings) != 3 {
		t.Errorf("findings = %q, want all 3 kept", titles(r.Findings))
	}
	if r.Verdict != HasBlockingIssues {
		t.Errorf("Verdict = %q, want %q (a failed verification never fails the skill)", r.Verdict, HasBlockingIssues)
	}
}

func TestVerify_UnparsableAnswerKeepsFindings(t *testing.T) {
	f := &fake{}
	f.respond = func(_ int, c call) runner.RunResult {
		if isVerifyCall(c) {
			return completed("They all look fine to me.")
		}
		return completed(answer(t, blockingTwoAndSuggestion()...))
	}
	req := newRequest(t, f, patch("a/x.go", "x"))
	req.Budget = 5 * time.Minute

	r := Run(context.Background(), req)[0]
	const want = "Verification failed: the answer was not in the required format."
	if !slices.Contains(r.Notes, want) {
		t.Errorf("Notes = %q, want it to contain %q", r.Notes, want)
	}
	if len(r.Findings) != 3 || r.Verdict != HasBlockingIssues {
		t.Errorf("result = %+v, want all findings kept", r)
	}
}

func TestVerify_NotRunWithoutBlocking(t *testing.T) {
	f := &fake{}
	f.respond = func(_ int, c call) runner.RunResult {
		return completed(answer(t, finding.Finding{File: "a/x.go", Line: 3, Severity: finding.Suggestion, Title: "A suggestion", Explanation: "e"}))
	}
	req := newRequest(t, f, patch("a/x.go", "x"))
	req.Budget = 5 * time.Minute

	r := Run(context.Background(), req)[0]
	if len(f.calls) != 1 {
		t.Errorf("runner called %d times, want 1 (no verification)", len(f.calls))
	}
	if len(r.Notes) != 0 || len(r.Findings) != 1 {
		t.Errorf("result = %+v, want the suggestion and no notes", r)
	}
}
