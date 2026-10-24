// Package review runs the skills' reviews of a change: it selects each
// skill's files, batches them, runs one opencode review per batch, parses
// the structured findings and decides each skill's verdict.
package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gopaytech/pergent/internal/config"
	"github.com/gopaytech/pergent/internal/diff"
	"github.com/gopaytech/pergent/internal/finding"
	"github.com/gopaytech/pergent/internal/runner"
	"github.com/gopaytech/pergent/internal/skill"
)

// Verdict is a skill's overall outcome. Failed never counts as a pass.
type Verdict string

const (
	NoBlockingIssues  Verdict = "no_blocking_issues"
	HasBlockingIssues Verdict = "has_blocking_issues"
	Failed            Verdict = "failed"
)

// RunFunc runs opencode once; runner.Run in production.
type RunFunc func(ctx context.Context, opts runner.Options) runner.RunResult

// Request is one review of a change by a set of skills.
type Request struct {
	Skills         []skill.Skill
	Diff, RepoPath string
	// Budget bounds the whole review (10 minutes when not positive).
	Budget time.Duration
	// Concurrency is the most opencode runs in flight at once across the
	// whole review (1 when not positive).
	Concurrency, MaxTurns, ContextLimit int
	// Previous holds each skill's findings from the previous review, by
	// skill name.
	Previous map[string][]finding.Finding
	// Run runs opencode; nil means runner.Run.
	Run RunFunc
}

// BatchFailure records in-scope files that were not reviewed. Message
// explains why when Cause is nil; when both are set, Message is context
// shown before the cause.
type BatchFailure struct {
	Files   []string
	Cause   *runner.Cause
	Message string
}

// SkillResult is one skill's review. Batches is the number of batches built
// (oversize files excluded); BatchesFinished counts those whose run ended
// with an answer in the required format.
type SkillResult struct {
	Skill                    string
	Verdict                  Verdict
	Findings                 []finding.Finding
	Skipped                  []diff.Skip
	Failures                 []BatchFailure
	Notes                    []string
	Batches, BatchesFinished int
}

const defaultBudget = 10 * time.Minute

// excerptLen is how many characters of a malformed answer a failure shows.
const excerptLen = 200

const (
	noFilesNote        = "no files in scope"
	singleBatchMessage = "Review the attached diff."
	multiBatchMessage  = "Review the attached diff. It is batch %d of %d of this change; the attached list shows every changed file. Review only the files in this batch's diff, and read any file in the repository you need."
	previousMessage    = "Also attached are the findings from your previous review of an earlier revision, as JSON. Stay consistent with them: focus on what changed since, don't re-litigate unchanged code, drop findings the new changes resolve, and reuse a finding's fingerprint when it still applies."
	repairMessage      = "The attached review is not in the required output format (%s). Rewrite it in the required format without changing its findings."
	repairFailedPrefix = "The model's answer was not in the required format (%v), and the repair run failed:"
)

// errBudget is the review context's cause when the budget runs out.
var errBudget = errors.New("review budget ran out")

// errNotStarted reports that a run was not started because the review's
// deadline passed or its context ended.
var errNotStarted = errors.New("run not started")

// reviewer holds what every skill's review shares.
type reviewer struct {
	req         Request
	files       []diff.FileDiff
	changedList string
	classifier  diff.Classifier
	// slots holds one token per opencode run in flight.
	slots    chan struct{}
	deadline time.Time
}

// Run reviews the change with every skill and batch concurrently, with at
// most req.Concurrency opencode runs at once and all of them within
// req.Budget, and returns the results in req.Skills order.
func Run(ctx context.Context, req Request) []SkillResult {
	if req.Run == nil {
		req.Run = runner.Run
	}
	if req.Concurrency < 1 {
		req.Concurrency = 1
	}
	if req.Budget <= 0 {
		req.Budget = defaultBudget
	}
	files := diff.Split(req.Diff)
	var list strings.Builder
	for _, f := range files {
		fmt.Fprintf(&list, "- %s\n", f.Path)
	}
	ctx, cancel := context.WithDeadlineCause(ctx, time.Now().Add(req.Budget), errBudget)
	defer cancel()
	deadline, _ := ctx.Deadline()
	r := &reviewer{
		req:         req,
		files:       files,
		changedList: list.String(),
		classifier:  diff.NewClassifier(req.RepoPath),
		slots:       make(chan struct{}, req.Concurrency),
		deadline:    deadline,
	}

	results := make([]SkillResult, len(req.Skills))
	var wg sync.WaitGroup
	for i, s := range req.Skills {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = r.reviewSkill(ctx, s)
		}()
	}
	wg.Wait()
	return results
}

// batchOutcome is one batch's review: its findings, why it was not
// reviewed, or that its run never started.
type batchOutcome struct {
	findings   []finding.Finding
	failure    *BatchFailure
	notStarted bool
}

func (r *reviewer) reviewSkill(ctx context.Context, s skill.Skill) SkillResult {
	res := SkillResult{Skill: s.Name}
	in, skipped := Select(s, r.files, r.classifier)
	res.Skipped = skipped
	if len(in) == 0 {
		res.Verdict = NoBlockingIssues
		res.Notes = append(res.Notes, noFilesNote)
		return res
	}

	budget := diff.BatchBudget(r.req.ContextLimit)
	batches, oversize := diff.Batch(in, budget)
	res.Batches = len(batches)
	for _, f := range oversize {
		res.Failures = append(res.Failures, BatchFailure{
			Files: []string{f.Path},
			Message: fmt.Sprintf("Not reviewed: its diff is about %d tokens, larger than the per-batch limit of %d tokens.",
				diff.EstimateTokens(f.Patch), budget),
		})
	}

	prev, hasPrev := r.req.Previous[s.Name]
	outcomes := make([]batchOutcome, len(batches))
	var wg sync.WaitGroup
	for i, b := range batches {
		wg.Add(1)
		go func() {
			defer wg.Done()
			outcomes[i] = r.reviewBatch(ctx, s, b, i+1, len(batches), prev, hasPrev)
		}()
	}
	wg.Wait()

	// Collect in batch order so the result doesn't depend on which run
	// finished first.
	var all []finding.Finding
	var unstarted []string
	for i, o := range outcomes {
		switch {
		case o.notStarted:
			for _, f := range batches[i] {
				unstarted = append(unstarted, f.Path)
			}
		case o.failure != nil:
			res.Failures = append(res.Failures, *o.failure)
		default:
			res.BatchesFinished++
			all = append(all, o.findings...)
		}
	}
	if len(unstarted) > 0 {
		res.Failures = append(res.Failures, BatchFailure{
			Files:   unstarted,
			Message: fmt.Sprintf("Not reviewed: %s; %d of %d batches finished.", r.stopReason(ctx), res.BatchesFinished, res.Batches),
		})
	}
	res.Findings = finding.Dedup(assignFingerprints(s.Name, all, prev))
	// The optional cross-batch run goes before verification so the findings it
	// adds are verified too.
	r.crossBatch(ctx, s, &res, prev)
	r.verify(ctx, s, &res)
	// The verdict is computed after verification: findings it drops no
	// longer block.
	res.Verdict = verdict(res)
	return res
}

// reviewBatch runs the review of one batch, and one repair run when the
// answer is not in the required format. It returns the batch's findings, why
// the batch was not reviewed, or that its run never started.
func (r *reviewer) reviewBatch(ctx context.Context, s skill.Skill, batch []diff.FileDiff, n, total int, prev []finding.Finding, hasPrev bool) batchOutcome {
	paths := make([]string, len(batch))
	var patch strings.Builder
	for i, f := range batch {
		paths[i] = f.Path
		patch.WriteString(f.Patch)
	}
	notReviewed := func(format string, args ...any) batchOutcome {
		return batchOutcome{failure: &BatchFailure{Files: paths, Message: "Not reviewed: " + fmt.Sprintf(format, args...)}}
	}

	dir, err := os.MkdirTemp("", "pergent-batch-*")
	if err != nil {
		return notReviewed("could not write the batch files: %v", err)
	}
	defer os.RemoveAll(dir)
	write := func(name, content string) (string, error) {
		p := filepath.Join(dir, name)
		return p, os.WriteFile(p, []byte(content), 0600)
	}

	message := singleBatchMessage
	if total > 1 {
		message = fmt.Sprintf(multiBatchMessage, n, total)
	}
	type attachment struct{ name, content string }
	attachments := []attachment{
		{"diff.patch", patch.String()},
		{"changed-files.md", r.changedList},
	}
	if hasPrev {
		if prev == nil {
			prev = []finding.Finding{}
		}
		data, err := json.MarshalIndent(map[string][]finding.Finding{"findings": prev}, "", "  ")
		if err != nil {
			return notReviewed("could not encode the previous findings: %v", err)
		}
		attachments = append(attachments, attachment{"previous-findings.json", string(data)})
		message += " " + previousMessage
	}
	var files []string
	for _, a := range attachments {
		p, err := write(a.name, a.content)
		if err != nil {
			return notReviewed("could not write the batch files: %v", err)
		}
		files = append(files, p)
	}

	res, err := r.run(ctx, s, files, message)
	if errors.Is(err, errNotStarted) {
		return batchOutcome{notStarted: true}
	}
	if err != nil {
		return notReviewed("could not generate the opencode config: %v", err)
	}
	if res.Status != runner.StatusCompleted {
		if res.Cause == nil {
			return notReviewed("the run failed without a reported cause.")
		}
		return batchOutcome{failure: &BatchFailure{Files: paths, Cause: res.Cause}}
	}
	fs, parseErr := finding.ParseAnswer(res.Output)
	if parseErr == nil {
		return batchOutcome{findings: fs}
	}

	bad, err := write("review-answer.md", res.Output)
	if err != nil {
		return notReviewed("could not write the answer for repair: %v", err)
	}
	repaired, err := r.run(ctx, s, []string{bad}, fmt.Sprintf(repairMessage, parseErr))
	if errors.Is(err, errNotStarted) {
		return notReviewed("%s before the repair run could start; the model's answer was not in the required format (%v).", r.stopReason(ctx), parseErr)
	}
	if err != nil {
		return notReviewed("could not generate the opencode config: %v", err)
	}
	if repaired.Status != runner.StatusCompleted {
		if repaired.Cause == nil {
			return notReviewed("the answer was not in the required format (%v), and the repair run failed without a reported cause.", parseErr)
		}
		return batchOutcome{failure: &BatchFailure{Files: paths, Cause: repaired.Cause, Message: fmt.Sprintf(repairFailedPrefix, parseErr)}}
	}
	if fs, err := finding.ParseAnswer(repaired.Output); err == nil {
		return batchOutcome{findings: fs}
	}
	return notReviewed("the answer was not in the required format (%v), and a repair run did not fix it. The answer began: %s",
		parseErr, excerpt(res.Output))
}

// stopReason says why runs can no longer start: the budget ran out, or the
// caller's context ended first.
func (r *reviewer) stopReason(ctx context.Context) string {
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, errBudget) {
		return fmt.Sprintf("the review was cancelled (%v)", cause)
	}
	return fmt.Sprintf("the %s review budget ran out", r.req.Budget)
}

// acquire waits for a free run slot. It reports false, holding no slot,
// when the review's context ends or its deadline has passed.
func (r *reviewer) acquire(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	select {
	case r.slots <- struct{}{}:
	case <-ctx.Done():
		return false
	}
	if ctx.Err() != nil || time.Until(r.deadline) <= 0 {
		<-r.slots
		return false
	}
	return true
}

func (r *reviewer) release() { <-r.slots }

// run waits for a run slot, generates the opencode config for skill s and
// runs opencode once with the time left in the budget. It returns
// errNotStarted when no time is left.
func (r *reviewer) run(ctx context.Context, s skill.Skill, files []string, message string) (runner.RunResult, error) {
	return r.runWithPrompt(ctx, s, s.Prompt(), files, message)
}

// runWithPrompt is run with an explicit agent prompt in place of the
// skill's own.
func (r *reviewer) runWithPrompt(ctx context.Context, s skill.Skill, prompt string, files []string, message string) (runner.RunResult, error) {
	if !r.acquire(ctx) {
		return runner.RunResult{}, errNotStarted
	}
	defer r.release()
	configPath, cleanup, err := config.GenerateOpenCodeConfig(r.req.MaxTurns, r.req.ContextLimit, prompt, r.req.RepoPath)
	if err != nil {
		return runner.RunResult{}, err
	}
	defer cleanup()
	timeout := time.Until(r.deadline)
	if timeout <= 0 {
		return runner.RunResult{}, errNotStarted
	}
	// The step and context limits only apply when pergent wrote the config.
	stepLimit, ctxLimit := 0, 0
	if configPath != "" {
		stepLimit, ctxLimit = r.req.MaxTurns, r.req.ContextLimit
	}
	return r.req.Run(ctx, runner.Options{
		SkillName:    s.Name,
		ConfigPath:   configPath,
		Files:        files,
		Message:      message,
		RepoPath:     r.req.RepoPath,
		Timeout:      timeout,
		StepLimit:    stepLimit,
		ContextLimit: ctxLimit,
	}), nil
}

// assignFingerprints keeps a finding's fingerprint when it matches one of
// the previous findings and computes it otherwise.
func assignFingerprints(skillName string, fs, prev []finding.Finding) []finding.Finding {
	known := make(map[string]bool, len(prev))
	for _, p := range prev {
		if p.Fingerprint != "" {
			known[p.Fingerprint] = true
		}
	}
	out := make([]finding.Finding, len(fs))
	for i, f := range fs {
		if !known[f.Fingerprint] {
			f.Fingerprint = finding.Fingerprint(skillName, f.File, f.Title)
		}
		out[i] = f
	}
	return out
}

func verdict(res SkillResult) Verdict {
	if len(res.Failures) > 0 {
		return Failed
	}
	for _, f := range res.Findings {
		if f.Severity == finding.Blocking {
			return HasBlockingIssues
		}
	}
	return NoBlockingIssues
}

// excerpt returns the first excerptLen characters of s.
func excerpt(s string) string {
	runes := []rune(s)
	if len(runes) <= excerptLen {
		return s
	}
	return string(runes[:excerptLen])
}
