package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Options struct {
	Skills            []string
	Platform          string
	MaxTurns          int
	MaxTokens         int // deprecated: accepted for compatibility, ignored
	ContextLimit      int
	Concurrency       int
	ConcurrencySet    bool // true when the flag was explicitly passed on the command line
	Timeout           time.Duration
	RepoPath          string
	Local             bool
	BaseBranch        string
	PreviousReview    bool
	PreviousReviewSet bool // true when the flag was explicitly passed on the command line
}

type GitHubConfig struct {
	Token      string
	Repo       string
	PRNumber   int
	BaseBranch string
	DiffBase   string
	// HeadSHA is the PR's head commit (event pull_request.head.sha).
	HeadSHA string
	// WebURL is the repository's web address, for links to files; empty when
	// the repository is unknown.
	WebURL string
}

type GitLabConfig struct {
	Token      string
	URL        string
	ProjectID  string
	MRIID      int
	BaseBranch string
	DiffBase   string
	// HeadSHA is the merge request's source branch head
	// (CI_MERGE_REQUEST_SOURCE_BRANCH_SHA, set in merged-results pipelines,
	// where CI_COMMIT_SHA is a synthetic merge commit), else the commit the
	// pipeline runs on (CI_COMMIT_SHA).
	HeadSHA string
	// WebURL is the project's web address (CI_PROJECT_URL).
	WebURL string
}

type Config struct {
	Skills         []string
	Platform       string
	MaxTurns       int
	ContextLimit   int           // explicit model context window; 0 = use opencode's model registry
	Concurrency    int           // most opencode runs in flight at once
	Timeout        time.Duration // bounds the whole review, all skills and batches together
	RepoPath       string
	Local          bool
	BaseBranch     string
	PreviousReview bool
	// UseOpenCodeConfig is PERGENT_USE_OPENCODE_CONFIG: the opt-in to run on
	// opencode's own config when OPENCODE_PROVIDER is empty.
	UseOpenCodeConfig bool
	GitHub            GitHubConfig
	GitLab            GitLabConfig
	// Warnings are non-fatal configuration problems to print before running.
	Warnings []string
}

const maxTokensDeprecation = "--max-tokens / PERGENT_MAX_TOKENS is deprecated and ignored: opencode now uses the model's own context window. Use --context-limit / PERGENT_CONTEXT_LIMIT only if opencode doesn't know your model's limits (e.g. a custom OPENCODE_BASE_URL)."

func Resolve(opts Options) (Config, error) {
	if len(opts.Skills) == 0 {
		return Config{}, fmt.Errorf("at least one --skill is required")
	}

	if opts.Local && opts.Platform != "" {
		return Config{}, fmt.Errorf("--local and --platform are mutually exclusive")
	}

	cfg, err := resolveReview(opts)
	if err != nil {
		return Config{}, err
	}
	cfg.Skills = opts.Skills
	cfg.RepoPath = resolveString(opts.RepoPath, "", ".")
	cfg.Local = opts.Local

	cfg.PreviousReview, err = resolveBool(opts.PreviousReview, opts.PreviousReviewSet, "PERGENT_PREVIOUS_REVIEW", false)
	if err != nil {
		return Config{}, err
	}

	if cfg.Local && cfg.PreviousReview {
		return Config{}, fmt.Errorf("previous-review requires platform mode (cannot be used with --local)")
	}

	if cfg.UseOpenCodeConfig, err = resolveBool(false, false, "PERGENT_USE_OPENCODE_CONFIG", false); err != nil {
		return Config{}, err
	}
	// Without a provider pergent writes no opencode config, so none of the
	// sandbox applies: the model is not pergent's to choose either.
	noProvider := os.Getenv("OPENCODE_PROVIDER") == ""
	modelIgnored := noProvider && os.Getenv("OPENCODE_MODEL") != ""

	if cfg.Local {
		if modelIgnored {
			cfg.Warnings = append(cfg.Warnings, modelWithoutProvider+" "+modelWithoutProviderFix)
		}
		cfg.BaseBranch = opts.BaseBranch
		if cfg.BaseBranch == "" {
			cfg.BaseBranch = "main"
		}
		return cfg, nil
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

	if noProvider {
		if !cfg.UseOpenCodeConfig {
			// An unset CI variable expands to "", so a missing provider is
			// likelier a mistake than a choice. Running on, the review would
			// have no sandbox, with the platform token in opencode's
			// environment and the change's own AGENTS.md and opencode.json
			// applying.
			msg := "OPENCODE_PROVIDER is not set. Set OPENCODE_PROVIDER and OPENCODE_MODEL (and OPENCODE_API_KEY) so pergent runs the review in its read-only sandbox. To run on opencode's own config instead, set PERGENT_USE_OPENCODE_CONFIG=true: that mode has no sandbox (the change's own AGENTS.md and opencode.json apply and the platform token stays in opencode's environment), so use it only on changes you trust."
			if modelIgnored {
				msg += " OPENCODE_MODEL is set without OPENCODE_PROVIDER, which looks like a missing OPENCODE_PROVIDER."
			}
			return Config{}, errors.New(msg)
		}
		cfg.Warnings = append(cfg.Warnings, "PERGENT_USE_OPENCODE_CONFIG=true: the review runs on opencode's own config with no sandbox. The change's own AGENTS.md and opencode.json apply and the platform token stays in opencode's environment; do not use this on untrusted changes.")
		if modelIgnored {
			cfg.Warnings = append(cfg.Warnings, modelWithoutProvider+" "+modelWithoutProviderFix)
		}
	}

	return cfg, nil
}

const (
	modelWithoutProvider    = "OPENCODE_MODEL is set but OPENCODE_PROVIDER is not, so pergent ignores OPENCODE_MODEL and opencode uses its own config."
	modelWithoutProviderFix = "Set OPENCODE_PROVIDER too if you want pergent's sandboxed config."
)

// ResolveAgent resolves the review settings agent mode uses: Timeout,
// Concurrency, MaxTurns and ContextLimit (flags > PERGENT_* env > defaults,
// validated as Resolve does). It never detects a platform, and leaves every
// other field empty.
func ResolveAgent(opts Options) (Config, error) {
	return resolveReview(opts)
}

// resolveReview resolves and validates the settings shared by every mode:
// the review's limits, plus the --max-tokens deprecation warning.
func resolveReview(opts Options) (Config, error) {
	var cfg Config
	var err error
	if cfg.MaxTurns, err = resolveInt(opts.MaxTurns, "PERGENT_MAX_TURNS", 20); err != nil {
		return Config{}, err
	}
	if cfg.ContextLimit, err = resolveInt(opts.ContextLimit, "PERGENT_CONTEXT_LIMIT", 0); err != nil {
		return Config{}, err
	}
	if opts.ConcurrencySet {
		// An explicit flag is used as given, so 0 is an error rather than
		// "unset", and the variable is not read.
		cfg.Concurrency = opts.Concurrency
	} else if cfg.Concurrency, err = resolveInt(opts.Concurrency, "PERGENT_CONCURRENCY", 4); err != nil {
		return Config{}, err
	}
	if cfg.Timeout, err = resolveDuration(opts.Timeout, "PERGENT_TIMEOUT", 10*time.Minute); err != nil {
		return Config{}, err
	}

	if cfg.MaxTurns < 1 {
		return Config{}, fmt.Errorf("--max-turns must be at least 1, got %d", cfg.MaxTurns)
	}
	if cfg.ContextLimit < 0 {
		return Config{}, fmt.Errorf("--context-limit must be a positive number of tokens, got %d", cfg.ContextLimit)
	}
	if cfg.Timeout < 0 {
		return Config{}, fmt.Errorf("--timeout must be positive, got %s", cfg.Timeout)
	}
	if cfg.Concurrency < 1 {
		return Config{}, fmt.Errorf("--concurrency must be at least 1, got %d", cfg.Concurrency)
	}
	if opts.MaxTokens != 0 || os.Getenv("PERGENT_MAX_TOKENS") != "" {
		cfg.Warnings = append(cfg.Warnings, maxTokensDeprecation)
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

	if cfg.Repo != "" {
		server := strings.TrimRight(os.Getenv("GITHUB_SERVER_URL"), "/")
		if server == "" {
			server = "https://github.com"
		}
		cfg.WebURL = server + "/" + cfg.Repo
	}

	eventPath := os.Getenv("GITHUB_EVENT_PATH")
	if eventPath != "" {
		data, err := os.ReadFile(eventPath)
		if err == nil {
			var event struct {
				PullRequest struct {
					Number int `json:"number"`
					Head   struct {
						SHA string `json:"sha"`
					} `json:"head"`
					Base struct {
						Ref string `json:"ref"`
						SHA string `json:"sha"`
					} `json:"base"`
				} `json:"pull_request"`
			}
			if json.Unmarshal(data, &event) == nil {
				cfg.PRNumber = event.PullRequest.Number
				cfg.BaseBranch = event.PullRequest.Base.Ref
				cfg.DiffBase = event.PullRequest.Base.SHA
				cfg.HeadSHA = event.PullRequest.Head.SHA
			}
		}
	}

	return cfg
}

func resolveGitLab() GitLabConfig {
	mriid, _ := strconv.Atoi(os.Getenv("CI_MERGE_REQUEST_IID"))
	head := os.Getenv("CI_MERGE_REQUEST_SOURCE_BRANCH_SHA")
	if head == "" {
		head = os.Getenv("CI_COMMIT_SHA")
	}
	return GitLabConfig{
		Token:      os.Getenv("GITLAB_TOKEN"),
		URL:        os.Getenv("CI_SERVER_URL"),
		ProjectID:  os.Getenv("CI_PROJECT_ID"),
		MRIID:      mriid,
		BaseBranch: os.Getenv("CI_MERGE_REQUEST_TARGET_BRANCH_NAME"),
		DiffBase:   os.Getenv("CI_MERGE_REQUEST_DIFF_BASE_SHA"),
		HeadSHA:    head,
		WebURL:     os.Getenv("CI_PROJECT_URL"),
	}
}

// resolveInt returns flag, else the integer in envKey, else defaultVal. A zero
// flag and an empty variable mean unset; a variable that is not a whole
// number is an error.
func resolveInt(flag int, envKey string, defaultVal int) (int, error) {
	if flag != 0 {
		return flag, nil
	}
	if env := os.Getenv(envKey); env != "" {
		v, err := strconv.Atoi(env)
		if err != nil {
			return 0, fmt.Errorf("%s=%q is not a whole number", envKey, env)
		}
		return v, nil
	}
	return defaultVal, nil
}

// resolveDuration returns flag, else the duration in envKey, else defaultVal.
// A zero flag or env value means unset; a variable that is not a duration is
// an error.
func resolveDuration(flag time.Duration, envKey string, defaultVal time.Duration) (time.Duration, error) {
	if flag != 0 {
		return flag, nil
	}
	if env := os.Getenv(envKey); env != "" {
		v, err := time.ParseDuration(env)
		if err != nil {
			return 0, fmt.Errorf("%s=%q is not a duration such as 90s or 10m", envKey, env)
		}
		if v != 0 {
			return v, nil
		}
	}
	return defaultVal, nil
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

// resolveBool returns flagVal when the flag was set, else the boolean in
// envKey, else defaultVal. A variable that is not a boolean is an error.
func resolveBool(flagVal bool, flagSet bool, envKey string, defaultVal bool) (bool, error) {
	if flagSet {
		return flagVal, nil
	}
	if env := os.Getenv(envKey); env != "" {
		v, err := strconv.ParseBool(env)
		if err != nil {
			return false, fmt.Errorf("%s=%q is not true or false", envKey, env)
		}
		return v, nil
	}
	return defaultVal, nil
}
