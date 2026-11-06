package checkout

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

	got, err := Scan(root, nil)
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

// A dangling link is judged by where its text really leads, through any
// symlinked directory on the way, not by the text alone. One that cannot be
// shown to stay inside leaves the checkout.
func TestScan_DanglingLinks(t *testing.T) {
	outsideDir := filepath.Join(t.TempDir(), "deep", "dir")
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	root := tree(t, "a.go", "sub/")
	link(t, root, "ldir", outsideDir)
	// Inside: nothing on the way leaves the checkout.
	link(t, root, "missing", "missing.go")
	link(t, root, "sub/up-missing", "../missing.go")
	link(t, root, "sub/deeper", "nope/further/still")
	// Outside: through a symlinked directory, plain or with "..".
	link(t, root, "through", "ldir/missing.go")
	link(t, root, "sub/through-dotdot", "../ldir/../missing.go")
	link(t, root, "dotdot-after", "ldir/../../missing.go")
	link(t, root, "abs-missing", filepath.Join(outsideDir, "missing.go"))

	got, err := Scan(root, nil)
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}
	var outside []string
	for _, l := range got.Outside {
		outside = append(outside, l.Path)
	}
	want := []string{"abs-missing", "dotdot-after", "ldir", "sub/through-dotdot", "through"}
	if !reflect.DeepEqual(outside, want) {
		t.Errorf("Outside = %v, want %v", outside, want)
	}
}

// A directory Scan cannot read is reported, and the walk goes on, so a link
// after it is still found.
func TestScan_UnreadableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every directory")
	}
	root := tree(t, "locked/", "z/")
	link(t, root, "z/up", "../..")
	locked := filepath.Join(root, "locked")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })

	got, err := Scan(root, nil)
	if err != nil {
		t.Fatalf("Scan() error: %v, want the unreadable directory reported instead", err)
	}
	if want := []string{"locked"}; !reflect.DeepEqual(got.Unreadable, want) {
		t.Errorf("Unreadable = %v, want %v", got.Unreadable, want)
	}
	if want := []Link{{"z/up", "../.."}}; !reflect.DeepEqual(got.Outside, want) {
		t.Errorf("Outside = %v, want %v", got.Outside, want)
	}
}

// A link inside the checkout whose real target the caller's rule denies is
// reported as Denied: the read rules match the name that is read, not where a
// link leads. The rule is given the target's path relative to the root.
func TestScan_DeniedTargets(t *testing.T) {
	root := tree(t, "prod.env", "cfg/db.env", "cfg/ok.txt", "keys/")
	denied := func(rel string) bool {
		return strings.HasSuffix(rel, ".env") || strings.HasPrefix(rel, "keys/")
	}
	link(t, root, "safe.txt", "prod.env")
	link(t, root, "sub/deep.txt", "../cfg/db.env")
	link(t, root, "chain", "safe.txt")
	link(t, root, "viadir", "cfg")
	link(t, root, "okay", "cfg/ok.txt")
	link(t, root, "dangle-denied", "missing.env")
	link(t, root, "dangle-fine", "missing.txt")
	link(t, root, "keys/k", "../cfg/ok.txt")
	link(t, root, "out", "/etc") // outside, not Denied
	link(t, root, "root", ".")   // to the root, not Denied

	got, err := Scan(root, denied)
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}
	want := []Link{
		{"chain", "safe.txt"},
		{"dangle-denied", "missing.env"},
		{"safe.txt", "prod.env"},
		{"sub/deep.txt", "../cfg/db.env"},
	}
	if !reflect.DeepEqual(got.Denied, want) {
		t.Errorf("Denied = %v, want %v", got.Denied, want)
	}
	if want := []Link{{"out", "/etc"}}; !reflect.DeepEqual(got.Outside, want) {
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
