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
// A review must not stop for this. A link that cannot be removed, or a
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
	found, err := checkout.Scan(root)
	if err != nil {
		logger.Printf("warning: looking for symlinks that leave the checkout: %v", err)
	}
	for _, rel := range found.Unreadable {
		logger.Printf("warning: could not look into the directory %q for symlinks that leave the checkout", rel)
	}
	for _, l := range found.Outside {
		// os.Remove unlinks the symlink itself, never its target.
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(l.Path))); err != nil {
			logger.Printf("warning: could not delete symlink %q -> %q, which leaves the checkout: %v", l.Path, l.Target, err)
			continue
		}
		logger.Printf("deleted symlink %q -> %q: it leaves the checkout", l.Path, l.Target)
	}
}
