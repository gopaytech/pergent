package graphpack

import (
	"reflect"
	"testing"
)

func TestOldLines(t *testing.T) {
	cases := []struct {
		name  string
		patch string
		want  []int
	}{
		{"removed line", "@@ -5,3 +5,2 @@\n a\n-b\n c\n", []int{6}},
		{"added after context", "@@ -5,2 +5,3 @@\n a\n+x\n b\n", []int{5}},
		{"added at hunk start", "@@ -5,2 +5,3 @@\n+x\n a\n b\n", []int{5}},
		{"replaced line", "@@ -5,3 +5,3 @@\n a\n-b\n+B\n c\n", []int{6}},
		{"two hunks", "@@ -2,2 +2,2 @@\n a\n-b\n+B\n@@ -20,2 +20,3 @@\n c\n+d\n e\n", []int{3, 20}},
		{"new file", "diff --git a/n.go b/n.go\nnew file mode 100644\n--- /dev/null\n+++ b/n.go\n@@ -0,0 +1,3 @@\n+a\n+b\n+c\n", nil},
		{"deleted file", "--- a/d.go\n+++ /dev/null\n@@ -1,3 +0,0 @@\n-a\n-b\n-c\n", []int{1, 2, 3}},
		{"no newline marker", "@@ -1,2 +1,2 @@\n a\n-b\n\\ No newline at end of file\n+B\n\\ No newline at end of file\n", []int{2}},
		{"bad header", "@@ -x,1 +1 @@\n-a\n", nil},
		{"one-line counts", "@@ -7 +7 @@\n-a\n+b\n", []int{7}},
		{"header lines after the hunk", "@@ -1,1 +1,1 @@\n-a\n+b\n--- a/other\n+++ b/other\n", []int{1}},
		{"empty", "", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := OldLines(c.patch); !reflect.DeepEqual(got, c.want) {
				t.Errorf("OldLines = %v, want %v", got, c.want)
			}
		})
	}
}
