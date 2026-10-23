package diff

import (
	"bufio"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// Skip records a file that was left out of a review and why.
type Skip struct {
	Path, Rule string
}

var lockfiles = map[string]bool{
	"package-lock.json": true,
	"go.sum":            true,
	"yarn.lock":         true,
	"pnpm-lock.yaml":    true,
	"Cargo.lock":        true,
	"composer.lock":     true,
	"Gemfile.lock":      true,
	"poetry.lock":       true,
	"Pipfile.lock":      true,
}

var vendoredDirs = map[string]bool{
	"vendor":       true,
	"node_modules": true,
	"third_party":  true,
}

var generatedLine = regexp.MustCompile(`(?i)^\+.*code generated .*do not edit`)

// Classifier decides which files every skill skips by default.
type Classifier struct {
	generated []string
}

// NewClassifier reads <repoPath>/.gitattributes, if present, and keeps the
// patterns marked linguist-generated.
func NewClassifier(repoPath string) Classifier {
	var c Classifier
	fh, err := os.Open(filepath.Join(repoPath, ".gitattributes"))
	if err != nil {
		return c
	}
	defer fh.Close()

	sc := bufio.NewScanner(fh)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		for _, attr := range fields[1:] {
			if attr == "linguist-generated" || attr == "linguist-generated=true" {
				pattern := strings.TrimPrefix(fields[0], "/")
				if strings.HasSuffix(pattern, "/") {
					pattern += "**"
				}
				c.generated = append(c.generated, pattern)
				break
			}
		}
	}
	return c
}

// Exclusion returns the first rule that applies to f, or "" when none does.
// optIn reports whether a skill's paths may override the rule.
func (c Classifier) Exclusion(f FileDiff) (rule string, optIn bool) {
	if f.Deleted {
		return "deleted", false
	}
	if f.Binary || hasLine(f.Patch, func(l string) bool {
		return strings.HasPrefix(l, "GIT binary patch") ||
			(strings.HasPrefix(l, "Binary files ") && strings.HasSuffix(l, " differ"))
	}) {
		return "binary", false
	}
	if !hasLine(f.Patch, func(l string) bool { return strings.HasPrefix(l, "@@") }) {
		return "no content changes", false
	}
	if lockfiles[path.Base(f.Path)] {
		return "lockfile", true
	}
	for _, seg := range strings.Split(f.Path, "/") {
		if vendoredDirs[seg] {
			return "vendored", true
		}
	}
	for _, pattern := range c.generated {
		if Match(pattern, f.Path) {
			return "generated", true
		}
	}
	if hasLine(f.Patch, generatedLine.MatchString) {
		return "generated", true
	}
	return "", false
}

func hasLine(s string, pred func(string) bool) bool {
	for _, l := range strings.Split(s, "\n") {
		if pred(strings.TrimRight(l, "\r")) {
			return true
		}
	}
	return false
}

// EstimateTokens roughly estimates the token count of s at four bytes per token.
func EstimateTokens(s string) int {
	return (len(s) + 3) / 4
}
