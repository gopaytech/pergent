package runner

import "testing"

func TestPathTarget(t *testing.T) {
	tests := []struct {
		name, repo, path, want string
	}{
		{"absolute in repo", "/work/repo", "/work/repo/a.go", TargetRepository},
		{"relative", "/work/repo", "a.go", TargetRepository},
		{"no path", "/work/repo", "", TargetRepository},
		{"dotdot escapes", "/work/repo", "/work/repo/../secret", TargetOutside},
		{"diff file", "/work/repo", "/tmp/pergent-diffs-1/0001.patch", TargetPergent},
		{"diff index", "/work/repo", "/tmp/pergent-diffs-1/index.txt", TargetPergent},
		{"attachment", "/work/repo", "/tmp/pergent-batch-9/diff.patch", TargetPergent},
		{"tool output", "/work/repo", "/tmp/opencode-data-pergent-3/opencode/tool-output/tool_1", TargetToolOutput},
		{"auth file", "/work/repo", "/tmp/opencode-data-pergent-3/opencode/auth.json", TargetOutside},
		{"other temp", "/work/repo", "/tmp/other/x", TargetOutside},
		{"linked clone", "/work/repo", "/work/linked-1/a.go", TargetOutside},
		{"repo inside a pergent directory", "/tmp/pergent-repo", "/tmp/pergent-repo/a.go", TargetRepository},
		{"repo nested deeper than a pergent directory", "/tmp/pergent-x/checkout", "/tmp/pergent-x/checkout/a.go", TargetRepository},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pathTarget(tt.repo, "/tmp/", tt.path); got != tt.want {
				t.Errorf("pathTarget(%q, %q) = %q, want %q", tt.repo, tt.path, got, tt.want)
			}
		})
	}
}
