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
