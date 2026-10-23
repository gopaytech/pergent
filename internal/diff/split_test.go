package diff

import (
	"strings"
	"testing"
)

const threeFiles = `diff --git a/main.go b/main.go
index 111..222 100644
--- a/main.go
+++ b/main.go
@@ -1,3 +1,3 @@
 package main
-var a = 1
+var a = 2
diff --git a/added.go b/added.go
new file mode 100644
index 0000000..333
--- /dev/null
+++ b/added.go
@@ -0,0 +1,2 @@
+package main
+var b = 1
diff --git a/gone.go b/gone.go
deleted file mode 100644
index 444..0000000
--- a/gone.go
+++ /dev/null
@@ -1,2 +0,0 @@
-package main
-var c = 1
`

func TestSplit_ThreeFiles(t *testing.T) {
	files := Split(threeFiles)
	if len(files) != 3 {
		t.Fatalf("len = %d, want 3", len(files))
	}
	wantPaths := []string{"main.go", "added.go", "gone.go"}
	for i, f := range files {
		if f.Path != wantPaths[i] {
			t.Errorf("files[%d].Path = %q, want %q", i, f.Path, wantPaths[i])
		}
		if wantDeleted := i == 2; f.Deleted != wantDeleted {
			t.Errorf("files[%d].Deleted = %v, want %v", i, f.Deleted, wantDeleted)
		}
		if f.Binary {
			t.Errorf("files[%d].Binary = true, want false", i)
		}
	}
	if got := files[0].Patch[:len("diff --git a/main.go")]; got != "diff --git a/main.go" {
		t.Errorf("patch 0 starts with %q", got)
	}
	if !strings.Contains(files[0].Patch, "+var a = 2") || strings.Contains(files[0].Patch, "added.go") {
		t.Errorf("patch 0 has wrong boundaries: %q", files[0].Patch)
	}
	if !strings.Contains(files[2].Patch, "-var c = 1") {
		t.Errorf("patch 2 missing its content: %q", files[2].Patch)
	}
}

func TestSplit_Rename(t *testing.T) {
	in := `diff --git a/old.yaml b/new.yaml
similarity index 100%
rename from old.yaml
rename to new.yaml
`
	files := Split(in)
	if len(files) != 1 {
		t.Fatalf("len = %d, want 1", len(files))
	}
	if files[0].Path != "new.yaml" || files[0].OldPath != "old.yaml" {
		t.Errorf("Path/OldPath = %q/%q, want new.yaml/old.yaml", files[0].Path, files[0].OldPath)
	}
}

func TestSplit_QuotedPathWithSpaces(t *testing.T) {
	in := `diff --git "a/dir/x y.yaml" "b/dir/x y.yaml"
index 111..222 100644
--- "a/dir/x y.yaml"
+++ "b/dir/x y.yaml"
@@ -1 +1 @@
-a: 1
+a: 2
`
	files := Split(in)
	if len(files) != 1 || files[0].Path != "dir/x y.yaml" {
		t.Fatalf("files = %+v, want one file dir/x y.yaml", files)
	}
}

func TestSplit_PathParsing(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		path     string
		oldPath  string
		deleted  bool
		binary   bool
		wantFile int
	}{
		{
			name:     "unquoted spaces, binary, no ---/+++ lines",
			in:       "diff --git a/dir/x y.png b/dir/x y.png\nindex 1..2 100644\nBinary files a/dir/x y.png and b/dir/x y.png differ\n",
			path:     "dir/x y.png",
			binary:   true,
			wantFile: 1,
		},
		{
			name:     "git binary patch",
			in:       "diff --git a/i.bin b/i.bin\nnew file mode 100644\nindex 0..1\nGIT binary patch\nliteral 3\nKcmZQz\n\nliteral 0\nHcmV?d00001\n\n",
			path:     "i.bin",
			binary:   true,
			wantFile: 1,
		},
		{
			name:     "escaped quoted path",
			in:       "diff --git \"a/d\\303\\251.txt\" \"b/d\\303\\251.txt\"\n--- \"a/d\\303\\251.txt\"\n+++ \"b/d\\303\\251.txt\"\n@@ -1 +1 @@\n-a\n+b\n",
			path:     "dé.txt",
			wantFile: 1,
		},
		{
			name:     "no files in empty input",
			in:       "",
			wantFile: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := Split(tt.in)
			if len(files) != tt.wantFile {
				t.Fatalf("len = %d, want %d", len(files), tt.wantFile)
			}
			if tt.wantFile == 0 {
				return
			}
			f := files[0]
			if f.Path != tt.path || f.OldPath != tt.oldPath || f.Deleted != tt.deleted || f.Binary != tt.binary {
				t.Errorf("got %+v", f)
			}
		})
	}
}
