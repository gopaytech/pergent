package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
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

	// Cancel context on Ctrl+C — kills opencode subprocess
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var skills stringSlice
	var platformFlag string
	var maxTurns int
	var maxTokens int
	var timeout time.Duration
	var repoPath string
	var localMode bool
	var baseBranch string
	var testMode bool

	flag.Var(&skills, "skill", "Preset name or path to skill .md file (repeatable)")
	flag.StringVar(&platformFlag, "platform", "", "Platform: github or gitlab")
	flag.IntVar(&maxTurns, "max-turns", 0, "Max agentic turns per skill run (default 20)")
	flag.IntVar(&maxTokens, "max-tokens", 0, "Max token usage per skill run (default 100000)")
	flag.DurationVar(&timeout, "timeout", 0, "Max wall-clock time per skill run (default 10m)")
	flag.StringVar(&repoPath, "repo-path", "", "Path to repo root (default .)")
	flag.BoolVar(&localMode, "local", false, "Local mode: diff from git, output to stdout, no platform")
	flag.StringVar(&baseBranch, "base-branch", "", "Base branch for git diff (default main in local mode)")
	flag.BoolVar(&testMode, "test", false, "Test opencode connection by sending a hello prompt")
	flag.Parse()

	if testMode {
		runTest(ctx)
		return
	}

	// Resolve config
	cfg, err := config.Resolve(config.Options{
		Skills:     skills,
		Platform:   platformFlag,
		MaxTurns:   maxTurns,
		MaxTokens:  maxTokens,
		Timeout:    timeout,
		RepoPath:   repoPath,
		Local:      localMode,
		BaseBranch: baseBranch,
	})
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	// Load skills
	var loadedSkills []skill.Skill
	for _, value := range cfg.Skills {
		s, err := skill.Resolve(value)
		if err != nil {
			log.Fatalf("loading skill %s: %v", value, err)
		}
		loadedSkills = append(loadedSkills, s)
	}

	// Gather diff
	var diff string
	var changedFiles []string
	var plat platform.Platform

	if cfg.Local {
		diff, changedFiles, err = platform.LocalDiff(cfg.RepoPath, "origin/"+cfg.BaseBranch)
		if err != nil {
			log.Fatalf("local git diff failed: %v", err)
		}
	} else {
		plat = newPlatform(cfg)
		diff, changedFiles, err = gatherDiff(cfg, plat)
		if err != nil {
			log.Fatalf("gathering diff: %v", err)
		}
	}

	diffFile, err := writeTempDiff(diff)
	if err != nil {
		log.Fatalf("writing diff: %v", err)
	}
	defer os.Remove(diffFile)

	// Run each skill with its own opencode config
	var results []runner.RunResult
	for _, s := range loadedSkills {
		fmt.Fprintf(os.Stderr, "Running skill: %s\n", s.Name)

		configPath, cleanup, err := config.GenerateOpenCodeConfig(cfg.MaxTurns, cfg.MaxTokens, s.Body)
		if err != nil {
			log.Fatalf("generating opencode config: %v", err)
		}

		result, err := runner.Run(ctx, s.Name, configPath, diffFile, "Review the attached diff", cfg.RepoPath, cfg.Timeout)
		cleanup()
		if err != nil {
			log.Printf("skill %s error: %v", s.Name, err)
			result = runner.RunResult{
				SkillName: s.Name,
				Output:    fmt.Sprintf("Error running skill: %v", err),
			}
		}
		results = append(results, result)
	}

	comment := output.FormatComment(results, changedFiles)

	if cfg.Local {
		fmt.Print(comment)
		return
	}

	// Post or update comment on platform
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
	// Prefer DiffBase SHA (exact merge base commit) over branch name
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

	// Try local git diff first
	if ref != "" {
		diff, files, err := platform.LocalDiff(cfg.RepoPath, ref)
		if err == nil && diff != "" {
			return diff, files, nil
		}
		fmt.Fprintf(os.Stderr, "Local git diff failed (%v), fetching from API\n", err)
	}

	// Fall back to platform API
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

func runTest(ctx context.Context) {
	fmt.Fprintf(os.Stderr, "Testing opencode connection...\n")

	configPath, cleanup, err := config.GenerateOpenCodeConfig(1, 10000, "")
	if err != nil {
		log.Fatalf("generating opencode config: %v", err)
	}
	defer cleanup()

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	args := []string{"run", "--format", "json", "--", "Reply with exactly: hello from opencode"}
	cmd := exec.CommandContext(ctx, "opencode", args...)
	cmd.Dir = "."

	env := os.Environ()
	if configPath != "" {
		env = append(env, "OPENCODE_CONFIG="+configPath)
	}
	cmd.Env = env

	var stdout bytes.Buffer
	cmd.Stdout = io.MultiWriter(&stdout, os.Stderr)
	cmd.Stderr = os.Stderr

	err = cmd.Run()

	output, _ := runner.ParseOutput(stdout.Bytes())

	if err != nil && output == "" {
		fmt.Fprintf(os.Stderr, "\nFAIL: %v\n", err)
		os.Exit(1)
	}

	if output == "" {
		fmt.Fprintf(os.Stderr, "\nFAIL: opencode returned empty response\n")
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "\nOK: %s\n", output)
}
