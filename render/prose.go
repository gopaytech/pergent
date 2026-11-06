package render

import "strings"

// Model text reaches the comment through two entry points, and both follow one
// rule: pergent does not guess how a platform reads the text, it controls it.
// Every character that could do harm is broken, so no live "[", "<", "@", "|",
// stray backtick or entity start is left outside the few things pergent keeps:
//
//   - A fence. The model's fenced code is written again inside a fence pergent
//     builds itself (see blockText), so its content is code whatever the
//     model's fence was. This is a container of pergent's.
//   - A code span, on a narrow rule (see lineSpans). A span is the model's own
//     bytes, not a container of pergent's, so a platform that reads the line
//     differently could make its content live. The rule only keeps a span where
//     nothing before or after it can reach its backticks: it must start at the
//     line's start or after a space, a tab or "(" and end at the line's end or
//     before a space, a tab or one of ").,:;!?". That keeps an autolinked URL,
//     or math around the backticks, from taking one, and the spans of one line
//     from pairing with a backtick of another. A table cell keeps no span.
//
// A wrong guess about a fence changes how text looks (text shown as code, code
// as text). For a span the rule is the only protection, and it is narrower
// than what Markdown allows on purpose.
//
//   - lineText is for one line: titles, reasons, notes and the like.
//   - cellText is for a table cell.
//   - blockText is for explanations, fixes and a failure's lead, which may
//     carry fenced code and several lines; every other line goes through the
//     same rules as a line does.
//
// A joiner (the zero-width joiner U+200D) is the break, not an entity or a
// backslash, because those show up literally inside code.
const joiner = "\u200d"

// redactedToken is what redaction leaves in place of a credential; it is the
// one "[" left alone, unless it could start a link or a definition.
const redactedToken = "[REDACTED]"

// lineText renders model text on one line. Code spans are kept as written
// (see lineSpans); the rest is broken so it can't ping anyone or render an
// image, link, footnote, wiki link or HTML.
func lineText(s string) string { return inertLine(oneLine(s), modeLine) }

// wrappedText is lineText for text that pergent writes between its own
// characters ("**title**", "_note_"): a span can't be at an edge of the line,
// as the character beside it would be next to a backtick.
func wrappedText(s string) string { return inertLine(oneLine(s), modeWrapped) }

// lineMode says what is around a line of text.
type lineMode int

const (
	modeLine    lineMode = iota // alone on its line, or after a space
	modeWrapped                 // between pergent's characters
	modeCell                    // in a table cell
)

// cellText is lineText for a table cell. A cell is split at a "|" before spans
// are read, so it keeps no span, and a "|" is the entity "&#124;", which a
// platform reads after splitting; a backslash before the pipe gets one more,
// so the entity isn't escaped.
func cellText(s string) string { return inertLine(oneLine(s), modeCell) }

// inertLine makes one line of text inert.
//
//   - A run of three or more tildes gets a joiner after each, so it can't open
//     a fence behind a quote or list marker.
//   - "@", "<" and "|" get a joiner after them, "[" one before it (so no line
//     starts with "[", where a definition would), "]" one after it, and "&"
//     one after it where it starts an entity ("&#" or "&name;"). Bare URLs,
//     "*", "_", "#", "-", ">", "~" and "!" are left as they are; a URL that
//     holds a broken character loses its link.
//   - A code span (see lineSpans) is kept as written.
//   - Every other backtick is stray and gets a backslash, so it is a literal
//     backtick that can't open or close a span. A joiner would not do: it
//     leaves the backtick a backtick, which could pair with a span's.
func inertLine(line string, mode lineMode) string {
	cell := mode == modeCell
	var spans [][2]int
	if !cell {
		spans = lineSpans(line, mode == modeWrapped)
	}
	var b strings.Builder
	for i := 0; i < len(line); {
		c := line[i]
		switch {
		case len(spans) > 0 && spans[0][0] == i:
			b.WriteString(line[i:spans[0][1]])
			i = spans[0][1]
			spans = spans[1:]
			continue
		case c == '`':
			if !escapedAt(line, i) {
				b.WriteByte('\\')
			}
			b.WriteByte(c)
		case c == '@' || c == '<':
			b.WriteByte(c)
			b.WriteString(joiner)
		case c == '~' && tildeRun(line[i:]) >= 3:
			for n := tildeRun(line[i:]); n > 0; n-- {
				b.WriteString("~" + joiner)
				i++
			}
			continue
		case c == '[':
			if strings.HasPrefix(line[i:], redactedToken) && (i == 0 || line[i-1] != '[') && !startsLink(line[i+len(redactedToken):]) {
				b.WriteString(redactedToken)
				i += len(redactedToken)
				continue
			}
			if !escapedAt(line, i) {
				b.WriteString(joiner)
			}
			b.WriteByte(c)
		case c == ']':
			b.WriteByte(c)
			b.WriteString(joiner)
		case c == '|':
			switch {
			case !cell:
				b.WriteString("|" + joiner)
			case escapedAt(line, i):
				b.WriteString(`\&#124;`)
			default:
				b.WriteString("&#124;")
			}
		case c == '&' && entityStart(line[i+1:]):
			b.WriteByte(c)
			b.WriteString(joiner)
		default:
			b.WriteByte(c)
		}
		i++
	}
	return b.String()
}

// tildeRun returns the length of the run of tildes at the start of s.
func tildeRun(s string) int {
	n := 0
	for n < len(s) && s[n] == '~' {
		n++
	}
	return n
}

// startsLink reports whether s, after a "[REDACTED]", could make it the start
// of a link or a definition.
func startsLink(s string) bool {
	return strings.HasPrefix(s, "(") || strings.HasPrefix(s, "[") || strings.HasPrefix(s, ":")
}

// entityStart reports whether s, after an "&", starts a character reference:
// "#", or a name of letters and digits and a ";".
func entityStart(s string) bool {
	if strings.HasPrefix(s, "#") {
		return true
	}
	n := 0
	for n < len(s) && (s[n] >= 'a' && s[n] <= 'z' || s[n] >= 'A' && s[n] <= 'Z' || s[n] >= '0' && s[n] <= '9') {
		n++
	}
	return n > 0 && n < len(s) && s[n] == ';'
}

// escapedAt reports whether the byte at i is escaped: an odd number of
// backslashes come right before it.
func escapedAt(s string, i int) bool {
	n := 0
	for i-n-1 >= 0 && s[i-n-1] == '\\' {
		n++
	}
	return n%2 == 1
}

// lineSpans returns the code spans of a line as byte ranges, or none if any
// backtick of the line is not part of one. A span is one backtick, one or
// more characters that are not backticks and don't start or end with a
// space, and one backtick. Its first backtick is at the start of the line or
// right after a space, a tab or "(", and its last at the end of the line or
// right before a space, a tab or one of ").,:;!?"; for text between pergent's
// characters (wrapped) the start and the end of the line don't count. A line
// with an odd number of backticks has no span.
func lineSpans(line string, wrapped bool) [][2]int {
	if strings.Count(line, "`")%2 == 1 {
		return nil
	}
	var spans [][2]int
	for i := 0; i < len(line); {
		if line[i] != '`' {
			i++
			continue
		}
		end := spanEnd(line, i, wrapped)
		if end == 0 {
			return nil
		}
		spans = append(spans, [2]int{i, end})
		i = end
	}
	return spans
}

// spanEnd returns the end of the code span that starts at the backtick at
// line[i], or 0 if there is none.
func spanEnd(line string, i int, wrapped bool) int {
	if i > 0 && !strings.ContainsRune(" \t(", rune(line[i-1])) || i == 0 && wrapped {
		return 0
	}
	j := strings.IndexByte(line[i+1:], '`')
	if j <= 0 {
		return 0
	}
	j += i + 1
	if j+1 < len(line) && !strings.ContainsRune(" \t).,:;!?", rune(line[j+1])) || j+1 == len(line) && wrapped {
		return 0
	}
	if content := line[i+1 : j]; content[0] == ' ' || content[len(content)-1] == ' ' {
		return 0
	}
	return j + 1
}

// blockText renders multi-line model text. "\r" is removed first:
// neutralizeQuickActions removes it from the whole comment later, which would
// rebuild a marker or a fence that the steps here had broken up.
//
// A fence is a line of 0 to 3 spaces and then 3 or more backticks or tildes
// and an info string of at most one word (a backtick fence's may have no
// backtick; "~~~ not a fence in prose" is text). It runs to a line
// that, without its spaces and tabs at the ends, is the same character, at
// least as many times, and nothing else, or to the end of the text. Its
// content is written again, byte for byte except that the opener's indent is
// removed, inside a fence pergent writes: backticks, more than any run in the
// content (3 at least), at column 0, with no info string and with a blank line
// before and after. No info string means a fence can't be a suggestion, a
// diagram or anything else a platform renders for a language. A closed fence
// is not a guess about the platform: if it reads the lines differently, the
// text shows as code or code as text.
//
// Every other line is text. A line that starts with ">>>" gets a joiner, as
// GitLab reads a line of ">>>" as a quote that ends at the next one, fences
// or not.
func blockText(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r", ""), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return ""
	}
	// The indent of a fence on the first line is measured; any other first
	// line starts at column 0, and the text ends where its last line does.
	if marker, _, _ := fenceOpener(lines[0]); marker == 0 {
		lines[0] = strings.TrimLeft(lines[0], " \t")
	}
	lines[len(lines)-1] = strings.TrimRight(lines[len(lines)-1], " \t")
	var out []string
	gap := func() { // a blank line, unless there is one or this is the start
		if n := len(out); n > 0 && !isBlank(out[n-1]) {
			out = append(out, "")
		}
	}
	for i := 0; i < len(lines); {
		marker, n, indent := fenceOpener(lines[i])
		if marker == 0 {
			line := inertLine(lines[i], modeLine)
			if t := strings.TrimLeft(line, " "); strings.HasPrefix(t, ">>>") {
				line = line[:len(line)-len(t)+1] + joiner + t[1:]
			}
			out = append(out, line)
			i++
			continue
		}
		end := i + 1
		for end < len(lines) && !fenceCloses(lines[end], marker, n) {
			end++
		}
		content := make([]string, 0, end-i-1)
		longest := 0
		for _, l := range lines[i+1 : end] {
			l = stripIndent(l, indent)
			content = append(content, l)
			longest = max(longest, longestBacktickRun(l))
		}
		fence := strings.Repeat("`", max(3, longest+1))
		gap()
		out = append(out, fence)
		out = append(out, content...)
		out = append(out, fence)
		i = end + 1
		if i < len(lines) && !isBlank(lines[i]) {
			out = append(out, "")
		}
	}
	return strings.Join(out, "\n")
}

// stripIndent removes up to n leading spaces from line.
func stripIndent(line string, n int) string {
	i := 0
	for i < n && i < len(line) && line[i] == ' ' {
		i++
	}
	return line[i:]
}

func isBlank(line string) bool { return strings.Trim(line, " \t") == "" }

// fenceOpener parses a line as the start of a fence: its marker character
// (0 if it is not one), the length of the marker run and the indent.
func fenceOpener(line string) (marker byte, n, indent int) {
	trimmed := strings.TrimLeft(line, " ")
	indent = len(line) - len(trimmed)
	if indent > 3 || trimmed == "" || (trimmed[0] != '`' && trimmed[0] != '~') {
		return 0, 0, 0
	}
	marker = trimmed[0]
	for n < len(trimmed) && trimmed[n] == marker {
		n++
	}
	info := trimmed[n:]
	if n < 3 || (marker == '`' && strings.Contains(info, "`")) || strings.ContainsAny(strings.TrimSpace(info), " \t") {
		return 0, 0, 0
	}
	return marker, n, indent
}

// fenceCloses reports whether line closes a fence of n markers: only the
// marker, at least n times, and spaces and tabs around it.
func fenceCloses(line string, marker byte, n int) bool {
	t := strings.Trim(line, " \t")
	return len(t) >= n && strings.Trim(t, string(marker)) == ""
}
