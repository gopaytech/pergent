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
| `--context-limit` | Model context window in tokens. Only for models opencode has no limits for (e.g. behind a custom `OPENCODE_BASE_URL`); otherwise opencode's model registry is used. It also sets the batch size: a quarter of the window, at most 40,000 tokens. **Behaviour change:** a `--context-limit` above 160,000 used to give bigger batches and now gives 40,000-token batches. A smaller one still gives smaller batches. Set both limits, or neither. When only one is set, pergent writes the other as 0, and opencode then ignores its own value for it. 0 or unset means not set. With neither set, the config is as before. A flag that is not 0 wins over `PERGENT_CONTEXT_LIMIT` | unset |
| `--output-limit` | The most tokens one reply may use, thinking included. Only for models opencode has no limits for. opencode asks for at most 256,000 itself. 64,000 is enough for reviews and scans. A provider that counts the asked amount against a rate limit may slow a higher value. Set both limits, or neither: see `--context-limit`. 0 or unset means not set. A flag that is not 0 wins over `PERGENT_OUTPUT_LIMIT` | unset |
| `--max-tokens` | **Deprecated and ignored.** It used to set the context window, which made opencode compact far below the model's real limit. Prints a warning | — |
| `--timeout` | **Breaking:** max wall-clock time for the whole review, all skills, batches, retries and checks together. It used to apply to each skill run separately, so a job that relied on a per-skill limit now shares one budget | `10m` |
| `--concurrency` | Max opencode runs in flight at once, across all skills and batches. Must be at least 1 | `4` |
| `--check-deleted` | Review the lines a change deletes, and check what still uses deleted files and the old paths of renamed files. Costs more tokens (see [Deleted files](../concepts/deleted-files.md)) | `false` |
| `--usage-file` | Write each opencode run's tokens, steps and tool use as JSON to this file. Each skill has `name`, `tokens` and `runs`, with `runs` as in the [agent result](../agent-result-schema.md). Raw counts only, no prices. A file that cannot be written is a warning | unset |
| `--graph` | Experimental. A [code graph](../concepts/code-graph.md) of the default branch (`pergent-graph/v1`). Each batch run gets the callers and callees of the code it changes. A missing or bad file is skipped with a log line | `$PERGENT_GRAPH`, else unset |
| `--rules` | A JSON list of [path rules](../concepts/rules.md), used in place of `.pergent/rules.yaml` from the base branch. A list that can't be read or used stops pergent | `$PERGENT_RULES`, else unset |
| `--no-built-in-rules` | Leave the built-in path rules out (`rule/builtin/`) | `false` |
| `--built-in-rules` | Comma-separated names of the only built-in path rules to load, such as `go,yaml,ts_js` (the `.md` file names in `rule/builtin/`). Order does not matter: `builtin.yaml` order is kept. An unknown name is skipped with a warning. A value with no name loads them all. A config error with `--no-built-in-rules` on. See [Path rules](../concepts/rules.md) | `$PERGENT_BUILT_IN_RULES`, else all |
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
| `PERGENT_CONTEXT_LIMIT` | Model context window in tokens, also the batch size (see `--context-limit`, including its behaviour change) |
| `PERGENT_OUTPUT_LIMIT` | The most tokens one reply may use (see `--output-limit`) |
| `PERGENT_MAX_TOKENS` | **Deprecated and ignored** (see `--max-tokens`) |
| `PERGENT_TIMEOUT` | Max wall-clock time for the whole review (see `--timeout`; **breaking**: it used to be per skill run) |
| `PERGENT_CONCURRENCY` | Max opencode runs in flight at once (see `--concurrency`) |
| `PERGENT_PREVIOUS_REVIEW` | `true`/`false` — attach the previous review's findings as context (explicit `--previous-review` flag wins over this) |
| `PERGENT_GRAPH` | Experimental. The code graph file, as `--graph` (an explicit `--graph` flag wins over this). A missing or bad file is skipped with a log line, as with the flag |
| `PERGENT_CHECK_DELETED` | `true`/`false`: review deleted files and check what still uses deleted and renamed files (explicit `--check-deleted` flag wins over this; default `false`; any other value is a config error) |
| `PERGENT_RULES` | The path rule list, as `--rules` (an explicit `--rules` flag wins over this) |
| `PERGENT_NO_BUILT_IN_RULES` | `true`/`false`: leave the built-in path rules out (explicit `--no-built-in-rules` flag wins over this; default `false`; any other value is a config error) |
| `PERGENT_BUILT_IN_RULES` | The built-in rule names to load, as `--built-in-rules` (an explicit `--built-in-rules` flag wins over this, even an empty one). Empty means all. Set together with `PERGENT_NO_BUILT_IN_RULES=true` it is a config error |
| `PERGENT_OPENCODE_ENV` | Comma-separated names of extra environment variables to pass to opencode in a run with a generated config (for example `OTEL_EXPORTER_OTLP_ENDPOINT`). Env only, no flag. Exact names, spaces trimmed. A name that is not a variable name, starts with `OPENCODE_` or `PERGENT_`, or is `GITHUB_TOKEN`, `GITLAB_TOKEN`, `PWD`, `XDG_DATA_HOME` or `XDG_STATE_HOME` is a config error (`usage` in agent mode). Empty by default. See [the sandbox](../concepts/sandbox.md) |

### Own-config mode

| Variable | Description |
|---|---|
| `PERGENT_USE_OPENCODE_CONFIG` | `true`/`false` — in GitHub/GitLab CI, allow a review with an empty `OPENCODE_PROVIDER` on opencode's own config. **No sandbox**: do not use on untrusted changes (see [option B](#own-config-mode)). Not needed with `--local`; agent mode always needs a provider |

**Breaking:** every mode needs opencode 2.3.2 or later on the `PATH`, and an older or unreadable version is refused before any review. The image ships it. Own-config mode, `--local` and any run outside the image need it installed. A login stored by opencode 2 (`opencode auth login`) does not reach a run, because each run gets its own data directory with only the 1.x `auth.json` copied in. Give the key through `OPENCODE_API_KEY`, or in own-config mode through the provider's own variable or your opencode config.

Config resolution priority: CLI flags > `PERGENT_*` env vars > auto-detect from CI > defaults.

An unset or empty variable means the default. A value that can't be used is a config error that names the variable and the value (`PERGENT_TIMEOUT="600" is not a duration such as 90s or 10m`), not a silent fall back to the default. Numbers must be whole (`PERGENT_MAX_TURNS` and `PERGENT_CONCURRENCY` at least 1, `PERGENT_CONTEXT_LIMIT` and `PERGENT_OUTPUT_LIMIT` not negative), `PERGENT_TIMEOUT` needs a unit, and `PERGENT_PREVIOUS_REVIEW`, `PERGENT_CHECK_DELETED` and `PERGENT_USE_OPENCODE_CONFIG` are `true` or `false`. `--max-turns` below 1 is rejected as well. A flag that is passed wins, and its variable is not read.
