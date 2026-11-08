<div align="center">
  <img src="docs/assets/logo.png" alt="pergent" width="110">

  <h1>pergent</h1>

  <p><strong>Agentic merge request reviews in your CI pipeline, powered by opencode.</strong></p>

  <p>
    <a href="LICENSE"><img alt="License" src="https://img.shields.io/badge/license-MIT-blue.svg"></a>
    <a href="go.mod"><img alt="Go" src="https://img.shields.io/github/go-mod/go-version/gopaytech/pergent"></a>
    <a href="https://github.com/gopaytech/pergent/pkgs/container/pergent"><img alt="Image" src="https://img.shields.io/badge/ghcr.io-pergent-informational"></a>
  </p>

  <p>
    <a href="#-usage">Quick start</a> ·
    <a href="docs/concepts/skills.md">Skills</a> ·
    <a href="docs/concepts/agent-mode.md">Agent mode</a> ·
    <a href="docs/agent-result-schema.md">Result schema</a>
  </p>
</div>

---

Agentic PR/MR review tool for CI/CD pipelines. Unlike single-shot review tools, pergent spawns [opencode](https://github.com/anomalyco/opencode) as a subprocess that autonomously loops through the repository -- reading files, following references, grepping for patterns -- to build deep context before writing its review.

## ✨ Features

- **Agentic review**: opencode reads files, greps and follows references in the repository before it writes a finding, instead of judging the diff alone.
- **Skills**: review behaviour lives in Markdown skill files, each with its own scope (`paths`, `exclude`) and its own idea of what is blocking.
- **Any size of change**: changed files are split into batches that fit the model's context, and every skill and batch runs in parallel under one time budget.
- **Structured findings and verdicts**: each skill ends `no_blocking_issues`, `has_blocking_issues` or `failed`; blocking findings are re-checked by a separate run before they count.
- **Honest failures**: a timeout, context overflow or provider error is reported with its cause, never as a pass, and the CI job stays green.
- **Read-only sandbox**: the agent can only read, grep, glob and list; edits, shell, web access and `.env` reads are denied, and secrets are redacted from everything pergent prints.
- **One comment, updated**: GitHub and GitLab get a single review comment that is edited on every push instead of piling up; `--previous-review` gives the model the last review's findings as context.
- **Agent mode**: `pergent agent` reviews a prepared checkout with trusted inputs only and delivers a versioned JSON result, for running pergent as a service.

## 🔁 How it works

1. You define review behavior via **skill files** (`.md` files with review instructions)
2. For each skill, pergent picks the changed files in its scope, groups them into batches that fit the model's context, and spawns one `opencode run` per batch, attaching that batch's diff
3. The agent explores the repo iteratively (reads files, greps, follows references) and answers with structured findings
4. pergent merges the findings, re-checks the blocking ones, decides a verdict per skill, and posts one combined review comment on the PR/MR

Skills and batches run in parallel (see `--concurrency`), all within one time budget for the whole review (see `--timeout`).

A run gets only its own batch's diff attached. So that a run can see how another changed file changed (a skill with `paths`, or a batch of a big change, would otherwise miss a problem spanning two areas), pergent writes the diff of each reviewable changed file to its own temporary file, plus an index of them, once per review. Nothing is attached: a run that does not cover every such file is told in its message where the index is (batch, cross-batch and verification runs alike), and it can read the index and then the diffs it needs. A run may read them; nothing proves it does. Only files that at least one skill reviews, with no default exclusion, whose path the read rules do not deny (`*.env`, `*.env.*` except `*.env.example`, and in agent mode `*.pem`, `*.key`, `*.tfvars`) and whose diff fits in a batch, are listed. If the files cannot be written, pergent logs a warning and the review runs without them. When you use opencode's own config (no `OPENCODE_PROVIDER`), nothing is written and no run is told about diffs, because only pergent's generated config lets a run read outside the repository.

The agent also picks up project context from the repo root's `AGENTS.md` (or `CLAUDE.md` if there is no `AGENTS.md`). The file may be a symlink, but only to a regular file inside the checkout that the read rules allow (`CLAUDE.md -> AGENTS.md` is fine). A link that leaves the checkout, does not resolve, or points at a file the read rules deny (such as `prod.env`) is skipped with a warning naming it, and the next name is tried.

## 📦 Installation

### Docker (recommended for CI)

```bash
docker pull ghcr.io/gopaytech/pergent:latest
```

The image is built for `linux/amd64` and `linux/arm64` (from v0.1.2); Docker pulls the one that matches the host. It ships `opencode`, `git` and `ripgrep` (opencode's glob and grep tools need `rg`; from v0.1.3).

### Build from source

```bash
git clone https://github.com/gopaytech/pergent.git
cd pergent
make build
# Binary at ./bin/pergent
```

Requires [opencode](https://github.com/anomalyco/opencode) installed on the system.

A build from source reports its version as `dev`; only a release image carries a real one. pergent.sh treats `dev` as older than any release, so such an image gets full reviews.

## 🚀 Usage

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

If you already have opencode configured (e.g., GitHub Copilot, custom `opencode.json`), leave `OPENCODE_PROVIDER` (and `OPENCODE_MODEL`) unset. pergent will use opencode's own config. In this mode pergent does not generate a config, so the [agent sandbox](docs/concepts/sandbox.md) above does not apply: opencode runs with whatever agents and permissions your config defines. pergent attaches the skill's instructions (with the output format) to each run as a file `review-instructions.md`, because they would otherwise live only in the config it did not write.

- With `--local` this needs nothing more: it is your own machine. `OPENCODE_MODEL` without `OPENCODE_PROVIDER` is ignored and logs a warning.
- **In GitHub or GitLab CI it needs an opt-in: set `PERGENT_USE_OPENCODE_CONFIG=true`.** Without it, an empty `OPENCODE_PROVIDER` is a config error (an unset CI variable expands to an empty one, and the review would silently lose its sandbox). The error also names `OPENCODE_MODEL` when that is set without a provider.
- **This mode has no sandbox, so do not use it on untrusted changes.** The project config is on, so the change's own `AGENTS.md` and `opencode.json` (agents, plugins, permissions) apply, opencode may run commands, and `GITHUB_TOKEN` / `GITLAB_TOKEN` stay in opencode's environment because it may sign in with one. A pull request from a fork or an outside contributor can use that. pergent logs a warning on every such run. For untrusted changes, set `OPENCODE_PROVIDER` so pergent runs the review in its read-only sandbox.

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

## 📚 Documentation

**Concepts**

| | |
| --- | --- |
| [Skills](docs/concepts/skills.md) | Preset and custom skills, scope, what counts as blocking |
| [The review comment](docs/concepts/review-comment.md) | What the comment shows, and its layouts |
| [The change's title and description](docs/concepts/change-description.md) | How a review uses what the author wrote |
| [Previous review as context](docs/concepts/previous-review.md) | How a re-review knows what it said before |
| [When a review fails](docs/concepts/failed-reviews.md) | What a failed run means, and when CI goes red |
| [Agent sandbox](docs/concepts/sandbox.md) | What the review agent can and can't do |
| [Agent mode](docs/concepts/agent-mode.md) | `pergent agent`, for running pergent from another service |
| [How it differs from pr-agent](docs/concepts/comparison.md) | The comparison |

**Reference**

| | |
| --- | --- |
| [CLI flags and environment variables](docs/reference/configuration.md) | Every flag and variable |
| [Custom LLM](docs/setup/custom-llm.md) | LiteLLM, vLLM, Ollama |
| [Agent result schema](docs/agent-result-schema.md) | The JSON `pergent agent` delivers |
| [Troubleshooting](docs/troubleshooting.md) | Common errors and what to do |

## 📄 License

MIT. See [LICENSE](LICENSE).
