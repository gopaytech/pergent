# Configuration

## ⚙️ CLI flags

| Flag | Description | Default |
|---|---|---|
| `--skill` | Preset name or path to skill `.md` file (repeatable) | required |
| `--local` | Local mode: diff from git, output to stdout | `false` |
| `--base-branch` | Base branch for local git diff | `main` |
| `--test` | Test opencode connection (sends a hello prompt) | `false` |
| `--previous-review` | Attach the findings from pergent's previous review comment as context on re-runs (platform mode only) | `false` |
| `--platform` | `github` or `gitlab` | auto-detect |
| `--max-turns` | Max agentic turns per opencode run | `20` |
| `--context-limit` | Model context window in tokens. Only for models opencode has no limits for (e.g. behind a custom `OPENCODE_BASE_URL`); otherwise opencode's model registry is used | unset |
| `--max-tokens` | **Deprecated and ignored.** It used to set the context window, which made opencode compact far below the model's real limit. Prints a warning | — |
| `--timeout` | **Breaking:** max wall-clock time for the whole review, all skills, batches, retries and checks together. It used to apply to each skill run separately, so a job that relied on a per-skill limit now shares one budget | `10m` |
| `--concurrency` | Max opencode runs in flight at once, across all skills and batches. Must be at least 1 | `4` |
| `--check-deleted` | Check what still refers to the files the change deletes (see [Deleted files](../concepts/deleted-files.md)). Pass `--check-deleted=false` to turn it off | `true` |
| `--repo-path` | Path to repo root | `.` |

## 🌱 Environment variables

### LLM (passed through to opencode)

| Variable | Description |
|---|---|
| `OPENCODE_PROVIDER` | LLM provider (`anthropic`, `openai`, etc.). Required in CI unless `PERGENT_USE_OPENCODE_CONFIG=true` |
| `OPENCODE_API_KEY` | API key for the LLM provider |
| `OPENCODE_MODEL` | Model to use (e.g., `claude-sonnet-4`). Ignored, with a warning or an error, when `OPENCODE_PROVIDER` is empty |
| `OPENCODE_BASE_URL` | Custom API base URL (for LiteLLM, vLLM, Ollama, etc.) |
| `OPENCODE_NPM` | Override AI SDK package (default: `@ai-sdk/openai-compatible` when `OPENCODE_BASE_URL` is set) |

### Platform

| Variable | Description |
|---|---|
| `GITHUB_TOKEN` | GitHub API access token |
| `GITLAB_TOKEN` | GitLab API access token |

### Limits (alternative to flags)

| Variable | Description |
|---|---|
| `PERGENT_MAX_TURNS` | Max agentic turns per opencode run |
| `PERGENT_CONTEXT_LIMIT` | Model context window in tokens (see `--context-limit`) |
| `PERGENT_MAX_TOKENS` | **Deprecated and ignored** (see `--max-tokens`) |
| `PERGENT_TIMEOUT` | Max wall-clock time for the whole review (see `--timeout`; **breaking**: it used to be per skill run) |
| `PERGENT_CONCURRENCY` | Max opencode runs in flight at once (see `--concurrency`) |
| `PERGENT_PREVIOUS_REVIEW` | `true`/`false` — attach the previous review's findings as context (explicit `--previous-review` flag wins over this) |
| `PERGENT_CHECK_DELETED` | `true`/`false` — check what still refers to the files the change deletes (explicit `--check-deleted` flag wins over this; default `true`) |

### Own-config mode

| Variable | Description |
|---|---|
| `PERGENT_USE_OPENCODE_CONFIG` | `true`/`false` — in GitHub/GitLab CI, allow a review with an empty `OPENCODE_PROVIDER` on opencode's own config. **No sandbox**: do not use on untrusted changes (see [option B](#own-config-mode)). Not needed with `--local`; agent mode always needs a provider |

Config resolution priority: CLI flags > `PERGENT_*` env vars > auto-detect from CI > defaults.

An unset or empty variable means the default. A value that can't be used is a config error that names the variable and the value (`PERGENT_TIMEOUT="600" is not a duration such as 90s or 10m`), not a silent fall back to the default. Numbers must be whole (`PERGENT_MAX_TURNS` and `PERGENT_CONCURRENCY` at least 1, `PERGENT_CONTEXT_LIMIT` not negative), `PERGENT_TIMEOUT` needs a unit, and `PERGENT_PREVIOUS_REVIEW`, `PERGENT_CHECK_DELETED` and `PERGENT_USE_OPENCODE_CONFIG` are `true` or `false`. `--max-turns` below 1 is rejected as well. A flag that is passed wins, and its variable is not read.
