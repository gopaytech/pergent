package diff

import "testing"

func TestMatch(t *testing.T) {
	tests := []struct {
		pattern, path string
		want          bool
	}{
		{"**/manifests/**", "a/manifests/b/c.yaml", true},
		{"**/manifests/**", "manifests/c.yaml", true},
		{"**/manifests/**", "a/other/c.yaml", false},
		{"*.md", "docs/x.md", true},
		{"*.md", "x.md", true},
		{"*.md", "docs/x.txt", false},
		{"src/*.go", "src/a.go", true},
		{"src/*.go", "src/sub/a.go", false},
		{"src/**/*.go", "src/a.go", true},
		{"src/**/*.go", "src/sub/deep/a.go", true},
		{"**", "anything/at/all.txt", true},
		{"**", "x", true},
		{"go.mod", "sub/go.mod", true},
		{"cmd/main.go", "cmd/main.go", true},
		{"cmd/main.go", "other/cmd/main.go", false},
	}
	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.path, func(t *testing.T) {
			if got := Match(tt.pattern, tt.path); got != tt.want {
				t.Errorf("Match(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
			}
		})
	}
}
