package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/gopaytech/pergent/internal/config"
	"github.com/gopaytech/pergent/internal/redact"
	"github.com/gopaytech/pergent/internal/runner"
)

func runTest(ctx context.Context) {
	if !checkOpencode(ctx, os.Stderr) {
		os.Exit(1)
	}
}

// lockedWriter serialises writes: opencode's stdout and stderr are copied by
// separate goroutines.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// checkOpencode sends opencode a hello prompt and reports on w whether it
// answered. Everything that reaches w is redacted: opencode can echo the
// API key in its events, its stderr or the model's reply.
func checkOpencode(ctx context.Context, w io.Writer) bool {
	w = &lockedWriter{w: w}
	fmt.Fprintf(w, "Testing opencode connection...\n")

	configPath, cleanup, err := config.GenerateOpenCodeConfig(1, 0, "", ".")
	if err != nil {
		log.Fatalf("generating opencode config: %v", err)
	}
	defer cleanup()

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	cmd := runner.BuildCommand(ctx, configPath, nil, "Reply with exactly: hello from opencode", ".")

	// What opencode prints is shown line by line with credentials redacted.
	// stdout is also kept for parsing.
	var stdout bytes.Buffer
	shownOut, shownErr := runner.NewRedactedLineWriter(w), runner.NewRedactedLineWriter(w)
	cmd.Stdout = io.MultiWriter(&stdout, shownOut)
	cmd.Stderr = shownErr

	err = cmd.Run()
	shownOut.Flush()
	shownErr.Flush()

	transcript := runner.ParseOutput(stdout.Bytes())
	output := transcript.Answer

	if e := transcript.LastError(); e != nil {
		fmt.Fprintf(w, "\nFAIL: %s\n", redact.String(e.Name+": "+e.Message))
		return false
	}
	if err != nil && output == "" {
		fmt.Fprintf(w, "\nFAIL: %s\n", redact.String(err.Error()))
		return false
	}

	if output == "" {
		fmt.Fprintf(w, "\nFAIL: opencode returned empty response\n")
		return false
	}

	fmt.Fprintf(w, "\nOK: %s\n", redact.String(output))
	return true
}
