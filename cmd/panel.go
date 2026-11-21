package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"syscall"
	"time"

	"github.com/gopaytech/pergent/internal/diff"
	"github.com/gopaytech/pergent/internal/finding"
	"github.com/gopaytech/pergent/internal/panel"
	"github.com/gopaytech/pergent/internal/redact"
	"github.com/gopaytech/pergent/internal/result"
	"github.com/gopaytech/pergent/internal/review"
	"github.com/gopaytech/pergent/render"
)

// noUsableMember is the usage error of a merge with nothing to merge.
const noUsableMember = "--panel needs at least one usable member result"

// readMember reads a member's agent result. It is usable when it decodes,
// has schema_version 1 and status completed; otherwise reason says why, for
// the log (never the result). A file over the result size limit is not read:
// no member writes one. Only a regular file is opened, so a FIFO cannot
// block the merge.
func readMember(path string) (r *result.Result, reason string) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Sprintf("reading %q: %v", path, pathCause(err))
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Sprintf("%q is not a regular file", path)
	}
	// O_NONBLOCK, and the check again on the open file, in case the path was
	// replaced after the Stat.
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Sprintf("reading %q: %v", path, pathCause(err))
	}
	defer f.Close()
	if st, err = f.Stat(); err != nil {
		return nil, fmt.Sprintf("reading %q: %v", path, pathCause(err))
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Sprintf("%q is not a regular file", path)
	}
	data, err := io.ReadAll(io.LimitReader(f, result.MaxSize+1))
	if err != nil {
		return nil, fmt.Sprintf("reading %q: %v", path, pathCause(err))
	}
	if len(data) > result.MaxSize {
		return nil, fmt.Sprintf("%q is over the 10 MB limit", path)
	}
	var res result.Result
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, fmt.Sprintf("decoding %q: %v", path, err)
	}
	switch {
	case res.SchemaVersion != result.SchemaVersion:
		return nil, fmt.Sprintf("schema_version %d, want %d", res.SchemaVersion, result.SchemaVersion)
	case res.Status == result.StatusError:
		return nil, "status error"
	case res.Status != result.StatusCompleted:
		return nil, fmt.Sprintf("status %q", res.Status)
	}
	return &res, ""
}

// pathCause is err without the path a *fs.PathError repeats.
func pathCause(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}

// memberMismatch says why a member's result is not a review of base..head,
// or "" when it is one.
func memberMismatch(r *result.Result, base, head string) string {
	switch {
	case r.Head != head:
		return fmt.Sprintf("head %q, want %q", r.Head, head)
	case r.Base != base:
		return fmt.Sprintf("base %q, want %q", r.Base, base)
	}
	return ""
}

// memberSkills converts a member result's skills for review.RunPanel: every
// skill of a usable result, nil for an unusable one. A skill with a failure,
// a batch that did not finish, a file not reviewed or a verdict pergent does
// not know is failed, so the panel never uses it. So is a skill the result
// holds twice.
func memberSkills(r *result.Result) map[string]review.SkillResult {
	if r == nil {
		return nil
	}
	skills := make(map[string]review.SkillResult, len(r.Skills))
	for _, s := range r.Skills {
		if prev, ok := skills[s.Name]; ok {
			prev.Verdict = review.Failed
			skills[s.Name] = prev
			continue
		}
		skills[s.Name] = memberSkill(s)
	}
	return skills
}

// memberSkill is one skill of a member's result as a review result. The
// tokens, runs and duration stay with the member: the merge reports its own.
// A failure keeps its text but not its cause, which the member reported.
func memberSkill(s result.Skill) review.SkillResult {
	res := review.SkillResult{
		Skill:            s.Name,
		Verdict:          review.Verdict(s.Verdict),
		Notes:            s.Notes,
		Batches:          s.Batches,
		BatchesFinished:  s.BatchesFinished,
		Reviewed:         s.Files.Reviewed,
		Carried:          s.Files.Carried,
		Deleted:          s.Files.Deleted,
		Confidence:       s.Confidence,
		ConfidenceReason: s.ConfidenceReason,
	}
	for _, f := range s.Findings {
		res.Findings = append(res.Findings, finding.Finding{
			File: f.File, Line: f.Line, EndLine: f.EndLine, Severity: memberSeverity(f.Severity),
			Title: f.Title, Explanation: f.Explanation, SuggestedFix: f.SuggestedFix, Suggestion: f.Suggestion,
			Fingerprint: f.Fingerprint, Linked: f.Linked,
		})
	}
	for _, sk := range s.Files.Skipped {
		res.Skipped = append(res.Skipped, diff.Skip{Path: sk.Path, Rule: sk.Rule})
	}
	for _, r := range s.Files.Renamed {
		res.Renamed = append(res.Renamed, review.Rename{Path: r.Path, From: r.From})
	}
	for _, fr := range s.Files.Rules {
		res.FileRules = append(res.FileRules, review.FileRule{Path: fr.Path, Rule: fr.Rule})
	}
	for _, f := range s.Failures {
		res.Failures = append(res.Failures, review.BatchFailure{Files: f.Files, Message: f.Message})
	}
	for _, sum := range s.Summaries {
		res.Summaries = append(res.Summaries, review.BatchSummary{Batch: sum.Batch, Text: sum.Text})
	}
	for _, bc := range s.Checks {
		checks := make([]finding.Check, 0, len(bc.Checks))
		for _, c := range bc.Checks {
			checks = append(checks, finding.Check{Section: c.Section, Name: c.Name, Status: finding.CheckStatus(c.Status), Detail: c.Detail})
		}
		res.Checks = append(res.Checks, review.BatchChecks{Batch: bc.Batch, Checks: checks, Dropped: bc.Dropped, Findings: bc.Findings})
	}
	switch res.Verdict {
	case review.NoBlockingIssues, review.HasBlockingIssues:
		// A file the member did not review is a batch that failed, even when
		// the result reports no failure for it.
		if len(res.Failures) > 0 || res.BatchesFinished < res.Batches || len(s.Files.NotReviewed) > 0 {
			res.Verdict = review.Failed
		}
	default:
		res.Verdict = review.Failed
	}
	return res
}

// memberSeverity is a member finding's severity: a suggestion only when it
// says so, so an unknown severity is never dropped to a suggestion.
func memberSeverity(s string) finding.Severity {
	if finding.Severity(s) == finding.Suggestion {
		return finding.Suggestion
	}
	return finding.Blocking
}

// readPanelMember reads one member's result, and logs why it can't be
// used: it can't be read, or it is not a review of in's base and head.
func readPanelMember(path, role string, in *agentInputs, logger *log.Logger) *result.Result {
	r, reason := readMember(path)
	if r != nil {
		if reason = memberMismatch(r, in.Base, in.Head); reason != "" {
			r = nil
		}
	}
	if r == nil {
		logger.Printf("panel: member %s can't be used: %s", role, redact.String(reason))
	}
	return r
}

// runPanel reviews nothing: it merges the members and returns the result to
// deliver, or an error result.
func runPanel(ctx context.Context, in *agentInputs, meta result.Meta, run review.RunFunc, logger *log.Logger) (result.Result, *result.Error) {
	a := readPanelMember(in.panelAPath, panel.A, in, logger)
	b := readPanelMember(in.panelBPath, panel.B, in, logger)
	var mode string
	switch {
	case a != nil && b != nil:
		mode = review.PanelBoth
	case a != nil:
		mode = review.PanelAOnly
	case b != nil:
		mode = review.PanelBOnly
	default:
		return result.Result{}, &result.Error{Code: result.CodeUsage, Message: noUsableMember}
	}

	logger.Printf("Merging the review panel (%s) for %d skill(s), up to %d opencode runs at once, within %s",
		mode, len(in.Skills), in.Review.Concurrency, in.Review.Timeout)
	results := review.RunPanel(ctx, review.PanelRequest{
		Request: agentRequest(in, run),
		A:       review.Member{Role: panel.A, Model: in.panelA, Key: in.panelAKey, Skills: memberSkills(a)},
		B:       review.Member{Role: panel.B, Model: in.panelB, Key: in.panelBKey, Skills: memberSkills(b)},
		NoJudge: in.panelNoJudge,
	})
	meta.Finished = time.Now()
	meta.Budget = in.Review.Timeout
	logFailures(results, logger)

	p := result.Panel{
		A:      result.PanelMember{Provider: in.panelA.Provider, Model: in.panelA.Model},
		B:      result.PanelMember{Provider: in.panelB.Provider, Model: in.panelB.Model},
		Mode:   mode,
		Judged: !in.panelNoJudge,
	}
	res := result.BuildPanel(meta, p, results)
	names := &render.PanelNames{A: in.panelA.Provider + "/" + in.panelA.Model, B: in.panelB.Provider + "/" + in.panelB.Model}
	res.Comment = agentComment(in, results, res, names)
	return res, nil
}
