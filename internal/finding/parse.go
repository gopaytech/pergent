package finding

import (
	"encoding/json"
	"errors"
	"fmt"
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

// Answer is the model's parsed final answer.
type Answer struct {
	// Summary is what the model says it checked and concluded, on one line.
	// Empty when the answer has none; a missing summary is not an error.
	Summary string
	// Checks are the valid checks the model reported, at most maxChecks of
	// them; ChecksDropped counts the valid ones beyond that.
	Checks        []Check
	ChecksDropped int
	Findings      []Finding
}

type answer struct {
	// Summary and Checks are decoded loosely: an unexpected shape must not
	// cost the answer its findings.
	Summary  json.RawMessage `json:"summary"`
	Checks   json.RawMessage `json:"checks"`
	Findings *[]Finding      `json:"findings"`
}

type rawCheck struct {
	Section json.RawMessage `json:"section"`
	Name    json.RawMessage `json:"name"`
	Status  json.RawMessage `json:"status"`
	Detail  json.RawMessage `json:"detail"`
}

// ParseAnswer extracts the summary and findings from the model's final
// answer. It looks at every ```json fenced block, from last to first, and
// returns the first block that parses and validates.
func ParseAnswer(text string) (Answer, error) {
	blocks := JSONBlocks(text)
	if len(blocks) == 0 {
		return Answer{}, errors.New("no json code block found in the answer")
	}

	var lastErr error
	for i := len(blocks) - 1; i >= 0; i-- {
		a, err := parseBlock(blocks[i])
		if err == nil {
			return a, nil
		}
		if lastErr == nil {
			lastErr = err
		}
	}
	return Answer{}, lastErr
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
		return Answer{}, errors.New(`json block has no "findings" key`)
	}
	for i, f := range *a.Findings {
		if err := validate(f); err != nil {
			return Answer{}, fmt.Errorf("finding %d: %w", i, err)
		}
	}
	checks, dropped := parseChecks(a.Checks)
	return Answer{
		Summary:       cleanText(summaryText(a.Summary), maxSummaryLen),
		Checks:        checks,
		ChecksDropped: dropped,
		Findings:      *a.Findings,
	}, nil
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

// checkStatus reads a status, ignoring case and surrounding whitespace, and
// accepting "na" for "n/a".
func checkStatus(s string) (CheckStatus, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "pass":
		return CheckPass, true
	case "fail":
		return CheckFail, true
	case "n/a", "na":
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
