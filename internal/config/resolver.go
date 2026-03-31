package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Options struct {
	Skills    []string
	Platform  string
	MaxTurns  int
	MaxTokens int
	Timeout   time.Duration
	RepoPath  string
}

type GitHubConfig struct {
	Token      string
	Repo       string
	PRNumber   int
	BaseBranch string
}

type GitLabConfig struct {
	Token      string
	URL        string
	ProjectID  string
	MRIID      int
	BaseBranch string
}

type Config struct {
	Skills    []string
	Platform  string
	MaxTurns  int
	MaxTokens int
	Timeout   time.Duration
	RepoPath  string
	GitHub    GitHubConfig
	GitLab    GitLabConfig
}

func Resolve(opts Options) (Config, error) {
	if len(opts.Skills) == 0 {
		return Config{}, fmt.Errorf("at least one --skill is required")
	}

	cfg := Config{
		Skills:    opts.Skills,
		MaxTurns:  resolveInt(opts.MaxTurns, "PERGENT_MAX_TURNS", 20),
		MaxTokens: resolveInt(opts.MaxTokens, "PERGENT_MAX_TOKENS", 100000),
		Timeout:   resolveDuration(opts.Timeout, "PERGENT_TIMEOUT", 10*time.Minute),
		RepoPath:  resolveString(opts.RepoPath, "", "."),
	}

	cfg.Platform = resolvePlatform(opts.Platform)

	switch cfg.Platform {
	case "github":
		cfg.GitHub = resolveGitHub()
		if cfg.GitHub.Token == "" {
			return Config{}, fmt.Errorf("GITHUB_TOKEN is required")
		}
		if cfg.GitHub.PRNumber == 0 {
			return Config{}, fmt.Errorf("could not detect PR number from GITHUB_EVENT_PATH")
		}
	case "gitlab":
		cfg.GitLab = resolveGitLab()
		if cfg.GitLab.Token == "" {
			return Config{}, fmt.Errorf("GITLAB_TOKEN is required")
		}
		if cfg.GitLab.MRIID == 0 {
			return Config{}, fmt.Errorf("could not detect MR IID from CI_MERGE_REQUEST_IID")
		}
		if cfg.GitLab.URL == "" {
			return Config{}, fmt.Errorf("could not detect GitLab URL from CI_SERVER_URL")
		}
		if cfg.GitLab.ProjectID == "" {
			return Config{}, fmt.Errorf("could not detect GitLab project ID from CI_PROJECT_ID")
		}
	case "":
		return Config{}, fmt.Errorf("could not detect platform: set --platform or PERGENT_PLATFORM, or run in GitHub Actions / GitLab CI")
	}

	return cfg, nil
}

func resolvePlatform(flag string) string {
	if flag != "" {
		return flag
	}
	if env := os.Getenv("PERGENT_PLATFORM"); env != "" {
		return env
	}
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		return "github"
	}
	if os.Getenv("CI_MERGE_REQUEST_IID") != "" {
		return "gitlab"
	}
	return ""
}

func resolveGitHub() GitHubConfig {
	cfg := GitHubConfig{
		Token: os.Getenv("GITHUB_TOKEN"),
		Repo:  os.Getenv("GITHUB_REPOSITORY"),
	}

	eventPath := os.Getenv("GITHUB_EVENT_PATH")
	if eventPath != "" {
		data, err := os.ReadFile(eventPath)
		if err == nil {
			var event struct {
				PullRequest struct {
					Number int    `json:"number"`
					Base   struct {
						Ref string `json:"ref"`
					} `json:"base"`
				} `json:"pull_request"`
			}
			if json.Unmarshal(data, &event) == nil {
				cfg.PRNumber = event.PullRequest.Number
				cfg.BaseBranch = event.PullRequest.Base.Ref
			}
		}
	}

	return cfg
}

func resolveGitLab() GitLabConfig {
	mriid, _ := strconv.Atoi(os.Getenv("CI_MERGE_REQUEST_IID"))
	return GitLabConfig{
		Token:      os.Getenv("GITLAB_TOKEN"),
		URL:        resolveString("", "", os.Getenv("CI_SERVER_URL")),
		ProjectID:  os.Getenv("CI_PROJECT_ID"),
		MRIID:      mriid,
		BaseBranch: os.Getenv("CI_MERGE_REQUEST_TARGET_BRANCH_NAME"),
	}
}

func resolveInt(flag int, envKey string, defaultVal int) int {
	if flag != 0 {
		return flag
	}
	if env := os.Getenv(envKey); env != "" {
		if v, err := strconv.Atoi(env); err == nil {
			return v
		}
	}
	return defaultVal
}

func resolveDuration(flag time.Duration, envKey string, defaultVal time.Duration) time.Duration {
	if flag != 0 {
		return flag
	}
	if env := os.Getenv(envKey); env != "" {
		if v, err := time.ParseDuration(env); err == nil {
			return v
		}
	}
	return defaultVal
}

func resolveString(flag string, envKey string, defaultVal string) string {
	if flag != "" {
		return flag
	}
	if envKey != "" {
		if env := os.Getenv(envKey); env != "" {
			return env
		}
	}
	return defaultVal
}
