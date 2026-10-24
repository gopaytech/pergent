package platform

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/gopaytech/pergent/internal/diff"
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

func TestGitLab_FetchDiff_FilesSplitWithFlags(t *testing.T) {
	pages := map[string]string{
		"1": `[
			{"old_path": "app/main.go", "new_path": "app/main.go", "a_mode": "100644", "b_mode": "100644", "diff": "@@ -1 +1 @@\n-old\n+new\n"},
			{"old_path": "app/added.go", "new_path": "app/added.go", "a_mode": "0", "b_mode": "100644", "new_file": true, "diff": "@@ -0,0 +1 @@\n+package app\n"}
		]`,
		"2": `[
			{"old_path": "app/gone.go", "new_path": "app/gone.go", "a_mode": "100644", "b_mode": "0", "deleted_file": true, "diff": "@@ -1 +0,0 @@\n-package app\n"},
			{"old_path": "app/old name.go", "new_path": "app/new name.go", "a_mode": "100644", "b_mode": "100644", "renamed_file": true, "diff": "@@ -1 +1 @@\n-a\n+b\n"},
			{"old_path": "app/huge.go", "new_path": "app/huge.go", "a_mode": "100644", "b_mode": "100644", "too_large": true, "diff": ""},
			{"old_path": "app/collapsed.go", "new_path": "app/collapsed.go", "a_mode": "100644", "b_mode": "100644", "collapsed": true, "diff": ""},
			{"old_path": "app/moved.go", "new_path": "app/moved2.go", "a_mode": "100644", "b_mode": "100644", "renamed_file": true, "diff": ""},
			{"old_path": "app/run.sh", "new_path": "app/run.sh", "a_mode": "100644", "b_mode": "100755", "diff": ""}
		]`,
	}
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/projects/123/merge_requests/42/diffs" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		page := r.URL.Query().Get("page")
		requested = append(requested, page)
		if r.URL.Query().Get("per_page") != "100" {
			t.Errorf("per_page = %q, want 100", r.URL.Query().Get("per_page"))
		}
		next := ""
		if page == "1" {
			next = "2"
		}
		w.Header().Set("X-Next-Page", next)
		body, ok := pages[page]
		if !ok {
			body = "[]"
		}
		w.Write([]byte(body))
	}))
	defer server.Close()

	gl := &GitLab{Token: "test-token", URL: server.URL, ProjectID: "123", MRIID: 42}
	unified, files, err := gl.FetchDiff()
	if err != nil {
		t.Fatalf("FetchDiff() error: %v", err)
	}
	if want := []string{"1", "2"}; !slices.Equal(requested, want) {
		t.Errorf("requested pages %q, want %q", requested, want)
	}
	wantFiles := []string{"app/main.go", "app/added.go", "app/gone.go", "app/new name.go", "app/huge.go", "app/collapsed.go", "app/moved2.go", "app/run.sh"}
	if !slices.Equal(files, wantFiles) {
		t.Errorf("files = %q, want %q", files, wantFiles)
	}

	split := diff.Split(unified)
	type got struct {
		Path, OldPath, NotReviewed string
		Deleted                    bool
	}
	var gots []got
	for _, f := range split {
		gots = append(gots, got{f.Path, f.OldPath, f.NotReviewed, f.Deleted})
	}
	const tooLarge = "GitLab did not return this file's diff (too large)"
	want := []got{
		{Path: "app/main.go"},
		{Path: "app/added.go"},
		{Path: "app/gone.go", Deleted: true},
		{Path: "app/new name.go", OldPath: "app/old name.go"},
		{Path: "app/huge.go", NotReviewed: tooLarge},
		{Path: "app/collapsed.go", NotReviewed: tooLarge},
		{Path: "app/moved2.go", OldPath: "app/moved.go"},
		{Path: "app/run.sh"},
	}
	if !slices.Equal(gots, want) {
		t.Fatalf("split files:\n got %+v\nwant %+v\ndiff:\n%s", gots, want, unified)
	}

	c := diff.NewClassifier(t.TempDir())
	wantRules := []string{"", "", "deleted", "", "", "", "no content changes", "no content changes"}
	for i, f := range split {
		if rule, _ := c.Exclusion(f); rule != wantRules[i] {
			t.Errorf("%s: exclusion = %q, want %q", f.Path, rule, wantRules[i])
		}
	}
	if !strings.Contains(split[1].Patch, "new file mode 100644\n--- /dev/null\n+++ b/app/added.go\n@@ -0,0 +1 @@\n+package app\n") {
		t.Errorf("new file patch = %q", split[1].Patch)
	}
}

func TestGitLab_FetchDiff_EmptyDiffWithoutModeChangeIsNotReviewed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Next-Page", "")
		w.Write([]byte(`[{"old_path": "a.go", "new_path": "a.go", "a_mode": "100644", "b_mode": "100644", "diff": ""}]`))
	}))
	defer server.Close()

	gl := &GitLab{Token: "test-token", URL: server.URL, ProjectID: "123", MRIID: 42}
	unified, _, err := gl.FetchDiff()
	if err != nil {
		t.Fatalf("FetchDiff() error: %v", err)
	}
	split := diff.Split(unified)
	if len(split) != 1 || split[0].NotReviewed != "GitLab did not return this file's diff" {
		t.Errorf("split = %+v, want a.go marked not reviewed", split)
	}
}
