package platform

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGitLab_FetchDiff(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/projects/123/merge_requests/42/diffs" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Write([]byte(`[
			{"old_path": "main.go", "new_path": "main.go", "diff": "@@ -1,3 +1,4 @@\n package main\n+import \"fmt\"\n func main() {}"}
		]`))
	}))
	defer server.Close()

	gl := &GitLab{
		Token:     "test-token",
		URL:       server.URL,
		ProjectID: "123",
		MRIID:     42,
	}

	diff, files, err := gl.FetchDiff()
	if err != nil {
		t.Fatalf("FetchDiff() error: %v", err)
	}
	if diff == "" {
		t.Error("diff should not be empty")
	}
	if len(files) != 1 || files[0] != "main.go" {
		t.Errorf("files = %v, want [main.go]", files)
	}
}

func TestGitLab_CreateComment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v4/projects/123/merge_requests/42/notes" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(201)
		w.Write([]byte(`{"id": 1}`))
	}))
	defer server.Close()

	gl := &GitLab{
		Token:     "test-token",
		URL:       server.URL,
		ProjectID: "123",
		MRIID:     42,
	}

	err := gl.CreateComment("test review body")
	if err != nil {
		t.Fatalf("CreateComment() error: %v", err)
	}
}

func TestGitLab_FindComment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[
			{"id": 100, "body": "some other comment"},
			{"id": 200, "body": "<!-- pergent -->\n## pergent review\nstuff"}
		]`))
	}))
	defer server.Close()

	gl := &GitLab{
		Token:     "test-token",
		URL:       server.URL,
		ProjectID: "123",
		MRIID:     42,
	}

	id, body, err := gl.FindComment("<!-- pergent -->")
	if err != nil {
		t.Fatalf("FindComment() error: %v", err)
	}
	if id != 200 {
		t.Errorf("id = %d, want 200", id)
	}
	if body != "<!-- pergent -->\n## pergent review\nstuff" {
		t.Errorf("body = %q, want %q", body, "<!-- pergent -->\n## pergent review\nstuff")
	}
}

func TestGitLab_FindComment_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id": 100, "body": "some other comment"}]`))
	}))
	defer server.Close()

	gl := &GitLab{
		Token:     "test-token",
		URL:       server.URL,
		ProjectID: "123",
		MRIID:     42,
	}

	id, body, err := gl.FindComment("<!-- pergent -->")
	if err != nil {
		t.Fatalf("FindComment() error: %v", err)
	}
	if id != 0 {
		t.Errorf("id = %d, want 0 (not found)", id)
	}
	if body != "" {
		t.Errorf("body = %q, want empty (not found)", body)
	}
}

func TestGitLab_UpdateComment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if r.URL.Path != "/api/v4/projects/123/merge_requests/42/notes/200" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(200)
		w.Write([]byte(`{"id": 200}`))
	}))
	defer server.Close()

	gl := &GitLab{
		Token:     "test-token",
		URL:       server.URL,
		ProjectID: "123",
		MRIID:     42,
	}

	err := gl.UpdateComment(200, "updated body")
	if err != nil {
		t.Fatalf("UpdateComment() error: %v", err)
	}
}
