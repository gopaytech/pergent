package platform

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGitHub_FetchDiff(t *testing.T) {
	diffContent := `diff --git a/main.go b/main.go
index 1234567..abcdefg 100644
--- a/main.go
+++ b/main.go
@@ -1,3 +1,4 @@
 package main
+import "fmt"
 func main() {}
`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/pulls/42" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Accept") != "application/vnd.github.v3.diff" {
			t.Errorf("unexpected Accept header: %s", r.Header.Get("Accept"))
		}
		w.Write([]byte(diffContent))
	}))
	defer server.Close()

	gh := &GitHub{
		Token:    "test-token",
		Repo:     "owner/repo",
		PRNumber: 42,
		APIURL:   server.URL,
	}

	diff, files, err := gh.FetchDiff()
	if err != nil {
		t.Fatalf("FetchDiff() error: %v", err)
	}
	if diff != diffContent {
		t.Errorf("diff mismatch")
	}
	if len(files) != 1 || files[0] != "main.go" {
		t.Errorf("files = %v, want [main.go]", files)
	}
}

func TestGitHub_CreateComment(t *testing.T) {
	var receivedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/repos/owner/repo/issues/42/comments" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		buf := make([]byte, r.ContentLength)
		r.Body.Read(buf)
		receivedBody = string(buf)
		w.WriteHeader(201)
		w.Write([]byte(`{"id": 1}`))
	}))
	defer server.Close()

	gh := &GitHub{
		Token:    "test-token",
		Repo:     "owner/repo",
		PRNumber: 42,
		APIURL:   server.URL,
	}

	err := gh.CreateComment("test review body")
	if err != nil {
		t.Fatalf("CreateComment() error: %v", err)
	}
	if receivedBody == "" {
		t.Error("no body sent")
	}
}

func TestGitHub_FindComment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[
			{"id": 100, "body": "some other comment"},
			{"id": 200, "body": "<!-- pergent -->\n## pergent review\nstuff"}
		]`))
	}))
	defer server.Close()

	gh := &GitHub{
		Token:    "test-token",
		Repo:     "owner/repo",
		PRNumber: 42,
		APIURL:   server.URL,
	}

	id, err := gh.FindComment("<!-- pergent -->")
	if err != nil {
		t.Fatalf("FindComment() error: %v", err)
	}
	if id != 200 {
		t.Errorf("id = %d, want 200", id)
	}
}

func TestGitHub_FindComment_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id": 100, "body": "some other comment"}]`))
	}))
	defer server.Close()

	gh := &GitHub{
		Token:    "test-token",
		Repo:     "owner/repo",
		PRNumber: 42,
		APIURL:   server.URL,
	}

	id, err := gh.FindComment("<!-- pergent -->")
	if err != nil {
		t.Fatalf("FindComment() error: %v", err)
	}
	if id != 0 {
		t.Errorf("id = %d, want 0 (not found)", id)
	}
}

func TestGitHub_UpdateComment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		if r.URL.Path != "/repos/owner/repo/issues/comments/200" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(200)
		w.Write([]byte(`{"id": 200}`))
	}))
	defer server.Close()

	gh := &GitHub{
		Token:    "test-token",
		Repo:     "owner/repo",
		PRNumber: 42,
		APIURL:   server.URL,
	}

	err := gh.UpdateComment(200, "updated body")
	if err != nil {
		t.Fatalf("UpdateComment() error: %v", err)
	}
}
