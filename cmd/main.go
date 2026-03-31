package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/zufardhiyaulhaq/pergent/internal/config"
	"github.com/zufardhiyaulhaq/pergent/internal/output"
	"github.com/zufardhiyaulhaq/pergent/internal/platform"
	"github.com/zufardhiyaulhaq/pergent/internal/runner"
	"github.com/zufardhiyaulhaq/pergent/internal/skill"
)

const version = "0.1.0"

type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ", ") }
func (s *stringSlice) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("pergent", version)
		return
	}

	var skills stringSlice
	var platformFlag string
	var maxTurns int
	var maxTokens int
	var timeout time.Duration
	var repoPath string

	flag.Var(&skills, "skill", "Path to skill .md file (repeatable)")
	flag.StringVar(&platformFlag, "platform", "", "Platform: github or gitlab")
	flag.IntVar(&maxTurns, "max-turns", 0, "Max agentic turns per skill run (default 20)")
	flag.IntVar(&maxTokens, "max-tokens", 0, "Max token usage per skill run (default 100000)")
	flag.DurationVar(&timeout, "timeout", 0, "Max wall-clock time per skill run (default 10m)")
	flag.StringVar(&repoPath, "repo-path", "", "Path to repo root (default .)")
	flag.Parse()

	// Resolve config
	cfg, err := config.Resolve(config.Options{
		Skills:    skills,
		Platform:  platformFlag,
		MaxTurns:  maxTurns,
		MaxTokens: maxTokens,
		Timeout:   timeout,
		RepoPath:  repoPath,
	})
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	// Load skills
	var loadedSkills []skill.Skill
	for _, path := range cfg.Skills {
		s, err := skill.Load(path)
		if err != nil {
			log.Fatalf("loading skill %s: %v", path, err)
		}
		loadedSkills = append(loadedSkills, s)
	}

	// Create platform client
	plat := newPlatform(cfg)

	// Gather diff
	diff, changedFiles, err := gatherDiff(cfg, plat)
	if err != nil {
		log.Fatalf("gathering diff: %v", err)
	}

	// Write diff to temp file
	diffFile, err := writeTempDiff(diff)
	if err != nil {
		log.Fatalf("writing diff: %v", err)
	}
	defer os.Remove(diffFile)

	// Build opencode config
	configPath, cleanup, err := config.GenerateOpenCodeConfig(cfg.MaxTurns, cfg.MaxTokens)
	if err != nil {
		log.Fatalf("generating opencode config: %v", err)
	}
	defer cleanup()

	// Run each skill
	ctx := context.Background()
	var results []runner.RunResult
	for _, s := range loadedSkills {
		fmt.Fprintf(os.Stderr, "Running skill: %s\n", s.Name)
		result, err := runner.Run(ctx, s.Name, configPath, diffFile, s.Body, cfg.RepoPath, cfg.Timeout)
		if err != nil {
			log.Printf("skill %s error: %v", s.Name, err)
			result = runner.RunResult{
				SkillName: s.Name,
				Output:    fmt.Sprintf("Error running skill: %v", err),
			}
		}
		results = append(results, result)
	}

	// Format comment
	comment := output.FormatComment(results, changedFiles)

	// Post or update comment
	marker := "<!-- pergent -->"
	commentID, err := plat.FindComment(marker)
	if err != nil {
		log.Printf("warning: could not search for existing comment: %v", err)
	}

	if commentID != 0 {
		fmt.Fprintf(os.Stderr, "Updating existing comment %d\n", commentID)
		if err := plat.UpdateComment(commentID, comment); err != nil {
			log.Fatalf("updating comment: %v", err)
		}
	} else {
		fmt.Fprintf(os.Stderr, "Creating new comment\n")
		if err := plat.CreateComment(comment); err != nil {
			log.Fatalf("creating comment: %v", err)
		}
	}

	fmt.Fprintf(os.Stderr, "Done.\n")
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

func gatherDiff(cfg config.Config, plat platform.Platform) (string, []string, error) {
	var baseBranch string
	switch cfg.Platform {
	case "github":
		baseBranch = cfg.GitHub.BaseBranch
	case "gitlab":
		baseBranch = cfg.GitLab.BaseBranch
	}

	// Try local git diff first
	diff, files, err := platform.LocalDiff(cfg.RepoPath, baseBranch)
	if err == nil && diff != "" {
		return diff, files, nil
	}

	// Fall back to platform API
	fmt.Fprintf(os.Stderr, "Local git diff failed (%v), fetching from API\n", err)
	return plat.FetchDiff()
}

func writeTempDiff(diff string) (string, error) {
	f, err := os.CreateTemp("", "pergent-diff-*.patch")
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(diff); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	f.Close()
	return f.Name(), nil
}
