// Package scope holds the file-selection rules of a review: the glob matcher,
// the default exclusions (lockfiles, vendored directories, generated files)
// and how one skill's paths and exclude patterns pick files. It imports no
// pergent internal package, so other programs can apply the same rules.
package scope

import (
	"bufio"
	"bytes"
	"path"
	"regexp"
	"strings"
)

// The rule names reported for a skipped file.
const (
	RuleLockfile        = "lockfile"
	RuleVendored        = "vendored"
	RuleGenerated       = "generated"
	RuleOutOfScope      = "out of scope"
	RuleExcludedBySkill = "excluded by skill"
)

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

// generatedHeader matches the content of a line (without its diff prefix)
// that is a comment saying the file is generated.
var generatedHeader = regexp.MustCompile(`(?i)^\s*(?://|#|/\*|\*|--|<!--|;).*code generated .*do not edit`)

// generatedHeaderLines is how many lines at the top of a file may hold the
// generated-file comment.
const generatedHeaderLines = 10

// Attributes holds the patterns a .gitattributes file marks linguist-generated.
// The zero value has none.
type Attributes struct {
	generated []string
}

// ParseAttributes reads the patterns marked linguist-generated (or
// linguist-generated=true) from .gitattributes bytes.
func ParseAttributes(gitattributes []byte) Attributes {
	var a Attributes
	sc := bufio.NewScanner(bytes.NewReader(gitattributes))
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
				a.generated = append(a.generated, pattern)
				break
			}
		}
	}
	return a
}

// PathRule returns the default exclusion that p's path alone triggers, in the
// order lockfile, vendored, generated (by attribute), or "" when none does.
// Every one of them is opt-in: a skill's paths may override it.
func (a Attributes) PathRule(p string) string {
	if lockfiles[path.Base(p)] {
		return RuleLockfile
	}
	for _, seg := range strings.Split(p, "/") {
		if vendoredDirs[seg] {
			return RuleVendored
		}
	}
	for _, pattern := range a.generated {
		if Match(pattern, p) {
			return RuleGenerated
		}
	}
	return ""
}

// HasGeneratedHeader reports whether content has the generated-file comment
// within its first 10 lines.
func HasGeneratedHeader(content string) bool {
	for i, l := range strings.Split(content, "\n") {
		if i >= generatedHeaderLines {
			break
		}
		if IsGeneratedHeaderLine(strings.TrimRight(l, "\r")) {
			return true
		}
	}
	return false
}

// IsGeneratedHeaderLine reports whether one line of file content (without any
// diff prefix) is the generated-file comment.
func IsGeneratedHeaderLine(line string) bool {
	return generatedHeader.MatchString(line)
}

// Decide applies one skill to one file whose default exclusion is rule. optIn
// says whether the skill's paths may override that rule. It returns the rule
// that skips the file, or "" when the skill reviews it. The order is:
//   - a rule that paths can't override (deleted, binary, ...);
//   - "out of scope" when the skill has paths and none matches;
//   - "excluded by skill" when one of exclude matches;
//   - the overridable rule, unless one of paths matches the file.
func Decide(paths, exclude []string, p, rule string, optIn bool) string {
	if rule != "" && !optIn {
		return rule
	}
	inPaths := MatchAny(paths, p)
	if len(paths) > 0 && !inPaths {
		return RuleOutOfScope
	}
	if MatchAny(exclude, p) {
		return RuleExcludedBySkill
	}
	if rule != "" && !inPaths {
		return rule
	}
	return ""
}
