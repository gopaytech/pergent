package config

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// instructionsFor generates a CI-mode config for repo and returns its
// instructions and what was logged.
func instructionsFor(t *testing.T, repo string, denySecretFiles bool) ([]string, string) {
	t.Helper()
	t.Setenv("OPENCODE_PROVIDER", "anthropic")
	t.Setenv("OPENCODE_MODEL", "claude-sonnet-4")
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	configPath, cleanup, err := GenerateOpenCodeConfigWith(OpenCodeOptions{
		MaxTurns: 20, RepoPath: repo, DenySecretFiles: denySecretFiles,
	})
	if err != nil {
		t.Fatalf("GenerateOpenCodeConfigWith() error: %v", err)
	}
	defer cleanup()
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var cfg openCodeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg.Instructions, logs.String()
}

func put(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func symlink(t *testing.T, target, dir, name string) {
	t.Helper()
	if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
		t.Fatal(err)
	}
}

func TestProjectInstructions_SymlinkOutsideTheRepo(t *testing.T) {
	outside := put(t, t.TempDir(), "secret.txt", "TOKEN=abc\n")

	t.Run("skipped with a warning, the next name is used", func(t *testing.T) {
		repo := t.TempDir()
		symlink(t, outside, repo, "AGENTS.md")
		claude := put(t, repo, "CLAUDE.md", "# context")

		got, logs := instructionsFor(t, repo, false)
		if len(got) != 1 || got[0] != claude {
			t.Errorf("instructions = %v, want [%s]", got, claude)
		}
		if !strings.Contains(logs, "warning") || !strings.Contains(logs, filepath.Join(repo, "AGENTS.md")) {
			t.Errorf("log = %q, want a warning naming %s", logs, filepath.Join(repo, "AGENTS.md"))
		}
	})

	t.Run("no instructions when nothing else is usable", func(t *testing.T) {
		repo := t.TempDir()
		symlink(t, outside, repo, "AGENTS.md")
		symlink(t, "..", repo, "CLAUDE.md")

		got, _ := instructionsFor(t, repo, false)
		if len(got) != 0 {
			t.Errorf("instructions = %v, want none", got)
		}
	})

	t.Run("a dangling link is skipped", func(t *testing.T) {
		repo := t.TempDir()
		symlink(t, "nowhere.md", repo, "AGENTS.md")

		got, _ := instructionsFor(t, repo, false)
		if len(got) != 0 {
			t.Errorf("instructions = %v, want none", got)
		}
	})

	t.Run("/proc/self/environ", func(t *testing.T) {
		if runtime.GOOS != "linux" {
			t.Skip("no /proc/self/environ on " + runtime.GOOS)
		}
		repo := t.TempDir()
		symlink(t, "/proc/self/environ", repo, "AGENTS.md")

		got, _ := instructionsFor(t, repo, false)
		if len(got) != 0 {
			t.Errorf("instructions = %v, want none", got)
		}
	})
}

func TestProjectInstructions_SymlinkInsideTheRepo(t *testing.T) {
	t.Run("AGENTS.md to a file in a subdirectory", func(t *testing.T) {
		repo := t.TempDir()
		put(t, repo, "docs/agents.md", "# context")
		symlink(t, "docs/agents.md", repo, "AGENTS.md")

		got, logs := instructionsFor(t, repo, false)
		want := filepath.Join(repo, "AGENTS.md")
		if len(got) != 1 || got[0] != want {
			t.Errorf("instructions = %v, want [%s]", got, want)
		}
		if strings.Contains(logs, "warning") {
			t.Errorf("log = %q, want no warning", logs)
		}
	})

	t.Run("AGENTS.md to CLAUDE.md", func(t *testing.T) {
		repo := t.TempDir()
		put(t, repo, "CLAUDE.md", "# context")
		symlink(t, "CLAUDE.md", repo, "AGENTS.md")

		got, logs := instructionsFor(t, repo, false)
		want := filepath.Join(repo, "AGENTS.md")
		if len(got) != 1 || got[0] != want {
			t.Errorf("instructions = %v, want [%s]", got, want)
		}
		if strings.Contains(logs, "warning") {
			t.Errorf("log = %q, want no warning", logs)
		}
	})

	t.Run("repo root reached through a symlink", func(t *testing.T) {
		real := t.TempDir()
		put(t, real, "docs/agents.md", "# context")
		symlink(t, "docs/agents.md", real, "AGENTS.md")
		via := filepath.Join(t.TempDir(), "checkout")
		if err := os.Symlink(real, via); err != nil {
			t.Fatal(err)
		}

		got, logs := instructionsFor(t, via, false)
		want := filepath.Join(via, "AGENTS.md")
		if len(got) != 1 || got[0] != want {
			t.Errorf("instructions = %v, want [%s]", got, want)
		}
		if strings.Contains(logs, "warning") {
			t.Errorf("log = %q, want no warning", logs)
		}
	})
}

func TestProjectInstructions_SymlinkToADeniedFile(t *testing.T) {
	t.Run("an env file", func(t *testing.T) {
		repo := t.TempDir()
		put(t, repo, "prod.env", "TOKEN=abc\n")
		symlink(t, "prod.env", repo, "AGENTS.md")

		got, logs := instructionsFor(t, repo, false)
		if len(got) != 0 {
			t.Errorf("instructions = %v, want none", got)
		}
		if !strings.Contains(logs, "warning") {
			t.Errorf("log = %q, want a warning", logs)
		}
	})

	t.Run("a secret file is denied only with the deny flag", func(t *testing.T) {
		repo := t.TempDir()
		put(t, repo, "keys/id.pem", "KEY\n")
		symlink(t, "keys/id.pem", repo, "AGENTS.md")

		if got, _ := instructionsFor(t, repo, false); len(got) != 1 {
			t.Errorf("without the flag: instructions = %v, want the link kept", got)
		}
		if got, _ := instructionsFor(t, repo, true); len(got) != 0 {
			t.Errorf("with the flag: instructions = %v, want none", got)
		}
	})
}

func TestProjectInstructions_NotARegularFile(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	symlink(t, "dir", repo, "AGENTS.md")

	if got, _ := instructionsFor(t, repo, false); len(got) != 0 {
		t.Errorf("instructions = %v, want none for a link to a directory", got)
	}
}
