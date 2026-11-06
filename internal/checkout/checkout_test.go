package checkout

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// tree makes a checkout root (symlinks resolved, as Scan wants) and the files
// and directories in it. A name ending in "/" is a directory.
func tree(t *testing.T, names ...string) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		p := filepath.Join(root, filepath.FromSlash(n))
		if n[len(n)-1] == '/' {
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func link(t *testing.T, root, name, target string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, p); err != nil {
		t.Fatal(err)
	}
}

func TestWithin(t *testing.T) {
	tests := []struct {
		dir, path string
		want      bool
	}{
		{"/r", "/r", true},
		{"/r", "/r/a/b", true},
		{"/r", "/r2", false},
		{"/r", "/r2/a", false},
		{"/r", "/", false},
		{"/r/a", "/r", false},
	}
	for _, tt := range tests {
		if got := Within(tt.dir, tt.path); got != tt.want {
			t.Errorf("Within(%q, %q) = %v, want %v", tt.dir, tt.path, got, tt.want)
		}
	}
}

func TestScan(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("s\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := tree(t, "a.go", "sub/", "sub/AGENTS.md", "Claude.md", ".git/AGENTS.md", ".git/ln")
	link(t, root, "up", "..")
	link(t, root, "abs", filepath.Join(outside, "secret.txt"))
	link(t, root, "gone", "../../nope")
	link(t, root, "inlink", "a.go")
	link(t, root, "dangle", "missing.go")
	link(t, root, "self", ".")
	link(t, root, "sub/back", "..")
	link(t, root, ".git/escape", "/etc")

	got, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}
	if want := []string{"Claude.md", "sub/AGENTS.md"}; !reflect.DeepEqual(got.Instructions, want) {
		t.Errorf("Instructions = %v, want %v", got.Instructions, want)
	}
	if want := []string{"self", "sub/back"}; !reflect.DeepEqual(got.ToRoot, want) {
		t.Errorf("ToRoot = %v, want %v", got.ToRoot, want)
	}
	want := []Link{{"abs", filepath.Join(outside, "secret.txt")}, {"gone", "../../nope"}, {"up", ".."}}
	if !reflect.DeepEqual(got.Outside, want) {
		t.Errorf("Outside = %v, want %v", got.Outside, want)
	}
}

func TestIsInstructionName(t *testing.T) {
	for _, n := range []string{"AGENTS.md", "agents.MD", "CLAUDE.md", "Context.md"} {
		if !IsInstructionName(n) {
			t.Errorf("IsInstructionName(%q) = false, want true", n)
		}
	}
	for _, n := range []string{"README.md", "AGENTS.md.bak", "sub/AGENTS.md"} {
		if IsInstructionName(n) {
			t.Errorf("IsInstructionName(%q) = true, want false", n)
		}
	}
}
