// Package result defines the JSON result of a `pergent agent` review: the
// schema (version 1), finding IDs, the error result and the size cap.
package result

import (
	"math"
	"strings"
	"time"

	"github.com/gopaytech/pergent/internal/finding"
	"github.com/gopaytech/pergent/internal/output"
	"github.com/gopaytech/pergent/internal/redact"
	"github.com/gopaytech/pergent/internal/review"
)

// SchemaVersion is the result schema version. Adding fields is compatible;
// renaming or removing one needs a new version.
const SchemaVersion = 1

// Status values.
const (
	StatusCompleted = "completed"
	StatusError     = "error"
)

// ErrorCode is the fixed set of reasons a run produces an error result.
type ErrorCode string

const (
	CodeUsage                 ErrorCode = "usage"
	CodeCheckoutMissing       ErrorCode = "checkout_missing"
	CodeCommitMissing         ErrorCode = "commit_missing"
	CodeHeadMismatch          ErrorCode = "head_mismatch"
	CodeDiffFailed            ErrorCode = "diff_failed"
	CodeSkillsInvalid         ErrorCode = "skills_invalid"
	CodeUntrustedInstructions ErrorCode = "untrusted_instructions"
	CodeInternal              ErrorCode = "internal"
)

// Result is the JSON document an agent run delivers.
type Result struct {
	SchemaVersion  int     `json:"schema_version"`
	Status         string  `json:"status"`
	PergentVersion string  `json:"pergent_version,omitempty"`
	Base           string  `json:"base,omitempty"`
	Head           string  `json:"head,omitempty"`
	StartedAt      string  `json:"started_at,omitempty"`
	FinishedAt     string  `json:"finished_at,omitempty"`
	BudgetSeconds  int     `json:"budget_seconds,omitempty"`
	Skills         []Skill `json:"skills,omitempty"`
	Error          *Error  `json:"error,omitempty"`
	Comment        Comment `json:"comment"`
}

// Skill is one skill's review.
type Skill struct {
	Name            string        `json:"name"`
	Verdict         string        `json:"verdict"`
	Findings        []Finding     `json:"findings"`
	Summaries       []Summary     `json:"summaries,omitempty"`
	Checks          []BatchChecks `json:"checks,omitempty"`
	Files           Files         `json:"files"`
	Failures        []Failure     `json:"failures"`
	Notes           []string      `json:"notes"`
	Batches         int           `json:"batches"`
	BatchesFinished int           `json:"batches_finished"`
	DurationSeconds float64       `json:"duration_seconds"`
	Tokens          Tokens        `json:"tokens"`
}

// Finding is a finding with its short ID.
type Finding struct {
	ID           string `json:"id"`
	Fingerprint  string `json:"fingerprint"`
	File         string `json:"file"`
	Line         int    `json:"line,omitempty"`
	Severity     string `json:"severity"`
	Title        string `json:"title"`
	Explanation  string `json:"explanation"`
	SuggestedFix string `json:"suggested_fix,omitempty"`
}

// Summary is the model's summary of one batch.
type Summary struct {
	Batch int    `json:"batch"`
	Text  string `json:"text"`
}

// BatchChecks is the checklist the model reported for one batch.
type BatchChecks struct {
	Batch    int     `json:"batch"`
	Dropped  int     `json:"dropped"`
	Findings int     `json:"findings"`
	Checks   []Check `json:"checks"`
}

// Check is one checklist item.
type Check struct {
	Section string `json:"section"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Detail  string `json:"detail"`
}

// Files says which files a skill reviewed, skipped and could not review.
type Files struct {
	Reviewed    []string      `json:"reviewed"`
	Skipped     []Skipped     `json:"skipped"`
	NotReviewed []NotReviewed `json:"not_reviewed"`
}

// Skipped is a file excluded by a default exclusion rule.
type Skipped struct {
	Path string `json:"path"`
	Rule string `json:"rule"`
}

// NotReviewed is an in-scope file whose batch failed.
type NotReviewed struct {
	Path  string `json:"path"`
	Cause string `json:"cause"`
}

// Failure is a batch that did not finish.
type Failure struct {
	Files   []string `json:"files"`
	Message string   `json:"message"`
	Cause   *Cause   `json:"cause,omitempty"`
}

// Cause is why a run failed.
type Cause struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

// Tokens is the model usage of a skill's runs.
type Tokens struct {
	Input  int `json:"input"`
	Output int `json:"output"`
}

// Comment is the rendered review comment. The caller fills it.
type Comment struct {
	Markdown      string `json:"markdown"`
	FindingsBlock string `json:"findings_block,omitempty"`
}

// Error is why no review was produced.
type Error struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

// Meta is what a result says about the run itself.
type Meta struct {
	PergentVersion, Base, Head string
	Started, Finished          time.Time
	Budget                     time.Duration
}

// Build converts the skills' results into a completed Result with finding
// IDs assigned. The comment is left empty for the caller to fill.
func Build(meta Meta, skills []review.SkillResult) Result {
	var fps []string
	for _, s := range skills {
		for _, f := range s.Findings {
			fps = append(fps, f.Fingerprint)
		}
	}
	ids := AssignIDs(fps)

	r := baseResult(meta, StatusCompleted)
	r.Skills = make([]Skill, 0, len(skills))
	for _, s := range skills {
		r.Skills = append(r.Skills, buildSkill(s, ids))
	}
	return r
}

func baseResult(meta Meta, status string) Result {
	r := Result{
		SchemaVersion:  SchemaVersion,
		Status:         status,
		PergentVersion: meta.PergentVersion,
		Base:           meta.Base,
		Head:           meta.Head,
		BudgetSeconds:  int(meta.Budget / time.Second),
	}
	if !meta.Started.IsZero() {
		r.StartedAt = meta.Started.UTC().Format(time.RFC3339)
	}
	if !meta.Finished.IsZero() {
		r.FinishedAt = meta.Finished.UTC().Format(time.RFC3339)
	}
	return r
}

func buildSkill(s review.SkillResult, ids map[string]string) Skill {
	out := Skill{
		Name:            s.Skill,
		Verdict:         string(s.Verdict),
		Findings:        make([]Finding, 0, len(s.Findings)),
		Files:           Files{Reviewed: nonNil(s.Reviewed), Skipped: []Skipped{}, NotReviewed: []NotReviewed{}},
		Failures:        []Failure{},
		Notes:           nonNil(s.Notes),
		Batches:         s.Batches,
		BatchesFinished: s.BatchesFinished,
		DurationSeconds: math.Round(s.Duration.Seconds()*10) / 10,
		Tokens:          Tokens{Input: s.Usage.Input, Output: s.Usage.Output},
	}
	for _, f := range s.Findings {
		out.Findings = append(out.Findings, buildFinding(f, ids))
	}
	for _, sum := range s.Summaries {
		out.Summaries = append(out.Summaries, Summary{Batch: sum.Batch, Text: sum.Text})
	}
	for _, bc := range s.Checks {
		checks := make([]Check, 0, len(bc.Checks))
		for _, c := range bc.Checks {
			checks = append(checks, Check{Section: c.Section, Name: c.Name, Status: string(c.Status), Detail: c.Detail})
		}
		out.Checks = append(out.Checks, BatchChecks{Batch: bc.Batch, Dropped: bc.Dropped, Findings: bc.Findings, Checks: checks})
	}
	for _, sk := range s.Skipped {
		out.Files.Skipped = append(out.Files.Skipped, Skipped{Path: sk.Path, Rule: sk.Rule})
	}
	for _, f := range s.Failures {
		text := failureText(f)
		fail := Failure{Files: nonNil(f.Files), Message: text}
		if f.Cause != nil {
			fail.Cause = &Cause{Kind: string(f.Cause.Kind), Message: plainText(f.Cause.Message())}
		}
		out.Failures = append(out.Failures, fail)
		for _, path := range f.Files {
			out.Files.NotReviewed = append(out.Files.NotReviewed, NotReviewed{Path: path, Cause: text})
		}
	}
	return out
}

// failureText is a failure as one plain line for machines: the failure's own
// message and the cause's message, joined by a space. It has no markdown and
// no stderr.
func failureText(f review.BatchFailure) string {
	return redact.String(plainText(plainFailure(f)))
}

// markdownMarks are the markdown the failure texts carry for the CI comment:
// code spans around paths and tool names, and bold.
var markdownMarks = strings.NewReplacer("`", "", "**", "")

// plainText is s on one line without code-span backticks or bold markers.
func plainText(s string) string {
	return oneLine(markdownMarks.Replace(s))
}

func plainFailure(f review.BatchFailure) string {
	lead := oneLine(f.Message)
	switch {
	case f.Cause != nil && lead != "":
		return lead + " " + oneLine(f.Cause.Message())
	case f.Cause != nil:
		return oneLine(f.Cause.Message())
	case lead != "":
		return lead
	}
	return "The run ended without a review and without a reported cause."
}

func buildFinding(f finding.Finding, ids map[string]string) Finding {
	return Finding{
		ID:           ids[f.Fingerprint],
		Fingerprint:  f.Fingerprint,
		File:         f.File,
		Line:         f.Line,
		Severity:     string(f.Severity),
		Title:        f.Title,
		Explanation:  f.Explanation,
		SuggestedFix: f.SuggestedFix,
	}
}

// ErrorResult is the result of a run that produced no review. Its comment
// is ready to post: the pergent marker line, then the message with markdown
// escaped, since it can name files from the checkout.
func ErrorResult(meta Meta, code ErrorCode, message string) Result {
	message = oneLine(redact.String(message))
	r := baseResult(meta, StatusError)
	// An error result has no timing or budget: nothing ran to completion.
	r.StartedAt, r.FinishedAt, r.BudgetSeconds = "", "", 0
	r.Error = &Error{Code: code, Message: message}
	r.Comment = Comment{Markdown: output.Marker + "\n⚠️ **pergent couldn't review this MR:** " + escapeMarkdown(message)}
	return r
}

// asciiPunctuation is CommonMark's ASCII punctuation: each of these can be
// backslash-escaped.
const asciiPunctuation = "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~"

// escapeMarkdown puts a backslash before every ASCII punctuation character,
// which CommonMark renders as the character itself, so s can't open a link,
// an HTML tag, an entity or any other markup.
func escapeMarkdown(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(asciiPunctuation, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// redactResult returns a copy of r with credentials removed from every
// string. The caller's slices are not changed.
func redactResult(r Result) Result {
	r.PergentVersion = redact.String(r.PergentVersion)
	r.Base = redact.String(r.Base)
	r.Head = redact.String(r.Head)
	r.Comment.Markdown = redact.String(r.Comment.Markdown)
	r.Comment.FindingsBlock = redact.String(r.Comment.FindingsBlock)
	if r.Error != nil {
		e := *r.Error
		e.Message = redact.String(e.Message)
		r.Error = &e
	}
	if r.Skills != nil {
		skills := make([]Skill, len(r.Skills))
		for i, s := range r.Skills {
			skills[i] = redactSkill(s)
		}
		r.Skills = skills
	}
	return r
}

func redactSkill(s Skill) Skill {
	s.Name = redact.String(s.Name)
	findings := make([]Finding, len(s.Findings))
	for i, f := range s.Findings {
		f.File = redact.String(f.File)
		f.Title = redact.String(f.Title)
		f.Explanation = redact.String(f.Explanation)
		f.SuggestedFix = redact.String(f.SuggestedFix)
		findings[i] = f
	}
	s.Findings = findings

	if s.Summaries != nil {
		sums := make([]Summary, len(s.Summaries))
		for i, x := range s.Summaries {
			sums[i] = Summary{Batch: x.Batch, Text: redact.String(x.Text)}
		}
		s.Summaries = sums
	}
	if s.Checks != nil {
		bcs := make([]BatchChecks, len(s.Checks))
		for i, bc := range s.Checks {
			checks := make([]Check, len(bc.Checks))
			for j, c := range bc.Checks {
				checks[j] = Check{
					Section: redact.String(c.Section), Name: redact.String(c.Name),
					Status: c.Status, Detail: redact.String(c.Detail),
				}
			}
			bc.Checks = checks
			bcs[i] = bc
		}
		s.Checks = bcs
	}

	s.Files.Reviewed = redactStrings(s.Files.Reviewed)
	skipped := make([]Skipped, len(s.Files.Skipped))
	for i, x := range s.Files.Skipped {
		skipped[i] = Skipped{Path: redact.String(x.Path), Rule: x.Rule}
	}
	s.Files.Skipped = skipped
	nr := make([]NotReviewed, len(s.Files.NotReviewed))
	for i, x := range s.Files.NotReviewed {
		nr[i] = NotReviewed{Path: redact.String(x.Path), Cause: redact.String(x.Cause)}
	}
	s.Files.NotReviewed = nr

	failures := make([]Failure, len(s.Failures))
	for i, f := range s.Failures {
		f.Files = redactStrings(f.Files)
		f.Message = redact.String(f.Message)
		if f.Cause != nil {
			c := Cause{Kind: f.Cause.Kind, Message: redact.String(f.Cause.Message)}
			f.Cause = &c
		}
		failures[i] = f
	}
	s.Failures = failures
	s.Notes = redactStrings(s.Notes)
	return s
}

func redactStrings(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = redact.String(s)
	}
	return out
}
