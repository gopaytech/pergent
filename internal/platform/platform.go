package platform

import (
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

type Platform interface {
	FetchDiff() (diff string, changedFiles []string, err error)
	FindComment(marker string) (commentID int64, body string, err error)
	CreateComment(body string) error
	UpdateComment(commentID int64, body string) error
}

// LocalDiff runs git diff against a base ref.
// ref can be a commit SHA, a branch name like "origin/main", or any git ref.
func LocalDiff(repoPath string, ref string) (diff string, changedFiles []string, err error) {
	if ref == "" {
		return "", nil, fmt.Errorf("base ref is empty")
	}

	diffCmd := exec.Command("git", "-C", repoPath, "diff", ref+"...HEAD")
	diffOut, err := diffCmd.Output()
	if err != nil {
		return "", nil, fmt.Errorf("git diff failed: %w", err)
	}

	filesCmd := exec.Command("git", "-C", repoPath, "diff", "--name-only", ref+"...HEAD")
	filesOut, err := filesCmd.Output()
	if err != nil {
		return "", nil, fmt.Errorf("git diff --name-only failed: %w", err)
	}

	files := strings.Split(strings.TrimSpace(string(filesOut)), "\n")
	if len(files) == 1 && files[0] == "" {
		files = nil
	}

	return string(diffOut), files, nil
}

// readErrorBody extracts up to 512 bytes of the response body for error messages.
func readErrorBody(resp *http.Response) string {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512))
	if err != nil || len(body) == 0 {
		return ""
	}
	return ": " + string(body)
}
