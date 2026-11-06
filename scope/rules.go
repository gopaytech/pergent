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

// GeneratedHeaderLines is how many lines at the top of a file may hold the
// generated-file comment.
const GeneratedHeaderLines = 10

// Attributes holds what a .gitattributes file says about linguist-generated:
// one entry per line that sets, unsets or resets it, in file order. The zero
// value has none.
type Attributes struct {
	generated []generatedRule
}

// generatedRule is one .gitattributes line that decides linguist-generated
// for the paths matching pattern.
type generatedRule struct {
	pattern string
	// set is true when the line sets the attribute (linguist-generated or
	// linguist-generated=true). It is false when the line unsets it
	// (-linguist-generated, linguist-generated=false, any other value) or
	// resets it to unspecified (!linguist-generated): neither makes a file
	// generated.
	set bool
}

// ParseAttributes reads from .gitattributes bytes which patterns are marked
// linguist-generated (or linguist-generated=true) and which lines unset it
// again (-linguist-generated, linguist-generated=false, !linguist-generated).
// As in git, PathRule lets the last matching line decide, and within one line
// the last token about the attribute decides.
func ParseAttributes(gitattributes []byte) Attributes {
	var a Attributes
	sc := bufio.NewScanner(bytes.NewReader(gitattributes))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 || strings.HasPrefix(fields[0], "#") || strings.HasPrefix(fields[0], "[attr]") {
			continue
		}
		var set, seen bool
		for _, attr := range fields[1:] {
			switch {
			case attr == "linguist-generated", attr == "linguist-generated=true":
				set, seen = true, true
			case attr == "-linguist-generated", attr == "!linguist-generated", strings.HasPrefix(attr, "linguist-generated="):
				set, seen = false, true
			}
		}
		if !seen {
			continue
		}
		pattern := strings.TrimPrefix(fields[0], "/")
		if strings.HasSuffix(pattern, "/") {
			pattern += "**"
		}
		a.generated = append(a.generated, generatedRule{pattern, set})
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
	// The last matching line decides, as in .gitattributes.
	generated := false
	for _, r := range a.generated {
		if Match(r.pattern, p) {
			generated = r.set
		}
	}
	if generated {
		return RuleGenerated
	}
	return ""
}

// HasGeneratedHeader reports whether content has the generated-file comment
// within its first GeneratedHeaderLines lines.
func HasGeneratedHeader(content string) bool {
	for i, l := range strings.Split(content, "\n") {
		if i >= GeneratedHeaderLines {
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
