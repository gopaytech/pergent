package render

import (
	"fmt"
	"html"
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
// identifies the finding it was posted for. The fingerprint must match
// [0-9a-zA-Z:_-]{1,128}, or InlineFingerprint won't recognise the marker.
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

// fenceRunRe matches a run of 3 or more backticks or tildes.
var fenceRunRe = regexp.MustCompile("`{3,}|~{3,}")

// defuseSuggestionFences keeps a fence in model text from being a suggestion.
// The platform offers to apply a fence whose rendered info string starts with
// "suggestion", and in an inline thread only the one InlineComment builds from
// Finding.Suggestion may be one. For every run of 3 or more backticks or
// tildes, anywhere on a line (a fence inside a blockquote, a list item or an
// indented block counts too), it decodes the rest of the line the way the
// renderers do (entity and numeric references, leading ASCII whitespace
// dropped, case ignored); if that starts with "suggestion", it puts "text-"
// right after the run, so the rendered info string starts with "text-" however
// it was spelled: "```&#115;uggestion" becomes "```text-&#115;uggestion". It
// also changes inline prose that happens to contain such a run, which is fine.
func defuseSuggestionFences(s string) string {
	var b strings.Builder
	last := 0
	for _, m := range fenceRunRe.FindAllStringIndex(s, -1) {
		rest := s[m[1]:]
		if i := strings.IndexByte(rest, '\n'); i >= 0 {
			rest = rest[:i]
		}
		rest = strings.ToLower(strings.TrimLeft(html.UnescapeString(rest), " \t\n\v\f\r"))
		if strings.HasPrefix(rest, "suggestion") {
			b.WriteString(s[last:m[1]] + "text-")
			last = m[1]
		}
	}
	b.WriteString(s[last:])
	return b.String()
}

// inlineText prepares multi-line model text for an inline comment: comments
// and fences neutralised, suggestion fences renamed, mentions broken and
// quick actions defused.
func inlineText(s string) string {
	s = defuseSuggestionFences(modelText(s))
	s = strings.ReplaceAll(s, "@", "@‍")
	return neutralizeQuickActions(s)
}

// InlineComment renders one finding as the body of an inline review thread.
// It ends with InlineMarker(f.Fingerprint) on its own line; the fingerprint
// must match [0-9a-zA-Z:_-]{1,128}, or InlineFingerprint won't recognise it.
//
// Title, explanation and fix are made safe like the summary comment's. The
// suggestion is not: it is written byte for byte inside a fence longer than
// any backtick run in it, because escaping would change what the platform
// applies. A suggestion with a carriage return is left out, as the platform
// would apply it differently from what is shown. It is also left out when any
// of its lines starts with "/" in column 0: GitLab runs such a line as a quick
// action unless it finds it inside a code block, and its code block detection
// (a regular expression, not a Markdown parser) can be fooled by fences, HTML
// and quote regions in the suggestion. The suggestion can't be escaped, so it
// is shown only as advice in words, in the fix. Suggestion fences in the
// explanation and fix are renamed (see defuseSuggestionFences).
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

	if o.SuggestionInfo != "" && f.Suggestion != "" && safeSuggestion(f.Suggestion) {
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

// safeSuggestion reports whether s can be shown byte for byte in a suggestion
// fence: it has no "\r" and no line starting with "/" in column 0.
func safeSuggestion(s string) bool {
	if strings.Contains(s, "\r") {
		return false
	}
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, "/") {
			return false
		}
	}
	return true
}
