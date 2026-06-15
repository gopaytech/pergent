package runner

import (
	"context"
	"testing"
)

func TestBuildCommand(t *testing.T) {
	ctx := context.Background()
	cmd := BuildCommand(ctx, "/tmp/config.json", "/tmp/diff.patch", "", "Review the attached diff", ".")

	found := false
	for _, env := range cmd.Env {
		if env == "OPENCODE_CONFIG=/tmp/config.json" {
			found = true
		}
	}
	if !found {
		t.Error("OPENCODE_CONFIG not set in command env")
	}

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
	cmd := BuildCommand(ctx, "/tmp/config.json", "", "", "Say hello", ".")

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

func TestBuildCommand_NoConfig(t *testing.T) {
	ctx := context.Background()
	cmd := BuildCommand(ctx, "", "/tmp/diff.patch", "", "Review", ".")

	for _, env := range cmd.Env {
		if env == "OPENCODE_CONFIG=" {
			t.Error("should not set empty OPENCODE_CONFIG")
		}
	}
}

func TestBuildCommand_WithPrevReviewFile(t *testing.T) {
	ctx := context.Background()
	cmd := BuildCommand(ctx, "/tmp/config.json", "/tmp/diff.patch", "/tmp/prev-review.md", "Review the attached diff", ".")

	diffIdx := -1
	prevIdx := -1
	for i, arg := range cmd.Args {
		if arg == "--file" && i+1 < len(cmd.Args) && cmd.Args[i+1] == "/tmp/diff.patch" {
			diffIdx = i
		}
		if arg == "--file" && i+1 < len(cmd.Args) && cmd.Args[i+1] == "/tmp/prev-review.md" {
			prevIdx = i
		}
	}
	if diffIdx == -1 {
		t.Error("missing --file flag for diff")
	}
	if prevIdx == -1 {
		t.Error("missing --file flag for previous review")
	}
	if diffIdx != -1 && prevIdx != -1 && diffIdx > prevIdx {
		t.Errorf("diff --file (index %d) should come before previous-review --file (index %d)", diffIdx, prevIdx)
	}
}

func TestBuildCommand_NoPrevReviewFile(t *testing.T) {
	ctx := context.Background()
	cmd := BuildCommand(ctx, "/tmp/config.json", "/tmp/diff.patch", "", "Review the attached diff", ".")

	fileCount := 0
	for _, arg := range cmd.Args {
		if arg == "--file" {
			fileCount++
		}
	}
	if fileCount != 1 {
		t.Errorf("--file count = %d, want 1 (diff only)", fileCount)
	}
}
