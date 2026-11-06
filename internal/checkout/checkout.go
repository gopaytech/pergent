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
	// Denied are the symlinks that stay inside the checkout but whose real
	// target the denied function given to Scan rejects.
	Denied []Link
	// Unreadable are the directories Scan could not read, and so did not look
	// into.
	Unreadable []string
}

// Scan walks root, which must have its symlinks resolved. It skips .git
// directories, does not follow symlinked directories, and classifies what it
// finds. The read rules match the name of the path being read, not where a
// link leads, so denied (nil for none) is asked about the real target of every
// link that stays inside, as a slash-separated path relative to root.
func Scan(root string, denied func(rel string) bool) (Found, error) {
	var found Found
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() && path != root {
				// ReadDir failed: whatever is in it is as unreadable to opencode.
				// Go on, so the links after it are still found.
				rel, relErr := filepath.Rel(root, path)
				if relErr != nil {
					return err
				}
				found.Unreadable = append(found.Unreadable, filepath.ToSlash(rel))
				return nil
			}
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
			// Dangling (or looping): judge where the link text leads.
			target = looseTarget(filepath.Dir(path), text)
		}
		switch {
		case target == root:
			found.ToRoot = append(found.ToRoot, rel)
		case !Within(root, target):
			found.Outside = append(found.Outside, Link{Path: rel, Target: text})
		case denied != nil:
			if targetRel, err := filepath.Rel(root, target); err == nil && denied(filepath.ToSlash(targetRel)) {
				found.Denied = append(found.Denied, Link{Path: rel, Target: text})
			}
		}
		return nil
	})
	sort.Strings(found.Instructions)
	sort.Strings(found.ToRoot)
	sort.Strings(found.Unreadable)
	sort.Slice(found.Outside, func(i, j int) bool { return found.Outside[i].Path < found.Outside[j].Path })
	sort.Slice(found.Denied, func(i, j int) bool { return found.Denied[i].Path < found.Denied[j].Path })
	return found, err
}

// looseTarget is where a link with the given text, in the real directory dir,
// leads when its target does not exist. The longest leading part of the path
// that exists is resolved by the file system, so a symlinked directory on the
// way counts and ".." after one is read as the kernel reads it; the rest is
// added as written. Resolving the text alone would call "ldir/../x" inside
// when ldir leads out. A target that cannot be shown to stay inside therefore
// shows up outside.
func looseTarget(dir, text string) string {
	sep := string(filepath.Separator)
	raw := text
	if !filepath.IsAbs(raw) {
		raw = dir + sep + text
	}
	parts := strings.Split(raw, sep)
	for i := len(parts); i > 0; i-- {
		prefix := strings.Join(parts[:i], sep)
		if prefix == "" {
			prefix = sep
		}
		if r, err := filepath.EvalSymlinks(prefix); err == nil {
			return filepath.Join(append([]string{r}, parts[i:]...)...)
		}
	}
	return filepath.Clean(raw)
}

// Remove deletes the symlink l, found under root. It unlinks the link itself,
// never its target.
func Remove(root string, l Link) error {
	return os.Remove(filepath.Join(root, filepath.FromSlash(l.Path)))
}
