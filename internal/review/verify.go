package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
		`{"results": [{"id": "1", "real": true, "reason": "One sentence"}]}` + "\n" +
		"```\n"

	verifyBadFormat = "Verification failed: the answer was not in the required format."
)

// verifyFinding is a blocking finding as sent to the verifier. ID numbers
// the findings of one verification run; fingerprints can't identify them,
// since two findings in one file may share a title.
type verifyFinding struct {
	ID string `json:"id"`
	finding.Finding
}

// verifyResult is one entry of the verifier's answer. Real is a pointer so
// an entry without it is ignored rather than read as false.
type verifyResult struct {
	ID     verifyID `json:"id"`
	Real   *bool    `json:"real"`
	Reason string   `json:"reason"`
}

// verifyID is a result's id. The prompt asks for a string, but a number is
// accepted too.
type verifyID string

func (id *verifyID) UnmarshalJSON(data []byte) error {
	var n json.Number
	if err := json.Unmarshal(data, &n); err == nil {
		*id = verifyID(n.String())
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	*id = verifyID(s)
	return nil
}

// parseVerifyAnswer returns the ids the verifier judged not real, with its
// reason for each, from the last ```json block of its answer that holds a
// "results" list.
func parseVerifyAnswer(text string) (notReal map[string]string, err error) {
	blocks := finding.JSONBlocks(text)
	if len(blocks) == 0 {
		return nil, errors.New("no json code block found in the answer")
	}
	var lastErr error
	for i := len(blocks) - 1; i >= 0; i-- {
		notReal, err := parseVerifyBlock(blocks[i])
		if err == nil {
			return notReal, nil
		}
		if lastErr == nil {
			lastErr = err
		}
	}
	return nil, lastErr
}

func parseVerifyBlock(block finding.JSONBlock) (map[string]string, error) {
	if block.Err != nil {
		return nil, fmt.Errorf("invalid json: %w", block.Err)
	}
	var a struct {
		Results *[]verifyResult `json:"results"`
	}
	if err := json.Unmarshal(block.Value, &a); err != nil {
		return nil, fmt.Errorf("invalid json: %w", err)
	}
	if a.Results == nil {
		return nil, errors.New(`json block has no "results" key`)
	}
	notReal := map[string]string{}
	for _, r := range *a.Results {
		if r.ID != "" && r.Real != nil && !*r.Real {
			notReal[string(r.ID)] = r.Reason
		}
	}
	return notReal, nil
}

// verify re-checks the skill's blocking findings in one run and drops the
// ones the verifier judges not real. Suggestions are never sent and never
// dropped. A verification that cannot run, or whose answer cannot be used,
// adds a note and keeps every finding; it never fails the skill.
func (r *reviewer) verify(ctx context.Context, s skill.Skill, res *SkillResult) {
	var blocking []verifyFinding
	for _, f := range res.Findings {
		if f.Severity == finding.Blocking {
			// The fingerprint is left out so the verifier answers by id.
			f.Fingerprint = ""
			blocking = append(blocking, verifyFinding{ID: strconv.Itoa(len(blocking) + 1), Finding: f})
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

	data, err := json.MarshalIndent(map[string][]verifyFinding{"findings": blocking}, "", "  ")
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

	out, err := r.runWithPrompt(ctx, s, "verify", verifyPrompt, []string{path}, verifyMessage)
	res.Usage = res.Usage.Add(out.Usage)
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

	// Blocking findings are numbered in res.Findings order, as sent.
	kept := res.Findings[:0:0]
	var dropped []string
	n := 0
	for _, f := range res.Findings {
		if f.Severity == finding.Blocking {
			n++
			if reason, ok := notReal[strconv.Itoa(n)]; ok {
				if reason = strings.Join(strings.Fields(reason), " "); reason != "" {
					dropped = append(dropped, fmt.Sprintf("%s (%s)", f.Title, reason))
				} else {
					dropped = append(dropped, f.Title)
				}
				continue
			}
		}
		kept = append(kept, f)
	}
	res.Findings = kept
	if len(dropped) > 0 {
		noun := "findings"
		if len(blocking) == 1 {
			noun = "finding"
		}
		res.Notes = append(res.Notes, fmt.Sprintf("Verification dropped %d of %d blocking %s: %s",
			len(dropped), len(blocking), noun, strings.Join(dropped, "; ")))
	}
}
