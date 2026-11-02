package diff

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gopaytech/pergent/scope"
)

// Skip records a file that was left out of a review and why.
type Skip struct {
	Path, Rule string
}

// hunkHeader captures the start line of a hunk's new-file range.
var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// generatedHeaderLines is how many lines of the new side of a hunk starting
// at line 1 may hold the generated-file comment (the same 10 as
// scope.HasGeneratedHeader).
const generatedHeaderLines = 10

// Classifier decides which files every skill skips by default.
type Classifier struct {
	attrs scope.Attributes
	// generatedAtBase, when set, reports whether the base version of a path
	// already carried the generated-file header. A header that a change adds
	// itself then does not skip the file.
	generatedAtBase func(path string) bool
}

// NewClassifier reads <repoPath>/.gitattributes, if present, and keeps the
// patterns marked linguist-generated.
func NewClassifier(repoPath string) Classifier {
	data, _ := os.ReadFile(filepath.Join(repoPath, ".gitattributes"))
	return NewClassifierFrom(data, nil)
}

// NewClassifierFrom builds a Classifier from the given .gitattributes bytes.
// With a non-nil generatedAtBase, a generated-file header in a patch only
// skips the file when generatedAtBase(path) is also true, so a change cannot
// exempt its own code from review by adding the header.
func NewClassifierFrom(gitattributes []byte, generatedAtBase func(path string) bool) Classifier {
	return Classifier{attrs: scope.ParseAttributes(gitattributes), generatedAtBase: generatedAtBase}
}

// Exclusion returns the first rule that applies to f, or "" when none does.
// optIn reports whether a skill's paths may override the rule.
func (c Classifier) Exclusion(f FileDiff) (rule string, optIn bool) {
	if f.Deleted {
		return "deleted", false
	}
	scan := scanPatch(f.Patch)
	// A file whose diff the platform did not return has no hunks to look at,
	// but it did change: only the path rules below may skip it.
	if f.NotReviewed == "" {
		if f.Binary || scan.binary {
			return "binary", false
		}
		if !scan.hasHunk {
			return "no content changes", false
		}
	}
	if rule := c.attrs.PathRule(f.Path); rule != "" {
		return rule, true
	}
	if scan.generatedHeader && (c.generatedAtBase == nil || c.generatedAtBase(f.Path)) {
		return scope.RuleGenerated, true
	}
	return "", false
}

type patchScan struct {
	binary, hasHunk, generatedHeader bool
}

// scanPatch makes one pass over a file's patch. The generated-file header
// only counts when it is an added or context comment line within the first
// generatedHeaderLines lines of a hunk that starts at line 1 of the new file.
func scanPatch(patch string) patchScan {
	var r patchScan
	inTopHunk := false
	newLine := 0
	for _, l := range strings.Split(patch, "\n") {
		l = strings.TrimRight(l, "\r")
		if strings.HasPrefix(l, "@@") {
			r.hasHunk = true
			m := hunkHeader.FindStringSubmatch(l)
			inTopHunk = m != nil && m[1] == "1"
			newLine = 0
			continue
		}
		if !r.hasHunk {
			if strings.HasPrefix(l, "GIT binary patch") ||
				(strings.HasPrefix(l, "Binary files ") && strings.HasSuffix(l, " differ")) {
				r.binary = true
			}
			continue
		}
		if l == "" || (l[0] != '+' && l[0] != ' ') {
			continue // removed lines and "\ No newline" do not advance the new side
		}
		newLine++
		if inTopHunk && newLine <= generatedHeaderLines && scope.IsGeneratedHeaderLine(l[1:]) {
			r.generatedHeader = true
		}
	}
	return r
}

// EstimateTokens roughly estimates the token count of s at four bytes per token.
func EstimateTokens(s string) int {
	return (len(s) + 3) / 4
}
