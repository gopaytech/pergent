package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gopaytech/pergent/internal/diff"
)

type GitHub struct {
	Token    string
	Repo     string
	PRNumber int
	APIURL   string

	// The token's own identity, read once by FindComment.
	identityRead bool
	selfID       int64 // 0 when the token cannot read /user
}

func (g *GitHub) apiURL() string {
	if g.APIURL != "" {
		return g.APIURL
	}
	return "https://api.github.com"
}

func (g *GitHub) do(method, path string, accept string, body io.Reader) (*http.Response, error) {
	url := g.apiURL() + path
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+g.Token)
	if accept != "" {
		req.Header.Set("Accept", accept)
	} else {
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return httpClient.Do(req)
}

func (g *GitHub) FetchDiff() (string, []string, error) {
	path := fmt.Sprintf("/repos/%s/pulls/%d", g.Repo, g.PRNumber)
	resp, err := g.do("GET", path, "application/vnd.github.v3.diff", nil)
	if err != nil {
		return "", nil, fmt.Errorf("fetching diff: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", nil, fmt.Errorf("fetching diff: status %d%s", resp.StatusCode, readErrorBody(resp))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, fmt.Errorf("reading diff: %w", err)
	}

	unified := string(data)
	return unified, parseDiffFiles(unified), nil
}

// FetchChangeDescription reads the pull request's title and body.
func (g *GitHub) FetchChangeDescription() (string, error) {
	resp, err := g.do("GET", fmt.Sprintf("/repos/%s/pulls/%d", g.Repo, g.PRNumber), "", nil)
	if err != nil {
		return "", fmt.Errorf("fetching the pull request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("fetching the pull request: status %d%s", resp.StatusCode, readErrorBody(resp))
	}
	var pr struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return "", fmt.Errorf("decoding the pull request: %w", err)
	}
	return changeDescription(pr.Title, pr.Body), nil
}

// readIdentity learns who the token posts as, once per run. GitHub Actions'
// GITHUB_TOKEN (and an App installation token) cannot call /user: it answers
// 401, 403 or 404, and then selfID stays 0 and FindComment accepts only a
// Bot author (a person cannot post as a Bot). Any other failure is an error,
// not a guess, so the caller can retry.
func (g *GitHub) readIdentity() error {
	if g.identityRead {
		return nil
	}
	resp, err := g.do("GET", "/user", "", nil)
	if err != nil {
		return fmt.Errorf("reading the token's identity: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case 200:
		var u struct {
			ID int64 `json:"id"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&u); err != nil || u.ID == 0 {
			return fmt.Errorf("reading the token's identity: no user id in the response")
		}
		g.selfID = u.ID
	case 401, 403, 404:
		g.selfID = 0
	default:
		return fmt.Errorf("reading the token's identity: status %d%s", resp.StatusCode, readErrorBody(resp))
	}
	g.identityRead = true
	return nil
}

// FindComment returns the first comment that starts with marker and was
// posted by the token's own identity, or, when that is unreadable, by a Bot.
// It pages on past comments by anyone else.
func (g *GitHub) FindComment(marker string) (int64, string, error) {
	if err := g.readIdentity(); err != nil {
		return 0, "", err
	}
	page := 1
	for {
		path := fmt.Sprintf("/repos/%s/issues/%d/comments?per_page=100&page=%d", g.Repo, g.PRNumber, page)
		resp, err := g.do("GET", path, "", nil)
		if err != nil {
			return 0, "", fmt.Errorf("listing comments: %w", err)
		}

		if resp.StatusCode != 200 {
			errBody := readErrorBody(resp)
			resp.Body.Close()
			return 0, "", fmt.Errorf("listing comments: status %d%s", resp.StatusCode, errBody)
		}

		var comments []struct {
			ID   int64  `json:"id"`
			Body string `json:"body"`
			User *struct {
				ID   int64  `json:"id"`
				Type string `json:"type"`
			} `json:"user"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&comments); err != nil {
			resp.Body.Close()
			return 0, "", fmt.Errorf("decoding comments: %w", err)
		}
		resp.Body.Close()

		for _, c := range comments {
			if !strings.HasPrefix(c.Body, marker) || c.User == nil {
				continue
			}
			if (g.selfID != 0 && c.User.ID == g.selfID) || (g.selfID == 0 && c.User.Type == "Bot") {
				return c.ID, c.Body, nil
			}
		}

		if len(comments) < 100 {
			break
		}
		page++
	}
	return 0, "", nil
}

func (g *GitHub) CreateComment(body string) error {
	path := fmt.Sprintf("/repos/%s/issues/%d/comments", g.Repo, g.PRNumber)
	payload, _ := json.Marshal(map[string]string{"body": body})
	resp, err := g.do("POST", path, "", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("creating comment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		return fmt.Errorf("creating comment: status %d%s", resp.StatusCode, readErrorBody(resp))
	}
	return nil
}

func (g *GitHub) UpdateComment(commentID int64, body string) error {
	path := fmt.Sprintf("/repos/%s/issues/comments/%d", g.Repo, commentID)
	payload, _ := json.Marshal(map[string]string{"body": body})
	resp, err := g.do("PATCH", path, "", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("updating comment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("updating comment: status %d%s", resp.StatusCode, readErrorBody(resp))
	}
	return nil
}

// parseDiffFiles lists the files of a unified diff, named as diff.Split names
// them: a rename lists its new path and quoted paths are unquoted.
func parseDiffFiles(unified string) []string {
	var files []string
	seen := make(map[string]bool)
	for _, f := range diff.Split(unified) {
		if f.Path != "" && !seen[f.Path] {
			files = append(files, f.Path)
			seen[f.Path] = true
		}
	}
	return files
}
