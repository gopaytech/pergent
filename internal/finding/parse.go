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

type answer struct {
	Findings *[]Finding `json:"findings"`
}

// ParseAnswer extracts the findings from the model's final answer. It looks
// at every ```json fenced block, from last to first, and returns the findings
// of the first block that parses and validates.
func ParseAnswer(text string) ([]Finding, error) {
	blocks := JSONBlocks(text)
	if len(blocks) == 0 {
		return nil, errors.New("no json code block found in the answer")
	}

	var lastErr error
	for i := len(blocks) - 1; i >= 0; i-- {
		fs, err := parseBlock(blocks[i])
		if err == nil {
			return fs, nil
		}
		if lastErr == nil {
			lastErr = err
		}
	}
	return nil, lastErr
}

func parseBlock(block JSONBlock) ([]Finding, error) {
	if block.Err != nil {
		return nil, fmt.Errorf("invalid json: %w", block.Err)
	}
	var a answer
	if err := json.Unmarshal(block.Value, &a); err != nil {
		return nil, fmt.Errorf("invalid json: %w", err)
	}
	if a.Findings == nil {
		return nil, errors.New(`json block has no "findings" key`)
	}
	for i, f := range *a.Findings {
		if err := validate(f); err != nil {
			return nil, fmt.Errorf("finding %d: %w", i, err)
		}
	}
	return *a.Findings, nil
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
