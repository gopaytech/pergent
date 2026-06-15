# pergent Previous Review Context Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> **NOTE: Do NOT commit during this plan.** Per user instruction, the user handles all commits themselves. Tasks end at "run tests and verify they pass" — there are no commit steps.

**Goal:** When re-reviewing a PR/MR, pergent fetches its own previous review comment from the platform API and attaches it to the opencode run as context, so re-runs are consistent, cheaper, and focused on what changed.

**Architecture:** Opt-in via `--previous-review` flag / `PERGENT_PREVIOUS_REVIEW` env. The existing `FindComment` lookup moves to the start of the run and now also returns the comment body (it already downloads bodies while searching — zero extra API calls). Per skill, pergent extracts that skill's section via the existing `<!-- pergent:NAME -->` markers, writes it to a temp file, and passes a second `--file` to `opencode run`. At post time the comment ID from the initial lookup is reused. pergent stays stateless — the MR comment is the only persistent state.

**Tech Stack:** Go standard library only (`flag.Visit`, `strconv.ParseBool`, `strings.Index`). No new dependencies.

**Spec:** `docs/superpowers/specs/2026-06-12-pergent-previous-review-design.md`

---

## File Map

```
internal/config/
├── resolver.go            # Modify: PreviousReview fields + resolveBool + local-mode exclusion
└── resolver_test.go       # Modify: precedence-table tests
internal/platform/
├── platform.go            # Modify: FindComment interface signature gains body return
├── github.go              # Modify: FindComment returns body
├── github_test.go         # Modify: update signatures, assert body
├── gitlab.go              # Modify: FindComment returns body
└── gitlab_test.go         # Modify: update signatures, assert body, add not-found test
internal/output/
├── formatter.go           # Modify: add ExtractSkillSection
└── formatter_test.go      # Modify: ExtractSkillSection tests
internal/runner/
├── runner.go              # Modify: BuildCommand/Run gain prevReviewFile param
└── runner_test.go         # Modify: update signatures, add prev-review-file test
cmd/
├── main.go                # Modify: flag, flag.Visit, early lookup, temp file, message, ID reuse
└── diff.go                # Modify: generalize writeTempDiff -> writeTempFile
README.md                  # Modify: flag table, env table, GitLab CI example
```

Task ordering keeps the build green after every task: interface changes (Tasks 2, 4) include their `cmd/main.go` call-site fixes.

---

### Task 1: Config Resolver — `PreviousReview` Support

**Files:**
- Modify: `internal/config/resolver.go`
- Test: `internal/config/resolver_test.go`

- [ ] **Step 1: Write failing tests**

Add to the end of `internal/config/resolver_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run TestResolve_PreviousReview -v`
Expected: compile error — `Options` has no field `PreviousReview`

- [ ] **Step 3: Implement**

In `internal/config/resolver.go`:

Add to the `Options` struct (after `BaseBranch string`):

```go
	PreviousReview    bool
	PreviousReviewSet bool // true when the flag was explicitly passed on the command line
```

Add to the `Config` struct (after `BaseBranch string`):

```go
	PreviousReview bool
```

In `Resolve`, after the `cfg := Config{...}` literal and **before** the `if cfg.Local {` block, add:

```go
	cfg.PreviousReview = resolveBool(opts.PreviousReview, opts.PreviousReviewSet, "PERGENT_PREVIOUS_REVIEW", false)

	if cfg.Local && cfg.PreviousReview {
		return Config{}, fmt.Errorf("--previous-review requires platform mode")
	}
```

Add at the end of the file, next to the other resolve helpers:

```go
func resolveBool(flagVal bool, flagSet bool, envKey string, defaultVal bool) bool {
	if flagSet {
		return flagVal
	}
	if env := os.Getenv(envKey); env != "" {
		if v, err := strconv.ParseBool(env); err == nil {
			return v
		}
	}
	return defaultVal
}
```

(`os` and `strconv` are already imported.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: all PASS (including pre-existing tests)

---

### Task 2: Platform — `FindComment` Returns the Comment Body

**Files:**
- Modify: `internal/platform/platform.go:17` (interface)
- Modify: `internal/platform/github.go:67-104`
- Modify: `internal/platform/gitlab.go:68-105`
- Modify: `cmd/main.go:135` (call site, minimal fix to keep build green)
- Test: `internal/platform/github_test.go`, `internal/platform/gitlab_test.go`

- [ ] **Step 1: Update tests to the new signature and assert the body**

In `internal/platform/github_test.go`, replace `TestGitHub_FindComment` and `TestGitHub_FindComment_NotFound`:

```go
func TestGitHub_FindComment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[
			{"id": 100, "body": "some other comment"},
			{"id": 200, "body": "<!-- pergent -->\n## pergent review\nstuff"}
		]`))
	}))
	defer server.Close()

	gh := &GitHub{
		Token:    "test-token",
		Repo:     "owner/repo",
		PRNumber: 42,
		APIURL:   server.URL,
	}

	id, body, err := gh.FindComment("<!-- pergent -->")
	if err != nil {
		t.Fatalf("FindComment() error: %v", err)
	}
	if id != 200 {
		t.Errorf("id = %d, want 200", id)
	}
	if body != "<!-- pergent -->\n## pergent review\nstuff" {
		t.Errorf("body = %q, want the matched comment body", body)
	}
}

func TestGitHub_FindComment_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id": 100, "body": "some other comment"}]`))
	}))
	defer server.Close()

	gh := &GitHub{
		Token:    "test-token",
		Repo:     "owner/repo",
		PRNumber: 42,
		APIURL:   server.URL,
	}

	id, body, err := gh.FindComment("<!-- pergent -->")
	if err != nil {
		t.Fatalf("FindComment() error: %v", err)
	}
	if id != 0 {
		t.Errorf("id = %d, want 0 (not found)", id)
	}
	if body != "" {
		t.Errorf("body = %q, want empty (not found)", body)
	}
}
```

In `internal/platform/gitlab_test.go`, replace `TestGitLab_FindComment` and add a not-found test:

```go
func TestGitLab_FindComment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[
			{"id": 100, "body": "some other comment"},
			{"id": 200, "body": "<!-- pergent -->\n## pergent review\nstuff"}
		]`))
	}))
	defer server.Close()

	gl := &GitLab{
		Token:     "test-token",
		URL:       server.URL,
		ProjectID: "123",
		MRIID:     42,
	}

	id, body, err := gl.FindComment("<!-- pergent -->")
	if err != nil {
		t.Fatalf("FindComment() error: %v", err)
	}
	if id != 200 {
		t.Errorf("id = %d, want 200", id)
	}
	if body != "<!-- pergent -->\n## pergent review\nstuff" {
		t.Errorf("body = %q, want the matched comment body", body)
	}
}

func TestGitLab_FindComment_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id": 100, "body": "some other comment"}]`))
	}))
	defer server.Close()

	gl := &GitLab{
		Token:     "test-token",
		URL:       server.URL,
		ProjectID: "123",
		MRIID:     42,
	}

	id, body, err := gl.FindComment("<!-- pergent -->")
	if err != nil {
		t.Fatalf("FindComment() error: %v", err)
	}
	if id != 0 {
		t.Errorf("id = %d, want 0 (not found)", id)
	}
	if body != "" {
		t.Errorf("body = %q, want empty (not found)", body)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/platform/ -run FindComment -v`
Expected: compile error — `FindComment` returns 2 values, test expects 3

- [ ] **Step 3: Implement**

In `internal/platform/platform.go`, change the interface method:

```go
type Platform interface {
	FetchDiff() (diff string, changedFiles []string, err error)
	FindComment(marker string) (commentID int64, body string, err error)
	CreateComment(body string) error
	UpdateComment(commentID int64, body string) error
}
```

In `internal/platform/github.go`, change `FindComment`:

```go
func (g *GitHub) FindComment(marker string) (int64, string, error) {
	page := 1
	for {
		path := fmt.Sprintf("/repos/%s/issues/%d/comments?per_page=100&page=%d", g.Repo, g.PRNumber, page)
		resp, err := g.do("GET", path, "", nil)
		if err != nil {
			return 0, "", fmt.Errorf("listing comments: %w", err)
		}

		if resp.StatusCode != 200 {
			errBody := readErrorBody(resp)
			resp.Body.Close()
			return 0, "", fmt.Errorf("listing comments: status %d%s", resp.StatusCode, errBody)
		}

		var comments []struct {
			ID   int64  `json:"id"`
			Body string `json:"body"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&comments); err != nil {
			resp.Body.Close()
			return 0, "", fmt.Errorf("decoding comments: %w", err)
		}
		resp.Body.Close()

		for _, c := range comments {
			if strings.Contains(c.Body, marker) {
				return c.ID, c.Body, nil
			}
		}

		if len(comments) < 100 {
			break
		}
		page++
	}
	return 0, "", nil
}
```

In `internal/platform/gitlab.go`, change `FindComment` the same way:

```go
func (gl *GitLab) FindComment(marker string) (int64, string, error) {
	page := 1
	for {
		path := fmt.Sprintf("/api/v4/projects/%s/merge_requests/%d/notes?per_page=100&page=%d", gl.ProjectID, gl.MRIID, page)
		resp, err := gl.do("GET", path, nil)
		if err != nil {
			return 0, "", fmt.Errorf("listing notes: %w", err)
		}

		if resp.StatusCode != 200 {
			errBody := readErrorBody(resp)
			resp.Body.Close()
			return 0, "", fmt.Errorf("listing notes: status %d%s", resp.StatusCode, errBody)
		}

		var notes []struct {
			ID   int64  `json:"id"`
			Body string `json:"body"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&notes); err != nil {
			resp.Body.Close()
			return 0, "", fmt.Errorf("decoding notes: %w", err)
		}
		resp.Body.Close()

		for _, n := range notes {
			if strings.Contains(n.Body, marker) {
				return n.ID, n.Body, nil
			}
		}

		if len(notes) < 100 {
			break
		}
		page++
	}
	return 0, "", nil
}
```

In `cmd/main.go`, fix the call site (around line 135) so the whole module still compiles — the body is wired up properly in Task 5:

```go
	commentID, _, err := plat.FindComment(marker)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/platform/ -v && go build ./...`
Expected: all PASS, build succeeds

---

### Task 3: Output — `ExtractSkillSection`

**Files:**
- Modify: `internal/output/formatter.go`
- Test: `internal/output/formatter_test.go`

- [ ] **Step 1: Write failing tests**

Add to the end of `internal/output/formatter_test.go`:

```go
func TestExtractSkillSection_Found(t *testing.T) {
	body := "<!-- pergent -->\n## pergent review\n\n" +
		"<!-- pergent:code-review -->\n### code-review\n\nFinding A\n<!-- /pergent:code-review -->\n\n" +
		"<!-- pergent:security-review -->\n### security-review\n\nFinding B\n<!-- /pergent:security-review -->\n"

	section := ExtractSkillSection(body, "code-review")

	if !strings.Contains(section, "Finding A") {
		t.Errorf("section = %q, should contain code-review finding", section)
	}
	if strings.Contains(section, "Finding B") {
		t.Errorf("section = %q, should not contain other skill's finding", section)
	}
	if strings.Contains(section, "<!-- pergent:code-review -->") {
		t.Errorf("section = %q, should not include the markers themselves", section)
	}
}

func TestExtractSkillSection_Missing(t *testing.T) {
	body := "<!-- pergent -->\n## pergent review\n\n" +
		"<!-- pergent:code-review -->\n### code-review\n\nFinding A\n<!-- /pergent:code-review -->\n"

	if section := ExtractSkillSection(body, "security-review"); section != "" {
		t.Errorf("section = %q, want empty for missing skill", section)
	}
}

func TestExtractSkillSection_MissingEndMarker(t *testing.T) {
	body := "<!-- pergent:code-review -->\n### code-review\n\nFinding A\n"

	if section := ExtractSkillSection(body, "code-review"); section != "" {
		t.Errorf("section = %q, want empty when end marker is missing", section)
	}
}

func TestExtractSkillSection_EmptyBody(t *testing.T) {
	if section := ExtractSkillSection("", "code-review"); section != "" {
		t.Errorf("section = %q, want empty for empty body", section)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/output/ -run TestExtractSkillSection -v`
Expected: compile error — `ExtractSkillSection` undefined

- [ ] **Step 3: Implement**

Add to the end of `internal/output/formatter.go`:

```go
// ExtractSkillSection returns the content between the per-skill markers
// that FormatComment writes (<!-- pergent:NAME --> ... <!-- /pergent:NAME -->).
// Returns "" when either marker is missing.
func ExtractSkillSection(body string, skillName string) string {
	start := fmt.Sprintf("<!-- pergent:%s -->", skillName)
	end := fmt.Sprintf("<!-- /pergent:%s -->", skillName)

	startIdx := strings.Index(body, start)
	if startIdx == -1 {
		return ""
	}
	rest := body[startIdx+len(start):]
	endIdx := strings.Index(rest, end)
	if endIdx == -1 {
		return ""
	}
	return strings.TrimSpace(rest[:endIdx])
}
```

(`fmt` and `strings` are already imported.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/output/ -v`
Expected: all PASS

---

### Task 4: Runner — Attach a Second `--file`

**Files:**
- Modify: `internal/runner/runner.go:18-44`
- Modify: `cmd/main.go:114` and `cmd/test.go` (no change needed in test.go — it builds its own command; verify only)
- Test: `internal/runner/runner_test.go`

- [ ] **Step 1: Update tests to the new signature and add the prev-review case**

In `internal/runner/runner_test.go`, update the three existing `BuildCommand` calls to pass an empty `prevReviewFile` (new 4th argument, after `diffFile`):

```go
	cmd := BuildCommand(ctx, "/tmp/config.json", "/tmp/diff.patch", "", "Review the attached diff", ".")
```

```go
	cmd := BuildCommand(ctx, "/tmp/config.json", "", "", "Say hello", ".")
```

```go
	cmd := BuildCommand(ctx, "", "/tmp/diff.patch", "", "Review", ".")
```

Then add new tests at the end of the file:

```go
func TestBuildCommand_WithPrevReviewFile(t *testing.T) {
	ctx := context.Background()
	cmd := BuildCommand(ctx, "/tmp/config.json", "/tmp/diff.patch", "/tmp/prev-review.md", "Review the attached diff", ".")

	hasDiffFile := false
	hasPrevFile := false
	for i, arg := range cmd.Args {
		if arg == "--file" && i+1 < len(cmd.Args) && cmd.Args[i+1] == "/tmp/diff.patch" {
			hasDiffFile = true
		}
		if arg == "--file" && i+1 < len(cmd.Args) && cmd.Args[i+1] == "/tmp/prev-review.md" {
			hasPrevFile = true
		}
	}
	if !hasDiffFile {
		t.Error("missing --file flag for diff")
	}
	if !hasPrevFile {
		t.Error("missing --file flag for previous review")
	}
}

func TestBuildCommand_NoPrevReviewFile(t *testing.T) {
	ctx := context.Background()
	cmd := BuildCommand(ctx, "/tmp/config.json", "/tmp/diff.patch", "", "Review the attached diff", ".")

	fileCount := 0
	for _, arg := range cmd.Args {
		if arg == "--file" {
			fileCount++
		}
	}
	if fileCount != 1 {
		t.Errorf("--file count = %d, want 1 (diff only)", fileCount)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runner/ -v`
Expected: compile error — `BuildCommand` takes 5 arguments, tests pass 6

- [ ] **Step 3: Implement**

In `internal/runner/runner.go`, change `BuildCommand`:

```go
func BuildCommand(ctx context.Context, configPath string, diffFile string, prevReviewFile string, message string, repoPath string) *exec.Cmd {
	args := []string{
		"run",
		"--format", "json",
	}
	if diffFile != "" {
		args = append(args, "--file", diffFile)
	}
	if prevReviewFile != "" {
		args = append(args, "--file", prevReviewFile)
	}
	args = append(args, "--", message)

	cmd := exec.CommandContext(ctx, "opencode", args...)
	cmd.Dir = repoPath

	env := os.Environ()
	if configPath != "" {
		env = append(env, "OPENCODE_CONFIG="+configPath)
	}
	cmd.Env = env

	return cmd
}
```

Change `Run`'s signature and its `BuildCommand` call:

```go
func Run(ctx context.Context, skillName string, configPath string, diffFile string, prevReviewFile string, message string, repoPath string, timeout time.Duration) (RunResult, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := BuildCommand(ctx, configPath, diffFile, prevReviewFile, message, repoPath)
```

(rest of `Run` unchanged)

In `cmd/main.go`, fix the call site (around line 114) so the module compiles — the real value is wired in Task 5:

```go
		result, err := runner.Run(ctx, s.Name, configPath, diffFile, "", "Review the attached diff", cfg.RepoPath, cfg.Timeout)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runner/ -v && go build ./...`
Expected: all PASS, build succeeds

---

### Task 5: `cmd/main.go` Orchestration

**Files:**
- Modify: `cmd/main.go`
- Modify: `cmd/diff.go:39-51` (generalize `writeTempDiff`)

No unit tests in this task — `cmd/main.go` is thin orchestration over the units tested in Tasks 1–4. Verification is `go build` + `go vet` + a `--local` smoke run.

- [ ] **Step 1: Generalize the temp-file helper**

In `cmd/diff.go`, replace `writeTempDiff` with a general helper (the diff call site moves to it):

```go
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
```

- [ ] **Step 2: Add the flag and `flag.Visit` detection**

In `cmd/main.go`, add to the flag declarations (after `var testMode bool`):

```go
	var previousReview bool
```

After the existing `flag.BoolVar(&testMode, ...)` line:

```go
	flag.BoolVar(&previousReview, "previous-review", false, "Attach pergent's previous review comment as context (platform mode only)")
```

Immediately after `flag.Parse()`:

```go
	previousReviewSet := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "previous-review" {
			previousReviewSet = true
		}
	})
```

And pass both into `config.Resolve`:

```go
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
```

- [ ] **Step 3: Fetch the previous review at the start, update the diff temp-file call**

Replace the existing diff temp-file call:

```go
	diffFile, err := writeTempFile("pergent-diff-*.patch", diff)
	if err != nil {
		log.Fatalf("writing diff: %v", err)
	}
	defer os.Remove(diffFile)
```

After that block (and before the "Run each skill" loop), add the early lookup. Note `marker` moves up here from its old location near posting:

```go
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
```

(`cfg.PreviousReview` is only ever true in platform mode — the resolver rejects it with `--local` — so `plat` is always non-nil here.)

- [ ] **Step 4: Per-skill extraction, attachment, and message selection**

Add this constant near the top of `cmd/main.go` (next to `const version`):

```go
const reviewMessage = "Review the attached diff"
const reviewWithPrevMessage = "Review the attached diff. Also attached is your previous review of an earlier revision of this change. Stay consistent with it: focus on what changed since, don't re-litigate unchanged code, and drop findings that the new changes resolve."
```

Replace the body of the per-skill loop (between `fmt.Fprintf(os.Stderr, "Running skill: ...")` and `results = append(results, result)`):

```go
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
```

(`ExtractSkillSection("")` returns "" — first runs and disabled mode skip the attachment without special-casing.)

- [ ] **Step 5: Reuse the comment ID at post time**

Replace the posting block (which previously declared `marker :=` and did the only `FindComment` call):

```go
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
```

Semantics: feature disabled → exactly today's flow (one lookup at post time). Feature enabled and the early lookup succeeded → reuse its ID, zero extra calls. Feature enabled but the early lookup errored → retry at post time so a transient API blip doesn't create a duplicate comment.

- [ ] **Step 6: Build, vet, and full test suite**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: build succeeds, vet clean, all tests PASS

- [ ] **Step 7: Smoke-test the flag wiring**

Run: `make build && ./bin/pergent --local --previous-review --skill code-review`
Expected: exits with `config error: --previous-review requires platform mode`

Run: `./bin/pergent --previous-review --skill code-review`
Expected: exits with the platform-detection error (`could not detect platform: ...`) — proves the flag parses and local-mode exclusion only triggers with `--local`

---

### Task 6: Documentation

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Add the flag to the CLI Flags table**

In the `## CLI Flags` table, add after the `--test` row:

```markdown
| `--previous-review` | Attach pergent's previous review comment as context on re-runs (platform mode only) | `false` |
```

- [ ] **Step 2: Add the env var to the Limits table**

In the `### Limits (alternative to flags)` table, add after the `PERGENT_TIMEOUT` row:

```markdown
| `PERGENT_PREVIOUS_REVIEW` | `true`/`false` — attach the previous review comment as context (explicit `--previous-review` flag wins over this) |
```

- [ ] **Step 3: Document the behavior**

Add a new section after `## Skills` (before `## CLI Flags`):

```markdown
## Previous Review as Context

By default each run reviews from scratch. With `--previous-review` (or `PERGENT_PREVIOUS_REVIEW=true`), pergent fetches the review comment it posted on the previous run, extracts each skill's own section, and attaches it to the agent alongside the diff. The agent is instructed to stay consistent with its earlier findings, focus on what changed since, and drop findings the new commits resolve.

pergent stays stateless — the PR/MR comment is the only persistent state. First runs (no existing comment) behave exactly as before. Requires platform mode (not available with `--local`).

```yaml
mr-review:
  stage: review
  image: ghcr.io/gopaytech/pergent:latest
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
  script:
    - pergent --skill code-review
  variables:
    GITLAB_TOKEN: $GITLAB_TOKEN
    PERGENT_PREVIOUS_REVIEW: "true"
    OPENCODE_PROVIDER: anthropic
    OPENCODE_API_KEY: $ANTHROPIC_API_KEY
    OPENCODE_MODEL: claude-sonnet-4
```
```

- [ ] **Step 4: Verify docs render sanely**

Run: `grep -n "previous-review\|PREVIOUS_REVIEW" README.md`
Expected: hits in the flag table, env table, and the new section

---

### Task 7: Final Verification

**Files:** none (verification only)

- [ ] **Step 1: Full suite**

Run: `make test && go vet ./... && make build`
Expected: all tests PASS, vet clean, `bin/pergent` builds

- [ ] **Step 2: Confirm zero behavior change when disabled**

Run: `./bin/pergent --local --skill code-review --base-branch master` (in a repo with commits ahead of `origin/master`, opencode configured)
Expected: works exactly as before this feature (no previous-review attachment, single `--file` in the opencode invocation)

This step needs a configured opencode; if unavailable, verify instead via: `go test ./...` plus reading the `cmd/main.go` diff to confirm the disabled path is unchanged.

---

## Spec Coverage Checklist

| Spec requirement | Task |
|---|---|
| `--previous-review` flag + `PERGENT_PREVIOUS_REVIEW` env, precedence table | 1, 5 |
| `flag.Visit` explicit-flag detection, resolver stays flag-package-free | 1, 5 |
| `--local` exclusion error | 1 |
| `FindComment` returns body, zero extra API calls | 2 |
| `ExtractSkillSection` next to the formatter | 3 |
| Runner second `--file`, main.go owns the message | 4, 5 |
| Early lookup, warn-and-continue on error, retry before posting | 5 |
| Temp file same lifecycle as diff | 5 |
| First run / missing section / edited comment degrade gracefully | 3, 5 |
| README updates | 6 |
