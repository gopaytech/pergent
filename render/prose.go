package render

import (
	"regexp"
	"strings"
)

// proseText makes multi-line model text inert like noteText does, but leaves
// code as it is: fenced blocks and code spans stay byte for byte, so `@Override`
// and `Map<String>` read as written, and so do lists, quotes and bare URLs.
//
// It errs towards treating text as live. The one dangerous mistake is to take
// text for code that Markdown reads as text; the other way costs a joiner no
// one sees. So:
//   - A fence opens only with its marker at column 0 (see fenceOpen), but
//     closes as liberally as Markdown allows (fenceLine). An indented line
//     inside a list item can close a fence there, so it is never an opener.
//   - Code spans count only in a block of consecutive non-blank lines in which
//     every line is balanced (see codeSpans). An unclosed backtick shifts how
//     the others pair up, and the pairing differs between Markdown readers.
//
// A block ends only at a line of spaces and tabs, which is all Markdown calls
// blank: any other space continues the paragraph, and the spans pair across it.
//
// An unclosed fence runs to the end; closeFences closes it.
func proseText(s string) string {
	lines := strings.Split(s, "\n")
	var fence byte // the open fence's marker; 0 outside one
	var fenceLen int
	start := 0 // first line of the block being collected
	for i, line := range lines {
		switch {
		case fence != 0:
			if ch, n, bare := fenceLine(line); ch == fence && n >= fenceLen && bare {
				fence, start = 0, i+1
			}
		case strings.Trim(line, " \t") == "":
			proseBlock(lines[start:i])
			start = i + 1
		default:
			if ch, n := fenceOpen(line); ch != 0 {
				proseBlock(lines[start:i])
				fence, fenceLen, start = ch, n, i+1
			}
		}
	}
	if fence == 0 {
		proseBlock(lines[start:])
	}
	return strings.Join(lines, "\n")
}

// fenceOpen parses a line as the start of a code fence, which needs its marker
// at column 0.
func fenceOpen(line string) (ch byte, n int) {
	ch, n, _ = fenceLine(line)
	if ch == 0 || line[0] != ch {
		return 0, 0
	}
	return ch, n
}

// proseReplacer is noteTextReplacer without "&": prose breaks it only where it
// starts an entity (see entityStartRe), so a URL with a query keeps its "&".
var proseReplacer = strings.NewReplacer(concat(mentionPairs, anglePairs, linkPairs)...)

// entityStartRe matches the start of a character reference: "&#", or "&" and a
// name up to ";".
var entityStartRe = regexp.MustCompile(`&(#|[A-Za-z0-9]+;)`)

// proseInert is noteText for text that keeps its line breaks and its "&" that
// start no entity.
func proseInert(s string) string {
	s = entityStartRe.ReplaceAllString(s, "&\u200d${1}")
	return breakWikiLinks(proseReplacer.Replace(s))
}

// proseBlock makes the lines of one block of text inert, in place.
func proseBlock(lines []string) {
	spans := make([][][2]int, len(lines))
	for i, line := range lines {
		var ok bool
		if spans[i], ok = codeSpans(line); !ok {
			spans = nil
			break
		}
	}
	for i, line := range lines {
		if spans == nil {
			lines[i] = proseInert(line)
			continue
		}
		var b strings.Builder
		last := 0
		for _, sp := range spans[i] {
			b.WriteString(proseInert(line[last:sp[0]]) + line[sp[0]:sp[1]])
			last = sp[1]
		}
		lines[i] = b.String() + proseInert(line[last:])
	}
}

// codeSpans returns the code spans of one line as byte ranges, scanning left
// to right: a run of n backticks opens a span that the next run of exactly n
// closes. It reports false if some run has no closer on the line, or if a
// backtick follows a backslash, since either lets Markdown read the line
// another way than the scan.
func codeSpans(line string) (spans [][2]int, ok bool) {
	if strings.Contains(line, "\\`") {
		return nil, false
	}
	for i := 0; i < len(line); {
		if line[i] != '`' {
			i++
			continue
		}
		n := backtickRun(line[i:])
		end := -1
		for j := i + n; j < len(line); {
			if line[j] != '`' {
				j++
				continue
			}
			m := backtickRun(line[j:])
			if m == n {
				end = j + m
				break
			}
			j += m
		}
		if end < 0 {
			return nil, false
		}
		spans = append(spans, [2]int{i, end})
		i = end
	}
	return spans, true
}

// backtickRun returns the length of the run of backticks at the start of s.
func backtickRun(s string) int {
	n := 0
	for n < len(s) && s[n] == '`' {
		n++
	}
	return n
}
