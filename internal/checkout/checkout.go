// Package checkout finds what in a checkout lets opencode read beyond what
// the review is meant to see: instruction files it loads by name, and symlinks
// that reach outside the checkout. opencode's path checks are lexical, so a
// link passes them and is then read through. Both CI mode and agent mode use
// it; each decides what to delete or refuse.
package checkout

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// instructionNames are the file names opencode loads as instructions from
// any directory it reads in, compared case-insensitively.
var instructionNames = []string{"AGENTS.md", "CLAUDE.md", "CONTEXT.md"}

// IsInstructionName reports whether name is a file opencode loads as
// instructions.
func IsInstructionName(name string) bool {
	for _, n := range instructionNames {
		if strings.EqualFold(name, n) {
			return true
		}
	}
	return false
}

// Within reports whether path is dir or below it. Both must be clean and
// absolute.
func Within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Link is a symlink in the checkout. Path is slash-separated and relative to
// the root; Target is the link's own text.
type Link struct{ Path, Target string }

// Found is what Scan finds. Paths are slash-separated, relative to the root,
// and sorted.
type Found struct {
	// Instructions are the non-directory entries named like an instruction
	// file, symlinks so named included.
	Instructions []string
	// ToRoot are the symlinks that resolve to the root itself.
	ToRoot []string
	// Outside are the symlinks whose target is neither the root nor below it.
	Outside []Link
}

// Scan walks root, which must have its symlinks resolved. It skips .git
// directories, does not follow symlinked directories, and classifies what it
// finds.
func Scan(root string) (Found, error) {
	var found Found
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if IsInstructionName(d.Name()) {
			found.Instructions = append(found.Instructions, rel)
		}
		if d.Type()&fs.ModeSymlink == 0 {
			return nil
		}
		text, err := os.Readlink(path)
		if err != nil {
			return err
		}
		target, err := filepath.EvalSymlinks(path)
		if err != nil {
			// Dangling (or looping): judge where the link text points.
			target = text
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(path), target)
			}
			target = filepath.Clean(target)
		}
		switch {
		case target == root:
			found.ToRoot = append(found.ToRoot, rel)
		case !Within(root, target):
			found.Outside = append(found.Outside, Link{Path: rel, Target: text})
		}
		return nil
	})
	sort.Strings(found.Instructions)
	sort.Strings(found.ToRoot)
	sort.Slice(found.Outside, func(i, j int) bool { return found.Outside[i].Path < found.Outside[j].Path })
	return found, err
}
