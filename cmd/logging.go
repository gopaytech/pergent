package main

import (
	"io"
	"log"

	"github.com/gopaytech/pergent/internal/runner"
)

// setupLogging sends the standard logger to w with credentials redacted, line
// by line. Errors from the platform clients, config and opencode reach the
// logger through log.Printf and log.Fatalf, so redacting at the logger covers
// every call site, also one added later.
func setupLogging(w io.Writer) {
	log.SetOutput(runner.NewRedactedLineWriter(w))
}
