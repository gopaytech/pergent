package main

import (
	"fmt"
	"os"

	"github.com/gopaytech/pergent/internal/config"
	"github.com/gopaytech/pergent/internal/platform"
)

func gatherDiff(cfg config.Config, plat platform.Platform) (string, []string, error) {
	var ref string
	switch cfg.Platform {
	case "github":
		if cfg.GitHub.DiffBase != "" {
			ref = cfg.GitHub.DiffBase
		} else if cfg.GitHub.BaseBranch != "" {
			ref = "origin/" + cfg.GitHub.BaseBranch
		}
	case "gitlab":
		if cfg.GitLab.DiffBase != "" {
			ref = cfg.GitLab.DiffBase
		} else if cfg.GitLab.BaseBranch != "" {
			ref = "origin/" + cfg.GitLab.BaseBranch
		}
	}

	if ref != "" {
		diff, files, err := platform.LocalDiff(cfg.RepoPath, ref)
		if err == nil {
			return diff, files, nil
		}
		fmt.Fprintf(os.Stderr, "Local git diff failed (%v), fetching from API\n", err)
	}

	return plat.FetchDiff()
}

func writeTempFile(pattern string, content string) (string, error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	f.Close()
	return f.Name(), nil
}
