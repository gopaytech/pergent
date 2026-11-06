package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gopaytech/pergent/internal/config"
)

// ciCheckout is a plain directory standing in for a CI checkout: a file, a
// subdirectory, and a secret outside it.
func ciCheckout(t *testing.T) (root, outsideFile string) {
	t.Helper()
	root = t.TempDir()
	writeFile(t, root, "a.go", "package a\n")
	writeFile(t, root, "sub/b.go", "package b\n")
	outsideFile = filepath.Join(t.TempDir(), "secret.txt")
	writeFile(t, filepath.Dir(outsideFile), "secret.txt", "secret\n")
	return root, outsideFile
}

func symlinkAt(t *testing.T, root, name, target string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, p); err != nil {
		t.Fatal(err)
	}
}

func exists(root, name string) bool {
	_, err := os.Lstat(filepath.Join(root, filepath.FromSlash(name)))
	return err == nil
}

func TestScrubCheckout_PlatformModeRemovesLinksOutOfTheCheckout(t *testing.T) {
	for _, platform := range []string{"github", "gitlab"} {
		t.Run(platform, func(t *testing.T) {
			root, outsideFile := ciCheckout(t)
			links := map[string]string{
				"up":         "..",
				"abs":        outsideFile,
				"sub/gone":   "../../nope",
				"sub/escape": "../../" + filepath.Base(filepath.Dir(outsideFile)),
			}
			for name, target := range links {
				symlinkAt(t, root, name, target)
			}
			// Inside links, dangling inside links and links under .git stay.
			symlinkAt(t, root, "inlink", "a.go")
			symlinkAt(t, root, "dangle", "missing.go")
			symlinkAt(t, root, "dir-link", "sub")
			symlinkAt(t, root, ".git/hook-link", outsideFile)

			logger, logs := bufferLog()
			scrubCheckout(config.Config{Platform: platform, RepoPath: root}, logger)

			for name, target := range links {
				if exists(root, name) {
					t.Errorf("symlink %s still exists, want it removed", name)
				}
				if want := "symlink " + strconv.Quote(name) + " -> " + strconv.Quote(target); !strings.Contains(logs.String(), want) {
					t.Errorf("log lacks %q:\n%s", want, logs)
				}
			}
			for _, name := range []string{"inlink", "dangle", "dir-link", ".git/hook-link", "a.go", "sub/b.go"} {
				if !exists(root, name) {
					t.Errorf("%s was removed, want it kept", name)
				}
			}
			if _, err := os.Stat(outsideFile); err != nil {
				t.Errorf("the link's target was removed: %v", err)
			}
		})
	}
}

// --local is the user's own working tree: nothing in it is ever deleted.
func TestScrubCheckout_LocalModeDeletesNothing(t *testing.T) {
	root, outsideFile := ciCheckout(t)
	symlinkAt(t, root, "abs", outsideFile)
	symlinkAt(t, root, "up", "..")

	logger, logs := bufferLog()
	scrubCheckout(config.Config{Local: true, RepoPath: root}, logger)

	for _, name := range []string{"abs", "up"} {
		if !exists(root, name) {
			t.Errorf("symlink %s was removed in --local mode", name)
		}
	}
	if logs.Len() != 0 {
		t.Errorf("log = %q, want nothing", logs)
	}
}

func TestScrubCheckout_RepoRootThroughASymlink(t *testing.T) {
	root, outsideFile := ciCheckout(t)
	symlinkAt(t, root, "abs", outsideFile)
	symlinkAt(t, root, "inlink", "a.go")
	via := filepath.Join(t.TempDir(), "checkout")
	if err := os.Symlink(root, via); err != nil {
		t.Fatal(err)
	}

	scrubCheckout(config.Config{Platform: "github", RepoPath: via}, quietLog)

	if exists(root, "abs") {
		t.Error("the outside symlink still exists")
	}
	if !exists(root, "inlink") {
		t.Error("the inside symlink was removed")
	}
}

// A link that cannot be removed, or a directory that cannot be read, is a
// warning: the review goes on and the rest is still scrubbed.
func TestScrubCheckout_ProblemsAreWarningsAndTheRestIsScrubbed(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root removes and reads anything")
	}
	root, outsideFile := ciCheckout(t)
	symlinkAt(t, root, "kept/abs", outsideFile)
	symlinkAt(t, root, "z-abs", outsideFile)
	symlinkAt(t, root, "zz/abs", outsideFile)
	if err := os.Mkdir(filepath.Join(root, "locked"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, "locked"), 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, "kept"), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		os.Chmod(filepath.Join(root, "locked"), 0o755)
		os.Chmod(filepath.Join(root, "kept"), 0o755)
	})

	logger, logs := bufferLog()
	scrubCheckout(config.Config{Platform: "gitlab", RepoPath: root}, logger)

	for _, name := range []string{"z-abs", "zz/abs"} {
		if exists(root, name) {
			t.Errorf("symlink %s still exists, want it removed", name)
		}
	}
	if !exists(root, "kept/abs") {
		t.Error("kept/abs was removed from a read-only directory")
	}
	for _, want := range []string{`"kept/abs"`, `"locked"`} {
		if !strings.Contains(logs.String(), "warning") || !strings.Contains(logs.String(), want) {
			t.Errorf("log lacks a warning naming %s:\n%s", want, logs)
		}
	}
}

func TestScrubCheckout_MissingRepoPathIsAWarning(t *testing.T) {
	logger, logs := bufferLog()
	scrubCheckout(config.Config{Platform: "github", RepoPath: filepath.Join(t.TempDir(), "nope")}, logger)
	if !strings.Contains(logs.String(), "warning") {
		t.Errorf("log = %q, want a warning", logs)
	}
}
