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

// Rejected is a finding people rejected in an earlier review, with their
// reason. It is attached to a run as data: a trusted input of the caller, not
// something the model produced.
type Rejected struct {
	File     string `json:"file"`
	Title    string `json:"title"`
	Severity string `json:"severity"`
	Reason   string `json:"reason"`
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

// Dedup keeps one finding for each (file, line, normalized title), preserving
// order: the first, unless a blocking one follows a suggestion, which then
// takes its place. A duplicate must never lower a finding's severity.
func Dedup(fs []Finding) []Finding {
	seen := make(map[string]int, len(fs))
	out := make([]Finding, 0, len(fs))
	for _, f := range fs {
		key := fmt.Sprintf("%s\x00%d\x00%s", f.File, f.Line, normalize(f.Title))
		if i, ok := seen[key]; ok {
			if f.Severity == Blocking && out[i].Severity != Blocking {
				out[i] = f
			}
			continue
		}
		seen[key] = len(out)
		out = append(out, f)
	}
	return out
}
