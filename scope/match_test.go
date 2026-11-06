package scope

import (
	"strings"
	"testing"
)

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

func TestCheckPattern(t *testing.T) {
	tests := []struct {
		pattern string
		wantErr string
	}{
		{pattern: "**"},
		{pattern: "*.proto"},
		{pattern: "api/**"},
		{pattern: "**/*.go"},
		{pattern: "src/**/test/*.go"},
		{pattern: ".github/**"},
		{pattern: "[ab].go"},
		{pattern: "", wantErr: "empty"},
		{pattern: "api/", wantErr: `"api/**"`},
		{pattern: "**/", wantErr: "never match"},
		{pattern: "/api/**", wantErr: "never match"},
		{pattern: "./api/**", wantErr: "never match"},
		{pattern: "api//x.go", wantErr: "never match"},
		{pattern: "api/../x.go", wantErr: "never match"},
		{pattern: "[x", wantErr: "syntax error"},
		{pattern: "api/[x", wantErr: "syntax error"},
		{pattern: "api/[x/b.go", wantErr: "syntax error"},
	}
	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			err := CheckPattern(tt.pattern)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("CheckPattern(%q) error: %v", tt.pattern, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("CheckPattern(%q) = nil, want an error containing %q", tt.pattern, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("CheckPattern(%q) = %q, want containing %q", tt.pattern, err, tt.wantErr)
			}
		})
	}
}
