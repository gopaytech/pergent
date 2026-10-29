package skill

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed presets/*.md
var presetFiles embed.FS

var presets = map[string]string{
	"code-review": "presets/code-review.md",
}

type Skill struct {
	Name        string
	Body        string
	Paths       []string
	Exclude     []string
	CrossBatch  bool
	HasBlocking bool
}

type frontmatter struct {
	Name       string   `yaml:"name"`
	Paths      []string `yaml:"paths"`
	Exclude    []string `yaml:"exclude"`
	CrossBatch bool     `yaml:"cross_batch"`
}

var blockingHeading = regexp.MustCompile(`(?im)^#{1,6}\s*blocking\b`)

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

// LoadDir loads skills from a directory. Two layouts are accepted: top-level
// "*.md" files and "<name>/SKILL.md" folders. Anything else is ignored. The
// result is sorted by path. A SKILL.md without a frontmatter name takes its
// folder name.
func LoadDir(dir string) ([]Skill, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading skills directory: %w", err)
	}

	var paths []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		paths = append(paths, filepath.Join(dir, entry.Name()))
	}

	folders, err := filepath.Glob(filepath.Join(dir, "*", "SKILL.md"))
	if err != nil {
		return nil, fmt.Errorf("listing skill folders: %w", err)
	}
	paths = append(paths, folders...)
	sort.Strings(paths)

	skills := make([]Skill, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading skill file: %w", err)
		}

		fallbackName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if filepath.Base(path) == "SKILL.md" {
			fallbackName = filepath.Base(filepath.Dir(path))
		}

		s, err := parse(data, fallbackName)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		skills = append(skills, s)
	}

	return skills, nil
}

// CheckUnique returns an error naming the first skill name used twice.
func CheckUnique(skills []Skill) error {
	seen := make(map[string]bool, len(skills))
	for _, s := range skills {
		if seen[s.Name] {
			return fmt.Errorf("duplicate skill name %q", s.Name)
		}
		seen[s.Name] = true
	}
	return nil
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

	return Skill{
		Name:        name,
		Body:        body,
		Paths:       fm.Paths,
		Exclude:     fm.Exclude,
		CrossBatch:  fm.CrossBatch,
		HasBlocking: blockingHeading.MatchString(body),
	}, nil
}
