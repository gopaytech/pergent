package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gopaytech/pergent/internal/config"
	"github.com/gopaytech/pergent/internal/output"
	"github.com/gopaytech/pergent/internal/platform"
	"github.com/gopaytech/pergent/internal/runner"
	"github.com/gopaytech/pergent/internal/skill"
)

const version = "0.1.0"

const reviewMessage = "Review the attached diff"
const reviewWithPrevMessage = "Review the attached diff. Also attached is your previous review of an earlier revision of this change. Stay consistent with it: focus on what changed since, don't re-litigate unchanged code, and drop findings that the new changes resolve."

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("pergent", version)
		return
	}

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
	var previousReview bool

	flag.Var(&skills, "skill", "Preset name or path to skill .md file (repeatable)")
	flag.StringVar(&platformFlag, "platform", "", "Platform: github or gitlab")
	flag.IntVar(&maxTurns, "max-turns", 0, "Max agentic turns per skill run (default 20)")
	flag.IntVar(&maxTokens, "max-tokens", 0, "Max token usage per skill run (default 100000)")
	flag.DurationVar(&timeout, "timeout", 0, "Max wall-clock time per skill run (default 10m)")
	flag.StringVar(&repoPath, "repo-path", "", "Path to repo root (default .)")
	flag.BoolVar(&localMode, "local", false, "Local mode: diff from git, output to stdout, no platform")
	flag.StringVar(&baseBranch, "base-branch", "", "Base branch for git diff (default main in local mode)")
	flag.BoolVar(&testMode, "test", false, "Test opencode connection by sending a hello prompt")
	flag.BoolVar(&previousReview, "previous-review", false, "Attach pergent's previous review comment as context (platform mode only)")
	flag.Parse()

	previousReviewSet := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "previous-review" {
			previousReviewSet = true
		}
	})

	if testMode {
		runTest(ctx)
		return
	}

	cfg, err := config.Resolve(config.Options{
		Skills:            skills,
		Platform:          platformFlag,
		MaxTurns:          maxTurns,
		MaxTokens:         maxTokens,
		Timeout:           timeout,
		RepoPath:          repoPath,
		Local:             localMode,
		BaseBranch:        baseBranch,
		PreviousReview:    previousReview,
		PreviousReviewSet: previousReviewSet,
	})
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

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

	diffFile, err := writeTempFile("pergent-diff-*.patch", diff)
	if err != nil {
		log.Fatalf("writing diff: %v", err)
	}
	defer os.Remove(diffFile)

	const marker = "<!-- pergent -->"

	var prevCommentID int64
	var prevBody string
	var prevLookupFailed bool
	if cfg.PreviousReview {
		var lookupErr error
		prevCommentID, prevBody, lookupErr = plat.FindComment(marker)
		if lookupErr != nil {
			prevLookupFailed = true
			log.Printf("warning: could not fetch previous review comment: %v", lookupErr)
		}
	}

	// Run each skill
	var results []runner.RunResult
	for _, s := range loadedSkills {
		fmt.Fprintf(os.Stderr, "Running skill: %s\n", s.Name)

		configPath, cleanup, err := config.GenerateOpenCodeConfig(cfg.MaxTurns, cfg.MaxTokens, s.Body)
		if err != nil {
			log.Fatalf("generating opencode config: %v", err)
		}

		message := reviewMessage
		prevReviewFile := ""
		if section := output.ExtractSkillSection(prevBody, s.Name); section != "" {
			f, err := writeTempFile("pergent-prev-review-*.md", section)
			if err != nil {
				log.Printf("warning: could not write previous review file: %v", err)
			} else {
				prevReviewFile = f
				message = reviewWithPrevMessage
				fmt.Fprintf(os.Stderr, "  Attaching previous review as context\n")
			}
		}

		result, err := runner.Run(ctx, s.Name, configPath, diffFile, prevReviewFile, message, cfg.RepoPath, cfg.Timeout)
		cleanup()
		if prevReviewFile != "" {
			os.Remove(prevReviewFile)
		}
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

	// Post or update comment
	commentID := prevCommentID
	if !cfg.PreviousReview || prevLookupFailed {
		var findErr error
		commentID, _, findErr = plat.FindComment(marker)
		if findErr != nil {
			log.Printf("warning: could not search for existing comment: %v", findErr)
		}
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
