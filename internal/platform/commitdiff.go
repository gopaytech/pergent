package platform

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/gopaytech/pergent/internal/diff"
)

// diffFormat pins the diff output so the user's git config (colour, external
// diff tools, diff.noprefix and the like) can't change what Split parses.
var diffFormat = []string{"--no-color", "--no-ext-diff", "--src-prefix=a/", "--dst-prefix=b/"}

// CommitDiff returns the diff between two commits in repoPath, and the paths
// it changes. It needs no history between them and ignores the working tree,
// except that git attributes are read from base (GIT_ATTR_SOURCE, git 2.40
// and later), so a .gitattributes the change adds can't mark its own files as
// binary to keep them out of the review.
func CommitDiff(repoPath, base, head string) (unified string, files []string, err error) {
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

	// --name-only C-quotes unusual paths ("\303\251.go"); undo it as Split
	// does, so Files and the split diff name files the same way. Spaces are
	// not quoted, so only the newline after the last name is dropped.
	lines := strings.Split(string(names), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for _, name := range lines {
		files = append(files, diff.Unquote(name))
	}
	return string(out), files, nil
}

// GitVersion runs `git version` and parses it with ParseGitVersion.
func GitVersion() (version string, major, minor int, err error) {
	out, err := exec.Command("git", "version").Output()
	if err != nil {
		return "", 0, 0, fmt.Errorf("git version: %w", gitError(err))
	}
	return ParseGitVersion(string(out))
}

// ParseGitVersion reads the output of `git version`, such as "git version
// 2.39.5 (Apple Git-154)" or "git version 2.47.1.windows.1". version is the
// version word ("2.39.5", "2.47.1.windows.1").
func ParseGitVersion(out string) (version string, major, minor int, err error) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(out), "git version ")
	fields := strings.Fields(rest)
	if !ok || len(fields) == 0 {
		return "", 0, 0, fmt.Errorf("unrecognised git version output %q", out)
	}
	version = fields[0]
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return "", 0, 0, fmt.Errorf("unrecognised git version %q", version)
	}
	if major, err = strconv.Atoi(parts[0]); err == nil {
		minor, err = strconv.Atoi(parts[1])
	}
	if err != nil {
		return "", 0, 0, fmt.Errorf("unrecognised git version %q", version)
	}
	return version, major, minor, nil
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
