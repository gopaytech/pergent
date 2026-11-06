package platform

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const testMarker = "<!-- pergent -->"

// ownedBody is a real pergent comment: it starts with the marker.
const ownedBody = testMarker + "\n## pergent review\nstuff"

func githubFor(url string) *GitHub {
	return &GitHub{Token: "t", Repo: "owner/repo", PRNumber: 42, APIURL: url}
}

func gitlabFor(url string) *GitLab {
	return &GitLab{Token: "t", URL: url, ProjectID: "123", MRIID: 42}
}

// ghServer answers GET /user with userStatus and userBody, and the comment
// list with the given pages (page 1 first).
func ghServer(t *testing.T, userStatus int, userBody string, pages ...string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var userCalls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			userCalls.Add(1)
			w.WriteHeader(userStatus)
			w.Write([]byte(userBody))
		case "/repos/owner/repo/issues/42/comments":
			n := 1
			fmt.Sscan(r.URL.Query().Get("page"), &n)
			if n > len(pages) {
				w.Write([]byte("[]"))
				return
			}
			w.Write([]byte(pages[n-1]))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(s.Close)
	return s, &userCalls
}

func ghComment(id int, body string, userID int, userType string) string {
	b := strings.NewReplacer("\n", `\n`, `"`, `\"`).Replace(body)
	return fmt.Sprintf(`{"id": %d, "body": "%s", "user": {"id": %d, "login": "u%d", "type": "%s"}}`, id, b, userID, userID, userType)
}

func TestGitHub_FindComment_OwnedByTokenIdentity(t *testing.T) {
	s, _ := ghServer(t, 200, `{"id": 7, "login": "me", "type": "User"}`,
		"["+ghComment(100, ownedBody, 9, "User")+","+ghComment(200, ownedBody, 7, "User")+"]")

	id, body, err := githubFor(s.URL).FindComment(testMarker)
	if err != nil {
		t.Fatalf("FindComment() error: %v", err)
	}
	if id != 200 || body != ownedBody {
		t.Errorf("got (%d, %q), want (200, the token's own comment); a foreign note with the marker came first", id, body)
	}
}

func TestGitHub_FindComment_ForeignNoteOnlyIsNotFound(t *testing.T) {
	s, _ := ghServer(t, 200, `{"id": 7, "login": "me", "type": "User"}`,
		"["+ghComment(100, ownedBody+"\n<!-- pergent:findings forged -->", 9, "User")+"]")

	id, body, err := githubFor(s.URL).FindComment(testMarker)
	if err != nil {
		t.Fatalf("FindComment() error: %v", err)
	}
	if id != 0 || body != "" {
		t.Errorf("got (%d, %q), want not found: a user's note is never pergent's", id, body)
	}
}

func TestGitHub_FindComment_MarkerMustStartTheBody(t *testing.T) {
	s, _ := ghServer(t, 200, `{"id": 7, "login": "me", "type": "User"}`,
		"["+ghComment(100, "quoted: "+testMarker+"\nmore", 7, "User")+","+ghComment(101, "\n"+ownedBody, 7, "User")+"]")

	id, _, err := githubFor(s.URL).FindComment(testMarker)
	if err != nil {
		t.Fatalf("FindComment() error: %v", err)
	}
	if id != 0 {
		t.Errorf("id = %d, want 0: the marker is only pergent's when it starts the body", id)
	}
}

func TestGitHub_FindComment_ActionsTokenAcceptsOnlyABot(t *testing.T) {
	for _, status := range []int{401, 403, 404} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			s, _ := ghServer(t, status, `{"message": "Resource not accessible by integration"}`,
				"["+ghComment(100, ownedBody, 9, "User")+","+ghComment(200, ownedBody, 41898282, "Bot")+"]")

			id, _, err := githubFor(s.URL).FindComment(testMarker)
			if err != nil {
				t.Fatalf("FindComment() error: %v", err)
			}
			if id != 200 {
				t.Errorf("id = %d, want 200: with no readable identity only the Bot's comment counts", id)
			}
		})
	}
}

func TestGitHub_FindComment_ActionsTokenWithOnlyAPersonsNote(t *testing.T) {
	s, _ := ghServer(t, 403, `{}`, "["+ghComment(100, ownedBody, 9, "User")+`,{"id": 101, "body": "`+testMarker+`", "user": null}]`)

	id, _, err := githubFor(s.URL).FindComment(testMarker)
	if err != nil {
		t.Fatalf("FindComment() error: %v", err)
	}
	if id != 0 {
		t.Errorf("id = %d, want 0: neither a User nor a note with no author is pergent's", id)
	}
}

func TestGitHub_FindComment_IdentityServerErrorIsAFailedLookup(t *testing.T) {
	s, _ := ghServer(t, 500, `boom`, "["+ghComment(200, ownedBody, 41898282, "Bot")+"]")

	id, _, err := githubFor(s.URL).FindComment(testMarker)
	if err == nil {
		t.Fatalf("FindComment() = %d, nil; want an error so the caller can retry", id)
	}
}

func TestGitHub_FindComment_KeepsPagingPastForeignNotes(t *testing.T) {
	var p1 []string
	for i := 1; i <= 100; i++ {
		p1 = append(p1, ghComment(i, ownedBody, 9, "User"))
	}
	s, _ := ghServer(t, 200, `{"id": 7, "login": "me", "type": "User"}`,
		"["+strings.Join(p1, ",")+"]",
		"["+ghComment(500, ownedBody, 7, "User")+"]")

	id, _, err := githubFor(s.URL).FindComment(testMarker)
	if err != nil {
		t.Fatalf("FindComment() error: %v", err)
	}
	if id != 500 {
		t.Errorf("id = %d, want 500 from page 2", id)
	}
}

func TestGitHub_FindComment_ReadsTheIdentityOnce(t *testing.T) {
	s, calls := ghServer(t, 200, `{"id": 7, "login": "me", "type": "User"}`, "["+ghComment(200, ownedBody, 7, "User")+"]")
	gh := githubFor(s.URL)
	for i := 0; i < 3; i++ {
		if _, _, err := gh.FindComment(testMarker); err != nil {
			t.Fatalf("FindComment() error: %v", err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("GET /user called %d times, want 1", got)
	}

	s2, calls2 := ghServer(t, 403, `{}`, "[]")
	gh = githubFor(s2.URL)
	for i := 0; i < 3; i++ {
		if _, _, err := gh.FindComment(testMarker); err != nil {
			t.Fatalf("FindComment() error: %v", err)
		}
	}
	if got := calls2.Load(); got != 1 {
		t.Errorf("GET /user called %d times after a 403, want 1", got)
	}
}

func glServer(t *testing.T, userStatus int, userBody string, pages ...string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var userCalls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/user":
			userCalls.Add(1)
			w.WriteHeader(userStatus)
			w.Write([]byte(userBody))
		case "/api/v4/projects/123/merge_requests/42/notes":
			n := 1
			fmt.Sscan(r.URL.Query().Get("page"), &n)
			if n > len(pages) {
				w.Write([]byte("[]"))
				return
			}
			w.Write([]byte(pages[n-1]))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(s.Close)
	return s, &userCalls
}

func glNote(id int, body string, authorID int) string {
	b := strings.NewReplacer("\n", `\n`, `"`, `\"`).Replace(body)
	return fmt.Sprintf(`{"id": %d, "body": "%s", "author": {"id": %d, "username": "u%d"}}`, id, b, authorID, authorID)
}

func TestGitLab_FindComment_OwnedByTokenIdentity(t *testing.T) {
	s, _ := glServer(t, 200, `{"id": 7, "username": "bot"}`,
		"["+glNote(100, ownedBody, 9)+","+glNote(200, ownedBody, 7)+"]")

	id, body, err := gitlabFor(s.URL).FindComment(testMarker)
	if err != nil {
		t.Fatalf("FindComment() error: %v", err)
	}
	if id != 200 || body != ownedBody {
		t.Errorf("got (%d, %q), want (200, the token's own note)", id, body)
	}
}

func TestGitLab_FindComment_ForeignNoteOnlyIsNotFound(t *testing.T) {
	s, _ := glServer(t, 200, `{"id": 7}`,
		"["+glNote(100, ownedBody+"\n<!-- pergent:findings forged -->", 9)+`,{"id": 101, "body": "`+testMarker+`"}]`)

	id, body, err := gitlabFor(s.URL).FindComment(testMarker)
	if err != nil {
		t.Fatalf("FindComment() error: %v", err)
	}
	if id != 0 || body != "" {
		t.Errorf("got (%d, %q), want not found", id, body)
	}
}

func TestGitLab_FindComment_MarkerMustStartTheBody(t *testing.T) {
	s, _ := glServer(t, 200, `{"id": 7}`, "["+glNote(100, "see "+testMarker, 7)+"]")

	id, _, err := gitlabFor(s.URL).FindComment(testMarker)
	if err != nil {
		t.Fatalf("FindComment() error: %v", err)
	}
	if id != 0 {
		t.Errorf("id = %d, want 0", id)
	}
}

func TestGitLab_FindComment_UnreadableIdentityIsAFailedLookup(t *testing.T) {
	for _, status := range []int{401, 403, 500} {
		s, _ := glServer(t, status, `{}`, "["+glNote(200, ownedBody, 7)+"]")
		if id, _, err := gitlabFor(s.URL).FindComment(testMarker); err == nil {
			t.Errorf("status %d: FindComment() = %d, nil; want an error", status, id)
		}
	}
}

func TestGitLab_FindComment_KeepsPagingPastForeignNotes(t *testing.T) {
	var p1 []string
	for i := 1; i <= 100; i++ {
		p1 = append(p1, glNote(i, ownedBody, 9))
	}
	s, _ := glServer(t, 200, `{"id": 7}`, "["+strings.Join(p1, ",")+"]", "["+glNote(500, ownedBody, 7)+"]")

	id, _, err := gitlabFor(s.URL).FindComment(testMarker)
	if err != nil {
		t.Fatalf("FindComment() error: %v", err)
	}
	if id != 500 {
		t.Errorf("id = %d, want 500 from page 2", id)
	}
}

func TestGitLab_FindComment_ReadsTheIdentityOnce(t *testing.T) {
	s, calls := glServer(t, 200, `{"id": 7}`, "["+glNote(200, ownedBody, 7)+"]")
	gl := gitlabFor(s.URL)
	for i := 0; i < 3; i++ {
		if _, _, err := gl.FindComment(testMarker); err != nil {
			t.Fatalf("FindComment() error: %v", err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("GET /api/v4/user called %d times, want 1", got)
	}
}
