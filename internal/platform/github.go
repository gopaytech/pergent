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

func (g *GitHub) FindComment(marker string) (int64, string, error) {
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
		}
		if err := json.NewDecoder(resp.Body).Decode(&comments); err != nil {
			resp.Body.Close()
			return 0, "", fmt.Errorf("decoding comments: %w", err)
		}
		resp.Body.Close()

		for _, c := range comments {
			if strings.Contains(c.Body, marker) {
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
