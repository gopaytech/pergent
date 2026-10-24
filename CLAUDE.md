# pergent

CLI tool that runs agentic PR/MR reviews in CI/CD pipelines by spawning `opencode` as a subprocess.

## Architecture

- `cmd/main.go` — CLI entrypoint, flag parsing, orchestration
- `internal/skill/` — Parses `.md` skill files (YAML frontmatter `name`/`paths`/`exclude`/`cross_batch` + body); `Prompt()` adds the default `## Blocking` section and pergent's fixed output-format instructions
- `internal/diff/` — Splits a unified diff into per-file patches, classifies default exclusions (deleted, binary, lockfile, vendored, generated), matches globs, and groups files into token-budgeted batches
- `internal/finding/` — The `Finding` type (file, line, severity, title, explanation, fix), stable fingerprints, dedup
- `internal/review/` — Runs a whole review: per skill selects files, batches them, runs one opencode run per batch (plus a repair run, an optional cross-batch run and a verification run), and decides each skill's verdict; one shared concurrency limit and time budget
- `internal/config/` — Resolves config (flags > env > auto-detect), generates temporary `opencode.json`
- `internal/runner/` — Spawns `opencode run` subprocess, parses NDJSON output, classifies the run (completed or failed with a cause), retries transient provider errors, enforces timeout and step limit
- `internal/output/` — Formats per-skill review results (verdict, findings, failures) into a single markdown comment with HTML dedup markers and a hidden findings block; parses that block back for `--previous-review`
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
