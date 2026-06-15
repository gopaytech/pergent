package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// helper to create a fake GitHub event file with a PR number and base branch
func setupGitHubEvent(t *testing.T, prNumber int, baseBranch string) {
	t.Helper()
	dir := t.TempDir()
	eventPath := filepath.Join(dir, "event.json")
	event := map[string]interface{}{
		"pull_request": map[string]interface{}{
			"number": prNumber,
			"base":   map[string]string{"ref": baseBranch},
		},
	}
	data, _ := json.Marshal(event)
	os.WriteFile(eventPath, data, 0644)
	t.Setenv("GITHUB_EVENT_PATH", eventPath)
}

func setupValidGitHub(t *testing.T) {
	t.Helper()
	t.Setenv("GITHUB_TOKEN", "ghp_test123")
	setupGitHubEvent(t, 1, "main")
}

func setupValidGitLab(t *testing.T) {
	t.Helper()
	t.Setenv("GITLAB_TOKEN", "glpat-test123")
	t.Setenv("CI_MERGE_REQUEST_IID", "42")
	t.Setenv("CI_SERVER_URL", "https://gitlab.example.com")
}

func TestResolve_Defaults(t *testing.T) {
	setupValidGitHub(t)

	cfg, err := Resolve(Options{
		Skills:   []string{"./skills/code-review.md"},
		Platform: "github",
	})
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if cfg.MaxTurns != 20 {
		t.Errorf("MaxTurns = %d, want 20", cfg.MaxTurns)
	}
	if cfg.MaxTokens != 100000 {
		t.Errorf("MaxTokens = %d, want 100000", cfg.MaxTokens)
	}
	if cfg.Timeout != 10*time.Minute {
		t.Errorf("Timeout = %v, want 10m", cfg.Timeout)
	}
	if cfg.RepoPath != "." {
		t.Errorf("RepoPath = %q, want %q", cfg.RepoPath, ".")
	}
}

func TestResolve_FlagOverridesEnv(t *testing.T) {
	setupValidGitHub(t)
	t.Setenv("PERGENT_MAX_TURNS", "50")

	cfg, err := Resolve(Options{
		Skills:   []string{"./skills/code-review.md"},
		Platform: "github",
		MaxTurns: 30,
	})
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if cfg.MaxTurns != 30 {
		t.Errorf("MaxTurns = %d, want 30 (flag should override env)", cfg.MaxTurns)
	}
}

func TestResolve_EnvOverridesDefault(t *testing.T) {
	setupValidGitHub(t)
	t.Setenv("PERGENT_MAX_TURNS", "50")

	cfg, err := Resolve(Options{
		Skills:   []string{"./skills/code-review.md"},
		Platform: "github",
	})
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if cfg.MaxTurns != 50 {
		t.Errorf("MaxTurns = %d, want 50 (from env)", cfg.MaxTurns)
	}
}

func TestResolve_AutoDetectGitHub(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_REPOSITORY", "owner/repo")
	t.Setenv("GITHUB_TOKEN", "ghp_test123")
	setupGitHubEvent(t, 42, "main")

	cfg, err := Resolve(Options{
		Skills: []string{"./skills/code-review.md"},
	})
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if cfg.Platform != "github" {
		t.Errorf("Platform = %q, want %q", cfg.Platform, "github")
	}
	if cfg.GitHub.Repo != "owner/repo" {
		t.Errorf("GitHub.Repo = %q, want %q", cfg.GitHub.Repo, "owner/repo")
	}
	if cfg.GitHub.Token != "ghp_test123" {
		t.Errorf("GitHub.Token not set from env")
	}
	if cfg.GitHub.PRNumber != 42 {
		t.Errorf("GitHub.PRNumber = %d, want 42", cfg.GitHub.PRNumber)
	}
}

func TestResolve_AutoDetectGitLab(t *testing.T) {
	t.Setenv("CI_MERGE_REQUEST_IID", "42")
	t.Setenv("CI_PROJECT_ID", "123")
	t.Setenv("CI_SERVER_URL", "https://gitlab.example.com")
	t.Setenv("CI_MERGE_REQUEST_TARGET_BRANCH_NAME", "main")
	t.Setenv("GITLAB_TOKEN", "glpat-test123")

	cfg, err := Resolve(Options{
		Skills: []string{"./skills/code-review.md"},
	})
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if cfg.Platform != "gitlab" {
		t.Errorf("Platform = %q, want %q", cfg.Platform, "gitlab")
	}
	if cfg.GitLab.MRIID != 42 {
		t.Errorf("GitLab.MRIID = %d, want 42", cfg.GitLab.MRIID)
	}
	if cfg.GitLab.ProjectID != "123" {
		t.Errorf("GitLab.ProjectID = %q, want %q", cfg.GitLab.ProjectID, "123")
	}
	if cfg.GitLab.BaseBranch != "main" {
		t.Errorf("GitLab.BaseBranch = %q, want %q", cfg.GitLab.BaseBranch, "main")
	}
}

func TestResolve_NoPlatform(t *testing.T) {
	_, err := Resolve(Options{
		Skills: []string{"./skills/code-review.md"},
	})
	if err == nil {
		t.Error("Resolve() should error when no platform can be detected")
	}
}

func TestResolve_NoSkills(t *testing.T) {
	_, err := Resolve(Options{
		Platform: "github",
	})
	if err == nil {
		t.Error("Resolve() should error when no skills provided")
	}
}

func TestResolve_GitHubMissingToken(t *testing.T) {
	setupGitHubEvent(t, 1, "main")
	// No GITHUB_TOKEN set

	_, err := Resolve(Options{
		Skills:   []string{"./skills/code-review.md"},
		Platform: "github",
	})
	if err == nil {
		t.Error("Resolve() should error when GITHUB_TOKEN is missing")
	}
}

func TestResolve_GitHubMissingPRNumber(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_test123")
	// No GITHUB_EVENT_PATH set, so PRNumber = 0

	_, err := Resolve(Options{
		Skills:   []string{"./skills/code-review.md"},
		Platform: "github",
	})
	if err == nil {
		t.Error("Resolve() should error when PR number cannot be detected")
	}
}

func TestResolve_GitLabMissingToken(t *testing.T) {
	t.Setenv("CI_MERGE_REQUEST_IID", "42")
	t.Setenv("CI_SERVER_URL", "https://gitlab.example.com")
	// No GITLAB_TOKEN set

	_, err := Resolve(Options{
		Skills:   []string{"./skills/code-review.md"},
		Platform: "gitlab",
	})
	if err == nil {
		t.Error("Resolve() should error when GITLAB_TOKEN is missing")
	}
}

func TestResolve_GitLabMissingURL(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "glpat-test123")
	t.Setenv("CI_MERGE_REQUEST_IID", "42")
	// No CI_SERVER_URL set

	_, err := Resolve(Options{
		Skills:   []string{"./skills/code-review.md"},
		Platform: "gitlab",
	})
	if err == nil {
		t.Error("Resolve() should error when GitLab URL cannot be detected")
	}
}

func TestResolve_GitLabMissingProjectID(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "glpat-test123")
	t.Setenv("CI_MERGE_REQUEST_IID", "42")
	t.Setenv("CI_SERVER_URL", "https://gitlab.example.com")
	// No CI_PROJECT_ID set

	_, err := Resolve(Options{
		Skills:   []string{"./skills/code-review.md"},
		Platform: "gitlab",
	})
	if err == nil {
		t.Error("Resolve() should error when GitLab project ID cannot be detected")
	}
}

func TestResolve_LocalMode(t *testing.T) {
	cfg, err := Resolve(Options{
		Skills: []string{"./skills/code-review.md"},
		Local:  true,
	})
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if !cfg.Local {
		t.Error("Local should be true")
	}
	if cfg.BaseBranch != "main" {
		t.Errorf("BaseBranch = %q, want %q (default)", cfg.BaseBranch, "main")
	}
	if cfg.Platform != "" {
		t.Errorf("Platform = %q, want empty in local mode", cfg.Platform)
	}
}

func TestResolve_LocalModeCustomBaseBranch(t *testing.T) {
	cfg, err := Resolve(Options{
		Skills:     []string{"./skills/code-review.md"},
		Local:      true,
		BaseBranch: "develop",
	})
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if cfg.BaseBranch != "develop" {
		t.Errorf("BaseBranch = %q, want %q", cfg.BaseBranch, "develop")
	}
}

func TestResolve_LocalWithPlatformError(t *testing.T) {
	_, err := Resolve(Options{
		Skills:   []string{"./skills/code-review.md"},
		Local:    true,
		Platform: "github",
	})
	if err == nil {
		t.Error("Resolve() should error when --local and --platform are both set")
	}
}

func TestResolve_LocalNoSkillsError(t *testing.T) {
	_, err := Resolve(Options{
		Local: true,
	})
	if err == nil {
		t.Error("Resolve() should error when no skills provided in local mode")
	}
}

func TestResolve_PreviousReviewDefault(t *testing.T) {
	setupValidGitHub(t)

	cfg, err := Resolve(Options{
		Skills:   []string{"./skills/code-review.md"},
		Platform: "github",
	})
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if cfg.PreviousReview {
		t.Error("PreviousReview should default to false")
	}
}

func TestResolve_PreviousReviewFlag(t *testing.T) {
	setupValidGitHub(t)

	cfg, err := Resolve(Options{
		Skills:            []string{"./skills/code-review.md"},
		Platform:          "github",
		PreviousReview:    true,
		PreviousReviewSet: true,
	})
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if !cfg.PreviousReview {
		t.Error("PreviousReview should be true when flag is set")
	}
}

func TestResolve_PreviousReviewEnvTrue(t *testing.T) {
	setupValidGitHub(t)
	t.Setenv("PERGENT_PREVIOUS_REVIEW", "true")

	cfg, err := Resolve(Options{
		Skills:   []string{"./skills/code-review.md"},
		Platform: "github",
	})
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if !cfg.PreviousReview {
		t.Error("PreviousReview should be true from env")
	}
}

func TestResolve_PreviousReviewEnvFalse(t *testing.T) {
	setupValidGitHub(t)
	t.Setenv("PERGENT_PREVIOUS_REVIEW", "false")

	cfg, err := Resolve(Options{
		Skills:   []string{"./skills/code-review.md"},
		Platform: "github",
	})
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if cfg.PreviousReview {
		t.Error("PreviousReview should be false from env")
	}
}

func TestResolve_PreviousReviewExplicitFlagBeatsEnv(t *testing.T) {
	setupValidGitHub(t)
	t.Setenv("PERGENT_PREVIOUS_REVIEW", "true")

	cfg, err := Resolve(Options{
		Skills:            []string{"./skills/code-review.md"},
		Platform:          "github",
		PreviousReview:    false,
		PreviousReviewSet: true, // --previous-review=false passed explicitly
	})
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if cfg.PreviousReview {
		t.Error("explicit --previous-review=false should beat env true")
	}
}

func TestResolve_PreviousReviewEnvGarbage(t *testing.T) {
	setupValidGitHub(t)
	t.Setenv("PERGENT_PREVIOUS_REVIEW", "yes")

	cfg, err := Resolve(Options{
		Skills:   []string{"./skills/code-review.md"},
		Platform: "github",
	})
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if cfg.PreviousReview {
		t.Error("unparseable env value should fall through to default false")
	}
}

func TestResolve_PreviousReviewLocalError(t *testing.T) {
	_, err := Resolve(Options{
		Skills:            []string{"./skills/code-review.md"},
		Local:             true,
		PreviousReview:    true,
		PreviousReviewSet: true,
	})
	if err == nil {
		t.Error("Resolve() should error when --previous-review is combined with --local")
	}
}

func TestResolve_PreviousReviewEnvLocalError(t *testing.T) {
	t.Setenv("PERGENT_PREVIOUS_REVIEW", "true")
	_, err := Resolve(Options{
		Skills: []string{"./skills/code-review.md"},
		Local:  true,
	})
	if err == nil {
		t.Error("Resolve() should error when PERGENT_PREVIOUS_REVIEW=true combined with --local")
	}
}
