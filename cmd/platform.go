package main

import (
	"log"
	"strings"

	"github.com/gopaytech/pergent/internal/config"
	"github.com/gopaytech/pergent/internal/platform"
)

type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ", ") }
func (s *stringSlice) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func newPlatform(cfg config.Config) platform.Platform {
	switch cfg.Platform {
	case "github":
		return &platform.GitHub{
			Token:    cfg.GitHub.Token,
			Repo:     cfg.GitHub.Repo,
			PRNumber: cfg.GitHub.PRNumber,
		}
	case "gitlab":
		return &platform.GitLab{
			Token:     cfg.GitLab.Token,
			URL:       cfg.GitLab.URL,
			ProjectID: cfg.GitLab.ProjectID,
			MRIID:     cfg.GitLab.MRIID,
		}
	default:
		log.Fatalf("unsupported platform: %s", cfg.Platform)
		return nil
	}
}
