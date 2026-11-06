package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gopaytech/pergent/internal/diff"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestLocalDiff_IgnoresUserDiffConfig(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	// Each setting changes git diff's output format.
	for _, setting := range [][2]string{
		{"diff.noprefix", "true"},
		{"diff.mnemonicPrefix", "true"},
		{"color.ui", "always"},
		{"diff.external", "false"},
	} {
		t.Run(setting[0], func(t *testing.T) {
			dir := t.TempDir()
			git(t, dir, "init", "-q", "-b", "main")
			git(t, dir, "config", "user.email", "test@example.com")
			git(t, dir, "config", "user.name", "Test")
			write := func(name, content string) {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
			}
			write("a.go", "package a\n")
			git(t, dir, "add", ".")
			git(t, dir, "commit", "-q", "-m", "base")
			git(t, dir, "branch", "base")
			write("a.go", "package a\n\nvar x = 1\n")
			git(t, dir, "commit", "-q", "-am", "change")
			git(t, dir, "config", setting[0], setting[1])

			unified, files, err := LocalDiff(dir, "base")
			if err != nil {
				t.Fatalf("LocalDiff() error: %v", err)
			}
			if !slices.Equal(files, []string{"a.go"}) {
				t.Errorf("files = %q, want [a.go]", files)
			}
			split := diff.Split(unified)
			if len(split) != 1 || split[0].Path != "a.go" {
				t.Fatalf("Split = %+v, want one file a.go; diff:\n%q", split, unified)
			}
			if rule, _ := diff.NewClassifier(dir).Exclusion(split[0]); rule != "" {
				t.Errorf("exclusion = %q, want none; diff:\n%q", rule, unified)
			}
		})
	}
}

// git C-quotes unusual paths in --name-only ("caf\303\251.go"). LocalDiff must
// name files the way Split does, or one file shows up as two.
func TestLocalDiff_UnquotesFileNames(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "user.name", "Test")
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "base")
	git(t, dir, "branch", "base")
	for _, n := range []string{"café.go", "my file.go", "plain.go"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("package a\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "change")

	unified, files, err := LocalDiff(dir, "base")
	if err != nil {
		t.Fatalf("LocalDiff() error: %v", err)
	}
	slices.Sort(files)
	if want := []string{"café.go", "my file.go", "plain.go"}; !slices.Equal(files, want) {
		t.Errorf("files = %q, want %q", files, want)
	}
	var paths []string
	for _, f := range diff.Split(unified) {
		paths = append(paths, f.Path)
	}
	slices.Sort(paths)
	if !slices.Equal(paths, files) {
		t.Errorf("Split paths %q differ from files %q", paths, files)
	}
}
