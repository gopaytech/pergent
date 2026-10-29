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

// IsFilePath reports whether a --skill value names a file rather than a
// preset.
func IsFilePath(value string) bool { return isFilePath(value) }

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
// result is sorted by path. Entries starting with "." are skipped. A
// "<name>/SKILL.md" without a frontmatter name takes the folder name; a flat
// "*.md" file (including a top-level SKILL.md) takes its file name.
func LoadDir(dir string) ([]Skill, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading skills directory: %w", err)
	}

	type candidate struct {
		path     string
		fallback string
	}

	var candidates []candidate
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}

		if entry.IsDir() {
			path := filepath.Join(dir, name, "SKILL.md")
			if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
				candidates = append(candidates, candidate{path: path, fallback: name})
			}
			continue
		}

		if filepath.Ext(name) == ".md" {
			candidates = append(candidates, candidate{
				path:     filepath.Join(dir, name),
				fallback: strings.TrimSuffix(name, ".md"),
			})
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].path < candidates[j].path })

	skills := make([]Skill, 0, len(candidates))
	for _, c := range candidates {
		path := c.path
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading skill file: %w", err)
		}

		s, err := parse(data, c.fallback)
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
