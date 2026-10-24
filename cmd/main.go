package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gopaytech/pergent/internal/config"
	"github.com/gopaytech/pergent/internal/finding"
	"github.com/gopaytech/pergent/internal/output"
	"github.com/gopaytech/pergent/internal/platform"
	"github.com/gopaytech/pergent/internal/redact"
	"github.com/gopaytech/pergent/internal/review"
	"github.com/gopaytech/pergent/internal/skill"
)

const version = "0.1.0"

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
	var contextLimit int
	var concurrency int
	var timeout time.Duration
	var repoPath string
	var localMode bool
	var baseBranch string
	var testMode bool
	var previousReview bool

	flag.Var(&skills, "skill", "Preset name or path to skill .md file (repeatable)")
	flag.StringVar(&platformFlag, "platform", "", "Platform: github or gitlab")
	flag.IntVar(&maxTurns, "max-turns", 0, "Max agentic turns per opencode run (default 20)")
	flag.IntVar(&maxTokens, "max-tokens", 0, "Deprecated and ignored; see --context-limit")
	flag.IntVar(&contextLimit, "context-limit", 0, "Model context window in tokens, only for models opencode has no limits for (default: opencode's model registry)")
	flag.IntVar(&concurrency, "concurrency", 0, "Max opencode runs in flight at once, across all skills and batches (default 4)")
	flag.DurationVar(&timeout, "timeout", 0, "Max wall-clock time for the whole review (default 10m)")
	flag.StringVar(&repoPath, "repo-path", "", "Path to repo root (default .)")
	flag.BoolVar(&localMode, "local", false, "Local mode: diff from git, output to stdout, no platform")
	flag.StringVar(&baseBranch, "base-branch", "", "Base branch for git diff (default main in local mode)")
	flag.BoolVar(&testMode, "test", false, "Test opencode connection by sending a hello prompt")
	flag.BoolVar(&previousReview, "previous-review", false, "Attach pergent's previous review comment as context (platform mode only)")
	flag.Parse()

	previousReviewSet, concurrencySet := false, false
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "previous-review":
			previousReviewSet = true
		case "concurrency":
			concurrencySet = true
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
		ContextLimit:      contextLimit,
		Concurrency:       concurrency,
		ConcurrencySet:    concurrencySet,
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
	for _, w := range cfg.Warnings {
		log.Printf("warning: %s", w)
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

	const marker = "<!-- pergent -->"

	var prevCommentID int64
	var prevLookupFailed bool
	var previous map[string][]finding.Finding
	if cfg.PreviousReview {
		var prevBody string
		var lookupErr error
		prevCommentID, prevBody, lookupErr = plat.FindComment(marker)
		if lookupErr != nil {
			prevLookupFailed = true
			log.Printf("warning: could not fetch previous review comment: %v", lookupErr)
		} else {
			previous = previousFindings(prevBody)
		}
	}

	// Review the change with every skill. A failed review is reported in the
	// comment and the log but doesn't fail the job; only setup errors above
	// exit non-zero.
	fmt.Fprintf(os.Stderr, "Reviewing with %d skill(s), up to %d opencode runs at once, within %s\n", len(loadedSkills), cfg.Concurrency, cfg.Timeout)
	results := review.Run(ctx, review.Request{
		Skills:       loadedSkills,
		Diff:         diff,
		RepoPath:     cfg.RepoPath,
		Budget:       cfg.Timeout,
		Concurrency:  cfg.Concurrency,
		MaxTurns:     cfg.MaxTurns,
		ContextLimit: cfg.ContextLimit,
		Previous:     previous,
	})
	failed := 0
	for _, r := range results {
		if r.Verdict != review.Failed {
			continue
		}
		failed++
		for _, f := range r.Failures {
			log.Print(failureLogLine(r.Skill, f))
		}
	}
	if failed > 0 {
		log.Printf("%d of %d skill reviews failed; the failures are reported in the review comment", failed, len(results))
	}

	comment := output.FormatReview(results, changedFiles)

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

// failureLogLine renders a failed batch for the CI log, with the files it
// left unreviewed.
func failureLogLine(skillName string, f review.BatchFailure) string {
	if len(f.Files) == 0 {
		return fmt.Sprintf("skill %s: %s", skillName, output.BatchFailureText(f))
	}
	return fmt.Sprintf("skill %s, files %s: %s", skillName, redact.String(strings.Join(f.Files, ", ")), output.BatchFailureText(f))
}

// previousFindings reads the findings a previous pergent comment carries. A
// comment from an older pergent has none, which makes this a first review; a
// corrupt block is logged and also treated as a first review. Only skills
// with findings get an entry, so an empty list attaches nothing.
func previousFindings(body string) map[string][]finding.Finding {
	all, err := output.ParsePreviousFindings(body)
	if err != nil {
		log.Printf("warning: ignoring the previous review's findings: %v", err)
		return nil
	}
	previous := make(map[string][]finding.Finding)
	for name, fs := range all {
		if len(fs) > 0 {
			previous[name] = fs
		}
	}
	return previous
}
