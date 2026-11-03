package render

import "testing"

func TestGitHubFileURL(t *testing.T) {
	f := GitHubFileURL("https://github.com/o/r/", "abc123")
	if f == nil {
		t.Fatal("GitHubFileURL returned nil")
	}
	base := "https://github.com/o/r/blob/abc123/"
	tests := []struct {
		path      string
		line, end int
		want      string
	}{
		{"dir/a b#c?.go", 17, 19, base + "dir/a%20b%23c%3F.go#L17-L19"},
		{"dir/a b#c?.go", 17, 17, base + "dir/a%20b%23c%3F.go#L17"},
		{"dir/a b#c?.go", 17, 0, base + "dir/a%20b%23c%3F.go#L17"},
		{"dir/a b#c?.go", 0, 0, base + "dir/a%20b%23c%3F.go"},
		{"ü/x%.go", 0, 0, base + "%C3%BC/x%25.go"},
	}
	for _, tt := range tests {
		if got := f(tt.path, tt.line, tt.end); got != tt.want {
			t.Errorf("f(%q, %d, %d) = %q, want %q", tt.path, tt.line, tt.end, got, tt.want)
		}
	}
}

func TestGitLabFileURL(t *testing.T) {
	f := GitLabFileURL("https://gitlab.example.com/g/p", "abc123")
	if f == nil {
		t.Fatal("GitLabFileURL returned nil")
	}
	base := "https://gitlab.example.com/g/p/-/blob/abc123/"
	tests := []struct {
		path      string
		line, end int
		want      string
	}{
		{"dir/a b#c?.go", 17, 19, base + "dir/a%20b%23c%3F.go#L17-19"},
		{"dir/a b#c?.go", 17, 17, base + "dir/a%20b%23c%3F.go#L17"},
		{"dir/a b#c?.go", 17, 0, base + "dir/a%20b%23c%3F.go#L17"},
		{"dir/a b#c?.go", 0, 0, base + "dir/a%20b%23c%3F.go"},
		{"ü/x%.go", 0, 0, base + "%C3%BC/x%25.go"},
	}
	for _, tt := range tests {
		if got := f(tt.path, tt.line, tt.end); got != tt.want {
			t.Errorf("f(%q, %d, %d) = %q, want %q", tt.path, tt.line, tt.end, got, tt.want)
		}
	}
	if got := GitLabFileURL("https://gitlab.example.com/g/p/", "abc123")("a.go", 0, 0); got != "https://gitlab.example.com/g/p/-/blob/abc123/a.go" {
		t.Errorf("trailing slash not stripped: %q", got)
	}
}

func TestFileURLNeedsRepoAndCommit(t *testing.T) {
	if GitHubFileURL("", "abc123") != nil || GitHubFileURL("https://github.com/o/r", "") != nil {
		t.Error("GitHubFileURL must return nil without a URL or a commit")
	}
	if GitLabFileURL("", "abc123") != nil || GitLabFileURL("https://gitlab.example.com/g/p", "") != nil {
		t.Error("GitLabFileURL must return nil without a URL or a commit")
	}
}

func TestLocation(t *testing.T) {
	tests := []struct {
		f    Finding
		want string
	}{
		{Finding{File: "a.go"}, "a.go"},
		{Finding{File: "a.go", Line: 3}, "a.go:3"},
		{Finding{File: "a.go", Line: 3, EndLine: 3}, "a.go:3"},
		{Finding{File: "a.go", Line: 3, EndLine: 5}, "a.go:3–5"},
		{Finding{File: "a.go", Line: 5, EndLine: 3}, "a.go:5"},
	}
	for _, tt := range tests {
		if got := location(tt.f); got != tt.want {
			t.Errorf("location(%+v) = %q, want %q", tt.f, got, tt.want)
		}
	}
}
