package skill

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Skill struct {
	Name string
	Body string
}

type frontmatter struct {
	Name string `yaml:"name"`
}

func Load(path string) (Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, fmt.Errorf("reading skill file: %w", err)
	}

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
		base := filepath.Base(path)
		name = strings.TrimSuffix(base, filepath.Ext(base))
	}

	return Skill{Name: name, Body: body}, nil
}
