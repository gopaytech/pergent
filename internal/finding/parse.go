package finding

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/gopaytech/pergent/internal/redact"
)

// jsonOpenerRe matches the line that opens a ```json fenced block.
var jsonOpenerRe = regexp.MustCompile("```json[ \t]*\r?\n")

// JSONBlock is one ```json fenced block of a model's answer: the JSON value
// it holds, or why it could not be read.
type JSONBlock struct {
	Value json.RawMessage
	Err   error
}

// JSONBlocks returns every ```json fenced block of text, in order. Each
// block is read as one JSON value, so a ``` inside a JSON string (a fenced
// code example in a suggested fix) does not end it; only whitespace and then
// a closing ``` (or the end of the text) may follow the value.
func JSONBlocks(text string) []JSONBlock {
	var blocks []JSONBlock
	for _, loc := range jsonOpenerRe.FindAllStringIndex(text, -1) {
		dec := json.NewDecoder(strings.NewReader(text[loc[1]:]))
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			blocks = append(blocks, JSONBlock{Err: err})
			continue
		}
		rest := strings.TrimLeft(text[loc[1]+int(dec.InputOffset()):], " \t\r\n")
		if rest != "" && !strings.HasPrefix(rest, "```") {
			blocks = append(blocks, JSONBlock{Err: errors.New("unexpected text after the JSON value in the block")})
			continue
		}
		blocks = append(blocks, JSONBlock{Value: raw})
	}
	return blocks
}

// maxSummaryLen caps an answer's summary, in runes, so one verbose answer
// can't crowd out the rest of the comment.
const maxSummaryLen = 600

// Limits on an answer's checks, so one verbose answer can't crowd out the
// rest of the comment. Field limits are in runes.
const (
	maxChecks       = 60
	maxCheckName    = 120
	maxCheckDetail  = 300
	maxCheckSection = 80
)

// Limits on an answer's left_out list. The field limit is in runes.
const (
	maxLeftOut      = 100
	maxLeftOutField = 200
)

// maxConfidenceReason caps an answer's confidence reason, in runes.
const maxConfidenceReason = 300

// CheckStatus is the outcome of one checklist item.
type CheckStatus string

const (
	CheckPass CheckStatus = "pass"
	CheckFail CheckStatus = "fail"
	CheckNA   CheckStatus = "n/a"
)

// Check is one item of the skill's checklist, as the model reported it.
type Check struct {
	Section string // the checklist heading it belongs to; may be empty
	Name    string
	Status  CheckStatus
	Detail  string // the evidence; may be empty
}

// LeftOut is a finding the model left out because people rejected the same
// mistake in an earlier review: its own title and the title of the rejected
// finding it matched.
type LeftOut struct {
	Title    string `json:"title"`
	Rejected string `json:"rejected,omitempty"` // may be empty
}

// Answer is the model's parsed final answer.
type Answer struct {
	// Summary is what the model says it checked and concluded, on one line.
	// Empty when the answer has none; a missing summary is not an error.
	Summary string
	// Checks are the valid checks the model reported, at most maxChecks of
	// them; ChecksDropped counts the valid ones beyond that.
	Checks        []Check
	ChecksDropped int
	// Confidence is the model's confidence in the review, 1 (low) to 5
	// (high); 0 when it gave none or gave a value outside that range.
	Confidence int
	// ConfidenceReason is the one-line reason for it; empty when Confidence
	// is 0.
	ConfidenceReason string
	Findings         []Finding
	// LeftOut are the findings the model says it left out because of past
	// dismissals, at most maxLeftOut of them.
	LeftOut []LeftOut
}

type answer struct {
	// Summary, Checks and LeftOut are decoded loosely: an unexpected shape
	// must not cost the answer its findings.
	Summary          json.RawMessage `json:"summary"`
	Checks           json.RawMessage `json:"checks"`
	LeftOut          json.RawMessage `json:"left_out"`
	Confidence       json.RawMessage `json:"confidence"`
	ConfidenceReason json.RawMessage `json:"confidence_reason"`
	Findings         json.RawMessage `json:"findings"`
}

// rawFinding is a Finding whose end_line and suggestion are decoded loosely
// (the two fields shadow Finding's own), so a wrong type costs the finding
// that field, not the block.
type rawFinding struct {
	Finding
	EndLine    json.RawMessage `json:"end_line"`
	Suggestion json.RawMessage `json:"suggestion"`
}

type rawLeftOut struct {
	Title    json.RawMessage `json:"title"`
	Rejected json.RawMessage `json:"rejected"`
}

type rawCheck struct {
	Section json.RawMessage `json:"section"`
	Name    json.RawMessage `json:"name"`
	Status  json.RawMessage `json:"status"`
	Detail  json.RawMessage `json:"detail"`
}

// errNoFindingsKey marks a block that is not an answer at all.
var errNoFindingsKey = errors.New(`json block has no "findings" key`)

// ParseAnswer extracts the summary and findings from the model's final
// answer: the last ```json fenced block with a "findings" key. Only readable
// objects without that key are passed over. A bad or unreadable block is an
// error, not a reason to look further back: an earlier block may be an example
// of the output shape, and taking it would turn a real answer into a clean pass.
func ParseAnswer(text string) (Answer, error) {
	blocks := JSONBlocks(text)
	if len(blocks) == 0 {
		return Answer{}, errors.New("no json code block found in the answer")
	}

	for i := len(blocks) - 1; i >= 0; i-- {
		a, err := parseBlock(blocks[i])
		if !errors.Is(err, errNoFindingsKey) {
			return a, err
		}
	}
	return Answer{}, errNoFindingsKey
}

func parseBlock(block JSONBlock) (Answer, error) {
	if block.Err != nil {
		return Answer{}, fmt.Errorf("invalid json: %w", block.Err)
	}
	var a answer
	if err := json.Unmarshal(block.Value, &a); err != nil {
		return Answer{}, fmt.Errorf("invalid json: %w", err)
	}
	if a.Findings == nil {
		return Answer{}, errNoFindingsKey
	}
	var raw []rawFinding
	if err := json.Unmarshal(a.Findings, &raw); err != nil {
		return Answer{}, fmt.Errorf("invalid json: %w", err)
	}
	if raw == nil {
		return Answer{}, errors.New(`"findings" is null, want a list`)
	}
	findings := make([]Finding, 0, len(raw))
	for i, rf := range raw {
		if err := validate(rf.Finding); err != nil {
			return Answer{}, fmt.Errorf("finding %d: %w", i, err)
		}
		findings = append(findings, rf.finding())
	}
	checks, dropped := parseChecks(a.Checks)
	confidence := parseConfidence(a.Confidence)
	reason := ""
	if confidence > 0 {
		reason = cleanText(stringValue(a.ConfidenceReason), maxConfidenceReason)
	}
	return Answer{
		Summary:          cleanText(summaryText(a.Summary), maxSummaryLen),
		Checks:           checks,
		ChecksDropped:    dropped,
		Confidence:       confidence,
		ConfidenceReason: reason,
		Findings:         findings,
		LeftOut:          parseLeftOut(a.LeftOut),
	}, nil
}

// finding returns the Finding with its end line and suggestion read from the
// loose fields. The end line is kept only when it is an integer at or after
// the line. The suggestion is kept only when the finding has a line and the
// range it replaces is at most MaxSuggestionLines.
func (rf rawFinding) finding() Finding {
	f := rf.Finding
	f.EndLine, f.Suggestion = 0, ""
	if end, ok := intValue(rf.EndLine); ok && f.Line > 0 && end >= f.Line {
		f.EndLine = end
	}
	last := f.Line
	if f.EndLine > 0 {
		last = f.EndLine
	}
	if f.Line > 0 && last-f.Line+1 <= MaxSuggestionLines {
		// Only the trailing newline goes: indentation is part of the fix.
		s := strings.ReplaceAll(stringValue(rf.Suggestion), "\r\n", "\n")
		f.Suggestion = strings.TrimSuffix(s, "\n")
	}
	return f
}

// parseConfidence reads a confidence: a JSON number with an integer value
// from 1 to 5. Anything else counts as not scored (0).
func parseConfidence(raw json.RawMessage) int {
	n, ok := intValue(raw)
	if !ok || n < 1 || n > 5 {
		return 0
	}
	return n
}

// intValue reads raw as a JSON number with an integer value (4 and 4.0 both
// count). Strings, null, fractions and absurdly large numbers do not.
func intValue(raw json.RawMessage) (int, bool) {
	var f float64
	if len(raw) == 0 || raw[0] == 'n' || json.Unmarshal(raw, &f) != nil {
		return 0, false
	}
	if f != math.Trunc(f) || math.Abs(f) > math.MaxInt32 {
		return 0, false
	}
	return int(f), true
}

// parseChecks reads the checks list, dropping entries that aren't objects or
// lack a name or a known status, and keeping at most maxChecks of the rest.
// Anything but a JSON array counts as no checks.
func parseChecks(raw json.RawMessage) ([]Check, int) {
	var entries []json.RawMessage
	if json.Unmarshal(raw, &entries) != nil {
		return nil, 0
	}
	var checks []Check
	dropped := 0
	for _, e := range entries {
		var rc rawCheck
		if json.Unmarshal(e, &rc) != nil {
			continue
		}
		name := cleanText(stringValue(rc.Name), maxCheckName)
		status, ok := checkStatus(stringValue(rc.Status))
		if name == "" || !ok {
			continue
		}
		if len(checks) == maxChecks {
			dropped++
			continue
		}
		checks = append(checks, Check{
			Section: cleanText(stringValue(rc.Section), maxCheckSection),
			Name:    name,
			Status:  status,
			Detail:  cleanText(stringValue(rc.Detail), maxCheckDetail),
		})
	}
	return checks, dropped
}

// parseLeftOut reads the left_out list, dropping entries that aren't objects
// or lack a title, and keeping at most maxLeftOut of the rest. Anything but a
// JSON array counts as nothing left out; it is never an error.
func parseLeftOut(raw json.RawMessage) []LeftOut {
	var entries []json.RawMessage
	if json.Unmarshal(raw, &entries) != nil {
		return nil
	}
	var out []LeftOut
	for _, e := range entries {
		var rl rawLeftOut
		if json.Unmarshal(e, &rl) != nil {
			continue
		}
		title := cleanText(stringValue(rl.Title), maxLeftOutField)
		if title == "" {
			continue
		}
		out = append(out, LeftOut{Title: title, Rejected: cleanText(stringValue(rl.Rejected), maxLeftOutField)})
		if len(out) == maxLeftOut {
			break
		}
	}
	return out
}

// checkStatus reads a status, ignoring case and surrounding whitespace, and
// accepting the word forms a model is likely to use ("passed", "failed",
// "na", "not applicable"), so a check isn't silently lost to wording.
func checkStatus(s string) (CheckStatus, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "pass", "passed":
		return CheckPass, true
	case "fail", "failed":
		return CheckFail, true
	case "n/a", "na", "not applicable":
		return CheckNA, true
	}
	return "", false
}

// stringValue returns raw as a string when it is a JSON string, else "".
func stringValue(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// summaryText reads a summary given as a string or as a list of strings
// (joined with spaces). Anything else counts as no summary.
func summaryText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []string
	if json.Unmarshal(raw, &parts) == nil {
		return strings.Join(parts, " ")
	}
	return ""
}

// cleanText redacts model text, collapses it to one line and caps it at max
// runes, ending with "…" when it is cut. Redacting first keeps the cap from
// leaving part of a secret that redaction would no longer match.
func cleanText(s string, max int) string {
	s = strings.Join(strings.Fields(redact.String(s)), " ")
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

func validate(f Finding) error {
	if f.File == "" {
		return errors.New("file is required")
	}
	if f.Title == "" {
		return errors.New("title is required")
	}
	if f.Severity != Blocking && f.Severity != Suggestion {
		return fmt.Errorf("invalid severity %q (want %q or %q)", f.Severity, Blocking, Suggestion)
	}
	if f.Line < 0 {
		return fmt.Errorf("line must be 0 or more, got %d", f.Line)
	}
	return nil
}
