# pergent Stable Reviews Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn pergent's free-form, one-run-per-skill review into structured findings with a clear verdict, reviewing every in-scope file of big MRs in parallel batches within one 10-minute budget.

**Architecture:** Phase 1 (exact failure causes, no synthetic text, compaction off, retries, `--max-tokens` deprecated, `--context-limit`) is already on this branch, `claude/fix-review-failures-e9b265`, which is gopaytech/pergent#4. This plan adds commits to the same branch. New packages: `internal/finding` (finding types, parsing), `internal/diff` (splitting, file rules, batching) and `internal/review` (the orchestration now in `cmd/main.go`: batches, concurrency, budget, verdicts, verification). `internal/output` renders the new comment layout and stores findings for the next run.

**Tech Stack:** Go 1.24.8, stdlib + `gopkg.in/yaml.v3` only, opencode 1.19.4.

**Spec:** `docs/superpowers/specs/2026-10-23-pergent-agent-design.md`

## Global Constraints

- No new Go dependencies. Stdlib plus `gopkg.in/yaml.v3`.
- Severity values: `blocking`, `suggestion`. Verdict values: `no_blocking_issues`, `has_blocking_issues`, `failed`.
- Finding JSON field names: `file`, `line`, `severity`, `title`, `explanation`, `suggested_fix`, optional `fingerprint`.
- `failed` never counts as a pass. A skill is `failed` if any in-scope file was not reviewed.
- Skipped files (out of scope, or excluded by rule) never make a skill `failed`.
- Review failures keep the CI job green (exit 0). Config errors before any review starts still exit 1.
- Whole-review budget default `10m` via `--timeout` / `PERGENT_TIMEOUT`. Concurrency default `4` via `--concurrency` / `PERGENT_CONCURRENCY`.
- Never post synthetic or partial text as a review. Every failure states its exact cause.
- Redact secrets (`redact.String`) in everything that reaches the comment, including the hidden findings block.
- Privacy: no real MR, project, repo path, skill name, bot name or person from the user's environment in code, tests, fixtures or commits. Use generic examples.
- The read-only opencode sandbox from #3 and phase 1 stays unchanged.

## Review Focus

- **Pure renames or mode-only changes** (a file diff with no `@@` hunk) must be skipped as `no content changes`, not sent to the model or reported as not reviewed. Tested in Task 3.
- **Paths with spaces**, which git quotes as `diff --git "a/x y.yaml" "b/x y.yaml"`, must parse to `x y.yaml`. Tested in Task 3.
- **An answer with several ```json blocks**, for example an example block and then the real one, must use the last valid block. Tested in Task 1.
- **Finding text containing `-->`, or a secret**, must not break the hidden findings block and must not leak. Tested in Task 9.
- **A previous comment from an older pergent** with no hidden findings block must run as a first review, with no error. Tested in Task 9 and Task 10.

---

### Task 1: Finding types and answer parsing

**Files:**
- Create: `internal/finding/finding.go`
- Create: `internal/finding/parse.go`
- Test: `internal/finding/finding_test.go`, `internal/finding/parse_test.go`

**Interfaces:**
- Produces:
  - `type Severity string` with constants `Blocking Severity = "blocking"` and `Suggestion Severity = "suggestion"`.
  - `type Finding struct { File string \`json:"file"\`; Line int \`json:"line,omitempty"\`; Severity Severity \`json:"severity"\`; Title string \`json:"title"\`; Explanation string \`json:"explanation"\`; SuggestedFix string \`json:"suggested_fix,omitempty"\`; Fingerprint string \`json:"fingerprint,omitempty"\` }`
  - `func Fingerprint(skill, file, title string) string`: the first 12 hex chars of sha256(`skill + "\x00" + file + "\x00" + normalize(title)`). `normalize` lowercases the title, collapses runs of whitespace to one space, and trims.
  - `func Dedup(fs []Finding) []Finding`: keeps the first finding for each (file, line, normalized title) and preserves order.
  - `func ParseAnswer(answer string) ([]Finding, error)`

- [ ] **Step 1: Write the failing tests**
  - `TestFingerprint_StableAndNormalized`: `Fingerprint("s","a.go","Nil  Deref ")` equals `Fingerprint("s","a.go","nil deref")`, has length 12, and differs from `Fingerprint("s","b.go","nil deref")`.
  - `TestDedup_KeepsFirst`: two findings with the same file, line and title (differing only in case and spacing) plus one other give 2 results, the first one kept.
  - `TestParseAnswer`, table-driven:
    - prose then ```json `{"findings":[{"file":"a.go","line":3,"severity":"blocking","title":"t","explanation":"e"}]}` → one finding, `Severity == Blocking`.
    - `{"findings": []}` → empty slice, nil error.
    - two json blocks, where the first is an example with `"severity":"blocking"` and the last is `{"findings":[]}` → empty, nil error (the last valid block wins).
    - no json block → error containing `no json code block`.
    - severity `"critical"` → error containing `severity`.
    - missing `title` → error containing `title`.
    - `{"other":1}` → error containing `findings`.
    - a block with optional `fingerprint` → `Fingerprint` field kept.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/finding/ -v`
Expected: FAIL (package does not compile)

- [ ] **Step 3: Implement**
  - `ParseAnswer` collects every ```json … ``` fenced block, tries them from last to first, and returns the first that unmarshals into a struct with a non-nil `Findings *[]Finding` and passes validation:
    - `file` is non-empty;
    - `title` is non-empty;
    - `severity` is one of the two constants;
    - `line` is 0 or more.
  - If none pass, return the last block's validation error, or `no json code block found in the answer`.

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/finding/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/finding
git commit -m "feat: add finding types and structured answer parsing"
```

---

### Task 2: Skill scope fields, Blocking section, and owned output format

**Files:**
- Modify: `internal/skill/loader.go` (`Skill`, `frontmatter`, `parse`)
- Create: `internal/skill/prompt.go`
- Modify: `internal/skill/presets/code-review.md`
- Test: `internal/skill/loader_test.go`, `internal/skill/prompt_test.go`

**Interfaces:**
- Produces:
  - `Skill` gains `Paths []string`, `Exclude []string`, `CrossBatch bool` and `HasBlocking bool`.
  - The frontmatter keys are `paths`, `exclude` and `cross_batch`.
  - `HasBlocking` is true when the body has a markdown heading line matching `(?im)^#{1,6}\s*blocking\b`.
  - `func (s Skill) Prompt() string` returns `s.Body`, then the default Blocking section (only if `!s.HasBlocking`), then the output-format section.

- [ ] **Step 1: Write the failing tests**
  - `TestParse_ScopeFrontmatter`: frontmatter with `paths: ["**/manifests/**"]`, `exclude: ["**/*.md"]` and `cross_batch: true` populates those fields.
  - `TestParse_HasBlocking`: the body `"Intro\n\n## Blocking\n- x"` gives true; `"Intro only"` gives false.
  - `TestPrompt_AppendsDefaultBlockingOnlyWhenMissing`: with `HasBlocking == false`, the prompt contains `Treat these as blocking: bugs, security vulnerabilities, and broken configuration.`; with `HasBlocking == true` it doesn't.
  - `TestPrompt_AlwaysAppendsOutputFormat`: the prompt contains `## Output format` and `{"findings": []}`.
  - `TestResolve_PresetByName` (existing): extend it to assert the preset `HasBlocking == true` and that the body no longer contains `**[filename:line]**`.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/skill/ -v`
Expected: FAIL

- [ ] **Step 3: Implement.** The appended text is exact copy:

~~~markdown
## Blocking

Treat these as blocking: bugs, security vulnerabilities, and broken configuration. Everything else is a suggestion.
~~~

~~~markdown
## Output format

End your final answer with exactly one fenced code block tagged `json`, with nothing after it, in this shape:

```json
{"findings": [{"file": "path/from/repo/root", "line": 42, "severity": "blocking", "title": "One-line summary", "explanation": "Why it matters", "suggested_fix": "Optional fix"}]}
```

- `severity` is `blocking` or `suggestion`. Only problems described under Blocking are `blocking`; everything else is a `suggestion`.
- `line` is the line number in the new version of the file. Omit it when the finding is not about one line.
- `suggested_fix` is optional.
- If you find nothing, answer `{"findings": []}`.
- Text before the code block is not shown to anyone.
~~~

  Rewrite `presets/code-review.md`: keep its review focus, remove the `**[filename:line]**` format instruction, and add a `## Blocking` section listing logic errors that cause wrong behaviour, crashes or data loss, security vulnerabilities, and broken configuration.

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/skill/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/skill
git commit -m "feat: skill scope, blocking section, and pergent-owned output format"
```

---

### Task 3: Split diffs and classify files

**Files:**
- Create: `internal/diff/split.go`, `internal/diff/glob.go`, `internal/diff/classify.go`
- Test: `internal/diff/split_test.go`, `internal/diff/glob_test.go`, `internal/diff/classify_test.go`

**Interfaces:**
- Produces:
  - `type FileDiff struct { Path, OldPath, Patch string; Deleted, Binary bool }`. `Patch` is the full text from this file's `diff --git` line up to the next one.
  - `func Split(unified string) []FileDiff`
  - `func Match(pattern, path string) bool`. `**` matches zero or more path segments and `*` matches within one segment. A pattern containing no `/` matches the basename at any depth.
  - `type Skip struct { Path, Rule string }`
  - `type Classifier struct` (unexported fields)
  - `func NewClassifier(repoPath string) Classifier`: reads `<repoPath>/.gitattributes` if present and keeps the patterns marked `linguist-generated` or `linguist-generated=true`.
  - `func (c Classifier) Exclusion(f FileDiff) (rule string, optIn bool)`. `optIn` reports whether a skill's `paths` may override the rule. The rules, checked in this order:
    - `deleted` (`optIn` false), from `deleted file mode`;
    - `binary` (`optIn` false), from `Binary files … differ` or `GIT binary patch`;
    - `no content changes` (`optIn` false), when the patch has no `@@` hunk;
    - `lockfile` (`optIn` true), basename in `package-lock.json`, `go.sum`, `yarn.lock`, `pnpm-lock.yaml`, `Cargo.lock`, `composer.lock`, `Gemfile.lock`, `poetry.lock`, `Pipfile.lock`;
    - `vendored` (`optIn` true), a path segment of `vendor`, `node_modules` or `third_party`;
    - `generated` (`optIn` true), a `.gitattributes` match or an added line matching `(?i)^\+.*code generated .*do not edit`;
    - `""` when no rule applies.
  - `func EstimateTokens(s string) int` returns `(len(s)+3)/4`.

- [ ] **Step 1: Write the failing tests**
  - `TestSplit_ThreeFiles`: a modified, an added and a deleted file give 3 `FileDiff` values with the right `Path`, and `Deleted` true only on the third.
  - `TestSplit_Rename`: `rename from old.yaml` / `rename to new.yaml` with no hunks gives `Path "new.yaml"` and `OldPath "old.yaml"`.
  - `TestSplit_QuotedPathWithSpaces`: `diff --git "a/dir/x y.yaml" "b/dir/x y.yaml"` gives `Path "dir/x y.yaml"`.
  - `TestMatch`, table-driven:
    - `**/manifests/**` matches `a/manifests/b/c.yaml`;
    - `*.md` matches `docs/x.md`;
    - `src/*.go` matches `src/a.go` but not `src/sub/a.go`;
    - `**` matches everything.
  - `TestExclusion`: one case per rule above, plus a plain `.go` change giving `""`. For the `generated` rule, include a `.gitattributes` line `gen/** linguist-generated` written to a `t.TempDir()`.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/diff/ -v`
Expected: FAIL

- [ ] **Step 3: Implement** with the signatures above. To unquote git's quoted paths, use `strconv.Unquote`.

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/diff/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/diff
git commit -m "feat: split diffs per file and classify default exclusions"
```

---

### Task 4: Batch files by directory within a token budget

**Files:**
- Create: `internal/diff/batch.go`
- Test: `internal/diff/batch_test.go`

**Interfaces:**
- Consumes: `FileDiff` and `EstimateTokens` (Task 3).
- Produces:
  - `func Batch(files []FileDiff, budget int) (batches [][]FileDiff, oversize []FileDiff)`
  - `func BatchBudget(contextLimit int) int` returns `contextLimit/4` when `contextLimit > 0`, otherwise `40000`.

- [ ] **Step 1: Write the failing tests**
  - `TestBatch_AllFitInOne`: 3 small files give 1 batch and no oversize.
  - `TestBatch_KeepsDirectoryTogether`: two directories of 2 files each, where each directory fits the budget but both together don't, give 2 batches, each holding exactly one directory's files.
  - `TestBatch_SplitsLargeDirectory`: one directory over budget is split file by file, and every file appears in exactly one batch.
  - `TestBatch_Oversize`: a single file over budget goes to `oversize`, and the other files are still batched.
  - `TestBatchBudget`: `BatchBudget(0) == 40000` and `BatchBudget(100000) == 25000`.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/diff/ -run 'TestBatch' -v`
Expected: FAIL

- [ ] **Step 3: Implement.** The algorithm:

```text
group files by path.Dir, in sorted directory order (files sorted within a group)
for each group:
  if group tokens <= budget: place the whole group (start a new batch if it doesn't fit the current one)
  else: place its files one at a time the same way
a single file with tokens > budget goes to oversize
```

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/diff/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/diff
git commit -m "feat: batch file diffs by directory within a token budget"
```

---

### Task 5: Review core: scope, batches, verdicts, answer repair

**Files:**
- Create: `internal/review/review.go`, `internal/review/select.go`
- Modify: `internal/runner/runner.go`: replace `Options.DiffFile` and `Options.PrevReviewFile` with `Files []string`, and change `BuildCommand` to `BuildCommand(ctx context.Context, configPath string, files []string, message, repoPath string) *exec.Cmd`, which passes each file with `--file`.
- Modify: `cmd/test.go` (the new `BuildCommand` call) and `internal/runner/runner_test.go`
- Test: `internal/review/review_test.go`, `internal/review/select_test.go`

**Interfaces:**
- Consumes:
  - `runner.Run(ctx, runner.Options) runner.RunResult`, `runner.Cause` and its `Message()` (phase 1)
  - `config.GenerateOpenCodeConfig(maxTurns, contextLimit int, skillPrompt, repoPath string) (string, func(), error)`
  - from Tasks 1–4: `skill.Skill.Prompt`, `finding.ParseAnswer`/`Dedup`/`Fingerprint`, `diff.Split`/`NewClassifier`/`Batch`/`BatchBudget`
- Produces (package `review`):
  - `type Verdict string`, with constants `NoBlockingIssues = "no_blocking_issues"`, `HasBlockingIssues = "has_blocking_issues"` and `Failed = "failed"`.
  - `type RunFunc func(ctx context.Context, opts runner.Options) runner.RunResult`
  - `type Request struct { Skills []skill.Skill; Diff, RepoPath string; Budget time.Duration; Concurrency, MaxTurns, ContextLimit int; Previous map[string][]finding.Finding; Run RunFunc }`. A nil `Run` means `runner.Run`.
  - `type BatchFailure struct { Files []string; Cause *runner.Cause; Message string }`. `Message` is used only when `Cause` is nil.
  - `type SkillResult struct { Skill string; Verdict Verdict; Findings []finding.Finding; Skipped []diff.Skip; Failures []BatchFailure; Notes []string; Batches, BatchesFinished int }`
  - `func Run(ctx context.Context, req Request) []SkillResult`, which returns results in `req.Skills` order.
  - `func Select(s skill.Skill, files []diff.FileDiff, c diff.Classifier) (in []diff.FileDiff, skipped []diff.Skip)`. The rules, in order:
    - `Exclusion` rules with `optIn == false`;
    - `out of scope` when `Paths` is non-empty and none match;
    - `excluded by skill` when an `Exclude` pattern matches;
    - `Exclusion` rules with `optIn == true`, unless a `Paths` pattern matches the file.

This task runs batches one after another. Task 6 adds concurrency and the shared budget. For now, each run's `Options.Timeout` is `req.Budget`.

- [ ] **Step 1: Write the failing tests.** Use a fake `RunFunc` that records `opts.Message` and `opts.Files` and returns canned answers.
  - `TestSelect`:
    - a skill with `Paths ["**/manifests/**"]` skips `cmd/a.go` as `out of scope`;
    - `Exclude ["**/*.md"]` skips `manifests/README.md` as `excluded by skill`;
    - `go.sum` is skipped as `lockfile` for a skill without `Paths`, but kept for a skill with `Paths ["go.sum"]`;
    - a deleted file is skipped as `deleted` even with `Paths ["**"]`.
  - `TestRun_NoFilesInScope`: a skill with zero in-scope files gives `NoBlockingIssues`, `Notes` containing `no files in scope`, and the fake is never called.
  - `TestRun_Verdicts`:
    - an answer with one blocking finding gives `HasBlockingIssues`;
    - only suggestions give `NoBlockingIssues`;
    - every finding has a 12-char `Fingerprint`.
  - `TestRun_BatchFailureMakesSkillFailed`: 2 batches (force them with a tiny `ContextLimit` and two directories), one returning `runner.StatusFailed` with a `Cause`. The result is `Failed`, keeps the other batch's findings, and the failure's `Files` lists that batch's files.
  - `TestRun_OversizeFileMakesSkillFailed`: the result is `Failed`, with a failure `Message` containing `larger than the per-batch limit`.
  - `TestRun_RepairsBadFormatOnce`:
    - when the first answer has no json block and the repair answer is valid, the result has its findings and the fake was called twice, the second time with the bad answer attached as a file;
    - when both answers are invalid, the result is `Failed` with a `Message` containing `not in the required format` and the first 200 chars of the answer.
  - `TestRun_PreviousFindingsAttached`:
    - `req.Previous["s"]` set means every batch run gets one extra attached JSON file and a message mentioning `previous review`;
    - an answer finding whose `fingerprint` matches a previous one keeps that fingerprint, and one with an unknown fingerprint gets a computed one.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/review/ ./internal/runner/ -v`
Expected: FAIL

- [ ] **Step 3: Implement**
  - **Each batch:** write the batch's patches, concatenated, to a temp `.patch` file. Write a temp `.md` file listing every changed file in the MR as `- path` lines. Attach the previous findings JSON for the skill, if any.
  - **Prompt and config:** the agent prompt is `s.Prompt()`. Generate the config per run and call cleanup after it.
  - **Messages:**
    - single batch: `Review the attached diff.`
    - more than one batch: `Review the attached diff. It is batch N of M of this change; the attached list shows every changed file. Review only the files in this batch's diff, and read any file in the repository you need.`
    - with previous findings, append: `Also attached are the findings from your previous review of an earlier revision, as JSON. Stay consistent with them: focus on what changed since, don't re-litigate unchanged code, drop findings the new changes resolve, and reuse a finding's fingerprint when it still applies.`
  - **Repair:** a second run with the same prompt, the bad answer attached as a `.md` file, and the message `The attached review is not in the required output format (<parse error>). Rewrite it in the required format without changing its findings.`
  - **Verdict:**
    - `Failed` if `Failures` is non-empty;
    - else `HasBlockingIssues` if any finding is blocking;
    - else `NoBlockingIssues`.
  - Run `finding.Dedup` over the merged findings.

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/review/ ./internal/runner/ ./cmd/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/review internal/runner cmd/test.go
git commit -m "feat: review skills per batch with structured findings and verdicts"
```

---

### Task 6: Parallel runs and the whole-review budget

**Files:**
- Modify: `internal/review/review.go`
- Test: `internal/review/budget_test.go`

**Interfaces:**
- Consumes: `Request.Concurrency` and `Request.Budget` (Task 5).
- Produces: no new names. `Run` now runs all skills and batches concurrently, with at most `Concurrency` opencode runs (batch, repair, verify, cross-batch) at once, all within `Budget`.

- [ ] **Step 1: Write the failing tests**
  - `TestRun_RespectsConcurrency`: 3 skills × 3 batches with `Concurrency: 2` and a fake that sleeps 20ms and tracks in-flight calls with an atomic counter. The observed maximum is ≤ 2 and all 9 batches run.
  - `TestRun_BudgetExhausted`: `Budget: 100ms`, `Concurrency: 1`, 4 batches, and a fake that blocks until `ctx.Done()`.
    - The result is `Failed`.
    - Unstarted batches appear as one failure whose `Message` is `Not reviewed: the 100ms review budget ran out; 0 of 4 batches finished.`, with the counts coming from `Batches`/`BatchesFinished`.
    - Every batch's files appear in some failure.
  - `TestRun_TimeoutIsRemainingBudget`: the `opts.Timeout` passed to the fake is > 0 and ≤ `Budget`.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/review/ -run 'TestRun_(RespectsConcurrency|BudgetExhausted|TimeoutIsRemainingBudget)' -v`
Expected: FAIL

- [ ] **Step 3: Implement**
  - Use a single `chan struct{}` semaphore of size `Concurrency` shared across all skills.
  - Derive `ctx` with a deadline of `now + Budget`.
  - Each run's `Options.Timeout` is `time.Until(deadline)`. Don't start a run when that is ≤ 0; record its batch as not reviewed instead.
  - A batch that `runner.Run` reports as timed out keeps its own runner `Cause`.

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/review/ -race -v`
Expected: PASS, no race reports

- [ ] **Step 5: Commit**

```bash
git add internal/review
git commit -m "feat: run skills and batches in parallel within one review budget"
```

---

### Task 7: Verification pass on blocking findings

**Files:**
- Create: `internal/review/verify.go`
- Test: `internal/review/verify_test.go`

**Interfaces:**
- Consumes: `SkillResult`, `RunFunc`, the semaphore and deadline from Tasks 5–6.
- Produces: `const verifyMinRemaining = 90 * time.Second`. Verification runs per skill after its batches, only when the skill has at least one blocking finding.

- [ ] **Step 1: Write the failing tests**
  - `TestVerify_DropsFalseFindings`: two blocking findings and a verify answer of ```json `{"results":[{"fingerprint":"<fp1>","real":false,"reason":"r"}]}`. The finding with `fp1` is dropped, the other is kept, and suggestions are untouched and never sent.
  - `TestVerify_SkippedWhenLowOnTime`: with less than `verifyMinRemaining` left, the fake isn't called for verification, `Notes` contains `Verification skipped: less than 1m30s of the review budget was left; blocking findings are unverified.`, and all findings are kept.
  - `TestVerify_FailureKeepsFindings`: when the verify run fails with a cause, all findings are kept and `Notes` contains `Verification failed:` plus the cause's `Message()`.
  - `TestVerify_NotRunWithoutBlocking`: with only suggestions, no verify call is made.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/review/ -run TestVerify -v`
Expected: FAIL

- [ ] **Step 3: Implement**
  - Attach the blocking findings as a JSON file and use this exact agent prompt:

~~~markdown
You are checking another reviewer's blocking findings on a code change. For each finding in the attached JSON, open the file it refers to and decide whether the problem is real in the current code. Do not add new findings.

End your final answer with exactly one fenced code block tagged `json`, with nothing after it:

```json
{"results": [{"fingerprint": "abc123def456", "real": true, "reason": "One sentence"}]}
```
~~~

  - Message: `Check the attached blocking findings.`
  - Findings missing from `results` are kept. An answer that can't be parsed counts as `Verification failed: the answer was not in the required format.`

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/review/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/review
git commit -m "feat: re-check blocking findings before reporting them"
```

---

### Task 8: Optional cross-batch pass

**Files:**
- Create: `internal/review/crossbatch.go`
- Test: `internal/review/crossbatch_test.go`

**Interfaces:**
- Consumes: `Skill.CrossBatch` (Task 2) and `verifyMinRemaining` (Task 7, which is also the time threshold here).
- Produces: a cross-batch run per skill, only when `s.CrossBatch && Batches > 1`. It runs after the batches and before verification.

- [ ] **Step 1: Write the failing tests**
  - `TestCrossBatch_OffByDefault`: a skill with 3 batches and `CrossBatch` false makes no cross-batch call.
  - `TestCrossBatch_AddsFindings`: with `CrossBatch` true, one extra call gets the changed-file list and all batch findings attached, and its new findings are merged and deduped.
  - `TestCrossBatch_SkippedWhenLowOnTime`: `Notes` contains `Cross-batch check skipped: less than 1m30s of the review budget was left.`
  - `TestCrossBatch_FailureIsANote`: a failed cross-batch run adds the note `Cross-batch check failed:` plus the cause message. It doesn't make the skill `Failed`, because every file was already reviewed.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/review/ -run TestCrossBatch -v`
Expected: FAIL

- [ ] **Step 3: Implement**
  - Agent prompt: `s.Prompt()`.
  - Message: `This change was reviewed in N batches. Attached are the list of every changed file and the findings from all batches. Look only for problems that span batches, such as conflicting changes between files in different batches. Report only new findings.`

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/review/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/review
git commit -m "feat: optional cross-batch check per skill"
```

---

### Task 9: Comment layout and hidden findings block

**Files:**
- Modify: `internal/output/formatter.go`
- Test: `internal/output/formatter_test.go`

**Interfaces:**
- Consumes: `review.SkillResult`, `review.BatchFailure`, and phase 1's `writeFailure` / `failedPrefix`.
- Produces:
  - `func FormatReview(results []review.SkillResult, changedFiles []string) string`
  - `func ParsePreviousFindings(body string) (map[string][]finding.Finding, error)`, which returns `nil, nil` when there is no block.
  - `func BatchFailureText(f review.BatchFailure) string`, which is redacted.
  - `FormatComment`, `ExtractSkillSection` and `IsFailedSection` are deleted in Task 10, once `main.go` stops using them.

- [ ] **Step 1: Write the failing tests**
  - `TestFormatReview_Layout`: one result per verdict.
    - The output starts with `<!-- pergent -->\n## pergent review`.
    - It has a `<details><summary>Files changed (2)</summary>` block.
    - Verdict lines: `✅ **No blocking issues**`, `❌ **2 blocking issues**` (and `❌ **1 blocking issue**` for one), and `⚠️ **Review failed:**` followed by each failure's text.
    - Blocking findings appear in full as `` **`file:line`** — **title** `` followed by the explanation and `_Suggested fix:_` when set.
    - Suggestions appear inside `<details><summary>Suggestions (N)</summary>`.
    - Skipped files appear inside `<details><summary>Skipped files (N)</summary>` as `` - `path` — rule ``.
    - Notes are rendered as `> note` lines.
  - `TestFormatReview_FailedStillShowsFindings`: a `Failed` result with findings shows both the failure and the findings.
  - `TestFindingsBlock_RoundTrip`: `ParsePreviousFindings(FormatReview(results, nil))` returns the same findings per skill, including one whose `Explanation` contains `-->` and a backtick fence.
  - `TestFindingsBlock_Redacted`: with `GITLAB_TOKEN=glpat-secretvalue` set via `t.Setenv` and a finding whose explanation contains `glpat-secretvalue`, neither the comment text nor the decoded block contains it.
  - `TestParsePreviousFindings_OldComment`: a comment without the block returns `nil, nil`.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/output/ -v`
Expected: FAIL

- [ ] **Step 3: Implement**
  - The hidden block is one line, `<!-- pergent:findings <base64> -->`.
  - `<base64>` is `base64.StdEncoding` of `{"version":1,"skills":{"<skill>":[findings…]}}`.
  - Apply `redact.String` to each finding's text fields before encoding, then to the whole comment as phase 1 does.
  - Render a `BatchFailure` with `writeFailure` when `Cause` is set, otherwise as `failedPrefix + " " + Message`, prefixed with the batch's files when there was more than one batch.

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/output/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/output
git commit -m "feat: verdict-first comment layout and hidden findings for the next run"
```

---

### Task 10: Wire it up: flags, main, docs

**Files:**
- Modify: `internal/config/resolver.go` (`Options.Concurrency`, `Config.Concurrency`) and `internal/config/resolver_test.go`
- Modify: `cmd/main.go`
- Modify: `internal/output/formatter.go`: delete `FormatComment`, `ExtractSkillSection` and `IsFailedSection`, plus their tests.
- Modify: `README.md`, `CLAUDE.md`

**Interfaces:**
- Consumes: `review.Run`, `output.FormatReview`, `output.ParsePreviousFindings`, `output.BatchFailureText`.
- Produces:
  - `--concurrency` / `PERGENT_CONCURRENCY`, default 4. A value below 1 is a config error: `--concurrency must be at least 1, got N`.
  - `--timeout` help text: `Max wall-clock time for the whole review (default 10m)`.

- [ ] **Step 1: Write the failing tests**
  - `TestResolve_ConcurrencyDefault`: the default is 4.
  - `TestResolve_ConcurrencyEnv`: `PERGENT_CONCURRENCY=8` gives 8.
  - `TestResolve_ConcurrencyInvalid`: `0` gives the error above.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/config/ -run Concurrency -v`
Expected: FAIL

- [ ] **Step 3: Implement**
  - Resolve `Concurrency` with `resolveInt(opts.Concurrency, "PERGENT_CONCURRENCY", 4)`.
  - In `main.go`:
    - replace the per-skill loop with one `review.Run` call, passing `Budget: cfg.Timeout`;
    - with `--previous-review`, fill `Previous` from `output.ParsePreviousFindings(prevBody)`, logging a warning on error and continuing with no previous findings;
    - log each failure with `output.BatchFailureText`;
    - post `output.FormatReview(results, changedFiles)`;
    - keep the exit codes from phase 1.
  - **README:**
    - skill frontmatter `paths` / `exclude` / `cross_batch`;
    - the `## Blocking` section, and the default when it's missing;
    - "don't put output-format instructions in skills";
    - the default exclusions list;
    - the comment layout;
    - `--concurrency`;
    - `--timeout` is now whole-review, marked **Breaking**;
    - `--previous-review` now uses the hidden findings block, and comments from older versions are ignored.
  - **CLAUDE.md:** update "One opencode run per skill" to describe batches, `internal/review`, `internal/diff` and `internal/finding`.

- [ ] **Step 4: Verify everything**

Run: `go vet ./... && make test && make build`
Expected: no vet output, all packages `ok`, `bin/pergent` built

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: wire structured, batched reviews into the CLI"
```

---

### Task 11: Update PR #4 (ask the user first)

- [ ] **Step 1:** Ask the user whether to push `claude/fix-review-failures-e9b265`. Pushing publishes the design doc, the research note and this plan in gopaytech/pergent#4. Do not push without a yes.
- [ ] **Step 2:** After a yes, run `git push`. Then update the PR title to `Stable reviews: exact failures, structured findings, batched review` and the body to summarize phase 1 plus Tasks 1–10. Include the **Breaking** `--timeout` note and the manual check below.
- [ ] **Step 3 (manual check, by the user):** on a real MR, run with `--previous-review` and `PERGENT_DEBUG_OUTPUT` set. Confirm:
  - the comment shows a verdict line per skill;
  - a large MR shows several batches with every in-scope file covered or named as not reviewed;
  - a second push reuses the previous findings.
