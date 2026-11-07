package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gopaytech/pergent/internal/checkout"
	"github.com/gopaytech/pergent/internal/redact"
	"github.com/gopaytech/pergent/internal/review"
)

// maxLinked is the most linked repositories a review takes.
const maxLinked = 3

// readLinkedRepos reads the linked repositories the caller cloned: a JSON list
// of {name, path, watched, can_block, error}. A file that can't be read as a
// whole (unreadable, not a list, too many entries, an entry with no name or
// two with one name) is an error: the caller makes it one failed entry
// (failedLinks), so the check never reads as clean. An entry whose path can't
// be used keeps its place with an Error, so the repository it names is told
// apart from the others. An entry with an error needs no path: the caller
// could not clone it, and it is neither scrubbed nor read.
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
			repos[i].Error = "the clone's path is not an absolute path"
		case filepath.Clean(r.Path) != r.Path || r.Path == string(filepath.Separator):
			repos[i].Error = "the clone's path is not a clean path below the root"
		case strings.ContainsAny(r.Path, "*?[]{}"):
			repos[i].Error = "the clone's path has a wildcard character"
		}
		if repos[i].Error != "" {
			logger.Printf("warning: --linked-repos %q: %s: %q", r.Name, repos[i].Error, r.Path)
		}
	}
	if len(repos) > 0 {
		logger.Printf("linked repositories: %d", len(repos))
	}
	return repos, nil
}

// failedLinks is what a linked-repos file that can't be used becomes: one
// entry with no name and an Error, so the impact check is failed and the
// skills still run. The details go to the log, not into the entry.
func failedLinks(path string, err error, logger *log.Logger) []review.LinkedRepo {
	logger.Printf("warning: --linked-repos %s: %v", path, err)
	return []review.LinkedRepo{{Error: "the linked repositories file could not be used"}}
}

// systemDirs are directories no clone may be in: the scrub would delete files
// there, or the clone's read rule would reach a credential.
var systemDirs = []string{"/etc", "/proc", "/sys", "/dev", "/run/secrets", "/var/run/secrets"}

// linkedOutside checks where the clones are. A clone that is, or holds, the
// checkout is a usage error: the scrub would delete the checkout's files, or
// the impact run would read the reviewed repository as if it were a caller.
// A clone whose path would widen the sandbox is not: its entry gets an Error
// (so the check is failed, the review goes on) and is neither scrubbed nor
// read. Those are a path that is, or holds, the temp directory, $HOME or
// $XDG_CONFIG_HOME (the read rule for a clone reaches everything below it,
// auth.json of a run's data directory included), one that overlaps a trusted
// input or the result file (the scrub would delete it), and one in a system
// directory. The returned list is a copy. Entries with an error are not used,
// so they are not checked.
func linkedOutside(root string, repos []review.LinkedRepo, in *agentInputs, logger *log.Logger) ([]review.LinkedRepo, error) {
	out := make([]review.LinkedRepo, len(repos))
	copy(out, repos)

	// What a clone may not be, or hold.
	var holds []string
	for _, p := range append([]string{os.TempDir(), in.home, in.configHome}, "/") {
		if p != "" {
			holds = append(holds, p)
		}
	}
	// What a clone may not overlap, either way.
	overlaps := map[string]string{}
	for _, i := range trustedInputs(in) {
		overlaps[i.path] = i.flag
	}
	if p := in.Target.Path; p != "" && p != "-" {
		overlaps[p] = "--result"
	}

	for i, r := range out {
		if r.Error != "" {
			continue
		}
		resolved, err := resolvePath(r.Path)
		if err != nil {
			logger.Printf("warning: --linked-repos %q: %s: %v", r.Name, "the clone's path could not be resolved", plainErr(err))
			out[i].Error = "the clone's path could not be resolved"
			continue
		}
		if within(root, resolved) || within(resolved, root) {
			return nil, fmt.Errorf("--linked-repos %q: %s overlaps --repo-path: a linked clone must be a separate directory", r.Name, r.Path)
		}
		why := ""
		for _, p := range holds {
			if q, err := resolvePath(p); err == nil && within(resolved, q) {
				why = "it is or holds " + p
			}
		}
		for p, flag := range overlaps {
			if q, err := resolvePath(p); err == nil && (within(resolved, q) || within(q, resolved)) {
				why = "it overlaps " + flag
			}
		}
		for _, p := range systemDirs {
			if q, err := resolvePath(p); err == nil && within(q, resolved) {
				why = "it is in " + p
			}
		}
		if why != "" {
			logger.Printf("warning: --linked-repos %q: %s is refused as a clone: %s", r.Name, strconv.Quote(r.Path), why)
			out[i].Error = "the clone's path is not allowed"
		}
	}
	return out, nil
}

// plainErr is err's text without the path of a *fs.PathError or *os.LinkError:
// the path is a name from a clone, and a line break in it would start a log
// line of its own.
func plainErr(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Op + ": " + pe.Err.Error()
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return le.Op + ": " + le.Err.Error()
	}
	return err.Error()
}

// scrubLinked makes each usable clone safe for an impact run to read, and
// returns the entries with an Error set on every one that could not be made
// so. The returned list is a copy. A clone's content is as untrusted as the
// checkout's: its instruction files would be loaded by opencode, a symlink
// could lead out of it, and grep and glob don't honour the read rules, so a
// file they deny has to be gone, not only denied. denied is asked about each
// file's path relative to the clone. The .git directory is left alone, as in
// the checkout scan. An entry that already has an Error is not touched.
//
// An entry's Error is fixed text: the names of the clone's files, which the
// clone controls, are in the log only.
func scrubLinked(repos []review.LinkedRepo, denied func(string) bool, logger *log.Logger) []review.LinkedRepo {
	out := make([]review.LinkedRepo, len(repos))
	copy(out, repos)
	for i := range out {
		if out[i].Error != "" {
			continue
		}
		if err := scrubClone(out[i].Path, denied); err != nil {
			logger.Printf("warning: linked repository %q: the clone could not be scrubbed: %v", out[i].Name, err)
			out[i].Error = "the clone could not be scrubbed"
		}
	}
	return out
}

// scrubClone scrubs the one clone at path. Its errors are for the log: they
// hold names from the clone, quoted, and never a path as the file system
// prints it.
func scrubClone(path string, denied func(string) bool) error {
	// The read rules are written in lower case and opencode matches them
	// case-sensitively, so `.ENV` is not denied there; on a case-insensitive
	// file system it is the same file as `.env`. Here every name is judged in
	// lower case, so any case of a denied name goes.
	deniedAnyCase := func(rel string) bool { return denied(strings.ToLower(rel)) }

	info, err := os.Lstat(path)
	if err != nil {
		return errors.New(plainErr(err))
	}
	if !info.IsDir() {
		// A link would be followed, and its target scrubbed instead.
		return fmt.Errorf("%q is not a plain directory", path)
	}
	root, err := filepath.EvalSymlinks(path)
	if err != nil {
		return errors.New(plainErr(err))
	}

	scan, err := checkout.Scan(root, deniedAnyCase)
	if err != nil {
		return errors.New(plainErr(err))
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
			return fmt.Errorf("deleting %q: %s", rel, plainErr(err))
		}
	}

	// The scan finds denied symlinks, not denied files. Symlinks are not
	// followed. Only the clone's own .git is skipped: a .git inside a
	// directory is a name like any other.
	gitDir := filepath.Join(root, ".git")
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walking %q: %s", p, plainErr(err))
		}
		if d.IsDir() {
			if p == gitDir {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return errors.New(plainErr(err))
		}
		rel = filepath.ToSlash(rel)
		// Scan skips every .git, so an instruction file in a nested one is
		// removed here too.
		if deniedAnyCase(rel) || checkout.IsInstructionName(d.Name()) {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("deleting %q: %s", rel, plainErr(err))
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
