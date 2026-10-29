# pergent

CLI tool that runs agentic PR/MR reviews in CI/CD pipelines by spawning `opencode` as a subprocess.

## Architecture

- `cmd/main.go` — CLI entrypoint, flag parsing, orchestration
- `cmd/agent.go` — `pergent agent`: flags, checkout checks, scrub, run, delivery, exit codes
- `internal/skill/` — Parses `.md` skill files (YAML frontmatter `name`/`paths`/`exclude`/`cross_batch` + body); `Prompt()` adds the default `## Blocking` section and pergent's fixed output-format instructions. `LoadDir` takes `*.md` files and `<name>/SKILL.md` folders, symlinks followed (ConfigMap volumes); a folder without `SKILL.md` is an error
- `internal/diff/` — Splits a unified diff into per-file patches, classifies default exclusions (deleted, binary, lockfile, vendored, generated), matches globs, and groups files into token-budgeted batches
- `internal/finding/` — The `Finding` type (file, line, severity, title, explanation, fix), stable fingerprints, dedup
- `internal/review/` — Runs a whole review: per skill selects files, batches them, runs one opencode run per batch (plus a repair run, an optional cross-batch run and a verification run), and decides each skill's verdict; one shared concurrency limit and time budget
- `internal/config/` — Resolves config (flags > env > auto-detect), generates temporary `opencode.json`
- `internal/runner/` — Spawns `opencode run` subprocess, parses NDJSON output, classifies the run (completed or failed with a cause), retries transient provider errors, enforces timeout and step limit; passes opencode's stderr through redacted line by line
- `internal/output/` — Formats per-skill review results (verdict, findings, failures) into a single markdown comment with HTML dedup markers and a hidden findings block; parses that block back for `--previous-review`
- `internal/result/` — Agent-mode result: schema v1 types, error codes, finding IDs, 10 MB size cap (`Encode`), delivery to file/stdout/HTTP (`Deliver`)
- `internal/redact/` — Removes credentials (token env values, bearer tokens, URL keys) from the comment and logs
- `internal/platform/` — GitHub/GitLab API clients for fetching diffs and posting/updating comments

## Key Concepts

- **One opencode run per batch.** Each `--skill` is reviewed over the changed files in its scope (`paths`/`exclude` plus default exclusions). `internal/diff` groups those files into batches that fit a quarter of the model's context, and `internal/review` runs one `opencode run` per batch, all skills and batches in parallel. Results are combined into one comment.
- **Structured findings.** The prompt (`skill.Prompt()`) tells the model to end with one `json` block of findings; severity is `blocking` or `suggestion`. A malformed answer gets one repair run, then the batch is a failure. Skills must not carry their own output-format instructions.
- **Verdicts.** Each skill is `no_blocking_issues`, `has_blocking_issues` or `failed` (any batch failure). Blocking findings are re-checked by a separate verification run before they count; `cross_batch: true` adds one run for problems spanning batches.
- **One budget, one concurrency limit.** `--timeout` bounds the whole review (not each run) and `--concurrency` caps opencode runs in flight across all skills and batches.
- **Previous review.** `--previous-review` reads the hidden `<!-- pergent:findings ... -->` block (last line of the comment) and attaches each skill's findings, only when non-empty. Comments from older versions have no block and count as a first review; a corrupt block logs a warning and is ignored.
- **Read-only agent.** The generated `opencode.json` defines a `pergent` agent (set as `default_agent` and passed via `--agent`) and denies edit/bash/task/skill/webfetch/websearch and `*.env` reads — the agent can only read files, grep, glob, and list. `OPENCODE_DISABLE_PROJECT_CONFIG=true` stops the reviewed repo's own opencode config from overriding this.
- **Config isolation.** `opencode.json` is written to a temp directory and passed via `OPENCODE_CONFIG` env var.
- **Run outcome.** Every opencode run is `StatusCompleted` (the model's own final answer) or `StatusFailed` with a `Cause` (context overflow, timeout, opencode error event, step limit, non-zero exit, no answer). Only assistant text from a step that ended on its own counts as the answer; `synthetic` text and text outside `step_start`/`step_finish` never do. A failed batch makes its skill's verdict `failed` and is reported in the comment and log, but keeps the CI job green; only config errors before any review exit non-zero.
- **No auto-compaction.** The generated config sets `compaction.auto: false` and leaves model limits to opencode's registry (`--context-limit` overrides; `--max-tokens` is deprecated and ignored). The model is always declared under `provider.<id>.models`, or opencode can't find models behind a custom base URL.
- **Retries.** A run that fails on a transient provider error (429, 5xx, connection) is started again up to 3 times with backoff, within the review budget. opencode retries 5 times internally first.
- **Agent mode.** `pergent agent` reviews `--base..--head` in a checkout the caller prepared and delivers a versioned JSON result (`docs/agent-result-schema.md`). It holds no platform token and never calls GitHub/GitLab; CI mode is untouched. Details:
  - Trusted inputs only: instructions, skills and `--previous` come from paths passed in; one that resolves inside `--repo-path` is a usage error, as are a `--result` file, `$HOME` or `$XDG_CONFIG_HOME` there, and duplicate skill names. The checkout's `AGENTS.md`/`CLAUDE.md`/`CONTEXT.md` are never loaded.
  - `--scrub-instructions` deletes those files (root included) and symlinks resolving outside the checkout; without it, nested files or such symlinks give `untrusted_instructions`. opencode loads nested instruction files on every read whatever the project-config switch says.
  - Base-commit skip rules: `.gitattributes` and the "generated" header are read from `--base`, never the checkout (`GIT_ATTR_SOURCE`, so git >= 2.40 or error `internal`). A new file is reviewed.
  - `--base`/`--head` are full lowercase SHAs, `--repo-path` is the work tree top, `HEAD` must equal `--head`. `OPENCODE_PROVIDER` and `OPENCODE_MODEL` are required; extra denied reads are `*.pem`, `*.key`, `*.tfvars`.
  - `PERGENT_RESULT_TOKEN`, `GITHUB_TOKEN` and `GITLAB_TOKEN` are removed from opencode's env; secrets are redacted. A verdict covers only `files.reviewed`; `files.skipped[].rule` records the rest. stdout carries only the result; logs go to stderr.
  - Exit codes: 0 delivered (even if every skill failed), 2 usage, 3 no review possible (error result delivered), 4 delivery failed. Any failure to review is an error result, never a false pass.
- **Comment deduplication.** HTML markers (`<!-- pergent -->`) identify existing comments for update (PATCH/PUT) instead of creating duplicates.
- **Diff fallback.** Tries local `git diff` first, falls back to platform API (for shallow clones).

## Build & Test

```bash
make build    # builds bin/pergent
make test     # runs all tests
make clean    # removes bin/
```

## Config Resolution Priority

CLI flags > `PERGENT_*` env vars > auto-detect from CI environment > defaults

## Required Environment Variables

- `GITHUB_TOKEN` or `GITLAB_TOKEN` — platform API access
- `OPENCODE_PROVIDER` — LLM provider (e.g., `anthropic`, `openai`)
- `OPENCODE_API_KEY` — API key for the LLM provider
- `OPENCODE_MODEL` — Model to use (e.g., `claude-sonnet-4`)
