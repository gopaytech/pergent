package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type GitLab struct {
	Token     string
	URL       string
	ProjectID string
	MRIID     int
}

func (gl *GitLab) do(method, path string, body io.Reader) (*http.Response, error) {
	url := gl.URL + path
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", gl.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return httpClient.Do(req)
}

func (gl *GitLab) FetchDiff() (string, []string, error) {
	path := fmt.Sprintf("/api/v4/projects/%s/merge_requests/%d/diffs", gl.ProjectID, gl.MRIID)
	resp, err := gl.do("GET", path, nil)
	if err != nil {
		return "", nil, fmt.Errorf("fetching diffs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", nil, fmt.Errorf("fetching diffs: status %d%s", resp.StatusCode, readErrorBody(resp))
	}

	var diffs []struct {
		OldPath string `json:"old_path"`
		NewPath string `json:"new_path"`
		Diff    string `json:"diff"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&diffs); err != nil {
		return "", nil, fmt.Errorf("decoding diffs: %w", err)
	}

	var combined strings.Builder
	var files []string
	seen := make(map[string]bool)

	for _, d := range diffs {
		fmt.Fprintf(&combined, "--- a/%s\n+++ b/%s\n%s\n", d.OldPath, d.NewPath, d.Diff)
		if !seen[d.NewPath] {
			files = append(files, d.NewPath)
			seen[d.NewPath] = true
		}
	}

	return combined.String(), files, nil
}

func (gl *GitLab) FindComment(marker string) (int64, error) {
	page := 1
	for {
		path := fmt.Sprintf("/api/v4/projects/%s/merge_requests/%d/notes?per_page=100&page=%d", gl.ProjectID, gl.MRIID, page)
		resp, err := gl.do("GET", path, nil)
		if err != nil {
			return 0, fmt.Errorf("listing notes: %w", err)
		}

		if resp.StatusCode != 200 {
			errBody := readErrorBody(resp)
			resp.Body.Close()
			return 0, fmt.Errorf("listing notes: status %d%s", resp.StatusCode, errBody)
		}

		var notes []struct {
			ID   int64  `json:"id"`
			Body string `json:"body"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&notes); err != nil {
			resp.Body.Close()
			return 0, fmt.Errorf("decoding notes: %w", err)
		}
		resp.Body.Close()

		for _, n := range notes {
			if strings.Contains(n.Body, marker) {
				return n.ID, nil
			}
		}

		if len(notes) < 100 {
			break
		}
		page++
	}
	return 0, nil
}

func (gl *GitLab) CreateComment(body string) error {
	path := fmt.Sprintf("/api/v4/projects/%s/merge_requests/%d/notes", gl.ProjectID, gl.MRIID)
	payload, _ := json.Marshal(map[string]string{"body": body})
	resp, err := gl.do("POST", path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("creating note: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		return fmt.Errorf("creating note: status %d%s", resp.StatusCode, readErrorBody(resp))
	}
	return nil
}

func (gl *GitLab) UpdateComment(commentID int64, body string) error {
	path := fmt.Sprintf("/api/v4/projects/%s/merge_requests/%d/notes/%d", gl.ProjectID, gl.MRIID, commentID)
	payload, _ := json.Marshal(map[string]string{"body": body})
	resp, err := gl.do("PUT", path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("updating note: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("updating note: status %d%s", resp.StatusCode, readErrorBody(resp))
	}
	return nil
}
