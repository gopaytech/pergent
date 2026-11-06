package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubOpencode puts an `opencode` script on PATH that prints stdout on its
// standard output and stderr on its standard error, then exits 0.
func stubOpencode(t *testing.T, stdout, stderr string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "out"), []byte(stdout), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "err"), []byte(stderr), 0o600); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ncat \"" + filepath.Join(dir, "out") + "\"\ncat \"" + filepath.Join(dir, "err") + "\" >&2\n"
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "opencode"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestCheckOpencode_RedactsEverythingItPrints(t *testing.T) {
	const key = "sk-test-0123456789abcdefghij"
	t.Setenv("OPENCODE_API_KEY", key)
	t.Setenv("OPENCODE_PROVIDER", "")
	// opencode echoes the key in its stdout events, in its stderr and in the
	// model's reply.
	stdout := `{"type":"step_start"}` + "\n" +
		`{"type":"text","part":{"type":"text","text":"hello ` + key + `"}}` + "\n" +
		`{"type":"step_finish","part":{"reason":"stop"}}` + "\n"
	stubOpencode(t, stdout, "warning: using key "+key+"\nlast line, no newline "+key)

	var out bytes.Buffer
	if !checkOpencode(context.Background(), &out) {
		t.Fatalf("checkOpencode failed:\n%s", out.String())
	}
	got := out.String()
	for i := 8; i <= len(key); i++ {
		if strings.Contains(got, key[:i]) {
			t.Fatalf("output keeps the first %d characters of the key:\n%s", i, got)
		}
	}
	if !strings.Contains(got, "OK: hello [REDACTED]") {
		t.Errorf("output = %q, want the redacted reply after OK:", got)
	}
}

func TestCheckOpencode_FailsOnEmptyReply(t *testing.T) {
	t.Setenv("OPENCODE_PROVIDER", "")
	stubOpencode(t, "", "")

	var out bytes.Buffer
	if checkOpencode(context.Background(), &out) {
		t.Errorf("checkOpencode passed without a reply:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "FAIL: opencode returned empty response") {
		t.Errorf("output = %q, want the empty-response failure", out.String())
	}
}
