# pergent as a Review Agent -- Design Document

**Date:** 2026-10-23
**Status:** Draft (decisions reviewed with the user)

---

## Overview

pergent today is a CLI that runs inside a CI job: it gathers the MR diff, runs `opencode run` once per skill, and posts one combined markdown comment.

The plan is to split the product in two:

- **pergent (this repo) becomes the review agent.** A container image that does one job: given a repo checkout and a change, review it with opencode and the configured skills, and produce a result.
- **pergent.sh (separate project, designed later)** is the management side: backend and web frontend, receives webhooks, decides which MRs to review, starts an agent container per review, posts results, handles approval, settings, history.

pergent.sh is only as good as the reviews, so **the agent's review quality and reliability are the product.** This document covers the agent only.

### Why now

Reviews can fail in ways that make people ignore pergent:

- **opencode's internal message posted as the review.** When opencode's context fills, it compacts the session and injects a synthetic message to the model ("Continue if you have next steps, or stop and ask for clarification if you are unsure how to proceed."; `packages/opencode/src/session/compaction.ts`, part marked `synthetic: true`). pergent's parser (`internal/runner/parser.go`) takes the last `text` event regardless of author, so a run that ends after compaction posts opencode's prompt as the review.
- **Big changes produce wrong reviews.** The whole diff goes to the model in one run, and pergent sets the model's context limit to `--max-tokens` (100k by default), far below real model windows.
- **Timeouts don't give a proper message.** A timeout posts whatever in-progress text came last (often "Let me check…") under a "truncated" note.

---

## Goals

- **Reviews are trustworthy.** Every skill run ends in exactly one of two states: a real review written by the model, or a failure that states the actual error.
- **Big MRs are reviewed in full**, by splitting into batches, within a fixed time budget.
- **Structured output.** Findings and a verdict as data, so the comment can show a clear verdict, findings can be tracked across pushes, and a future pergent.sh can act on them without parsing markdown.
- **Skills own their domain.** Each skill defines what counts as a blocking problem in its area.
- **CI mode keeps working.** pergent stays usable on its own as a job in `.gitlab-ci.yml` / GitHub Actions, as today.

## Non-Goals

- **pergent.sh itself.** Webhooks, queueing, repo caching, dashboard, per-repo settings storage, and the approval action are pergent.sh's job and get their own design.
- **Auto-approve from the agent.** The agent outputs a verdict; it never approves. Approving from a CI job is unsafe because MR authors can edit the job and use the approving token (in GitLab this would bypass "Prevent approval by author"). See `docs/research/2026-10-23-auto-approve-prior-art.md`.
- **A quality test set / eval harness.** Agreed as important, explicitly deferred by the user for now.
- **Learnings from feedback, inline line comments, MR summaries, interactive commands.** Possible later; not in this round.
- **Fine-tuning or per-repo model training.** Not needed; context comes from skills, `AGENTS.md`/`CLAUDE.md`, and previous findings.

## Anti-goals

These would make the work a failure even if it technically runs:

- Posting anything that looks like a review but isn't the model's finished answer (opencode's internal messages, partial in-progress text).
- A generic "review incomplete" with no cause.
- A skill reported as passing ("no blocking issues") when part of the MR was not reviewed.
- Small suggestions (naming, style) causing a "has blocking issues" verdict.

---

## Decisions

Every decision below was made by the user, either directly or by accepting a recommendation in review. Items marked **(builder's call)** are left open on purpose.

### 1. Run outcome: review or exact failure

Every skill run ends as one of:

- **Completed:** the model's own final answer, parsed into findings + verdict.
- **Failed:** a specific cause with the numbers pergent can observe. Never a generic "incomplete", never partial text shown as a review.

Failure causes and what each must say (example wording, not exact strings):

| Cause | Must include |
|---|---|
| Context overflow | the provider's error, tokens at the last step vs the context limit, step count. "Context full at 198,400 / 200,000 tokens after 22 steps." |
| Time budget exhausted | the budget, elapsed time, step count, last tool running and its target file if known. "Timed out after 10m0s at step 9 while reading `deploy/service.yaml`." |
| opencode error event (after retries) | error `name` and `data.message` verbatim. "APIError: 429 rate limit exceeded (after 3 retries)." |
| Step limit reached without answer | the limit. "Reached the 20-step limit without producing a review." |
| opencode exited non-zero | exit code + last few lines of opencode stderr |
| Exit 0 but no answer | that, plainly |
| Model answer not in the required format | that, plus a short excerpt of what came back |

Rules:

- **Never use synthetic parts** (or any non-assistant text) as the answer.
- **Redact secrets** before anything reaches the MR comment or the result file: values of `OPENCODE_API_KEY`, `GITHUB_TOKEN`, `GITLAB_TOKEN`, bearer tokens.
- **The same cause is logged to stderr**, so the CI job log has it.
- **Failures keep the CI job green** (exit 0); the error is reported in the comment and the log only. Configuration errors that stop pergent before any review starts (missing token, bad flag) still exit non-zero as today.

### 2. Context, compaction, and token limits

- **Auto-compaction is turned off** in the generated opencode config (`compaction.auto: false`, supported in opencode 1.19.4). A run that fills its context fails with the exact error (section 1) instead of compacting and continuing on a summary. Batching (section 5) keeps this rare; if it becomes common, the fix is smaller batches.
- **pergent stops setting the model's context window from `--max-tokens`.** opencode uses the model's real limits from its model registry.
- **`--max-tokens` / `PERGENT_MAX_TOKENS` are deprecated:** still accepted, print a deprecation warning, otherwise ignored. The time budget and step limit bound cost.
- **New optional `--context-limit` / `PERGENT_CONTEXT_LIMIT`**, used only when set, for custom providers (`OPENCODE_BASE_URL`) whose limits opencode doesn't know.
- **Retries:** transient provider errors (HTTP 429, 5xx, connection errors) are retried up to 3 times with exponential backoff while time remains in the budget. If all retries fail, the run fails with the last error verbatim. Retrying the whole opencode run is acceptable.

### 3. Findings and verdict

Each finding:

| Field | Notes |
|---|---|
| `file` | path relative to repo root |
| `line` | optional; new-side line number when it applies |
| `severity` | `blocking` or `suggestion` |
| `title` | one line |
| `explanation` | why it matters |
| `suggested_fix` | optional |

Two severities only (only real problems block approval):

- **blocking:** real problems, as defined by the skill (section 4).
- **suggestion:** naming, style, readability, nice-to-haves. Still reported, never affects the verdict.

Per-skill verdict: `no_blocking_issues`, `has_blocking_issues`, or `failed` (with cause). A future approver (pergent.sh) would approve only when every skill is `no_blocking_issues`; `failed` never counts as a pass.

### 4. Skills define what's blocking

- **Each skill defines what's blocking** in a "Blocking" section of its markdown, e.g. for a Kubernetes manifest skill: "a Service selector that matches no Deployment labels". Everything else the skill flags is a suggestion.
- **Default when a skill has no Blocking section:** bugs, security vulnerabilities, and broken configuration are blocking; everything else is a suggestion. Existing skills, including the embedded `code-review` preset (`internal/skill/presets/code-review.md`), keep working unchanged. Update the preset to have an explicit Blocking section and drop its format instructions.
- **pergent owns the output format.** pergent appends the structured-output instructions (finding schema and verdict) to every skill prompt. Skill authors write domain rules only.
- **How the model returns structured output is the builder's call.** `opencode run` in 1.19.4 has no structured-output flag (checked in `cli/cmd/run.ts`), so the likely route is: instruct a JSON block in the final answer, validate it, allow one repair attempt, then fail with the "not in required format" cause.

### 5. Big MRs: review everything in scope, in batches

- **Split by diff size in tokens, not file count.** Below a threshold, one run per skill as today. Threshold and token estimation are **the builder's call**; the threshold must leave room in the model's context for the agent to read surrounding files.
- **Group related files together**, by directory first. A single file larger than a batch becomes its own batch; if it alone cannot fit, it is reported as not reviewed with that reason, and the skill's verdict is `failed`.
- **Each batch is its own opencode run**, given only its part of the diff plus the full list of changed files in the MR. The agent can still read/grep any file in the repo, so cross-references resolve.
- **Merge** findings from all batches into one result per skill; remove duplicates (same file, line, and title; builder may refine).
- **Cross-batch pass: off by default**, can be turned on per skill (frontmatter). When on, one short run after the batches sees the full changed-file list and all batch findings, to catch conflicts between batches. Skipped with a stated reason if time is short. Revisit the default once a test set exists.
- **Partial failure:** other batches' findings are still reported. The failed batch is reported with its exact error and its file list, and the skill's verdict is `failed`.

### 5a. Which files a skill covers

"Review everything" means every file *in the skill's scope*:

- **Skill scope:** a skill may declare `paths` (include globs) and `exclude` (exclude globs) in its frontmatter, e.g. a Kubernetes manifest skill covers `**/manifests/**`. No `paths` means all files.
- **Default exclusions, for every skill:** lockfiles (`package-lock.json`, `go.sum`, `yarn.lock`, `pnpm-lock.yaml`, `Cargo.lock`, …), vendored dirs (`vendor/`, `node_modules/`, `third_party/`), generated files (`linguist-generated` in `.gitattributes`, or a `Code generated … DO NOT EDIT` header), binary files, and deleted files. A skill can opt files back in through `paths`.
- **Skipped is not "not reviewed".** Files out of scope or excluded by rule are listed as `skipped: <rule>` and do **not** make the verdict `failed`. Only in-scope files that could not be reviewed (too large, batch failed, budget ran out) make it `failed`.
- A skill with zero in-scope files gets `no_blocking_issues` with the note "no files in scope", and no opencode run happens.

### 6. Time budget: 10 minutes for the whole review

- The budget covers the **whole MR**: all skills, all batches, verification. Not per skill or per batch.
- **`--timeout` / `PERGENT_TIMEOUT` change meaning** from per-skill to whole-review, default 10m. This is a breaking change: call it out in the README and release notes.
- **Skills and batches run in parallel**, with a concurrency limit: default 4, configurable via `--concurrency` / `PERGENT_CONCURRENCY`.
- When the budget runs out, unfinished batches are reported by name with the exact cause ("not reviewed: the 10-minute review budget ran out; 3 of 8 batches finished") and the skill's verdict is `failed`.

### 7. Verification pass on blocking findings

- **On by default.** After a skill's batches finish, a short run re-checks each **blocking** finding against the code and drops ones that aren't real. Suggestions are not re-checked.
- It sees the findings and the referenced files, not the whole diff, so it is cheap.
- If the time budget is nearly used, it is skipped and the result says so. Unverified blocking findings are still reported and still count.

### 8. Scope: CI mode only

This design covers pergent as it runs today: a job in `.gitlab-ci.yml` / GitHub Actions that reviews the MR and posts one comment. The goal of this round is to make that stable and good. Agent mode (below, under "Later") is **not** built now.

Comment layout, per skill:

- a verdict line at the top: ✅ no blocking issues / ❌ N blocking / ⚠️ failed: <cause>;
- blocking findings listed in full;
- suggestions in a collapsed `<details>` block;
- skipped files in a collapsed block, each with the rule that skipped it.

The "Files changed" list in the comment header moves into a collapsed block too.

### 9. Previous review context

`--previous-review` exists today and passes the previous comment's markdown section for each skill. With structured findings:

- The posted comment embeds the findings as JSON in a hidden HTML comment, each with a stable fingerprint (hash of skill name, file, normalised title); the next run reads them back. This replaces passing back the old markdown, and is how pergent knows which findings are still open after a new push, with no storage.
- The prompt stays: stay consistent, focus on what changed, drop findings the new changes resolve, and reuse fingerprints for findings that persist.

---

## Constraints

- Go, same repo, same module. Reuse the existing packages (`skill`, `config`, `runner`, `output`, `platform`). Move the orchestration now inline in `cmd/main.go` into its own package, so the review flow can be tested without the CLI and reused by agent mode later.
- opencode stays the engine, pinned at 1.19.4 (merged in gopaytech/pergent#3). OpenCode 2.x is out of scope.
- The read-only sandbox from gopaytech/pergent#3 stays: dedicated `pergent` agent, edit/bash/task/skill/webfetch/websearch denied, `*.env` reads denied, `OPENCODE_DISABLE_PROJECT_CONFIG=true`.
- Existing flags and env vars keep working, except: `--timeout` becomes whole-review; `--max-tokens` is deprecated.
- pergent.sh is not being built yet, and nothing here depends on it.

## Phasing

- **Phase 1, open as gopaytech/pergent#4:** sections 1 and 2 (exact failures, no synthetic text, compaction off, retries, `--max-tokens` deprecation, `--context-limit`). Ships on its own because it fixes reviews in production today. The plan builds on it.
- Later phases are for the implementation plan to order.

---

## Later, with pergent.sh (not built now)

Recorded so the reasoning isn't lost. Decided in principle; to be designed properly together with pergent.sh.


- **Agent mode: `pergent agent --result <path> …`.** pergent reviews, writes the result as JSON to the given file, logs to stderr, prints nothing to stdout, and **never talks to GitHub/GitLab**. No platform token is given to the container; the caller posts and approves. This keeps platform credentials out of the container that runs on untrusted MR code.
- **Agent mode only uses trusted inputs.** Skills and project instructions (`AGENTS.md` / `CLAUDE.md`) come only from paths the caller passes in (typically from the target branch), never from the MR head checkout; otherwise an MR author could edit `AGENTS.md` or a repo skill to say "nothing is blocking" and get their own MR approved. Agent mode does not auto-load instructions from the checkout. CI mode keeps reading the repo as today, since CI mode never approves.
- Agent mode inputs (exact flags are **the builder's call**): repo checkout path at the MR head commit, base and head SHAs (pergent computes the diff with git) or a diff file, skills directory/paths, instructions file, time budget, optional previous findings JSON.
- The JSON result is versioned (`"schema_version": 1`) and documented in the repo. Version 1 includes, beyond findings and verdicts:
  - the **base and head SHAs** that were reviewed;
  - **per-skill coverage**: files reviewed, skipped (with rule), not reviewed (with cause);
  - a **stable fingerprint per finding** (hash of skill name, file, normalised title), to track findings as new / still open / resolved across runs;
  - **pre-rendered markdown** of the whole result (the text CI mode would post);
  - timing and token usage per skill and batch.

- Previous findings would be passed in as an input file.

---

## Left to the builder

- Token estimation method and batch threshold.
- How the JSON answer is extracted and repaired (section 4).
- Exact wording of failure messages, within the table in section 1.
- Package layout for the shared review flow.
