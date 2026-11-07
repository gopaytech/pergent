package platform

import (
	"net/http"
	"net/http/httptest"
	"slices"
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

func TestParseDiffFiles(t *testing.T) {
	diffContent := "diff --git a/old.go b/new.go\n" +
		"similarity index 90%\n" +
		"rename from old.go\n" +
		"rename to new.go\n" +
		"index 1234567..abcdefg 100644\n" +
		"--- a/old.go\n" +
		"+++ b/new.go\n" +
		"@@ -1 +1 @@\n" +
		"-package old\n" +
		"+package new\n" +
		"diff --git \"a/caf\\303\\251.go\" \"b/caf\\303\\251.go\"\n" +
		"index 1234567..abcdefg 100644\n" +
		"--- \"a/caf\\303\\251.go\"\n" +
		"+++ \"b/caf\\303\\251.go\"\n" +
		"@@ -1 +1 @@\n" +
		"-package a\n" +
		"+package b\n" +
		"diff --git a/my file.go b/my file.go\n" +
		"index 1234567..abcdefg 100644\n" +
		"--- a/my file.go\n" +
		"+++ b/my file.go\n" +
		"@@ -1 +1 @@\n" +
		"-package a\n" +
		"+package b\n" +
		"diff --git a/gone.go b/gone.go\n" +
		"deleted file mode 100644\n" +
		"index 1234567..0000000\n" +
		"--- a/gone.go\n" +
		"+++ /dev/null\n" +
		"@@ -1 +0,0 @@\n" +
		"-package a\n"
	got := parseDiffFiles(diffContent)
	want := []string{"new.go", "café.go", "my file.go", "gone.go"}
	if !slices.Equal(got, want) {
		t.Errorf("parseDiffFiles = %q, want %q", got, want)
	}
}

func TestGitHubFetchChangeDescription(t *testing.T) {
	cases := map[string]struct{ reply, want string }{
		"title and body": {`{"title": "Fix it", "body": "On purpose."}`, "Fix it\n\nOn purpose."},
		"no body":        {`{"title": "Fix it", "body": null}`, "Fix it"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/repos/owner/repo/pulls/42" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				if r.Header.Get("Accept") != "application/vnd.github+json" {
					t.Errorf("unexpected Accept header: %s", r.Header.Get("Accept"))
				}
				w.Write([]byte(c.reply))
			}))
			defer server.Close()

			gh := &GitHub{Token: "t", Repo: "owner/repo", PRNumber: 42, APIURL: server.URL}
			got, err := gh.FetchChangeDescription()
			if err != nil {
				t.Fatalf("FetchChangeDescription() error: %v", err)
			}
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestGitHubFetchChangeDescriptionFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	gh := &GitHub{Token: "t", Repo: "owner/repo", PRNumber: 42, APIURL: server.URL}
	if _, err := gh.FetchChangeDescription(); err == nil {
		t.Fatal("FetchChangeDescription() = nil error on a 403")
	}
}
