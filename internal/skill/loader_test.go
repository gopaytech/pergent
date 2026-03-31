package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_WithFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "code-review.md")
	content := `---
name: code-review
---

You are a senior engineer reviewing a pull request.
Focus on correctness, clarity, and potential bugs.`

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if s.Name != "code-review" {
		t.Errorf("Name = %q, want %q", s.Name, "code-review")
	}
	expected := "You are a senior engineer reviewing a pull request.\nFocus on correctness, clarity, and potential bugs."
	if s.Body != expected {
		t.Errorf("Body = %q, want %q", s.Body, expected)
	}
}

func TestLoad_WithoutFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "simple.md")
	content := `You are a reviewer. Check for bugs.`

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if s.Name != "simple" {
		t.Errorf("Name = %q, want %q (derived from filename)", s.Name, "simple")
	}
	if s.Body != content {
		t.Errorf("Body = %q, want %q", s.Body, content)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/skill.md")
	if err == nil {
		t.Error("Load() should return error for missing file")
	}
}
