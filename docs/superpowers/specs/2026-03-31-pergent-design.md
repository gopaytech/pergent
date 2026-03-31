# pergent -- Design Document

**Date:** 2026-03-31
**Status:** Draft

---

## Overview

`pergent` is a CLI tool that runs agentic PR/MR reviews in CI/CD pipelines. It spawns `opencode` CLI as a subprocess, giving the agent the ability to autonomously loop through the repository -- reading files, following references, grepping for patterns -- to build deep context before writing its review. The result is posted as a comment on GitHub PRs or GitLab MRs.

**Key differentiator from tools like pr-agent:** Instead of a single API call with a static diff dump, `pergent` leverages an agentic CLI that explores the repo iteratively, just like a human reviewer would. Users define review behavior through skill files and the agent inherits project context from `CLAUDE.md`/`AGENTS.md` automatically.

---

## Goals

- Agentic review: the LLM loops to gather context, not a single-shot API call
- Skill-driven: users define review behavior via `.md` skill files
- Platform-agnostic: works on GitHub Actions, GitLab CI, or any CI runner
- LLM-agnostic: supports Anthropic, OpenAI, or self-deployed models via `opencode`
- Controllable cost: max turn and max token limits prevent runaway loops
- Minimal config: env vars for secrets, flags for runtime behavior

---

## Architecture

```
                     pergent CLI
                         |
          +--------------+--------------+
          |              |              |
     Skill Loader   Config Builder   Platform Client
          |              |              |
     Parse .md      Generate          GitHub API /
     frontmatter    opencode.json     GitLab API
     + body              |
          |              |
          +--------------+
                 |
            Subprocess Runner
                 |
       (one opencode run per skill)
                 |
         (agentic loop: read files,
          grep, git diff, etc.)
                 |
            Captured Outputs
                 |
           Comment Formatter
                 |
     POST / PATCH comment to platform
```

### Components

**Skill Loader** -- Parses `.md` skill files. Extracts YAML frontmatter (name, metadata) and the body (review instructions prompt).

**Config Builder** -- Generates a temporary `opencode.json` with the correct provider, model, token limits, turn limits (`steps`), and read-only tool permissions. Written to a temp directory and passed to `opencode` via `OPENCODE_CONFIG` env var, so it does not conflict with any existing repo or user-level config.

**Subprocess Runner** -- Spawns one `opencode run` per skill with the skill prompt and diff file attachment. Captures stdout in JSON format. Enforces a configurable timeout as a safety net -- kills the process if exceeded.

**Comment Formatter** -- Collects output from each skill run and assembles a single markdown comment with per-skill sections, file list, and collapsible sections for long output.

**Platform Client** -- Handles two responsibilities:
1. **Fetch diff** -- Falls back to platform API when local `git diff` fails (e.g., shallow clone).
2. **Post/update comment** -- Posts the formatted comment to the platform API. On re-runs, finds the existing `pergent` comment (via HTML marker `<!-- pergent:skill-name -->`) and updates it instead of posting a duplicate.

---

## CLI Interface

```bash
pergent \
  --skill ./skills/code-review.md \
  --skill ./skills/security-review.md \
  --platform github
```

### Flags

| Flag | Description | Default |
|---|---|---|
| `--skill` | Path to skill `.md` file (repeatable) | required |
| `--platform` | `github` or `gitlab` | auto-detect from CI env |
| `--max-turns` | Max agentic turns per skill run | `20` |
| `--max-tokens` | Max token usage per skill run | `100000` |
| `--timeout` | Max wall-clock time per skill run (e.g., `10m`) | `10m` |
| `--repo-path` | Path to repo root | `.` |

### Environment Variables

**Platform:**

| Variable | Description |
|---|---|
| `PERGENT_PLATFORM` | `github` or `gitlab` (alternative to `--platform`) |
| `GITHUB_TOKEN` | GitHub API access token |
| `GITLAB_TOKEN` | GitLab API access token |

**LLM (passed through to opencode):**

| Variable | Description |
|---|---|
| `OPENCODE_PROVIDER` | LLM provider (e.g., `anthropic`, `openai`, custom endpoint) |
| `OPENCODE_API_KEY` | API key for the LLM provider |
| `OPENCODE_MODEL` | Model to use (e.g., `claude-sonnet-4`) |

**Limits (alternative to flags):**

| Variable | Description |
|---|---|
| `PERGENT_MAX_TURNS` | Max agentic turns per skill run |
| `PERGENT_MAX_TOKENS` | Max token usage per skill run |
| `PERGENT_TIMEOUT` | Max wall-clock time per skill run |

**Auto-detected from CI (no manual setup needed):**

| Variable | Source |
|---|---|
| `GITHUB_ACTIONS` | Detects GitHub Actions environment |
| `GITHUB_REPOSITORY` | `owner/repo` format |
| `GITHUB_EVENT_PATH` | JSON file with PR number and base branch |
| `CI_MERGE_REQUEST_IID` | GitLab CI MR number |
| `CI_MERGE_REQUEST_TARGET_BRANCH_NAME` | GitLab CI base branch |
| `CI_PROJECT_ID` | GitLab CI project ID |
| `CI_SERVER_URL` | GitLab instance URL |

**Resolution priority:** CLI flags > `PERGENT_*` env vars > auto-detect from CI.

---

## Skill File Format

A skill is a Markdown file with optional YAML frontmatter:

```yaml
---
name: code-review
---

You are a senior engineer reviewing a pull request.
Focus on correctness, clarity, and potential bugs.
Point out any security issues you find.
...
```

- **Frontmatter** declares the skill name and optional metadata
- **Body** is the review instruction prompt sent to the LLM
- Skills live anywhere -- `pergent` takes a path, so you can keep them in the repo or a shared skills repo

---

## Execution Flow

```
1. Parse CLI flags and resolve config
   +-- Read --skill files
   +-- Resolve platform (flag > env > auto-detect)
   +-- Resolve limits (flag > env > defaults)

2. Gather diff
   +-- Try local git diff (base branch from CI env vars)
   +-- If local diff fails (shallow clone), fetch from platform API
   +-- Write diff to a temp file

3. Build opencode config
   +-- Generate temporary opencode.json in temp directory
   +-- Set provider, model, steps (max turns), token limits
   +-- Restrict to read-only tools (file read, grep, git diff)
   +-- Pass config path via OPENCODE_CONFIG env var

4. For each skill:
   a. Assemble prompt
      +-- Skill body as the prompt
      +-- Attach diff as a file: opencode run --file /tmp/diff.patch
   b. Spawn opencode
      +-- OPENCODE_CONFIG=/tmp/pergent/opencode.json opencode run --file /tmp/diff.patch --format json "prompt"
      +-- opencode reads CLAUDE.md/AGENTS.md automatically for repo context
      +-- Agent loops: reads files, greps, follows references as needed
      +-- pergent monitors: kill process if timeout exceeded
   c. Capture output
      +-- Parse JSON output from opencode
      +-- Store result for this skill

5. Format combined comment
   +-- Assemble per-skill sections into one markdown comment
   +-- Add HTML markers (<!-- pergent:skill-name -->) for comment dedup
   +-- Add collapsible sections if output is long

6. Post or update comment
   +-- Search for existing pergent comment (by HTML marker)
   +-- If found: PATCH (update in place)
   +-- If not found: POST (new comment)
   +-- GitHub: /repos/:owner/:repo/issues/:number/comments
   +-- GitLab: /api/v4/projects/:id/merge_requests/:iid/notes
```

---

## How Context Works

The agent gets context from three sources:

1. **Skill file** -- The review instructions (what to look for). Passed as the prompt.
2. **CLAUDE.md / AGENTS.md** -- Project context (what this repo is, conventions, architecture). Picked up automatically by `opencode` from the repo root.
3. **Repository itself** -- The agent explores the repo via its built-in read-only tools (file read, grep, git diff). It discovers what it needs rather than being fed everything upfront.

This means even a model with 128k context works well -- it reads files on demand rather than loading everything at once.

---

## opencode Configuration

`pergent` generates a temporary `opencode.json` to control the subprocess. Key settings:

```json
{
  "provider": "<from OPENCODE_PROVIDER>",
  "model": "<from OPENCODE_MODEL>",
  "agents": {
    "default": {
      "steps": 20,
      "limit": {
        "context": 100000,
        "output": 10000
      }
    }
  }
}
```

**Read-only tools:** The config restricts `opencode` to read-only operations only (file read, grep, git diff). No file writes, no bash execution. This prevents the review agent from modifying the repo and avoids non-interactive permission prompt issues.

**Config isolation:** The config is written to a temp directory (e.g., `/tmp/pergent-<random>/opencode.json`) and passed via `OPENCODE_CONFIG` env var. This ensures no conflict with existing `opencode.json` in the repo or user home.

---

## Comment Deduplication

On re-runs (e.g., new push to the same PR), `pergent` updates the existing comment instead of posting a new one.

**Mechanism:** Each skill's output section is wrapped with an HTML marker:
```markdown
<!-- pergent:code-review -->
### code-review
...
<!-- /pergent:code-review -->
```

Before posting, `pergent` searches existing PR comments for these markers. If found, it updates (PATCH) the existing comment. If not, it creates (POST) a new one.

**One comment per `pergent` run:** All skill results are combined into a single comment. The markers are per-skill within that comment for future granularity, but v1 updates the entire comment as a unit.

---

## Project Structure

```
pergent/
+-- cmd/
|   +-- main.go                # CLI entrypoint, flag parsing
+-- internal/
|   +-- skill/
|   |   +-- loader.go          # Skill .md parser (frontmatter + body)
|   +-- config/
|   |   +-- resolver.go        # Resolves platform, limits from flags/env/auto-detect
|   |   +-- opencode.go        # Generates temporary opencode.json
|   +-- runner/
|   |   +-- runner.go          # Spawns opencode subprocess, captures output
|   |   +-- monitor.go         # Monitors subprocess, enforces timeout
|   +-- output/
|   |   +-- formatter.go       # LLM outputs -> combined markdown comment
|   +-- platform/
|       +-- platform.go        # Platform interface (fetch diff + post comment)
|       +-- github.go          # GitHub: PR diff API + PR comments API
|       +-- gitlab.go          # GitLab: MR diff API + MR notes API
+-- skills/
|   +-- code-review.md         # Example bundled skill
+-- Dockerfile                 # Bundles pergent + opencode
+-- Makefile
+-- .github/
|   +-- workflows/
|       +-- example.yml        # Example GitHub Actions usage
+-- .gitlab-ci.yml             # Example GitLab CI usage
+-- README.md
```

---

## Distribution

`pergent` is distributed as a **Docker image** (`ghcr.io/zufardhiyaulhaq/pergent:latest`) that bundles both `pergent` and `opencode` pre-installed. This gives CI runners zero-dependency setup.

```dockerfile
FROM golang:1.22 AS builder
# ... build pergent binary ...

FROM ubuntu:24.04
# Install opencode
# Copy pergent binary
ENTRYPOINT ["pergent"]
```

---

## Example CI Usage

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
        run: pergent --skill ./skills/code-review.md
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          OPENCODE_PROVIDER: anthropic
          OPENCODE_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
          OPENCODE_MODEL: claude-sonnet-4
```

### GitLab CI

```yaml
mr-review:
  stage: review
  image: ghcr.io/zufardhiyaulhaq/pergent:latest
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
  script:
    - pergent --skill ./skills/code-review.md
  variables:
    GITLAB_TOKEN: $GITLAB_TOKEN
    OPENCODE_PROVIDER: anthropic
    OPENCODE_API_KEY: $ANTHROPIC_API_KEY
    OPENCODE_MODEL: claude-sonnet-4
```

### Multiple Skills with File Filtering

```yaml
name: PR Review
on:
  pull_request:
    types: [opened, synchronize]
    paths:
      - '**.go'

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
        run: |
          pergent \
            --skill ./skills/code-review.md \
            --skill ./skills/security-review.md
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          OPENCODE_PROVIDER: anthropic
          OPENCODE_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
          OPENCODE_MODEL: claude-sonnet-4
```

---

## Output Comment Format

```markdown
## pergent review

**Skills:** code-review, security-review
**Files changed:** `internal/gateway/handler.go`, `internal/model/claude.go`

<!-- pergent:code-review -->
### code-review

- **[handler.go:42]** Missing error handling on `json.Unmarshal` -- if the payload
  is malformed, this will silently return a zero-value struct.
- **[claude.go:87]** HTTP client has no timeout set. Add `Timeout: 30 * time.Second`
  to avoid hanging the CI job.
<!-- /pergent:code-review -->

<!-- pergent:security-review -->
### security-review

- No security issues found in this change.
<!-- /pergent:security-review -->

### Summary

Overall the change is clean. One error handling issue and one timeout issue worth
fixing before merge.
```

---

## Limits and Safety

**Max turns** (`--max-turns` / `PERGENT_MAX_TURNS`): Caps the number of agentic steps `opencode` can take per skill run. Configured via the `steps` field in the generated `opencode.json`. Default: `20`.

**Max tokens** (`--max-tokens` / `PERGENT_MAX_TOKENS`): Caps total token usage per skill run. Configured via the `limit` field in the generated `opencode.json`. Default: `100000`.

**Timeout** (`--timeout` / `PERGENT_TIMEOUT`): Max wall-clock time per skill run. `pergent` kills the `opencode` subprocess if this is exceeded. Default: `10m`.

**Read-only tools:** `opencode` is configured to only use read-only tools (file read, grep, git diff). No file writes, no bash execution. This prevents repo modification and avoids interactive permission prompts in CI.

**Truncated output:** If a skill run is killed due to timeout, `pergent` posts whatever output was captured so far with a note that the review was truncated.

---

## Backend Design

`opencode` is the initial and primary backend. The runner is designed with a simple interface to allow future backends:

```go
type Backend interface {
    BuildConfig(opts RunOptions) (configPath string, err error)
    BuildCommand(prompt string, diffFile string, opts RunOptions) *exec.Cmd
    ParseOutput(stdout []byte) (string, error)
}
```

Adding `claude` CLI or another backend is one file implementing this interface.

---

## Out of Scope (v1)

- Inline line-level diff comments (top-level comment only)
- Generic webhook output
- Multi-backend support (opencode only for v1)
- Streaming output to CI logs
- Caching diffs between pipeline runs
- Binary releases (Docker image only for v1)
