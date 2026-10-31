package output

import "strings"

// Texts the tests look for in a shrunk comment; render owns the real ones.
const (
	hiddenDroppedNote  = "> Findings were too large to store for the next run; --previous-review will treat the next run as a first review."
	detailsDroppedNote = "> Suggestion details omitted to fit the comment size limit."
)

// fenceLine parses a line as a code fence: its marker character, the length
// of the marker run and whether anything but whitespace follows it. The tests
// use it to check that no fence is left open.
func fenceLine(line string) (ch byte, n int, bare bool) {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 || len(trimmed) < 3 || (trimmed[0] != '`' && trimmed[0] != '~') {
		return 0, 0, false
	}
	ch = trimmed[0]
	for n < len(trimmed) && trimmed[n] == ch {
		n++
	}
	if n < 3 {
		return 0, 0, false
	}
	rest := trimmed[n:]
	if ch == '`' && strings.Contains(rest, "`") {
		return 0, 0, false
	}
	return ch, n, strings.TrimSpace(rest) == ""
}
