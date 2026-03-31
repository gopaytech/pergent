package skill

import (
	"os"
	"path/filepath"
	"strings"
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

func TestResolve_PresetByName(t *testing.T) {
	s, err := Resolve("code-review")
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if s.Name != "code-review" {
		t.Errorf("Name = %q, want %q", s.Name, "code-review")
	}
	if s.Body == "" {
		t.Error("Body should not be empty for preset skill")
	}
	if !strings.Contains(s.Body, "senior software engineer") {
		t.Error("Body should contain preset content")
	}
}

func TestResolve_UnknownPreset(t *testing.T) {
	_, err := Resolve("nonexistent-skill")
	if err == nil {
		t.Error("Resolve() should error for unknown preset name")
	}
	if !strings.Contains(err.Error(), "unknown preset skill") {
		t.Errorf("error = %q, should mention 'unknown preset skill'", err.Error())
	}
}

func TestResolve_FilePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom.md")
	content := "You are a custom reviewer."
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	s, err := Resolve(path)
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if s.Name != "custom" {
		t.Errorf("Name = %q, want %q", s.Name, "custom")
	}
	if s.Body != content {
		t.Errorf("Body = %q, want %q", s.Body, content)
	}
}

func TestResolve_FilePathWithSlash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "review.md")
	content := "Review content."
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	s, err := Resolve(path)
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if s.Body != content {
		t.Errorf("Body = %q, want %q", s.Body, content)
	}
}

func TestResolve_FilePathWithDotMd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	content := "Test content."
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	s, err := Resolve(path)
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if s.Body != content {
		t.Errorf("Body = %q, want %q", s.Body, content)
	}
}

func TestIsFilePath(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{"code-review", false},
		{"security-review", false},
		{"", false},
		{"./custom.md", true},
		{"/abs/path.md", true},
		{"code-review.md", true},
		{"some/path", true},
		{"./skills/review.md", true},
		{"../other/skill.md", true},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			got := isFilePath(tt.value)
			if got != tt.want {
				t.Errorf("isFilePath(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}
