package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"github.com/gopaytech/pergent/internal/diff"
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

// gitlabDiff is one file of GET /merge_requests/:iid/diffs.
type gitlabDiff struct {
	OldPath     string `json:"old_path"`
	NewPath     string `json:"new_path"`
	AMode       string `json:"a_mode"`
	BMode       string `json:"b_mode"`
	Diff        string `json:"diff"`
	NewFile     bool   `json:"new_file"`
	RenamedFile bool   `json:"renamed_file"`
	DeletedFile bool   `json:"deleted_file"`
	TooLarge    bool   `json:"too_large"`
	Collapsed   bool   `json:"collapsed"`
}

// gitlabDiffsPerPage is the page size FetchDiff asks for (GitLab's maximum).
const gitlabDiffsPerPage = 100

// FetchDiff returns the merge request's changes as a git-style unified diff,
// one "diff --git" section per file. GitLab returns each file's hunks
// without git's headers, so FetchDiff writes them. A file whose diff GitLab
// left out (too large, collapsed, or empty without a rename or mode change)
// gets a diff.NotReviewedHeader line so it is reported as not reviewed.
func (gl *GitLab) FetchDiff() (string, []string, error) {
	var diffs []gitlabDiff
	for page := 1; ; {
		path := fmt.Sprintf("/api/v4/projects/%s/merge_requests/%d/diffs?per_page=%d&page=%d", gl.ProjectID, gl.MRIID, gitlabDiffsPerPage, page)
		resp, err := gl.do("GET", path, nil)
		if err != nil {
			return "", nil, fmt.Errorf("fetching diffs: %w", err)
		}
		if resp.StatusCode != 200 {
			errBody := readErrorBody(resp)
			resp.Body.Close()
			return "", nil, fmt.Errorf("fetching diffs: status %d%s", resp.StatusCode, errBody)
		}
		var batch []gitlabDiff
		err = json.NewDecoder(resp.Body).Decode(&batch)
		resp.Body.Close()
		if err != nil {
			return "", nil, fmt.Errorf("decoding diffs: %w", err)
		}
		diffs = append(diffs, batch...)

		// Follow X-Next-Page when GitLab sends it (empty on the last page);
		// otherwise stop at the first short page.
		if next, ok := resp.Header["X-Next-Page"]; ok {
			n, err := strconv.Atoi(strings.TrimSpace(strings.Join(next, "")))
			if err != nil || n <= page {
				break
			}
			page = n
			continue
		}
		if len(batch) < gitlabDiffsPerPage {
			break
		}
		page++
	}

	var combined strings.Builder
	var files []string
	seen := make(map[string]bool)
	for _, d := range diffs {
		writeGitLabFile(&combined, d)
		if !seen[d.NewPath] {
			files = append(files, d.NewPath)
			seen[d.NewPath] = true
		}
	}
	return combined.String(), files, nil
}

// writeGitLabFile writes one file of a GitLab diff with the headers git
// would have written.
func writeGitLabFile(b *strings.Builder, d gitlabDiff) {
	oldPath, newPath := d.OldPath, d.NewPath
	if oldPath == "" {
		oldPath = newPath
	}
	if newPath == "" {
		newPath = oldPath
	}
	modeChanged := d.AMode != "" && d.BMode != "" && d.AMode != d.BMode
	fmt.Fprintf(b, "diff --git %s %s\n", quotePath("a/"+oldPath), quotePath("b/"+newPath))
	switch {
	case d.NewFile:
		fmt.Fprintf(b, "new file mode %s\n", mode(d.BMode))
	case d.DeletedFile:
		fmt.Fprintf(b, "deleted file mode %s\n", mode(d.AMode))
	case modeChanged:
		fmt.Fprintf(b, "old mode %s\nnew mode %s\n", d.AMode, d.BMode)
	}
	if d.RenamedFile {
		fmt.Fprintf(b, "rename from %s\nrename to %s\n", quotePath(oldPath), quotePath(newPath))
	}
	switch {
	case d.TooLarge || d.Collapsed:
		fmt.Fprintf(b, "%sGitLab did not return this file's diff (too large)\n", diff.NotReviewedHeader)
		return
	case d.Diff == "" && !d.NewFile && !d.DeletedFile && !d.RenamedFile && !modeChanged:
		fmt.Fprintf(b, "%sGitLab did not return this file's diff\n", diff.NotReviewedHeader)
		return
	case d.Diff == "":
		return
	}
	minus, plus := quotePath("a/"+oldPath), quotePath("b/"+newPath)
	if d.NewFile {
		minus = "/dev/null"
	}
	if d.DeletedFile {
		plus = "/dev/null"
	}
	fmt.Fprintf(b, "--- %s\n+++ %s\n%s", minus, plus, d.Diff)
	if !strings.HasSuffix(d.Diff, "\n") {
		b.WriteString("\n")
	}
}

func mode(m string) string {
	if m == "" || m == "0" {
		return "100644"
	}
	return m
}

// quotePath quotes a path for a diff header when it holds spaces, quotes,
// backslashes or non-printable characters; diff.Split unquotes it.
func quotePath(p string) string {
	if strings.IndexFunc(p, func(r rune) bool {
		return r == ' ' || r == '"' || r == '\\' || !unicode.IsPrint(r)
	}) < 0 {
		return p
	}
	return strconv.Quote(p)
}

func (gl *GitLab) FindComment(marker string) (int64, string, error) {
	page := 1
	for {
		path := fmt.Sprintf("/api/v4/projects/%s/merge_requests/%d/notes?per_page=100&page=%d", gl.ProjectID, gl.MRIID, page)
		resp, err := gl.do("GET", path, nil)
		if err != nil {
			return 0, "", fmt.Errorf("listing notes: %w", err)
		}

		if resp.StatusCode != 200 {
			errBody := readErrorBody(resp)
			resp.Body.Close()
			return 0, "", fmt.Errorf("listing notes: status %d%s", resp.StatusCode, errBody)
		}

		var notes []struct {
			ID   int64  `json:"id"`
			Body string `json:"body"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&notes); err != nil {
			resp.Body.Close()
			return 0, "", fmt.Errorf("decoding notes: %w", err)
		}
		resp.Body.Close()

		for _, n := range notes {
			if strings.Contains(n.Body, marker) {
				return n.ID, n.Body, nil
			}
		}

		if len(notes) < 100 {
			break
		}
		page++
	}
	return 0, "", nil
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
