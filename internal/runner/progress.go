package runner

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/gopaytech/pergent/internal/redact"
)

// progressWriter parses NDJSON from opencode stdout and writes formatted
// progress lines to stderr, while also buffering raw bytes for later parsing.
type progressWriter struct {
	buf  *bytes.Buffer
	step int
	w    io.Writer
	// onStep, if set, is called with the step number at each step_start.
	onStep func(step int)
	// partial holds a line split across writes.
	partial []byte
}

func (pw *progressWriter) Write(p []byte) (n int, err error) {
	pw.buf.Write(p)

	data := append(pw.partial, p...)
	end := bytes.LastIndexByte(data, '\n')
	if end < 0 {
		pw.partial = data
		return len(p), nil
	}
	pw.partial = append([]byte(nil), data[end+1:]...)

	scanner := bufio.NewScanner(bytes.NewReader(data[:end+1]))
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var event openCodeEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		pw.formatEvent(event)
	}

	return len(p), nil
}

func (pw *progressWriter) formatEvent(event openCodeEvent) {
	switch event.Type {
	case "step_start":
		pw.step++
		fmt.Fprintf(pw.w, "  Step %d: starting...\n", pw.step)
		if pw.onStep != nil {
			pw.onStep(pw.step)
		}
	case "step_finish":
		if event.Part != nil && event.Part.Tokens != nil {
			fmt.Fprintf(pw.w, "  Step %d: done (%d tokens)\n", pw.step, event.Part.Tokens.Output)
		} else {
			fmt.Fprintf(pw.w, "  Step %d: done\n", pw.step)
		}
	case "tool_use":
		if event.Part != nil {
			tool := event.Part.Tool
			status := ""
			if event.Part.State != nil {
				status = event.Part.State.Status
			}
			if status == "error" && event.Part.State != nil {
				fmt.Fprintf(pw.w, "  Step %d: tool %s (error: %s)\n", pw.step, tool, truncate(redact.String(event.Part.State.Error), 80))
			} else {
				fmt.Fprintf(pw.w, "  Step %d: tool %s\n", pw.step, tool)
			}
		}
	case "text":
		if event.Part != nil && event.Part.Type == "text" {
			if event.Part.Synthetic {
				fmt.Fprintf(pw.w, "  Step %d: opencode added its own message (%s)\n", pw.step, truncate(redact.String(event.Part.Text), 80))
			} else {
				fmt.Fprintf(pw.w, "  Step %d: generating text...\n", pw.step)
			}
		}
	case "error":
		if event.Error != nil {
			msg := event.Error.Name
			if event.Error.Data != nil && event.Error.Data.Message != "" {
				// Redact before cutting, so a key across the cut can't print in part.
				msg = truncate(redact.String(event.Error.Data.Message), 120)
			}
			fmt.Fprintf(pw.w, "  Error: %s\n", redact.String(msg))
		}
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
