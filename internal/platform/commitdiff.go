package platform

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// diffFormat pins the diff output so the user's git config (colour, external
// diff tools, diff.noprefix and the like) can't change what Split parses.
var diffFormat = []string{"--no-color", "--no-ext-diff", "--src-prefix=a/", "--dst-prefix=b/"}

// CommitDiff returns the diff between two commits in repoPath, and the paths
// it changes. It needs no history between them and ignores the working tree,
// except that git attributes are read from base (GIT_ATTR_SOURCE, git 2.40
// and later), so a .gitattributes the change adds can't mark its own files as
// binary to keep them out of the review.
func CommitDiff(repoPath, base, head string) (diff string, files []string, err error) {
	for _, c := range []struct{ name, value string }{{"base", base}, {"head", head}} {
		if c.value == "" {
			return "", nil, fmt.Errorf("%s commit is empty", c.name)
		}
		if strings.HasPrefix(c.value, "-") {
			return "", nil, fmt.Errorf("%s commit %q is not a commit", c.name, c.value)
		}
	}

	run := func(extra ...string) ([]byte, error) {
		args := append([]string{"-C", repoPath, "diff"}, extra...)
		args = append(append(args, diffFormat...), base, head)
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(), "GIT_ATTR_SOURCE="+base)
		return cmd.Output()
	}

	out, err := run()
	if err != nil {
		return "", nil, fmt.Errorf("git diff %s %s failed: %w", base, head, gitError(err))
	}
	names, err := run("--name-only")
	if err != nil {
		return "", nil, fmt.Errorf("git diff --name-only %s %s failed: %w", base, head, gitError(err))
	}

	files = strings.Split(strings.TrimSpace(string(names)), "\n")
	if len(files) == 1 && files[0] == "" {
		files = nil
	}
	return string(out), files, nil
}

// gitError adds git's stderr, when it has any, to an exec error.
func gitError(err error) error {
	if ee, ok := err.(*exec.ExitError); ok {
		if msg := strings.TrimSpace(string(ee.Stderr)); msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
	}
	return err
}
