package output

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/gopaytech/pergent/internal/diff"
	"github.com/gopaytech/pergent/internal/finding"
	"github.com/gopaytech/pergent/internal/redact"
	"github.com/gopaytech/pergent/internal/review"
	"github.com/gopaytech/pergent/internal/runner"
	"github.com/gopaytech/pergent/render"
)

// Marker is the first line of every pergent comment; it finds the comment
// to update.
const Marker = render.Marker

// failedPrefix starts the report of a failed batch.
const failedPrefix = "**Review failed:**"

// writeFailureLead writes a failure: the prefix, then lead (context to show
// before the cause), then the cause and the tail of opencode's stderr. The
// first line is collapsed to one line. In markdown mode the credentials are
// removed; the text is not escaped, because render escapes a failure's lead
// itself.
func writeFailureLead(b *strings.Builder, lead string, cause *runner.Cause, markdown bool) {
	lead = oneLine(lead)
	var line string
	switch {
	case cause != nil && lead != "":
		line = lead + " " + oneLine(cause.Message())
	case cause != nil:
		line = oneLine(cause.Message())
	case lead != "":
		line = lead
	default:
		line = "The run ended without a review and without a reported cause."
	}
	if markdown {
		line = redact.String(line)
	}
	fmt.Fprintf(b, "%s %s\n", failedPrefix, line)
	if cause != nil && len(cause.StderrLines) > 0 {
		text := strings.Join(cause.StderrLines, "\n")
		fence := "```"
		for strings.Contains(text, fence) {
			fence += "`"
		}
		fmt.Fprintf(b, "\nLast lines of opencode's stderr:\n\n%stext\n%s\n%s\n", fence, text, fence)
	}
}

// oneLine collapses all whitespace, newlines included, to single spaces.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// BatchFailureText renders one failed batch as redacted text for the CI log:
// the failure line, then the end of opencode's stderr when there is one.
func BatchFailureText(f review.BatchFailure) string {
	var b strings.Builder
	writeFailureLead(&b, f.Message, f.Cause, false)
	return redact.String(strings.TrimSpace(b.String()))
}

// findingsMarker starts the hidden line that carries a review's findings to
// the next run. render writes the line; this is the same text, for reading
// it back.
const findingsMarker = "<!-- pergent:findings "

// findingsLineRe matches the hidden line. The real one is the last line of
// the comment, so ParsePreviousFindings uses the last match: text quoted from
// the reviewed code can't replace it.
var findingsLineRe = regexp.MustCompile(`(?m)^<!-- pergent:findings (\S+) -->[ \t\r]*$`)

// findingsBlockVersion is the format version of the hidden findings block.
const findingsBlockVersion = 1

type findingsBlock struct {
	Version int                          `json:"version"`
	Skills  map[string][]finding.Finding `json:"skills"`
}

// Options changes how a review comment is rendered. The zero value is the CI
// comment.
type Options struct {
	// IDs maps a finding's fingerprint to its short ID. A finding with an ID
	// shows it at the end of its row's summary line as "id <code>…</code>".
	IDs map[string]string
	// Commit is the reviewed commit; the comment shows its first 7 characters.
	Commit string
	// FileURL returns the web link for a finding's lines (render.GitHubFileURL,
	// render.GitLabFileURL); nil leaves locations as plain text. render builds
	// the link from the redacted path.
	FileURL func(path string, line, endLine int) string
	// Version is shown in the footer, e.g. "v0.3.0".
	Version string
}

// FormatReview renders the results as one markdown comment: a headline with
// the verdict, a meta line, one list of findings across all skills (blocking
// ones first and open, suggestions collapsed), the checks, the notes and the
// list of files. A hidden last line carries the findings for the next run.
// Credentials are redacted from the whole comment and from the hidden block.
func FormatReview(results []review.SkillResult, changedFiles []string) string {
	return FormatReviewWithinOptions(results, changedFiles, 0, Options{})
}

// FormatReviewWithin is FormatReview for a platform that limits a comment to
// maxLen bytes (no limit when maxLen is not positive). A comment over the
// limit is shrunk in steps until it fits; see render.Comment.
func FormatReviewWithin(results []review.SkillResult, changedFiles []string, maxLen int) string {
	return FormatReviewWithinOptions(results, changedFiles, maxLen, Options{})
}

// FormatReviewWithinOptions is FormatReviewWithin with rendering options; the
// IDs are shown in every layout, so they survive each shrink step (the
// findings list, the headline and the failures are never dropped).
func FormatReviewWithinOptions(results []review.SkillResult, changedFiles []string, maxLen int, o Options) string {
	files := make([]string, len(changedFiles))
	for i, f := range changedFiles {
		files[i] = redact.String(f)
	}
	return render.Comment(toRender(results, o.IDs), render.Options{MaxLen: maxLen, ChangedFiles: files, Commit: o.Commit, FileURL: o.FileURL, Version: o.Version})
}

// FindingsBlock returns the hidden line that carries the findings to the next
// run, including its newline: the same bytes the comment ends with. The
// findings are redacted. It is empty if the block cannot be encoded.
func FindingsBlock(results []review.SkillResult) string {
	return render.FindingsBlock(toRender(results, nil))
}

// toRender converts the results to what render takes. render redacts
// nothing, so every text field is redacted here; ids maps a finding's
// fingerprint to the short ID shown after its title.
func toRender(results []review.SkillResult, ids map[string]string) []render.Skill {
	skills := make([]render.Skill, len(results))
	for i, r := range results {
		s := render.Skill{
			Name:             redact.String(r.Skill),
			Verdict:          string(r.Verdict),
			Batches:          r.Batches,
			BatchesFinished:  r.BatchesFinished,
			Confidence:       r.Confidence,
			ConfidenceReason: redact.String(r.ConfidenceReason),
		}
		for _, f := range redactFindings(r.Findings) {
			s.Findings = append(s.Findings, render.Finding{
				ID: ids[f.Fingerprint], Fingerprint: f.Fingerprint, File: f.File, Line: f.Line, EndLine: f.EndLine,
				Severity: render.Severity(f.Severity), Title: f.Title, Explanation: f.Explanation, SuggestedFix: f.SuggestedFix,
				Suggestion: f.Suggestion,
			})
		}
		if s.Findings == nil {
			s.Findings = []render.Finding{}
		}
		for _, f := range r.Failures {
			s.Failures = append(s.Failures, failure(f))
		}
		for _, sk := range r.Skipped {
			s.Skipped = append(s.Skipped, skipped(sk))
		}
		for _, p := range r.Reviewed {
			s.Reviewed = append(s.Reviewed, redact.String(p))
		}
		for _, n := range r.Notes {
			s.Notes = append(s.Notes, redact.String(n))
		}
		for _, sum := range r.Summaries {
			s.Summaries = append(s.Summaries, render.Summary{Batch: sum.Batch, Text: redact.String(sum.Text)})
		}
		for _, bc := range r.Checks {
			rc := render.BatchChecks{Batch: bc.Batch, Dropped: bc.Dropped, Findings: bc.Findings}
			for _, c := range bc.Checks {
				rc.Checks = append(rc.Checks, render.Check{
					Section: redact.String(c.Section), Name: redact.String(c.Name),
					Status: string(c.Status), Detail: redact.String(c.Detail),
				})
			}
			s.Checks = append(s.Checks, rc)
		}
		skills[i] = s
	}
	return skills
}

func skipped(s diff.Skip) render.Skipped {
	return render.Skipped{Path: redact.String(s.Path), Rule: redact.String(s.Rule)}
}

// failure converts a failed batch. The lead is the whole failure text the CLI
// shows (the message, the cause and the tail of opencode's stderr), redacted.
func failure(f review.BatchFailure) render.Failure {
	files := make([]string, len(f.Files))
	for i, file := range f.Files {
		files[i] = redact.String(file)
	}
	var b strings.Builder
	writeFailureLead(&b, f.Message, f.Cause, true)
	lead := strings.TrimPrefix(redact.String(b.String()), failedPrefix+" ")
	return render.Failure{Files: files, Message: redact.String(f.Message), Lead: lead}
}

// redactFindings returns a copy of fs with credentials removed from every
// text field, so neither the comment nor the hidden block carries them.
func redactFindings(fs []finding.Finding) []finding.Finding {
	out := make([]finding.Finding, len(fs))
	for i, f := range fs {
		f.File = redact.String(f.File)
		f.Title = redact.String(f.Title)
		f.Explanation = redact.String(f.Explanation)
		f.SuggestedFix = redact.String(f.SuggestedFix)
		f.Suggestion = redact.String(f.Suggestion)
		f.Fingerprint = redact.String(f.Fingerprint)
		out[i] = f
	}
	return out
}

// ParsePreviousFindings reads the findings per skill from the hidden block of
// a previous pergent comment. It returns nil, nil when the comment has no
// block (for example, one posted by an older pergent), and an error when the
// block is there but cannot be read.
func ParsePreviousFindings(body string) (map[string][]finding.Finding, error) {
	matches := findingsLineRe.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil, nil
	}
	raw, err := base64.StdEncoding.DecodeString(matches[len(matches)-1][1])
	if err != nil {
		return nil, fmt.Errorf("decoding the findings block: %w", err)
	}
	var block findingsBlock
	if err := json.Unmarshal(raw, &block); err != nil {
		return nil, fmt.Errorf("parsing the findings block: %w", err)
	}
	if block.Version != findingsBlockVersion {
		return nil, fmt.Errorf("unsupported findings block version %d", block.Version)
	}
	return block.Skills, nil
}
