# pergent Agent Mode -- Design Document

**Date:** 2026-10-29
**Status:** Draft

---

## Overview

`pergent agent` is a new subcommand that reviews a change and **produces a result**. It doesn't post it anywhere. It is piece 1 of the pergent.sh roadmap ([pergent.sh: docs/superpowers/specs/2026-10-29-pergent-sh-roadmap.md](https://github.com/gopaytech/pergent.sh)), the self-hosted service that runs pergent reviews as Kubernetes Jobs. Everything pergent.sh does depends on it.

CI mode stays exactly as it is today. Agent mode reuses the same review engine (`internal/review`, `internal/output`) and differs in four ways:

1. **It never talks to GitHub or GitLab.** It has no platform token and makes no platform API calls. The caller posts the comment, sets the status and approves.
2. **It uses only the inputs it is given.** Skills and project instructions come from paths the caller passes. Agent mode never reads `AGENTS.md`, `CLAUDE.md` or skills from the checkout, because the checkout is the MR's own branch and an MR could rewrite them.
3. **It returns a versioned JSON result** to a file, stdout, or an HTTP endpoint.
4. **It gets the diff from two commits** in a checkout it is given, not from a platform.

## Why

- **Untrusted code, no powerful tokens.** pergent.sh runs each review in a short-lived Kubernetes Job on untrusted MR code. The Job must not hold a token that can comment, approve or write; it gets only the LLM key and a one-time result token.
- **Rules from the target branch.** Approval rules and review rules must come from the target branch or from pergent.sh, never from the MR being reviewed.
- **pergent stays reusable.** It remains a plain CLI that pergent.sh, or any other system, can drive.

## Goals

- `pergent agent` reviews `base..head` in a given checkout with given skills and instructions, and produces a schema-versioned JSON result.
- The result holds everything a caller needs to post and act on the review without re-implementing pergent:
  - per-skill verdicts;
  - findings with fingerprints and short IDs;
  - summaries, checks and coverage;
  - failures with their exact causes;
  - the rendered comment, fitted to a size limit;
  - the hidden findings block.
- Three ways to deliver the result: a file, stdout, or HTTP POST.
- A failed review is still a result. Exit codes separate "review failed" (exit 0, with the result saying why) from "no result" (non-zero).

## Non-Goals

- **Cloning or fetching.** The caller provides a checkout that contains both commits. Caching repositories is pergent.sh's job.
- **Drafting files** (e.g. `AGENTS.md`). That is a separate mode in piece 3 of the roadmap.
- **Approval logic.** The caller decides from the verdicts.
- **Any change to CI mode's flags, behaviour or output.**

## Anti-goals

- Agent mode reading any instruction or skill file from the checkout.
- Agent mode holding or using a platform token.
- A JSON result that can be mistaken for a passing review when a skill failed.
- Logs on stdout mixing with the JSON when `--result -` is used.
- The result token showing up in process listings or logs.

---

## Decisions

### 1. Command and inputs

```
pergent agent \
  --repo-path /work/repo \
  --base <sha> --head <sha> \
  --skills-dir /in/skills \
  --instructions /in/instructions.md \
  --previous /in/previous.json \
  --max-comment-size 65536 \
  --result-url https://pergent.example/runs/<id>/result   # and/or --result <file|->
```

| Flag | Required | Meaning |
|---|---|---|
| `--repo-path` | yes | A checkout of the head commit. The agent's read, grep and glob tools work here. |
| `--base`, `--head` | yes | Commit SHAs. pergent computes the diff with `git diff <base>...<head>` in `--repo-path`, using the same fixed git options as local mode (`--no-color --no-ext-diff --src-prefix=a/ --dst-prefix=b/`). Both must exist in the checkout; otherwise it exits with "no result" (section 4). |
| `--skills-dir` | yes, unless `--skill` is given | A directory of skill `.md` files, loaded in file-name order. Each file uses the existing skill format (frontmatter `name`, `paths`, `exclude`, `cross_batch`; body). `--skill <preset-or-path>` (existing, repeatable) also works, and the two can be combined. With neither, it exits with "no result". |
| `--instructions` | no | One file passed to the agent as project instructions, replacing CI mode's automatic `AGENTS.md`/`CLAUDE.md` discovery. Without it, the agent gets no project instructions. |
| `--previous` | no | A previous **agent result** (this schema). Its findings are attached per skill, as `--previous-review` does in CI mode. A missing or unreadable file, or one with a different `schema_version`, logs a warning and the review runs as a first review. |
| `--max-comment-size` | no | Byte limit for the rendered markdown, passed to the existing `FormatReviewWithin`. `0` (the default) means no limit. |
| `--result`, `--result-url` | at least one | Delivery (section 3). |

**Unchanged and reused:** the existing review flags and env vars (`--timeout`, `--concurrency`, `--max-turns`, `--context-limit`, `OPENCODE_*`).

**Rejected in agent mode** (with a usage error): `--local`, `--platform`, `--previous-review` and `--base-branch`, which are CI-mode concepts.

### 2. Trusted inputs only

- `GenerateOpenCodeConfig` gains a way to take an explicit instruction path (or none) instead of calling `projectInstructions(repoPath)`. Agent mode passes `--instructions` or nothing, and never discovers instructions from `--repo-path`. CI mode keeps calling `projectInstructions` exactly as today.
- Skills come only from `--skills-dir` and `--skill`. Agent mode never scans the checkout for skills.
- The existing sandbox stays: the read-only agent, `OPENCODE_DISABLE_PROJECT_CONFIG=true`, and denied `*.env` reads.
- **Default skip rules can't be set by the MR.** In agent mode, `diff.NewClassifier` gets its rules from the **base** commit, not the checkout:
  - `.gitattributes` comes from `git show <base>:.gitattributes`, and is empty if it doesn't exist there;
  - a file counts as "generated" by its header only if the **base** version of that file already had the header.

  A new file, or a file the MR newly marks as generated, is reviewed. This takes a new classifier input that `review.Request` carries. CI mode keeps today's behaviour, because CI mode never approves.
- **`AGENTS.md` / `CLAUDE.md` inside the checkout** are never loaded as instructions: pergent doesn't pass them, and `OPENCODE_DISABLE_PROJECT_CONFIG` stops opencode from discovering them. The builder confirms the second part against opencode 1.19.4's `session/instruction.ts`. The model may still read such files with its tools as content under review; that is fine.
- **`PERGENT_RESULT_TOKEN`** is removed from the environment passed to opencode, and added to the redaction list.

### 3. Result delivery

| Option | Flag | Behaviour |
|---|---|---|
| File | `--result <path>` | Writes the JSON to `<path>` (written to a temp file and renamed into place). |
| Stdout | `--result -` | Writes the JSON to stdout. **Everything else goes to stderr**: progress, warnings, opencode's passthrough. Nothing else may write to stdout in agent mode. |
| HTTP | `--result-url <url>` | `POST`s the JSON with `Content-Type: application/json` and `Authorization: Bearer <token>`. The token comes from the env var `PERGENT_RESULT_TOKEN`, never a flag, and is never logged. A 2xx response is success. A 5xx, 429 or connection error is retried 3 times with exponential backoff (2s, 4s, 8s). Any other 4xx fails at once. A missing `PERGENT_RESULT_TOKEN` with `--result-url` is a usage error. |

- `--result` and `--result-url` can be combined. Both are attempted, and the exit code reflects any delivery that failed.
- pergent.sh's Jobs use `--result-url`.

### 4. Exit codes

| Code | Meaning |
|---|---|
| `0` | A result was produced and every requested delivery succeeded, **even if every skill failed**. |
| `2` | Usage error: a bad or conflicting flag, or a missing required input. No result. |
| `3` | No result could be produced: the checkout or a commit is missing, `git diff` failed, a skills directory is unreadable, or another setup failure. pergent tries to deliver a minimal error result first (`status: "error"`, section 5) if a delivery target is configured. |
| `4` | A result was produced but at least one delivery failed after retries. |

On a non-zero exit the caller marks the run as "failed: no result" (or "not delivered") and shows the logs.

### 5. Result schema, version 1

```json
{
  "schema_version": 1,
  "status": "completed",
  "pergent_version": "0.x.y",
  "base": "<sha>", "head": "<sha>",
  "started_at": "RFC3339", "finished_at": "RFC3339",
  "budget_seconds": 600,
  "skills": [
    {
      "name": "code-review",
      "verdict": "no_blocking_issues",
      "findings": [
        {"id": "a1b2c3", "fingerprint": "a1b2c3d4e5f6", "file": "x.go", "line": 3, "severity": "blocking",
         "title": "…", "explanation": "…", "suggested_fix": "…"}
      ],
      "summaries": [{"batch": 1, "text": "…"}],
      "checks": [{"batch": 1, "dropped": 0, "findings": 1,
                  "checks": [{"section": "…", "name": "…", "status": "pass", "detail": "…"}]}],
      "files": {
        "reviewed": ["x.go"],
        "skipped": [{"path": "go.sum", "rule": "lockfile"}],
        "not_reviewed": [{"path": "big.json", "cause": "…"}]
      },
      "failures": [{"files": ["a.go"], "message": "…", "cause": {"kind": "timeout", "message": "…"}}],
      "notes": ["…"],
      "batches": 1, "batches_finished": 1,
      "duration_seconds": 84.2,
      "tokens": {"input": 0, "output": 0}
    }
  ],
  "comment": {"markdown": "…", "findings_block": "<!-- pergent:findings … -->"}
}
```

- **`status`** is `completed` for any review that ran, including one where every skill failed, or `error` for the minimal error result (exit code 3). An error result carries `error: {message}` and no `skills` or `comment`.
- **`id`** is the first 6 characters of the fingerprint: short enough for `/pergent dismiss <id>`. When two findings **anywhere in the same result** share 6 characters, both get 8.
  - IDs only need to be unique within one result. The caller resolves an ID against the latest result to get the fingerprint, and stores dismissals by **fingerprint**, so a dismissal keeps applying on later pushes even if the short ID changes.
  - In agent mode the rendered comment shows each finding's ID, e.g. `` `a1b2c3` `` after its title. This takes a new `output` option that agent mode turns on; CI-mode output doesn't change.
- **`comment.markdown`** is the full comment, rendered by `FormatReviewWithin` with `--max-comment-size`. **`comment.findings_block`** is the hidden block on its own, so the caller can store findings without parsing markdown.
- **`tokens`** sums the `step_finish` usage of the skill's runs. A provider that reports no usage gives zeros.
- **Redaction** runs before the result leaves pergent, as for the comment today.
- **The schema is documented** in `docs/agent-result-schema.md`, with this example. Adding fields is compatible. Renaming or removing one requires `schema_version: 2`.

### 6. Logs

Progress lines are the same as CI mode, on stderr. Each line is prefixed with the skill and batch (`[code-review 2/3]`), because runs are parallel and the caller will show these logs on the MR's page.

---

## Constraints

- No new Go dependencies.
- CI mode's flags, output and behaviour don't change. Existing tests keep passing unchanged.
- Agent mode code lives in `cmd/agent.go` plus a small `internal/result` package (schema types, encoding, delivery). It reuses `review.Run` and `output.FormatReviewWithin`.
- Privacy: generic examples only. This is a public repository.

## Designer's calls (not discussed with the user)

- **Exit codes:** the numbers 2, 3 and 4, and the minimal `status: "error"` result.
- **HTTP delivery:** retry policy of 3 retries with 2s, 4s and 8s backoff; the token arrives as a Bearer header.
- **Finding IDs:** 6-character IDs, widening to 8 on collision.
- **`--previous` format:** it takes a previous agent result, not the hidden comment block.
- **CI-mode flags in agent mode** are rejected with a usage error.
- **Log lines** are prefixed with skill and batch, in agent mode only.
- **Default skip rules come from the base commit** in agent mode (section 2), and a header-marked file counts as generated only if its base version was too.
- **Finding IDs are shown in the agent-mode comment,** unique per result, with dismissals stored by fingerprint.
- **Code location:** `cmd/agent.go` and `internal/result`.

## Left to the builder

- **Internal structure:** how `cmd/main.go` dispatches the subcommand, and how flags are shared between the two modes.
- **The exact JSON field order** and Go type names.
- **Test fixtures and helpers.**
