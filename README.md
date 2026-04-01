# pergent

Agentic PR/MR review tool for CI/CD pipelines. Unlike single-shot review tools, pergent spawns [opencode](https://github.com/anomalyco/opencode) as a subprocess that autonomously loops through the repository -- reading files, following references, grepping for patterns -- to build deep context before writing its review.

## How It Works

1. You define review behavior via **skill files** (`.md` files with review instructions)
2. pergent spawns one `opencode run` per skill, attaching the PR diff
3. The agent explores the repo iteratively (reads files, greps, follows references)
4. pergent collects outputs and posts a combined review comment on the PR/MR

The agent also picks up project context from `CLAUDE.md` / `AGENTS.md` automatically.

## Installation

### Docker (recommended for CI)

```bash
docker pull ghcr.io/zufardhiyaulhaq/pergent:latest
```

### Build from source

```bash
git clone https://github.com/gopaytech/pergent.git
cd pergent
make build
# Binary at ./bin/pergent
```

Requires [opencode](https://github.com/anomalyco/opencode) installed on the system.

## Usage

### Local Mode

Run reviews locally and print output to stdout. No platform token or PR required.

**Prerequisites:**
- [opencode](https://github.com/anomalyco/opencode) installed
- A git repo with commits ahead of the base branch

**Setup LLM (option A -- configure via env vars):**

```bash
export OPENCODE_PROVIDER=anthropic          # or openai, litellm, etc.
export OPENCODE_API_KEY=sk-your-api-key     # your LLM API key
export OPENCODE_MODEL=claude-sonnet-4       # model to use
```

**Setup LLM (option B -- use opencode's existing config):**

If you already have opencode configured (e.g., GitHub Copilot, custom `opencode.json`), just don't set any `OPENCODE_*` env vars. pergent will use opencode's own config.

**Test the connection:**

```bash
pergent --test
```

**Run:**

```bash
# Using a preset skill
pergent --local --skill code-review

# Using a custom skill file
pergent --local --skill ./my-skills/custom-review.md

# Custom base branch (default: main)
pergent --local --base-branch develop --skill code-review

# Multiple skills
pergent --local --skill code-review --skill ./my-skills/security-check.md
```

### GitLab CI

```yaml
mr-review:
  stage: review
  image: ghcr.io/zufardhiyaulhaq/pergent:latest
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
  script:
    - pergent --skill code-review
  variables:
    GITLAB_TOKEN: $GITLAB_TOKEN
    OPENCODE_PROVIDER: anthropic
    OPENCODE_API_KEY: $ANTHROPIC_API_KEY
    OPENCODE_MODEL: claude-sonnet-4
```

### GitHub Actions

```yaml
name: PR Review
on:
  pull_request:
    types: [opened, synchronize]

jobs:
  review:
    runs-on: ubuntu-latest
    container:
      image: ghcr.io/zufardhiyaulhaq/pergent:latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Run pergent
        run: pergent --skill code-review
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          OPENCODE_PROVIDER: anthropic
          OPENCODE_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
          OPENCODE_MODEL: claude-sonnet-4
```

### Custom LLM (LiteLLM, vLLM, Ollama)

pergent works with any OpenAI-compatible API. Set `OPENCODE_BASE_URL` to your endpoint and pergent automatically uses `/chat/completions` instead of `/responses`:

```bash
export OPENCODE_PROVIDER=openai
export OPENCODE_API_KEY=your-key
export OPENCODE_MODEL=your-model
export OPENCODE_BASE_URL=https://your-proxy.example.com/v1

pergent --local --skill code-review
```

## Skills

Skills are markdown files that define review behavior. The `--skill` flag accepts either a **preset name** or a **file path**.

### Preset Skills

Built-in skills embedded in the binary:

| Name | Description |
|---|---|
| `code-review` | General code quality, bugs, clarity, performance, security |

```bash
pergent --skill code-review
```

### Custom Skills

Create your own `.md` file with optional YAML frontmatter:

```yaml
---
name: security-review
---

You are a security engineer reviewing a pull request.
Focus on authentication, authorization, injection vulnerabilities,
and sensitive data exposure.
...
```

```bash
pergent --skill ./skills/security-review.md
```

## CLI Flags

| Flag | Description | Default |
|---|---|---|
| `--skill` | Preset name or path to skill `.md` file (repeatable) | required |
| `--local` | Local mode: diff from git, output to stdout | `false` |
| `--base-branch` | Base branch for local git diff | `main` |
| `--test` | Test opencode connection (sends a hello prompt) | `false` |
| `--platform` | `github` or `gitlab` | auto-detect |
| `--max-turns` | Max agentic turns per skill run | `20` |
| `--max-tokens` | Max token usage per skill run | `100000` |
| `--timeout` | Max wall-clock time per skill run | `10m` |
| `--repo-path` | Path to repo root | `.` |

## Environment Variables

### LLM (passed through to opencode)

| Variable | Description |
|---|---|
| `OPENCODE_PROVIDER` | LLM provider (`anthropic`, `openai`, etc.) |
| `OPENCODE_API_KEY` | API key for the LLM provider |
| `OPENCODE_MODEL` | Model to use (e.g., `claude-sonnet-4`) |
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
| `PERGENT_MAX_TURNS` | Max agentic turns per skill run |
| `PERGENT_MAX_TOKENS` | Max token usage per skill run |
| `PERGENT_TIMEOUT` | Max wall-clock time per skill run |

Config resolution priority: CLI flags > `PERGENT_*` env vars > auto-detect from CI > defaults.

## Troubleshooting

### opencode seems slow or stuck

pergent shows step-by-step progress as opencode works:
```
Running skill: code-review
  Step 1: starting...
  Step 1: tool read
  Step 1: done (472 tokens)
  Step 2: starting...
  Step 2: generating text...
  Step 2: done (800 tokens)
```

If no steps appear, the model may be slow to respond. Test the connection:
```bash
pergent --test
```

If `--test` also hangs, test opencode directly:
```bash
opencode run "Say hello"
```

If that also hangs, the issue is your LLM connection, not pergent.

### Empty review output

Dump the raw opencode output for inspection:
```bash
PERGENT_DEBUG_OUTPUT=/tmp/pergent-raw.json pergent --local --skill code-review
cat /tmp/pergent-raw.json
```

This shows the raw NDJSON events. Check if there are `"type":"text"` events with content.

### `git diff failed: exit status 128`

Common causes:
- **Wrong base branch**: your repo uses `master` not `main`. Use `--base-branch master`.
- **No git repo**: run `git init` and make at least one commit.
- **No remote**: `origin/main` doesn't exist. Push to a remote or create the ref.

### `/responses` API errors

If you see errors mentioning `/responses: Invalid model name` or 404s from `/v1/responses`, your API proxy doesn't support OpenAI's Responses API. pergent handles this automatically when `OPENCODE_BASE_URL` is set — it uses `@ai-sdk/openai-compatible` which calls `/chat/completions` instead. Make sure `OPENCODE_BASE_URL` ends with `/v1`:
```bash
export OPENCODE_BASE_URL=https://your-proxy.example.com/v1
```

### `ContextWindowExceededError`

The model's context window is too small for the diff + skill instructions. Either:
- Use `--max-tokens` to match your model's context: `--max-tokens 30000`
- Use a model with a larger context window

### `Configuration is invalid` from opencode

If you set `OPENCODE_PROVIDER`, pergent generates a config for opencode. If the generated config doesn't match your opencode version, unset the provider env vars and let opencode use its own config:
```bash
unset OPENCODE_PROVIDER OPENCODE_MODEL OPENCODE_BASE_URL OPENCODE_API_KEY
pergent --local --skill code-review
```

### Using opencode's existing config (GitHub Copilot, etc.)

If you already have opencode configured (e.g., connected to GitHub Copilot), just don't set any `OPENCODE_*` env vars. pergent will skip config generation and opencode uses its own config:
```bash
pergent --local --skill code-review
```

### `Ctrl+C` to cancel

Pressing `Ctrl+C` cleanly stops both pergent and the opencode subprocess.

## How It Differs from pr-agent

| | pergent | pr-agent |
|---|---|---|
| Review approach | Agentic loop -- reads files, greps, follows references | Single API call with static diff |
| Context | Builds context iteratively, on-demand | Entire diff sent in one prompt |
| LLM support | Any provider via opencode | Specific provider integrations |
| Customization | Skill files (`.md`) | Configuration flags |
| Small context models | Works with 128k models (reads on-demand) | Needs large context for big diffs |

## License

MIT
