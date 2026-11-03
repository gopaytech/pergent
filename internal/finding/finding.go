// Package finding defines the structured review findings an agent returns
// and helpers to fingerprint and deduplicate them.
package finding

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// Severity classifies how serious a finding is.
type Severity string

const (
	Blocking   Severity = "blocking"
	Suggestion Severity = "suggestion"
)

// Finding is a single review comment about a file (and optionally a line).
type Finding struct {
	File         string   `json:"file"`
	Line         int      `json:"line,omitempty"`
	Severity     Severity `json:"severity"`
	Title        string   `json:"title"`
	Explanation  string   `json:"explanation"`
	SuggestedFix string   `json:"suggested_fix,omitempty"`
	// EndLine ends the range that starts at Line; 0 means a single line.
	EndLine int `json:"end_line,omitempty"`
	// Suggestion is the exact replacement for lines Line..EndLine (or Line),
	// at most MaxSuggestionLines of them; empty when there is none.
	Suggestion  string `json:"suggestion,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

// MaxSuggestionLines is the most lines a Suggestion may replace; a longer
// range keeps its finding but loses the suggestion.
const MaxSuggestionLines = 10

// normalize lowercases s, collapses whitespace runs to one space, and trims.
func normalize(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// Fingerprint returns a stable 12-character identifier for a finding,
// independent of case and spacing in the title.
func Fingerprint(skill, file, title string) string {
	sum := sha256.Sum256([]byte(skill + "\x00" + file + "\x00" + normalize(title)))
	return hex.EncodeToString(sum[:])[:12]
}

// Dedup keeps the first finding for each (file, line, normalized title),
// preserving order.
func Dedup(fs []Finding) []Finding {
	seen := make(map[string]struct{}, len(fs))
	out := make([]Finding, 0, len(fs))
	for _, f := range fs {
		key := fmt.Sprintf("%s\x00%d\x00%s", f.File, f.Line, normalize(f.Title))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, f)
	}
	return out
}
