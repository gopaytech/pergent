package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/gopaytech/pergent/internal/config"
	"github.com/gopaytech/pergent/internal/runner"
)

func runTest(ctx context.Context) {
	fmt.Fprintf(os.Stderr, "Testing opencode connection...\n")

	configPath, cleanup, err := config.GenerateOpenCodeConfig(1, 10000, "")
	if err != nil {
		log.Fatalf("generating opencode config: %v", err)
	}
	defer cleanup()

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	args := []string{"run", "--format", "json", "--", "Reply with exactly: hello from opencode"}
	cmd := exec.CommandContext(ctx, "opencode", args...)
	cmd.Dir = "."

	env := os.Environ()
	if configPath != "" {
		env = append(env, "OPENCODE_CONFIG="+configPath)
	}
	cmd.Env = env

	var stdout bytes.Buffer
	cmd.Stdout = io.MultiWriter(&stdout, os.Stderr)
	cmd.Stderr = os.Stderr

	err = cmd.Run()

	output, _ := runner.ParseOutput(stdout.Bytes())

	if err != nil && output == "" {
		fmt.Fprintf(os.Stderr, "\nFAIL: %v\n", err)
		os.Exit(1)
	}

	if output == "" {
		fmt.Fprintf(os.Stderr, "\nFAIL: opencode returned empty response\n")
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "\nOK: %s\n", output)
}
