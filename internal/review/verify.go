package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/gopaytech/pergent/internal/finding"
	"github.com/gopaytech/pergent/internal/runner"
	"github.com/gopaytech/pergent/internal/skill"
)

// verifyMinRemaining is the least review budget that must be left for a
// skill's blocking findings to be verified.
const verifyMinRemaining = 90 * time.Second

const (
	verifyMessage = "Check the attached blocking findings."

	verifyPrompt = "You are checking another reviewer's blocking findings on a code change. For each finding in the attached JSON, open the file it refers to and decide whether the problem is real in the current code. Do not add new findings.\n\n" +
		"End your final answer with exactly one fenced code block tagged `json`, with nothing after it:\n\n" +
		"```json\n" +
		`{"results": [{"fingerprint": "abc123def456", "real": true, "reason": "One sentence"}]}` + "\n" +
		"```\n"

	verifyBadFormat = "Verification failed: the answer was not in the required format."
)

var verifyBlockRe = regexp.MustCompile("(?s)```json[ \t]*\r?\n(.*?)```")

// verifyResult is one entry of the verifier's answer. Real is a pointer so
// an entry without it is ignored rather than read as false.
type verifyResult struct {
	Fingerprint string `json:"fingerprint"`
	Real        *bool  `json:"real"`
	Reason      string `json:"reason"`
}

// parseVerifyAnswer returns the fingerprints the verifier judged not real,
// from the last ```json block of its answer.
func parseVerifyAnswer(text string) (notReal map[string]bool, err error) {
	matches := verifyBlockRe.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil, errors.New("no json code block found in the answer")
	}
	var a struct {
		Results *[]verifyResult `json:"results"`
	}
	if err := json.Unmarshal([]byte(matches[len(matches)-1][1]), &a); err != nil {
		return nil, fmt.Errorf("invalid json: %w", err)
	}
	if a.Results == nil {
		return nil, errors.New(`json block has no "results" key`)
	}
	notReal = map[string]bool{}
	for _, r := range *a.Results {
		if r.Fingerprint != "" && r.Real != nil && !*r.Real {
			notReal[r.Fingerprint] = true
		}
	}
	return notReal, nil
}

// verify re-checks the skill's blocking findings in one run and drops the
// ones the verifier judges not real. Suggestions are never sent and never
// dropped. A verification that cannot run, or whose answer cannot be used,
// adds a note and keeps every finding; it never fails the skill.
func (r *reviewer) verify(ctx context.Context, s skill.Skill, res *SkillResult) {
	var blocking []finding.Finding
	for _, f := range res.Findings {
		if f.Severity == finding.Blocking {
			blocking = append(blocking, f)
		}
	}
	if len(blocking) == 0 {
		return
	}
	if time.Until(r.deadline) < verifyMinRemaining {
		res.Notes = append(res.Notes, fmt.Sprintf("Verification skipped: less than %s of the review budget was left; blocking findings are unverified.", verifyMinRemaining))
		return
	}
	fail := func(format string, args ...any) {
		res.Notes = append(res.Notes, "Verification failed: "+fmt.Sprintf(format, args...))
	}

	data, err := json.MarshalIndent(map[string][]finding.Finding{"findings": blocking}, "", "  ")
	if err != nil {
		fail("could not encode the findings: %v", err)
		return
	}
	dir, err := os.MkdirTemp("", "pergent-verify-*")
	if err != nil {
		fail("could not write the findings file: %v", err)
		return
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "blocking-findings.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		fail("could not write the findings file: %v", err)
		return
	}

	out, err := r.runWithPrompt(ctx, s, verifyPrompt, []string{path}, verifyMessage)
	if errors.Is(err, errNotStarted) {
		res.Notes = append(res.Notes, fmt.Sprintf("Verification skipped: %s before it could start; blocking findings are unverified.", r.stopReason(ctx)))
		return
	}
	if err != nil {
		fail("could not generate the opencode config: %v", err)
		return
	}
	if out.Status != runner.StatusCompleted {
		if out.Cause == nil {
			fail("the run failed without a reported cause.")
			return
		}
		fail("%s", out.Cause.Message())
		return
	}
	notReal, err := parseVerifyAnswer(out.Output)
	if err != nil {
		res.Notes = append(res.Notes, verifyBadFormat)
		return
	}

	kept := res.Findings[:0:0]
	for _, f := range res.Findings {
		if f.Severity == finding.Blocking && notReal[f.Fingerprint] {
			continue
		}
		kept = append(kept, f)
	}
	res.Findings = kept
}
