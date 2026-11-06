package main

import (
	"bytes"
	"errors"
	"log"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// The test above covers setupLogging; this one covers main calling it, in a
// process of its own because log.Fatalf exits. It also shows the last line
// before an exit is written, not lost in a buffer.
func TestMain_RedactsLogFatalf(t *testing.T) {
	const token = "glpat-0123456789abcdefghij"
	if os.Getenv("PERGENT_TEST_RUN_MAIN") == "1" {
		os.Args = []string{"pergent", "--local", "--skill", "/nonexistent/" + token + ".md"}
		main()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestMain_RedactsLogFatalf$")
	cmd.Env = append(os.Environ(), "PERGENT_TEST_RUN_MAIN=1", "GITLAB_TOKEN="+token)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("main() did not exit with an error:\n%s", out)
	}
	if !strings.Contains(string(out), "loading skill") {
		t.Fatalf("output has no log.Fatalf line:\n%s", out)
	}
	if strings.Contains(string(out), token[:8]) {
		t.Errorf("log.Fatalf printed the token:\n%s", out)
	}
}

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
