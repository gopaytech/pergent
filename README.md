# pergent

Agentic PR/MR review tool for CI/CD pipelines. Unlike single-shot review tools, pergent spawns [opencode](https://github.com/anomalyco/opencode) as a subprocess that autonomously loops through the repository -- reading files, following references, grepping for patterns -- to build deep context before writing its review.

## How It Works

1. You define review behavior via **skill files** (`.md` files with review instructions)
2. For each skill, pergent picks the changed files in its scope, groups them into batches that fit the model's context, and spawns one `opencode run` per batch, attaching that batch's diff
3. The agent explores the repo iteratively (reads files, greps, follows references) and answers with structured findings
4. pergent merges the findings, re-checks the blocking ones, decides a verdict per skill, and posts one combined review comment on the PR/MR

Skills and batches run in parallel (see `--concurrency`), all within one time budget for the whole review (see `--timeout`).

The agent also picks up project context from the repo root's `AGENTS.md` (or `CLAUDE.md` if there is no `AGENTS.md`).

### Agent sandbox

When `OPENCODE_PROVIDER` is set, pergent writes a temporary `opencode.json` for each opencode run and points opencode at it with `OPENCODE_CONFIG`. That config:

- defines a `pergent` agent that carries the skill's prompt (its body plus pergent's output-format instructions) and `--max-turns` as its step limit, and makes it the default agent (pergent also passes `--agent pergent`)
- allows only `read`, `glob`, `grep` and `list`, and denies `edit` (which covers file writes and patches), `bash`, `task`, `skill`, `webfetch` and `websearch`
- denies reading `*.env` / `*.env.*` files (except `*.env.example`)
- turns off opencode's auto-compaction (`compaction.auto: false`), so a run that fills the model's context fails with the overflow error instead of continuing from a summary
- leaves the model's context and output limits to opencode's model registry, unless you set `--context-limit`

pergent also sets `OPENCODE_DISABLE_PROJECT_CONFIG=true`, so an `opencode.json` or `.opencode/` directory (agents, plugins, commands) in the reviewed branch can't change these permissions or run plugin code while your platform token and LLM key are in the environment. Because that setting also stops opencode from finding `AGENTS.md` / `CLAUDE.md` by itself, pergent passes the root file to opencode explicitly.

## When a review fails

Each opencode run (one per batch) ends in one of two ways: the model's own final answer, in the required format, is parsed into findings, or the batch is reported as **Review failed:** with the cause and the files it covered. pergent never posts opencode's internal messages or the model's in-progress text ("Let me check...") as a review. A skill with any failed batch has the verdict **failed**, never "no blocking issues", even if the other batches found nothing.

| Cause | Example |
|---|---|
| Context overflow | The model's context window overflowed after 14 steps: the last completed step used 186,400 tokens (limit: the model's context window as known to opencode). ContextOverflowError: prompt is too long: 201234 tokens > 200000 maximum. |
| Timeout | Timed out after 10m0s (review budget 10m0s) during step 9; the last tool call was `read` on `service/store.go`. |
| opencode error | opencode reported APIError (HTTP 429): Rate limit exceeded. Gave up after 4 attempts. |
| Step limit | Reached the 20-step limit before the model finished the review (raise --max-turns to allow more steps). |
| opencode exited non-zero | opencode exited with code 1 at step 2 without producing a review. (The last lines of opencode's stderr follow in a code block.) |
| No answer | opencode exited normally after 6 steps, but the model did not produce a final answer (last step finished with reason "tool-calls"). |

- **The CI job stays green** when a review fails; the cause is in the comment and the job log. Configuration errors found before any review starts (missing token, bad flag) still fail the job.
- **Transient provider errors** (HTTP 429, 5xx, connection errors) are retried up to 3 times with exponential backoff (5s, 10s, 20s) within the review's `--timeout` budget. opencode already retries these itself before giving up, so pergent's retries cover longer outages. If every attempt fails, the last error is reported as is.
- **Step limit:** on the last allowed step opencode asks the model to stop and summarise its progress. That summary is not a review, so it is reported as a step-limit failure. If the model ignores the instruction and keeps calling tools, pergent stops opencode.
- **An answer in the wrong format** (no JSON block, or a block that does not parse) gets one repair run that rewrites it; if that fails too, the batch is reported as failed with an excerpt of the answer.
- **Secrets are redacted** from the comment and the log: the values of `OPENCODE_API_KEY`, `GITHUB_TOKEN` and `GITLAB_TOKEN`, bearer tokens, and API keys in URLs.

## Installation

### Docker (recommended for CI)

```bash
docker pull ghcr.io/gopaytech/pergent:latest
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

If you already have opencode configured (e.g., GitHub Copilot, custom `opencode.json`), just don't set any `OPENCODE_*` env vars. pergent will use opencode's own config. In this mode pergent does not generate a config, so the [agent sandbox](#agent-sandbox) above does not apply: opencode runs with whatever agents and permissions your config defines.

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
  image: ghcr.io/gopaytech/pergent:latest
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
      image: ghcr.io/gopaytech/pergent:latest
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

### Scope: `paths`, `exclude` and `cross_batch`

The frontmatter can narrow which changed files a skill reviews and ask for an extra check across batches:

```yaml
---
name: api-review
paths:
  - "api/**"
  - "*.proto"
exclude:
  - "**/*_test.go"
cross_batch: true
---
```

| Field | Meaning |
|---|---|
| `paths` | Glob patterns. When set, the skill reviews only changed files matching one of them. Files that match none are listed under "Skipped files" as out of scope. Unset means every changed file. |
| `exclude` | Glob patterns for files the skill skips even though they are in scope. |
| `cross_batch` | `true` adds one extra run, after all batches, that looks for problems spanning batches (for example conflicting changes in different directories). It only runs when the change needed more than one batch. Default `false`. |

Patterns use `/`-separated paths from the repo root. `*` matches within one path segment and `**` matches any number of segments. A pattern with no `/` (such as `*.proto`) matches the file name at any depth.

### Default exclusions

Every skill skips these files, and the comment lists them under "Skipped files" with the reason:

- **Always skipped:** deleted files, binary files, and files with no content changes (for example a pure rename).
- **Skipped unless a skill's `paths` match them:** lockfiles (`package-lock.json`, `go.sum`, `yarn.lock`, `pnpm-lock.yaml`, `Cargo.lock`, `composer.lock`, `Gemfile.lock`, `poetry.lock`, `Pipfile.lock`), vendored code (anything under a `vendor`, `node_modules` or `third_party` directory), and generated files (paths marked `linguist-generated` in the repo's `.gitattributes`, or files whose first lines say the code is generated and not to be edited).

A file larger than a batch can hold (about a quarter of the model's context window, 40,000 tokens when the window is unknown) is not reviewed and is reported as a failure for that file.

### The `## Blocking` section

A review has two kinds of findings: **blocking** ones, which decide the verdict, and **suggestions**. Tell the skill what blocks by adding a `## Blocking` heading (any level) to its body and listing the problems that must be fixed before merging:

```markdown
## Blocking

- Logic errors that cause wrong behaviour, crashes, or data loss.
- Security vulnerabilities.

Everything else is a suggestion.
```

When a skill has no `## Blocking` section, pergent appends a default one: bugs, security vulnerabilities and broken configuration are blocking, and everything else is a suggestion. Before a blocking finding is reported, pergent runs a separate check that opens the code again and drops the finding if the problem is not real.

### Checklists become a checks table

If a skill contains a checklist (section headings with items under them, as bullets or checkboxes), the model reports every item as a check: pass, fail or n/a, with the evidence it compared. The comment shows them as one table per section. No special syntax is needed; write the checklist as you would for a human reviewer. The `## Blocking` section is not treated as a checklist. Findings still decide the verdict: a failed check should come with a finding, and the comment notes it when one doesn't.

### Don't put output-format instructions in skills

pergent appends its own output-format instructions to every skill: the model must end its answer with one JSON block holding a short `summary` of what it checked and concluded, its `checks` (the skill's checklist, if it has one), and its findings. A missing summary is not an error; the review still counts. Don't tell a skill how to format its answer (no "use bullet points", "start with a summary", or JSON schemas of your own). Instructions like that conflict with the required format and make runs fail. Write what to look for and what counts as blocking; pergent handles the rest.

## The review comment

pergent posts one comment per PR/MR, updated in place on each run. It starts with a collapsed list of the changed files, then has one section per skill:

- a verdict line first: `No blocking issues`, `N blocking issues`, or a warning that part of the review failed (and which files it did not cover)
- the model's summary of what it checked and concluded, written even when it found nothing: a `Summary:` line, or one bullet per batch (`Batch N of M`) for a batched review
- the checks, when the skill has a checklist: a count line (`Checks: 14 passed · 1 failed · 2 n/a`) and one table per checklist section (✅ pass, ❌ fail, ➖ n/a). In a batched review each batch's tables are collapsed, and a batch with a failed check starts open.
- each blocking finding in full: `file:line`, a title, the explanation and an optional suggested fix
- the suggestions in a collapsed block
- the files the skill reviewed, in a collapsed block
- the skipped files, with the reason for each, in a collapsed block
- notes, such as a skill with no files in scope, or a verification that was skipped

A skill with failed batches still shows the findings from the batches that finished, next to the failure.

A clean review looks like this:

```markdown
### code-review

✅ **No blocking issues**

**Summary:** Checked the handler and its tests in api/: input is validated before use, errors are returned to the caller, and the new query is parameterised.

**Checks:** 2 passed

**Input handling**

| Check | Status | Detail |
|---|---|---|
| Request body is validated | ✅ | `api/handler.go` calls `validate(req)` before use |
| Query is parameterised | ✅ | `db.Query("... WHERE id = $1", id)` |

<details><summary>Reviewed files (2)</summary> …
```

The last line of the comment is a hidden HTML comment holding the findings for the next run (see below). Don't edit it. Secrets are redacted from the comment and from that block.

A comment that would exceed the platform's size limit (65,536 characters on GitHub, 1,000,000 on GitLab) is shrunk to fit rather than failing to post: first the hidden findings block is dropped (the next `--previous-review` run is then a first review), then the suggestions' explanations and fixes, then the suggestions, then the passed and n/a checks, then the check tables (the count line stays), then the summaries, then the file lists are cut to a count. Blocking findings and failures are kept; each step leaves a note in the comment.

## Previous Review as Context

By default each run reviews from scratch. With `--previous-review` (or `PERGENT_PREVIOUS_REVIEW=true`), pergent fetches the review comment it posted on the previous run, reads the findings from its hidden findings block, and attaches each skill's findings to that skill's runs alongside the diff. The agent is instructed to stay consistent with them, focus on what changed since, drop findings the new commits resolve, and reuse a finding's fingerprint when it still applies. A skill with no previous findings gets nothing attached.

pergent stays stateless, the PR/MR comment is the only persistent state. First runs (no existing comment) behave exactly as before. Requires platform mode (not available with `--local`).

Comments posted by older versions of pergent have no findings block, so they are ignored: the first run after upgrading is a first review. A findings block that cannot be read is skipped with a warning in the log.

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

## CLI Flags

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
| `PERGENT_MAX_TURNS` | Max agentic turns per opencode run |
| `PERGENT_CONTEXT_LIMIT` | Model context window in tokens (see `--context-limit`) |
| `PERGENT_MAX_TOKENS` | **Deprecated and ignored** (see `--max-tokens`) |
| `PERGENT_TIMEOUT` | Max wall-clock time for the whole review (see `--timeout`; **breaking**: it used to be per skill run) |
| `PERGENT_CONCURRENCY` | Max opencode runs in flight at once (see `--concurrency`) |
| `PERGENT_PREVIOUS_REVIEW` | `true`/`false` — attach the previous review's findings as context (explicit `--previous-review` flag wins over this) |

Config resolution priority: CLI flags > `PERGENT_*` env vars > auto-detect from CI > defaults.

## Troubleshooting

### opencode seems slow or stuck

pergent shows step-by-step progress as opencode works. Skills and batches run in parallel, so lines from different runs interleave:
```
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

### `Review failed: ...`

The message is the cause (see [When a review fails](#when-a-review-fails)). To see everything opencode reported, dump its raw output:
```bash
PERGENT_DEBUG_OUTPUT=/tmp/pergent-raw.json pergent --local --skill code-review
cat /tmp/pergent-raw.json
```

This shows the raw NDJSON events (the last attempt's, when a run was retried). The review is the text of the last `step_start`...`step_finish` block whose `step_finish` has a reason other than `tool-calls`. `"type":"error"` events carry opencode's error, and `text` events with `"synthetic":true` are opencode's own messages, never the model's.

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

### `The model's context window overflowed`

The diff, skill instructions and the files the agent read no longer fit in the model's context. Either:
- Use a model with a larger context window
- Lower `--max-turns`, so the agent reads less before answering
- If opencode doesn't know your model's limits (custom `OPENCODE_BASE_URL`), set `--context-limit` to the real window

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
