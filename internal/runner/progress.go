package runner

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// progressWriter parses NDJSON from opencode stdout and writes formatted
// progress lines to stderr, while also buffering raw bytes for later parsing.
type progressWriter struct {
	buf  *bytes.Buffer
	step int
	w    io.Writer
}

func (pw *progressWriter) Write(p []byte) (n int, err error) {
	pw.buf.Write(p)

	scanner := bufio.NewScanner(bytes.NewReader(p))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
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
				fmt.Fprintf(pw.w, "  Step %d: tool %s (error: %s)\n", pw.step, tool, truncate(event.Part.State.Error, 80))
			} else {
				fmt.Fprintf(pw.w, "  Step %d: tool %s\n", pw.step, tool)
			}
		}
	case "text":
		if event.Part != nil && event.Part.Type == "text" {
			fmt.Fprintf(pw.w, "  Step %d: generating text...\n", pw.step)
		}
	case "error":
		if event.Error != nil {
			msg := event.Error.Name
			if event.Error.Data != nil && event.Error.Data.Message != "" {
				msg = truncate(event.Error.Data.Message, 120)
			}
			fmt.Fprintf(pw.w, "  Error: %s\n", msg)
		}
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
