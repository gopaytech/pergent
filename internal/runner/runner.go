package runner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

type RunResult struct {
	SkillName string
	Output    string
	Truncated bool
}

// NDJSON event types from opencode --format json
type openCodeEvent struct {
	Type  string          `json:"type"`
	Part  *openCodePart   `json:"part,omitempty"`
	Error *openCodeError  `json:"error,omitempty"`
}

type openCodePart struct {
	Type   string          `json:"type"`
	Text   string          `json:"text"`
	Tool   string          `json:"tool"`
	Reason string          `json:"reason"`
	State  *openCodeState  `json:"state,omitempty"`
	Tokens *openCodeTokens `json:"tokens,omitempty"`
}

type openCodeState struct {
	Status string                 `json:"status"`
	Input  map[string]interface{} `json:"input,omitempty"`
	Error  string                 `json:"error,omitempty"`
}

type openCodeTokens struct {
	Total  int `json:"total"`
	Input  int `json:"input"`
	Output int `json:"output"`
}

type openCodeError struct {
	Name string         `json:"name"`
	Data *openCodeErrData `json:"data,omitempty"`
}

type openCodeErrData struct {
	Message string `json:"message"`
}

// progressWriter parses NDJSON from opencode stdout and writes formatted
// progress lines to stderr, while also writing raw bytes to a buffer for parsing.
type progressWriter struct {
	buf    *bytes.Buffer
	step   int
	w      io.Writer // stderr
}

func (pw *progressWriter) Write(p []byte) (n int, err error) {
	// Always write to buffer for later parsing
	pw.buf.Write(p)

	// Parse each complete line for progress display
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

func ParseOutput(stdout []byte) (string, error) {
	var lastText string
	scanner := bufio.NewScanner(bytes.NewReader(stdout))
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
		if event.Part != nil && event.Part.Type == "text" {
			lastText = event.Part.Text
		}
	}
	return stripThinkTags(lastText), nil
}

// stripThinkTags removes <think>...</think> blocks from model output
// Some models (DeepSeek, Qwen) wrap reasoning in these tags
func stripThinkTags(s string) string {
	for {
		start := strings.Index(s, "<think>")
		if start == -1 {
			break
		}
		end := strings.Index(s, "</think>")
		if end == -1 {
			// Unclosed tag — remove from <think> to end
			s = s[:start]
			break
		}
		s = s[:start] + s[end+len("</think>"):]
	}
	return strings.TrimSpace(s)
}

func BuildCommand(ctx context.Context, configPath string, diffFile string, message string, repoPath string) *exec.Cmd {
	args := []string{
		"run",
		"--format", "json",
	}
	if diffFile != "" {
		args = append(args, "--file", diffFile)
	}
	args = append(args, "--", message)

	cmd := exec.CommandContext(ctx, "opencode", args...)
	cmd.Dir = repoPath

	// Inherit current env, add OPENCODE_CONFIG only if a config was generated
	env := os.Environ()
	if configPath != "" {
		env = append(env, "OPENCODE_CONFIG="+configPath)
	}
	cmd.Env = env

	return cmd
}

func Run(ctx context.Context, skillName string, configPath string, diffFile string, message string, repoPath string, timeout time.Duration) (RunResult, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := BuildCommand(ctx, configPath, diffFile, message, repoPath)

	pw := &progressWriter{buf: &bytes.Buffer{}, w: os.Stderr}
	cmd.Stdout = pw
	cmd.Stderr = os.Stderr

	err := cmd.Run()

	// Debug: write raw stdout to temp file for troubleshooting
	if debugFile := os.Getenv("PERGENT_DEBUG_OUTPUT"); debugFile != "" {
		os.WriteFile(debugFile, pw.buf.Bytes(), 0644)
	}

	output, parseErr := ParseOutput(pw.buf.Bytes())
	if parseErr != nil {
		return RunResult{SkillName: skillName}, fmt.Errorf("parsing output: %w", parseErr)
	}

	result := RunResult{
		SkillName: skillName,
		Output:    output,
	}

	if ctx.Err() == context.DeadlineExceeded {
		result.Truncated = true
		return result, nil
	}

	if err != nil {
		return result, fmt.Errorf("opencode exited with error: %w", err)
	}

	return result, nil
}
