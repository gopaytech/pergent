// Package diff splits unified diffs into per-file diffs and classifies the
// files that reviews skip by default.
package diff

import (
	"strconv"
	"strings"
)

// FileDiff is the diff of a single file. Patch is the full text from the
// file's "diff --git" line up to the next one.
type FileDiff struct {
	Path    string
	OldPath string
	Patch   string
	Deleted bool
	Binary  bool
	// NotReviewed, when set, says why the file's diff is missing: the
	// platform did not return it. Such a file must be reported as not
	// reviewed, never skipped as having no content changes.
	NotReviewed string
}

const gitHeader = "diff --git "

// NotReviewedHeader starts an extended header line, written by pergent
// (never by git), saying why the platform did not return the file's diff.
// The rest of the line is the reason; Split stores it in FileDiff.NotReviewed.
const NotReviewedHeader = "pergent-not-reviewed: "

// Split splits a unified diff (as produced by git or a platform API) into
// one FileDiff per file, in order.
func Split(unified string) []FileDiff {
	var files []FileDiff
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			files = append(files, parseFile(cur))
			cur = nil
		}
	}
	for _, line := range strings.SplitAfter(unified, "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, gitHeader) {
			flush()
		}
		if cur == nil && !strings.HasPrefix(line, gitHeader) {
			continue // preamble before the first file
		}
		cur = append(cur, line)
	}
	flush()
	return files
}

func parseFile(lines []string) FileDiff {
	f := FileDiff{Patch: strings.Join(lines, "")}
	hdrOld, hdrNew := parseGitLine(strings.TrimRight(lines[0], "\r\n"))
	var renameFrom, renameTo, minus, plus string
	var hasMinus, hasPlus bool

	// Extended headers end at the first hunk; never read paths from hunk bodies.
	for _, raw := range lines[1:] {
		line := strings.TrimRight(raw, "\r\n")
		if strings.HasPrefix(line, "@@") {
			break
		}
		switch {
		case strings.HasPrefix(line, NotReviewedHeader):
			f.NotReviewed = strings.TrimSpace(strings.TrimPrefix(line, NotReviewedHeader))
		case strings.HasPrefix(line, "deleted file mode"):
			f.Deleted = true
		case strings.HasPrefix(line, "Binary files ") && strings.HasSuffix(line, " differ"),
			strings.HasPrefix(line, "GIT binary patch"):
			f.Binary = true
		case strings.HasPrefix(line, "rename from "):
			renameFrom = unquote(strings.TrimPrefix(line, "rename from "))
		case strings.HasPrefix(line, "rename to "):
			renameTo = unquote(strings.TrimPrefix(line, "rename to "))
		case strings.HasPrefix(line, "copy from "):
			renameFrom = unquote(strings.TrimPrefix(line, "copy from "))
		case strings.HasPrefix(line, "copy to "):
			renameTo = unquote(strings.TrimPrefix(line, "copy to "))
		case strings.HasPrefix(line, "--- "):
			minus, hasMinus = headerPath(strings.TrimPrefix(line, "--- "), "a/")
		case strings.HasPrefix(line, "+++ "):
			plus, hasPlus = headerPath(strings.TrimPrefix(line, "+++ "), "b/")
		}
	}

	switch {
	case renameTo != "":
		f.Path = renameTo
	case hasPlus && plus != "":
		f.Path = plus
	case hasMinus && minus != "":
		f.Path = minus
	case hdrNew != "":
		f.Path = hdrNew
	default:
		f.Path = hdrOld
	}

	switch {
	case renameFrom != "":
		f.OldPath = renameFrom
	case hasMinus && minus != "" && minus != f.Path:
		f.OldPath = minus
	}
	return f
}

// headerPath parses the path of a "--- " or "+++ " line. ok is false when
// the line is /dev/null or unparsable.
func headerPath(s, prefix string) (string, bool) {
	s = strings.TrimRight(s, "\t ")
	if s == "/dev/null" {
		return "", false
	}
	p := unquote(s)
	if !strings.HasPrefix(p, prefix) {
		return p, p != ""
	}
	return strings.TrimPrefix(p, prefix), true
}

// parseGitLine extracts the old and new path from a "diff --git a/X b/Y"
// line. It is a fallback for diffs without ---/+++ lines (binary, rename,
// mode change) and copes with git's quoting.
func parseGitLine(line string) (oldPath, newPath string) {
	rest := strings.TrimPrefix(line, gitHeader)

	if strings.HasPrefix(rest, `"`) {
		first, after, ok := cutQuoted(rest)
		if !ok {
			return "", ""
		}
		after = strings.TrimPrefix(after, " ")
		var second string
		if strings.HasPrefix(after, `"`) {
			second, _, _ = cutQuoted(after)
			second = strings.TrimPrefix(unquote(`"`+second+`"`), "b/")
			return strings.TrimPrefix(unquote(`"`+first+`"`), "a/"), second
		}
		return strings.TrimPrefix(unquote(`"`+first+`"`), "a/"), strings.TrimPrefix(after, "b/")
	}

	// Unquoted: "a/P b/P" for modifications, which may contain spaces.
	if n := len(rest) - 5; n > 0 && n%2 == 0 {
		half := n / 2
		if rest[:2] == "a/" && rest[2+half:2+half+3] == " b/" && rest[2:2+half] == rest[5+half:] {
			return rest[2 : 2+half], rest[2 : 2+half]
		}
	}
	if i := strings.Index(rest, ` "b/`); i >= 0 {
		return strings.TrimPrefix(rest[:i], "a/"), strings.TrimPrefix(unquote(rest[i+1:]), "b/")
	}
	if i := strings.Index(rest, " b/"); i >= 0 {
		return strings.TrimPrefix(rest[:i], "a/"), rest[i+3:]
	}
	return "", ""
}

// cutQuoted reads one leading double-quoted token and returns its contents
// (still escaped, without the quotes) and the remainder.
func cutQuoted(s string) (inner, rest string, ok bool) {
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			return s[1:i], s[i+1:], true
		}
	}
	return "", "", false
}

// Unquote undoes git's C-style path quoting, as Split does for the paths it
// reads; unquoted input is returned as is.
func Unquote(s string) string { return unquote(s) }

// unquote undoes git's C-style path quoting; unquoted input is returned as is.
func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
	}
	return s
}
