# pergent Preset Skills Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Embed preset skills in the pergent binary so users can reference them by name (`--skill code-review`) instead of file path.

**Architecture:** Preset `.md` files live in `internal/skill/presets/` and are embedded via `//go:embed`. A new `Resolve()` function auto-detects whether a `--skill` value is a preset name or file path, then delegates to either the embedded FS or the existing `Load()`. `cmd/main.go` switches from `skill.Load()` to `skill.Resolve()`.

**Tech Stack:** Go `embed` package (standard library). No new dependencies.

---

## File Map

```
internal/skill/
├── loader.go           # Modify: add Resolve() + parse() + embed logic
├── loader_test.go      # Modify: add preset tests
└── presets/
    └── code-review.md  # Create: embedded preset skill (copied from skills/code-review.md)
cmd/
└── main.go             # Modify: skill.Load() → skill.Resolve()
```

---

### Task 1: Create Preset Directory and Embedded Skill File

**Files:**
- Create: `internal/skill/presets/code-review.md`

- [ ] **Step 1: Create presets directory**

Run:
```bash
mkdir -p internal/skill/presets
```

- [ ] **Step 2: Copy the existing skill file to presets directory**

Create `internal/skill/presets/code-review.md` with the same content as `skills/code-review.md`:

```markdown
---
name: code-review
---

You are a senior software engineer reviewing a pull request.

A diff file has been attached showing the changes. Review the code changes and provide feedback on:

1. **Correctness** -- Are there bugs, logic errors, or edge cases not handled?
2. **Clarity** -- Is the code readable and well-structured?
3. **Performance** -- Are there obvious performance issues?
4. **Security** -- Are there potential security vulnerabilities?

For each issue found, reference the specific file and line number in the format `**[filename:line]**`.

If the changes look good, say so briefly. Do not invent issues that don't exist.

Use the repository's AGENTS.md or CLAUDE.md for project-specific conventions if available.

Keep your review concise and actionable.
```

- [ ] **Step 3: Commit**

```bash
git add internal/skill/presets/code-review.md
git commit -m "feat: add preset code-review skill for embedding"
```

---

### Task 2: Add Resolve Function with Embed Support

**Files:**
- Modify: `internal/skill/loader.go`
- Modify: `internal/skill/loader_test.go`

- [ ] **Step 1: Write failing tests for preset skills**

Add the following tests to the end of `internal/skill/loader_test.go`:

```go
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
```

Also add `"strings"` to the import block in `loader_test.go` (it's not there yet).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/skill/ -v -run TestResolve`

Expected: FAIL — `Resolve` not defined.

- [ ] **Step 3: Implement Resolve function with embed**

Replace the entire `internal/skill/loader.go` with:

```go
package skill

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed presets/*.md
var presetFiles embed.FS

var presets = map[string]string{
	"code-review": "presets/code-review.md",
}

type Skill struct {
	Name string
	Body string
}

type frontmatter struct {
	Name string `yaml:"name"`
}

func Resolve(value string) (Skill, error) {
	if isFilePath(value) {
		return Load(value)
	}
	return LoadPreset(value)
}

func isFilePath(value string) bool {
	return strings.Contains(value, "/") || strings.HasSuffix(value, ".md")
}

func LoadPreset(name string) (Skill, error) {
	filename, ok := presets[name]
	if !ok {
		return Skill{}, fmt.Errorf("unknown preset skill: %q", name)
	}

	data, err := presetFiles.ReadFile(filename)
	if err != nil {
		return Skill{}, fmt.Errorf("reading preset skill: %w", err)
	}

	return parse(data, name)
}

func Load(path string) (Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, fmt.Errorf("reading skill file: %w", err)
	}

	base := filepath.Base(path)
	fallbackName := strings.TrimSuffix(base, filepath.Ext(base))
	return parse(data, fallbackName)
}

func parse(data []byte, fallbackName string) (Skill, error) {
	content := string(data)
	var fm frontmatter
	body := content

	if bytes.HasPrefix(data, []byte("---\n")) {
		parts := strings.SplitN(content, "---\n", 3)
		if len(parts) >= 3 {
			if err := yaml.Unmarshal([]byte(parts[1]), &fm); err != nil {
				return Skill{}, fmt.Errorf("parsing frontmatter: %w", err)
			}
			body = strings.TrimSpace(parts[2])
		}
	}

	name := fm.Name
	if name == "" {
		name = fallbackName
	}

	return Skill{Name: name, Body: body}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/skill/ -v`

Expected: All 8 tests PASS (3 existing + 5 new).

- [ ] **Step 5: Commit**

```bash
git add internal/skill/loader.go internal/skill/loader_test.go
git commit -m "feat: add Resolve() with embedded preset skills"
```

---

### Task 3: Wire CLI to Use Resolve

**Files:**
- Modify: `cmd/main.go`

- [ ] **Step 1: Change skill.Load to skill.Resolve**

In `cmd/main.go`, replace the skill loading loop (lines 70-77):

```go
	// Load skills
	var loadedSkills []skill.Skill
	for _, path := range cfg.Skills {
		s, err := skill.Load(path)
		if err != nil {
			log.Fatalf("loading skill %s: %v", path, err)
		}
		loadedSkills = append(loadedSkills, s)
	}
```

With:

```go
	// Load skills
	var loadedSkills []skill.Skill
	for _, value := range cfg.Skills {
		s, err := skill.Resolve(value)
		if err != nil {
			log.Fatalf("loading skill %s: %v", value, err)
		}
		loadedSkills = append(loadedSkills, s)
	}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build -o bin/pergent ./cmd/`

Expected: No errors.

- [ ] **Step 3: Verify --version still works**

Run: `./bin/pergent --version`

Expected: `pergent 0.1.0`

- [ ] **Step 4: Commit**

```bash
git add cmd/main.go
git commit -m "feat: wire CLI to use skill.Resolve for preset support"
```

---

### Task 4: Run All Tests

- [ ] **Step 1: Run full test suite**

Run: `make test`

Expected: All tests pass:
```
ok  github.com/zufardhiyaulhaq/pergent/internal/skill
ok  github.com/zufardhiyaulhaq/pergent/internal/config
ok  github.com/zufardhiyaulhaq/pergent/internal/output
ok  github.com/zufardhiyaulhaq/pergent/internal/platform
ok  github.com/zufardhiyaulhaq/pergent/internal/runner
```

- [ ] **Step 2: Verify build**

Run: `make build && ./bin/pergent --version`

Expected: `pergent 0.1.0`

- [ ] **Step 3: Commit any fixes if tests failed**

If any test failed, fix and commit:
```bash
git add -A
git commit -m "fix: resolve test failures"
```
