package finding

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

var jsonBlockRe = regexp.MustCompile("(?s)```json[ \t]*\r?\n(.*?)```")

type answer struct {
	Findings *[]Finding `json:"findings"`
}

// ParseAnswer extracts the findings from the model's final answer. It looks
// at every ```json fenced block, from last to first, and returns the findings
// of the first block that parses and validates.
func ParseAnswer(text string) ([]Finding, error) {
	matches := jsonBlockRe.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil, errors.New("no json code block found in the answer")
	}

	var lastErr error
	for i := len(matches) - 1; i >= 0; i-- {
		fs, err := parseBlock(matches[i][1])
		if err == nil {
			return fs, nil
		}
		if lastErr == nil {
			lastErr = err
		}
	}
	return nil, lastErr
}

func parseBlock(block string) ([]Finding, error) {
	var a answer
	if err := json.Unmarshal([]byte(block), &a); err != nil {
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
