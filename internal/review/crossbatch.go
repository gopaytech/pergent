package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gopaytech/pergent/internal/finding"
	"github.com/gopaytech/pergent/internal/runner"
	"github.com/gopaytech/pergent/internal/skill"
)

const crossBatchMessage = "This change was reviewed in %d batches. Attached are the list of every changed file and the findings from all batches. Look only for problems that span batches, such as conflicting changes between files in different batches. Report only new findings."

// crossBatch runs one extra review that looks for problems spanning the
// skill's batches and merges its new findings into res.Findings. It runs only
// when the skill asks for it and there is more than one batch. A cross-batch
// check that cannot run, or whose answer cannot be used, adds a note and
// changes nothing else: every file was already reviewed, so it never fails
// the skill. It returns what the run's answer says it left out because of past
// dismissals.
func (r *reviewer) crossBatch(ctx context.Context, s skill.Skill, res *SkillResult, prev []finding.Finding) (leftOut []finding.LeftOut) {
	if !s.CrossBatch || res.Batches <= 1 {
		return
	}
	if time.Until(r.deadline) < verifyMinRemaining {
		res.Notes = append(res.Notes, fmt.Sprintf("Cross-batch check skipped: less than %s of the review budget was left.", verifyMinRemaining))
		return
	}
	fail := func(format string, args ...any) {
		res.Notes = append(res.Notes, "Cross-batch check failed: "+fmt.Sprintf(format, args...))
	}

	batchFindings := res.Findings
	if batchFindings == nil {
		batchFindings = []finding.Finding{}
	}
	data, err := json.MarshalIndent(map[string][]finding.Finding{"findings": batchFindings}, "", "  ")
	if err != nil {
		fail("could not encode the batch findings: %v", err)
		return
	}
	dir, err := os.MkdirTemp("", "pergent-crossbatch-*")
	if err != nil {
		fail("could not write the check's files: %v", err)
		return
	}
	defer os.RemoveAll(dir)
	var files []string
	for _, a := range []struct{ name, content string }{
		{"changed-files.md", r.changedList},
		{"batch-findings.json", string(data)},
	} {
		p := filepath.Join(dir, a.name)
		if err := os.WriteFile(p, []byte(a.content), 0600); err != nil {
			fail("could not write the check's files: %v", err)
			return
		}
		files = append(files, p)
	}

	message := fmt.Sprintf(crossBatchMessage, res.Batches)
	// The run has no diff of its own, so it is told about every diff.
	if note := r.diffsNote(crossDiffsMessage, nil); note != "" {
		message += " " + note
	}
	if rejected := r.req.Dismissals[s.Name]; len(rejected) > 0 {
		if p, err := attachRejected(dir, rejected); err != nil {
			res.noteDismissalsFailed()
		} else {
			files = append(files, p)
			message += " " + rejectedMessage
		}
	}

	out, err := r.run(ctx, s, "cross", files, message)
	res.Usage = res.Usage.Add(out.Usage)
	if errors.Is(err, errNotStarted) {
		res.Notes = append(res.Notes, fmt.Sprintf("Cross-batch check skipped: %s before it could start.", r.stopReason(ctx)))
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
	ans, err := finding.ParseAnswer(out.Output)
	if err != nil {
		fail("the answer was not in the required format (%v). The answer began: %s", err, excerpt(out.Output))
		return
	}
	merged := append(append([]finding.Finding{}, res.Findings...), assignFingerprints(s.Name, ans.Findings, prev)...)
	res.Findings = finding.Dedup(merged)
	return ans.LeftOut
}
