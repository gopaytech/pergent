package runner

import (
	"context"
	"testing"
)

func TestParseOutput_TextEvents(t *testing.T) {
	// opencode --format json produces streaming NDJSON
	stdout := `{"type":"message.part.updated","part":{"type":"thinking","text":"Let me analyze..."}}
{"type":"message.part.updated","part":{"type":"text","text":"## Review\n\n"}}
{"type":"message.part.updated","part":{"type":"text","text":"## Review\n\n- Bug found in line 42"}}
{"type":"step_finish","tokens":{"input":1234,"output":567}}
`
	result, err := ParseOutput([]byte(stdout))
	if err != nil {
		t.Fatalf("ParseOutput() error: %v", err)
	}
	// Last text event should be the final cumulative output
	expected := "## Review\n\n- Bug found in line 42"
	if result != expected {
		t.Errorf("result = %q, want %q", result, expected)
	}
}

func TestParseOutput_NoTextEvents(t *testing.T) {
	stdout := `{"type":"step_finish","tokens":{"input":100,"output":50}}
`
	result, err := ParseOutput([]byte(stdout))
	if err != nil {
		t.Fatalf("ParseOutput() error: %v", err)
	}
	if result != "" {
		t.Errorf("result = %q, want empty string", result)
	}
}

func TestParseOutput_EmptyInput(t *testing.T) {
	result, err := ParseOutput([]byte(""))
	if err != nil {
		t.Fatalf("ParseOutput() error: %v", err)
	}
	if result != "" {
		t.Errorf("result = %q, want empty string", result)
	}
}

func TestBuildCommand(t *testing.T) {
	ctx := context.Background()
	cmd := BuildCommand(ctx, "/tmp/config.json", "/tmp/diff.patch", "Review the attached diff", ".")

	// Verify env contains OPENCODE_CONFIG
	found := false
	for _, env := range cmd.Env {
		if env == "OPENCODE_CONFIG=/tmp/config.json" {
			found = true
		}
	}
	if !found {
		t.Error("OPENCODE_CONFIG not set in command env")
	}

	// Verify args contain expected flags
	args := cmd.Args
	if args[0] != "opencode" {
		t.Errorf("command = %q, want %q", args[0], "opencode")
	}

	hasRun := false
	hasDiffFile := false
	hasFormat := false
	hasMessage := false
	for i, arg := range args {
		if arg == "run" {
			hasRun = true
		}
		if arg == "--file" && i+1 < len(args) && args[i+1] == "/tmp/diff.patch" {
			hasDiffFile = true
		}
		if arg == "--format" && i+1 < len(args) && args[i+1] == "json" {
			hasFormat = true
		}
		if arg == "Review the attached diff" {
			hasMessage = true
		}
	}
	if !hasRun {
		t.Error("missing 'run' subcommand")
	}
	if !hasDiffFile {
		t.Error("missing --file flag for diff")
	}
	if !hasFormat {
		t.Error("missing --format json flag")
	}
	if !hasMessage {
		t.Error("missing message argument")
	}
}

func TestBuildCommand_NoDiffFile(t *testing.T) {
	ctx := context.Background()
	cmd := BuildCommand(ctx, "/tmp/config.json", "", "Say hello", ".")

	hasFile := false
	for _, arg := range cmd.Args {
		if arg == "--file" {
			hasFile = true
		}
	}
	if hasFile {
		t.Error("should not have --file flag when diffFile is empty")
	}
}
