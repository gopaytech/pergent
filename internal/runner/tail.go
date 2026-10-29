package runner

import (
	"bytes"
	"io"
	"regexp"
	"strings"

	"github.com/gopaytech/pergent/internal/redact"
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

// stderrWriter passes opencode's stderr through to w one line at a time,
// with credentials redacted and prefix (when set) before each line. A line
// split across writes is held until it ends; Flush writes what is left.
type stderrWriter struct {
	w      io.Writer
	prefix string
	// partial holds the start of a line not yet ended.
	partial []byte
}

func (s *stderrWriter) Write(p []byte) (int, error) {
	s.partial = append(s.partial, p...)
	for {
		i := bytes.IndexByte(s.partial, '\n')
		if i < 0 {
			break
		}
		s.writeLine(string(s.partial[:i]), "\n")
		s.partial = s.partial[i+1:]
	}
	return len(p), nil
}

// Flush writes a last line that had no newline. Behind a prefix it is ended,
// so the next prefixed line starts on its own; without one the bytes stay as
// opencode wrote them.
func (s *stderrWriter) Flush() {
	if len(s.partial) == 0 {
		return
	}
	end := ""
	if s.prefix != "" {
		end = "\n"
	}
	s.writeLine(string(s.partial), end)
	s.partial = nil
}

// writeLine writes one line in a single Write, so lines from runs that share
// stderr don't interleave.
func (s *stderrWriter) writeLine(line, end string) {
	if s.prefix != "" {
		line = s.prefix + " " + line
	}
	_, _ = io.WriteString(s.w, redact.String(line)+end)
}
