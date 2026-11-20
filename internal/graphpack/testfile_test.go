package graphpack

import "testing"

func TestIsTestFile(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"server_test.go", true},
		{"internal/review/review_test.go", true},
		{"internal/review/review.go", false},
		{"cmd/test.go", false}, // a command named test, not a test file
		{"pkg/test_util.py", true},
		{"pkg/util_test.py", true},
		{"pkg/test_util.go", false},
		{"pkg/contest.py", false},
		{"web/src/app.test.ts", true},
		{"web/src/app.spec.ts", true},
		{"web/src/app.test.tsx", true},
		{"web/src/app.spec.jsx", true},
		{"web/src/app.test.js", true},
		{"web/src/app.ts", false},
		{"web/src/latest.ts", false},
		{"test/helpers.go", true},
		{"src/tests/util.py", true},
		{"web/src/__tests__/app.ts", true},
		{"src/test/java/com/acme/AppTest.java", true},
		{"testdata/input.go", false},
		{"contest/util.go", false},
		{"tests", false}, // a file named tests, not a folder
		{"Test/helpers.go", false},
	}
	for _, c := range cases {
		if got := isTestFile(c.path); got != c.want {
			t.Errorf("isTestFile(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}
