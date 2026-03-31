package runner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"
)

type RunResult struct {
	SkillName string
	Output    string
	Truncated bool
}

type openCodeEvent struct {
	Type string        `json:"type"`
	Part *openCodePart `json:"part,omitempty"`
}

type openCodePart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func ParseOutput(stdout []byte) (string, error) {
	var lastText string
	scanner := bufio.NewScanner(bytes.NewReader(stdout))
	// Increase buffer size for large outputs
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var event openCodeEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue // skip non-JSON lines
		}
		if event.Part != nil && event.Part.Type == "text" {
			lastText = event.Part.Text
		}
	}
	return lastText, nil
}

func BuildCommand(ctx context.Context, configPath string, diffFile string, prompt string, repoPath string) *exec.Cmd {
	args := []string{
		"run",
		"--format", "json",
		"--file", diffFile,
		prompt,
	}

	cmd := exec.CommandContext(ctx, "opencode", args...)
	cmd.Dir = repoPath

	// Inherit current env and add OPENCODE_CONFIG
	env := os.Environ()
	env = append(env, "OPENCODE_CONFIG="+configPath)
	cmd.Env = env

	return cmd
}

func Run(ctx context.Context, skillName string, configPath string, diffFile string, prompt string, repoPath string, timeout time.Duration) (RunResult, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := BuildCommand(ctx, configPath, diffFile, prompt, repoPath)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	output, parseErr := ParseOutput(stdout.Bytes())
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
		return result, fmt.Errorf("opencode exited with error: %w\nstderr: %s", err, stderr.String())
	}

	return result, nil
}
