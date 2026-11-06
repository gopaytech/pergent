package platform

import (
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"time"

	"github.com/gopaytech/pergent/internal/redact"
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

	// Pin the output format so the user's git config (colour, external diff
	// tools, diff.noprefix and the like) can't change what Split parses.
	format := []string{"--no-color", "--no-ext-diff", "--src-prefix=a/", "--dst-prefix=b/"}
	diffCmd := exec.Command("git", append(append([]string{"-C", repoPath, "diff"}, format...), ref+"...HEAD")...)
	diffOut, err := diffCmd.Output()
	if err != nil {
		return "", nil, fmt.Errorf("git diff failed: %w", err)
	}

	filesCmd := exec.Command("git", append(append([]string{"-C", repoPath, "diff", "--name-only"}, format...), ref+"...HEAD")...)
	filesOut, err := filesCmd.Output()
	if err != nil {
		return "", nil, fmt.Errorf("git diff --name-only failed: %w", err)
	}

	return string(diffOut), nameOnlyFiles(filesOut), nil
}

// errorBodyLen is how many characters of an error body an error message shows.
const errorBodyLen = 512

// errorBodyRead is how much of an error body is read, so redaction sees a
// credential in the part that is shown whole.
const errorBodyRead = 64 * 1024

// readErrorBody extracts the start of the response body for error messages.
// Every platform call reports a failure through it, so this is the one place
// that keeps a credential an error body echoes (a token in a 500 page) out of
// the logs. The body is redacted before it is cut: a credential across the
// cut would keep its first part.
func readErrorBody(resp *http.Response) string {
	body, err := io.ReadAll(io.LimitReader(resp.Body, errorBodyRead))
	if err != nil || len(body) == 0 {
		return ""
	}
	text := []rune(redact.String(string(body)))
	if len(text) > errorBodyLen {
		text = text[:errorBodyLen]
	}
	return ": " + string(text)
}
