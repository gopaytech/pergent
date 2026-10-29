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
	if !s.HasBlocking {
		t.Error("preset should declare a Blocking section")
	}
	if strings.Contains(s.Body, "**[filename:line]**") {
		t.Error("preset body should no longer carry the old output format instruction")
	}
	for _, format := range []string{"If the changes look good, say so briefly", "Keep your review concise and actionable"} {
		if strings.Contains(s.Body, format) {
			t.Errorf("preset body still carries the output-format line %q", format)
		}
	}
	if !strings.Contains(s.Body, "Do not invent issues that don't exist.") || !strings.Contains(s.Body, "**Security**") {
		t.Error("preset body lost its review focus")
	}
}

func TestParse_ScopeFrontmatter(t *testing.T) {
	data := []byte(`---
name: scoped
paths: ["**/manifests/**"]
exclude: ["**/*.md"]
cross_batch: true
---

Review the manifests.`)

	s, err := parse(data, "fallback")
	if err != nil {
		t.Fatalf("parse() error: %v", err)
	}
	if len(s.Paths) != 1 || s.Paths[0] != "**/manifests/**" {
		t.Errorf("Paths = %v, want [**/manifests/**]", s.Paths)
	}
	if len(s.Exclude) != 1 || s.Exclude[0] != "**/*.md" {
		t.Errorf("Exclude = %v, want [**/*.md]", s.Exclude)
	}
	if !s.CrossBatch {
		t.Error("CrossBatch = false, want true")
	}
}

func TestParse_HasBlocking(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"heading present", "Intro\n\n## Blocking\n- x", true},
		{"no heading", "Intro only", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := parse([]byte(tt.body), "x")
			if err != nil {
				t.Fatalf("parse() error: %v", err)
			}
			if s.HasBlocking != tt.want {
				t.Errorf("HasBlocking = %v, want %v", s.HasBlocking, tt.want)
			}
		})
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

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDir_BothLayouts(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.md"), "---\nname: alpha\n---\n\nAlpha body.")
	writeFile(t, filepath.Join(dir, "b", "SKILL.md"), "---\nname: beta\n---\n\nBeta body.")
	writeFile(t, filepath.Join(dir, "b", "README.md"), "Not a skill.")
	writeFile(t, filepath.Join(dir, "c", "deeper", "SKILL.md"), "Too deep.")
	writeFile(t, filepath.Join(dir, "notes.txt"), "Not markdown.")

	skills, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir() error: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("len(skills) = %d, want 2: %+v", len(skills), skills)
	}
	if skills[0].Name != "alpha" || skills[1].Name != "beta" {
		t.Errorf("names = %q, %q; want alpha, beta", skills[0].Name, skills[1].Name)
	}
	if skills[1].Body != "Beta body." {
		t.Errorf("Body = %q, want %q", skills[1].Body, "Beta body.")
	}
}

func TestLoadDir_SkillMdFallbackName(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "my-skill", "SKILL.md"), "Just a body.")

	skills, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir() error: %v", err)
	}
	if len(skills) != 1 || skills[0].Name != "my-skill" {
		t.Fatalf("skills = %+v, want one named my-skill", skills)
	}
}

func TestLoadDir_Missing(t *testing.T) {
	if _, err := LoadDir(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("LoadDir() error = nil, want error for missing dir")
	}
}

func TestLoadDir_InvalidFrontmatter(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "bad.md"), "---\nname: [unclosed\n---\n\nBody.")

	if _, err := LoadDir(dir); err == nil {
		t.Fatal("LoadDir() error = nil, want error for bad frontmatter")
	}
}

func TestCheckUnique(t *testing.T) {
	dup := []Skill{{Name: "s"}, {Name: "t"}, {Name: "s"}}
	err := CheckUnique(dup)
	if err == nil || !strings.Contains(err.Error(), `"s"`) {
		t.Errorf("CheckUnique(dup) = %v, want error naming %q", err, "s")
	}
	if err := CheckUnique([]Skill{{Name: "s"}, {Name: "t"}}); err != nil {
		t.Errorf("CheckUnique(distinct) = %v, want nil", err)
	}
}
