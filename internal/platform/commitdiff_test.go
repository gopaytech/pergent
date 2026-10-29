package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// git runs git in dir with a fixed identity and no user or system config.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeRepoFile(t *testing.T, dir, path, content string) {
	t.Helper()
	full := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commitRepo(t *testing.T, dir, msg string) string {
	t.Helper()
	gitOut(t, dir, "add", "-A")
	gitOut(t, dir, "commit", "-q", "--no-gpg-sign", "-m", msg)
	return gitOut(t, dir, "rev-parse", "HEAD")
}

func TestCommitDiff(t *testing.T) {
	dir := t.TempDir()
	gitOut(t, dir, "init", "-q")
	writeRepoFile(t, dir, "a.go", "package a\n")
	base := commitRepo(t, dir, "base")
	writeRepoFile(t, dir, "a.go", "package a\n\nfunc A() {}\n")
	head := commitRepo(t, dir, "head")
	// The working tree is irrelevant: the diff is between the two commits.
	gitOut(t, dir, "checkout", "-q", base)

	d, files, err := CommitDiff(dir, base, head)
	if err != nil {
		t.Fatalf("CommitDiff() error: %v", err)
	}
	if !strings.Contains(d, "diff --git a/a.go b/a.go") || !strings.Contains(d, "+func A() {}") {
		t.Errorf("CommitDiff() diff = %q, want the change to a.go", d)
	}
	if len(files) != 1 || files[0] != "a.go" {
		t.Errorf("CommitDiff() files = %v, want [a.go]", files)
	}
}

// git doesn't quote spaces in --name-only output, so a name's own leading
// or trailing spaces must survive.
func TestCommitDiff_NamesWithEdgeSpaces(t *testing.T) {
	dir := t.TempDir()
	gitOut(t, dir, "init", "-q")
	writeRepoFile(t, dir, "keep.go", "package a\n")
	base := commitRepo(t, dir, "base")
	writeRepoFile(t, dir, " lead.go", "package a\n")
	writeRepoFile(t, dir, "trail.go ", "package a\n")
	head := commitRepo(t, dir, "head")

	_, files, err := CommitDiff(dir, base, head)
	if err != nil {
		t.Fatalf("CommitDiff() error: %v", err)
	}
	if want := []string{" lead.go", "trail.go "}; !slices.Equal(files, want) {
		t.Errorf("CommitDiff() files = %q, want %q", files, want)
	}
}

func TestCommitDiff_IgnoresUserDiffConfig(t *testing.T) {
	dir := t.TempDir()
	gitOut(t, dir, "init", "-q")
	writeRepoFile(t, dir, "a.go", "package a\n")
	base := commitRepo(t, dir, "base")
	writeRepoFile(t, dir, "a.go", "package b\n")
	head := commitRepo(t, dir, "head")
	gitOut(t, dir, "config", "diff.noprefix", "true")
	gitOut(t, dir, "config", "color.diff", "always")
	gitOut(t, dir, "config", "diff.external", "false")
	gitOut(t, dir, "config", "diff.mnemonicPrefix", "true")

	d, _, err := CommitDiff(dir, base, head)
	if err != nil {
		t.Fatalf("CommitDiff() error: %v", err)
	}
	if !strings.Contains(d, "diff --git a/a.go b/a.go") || strings.Contains(d, "\x1b[") {
		t.Errorf("CommitDiff() diff = %q, want the fixed format", d)
	}
}

// A change cannot hide its own files from review by marking them as binary
// in a .gitattributes it adds: the diff reads attributes from the base.
func TestCommitDiff_AttributesFromBase(t *testing.T) {
	if version, major, minor, err := GitVersion(); err != nil || major < 2 || (major == 2 && minor < 40) {
		t.Skipf("git %s (err %v) is older than 2.40 and ignores GIT_ATTR_SOURCE; agent mode refuses to run on it", version, err)
	}
	dir := t.TempDir()
	gitOut(t, dir, "init", "-q")
	writeRepoFile(t, dir, "README", "x\n")
	base := commitRepo(t, dir, "base")
	writeRepoFile(t, dir, ".gitattributes", "*.go -diff\n")
	writeRepoFile(t, dir, "a.go", "package a\n")
	head := commitRepo(t, dir, "head")

	d, _, err := CommitDiff(dir, base, head)
	if err != nil {
		t.Fatalf("CommitDiff() error: %v", err)
	}
	if strings.Contains(d, "Binary files") || !strings.Contains(d, "+package a") {
		t.Errorf("CommitDiff() diff = %q, want a.go as text", d)
	}
}

func TestCommitDiff_Errors(t *testing.T) {
	dir := t.TempDir()
	gitOut(t, dir, "init", "-q")
	writeRepoFile(t, dir, "a.go", "package a\n")
	base := commitRepo(t, dir, "base")

	for _, tt := range []struct{ name, base, head string }{
		{"unknown head", base, strings.Repeat("ab", 20)},
		{"empty base", "", base},
		{"option-like base", "--output=x", base},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := CommitDiff(dir, tt.base, tt.head); err == nil {
				t.Errorf("CommitDiff(%q, %q) error = nil, want one", tt.base, tt.head)
			}
		})
	}
}

func TestCommitDiff_NonASCIIPath(t *testing.T) {
	dir := t.TempDir()
	gitOut(t, dir, "init", "-q")
	gitOut(t, dir, "config", "core.quotePath", "true")
	writeRepoFile(t, dir, "a.go", "package a\n")
	base := commitRepo(t, dir, "base")
	writeRepoFile(t, dir, "\u00e9 x.go", "package a\n")
	head := commitRepo(t, dir, "head")

	_, files, err := CommitDiff(dir, base, head)
	if err != nil {
		t.Fatalf("CommitDiff() error: %v", err)
	}
	if len(files) != 1 || files[0] != "\u00e9 x.go" {
		t.Errorf("CommitDiff() files = %q, want [\"\u00e9 x.go\"]", files)
	}
}

func TestParseGitVersion(t *testing.T) {
	for _, tt := range []struct {
		out, version string
		major, minor int
	}{
		{"git version 2.39.5\n", "2.39.5", 2, 39},
		{"git version 2.39.5 (Apple Git-154)\n", "2.39.5", 2, 39},
		{"git version 2.47.1.windows.1\n", "2.47.1.windows.1", 2, 47},
		{"git version 2.40.0", "2.40.0", 2, 40},
		{"git version 3.0", "3.0", 3, 0},
	} {
		version, major, minor, err := ParseGitVersion(tt.out)
		if err != nil || version != tt.version || major != tt.major || minor != tt.minor {
			t.Errorf("ParseGitVersion(%q) = %q, %d, %d, %v; want %q, %d, %d", tt.out, version, major, minor, err, tt.version, tt.major, tt.minor)
		}
	}
	for _, bad := range []string{"", "git version", "hello 2.40", "git version x.y", "git version 2"} {
		if _, _, _, err := ParseGitVersion(bad); err == nil {
			t.Errorf("ParseGitVersion(%q) error = nil, want one", bad)
		}
	}
}
