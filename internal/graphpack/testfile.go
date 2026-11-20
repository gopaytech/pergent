package graphpack

import (
	"path"
	"strings"
)

// testDirs are the folder names that hold tests. A path with one of them as
// a folder (not the file name) is a test file. Names match as written.
var testDirs = map[string]bool{"test": true, "tests": true, "__tests__": true}

// testExts are the JavaScript and TypeScript extensions of *.test.<ext> and
// *.spec.<ext> files.
var testExts = map[string]bool{".js": true, ".jsx": true, ".ts": true, ".tsx": true}

// isTestFile reports whether p, a slash-separated path in the repository, is
// a test file by its name alone: Go *_test.go; Python test_*.py and
// *_test.py; *.test.<ext> and *.spec.<ext> for js, jsx, ts and tsx; or any
// file under a test, tests or __tests__ folder. The hint lists callers in
// test files after the others, so this only changes an order, never what is
// listed.
func isTestFile(p string) bool {
	dir, base := path.Split(p)
	for _, d := range strings.Split(dir, "/") {
		if testDirs[d] {
			return true
		}
	}
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	switch {
	case ext == ".go":
		return strings.HasSuffix(stem, "_test")
	case ext == ".py":
		return strings.HasPrefix(stem, "test_") || strings.HasSuffix(stem, "_test")
	case testExts[ext]:
		return strings.HasSuffix(stem, ".test") || strings.HasSuffix(stem, ".spec")
	}
	return false
}
