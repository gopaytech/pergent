package main

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"testing"
)

func TestSetupLogging_RedactsTheStandardLogger(t *testing.T) {
	const token = "glpat-0123456789abcdefghij"
	t.Setenv("GITLAB_TOKEN", token)
	old := log.Writer()
	t.Cleanup(func() { log.SetOutput(old) })

	var buf bytes.Buffer
	setupLogging(&buf)
	err := errors.New("fetching diffs: status 500: " + token + " rejected")
	log.Printf("warning: could not search for existing comment: %v", err)
	log.Print("no newline needed: " + token)

	got := buf.String()
	if strings.Contains(got, token[:8]) {
		t.Errorf("log keeps part of the token:\n%s", got)
	}
	if strings.Count(got, "[REDACTED]") != 2 {
		t.Errorf("log = %q, want two redacted tokens", got)
	}
}
