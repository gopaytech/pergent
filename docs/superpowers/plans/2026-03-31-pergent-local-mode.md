# pergent --local Mode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `--local` mode that runs skill reviews against a local git diff and prints the formatted review to stdout, with no platform interaction.

**Architecture:** Two changes: (1) config resolver learns `Local` and `BaseBranch` fields, skipping platform validation when local, (2) `cmd/main.go` branches on `cfg.Local` to use local diff + stdout output instead of platform posting.

**Tech Stack:** Go, existing codebase. No new dependencies.

---

## File Map

```
pergent/
├── internal/
│   └── config/
│       ├── resolver.go            # Modify: add Local + BaseBranch fields, local mode logic
│       └── resolver_test.go       # Modify: add local mode tests
├── cmd/
│   └── main.go                    # Modify: add --local and --base-branch flags, local mode flow
```

---

### Task 1: Config Resolver -- Local Mode Support

**Files:**
- Modify: `internal/config/resolver.go`
- Modify: `internal/config/resolver_test.go`

- [ ] **Step 1: Write failing tests for local mode**

Add the following tests to the end of `internal/config/resolver_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -v -run TestResolve_Local`

Expected: FAIL — `Local` and `BaseBranch` fields not defined on `Options`.

- [ ] **Step 3: Add Local and BaseBranch fields to Options and Config structs**

In `internal/config/resolver.go`, replace the `Options` struct (lines 11-18):

```go
type Options struct {
	Skills     []string
	Platform   string
	MaxTurns   int
	MaxTokens  int
	Timeout    time.Duration
	RepoPath   string
	Local      bool
	BaseBranch string
}
```

Replace the `Config` struct (lines 35-44):

```go
type Config struct {
	Skills     []string
	Platform   string
	MaxTurns   int
	MaxTokens  int
	Timeout    time.Duration
	RepoPath   string
	Local      bool
	BaseBranch string
	GitHub     GitHubConfig
	GitLab     GitLabConfig
}
```

- [ ] **Step 4: Add local mode logic to Resolve function**

In `internal/config/resolver.go`, replace the `Resolve` function (lines 46-89):

```go
func Resolve(opts Options) (Config, error) {
	if len(opts.Skills) == 0 {
		return Config{}, fmt.Errorf("at least one --skill is required")
	}

	if opts.Local && opts.Platform != "" {
		return Config{}, fmt.Errorf("--local and --platform are mutually exclusive")
	}

	cfg := Config{
		Skills:    opts.Skills,
		MaxTurns:  resolveInt(opts.MaxTurns, "PERGENT_MAX_TURNS", 20),
		MaxTokens: resolveInt(opts.MaxTokens, "PERGENT_MAX_TOKENS", 100000),
		Timeout:   resolveDuration(opts.Timeout, "PERGENT_TIMEOUT", 10*time.Minute),
		RepoPath:  resolveString(opts.RepoPath, "", "."),
		Local:     opts.Local,
	}

	if cfg.Local {
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

	return cfg, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/config/ -v`

Expected: All tests PASS (existing 12 + new 4 = 16 total).

- [ ] **Step 6: Commit**

```bash
git add internal/config/resolver.go internal/config/resolver_test.go
git commit -m "feat: add Local and BaseBranch support to config resolver"
```

---

### Task 2: CLI Integration -- Local Mode Flow

**Files:**
- Modify: `cmd/main.go`

- [ ] **Step 1: Add --local and --base-branch flags**

In `cmd/main.go`, add two new variables after the existing flag variables (after line 40 `var repoPath string`):

```go
	var localMode bool
	var baseBranch string
```

Add the flag registration after the existing `flag.StringVar` for `repoPath` (after line 47):

```go
	flag.BoolVar(&localMode, "local", false, "Local mode: diff from git, output to stdout, no platform")
	flag.StringVar(&baseBranch, "base-branch", "", "Base branch for git diff (default main in local mode)")
```

Add `Local` and `BaseBranch` to the `config.Resolve` call. Replace the `config.Resolve` call (lines 51-58):

```go
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
```

- [ ] **Step 2: Replace the main flow after skill loading with local/normal branching**

Replace everything from the `// Create platform client` comment through the end of `main()` (lines 74-134) with:

```go
	if cfg.Local {
		// Local mode: diff from git, output to stdout
		diff, changedFiles, err := platform.LocalDiff(cfg.RepoPath, cfg.BaseBranch)
		if err != nil {
			log.Fatalf("local git diff failed: %v", err)
		}

		diffFile, err := writeTempDiff(diff)
		if err != nil {
			log.Fatalf("writing diff: %v", err)
		}
		defer os.Remove(diffFile)

		configPath, cleanup, err := config.GenerateOpenCodeConfig(cfg.MaxTurns, cfg.MaxTokens)
		if err != nil {
			log.Fatalf("generating opencode config: %v", err)
		}
		defer cleanup()

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

		comment := output.FormatComment(results, changedFiles)
		fmt.Print(comment)
		return
	}

	// Normal mode: platform interaction
	plat := newPlatform(cfg)

	diff, changedFiles, err := gatherDiff(cfg, plat)
	if err != nil {
		log.Fatalf("gathering diff: %v", err)
	}

	diffFile, err := writeTempDiff(diff)
	if err != nil {
		log.Fatalf("writing diff: %v", err)
	}
	defer os.Remove(diffFile)

	configPath, cleanup, err := config.GenerateOpenCodeConfig(cfg.MaxTurns, cfg.MaxTokens)
	if err != nil {
		log.Fatalf("generating opencode config: %v", err)
	}
	defer cleanup()

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

	comment := output.FormatComment(results, changedFiles)

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
```

- [ ] **Step 3: Verify it compiles**

Run: `go build -o bin/pergent ./cmd/`

Expected: No errors.

- [ ] **Step 4: Verify --version still works**

Run: `./bin/pergent --version`

Expected: `pergent 0.1.0`

- [ ] **Step 5: Verify --local flag is accepted**

Run: `./bin/pergent --local --skill ./skills/code-review.md 2>&1 || true`

Expected: Either a git diff error (expected — no git repo initialized) or opencode error (expected — opencode not installed). The important thing is it does NOT error on config validation (no "GITHUB_TOKEN is required" etc).

- [ ] **Step 6: Commit**

```bash
git add cmd/main.go
git commit -m "feat: add --local mode for local diff + stdout output"
```

---

### Task 3: Run All Tests

- [ ] **Step 1: Run full test suite**

Run: `make test`

Expected: All tests pass:
```
ok  github.com/zufardhiyaulhaq/pergent/internal/config
ok  github.com/zufardhiyaulhaq/pergent/internal/output
ok  github.com/zufardhiyaulhaq/pergent/internal/platform
ok  github.com/zufardhiyaulhaq/pergent/internal/runner
ok  github.com/zufardhiyaulhaq/pergent/internal/skill
```

- [ ] **Step 2: Verify build**

Run: `make build && ./bin/pergent --version`

Expected: `pergent 0.1.0`

- [ ] **Step 3: Commit any fixes if tests failed**

If any test failed, fix and commit:
```bash
git add -A
git commit -m "fix: resolve test failures"
```
