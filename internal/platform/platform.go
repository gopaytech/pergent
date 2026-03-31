package platform

import (
	"fmt"
	"os/exec"
	"strings"
)

type Platform interface {
	FetchDiff() (diff string, changedFiles []string, err error)
	FindComment(marker string) (commentID int64, err error)
	CreateComment(body string) error
	UpdateComment(commentID int64, body string) error
}

func LocalDiff(repoPath string, baseBranch string) (diff string, changedFiles []string, err error) {
	if baseBranch == "" {
		return "", nil, fmt.Errorf("base branch is empty")
	}

	ref := "origin/" + baseBranch

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
