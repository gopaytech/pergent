package finding

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
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

// Answer is the model's parsed final answer.
type Answer struct {
	// Summary is what the model says it checked and concluded, on one line.
	// Empty when the answer has none; a missing summary is not an error.
	Summary  string
	Findings []Finding
}

type answer struct {
	Summary  string     `json:"summary"`
	Findings *[]Finding `json:"findings"`
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
	return Answer{Summary: cleanSummary(a.Summary), Findings: *a.Findings}, nil
}

// cleanSummary collapses a summary to one line and caps it at maxSummaryLen
// runes, ending with "…" when it is cut.
func cleanSummary(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= maxSummaryLen {
		return s
	}
	return string(r[:maxSummaryLen-1]) + "…"
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
