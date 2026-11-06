package platform

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A platform's error body can echo a token. Every call that reports one must
// redact it, also when it sits across the 512-byte cut of the excerpt.
func TestErrorBodiesAreRedacted(t *testing.T) {
	const token = "glpat-0123456789abcdefghij"
	t.Setenv("GITLAB_TOKEN", token)
	t.Setenv("GITHUB_TOKEN", token)

	for _, pad := range []int{0, 480, 500, 511} {
		body := strings.Repeat("e", pad) + token + " rejected"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(500)
			w.Write([]byte(body))
		}))

		gh := &GitHub{Token: token, Repo: "o/r", PRNumber: 1, APIURL: server.URL}
		gl := &GitLab{Token: token, URL: server.URL, ProjectID: "1", MRIID: 1}
		calls := map[string]func() error{
			"github FetchDiff":     func() error { _, _, err := gh.FetchDiff(); return err },
			"github FindComment":   func() error { _, _, err := gh.FindComment("<!-- m -->"); return err },
			"github CreateComment": func() error { return gh.CreateComment("x") },
			"github UpdateComment": func() error { return gh.UpdateComment(1, "x") },
			"gitlab FetchDiff":     func() error { _, _, err := gl.FetchDiff(); return err },
			"gitlab FindComment":   func() error { _, _, err := gl.FindComment("<!-- m -->"); return err },
			"gitlab CreateComment": func() error { return gl.CreateComment("x") },
			"gitlab UpdateComment": func() error { return gl.UpdateComment(1, "x") },
		}
		for name, call := range calls {
			err := call()
			if err == nil {
				t.Errorf("pad %d, %s: no error from a 500", pad, name)
				continue
			}
			for n := 5; n <= len(token); n++ {
				if strings.Contains(err.Error(), token[:n]) {
					t.Errorf("pad %d, %s: error keeps the first %d characters of the token: %q", pad, name, n, err)
					break
				}
			}
		}
		server.Close()
	}
}
