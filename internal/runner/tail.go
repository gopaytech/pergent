package runner

import (
	"regexp"
	"strings"
)

// tailBuffer keeps the last max bytes written to it, for reporting the end of
// opencode's stderr when it fails.
type tailBuffer struct {
	max int
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.max; over > 0 {
		t.buf = t.buf[over:]
	}
	return len(p), nil
}

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

// lastLines returns up to n trailing non-empty lines, without terminal colors.
func (t *tailBuffer) lastLines(n int) []string {
	text := ansiEscape.ReplaceAllString(string(t.buf), "")
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r \t")
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}
