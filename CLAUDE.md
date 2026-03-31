# pergent

CLI tool that runs agentic PR/MR reviews in CI/CD pipelines by spawning `opencode` as a subprocess.

## Architecture

- `cmd/main.go` — CLI entrypoint, flag parsing, orchestration
- `internal/skill/` — Parses `.md` skill files (YAML frontmatter + body)
- `internal/config/` — Resolves config (flags > env > auto-detect), generates temporary `opencode.json`
- `internal/runner/` — Spawns `opencode run` subprocess, parses NDJSON output, enforces timeout
- `internal/output/` — Formats per-skill results into a single markdown comment with HTML dedup markers
- `internal/platform/` — GitHub/GitLab API clients for fetching diffs and posting/updating comments

## Key Concepts

- **One opencode run per skill.** Each `--skill` flag triggers a separate subprocess. Results are combined into one comment.
- **Read-only agent.** The generated `opencode.json` denies write/edit/bash permissions — the agent can only read files, grep, and glob.
- **Config isolation.** `opencode.json` is written to a temp directory and passed via `OPENCODE_CONFIG` env var.
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
