package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/gopaytech/pergent/internal/checkout"
	"github.com/gopaytech/pergent/internal/config"
)

// scrubCheckout removes, in platform CI mode, every symlink in the checkout
// that leaves it, and logs each one. opencode's path checks are lexical: the
// read tool checks the link's own path and then reads through it, so a link
// to /etc/passwd or to a file outside the checkout would be readable. The
// links are deleted, never their targets, and .git is skipped.
//
// The read rules match the name being read too, not where a link leads, so a
// link inside the checkout to a file they deny ("safe.txt -> prod.env") goes
// the same way. CI mode's rules deny *.env files only, so the extra secret
// files agent mode denies (*.pem, *.key, *.tfvars) are not checked here.
//
// opencode's grep and glob don't apply the read rules at all (grep searches
// hidden files too), so a file the rules deny is deleted as well, judged in
// lower case (checkout.DeniedFiles). The diff compares commits, so the change
// to such a file is still reviewed.
//
// A review must not stop for this. A link or file that cannot be removed, or a
// directory that cannot be read, is a warning and the rest is still scrubbed.
//
// --local never deletes anything: it is the user's own working tree, and it
// trusts the checkout.
func scrubCheckout(cfg config.Config, logger *log.Logger) {
	if cfg.Local {
		return
	}
	abs, err := filepath.Abs(cfg.RepoPath)
	if err != nil {
		logger.Printf("warning: not looking for symlinks that leave the checkout: %v", err)
		return
	}
	root, err := filepath.EvalSymlinks(abs)
	if err != nil {
		logger.Printf("warning: not looking for symlinks that leave the checkout: %v", err)
		return
	}
	found, err := checkout.Scan(root, func(rel string) bool { return config.ReadDenied(rel, false) })
	if err != nil {
		logger.Printf("warning: looking for symlinks that leave the checkout: %v", err)
	}
	for _, rel := range found.Unreadable {
		logger.Printf("warning: could not look into the directory %q for symlinks that leave the checkout or files the read rules deny", rel)
	}
	remove := func(l checkout.Link, why string) {
		if err := checkout.Remove(root, l); err != nil {
			logger.Printf("warning: could not delete symlink %q -> %q (%s): %v", l.Path, l.Target, why, err)
			return
		}
		logger.Printf("deleted symlink %q -> %q: %s", l.Path, l.Target, why)
	}
	for _, l := range found.Outside {
		remove(l, "it leaves the checkout")
	}
	for _, l := range found.Denied {
		remove(l, "its target is a file the read rules deny")
	}

	// Scan has already warned about the directories it could not read.
	files, _, err := checkout.DeniedFiles(root, func(rel string) bool { return config.ReadDenied(rel, false) })
	if err != nil {
		logger.Printf("warning: looking for files the read rules deny: %v", err)
	}
	for _, rel := range files {
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil && !os.IsNotExist(err) {
			logger.Printf("warning: could not delete %q, a file the read rules deny: %v", rel, err)
			continue
		}
		logger.Printf("deleted %q: the read rules deny it", rel)
	}
}
