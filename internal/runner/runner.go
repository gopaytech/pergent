package runner

import (
	"bytes"
	"context"
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

func BuildCommand(ctx context.Context, configPath string, diffFile string, prevReviewFile string, message string, repoPath string) *exec.Cmd {
	args := []string{
		"run",
		"--format", "json",
	}
	if diffFile != "" {
		args = append(args, "--file", diffFile)
	}
	if prevReviewFile != "" {
		args = append(args, "--file", prevReviewFile)
	}
	args = append(args, "--", message)

	cmd := exec.CommandContext(ctx, "opencode", args...)
	cmd.Dir = repoPath

	env := os.Environ()
	if configPath != "" {
		env = append(env, "OPENCODE_CONFIG="+configPath)
	}
	cmd.Env = env

	return cmd
}

func Run(ctx context.Context, skillName string, configPath string, diffFile string, prevReviewFile string, message string, repoPath string, timeout time.Duration) (RunResult, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := BuildCommand(ctx, configPath, diffFile, prevReviewFile, message, repoPath)

	pw := &progressWriter{buf: &bytes.Buffer{}, w: os.Stderr}
	cmd.Stdout = pw
	cmd.Stderr = os.Stderr

	err := cmd.Run()

	if debugFile := os.Getenv("PERGENT_DEBUG_OUTPUT"); debugFile != "" {
		if writeErr := os.WriteFile(debugFile, pw.buf.Bytes(), 0644); writeErr != nil {
			fmt.Fprintf(os.Stderr, "warning: could not write debug output: %v\n", writeErr)
		}
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
