package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/gopaytech/pergent/internal/checkout"
	"github.com/gopaytech/pergent/internal/redact"
	"github.com/gopaytech/pergent/internal/review"
)

// maxLinked is the most linked repositories a review takes.
const maxLinked = 3

// readLinkedRepos reads the linked repositories the caller cloned: a JSON list
// of {name, path, watched, can_block, error}. The file is a trusted input, so
// one that can't be used is an error and the review stops, rather than the
// links being quietly left out. An entry with an error needs no path: the
// caller could not clone it, and it is neither scrubbed nor read.
func readLinkedRepos(path string, logger *log.Logger) ([]review.LinkedRepo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var repos []review.LinkedRepo
	if err := json.Unmarshal(data, &repos); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(repos) > maxLinked {
		return nil, fmt.Errorf("%d linked repositories, at most %d are allowed", len(repos), maxLinked)
	}
	names := map[string]bool{}
	for i, r := range repos {
		if r.Name == "" {
			return nil, fmt.Errorf("entry %d has no name", i+1)
		}
		if names[r.Name] {
			return nil, fmt.Errorf("two entries are named %q", r.Name)
		}
		names[r.Name] = true
		if r.Error != "" {
			continue
		}
		// A pattern built from the path allows reads below it, so it must
		// not reach further than its own directory.
		switch {
		case r.Path == "" || !filepath.IsAbs(r.Path):
			return nil, fmt.Errorf("%q: path %q is not an absolute path", r.Name, r.Path)
		case filepath.Clean(r.Path) != r.Path || r.Path == string(filepath.Separator):
			return nil, fmt.Errorf("%q: path %q is not a clean path below the root", r.Name, r.Path)
		case strings.ContainsAny(r.Path, "*?[]{}"):
			return nil, fmt.Errorf("%q: path %q has a wildcard character", r.Name, r.Path)
		}
	}
	if len(repos) > 0 {
		logger.Printf("linked repositories: %d", len(repos))
	}
	return repos, nil
}

// linkedOutside refuses a clone that is, or holds, the checkout: the scrub
// would delete the checkout's files, or the impact run would read the
// reviewed repository as if it were a caller. Entries with an error are not
// used, so they are not checked.
func linkedOutside(root string, repos []review.LinkedRepo) error {
	for _, r := range repos {
		if r.Error != "" {
			continue
		}
		resolved, err := resolvePath(r.Path)
		if err != nil {
			return fmt.Errorf("--linked-repos %q: %v", r.Name, err)
		}
		if within(root, resolved) || within(resolved, root) {
			return fmt.Errorf("--linked-repos %q: %s overlaps --repo-path: a linked clone must be a separate directory", r.Name, r.Path)
		}
	}
	return nil
}

// scrubLinked makes each usable clone safe for an impact run to read, and
// returns the entries with an Error set on every one that could not be made
// so. The returned list is a copy. A clone's content is as untrusted as the
// checkout's: its instruction files would be loaded by opencode, a symlink
// could lead out of it, and grep and glob don't honour the read rules, so a
// file they deny has to be gone, not only denied. denied is asked about each
// file's path relative to the clone. The .git directory is left alone, as in
// the checkout scan. An entry that already has an Error is not touched.
func scrubLinked(repos []review.LinkedRepo, denied func(string) bool) []review.LinkedRepo {
	out := make([]review.LinkedRepo, len(repos))
	copy(out, repos)
	for i := range out {
		if out[i].Error != "" {
			continue
		}
		if err := scrubClone(out[i].Path, denied); err != nil {
			out[i].Error = "the clone could not be scrubbed: " + err.Error()
		}
	}
	return out
}

// scrubClone scrubs the one clone at path. File names from the clone are
// quoted in the errors: they are the clone's own text.
func scrubClone(path string, denied func(string) bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		// A link would be followed, and its target scrubbed instead.
		return fmt.Errorf("%s is not a plain directory", path)
	}
	root, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}

	scan, err := checkout.Scan(root, denied)
	if err != nil {
		return err
	}
	if len(scan.Unreadable) > 0 {
		// Whatever is in them might be a file or link the scan did not see.
		return fmt.Errorf("cannot read the directories %s", quoteAll(scan.Unreadable))
	}
	// Instruction files, symlinks to the clone's root, to outside it and to
	// denied files. A name can be in more than one list.
	remove := append([]string(nil), scan.Instructions...)
	remove = append(remove, scan.ToRoot...)
	for _, l := range append(append([]checkout.Link(nil), scan.Outside...), scan.Denied...) {
		remove = append(remove, l.Path)
	}
	for _, rel := range remove {
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("deleting %q: %v", rel, err)
		}
	}

	// The scan finds denied symlinks, not denied files. Symlinks are not
	// followed.
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walking %q: %v", p, err)
		}
		if d.IsDir() {
			if d.Name() == ".git" && p != root {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if denied(rel) {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("deleting %q: %v", rel, err)
			}
		}
		return nil
	})
}

// logLinked says which linked repositories can be used and why the others
// can't.
func logLinked(repos []review.LinkedRepo, logger *log.Logger) {
	for _, r := range repos {
		if r.Error != "" {
			logger.Printf("warning: linked repository %q is not usable: %s", r.Name, redact.String(r.Error))
		}
	}
}
