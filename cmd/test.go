package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/gopaytech/pergent/internal/config"
	"github.com/gopaytech/pergent/internal/redact"
	"github.com/gopaytech/pergent/internal/runner"
)

func runTest(ctx context.Context) {
	fmt.Fprintf(os.Stderr, "Testing opencode connection...\n")

	configPath, cleanup, err := config.GenerateOpenCodeConfig(1, 0, "", ".")
	if err != nil {
		log.Fatalf("generating opencode config: %v", err)
	}
	defer cleanup()

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	cmd := runner.BuildCommand(ctx, configPath, "", "", "Reply with exactly: hello from opencode", ".")

	var stdout bytes.Buffer
	cmd.Stdout = io.MultiWriter(&stdout, os.Stderr)
	cmd.Stderr = os.Stderr

	err = cmd.Run()

	transcript := runner.ParseOutput(stdout.Bytes())
	output := transcript.Answer

	if e := transcript.LastError(); e != nil {
		fmt.Fprintf(os.Stderr, "\nFAIL: %s\n", redact.String(e.Name+": "+e.Message))
		os.Exit(1)
	}
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
