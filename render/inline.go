package render

import (
	"fmt"
	"regexp"
	"strings"
)

// InlineOptions changes how InlineComment renders.
type InlineOptions struct {
	// SuggestionInfo is the fence info string that makes the platform offer
	// to apply Finding.Suggestion: "suggestion" (GitHub) or
	// "suggestion:-0+N" (GitLab). Empty shows no suggestion block.
	SuggestionInfo string
	// DismissCommand is the command prefix, such as "@pergent-review dismiss";
	// empty means no dismiss line.
	DismissCommand string
	// Skill names the skill on the comment; empty leaves it out.
	Skill string
}

const (
	inlineMarkerPrefix = "<!-- pergent:finding "
	inlineMarkerSuffix = " -->"
)

// inlineMarkerRe matches a whole line that is a finding marker.
var inlineMarkerRe = regexp.MustCompile(`^<!-- pergent:finding ([0-9a-zA-Z:_-]{1,128}) -->$`)

// InlineMarker returns the hidden marker that ends an inline comment and
// identifies the finding it was posted for.
func InlineMarker(fingerprint string) string {
	return inlineMarkerPrefix + fingerprint + inlineMarkerSuffix
}

// InlineFingerprint reads the fingerprint from the marker on the last
// non-empty line of body. A marker anywhere else, or one with a fingerprint
// that is empty, longer than 128 characters or has characters outside
// [0-9a-zA-Z:_-], gives "", false. Model text can't forge one: InlineComment
// always ends with its own marker, and escapes "<!--" in the model's text.
func InlineFingerprint(body string) (string, bool) {
	body = strings.TrimRight(body, " \t\r\n")
	line := strings.TrimSpace(body[strings.LastIndexByte(body, '\n')+1:])
	m := inlineMarkerRe.FindStringSubmatch(line)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// inlineText prepares multi-line model text for an inline comment: comments
// and fences neutralised, mentions broken and quick actions defused.
func inlineText(s string) string {
	s = modelText(s)
	s = strings.ReplaceAll(s, "@", "@‍")
	return neutralizeQuickActions(s)
}

// InlineComment renders one finding as the body of an inline review thread.
// It ends with InlineMarker(f.Fingerprint) on its own line.
//
// Title, explanation and fix are made safe like the summary comment's. The
// suggestion is not: it is written byte for byte inside a fence longer than
// any backtick run in it, because escaping would change what the platform
// applies. A suggestion with a carriage return is left out, as the platform
// would apply it differently from what is shown; the fix stays in words.
func InlineComment(f Finding, o InlineOptions) string {
	var b strings.Builder
	label := "**❌ Blocking: "
	if f.Severity == Suggestion {
		label = "**💡 Suggestion: "
	}
	b.WriteString(label + mentionSafe(f.Title) + "**\n")
	if f.Explanation != "" {
		b.WriteString(inlineText(f.Explanation) + "\n")
	}
	b.WriteString("\n")

	if f.SuggestedFix != "" {
		fix := inlineText(f.SuggestedFix)
		sep := " "
		if strings.Contains(fix, "\n") {
			sep = "\n\n"
		}
		fmt.Fprintf(&b, "**Fix:**%s%s\n\n", sep, fix)
	}

	if o.SuggestionInfo != "" && f.Suggestion != "" && !strings.Contains(f.Suggestion, "\r") {
		fence := strings.Repeat("`", max(3, longestBacktickRun(f.Suggestion)+1))
		fmt.Fprintf(&b, "%s%s\n%s\n%s\n\n", fence, oneLine(o.SuggestionInfo), f.Suggestion, fence)
	}

	var parts []string
	if o.Skill != "" {
		parts = append(parts, neutralizeComments(codeSpan(o.Skill)))
	}
	if f.ID != "" {
		parts = append(parts, "id "+neutralizeComments(codeSpan(f.ID)))
	}
	if o.DismissCommand != "" {
		parts = append(parts, "Not a real problem? Reply "+
			neutralizeComments(codeSpan(o.DismissCommand+" <reason>"))+" in this thread.")
	}
	if len(parts) > 0 {
		b.WriteString("<sub>" + strings.Join(parts, " · ") + "</sub>\n")
	}
	b.WriteString(InlineMarker(f.Fingerprint))
	return b.String()
}
