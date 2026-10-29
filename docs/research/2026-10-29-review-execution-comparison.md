# How hosted AI code-review services execute a review, compared with pergent agent mode

Research date: 2026-10-29.

The question: behind the webhook, how do commercial and open-source AI merge-request reviewers
actually run a review? How do they get the code, where does the model run, which credentials
sit next to the untrusted code, where do rules come from, and how are diffs, failures and
results handled? The second half compares the answers with the decisions in
[pergent agent mode](../superpowers/specs/2026-10-29-pergent-agent-mode-design.md).

This note covers the **execution pipeline**. The earlier survey,
[2026-10-29-hosted-review-product-surfaces.md](2026-10-29-hosted-review-product-surfaces.md),
covers dashboards, settings and analytics, and those are not repeated here.

**Sources and method.**
- Primary sources only: vendor docs, trust and security pages, vendor engineering blogs and
  changelogs, self-hosted install guides, Helm charts and compose files, and source code.
- Two third-party primary sources are used, and labelled where they appear:
  - Kudelski Security's own disclosure of a CodeRabbit vulnerability, alongside CodeRabbit's
    response to it.
  - The Google Cloud and Anthropic customer case studies, in which the vendors' own engineers
    describe their architecture.
- Pages were read through a fetch tool that summarises. Source files were read on the default
  branch on 2026-10-29, so citations are file paths, not line anchors.
- Nothing was signed up for, logged into, cloned or downloaded.

**Labels.**
- **(unverified)**: seen only in a search snippet, or the docs and the code disagree.
- **(not documented)**: looked for in the relevant docs and not found.
- **(inference)**: my reading, not a vendor statement.

Products covered:
- CodeRabbit (SaaS and self-hosted)
- Greptile (SaaS and the self-hosted `akupara` distribution)
- Qodo Merge and the open-source PR-Agent (source read)
- GitHub Copilot code review
- Cursor Bugbot
- Graphite Agent
- CodeAnt AI
- Bito AI Code Review Agent (including its self-hosted agent)
- GitLab Duo Code Review (source read, since GitLab is open core)
- Ellipsis
- Sourcery
- Kodus (source read)

None were skipped. Graphite, CodeAnt and Sourcery document very little about execution, and their
sections say so.

---

## (a) Summary

**1. Three ways to get the code.**
- **API only.** PR-Agent fetches the diff and file contents through the platform API and never
  clones for a review. The LLM gets a prompt, not a workspace. This is also how GitLab Duo builds
  its review context.
- **Clone into a sandbox.** CodeRabbit (Cloud Run microVM plus a jail), Bito (a container), Copilot
  (a GitHub Actions runner), Ellipsis (a per-session cloud environment), Kodus (an E2B VM per PR)
  and GitLab Duo (a CI job that checks out the source branch).
- **Long-lived worker with a shared clone volume.** Greptile self-hosted keeps a 1 TiB shared
  workdir volume and runs a privileged worker.

Kodus is the closest to pergent: `git init`, then a depth-1 fetch of the PR head ref and the base
branch.

**2. Per-review isolation is now the norm for agentic reviewers, but its strength varies a lot.**
- **Strongest:** CodeRabbit runs a gen2 microVM plus Jailkit and cgroups. Cursor runs its Autofix
  cloud agents in Firecracker microVMs. Kodus uses E2B VMs.
- **Weakest:**
  - Greptile's self-hosted worker runs `privileged` with `SYS_ADMIN` so its inner sandbox works.
  - GitLab Duo's sandbox needs a privileged runner, and it silently runs unsandboxed when the
    sandbox cannot start.
  - PR-Agent and Bito's server mode run every review in one long-lived process or container.
- **Network:** Copilot's firewall is on by default, but it covers only commands from the agent's
  Bash tool. Greptile's TREX has off, trusted and custom egress modes. Kodus uses an
  SNI-allowlisting relay for its custom-agent CLIs. Most vendors document no egress policy for the
  review agent at all.

**3. Few products keep write credentials away from the LLM.**
- **Products that do:**
  - Ellipsis gives reviewer sessions read-only GitHub tokens, and the platform posts the review.
  - GitHub Agentic Workflows, a sibling product of Copilot, runs agents with read-only tokens and
    writes only through declared "safe outputs" in a separate job.
  - GitLab keeps model keys in its AI Gateway, never on the runner.
- **Products that don't:**
  - PR-Agent, Kodus's worker, Greptile's worker and Bito's container all hold write-capable
    tokens in the same process or pod that drives the model over PR content.
  - GitLab's CI job holds a composite token that can post comments. It is bounded to the
    Developer role, intersected with the triggering user's access, and expires after 1 hour.
- **The incident that proves the point:** in January 2025 a PR's `.rubocop.yml` ran attacker code
  inside CodeRabbit. That process had the GitHub App private key and the LLM keys in its
  environment.

**4. The trust boundary for configuration is split.**
- **Base or default branch (a PR cannot change its own rules):**
  - Kodus: skills, knowledge, repo instructions and config.
  - PR-Agent: `.pr_agent.toml`.
  - GitLab Duo: review instructions and exclusion rules.
  - Ellipsis: all review config.
  - Cursor Bugbot: `bugbot.yaml`.
  - Greptile and CodeAnt: approval policy only.
- **PR head (the author controls the reviewer's rules for their own PR):**
  - Copilot, which since July 2026 reads instructions and `AGENTS.md` from the head branch.
  - CodeRabbit, except for fork PRs, which use the target branch's config.
  - Greptile's review settings.
  - Bito's `.bito.yaml`.
- **Structural prompt-injection defenses** are documented only by PR-Agent, Kodus and GitLab:
  - untrusted-content labelling;
  - escaping of untrusted text placed inside tags;
  - stripping of hidden Unicode and HTML comments;
  - for Kodus, checking that a finding which cites a rule quotes it verbatim.

**5. Diffs.**
- Where documented, the diff base is the platform's merge-base diff: PR-Agent uses
  `compare().merge_base_commit` and `diff_refs`, and GitLab uses the MR diffs API.
- **Incremental review** is almost universal: CodeRabbit, Bugbot, Bito, Ellipsis, GitLab, Kodus,
  Sourcery, and PR-Agent's `-i`.
- **Rebase-aware incremental review** is rare:
  - Kodus computes a line-key "rebase interdiff" and skips a rebase with no real changes.
  - GitLab compares `patch_id`, falls back to a full review when the base moved, and intersects
    compare results with the MR's own diff.

**6. Lifecycle.**
- **Superseding on a new push:**
  - GitLab stops the in-flight review and starts a new one.
  - Cursor ends a cancelled run as a `neutral` check.
  - CodeRabbit shows "Review cancelled" and aborts on close.
  - PR-Agent and Kodus never cancel a running review; they drop or defer the new one.
- **Failures are usually non-blocking:** CodeRabbit's failed check is neutral or success by
  default, Bugbot's errors are `neutral`, and Sourcery says a rate limit never blocks a merge.
- **Typed failure reasons:** GitLab has DCR error codes, Ellipsis has session error reasons, and
  Kodus has classified provider errors plus fidelity warnings.

**7. Output.**
- Everyone posts comments and most post a check run.
- Machine-readable results exist in a few places:
  - Ellipsis: a typed session `output.result` and signed outbound webhooks.
  - GitLab v4: schema-validated findings, posted by a deterministic step.
  - Greptile: CLI exit codes and MCP review states.
  - CodeAnt: CI `results.json`, SARIF and exit codes.
  - PR-Agent: structured review JSON with coverage.
- **Nobody publishes a versioned result schema for a single review run** the way pergent agent
  mode does.

**8. Limits.**
- **File caps:** CodeRabbit 150 or 300 files; Kodus 2,000; PR-Agent full content for only 50
  files.
- **Size caps:**
  - Graphite: 200k characters.
  - Cursor: 100k lines or 3M characters, skipped with a neutral check and no charge.
  - GitLab: 1 MiB of context, truncated to 800 KiB, with an honest "partial review" notice.
  - Sourcery: per-PR diff characters plus a rolling 7-day budget.
- **Money:** Ellipsis has dollar budgets per session.

**Bottom line for pergent (inference).**
- **Stricter than nearly all of the field:**
  - No platform credential in the review container.
  - Instructions and skills only from the caller.
  - Skip rules from the base commit.
  - A versioned JSON result with error results.
  - An explicit SIGTERM path to a "cancelled" result.
- **Where the field is ahead:**
  - Network egress control and stronger runtimes (microVMs).
  - Keeping the LLM provider key out of the sandbox (behind a proxy or gateway).
  - Rebase-aware and no-op push handling.
  - Re-checking the live head before posting or approving.
- **Where the design has gaps:**
  - The checkout's `.git/config` and `--reference` alternates could carry the git token or the
    cache into the review container.
  - `activeDeadlineSeconds` also counts clone time.

---

## (b) Comparison table

Cells are short summaries. Every claim is cited in the product's section under (c). "n/d" means not
documented.

### Dimensions 1 to 4

| Product | 1. Code access and caching | 2. Execution environment and isolation | 3. Credentials near the LLM | 4. Config and instruction trust |
|---|---|---|---|---|
| **CodeRabbit** | Full clone per review into an in-memory volume. Optional encrypted code and dependency cache. Vector index. Dependency graph rebuilt each review | Cloud Run gen2 microVM plus Jailkit and cgroups. Runs 50+ linters and LLM-written shell and Python. Egress n/d. A Jan 2025 RCE came from a linter running outside the sandbox | App has write on contents, PRs and checks. In 2025 the tool process held the App private key and LLM keys. Current separation n/d | `.coderabbit.yaml` and guideline files from the PR head. Fork PRs use the target branch config. Linter configs used "as-is" |
| **Greptile** | Worker clones into a shared persistent volume (1 TiB RWX PVC). Chunker embeds code into pgvector. Code graph built at onboarding | Long-lived worker pod, 2 reviews at a time, `privileged` with `SYS_ADMIN` and host cgroup mount for its inner sandbox. No NetworkPolicy shipped. TREX (testing) has egress modes | Worker gets the whole shared Secret by default (App private key, LLM keys). Only the LLM proxy opts out | `greptile.json` and `.greptile/` from the PR source branch. `autoApprove` from base, and the stricter value wins |
| **Qodo / PR-Agent** | API only: PR files API, contents pinned to head SHA and merge base. Full content for at most 50 files. `AGENTS.md` from the default branch | In-process background task in the webhook server. No sandbox, and the LLM runs nothing | Same process holds the write token, the LLM key and the prompt. Log redaction of tokens | `.pr_agent.toml` from the default or configured branch, never the PR. A key allowlist (`config_security.py`) keeps URLs, keys, timeouts and writes host-only |
| **Copilot code review** | Agentic context gathering on an Actions runner (clones the repo). MCP tools, read-only | GitHub Actions: hosted, larger, or ARC self-hosted runners. Firewall on by default (Bash-tool processes only, not on self-hosted). CodeQL, ESLint and PMD | Copilot gets "its own token" (scope n/d). Model service is GitHub's; no customer LLM key | Instructions, `AGENTS.md` and skills from the **head** branch since 2026-07-17 |
| **Cursor Bugbot** | Rust git integration that minimises fetches; on-demand tool calls. Clone vs API n/d. Never reads `.env`, `*.pem`, `*.tfvars` | Review environment n/d. Bugbot does not run code. Autofix uses Firecracker cloud-agent VMs | App has PR write and checks. Separation n/d | `bugbot.yaml` from the **base** branch. `BUGBOT.md` branch n/d |
| **Graphite Agent** | Opt-in code index, short-lived retention. Otherwise n/d | n/d | App has write on contents, PRs, checks and workflows. "Short-lived tokens" | UI rules plus repo docs by glob. Honours `linguist-generated`. Branch n/d |
| **CodeAnt AI** | n/d for PR review. CLI agent uses Read/Glob/Grep | n/d. SAST and SCA engines; where they run is n/d | App approves; separation n/d. GitLab token has `api` and `write_repository` | `.codeant/` config branch n/d. `approval.json` from the **base** branch, and a PR touching it goes to a human |
| **Bito** | Container clones the repo. Symbol index, AST and embeddings | Docker: `--rm` container per review (CLI mode) or one long-lived server. Runs fbinfer, mypy, ruff, linters. No hardening flags | Same container holds a classic PAT (repo scope) and runs analysers on PR code. LLM calls go through Bito's cloud | `.bito.yaml` from the PR **source** branch |
| **GitLab Duo Code Review** | CI job checks out the source branch (`GIT_DEPTH: 0`, blobless). Review context built mostly from the API. Original files read at the target branch tip | CI job on a `gitlab--duo` runner. Optional SRT/bubblewrap or Landlock sandbox (needs privileged), which falls back to unsandboxed. Domain allowlist | Composite OAuth token (user ∩ service account), 1-hour expiry, can post comments, held in the job. LLM keys stay in the AI Gateway | Instructions and exclusion rules from the **default** branch. Does not use `AGENTS.md` |
| **Ellipsis** | Repo checked out in a per-session cloud environment at the PR commit. Setup layers cached by inputs | Per-session cloud environment ("basic sandbox"). Technology and egress n/d. Agent harness (Claude Code or Codex) | Reviewer tokens **read-only**, and the platform posts. Model-key secret names are rejected | `.ellipsis/code-review.yaml` from the **default** branch. An invalid file keeps the last valid version |
| **Sourcery** | Reads the diff plus related code. Mechanism n/d | n/d. Secret, SAST, IaC and dependency scanners | App has broad write (code, workflows, checks) | Dashboard settings only. No PR-controlled review config documented |
| **Kodus (OSS)** | API for diff and contents (contents read by **branch name**). Plus `git init` and depth-1 fetch of the PR ref and base into a sandbox. AST graph of the default branch | E2B VM per PR (paused and reused), or a local temp dir with a command allowlist. Privilege drop. `checkTypes` runs project toolchains | Sandbox gets the clone token through env-only git config. The worker holds the write token and drives the LLM | Skills, knowledge, instructions and config from the **base** or default branch via git objects. Refuses symlinks, caps size, labels content untrusted |
| **pergent agent mode** | Job init container: `--reference` cache clone or `git init` plus depth-1 fetch of base and head | One Kubernetes Job per review. Read-only opencode agent (read, grep, glob, list), runs no repo code | Git token only in the init container. Review container has the LLM key and a single-use result token | Instructions and skills only from the caller. `.gitattributes` and generated headers from the base commit |

### Dimensions 5 to 8

| Product | 5. Diff and incremental | 6. Lifecycle (cancel, timeout, retry, failure UX) | 7. Output contract | 8. Limits |
|---|---|---|---|---|
| **CodeRabbit** | Incremental on commits since the last review. Force-push and rebase each trigger a review. Merge-base n/d | `abort_on_close`. "Review cancelled" when superseded. Failed check is neutral by default (`fail_commit_status`). Retries n/d | Comments plus check run. Request-changes or approve with guards. REST API for metrics, not reviews | 150 or 300 files. Per-developer hourly review limits |
| **Greptile** | Events `open`, `push`, `rebase`. Delta scope n/d | Emoji status; "tag to retry". MCP states up to `FAILED` and `SKIPPED`. CLI exit codes 0/3/4/5 | Comments, status check, confidence score, auto-approve. MCP API | Credit tiers. Size caps n/d |
| **Qodo / PR-Agent** | Merge-base contents (GitHub), `diff_refs` (GitLab). `-i` incremental anchored on its last comment by committer date | One running plus one waiting per PR; drops extra runs, never cancels. 120 s per LLM call. Tenacity retries plus fallback models | Persistent comment, inline comments, optional check run. YAML findings and JSON coverage | 32k model tokens default. 50 full-content files. 3 chunked calls |
| **Copilot code review** | n/d. May repeat resolved comments | Setup job up to 59 minutes. Falls back to a non-agentic review if Actions fails. Cancellation n/d | "Comment" review. Opt-in approvals (GA 2026-10-28), dismissed on push | 300-file and 20k-line cap removed. AI credits plus Actions minutes |
| **Cursor Bugbot** | Incremental since the last Bugbot review by default | Cancelled, timeout and internal error give a `neutral` check. Never `skipped` | Comments, `Cursor Bugbot` check, admin API with per-review results | Over 100k lines or 3M characters: skipped with a neutral check. Files over 2 MB skipped |
| **Graphite Agent** | "New and updated" PRs. Otherwise n/d | Running, Completed, Not running. Otherwise n/d | Inline comments. No API or webhooks for reviews | 200k-character PR limit |
| **CodeAnt AI** | n/d. Auto-approve pins the head SHA | n/d. Approval fails closed | Comments, quality-gate status checks, App approvals. CI writes `results.json` and SARIF | CLI and scan caps only |
| **Bito** | Incremental per push. `/review full` | `/abort`, `/pause`. Timeouts and retries n/d | Comments or request-changes. Undocumented `output_path` | n/d |
| **GitLab Duo Code Review** | MR diff (merge base). Incremental by `last_reviewed_head_sha`, `patch_id` and compare, with full-review fallback on rebase or force-push | Push stops the in-flight review and starts a new one. Typed DCR error codes. API client retries 4 times (3/9/27 s) | Inline comments plus summary. v4: schema-validated findings posted deterministically. Beta approve (does not count) | 1 MiB context. Flat price per review |
| **Ellipsis** | Each reviewer sees only commits it has not reviewed | Non-head commits start nothing. Session error reasons. A failed reviewer re-covers its commits next push | Review with blocking flag, `on_pass`/`on_fail` actions. Session API with typed output. Signed webhooks | 64 comments. $250 session budget |
| **Sourcery** | Re-review after each commit (one tenth the cost, at most 5) | Check states success, failure, skipped. A rate limit never blocks a merge | Summary, guide, inline comments, check, REST API | Diff-character caps per PR and per 7 days |
| **Kodus (OSS)** | `lastAnalyzedCommit` from the last **successful** run. Rebase interdiff. Full re-run after a partial error | Per-PR lock: automatic runs dropped, commands deferred. RabbitMQ retries plus DLQ. Context-overflow retry. Watchdog finalises orphaned checks | Comments, check run (NEUTRAL on partial error), guarded approve and request-changes | 2,000 files. Tool output caps |
| **pergent agent mode** | `git diff <merge-base> <head>`. Full review each time, with previous findings attached | Deadline = `--timeout` + 2 minutes. SIGTERM gives a cancelled result. Transient provider retries. Error results | Versioned JSON (file, stdout or HTTP POST). Exit codes 0/2/3/4 | 10 MB result cap. Token-budgeted batches |

---

## (c) Per-product details

### CodeRabbit

**1. Code access.**
- Each review clones the full repository for isolated analysis. Clone depth is not documented.
  [architecture](https://docs.coderabbit.ai/overview/architecture.md)
- Google Cloud's case study, written with CodeRabbit, describes the execution service:
  - it clones the repo and installs dependencies;
  - it keeps the repo and artefacts on an in-memory volume.
  - [Google Cloud blog](https://cloud.google.com/blog/products/ai-machine-learning/how-coderabbit-built-its-ai-code-review-agent-with-google-cloud-run)
- The code dependency graph is regenerated on every review so new dependencies aren't missed.
  [CodeRabbit blog](https://www.coderabbit.ai/blog/context-engineering-ai-code-reviews)
- Stored data:
  - optional caching of encrypted code and dependency archives;
  - a code index of vector representations;
  - `reviews.disable_cache` (default `false`) turns caching off.
  - [FAQ](https://docs.coderabbit.ai/faq.md), [configuration](https://docs.coderabbit.ai/reference/configuration.md)
- Multi-repo analysis links related repos.
  [knowledge base](https://docs.coderabbit.ai/knowledge-base/index.md)
- Index freshness and monorepo handling: **(not documented)**.

**2. Execution environment.**
- **Queueing:** a thin Cloud Run service receives webhooks and enqueues Cloud Tasks. A separate
  Cloud Run execution service runs the reviews. [Google Cloud blog](https://cloud.google.com/blog/products/ai-machine-learning/how-coderabbit-built-its-ai-code-review-agent-with-google-cloud-run)
  - Request timeout 3,600 s.
  - 8 concurrent requests per instance, on 8 vCPU and 32 GiB.
  - Reviews often take 10 to 20 minutes.
- **Isolation:** two layers, the gen2 microVM plus Jailkit-isolated processes with cgroups. The
  service identity has minimal IAM. (Same source.)
- **What runs inside:** 50+ analysers and linters, LLM-generated shell (`cat`, `grep`,
  `ast-grep`) and Python. Verification scripts check comments before they are posted.
  [architecture](https://docs.coderabbit.ai/overview/architecture.md),
  [CodeRabbit blog](https://www.coderabbit.ai/blog/context-engineering-ai-code-reviews)
- Linters use the repo's own config file when one exists. [tools](https://docs.coderabbit.ai/tools/index.md)
- **January 2025 incident** (Kudelski Security's disclosure):
  - A PR's `.rubocop.yml` loaded attacker Ruby code.
  - RuboCop was running outside the sandbox.
  - Its environment held the GitHub App private key, LLM keys and database credentials, and it had
    outbound internet.
  - [Kudelski](https://kudelskisecurity.com/research/how-we-exploited-coderabbit-from-a-simple-pr-to-rce-and-write-access-on-1m-repositories)
- **CodeRabbit's response:** RuboCop was the only service outside the sandbox. It added deploy-time
  enforcement that blocks services from running outside the sandbox, and rotated credentials.
  [CodeRabbit response](https://coderabbit.ai/blog/our-response-to-the-january-2025-kudelski-security-vulnerability-disclosure-action-and-continuous-improvement)
- Sandbox egress policy: **(not documented)**.
- A per-review environment disposed after posting is stated on the Trust Center, which could not
  be fetched directly. **(unverified)**
- **Read-only vs write:** reviews are read-only. Separate commands such as fix, autofix and
  docstrings commit to the PR branch. [review commands](https://docs.coderabbit.ai/reference/review-commands.md)

**3. Credentials.**
- **GitHub App:** read and write on checks, contents, commit statuses, issues, PRs and workflows.
  [github-com](https://docs.coderabbit.ai/platforms/github-com.md)
- **Self-hosted:** a single container with a dedicated service account or app. Only LLM traffic
  leaves. Setup details are given at Enterprise onboarding.
  [self-hosted](https://docs.coderabbit.ai/self-hosted/overview.md)
- **Reverse tunnel:** an optional outbound-only tunnel with a per-tenant bearer token.
  [reverse tunnel](https://docs.coderabbit.ai/self-hosted/coderabbit-reverse-tunnel.md)
- Whether tools in the sandbox see write credentials today: **(not documented)**.

**4. Trust boundary.**
- `.coderabbit.yaml` is read from the feature branch under review.
  [YAML config](https://docs.coderabbit.ai/getting-started/yaml-configuration.md)
- **Guideline files** are auto-detected: `AGENTS.md`, `CLAUDE.md`, `.cursorrules`,
  `.github/copilot-instructions.md` and others.
  [code guidelines](https://docs.coderabbit.ai/knowledge-base/code-guidelines.md)
  - Cross-repo guideline sources come from the source repo's default branch.
  - Fork PRs use the target branch's configuration.
  - A fork PR's description may set only `reviews.review_details`.
- **Before approving,** CodeRabbit checks that the head commit hasn't changed.
  [automatic approvals](https://docs.coderabbit.ai/pr-reviews/automatic-approvals.md)
- Prompt-injection defenses for reviews: **(not documented)**.

**5. Diff.**
- The first review covers the whole PR. Later reviews are incremental, on commits since the last
  review. [auto-review](https://docs.coderabbit.ai/configuration/auto-review.md)
- Force-push, rebase and web edits each trigger a review. [FAQ](https://docs.coderabbit.ai/faq.md)
- Merge-base vs target tip: **(not documented)**.

**6. Lifecycle.**
- **Triggers:** open and push. Drafts are off by default. Filters for base branch, labels, title
  keywords and users. Auto-pause after 5 reviewed commits.
  [auto-review](https://docs.coderabbit.ai/configuration/auto-review.md)
- **Cancellation:** `abort_on_close` stops a review when the PR is closed or merged.
  [configuration](https://docs.coderabbit.ai/reference/configuration.md)
- **Superseding:** pushes that come faster than reviews finish supersede earlier ones.
  [FAQ](https://docs.coderabbit.ai/faq.md)
- **Check states:** queued, passed, skipped/paused, rate-limited, "Review failed", cancelled. A
  failure is neutral by default; `fail_commit_status: true` makes it fail.
  [status check](https://docs.coderabbit.ai/pr-reviews/status-check.md)
- LLM retries: **(not documented)**.

**7. Output.**
- A walkthrough comment, inline comments, and a check run (commit status as fallback).
  [status check](https://docs.coderabbit.ai/pr-reviews/status-check.md)
- **Automatic approvals** are guarded by: latest commit reviewed, threads resolved, pre-merge
  checks passing, and a risk limit.
  [automatic approvals](https://docs.coderabbit.ai/pr-reviews/automatic-approvals.md)
- **API:** covers metrics, learnings, findings, seats and audit logs. Triggering a review and
  reading structured review results: **(not documented)**.
  [API](https://docs.coderabbit.ai/api/index.md)

**8. Limits.**
- Files per review after path filters: 150 or 300 depending on plan.
  [plans](https://docs.coderabbit.ai/management/plans.md)
- Per-developer hourly rate limits. Over the limit, a passing "rate limited" check is posted.
  [rate limits](https://docs.coderabbit.ai/management/rate-limits.md)

### Greptile

**1. Code access.**
- The worker clones repos, runs the review sandbox and posts comments. The chunker fetches,
  chunks and embeds code.
  [akupara architecture](https://raw.githubusercontent.com/greptileai/akupara/main/docs/reference/architecture.md)
- **The clone lives on a persistent shared volume:**
  - Compose: `cloned_repositories:/mnt/efs`.
  - Helm: a 1 TiB `ReadWriteMany` PVC.
  - [compose](https://raw.githubusercontent.com/greptileai/akupara/main/deploy/docker-compose/docker-compose.yaml),
    [values.yaml](https://raw.githubusercontent.com/greptileai/akupara/main/deploy/kubernetes/charts/greptile/values.yaml)
- **Index:** a repo graph of files, functions and classes with call and import edges, built at
  sign-up and queried per file reviewed. Embeddings are stored in Postgres with pgvector.
  [graph context](https://www.greptile.com/docs/how-greptile-works/graph-based-codebase-context.md),
  [system architecture](https://www.greptile.com/docs/self-hosting/system-architecture.md)
- **Agent:** built on the Claude Agent SDK, with sub-agents and hooks that force every changed
  file to be examined. [Anthropic case study](https://claude.com/customers/greptile)
- Index freshness and branch: **(not documented)**.

**2. Execution environment.**
- **The review sandbox runs inside the long-lived worker.** The worker is `privileged`, has
  `SYS_ADMIN`, and mounts the host `/sys/fs/cgroup` read-write so that sandboxing works. Sandbox
  technology: **(not documented)**.
  [architecture](https://raw.githubusercontent.com/greptileai/akupara/main/docs/reference/architecture.md),
  [troubleshooting](https://raw.githubusercontent.com/greptileai/akupara/main/docs/operations/troubleshooting.md)
- **Capacity:** each worker pod handles 2 concurrent reviews at 2 CPU and 4 GiB. Extra work waits
  in Hatchet. [scaling](https://raw.githubusercontent.com/greptileai/akupara/main/docs/operations/scaling.md)
- **Network:** no NetworkPolicy is shipped.
  [networking](https://raw.githubusercontent.com/greptileai/akupara/main/docs/configuration/networking.md)
- **TREX** (a separate runtime-testing agent) runs from a snapshot. Env vars reach only the setup
  script. Network modes are off, Trusted or Custom allowlist, with separate build and review
  policies. [TREX environments](https://www.greptile.com/docs/trex/environments.md)

**3. Credentials.**
- **Self-hosted GitHub App:** contents read, PRs, checks, statuses and issues read/write.
  [App creation](https://raw.githubusercontent.com/greptileai/akupara/main/docs/configuration/github_app_creation.md)
- **Shared Secret (inference from the chart):**
  - Every component gets the shared Secret through `envFrom` unless `useSharedSecret: false`.
  - Only the LLM proxy sets that.
  - So the privileged worker holds `GITHUB_PRIVATE_KEY` and the LLM keys.
  - [deployment template](https://raw.githubusercontent.com/greptileai/akupara/main/deploy/kubernetes/charts/greptile/templates/deployment-components.yaml)
- **LLM proxy:** LLM calls go through a LiteLLM proxy.
  [compose](https://raw.githubusercontent.com/greptileai/akupara/main/deploy/docker-compose/docker-compose.yaml)

**4. Trust boundary.**
- Settings are read from the PR's source branch.
  [greptile.json reference](https://www.greptile.com/docs/code-review/greptile-json-reference.md)
- **Exception:** `autoApprove` comes from the base branch, so a PR that edits `.greptile` cannot
  loosen it. Where file and dashboard disagree, the stricter value wins.
  [auto-approve](https://www.greptile.com/docs/code-review/auto-approve-prs.md)
- A self-hosted release fixed reviews to respect base-branch approval settings, and withholds
  approval if they can't be loaded.
  [release notes](https://raw.githubusercontent.com/greptileai/akupara/main/RELEASE_NOTES_202610.5.md)
- Prompt-injection defenses: **(not documented)**.

**5. Diff.**
- `autoReview` events: `open`, `push` (adds commits) and `rebase` (rewrites history).
  [config reference](https://www.greptile.com/docs/code-review/greptile-config-reference.md)
- Delta scope and merge-base: **(not documented)**.

**6. Lifecycle.**
- **Status reactions:** 👀 working, 👍 done, 😕 failed, with "tag to retry".
  [first review](https://www.greptile.com/docs/code-review/first-pr-review.md)
- **States:** `PENDING` through `COMPLETED`, `FAILED`, `SKIPPED`.
  [MCP tools](https://www.greptile.com/docs/mcp/tools.md)
- **CLI:** exit codes 0 complete, 3 running, 4 failed, 5 cancelled, plus `--resume`.
  [CLI](https://www.greptile.com/docs/code-review/greptile-cli.md)
- **Approval:** a push after the review started blocks approval, and a push after approval
  dismisses it. [auto-approve](https://www.greptile.com/docs/code-review/auto-approve-prs.md)
- Timeouts and retries: **(not documented)**.

**7. Output.**
- Summary, confidence score, P0 to P2 inline comments, and an optional status check.
  [config reference](https://www.greptile.com/docs/code-review/greptile-config-reference.md)
- MCP API: `trigger_code_review`, `get_code_review`. [MCP tools](https://www.greptile.com/docs/mcp/tools.md)
- CLI `--json`: its schema is **(not documented)**.
  [CLI reviews](https://www.greptile.com/docs/self-hosting/cli-reviews.md)

**8. Limits.** Credit tiers per review. File and token caps: **(not documented)**.
[review tiers](https://www.greptile.com/docs/code-review/review-tiers.md)

### Qodo Merge / open-source PR-Agent (source read)

**1. Code access: API only.**
- **GitHub:**
  - Changed files come from the PR files API, and contents are pinned to `head.sha`.
  - Original content is read at `compare(base, head).merge_base_commit`.
  - While paging it re-checks that head, base and `changed_files` didn't change.
  - Full content is loaded for at most 50 files (`MAX_FILES_ALLOWED_FULL`).
  - [github_provider.py](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/git_providers/github_provider.py),
    [git_provider.py](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/git_providers/git_provider.py)
- **GitLab:**
  - Uses `diff_refs.base_sha` and `head_sha`.
  - Retries once if `diff_refs` change during paging.
  - Refuses a truncated diff: a `changes_count` of "1000+" raises an error.
  - [gitlab_provider.py](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/git_providers/gitlab_provider.py)
- **Clone helper** (not used by review):
  - `--filter=blob:none --depth 1` with a 20 s timeout.
  - The token goes into `http.extraHeader` through `GIT_CONFIG_*` env vars, so it never appears in
    argv or the URL.
  - [git_provider.py](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/git_providers/git_provider.py)
- **Large PRs:** token-budgeted compression. Files that don't fit are listed by name, and clipped
  files are recorded as partial coverage.
  [pr_processing.py](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/algo/pr_processing.py)
- **Repo context:** `AGENTS.md` is read from the default branch, capped at 500 lines.
  [repo_context.py](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/algo/repo_context.py)

**2. Execution.**
- Webhook servers run reviews as in-process background tasks, with no queue and no sandbox. The
  LLM is one prompt in, YAML out.
  [github_app.py](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/servers/github_app.py),
  [pr_reviewer.py](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/tools/pr_reviewer.py)
- The GitHub Action runs on `pull_request_target` with no checkout step, and the docs warn against
  running PR code in a job that has secrets.
  [installation docs](https://github.com/qodo-ai/pr-agent/blob/main/docs/docs/installation/github.md)

**3. Credentials.**
- One process holds the installation or PAT write token and the LLM key.
  [github_provider.py](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/git_providers/github_provider.py)
- Providers with no key get a dummy key, so ambient env keys are never picked up.
  [litellm_ai_handler.py](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/algo/ai_handlers/litellm_ai_handler.py)
- Token formats are redacted from logs.
  [git_provider.py](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/git_providers/git_provider.py)

**4. Trust boundary.**
- **Root config:** `.pr_agent.toml` is read at a SHA resolved from the config branch, falling back
  to the default branch, never from the PR head.
  [github_provider.py](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/git_providers/github_provider.py)
- **Per-directory files:**
  - come from the same snapshot;
  - are capped at 20 files and 5 MB;
  - are limited to model, language and glob keys.
  - [git_providers/utils.py](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/git_providers/utils.py)
- **Host-only keys** (`config_security.py`): URLs, keys, tokens, retries, timeouts, `push_outputs`
  and prompt fragments. Any key ending in `url`, `key`, `token` or `secret` is host-only.
  [config_security.py](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/config_security.py)
- **Ignore rules** fail open on unexpected errors.
  [file_filter.py](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/algo/file_filter.py)
- **Prompt:** the system prompt marks title, description, commits and code as untrusted.
  Repo-context files go in escaped tags inside fences longer than any backtick run.
  [pr_reviewer_prompts.toml](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/settings/pr_reviewer_prompts.toml)
- **Hosted Qodo** says to commit the config to the default branch.
  [configuration file](https://docs.qodo.ai/configuration/configuration-file)

**5. Diff.**
- **Full review:** merge base to head.
- **Incremental review (`/review -i`):**
  - It is anchored on the bot's last review comment.
  - Commits are compared by committer date, so rebased commits count as new.
  - Merge commits are skipped.
  - It falls back to a full review when nothing predates the last review.
  - [pr_reviewer.py](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/tools/pr_reviewer.py)
- **Prior findings** are fed back as Active, Resolved or Dismissed.
  [pr_reviewer_prompts.toml](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/settings/pr_reviewer_prompts.toml)

**6. Lifecycle.**
- **Concurrency:** one running plus one waiting run per PR, process-local. Further pushes are
  dropped, and a running review is never cancelled.
  [servers/utils.py](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/servers/utils.py)
- **Timeouts:** `ai_timeout = 120` s per call, and no whole-job timeout.
  [configuration.toml](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/settings/configuration.toml)
- **Retries** in [litellm_ai_handler.py](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/algo/ai_handlers/litellm_ai_handler.py):
  - 4 retries on 429, 5xx and transport errors;
  - `Retry-After` honoured up to 30 s;
  - never replays 400, 401, 403 or 422;
  - then a chain of fallback models.
- **Failure:** the user sees "Failed to review PR". A reason line appears only with
  `publish_error_details`. Partial chunk results are still published.
  [pr_reviewer.py](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/tools/pr_reviewer.py)

**7. Output.**
- YAML validated by pydantic.
- Structured JSON with `files_not_reviewed` and `files_partially_reviewed`.
- One persistent comment found by a marker.
- An optional check run.
- `/review` does not call auto-approve.
- Sources: [pr_reviewer.py](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/tools/pr_reviewer.py),
  [github_provider.py](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/git_providers/github_provider.py)

**8. Limits.** `max_model_tokens = 32000`, 3 chunked calls, comment body capped at 65,000
characters. [configuration.toml](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/settings/configuration.toml)

Hosted Qodo's Context Engine indexing and its worker topology are **(not documented)**.
[on-prem architecture](https://docs.qodo.ai/on-prem/qodo-on-premise/architecture.md)

### GitHub Copilot code review

**1. Code access.**
- Agentic context gathering on Actions, pulling code, structure and references as needed.
  [changelog 2026-03-05](https://github.blog/changelog/2026-03-05-copilot-code-review-now-runs-on-an-agentic-architecture/),
  [concepts](https://docs.github.com/en/copilot/concepts/agents/code-review)
- It reuses the cloud agent's environment setup. If the setup steps don't clone, Copilot clones
  after them, and it overrides `fetch-depth`.
  [agent environment](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/customize-the-agent-environment)
- MCP tools are limited to read-only.
  [changelog 2026-07-29](https://github.blog/changelog/2026-07-29-copilot-code-review-agent-skills-and-mcp-now-generally-available/)
- The 300-file and 20k-line cap was removed.
  [changelog 2026-08-27](https://github.blog/changelog/2026-08-27-copilot-code-review-resolution-reasons-and-expanded-capabilities/)

**2. Execution.**
- **Runners:** GitHub-hosted, larger, or self-hosted. Self-hosted runners must be ARC on Ubuntu
  x64, with egress to Copilot hosts.
  [configure runners](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/request-a-code-review/configure-runners)
- **Firewall:** on by default, but not on self-hosted runners.
  [changelog 2026-07-17](https://github.blog/changelog/2026-07-17-copilot-code-review-customization-and-configurability-improvements/)
  It covers only processes started by the agent's Bash tool, not MCP servers or setup steps, and
  GitHub says it is not a comprehensive security solution.
  [firewall](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/customize-the-agent-firewall)
- **Static analysis:** CodeQL, ESLint and PMD can be toggled.
  [changelog 2025-11-20](https://github.blog/changelog/2025-11-20-linter-integration-with-copilot-code-review-now-in-public-preview/)
- **Fallback:** if Actions fails, a non-agentic review is still produced.
  [concepts](https://docs.github.com/en/copilot/concepts/agents/code-review)

**3. Credentials.**
- Setup steps run with minimal job `permissions`, and Copilot gets its own token. That token's
  scope and lifetime are **(not documented)**.
  [agent environment](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/customize-the-agent-environment)
- The model runs on GitHub's service, so there is no customer LLM key.
  [configure runners](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/request-a-code-review/configure-runners)
- **Contrast, GitHub Agentic Workflows:** read-only tokens by default, and writes only through
  declared safe outputs in isolated downstream jobs.
  [agentic workflows](https://docs.github.com/en/copilot/concepts/agents/about-github-agentic-workflows)

**4. Trust boundary.**
- Custom instructions are now read from the PR's head branch instead of the base. This covers
  `AGENTS.md`, `*.instructions.md` and skills.
  [changelog 2026-07-17](https://github.blog/changelog/2026-07-17-copilot-code-review-customization-and-configurability-improvements/),
  [coding guidelines](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/request-a-code-review/configure-coding-guidelines)
- Prompt-injection defenses for code review: **(not documented)**.
  [responsible use](https://docs.github.com/en/copilot/responsible-use/code-review)

**5. Diff.**
- Merge-base and delta scope: **(not documented)**.
- Copilot may repeat comments that were resolved before.
  [use code review](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/request-a-code-review/use-code-review)

**6. Lifecycle.**
- **Triggers:** manual, automatic, on new pushes, or by ruleset.
  [concepts](https://docs.github.com/en/copilot/concepts/agents/code-review)
- **Timeout:** the setup job's `timeout-minutes` is at most 59.
  [agent environment](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/customize-the-agent-environment)
- **Session logs** are linked from the PR timeline.
  [use code review](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/request-a-code-review/use-code-review)
- Cancellation on push and the failure UX: **(not documented)**.

**7. Output.**
- A "Comment" review by default.
  [use code review](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/request-a-code-review/use-code-review)
- Opt-in approvals that count toward required reviews and are dismissed on a new push (GA
  2026-10-28). [changelog 2026-10-28](https://github.blog/changelog/2026-10-28-copilot-code-review-merge-with-confidence-with-max-and-approvals/)
- Machine-readable findings: **(not documented)**.

**8. Limits.**
- AI credits per review.
- Actions minutes on private repos since 2026-06-01.
  [changelog 2026-04-27](https://github.blog/changelog/2026-04-27-github-copilot-code-review-will-start-consuming-github-actions-minutes-on-june-1-2026/)

### Cursor Bugbot

**1. Code access.**
- A Rust git integration built to minimise data fetched, with batching and rate-limit handling.
  The agent reasons over the diff and calls tools for context.
  [building Bugbot](https://cursor.com/blog/building-bugbot)
- Clone vs API: **(not documented)**.
- **Never read:** `.env`, `*.pem` and `*.tfvars` contents. `linguist-generated` files are excluded,
  and symlink targets are not reviewed.
  [Bugbot docs](https://cursor.com/docs/bugbot),
  [release notes](https://cursor.com/docs/release-notes/bugbot)

**2. Execution.**
- The review environment is **(not documented)**. Bugbot does not run code.
  [Autofix blog](https://cursor.com/blog/bugbot-autofix)
- **Autofix** uses cloud agents in Firecracker microVMs in a separate AWS account. Egress is open
  by default and can be allowlisted.
  [cloud agent security](https://cursor.com/docs/cloud-agent/security)

**3. Credentials.** The App has PR write and checks.
[GitHub integration](https://cursor.com/docs/integrations/github) Separation between the review LLM
and the write token: **(not documented)**.

**4. Trust boundary.**
- `.cursor/config/bugbot.yaml` is read from the PR's base branch. [Bugbot docs](https://cursor.com/docs/bugbot)
- The branch for `BUGBOT.md`: **(not documented)**.
- Commands posted by bots are ignored. Auto-review of outside contributors can be turned off.
  [release notes](https://cursor.com/docs/release-notes/bugbot)

**5. Diff.** By default, only the changes since the previous Bugbot review.
[Bugbot docs](https://cursor.com/docs/bugbot) Rebase handling: **(not documented)**.

**6. Lifecycle.**
- A run cancelled by a newer commit, or one that hit an internal error, completes its check as
  `neutral`. `skipped` is never used. [Bugbot docs](https://cursor.com/docs/bugbot)
- Brief network errors during the pre-post recheck no longer fail the run.
  [release notes](https://cursor.com/docs/release-notes/bugbot)
- v1 quality pipeline: 8 parallel passes with randomised diff order, majority voting and a
  validator. It has since been replaced by an agentic design.
  [building Bugbot](https://cursor.com/blog/building-bugbot)

**7. Output.**
- Comments, the `Cursor Bugbot` check, and an admin API with per-review results.
- `POST /bugbot/review` supports `dryRun`.
- [Bugbot docs](https://cursor.com/docs/bugbot)

**8. Limits.** PRs over 100k lines or 3M characters are skipped with a comment and a neutral check,
at no charge. Files over 2 MB are skipped.
[release notes](https://cursor.com/docs/release-notes/bugbot)

### Graphite Agent

- **Code access:** opt-in Code Indexing, kept "for short periods". Without it, tool calls are rate
  limited, which suggests API fallback **(inference)**.
  [code indexing](https://graphite.com/docs/code-indexing)
- **Credentials:** the App has write on contents, PRs, checks and workflows, and uses short-lived
  tokens. [privacy and security](https://graphite.com/docs/privacy-and-security),
  [GitHub App auth](https://graphite.com/docs/authenticate-with-github-app)
- **Config:** rules live in the UI, plus repo docs selected by glob. Files marked
  `linguist-generated=true` are skipped and large files are truncated. Branch: **(not
  documented)**. [customization](https://graphite.com/docs/ai-review-customization)
- **Lifecycle:** Running, Completed, Not running. PRs over 200k characters are not analysed.
  [AI review comments](https://graphite.com/docs/ai-review-comments)
- **Sandbox, diff base, retries, API:** **(not documented)**.

### CodeAnt AI

- **Code access and runtime:** **(not documented)** for PR review. The CLI agent receives the diff
  and full file content, and explores with Read, Glob and Grep.
  [CLI review](https://docs.codeant.ai/cli/review.md)
- **Approval policy:** `.codeant/approval.json` is read from the base branch, and a PR that changes
  it goes to human review.
  [auto-approve](https://docs.codeant.ai/pull_request/features/auto_approve_pr.md)
- **Auto-approve behaviour:**
  - pins the reviewed head SHA;
  - a push within a 60 s delay skips approval;
  - fails closed on model errors and above 300 files or 60k patch characters;
  - has a shadow mode.
  - Same source.
- **Machine-readable output:** CI writes `results.json`, SARIF and quality-gate XML. CLI headless
  mode prints JSON and exits 1 at the `--fail-on` threshold.
  [CI GitHub](https://docs.codeant.ai/control_center/ci/ci_github.md),
  [CLI review](https://docs.codeant.ai/cli/review.md)
- **Self-managed GitLab** needs `api` and `write_repository`.
  [GitLab self-hosted](https://docs.codeant.ai/setup/gitlab/self-hosted/control_center.md)

### Bito AI Code Review Agent

**1 and 2. Code access and execution.**
- The container checks out the diff and clones the repo. Both are deleted after each review.
  [privacy and security](https://docs.bito.ai/privacy-and-security.md)
- **Context:** symbol index, AST and embeddings.
  [overview](https://docs.bito.ai/ai-code-reviews-in-git/overview.md)
- **Self-hosted runtime:** `bitoai/cra` launched by `bito-cra.sh`.
  [bito-cra.sh](https://raw.githubusercontent.com/gitbito/CodeReviewAgent/main/cra-scripts/bito-cra.sh)
  - CLI mode: `docker run --rm` per review.
  - Server mode: one detached, long-lived container.
  - No network or hardening flags.
- **Tools run on PR code:** fbinfer, ruff, mypy, linters, a secret scanner and dependency checks.
  [bito-cra.properties](https://raw.githubusercontent.com/gitbito/CodeReviewAgent/main/cra-scripts/bito-cra.properties)
- **LLM path:** even when self-hosted, the diff and context go to Bito's service, which calls the
  LLM. [privacy and security](https://docs.bito.ai/privacy-and-security.md)

**3. Credentials.**
- A classic GitHub PAT with repo scope; fine-grained tokens are unsupported. Plus a Bito access key.
  [prerequisites](https://docs.bito.ai/ai-code-reviews-in-git/install-run-as-a-self-hosted-service/prerequisites.md)
- **Anti-pattern (inference from the script):** the script echoes the docker command, tokens
  included. [bito-cra.sh](https://raw.githubusercontent.com/gitbito/CodeReviewAgent/main/cra-scripts/bito-cra.sh)
- **Anti-pattern:** the sample workflow grants `contents: write`.
  [test_cra.yml](https://raw.githubusercontent.com/gitbito/CodeReviewAgent/main/.github/workflows/test_cra.yml)

**4. Trust boundary.** `.bito.yaml` and the guideline files it references are read from the PR's
source branch, unless an admin turns off file overrides.
[repo-level settings](https://docs.bito.ai/ai-code-reviews-in-git/agent-settings/repo-level-settings.md)

**5 and 6. Diff and lifecycle.**
- Incremental per push. `/review full` reviews everything. If the last automated review failed,
  `/review` runs a full review.
  [commands](https://docs.bito.ai/ai-code-reviews-in-git/available-commands.md)
- Control commands: `/abort`, `/pause`.
- Timeouts and retries: **(not documented)**.

**7. Output.**
- Comments, plus optional "Request changes" reviews.
  [request changes](https://docs.bito.ai/ai-code-reviews-in-git/request-changes-comments.md)
- The script has `output_path` and `posting_to_pr` options. Their format is **(unverified)**.
  [bito-cra.sh](https://raw.githubusercontent.com/gitbito/CodeReviewAgent/main/cra-scripts/bito-cra.sh)

### GitLab Duo Code Review (source read)

**1. Code access.**
- The Code Review Flow runs as a CI/CD job and needs a runner.
  [Code Review Flow](https://docs.gitlab.com/user/duo_agent_platform/flows/foundational_flows/code_review/)
- **Clone settings in source:**
  - `GIT_DEPTH: 0` and `GIT_LFS_SKIP_SMUDGE: 1`.
  - A blobless fetch by default.
  - An optional reference repository.
  - The job checks out the MR source branch.
  - [start_workflow_service.rb](https://gitlab.com/gitlab-org/gitlab/-/blob/master/ee/app/services/ai/duo_workflows/start_workflow_service.rb),
    [runtimes/ci.rb](https://gitlab.com/gitlab-org/gitlab/-/blob/master/ee/app/services/ai/duo_workflows/runtimes/ci.rb)
- **Review context** is built from the REST API:
  - the MR diffs;
  - original files read at `ref=<target_branch>`, the target branch tip, not the merge base.
  - [build_review_merge_request_context.py](https://gitlab.com/gitlab-org/modelops/applied-ml/code-suggestions/ai-assist/-/blob/main/duo_workflow_service/tools/code_review/build_review_merge_request_context.py)
- **Flow shape (v4):**
  1. Deterministic context build.
  2. LLM directory selection.
  3. LLM prescan with file tools.
  4. A review LLM call with no tools.
  5. Deterministic publish.
  - [4.0.0.yml](https://gitlab.com/gitlab-org/modelops/applied-ml/code-suggestions/ai-assist/-/blob/main/ai/features/review/code_review/config/4.0.0.yml)
- **Size:** context is capped at about 1 MiB and truncated to about 800 KiB. Files over 10k lines
  send only the diff. A deterministic detector labels a partial review as partial.
  [Code Review Flow](https://docs.gitlab.com/user/duo_agent_platform/flows/foundational_flows/code_review/),
  [detect_review_context_truncation.py](https://gitlab.com/gitlab-org/modelops/applied-ml/code-suggestions/ai-assist/-/blob/main/duo_workflow_service/tools/code_review/detect_review_context_truncation.py)

**2. Execution.**
- **Split:** the LLM side (Duo Workflow Service in the AI Gateway) drives an executor in the CI
  job over WebSocket. API calls are proxied through the executor.
  [flow execution](https://docs.gitlab.com/user/duo_agent_platform/flows/execution/),
  [executor_http_client.py](https://gitlab.com/gitlab-org/modelops/applied-ml/code-suggestions/ai-assist/-/blob/main/duo_workflow_service/gitlab/executor_http_client.py)
- **Runners:** need the `gitlab--duo` tag and a docker or kubernetes executor.
- **Sandbox (SRT):** gives network and filesystem isolation but needs a privileged runner. If it
  can't start, the flow runs unsandboxed and logs it. The setup script runs outside it.
  [flow execution](https://docs.gitlab.com/user/duo_agent_platform/flows/execution/)
- **Sandbox in source:** bubblewrap or Landlock, leaf-path filesystem grants, and a domain
  allowlist. [sandbox.rb](https://gitlab.com/gitlab-org/gitlab/-/blob/master/ee/lib/gitlab/duo_workflow/sandbox.rb)
- **Declared privileges** include read-write GitLab API and run-commands.
  [definition.rb](https://gitlab.com/gitlab-org/gitlab/-/blob/master/ee/app/models/ai/catalog/foundational_flow/code_review/definition.rb)

**3. Credentials.**
- **Composite identity:**
  - one token combining the triggering user and a per-flow service account;
  - effective access is the more restrictive of the two;
  - `ai_workflows` and `mcp` scopes only;
  - one-time use.
  - [composite identity](https://docs.gitlab.com/user/duo_agent_platform/composite_identity/)
- The token expires after 1 hour, and a revoke service exists.
  [create_composite_oauth_access_token_service.rb](https://gitlab.com/gitlab-org/gitlab/-/blob/master/ee/app/services/ai/duo_workflows/create_composite_oauth_access_token_service.rb)
- The job holds this token, and it can post comments through the `add_comments` API.
  [code_review.rb API](https://gitlab.com/gitlab-org/gitlab/-/blob/master/ee/lib/api/ai/duo_workflows/code_review.rb)
- Behind a flag, tokens are kept out of clone URLs, and a credential helper reads them from env.
  [start_workflow_service.rb](https://gitlab.com/gitlab-org/gitlab/-/blob/master/ee/app/services/ai/duo_workflows/start_workflow_service.rb)
- LLM keys stay in the AI Gateway.

**4. Trust boundary.**
- `.gitlab/duo/mr-review-instructions.yaml` is resolved from `project.default_branch_or_main`.
  [custom_instructions_resolver.rb](https://gitlab.com/gitlab-org/gitlab/-/blob/master/ee/lib/gitlab/duo/code_review/custom_instructions_resolver.rb)
- Exclusion rules and `agent-config.yml` are also read from the default branch. The flow does not
  reference `AGENTS.md` or `SKILL.md`.
  [Code Review Flow](https://docs.gitlab.com/user/duo_agent_platform/flows/foundational_flows/code_review/),
  [flow execution](https://docs.gitlab.com/user/duo_agent_platform/flows/execution/)
- **Prompt security** on tool output:
  - `<system>` and `<goal>` tags are encoded;
  - hidden Unicode and HTML comments are stripped;
  - tools carry trust levels;
  - detection by a third-party scanner is evaluation-only.
  - [TOOL_RESPONSE_SECURITY.md](https://gitlab.com/gitlab-org/modelops/applied-ml/code-suggestions/ai-assist/-/blob/main/duo_workflow_service/security/TOOL_RESPONSE_SECURITY.md)

**5. Diff.** Incremental review in v4, behind a flag:
- The baseline is `last_reviewed_head_sha`.
- Head unchanged: skip.
- `patch_id` unchanged: skip the compare.
- Base changed (rebase): full review.
- Otherwise, compare from the baseline and intersect with the MR diff.
- A force push that orphans the baseline (404) falls back to a full review.
- New lines are tagged as since the last review.
- [build_review_merge_request_context.py](https://gitlab.com/gitlab-org/modelops/applied-ml/code-suggestions/ai-assist/-/blob/main/duo_workflow_service/tools/code_review/build_review_merge_request_context.py)

**6. Lifecycle.**
- **Superseding:** a push during a review stops it and starts a new one.
  [Code Review Flow](https://docs.gitlab.com/user/duo_agent_platform/flows/foundational_flows/code_review/)
  `CancelReviewService` stops the workflow and deletes the progress note.
  [cancel_review_service.rb](https://gitlab.com/gitlab-org/gitlab/-/blob/master/ee/app/services/ai/duo_workflows/code_review/cancel_review_service.rb)
- **Typed errors** (DCR codes): no runner, no compute minutes, token minting failed, comments not
  posted after several attempts.
  [troubleshooting](https://docs.gitlab.com/user/duo_agent_platform/flows/foundational_flows/code_review/troubleshooting/)
- **Retries:** the executor's HTTP client retries 401, 429 and 5xx, 4 attempts at 3, 9 and 27 s.
  [executor_http_client.py](https://gitlab.com/gitlab-org/modelops/applied-ml/code-suggestions/ai-assist/-/blob/main/duo_workflow_service/gitlab/executor_http_client.py)
- The overall flow timeout: **(not documented)**.

**7. Output.**
- In v4 the review LLM returns schema-validated findings. A deterministic step gates them by
  confidence (logged, never silent) and posts them.
  [post_duo_code_review_findings.py](https://gitlab.com/gitlab-org/modelops/applied-ml/code-suggestions/ai-assist/-/blob/main/duo_workflow_service/tools/code_review/post_duo_code_review_findings.py)
- A beta Duo approval does not count toward required approvals.
  [Code Review Flow](https://docs.gitlab.com/user/duo_agent_platform/flows/foundational_flows/code_review/)

**8. Limits.** 1 MiB of context and a flat per-review price.
[GitLab credits](https://docs.gitlab.com/subscriptions/gitlab_credits/)

### Ellipsis

**1 and 2. Code access and execution.**
- Each session runs in its own cloud environment, with repos checked out under `/sandbox` at the
  PR commit. [environments](https://www.ellipsis.dev/docs/environments),
  [sessions](https://www.ellipsis.dev/docs/sessions)
- **Setup layers** are cached and rerun only when their `inputs` or the commit change, each with
  its own timeout. [environments](https://www.ellipsis.dev/docs/environments)
- Isolation technology and egress: **(not documented)**.
  [security](https://www.ellipsis.dev/docs/security)
- **Retention:** a session TTL set by the customer, and webhook payloads kept at most 3 days.
  [security](https://www.ellipsis.dev/docs/security)

**3. Credentials.**
- Every reviewer's GitHub token reads only contents and PRs. `permissions.github` can add read
  scopes but never write, and the platform posts the review.
  [code review](https://www.ellipsis.dev/docs/code-review)
- Environment secrets named like model keys (`ANTHROPIC_*`, `OPENAI_*`) are rejected.
  [environment schema](https://www.ellipsis.dev/docs/environments/schema)

**4. Trust boundary.**
- `.ellipsis/code-review.yaml` is committed to the default branch.
  [which PRs get reviewed](https://www.ellipsis.dev/docs/code-review/which-prs-get-reviewed)
- An invalid config keeps the last valid version running and reports the error as a check.
  [code review](https://www.ellipsis.dev/docs/code-review)
- Ellipsis's own changes never trigger sessions. [triggers](https://www.ellipsis.dev/docs/agents/triggers)

**5 and 6. Diff and lifecycle.**
- A reviewer sees only commits it has not reviewed.
  [which PRs get reviewed](https://www.ellipsis.dev/docs/code-review/which-prs-get-reviewed)
- A check for a commit that is no longer the head starts nothing.
  [triggers](https://www.ellipsis.dev/docs/agents/triggers)
- **Session error reasons:** `budget_hit`, `upstream_unavailable` (after retries), `interrupted`,
  `missing_token_permissions`. [lifecycle](https://www.ellipsis.dev/docs/sessions/lifecycle)
- **Partial failure** ([code review](https://www.ellipsis.dev/docs/code-review)):
  - A failed reviewer contributes nothing and re-covers the same commits on the next push.
  - If no reviewer finishes, nothing is posted and nothing is marked reviewed.
  - An incomplete review is never posted as an approval.

**7. Output.**
- Blocking or non-blocking comments, with `on_pass` (silent, comment, approve) and `on_fail`
  actions. [code review](https://www.ellipsis.dev/docs/code-review)
- Typed `output.json_schema`. [agent schema](https://www.ellipsis.dev/docs/agents/schema)
- Outbound webhooks: Standard Webhooks HMAC signatures, retries at 1 and 10 minutes, and possible
  duplicates. [webhooks](https://www.ellipsis.dev/docs/webhooks)

**8. Limits.** 64 comments per review, and a $250 default session budget.
[budgets](https://www.ellipsis.dev/docs/budgets)

### Sourcery

- **Code access:** reads the diff, plus related code for some changes. The mechanism is **(not
  documented)**. No code is retained.
  [code retention](https://docs.sourcery.ai/trust/code-retention/)
- **Credentials:** the App has write on actions, checks, code, statuses, issues, PRs and
  workflows. [GitHub quickstart](https://docs.sourcery.ai/start/quickstart-github/)
- **Config:** review settings are in the dashboard. No PR-controlled review config is documented.
  Lockfiles and generated files are skipped.
  [configure](https://docs.sourcery.ai/reviews/configure/), [FAQ](https://docs.sourcery.ai/reference/faq/)
- **Lifecycle:**
  - Re-review after each commit.
  - Check states: in progress, success, failure (blocking security findings only), skipped
    (rate limit or cap).
  - [anatomy of a review](https://docs.sourcery.ai/reviews/anatomy-of-a-review/)
  - A rate limit never blocks a merge. [plans](https://docs.sourcery.ai/admin/plans/)
- **Limits:** diff-character caps per PR (150k to 500k by plan) and per rolling 7 days.
  [plans](https://docs.sourcery.ai/admin/plans/)
- **Self-hosted:** Enterprise, through sales.
  [self-hosted](https://docs.sourcery.ai/trust/self-hosted-sourcery/)
- Runtime isolation: **(not documented)**.

### Kodus (source read)

The pipeline runs 19 linear stages, from ValidatePrerequisites through CreateSandbox, AgentReview
and RequestChangesOrApprove.
[code-review-pipeline.strategy.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/code-review/pipeline/strategy/code-review-pipeline.strategy.ts)

**1. Code access.**
- **Diff:** PR files from the API, cached by PR and head SHA.
- **Contents:** batched GraphQL reads at `head.ref`, which is a **branch name, not a SHA**, so a
  race with a newer push is possible **(inference)**.
  [github.service.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/platform/infrastructure/adapters/services/github/github.service.ts)
- **Sandbox checkout:**
  - `git init`, then a depth-1 fetch of `refs/pull/N/head` or `refs/merge-requests/N/head`.
  - The base branch is fetched at depth 1.
  - Submodules are fetched only if the base branch declares them.
  - `core.hooksPath=/dev/null`.
  - [e2b-sandbox.service.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/sandbox/infrastructure/providers/e2b-sandbox.service.ts),
    [local-sandbox.service.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/sandbox/infrastructure/providers/local-sandbox.service.ts)
- **Index:** an AST graph of the default branch, rebuilt incrementally on merge.
  [sandbox docs](https://docs.kodus.io/en/how_to_deploy/deploy_kodus/sandbox.md)
- **Limits:** 2,000 files. Context overflow gets one retry at 0.6× the window.
  [fetch-changed-files.stage.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/code-review/pipeline/stages/fetch-changed-files.stage.ts),
  [overflow-recovering-runner.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/code-review/infrastructure/agents/core/overflow-recovering-runner.ts)

**2. Execution.**
- **E2B VM per PR lease:**
  - 35-minute timeout;
  - paused and resumed for the next review of the same PR;
  - push URL set to `no-push-allowed`.
  - [e2b-sandbox.service.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/sandbox/infrastructure/providers/e2b-sandbox.service.ts),
    [sandbox-lease-manager.service.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/sandbox/infrastructure/services/sandbox-lease-manager.service.ts)
- **Local provider:**
  - a temp dir with no container isolation;
  - a command allowlist with pipes only;
  - blocked flags such as `find -exec`;
  - realpath containment;
  - 30 s and 5 MB per command.
  - [local-sandbox.service.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/sandbox/infrastructure/providers/local-sandbox.service.ts)
- **Privilege drop:** strips setuid bits and capabilities, and verifies that `sudo -n` fails.
  [review-user-privileges.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/sandbox/infrastructure/providers/review-user-privileges.ts)
- **`checkTypes` runs project toolchains** such as `tsc`, `cargo check` and `dotnet build` on PR
  code. [agent-tools.factory.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/code-review/infrastructure/agents/engine/agent-tools.factory.ts)
- **Custom-agent egress:** a relay that allowlists exact SNI hostnames and refuses private IPs.
  [cli-egress-proxy.source.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/code-review/infrastructure/agents/custom-agent-cli/cli-egress-proxy.source.ts)
- **Fallback:** if sandbox creation fails, the review continues without tools and shows a
  `SANDBOX_UNAVAILABLE` warning.
  [agent-review.stage.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/code-review/pipeline/stages/agent-review.stage.ts)

**3. Credentials.**
- **The clone token** reaches git only through env-based `http.extraHeader` or `GIT_CONFIG_*`,
  never the URL, argv or disk. No step scrubs it from the env afterwards **(not found)**.
  [git-auth-header.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/sandbox/infrastructure/providers/git-auth-header.ts)
- **The worker** holds the write token and drives the LLM.
  [finish-process-review.stage.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/code-review/pipeline/stages/finish-process-review.stage.ts)
- **BYOK keys** are decrypted only when a model is built.
  [libs/llm/README.md](https://github.com/kodustech/kodus-ai/blob/main/libs/llm/README.md)

**4. Trust boundary.**
- **Skills and docs** are always read from the target branch, never the head, so that a PR can't
  rewrite its own rules.
  - Read via `git ls-tree` and `cat-file` on `refs/remotes/origin/<base>`.
  - Symlinks are refused and files are capped at 512 KiB.
  - [base-branch-reader.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/code-review/infrastructure/agents/knowledge/base-branch-reader.ts)
- **Repo instructions** (`CLAUDE.md`, `AGENTS.md` and others):
  - opt-in, read from base;
  - 8 files and 64 KB at most;
  - a finding that cites one must quote it.
  - [repo-instructions.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/code-review/infrastructure/agents/collaborators/repo-instructions.ts)
- **`kodus-config.yml`** is read from the default branch.
  [codeBaseConfig.service.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/ee/codeBase/codeBaseConfig.service.ts)
- **Prompt framing:** repo text is labelled untrusted, and untrusted text inside tags is escaped.
  [prompt-builder.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/code-review/infrastructure/agents/prompts/prompt-builder.ts)

**5. Diff.**
- The incremental baseline is the last **successful** run.
- **Force push or rebase:**
  - an orphaned baseline is detected;
  - retargeting the base causes a full review;
  - otherwise a line-key LCS interdiff, bounded at 200 ms;
  - an empty interdiff skips the review.
  - [validate-new-commits.stage.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/code-review/pipeline/stages/validate-new-commits.stage.ts),
    [rebase-interdiff.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/code-review/infrastructure/adapters/services/rebase-interdiff.ts)
- A full re-run is forced after a partial error.

**6. Lifecycle.**
- **Per-PR lock:** a losing automatic run is dropped. A command run is deferred with backoff for
  up to about 25 minutes.
  [pr-review-deferral.service.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/code-review/workflow/pr-review-deferral.service.ts)
- **Queue:** inbox-claim idempotency, and a 25 s drain on shutdown.
  [workflow-job-consumer.service.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/core/workflow/infrastructure/workflow-job-consumer.service.ts)
- **Check runs:** NEUTRAL on partial error, FAILURE on critical. The check-run ID is saved so a
  watchdog can close the check if the worker dies.
  [code-review-job-processor.service.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/code-review/workflow/code-review-job-processor.service.ts)
- **Provider errors** are classified (auth, quota, rate limit, context overflow) and reported.
  [provider-error.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/code-review/infrastructure/agents/collaborators/provider-error.ts)

**7. Output.**
- Findings must be JSON or come through a `submitResult` tool. Repeats are dropped.
  [agent-review.stage.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/code-review/pipeline/stages/agent-review.stage.ts)
- **Approval:**
  - off by default;
  - needs zero comments and a clean review;
  - is held if the live head differs from the reviewed SHA.
  - [decide-pr-approval.stage.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/code-review/pipeline/stages/decide-pr-approval.stage.ts)

---

## (d) pergent agent mode vs the field

Each heading is a decision from the
[design](../superpowers/specs/2026-10-29-pergent-agent-mode-design.md). Each verdict means
"compared with the products that document this area".

### 1. The Job prepares the checkout: two-commit fetch plus a shared cache

**Verdict: same as the best; better than API-only and long-lived-worker designs; two gaps.**
- **Closest match:** Kodus does `git init` and depth-1 fetches of the PR head ref and base.
  [e2b-sandbox.service.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/sandbox/infrastructure/providers/e2b-sandbox.service.ts)
- **Heavier:**
  - GitLab Duo fetches full depth, blobless, with an optional reference repo.
    [start_workflow_service.rb](https://gitlab.com/gitlab-org/gitlab/-/blob/master/ee/app/services/ai/duo_workflows/start_workflow_service.rb)
  - CodeRabbit does a full clone with an optional encrypted cache.
    [FAQ](https://docs.coderabbit.ai/faq.md)
- **Greptile** shares one writable 1 TiB volume across reviews.
  [values.yaml](https://raw.githubusercontent.com/greptileai/akupara/main/deploy/kubernetes/charts/greptile/values.yaml)
  pergent's per-Job `emptyDir` is cleaner.
- **PR-Agent** doesn't clone at all, so its model can't explore.
  [github_provider.py](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/git_providers/github_provider.py)
- **pergent is ahead** in fetching the exact merge-base SHA.
  - GitLab Duo reads "before" files at the target branch **tip**, which can differ from the diff's
    base.
    [build_review_merge_request_context.py](https://gitlab.com/gitlab-org/modelops/applied-ml/code-suggestions/ai-assist/-/blob/main/duo_workflow_service/tools/code_review/build_review_merge_request_context.py)
  - Kodus reads contents by branch name.
    [github.service.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/platform/infrastructure/adapters/services/github/github.service.ts)
- **Gap (inference): the cache is reachable from the review container.**
  - `git clone --reference` writes an `objects/info/alternates` path into the checkout. The
    review container must therefore mount the cache at the same path, or the checkout breaks.
  - Once mounted, the agent's read tools can reach the cache. If the cache holds several repos,
    they can reach other repositories.
  - Fix: mount only that repo's cache, read-only, via `subPath`, or use `--dissociate`.
  - Only a trusted component should write the cache. Greptile's shared writable volume is the
    pattern to avoid.
- **Gap (inference): fetching by SHA must work.**
  - Fetching `<base>` by SHA needs the server to accept a request for that commit.
  - Check this on both platforms, including fork MRs.
  - Kodus fetches by PR ref (`refs/pull/N/head`, `refs/merge-requests/N/head`), which is a portable
    fallback for the head.

### 2. Kubernetes Job isolation

**Verdict: per-review isolation matches the better half of the field. Runtime strength and the
network are behind the leaders.**
- **Per-review units elsewhere:**
  - CodeRabbit: Cloud Run microVM plus jail.
    [Google Cloud blog](https://cloud.google.com/blog/products/ai-machine-learning/how-coderabbit-built-its-ai-code-review-agent-with-google-cloud-run)
  - Cursor cloud agents: Firecracker.
    [cloud agent security](https://cursor.com/docs/cloud-agent/security)
  - Kodus: an E2B VM.
  - Ellipsis: a per-session environment.
    [sessions](https://www.ellipsis.dev/docs/sessions)
  - Copilot: Actions runners.
    [concepts](https://docs.github.com/en/copilot/concepts/agents/code-review)
  - pergent's Job is the same idea at container strength.
- **Better than:**
  - Greptile's long-lived privileged worker.
    [architecture](https://raw.githubusercontent.com/greptileai/akupara/main/docs/reference/architecture.md)
  - PR-Agent's in-process tasks.
    [github_app.py](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/servers/github_app.py)
  - Bito's server mode.
    [bito-cra.sh](https://raw.githubusercontent.com/gitbito/CodeReviewAgent/main/cra-scripts/bito-cra.sh)
- **pergent runs no repo-controlled code.** It has no linters, toolchains or setup scripts, which
  avoids a whole class of attack:
  - CodeRabbit's RuboCop RCE.
    [Kudelski](https://kudelskisecurity.com/research/how-we-exploited-coderabbit-from-a-simple-pr-to-rce-and-write-access-on-1m-repositories)
  - Kodus's `checkTypes` running `cargo check` and `dotnet build`.
  - Bito's analysers running in the same container as its PAT.
- **Behind on network:**
  - Copilot's firewall is on by default.
    [changelog 2026-07-17](https://github.blog/changelog/2026-07-17-copilot-code-review-customization-and-configurability-improvements/)
  - GitLab's sandbox has a domain allowlist.
    [sandbox.rb](https://gitlab.com/gitlab-org/gitlab/-/blob/master/ee/lib/gitlab/duo_workflow/sandbox.rb)
  - Greptile's TREX has egress modes.
    [TREX environments](https://www.greptile.com/docs/trex/environments.md)
  - Kodus has an SNI relay.
  - The design says nothing about egress.
- **Behind on runtime:** a Job is a container unless pergent.sh sets a sandboxed RuntimeClass.
- **Pitfall to avoid:** GitLab silently falls back to running unsandboxed.
  [flow execution](https://docs.gitlab.com/user/duo_agent_platform/flows/execution/)
  pergent.sh's controls should fail closed.

### 3. Token split: git token only in the init container; LLM key and one-time result token in the review container

**Verdict: stricter than almost everyone on platform credentials. Behind GitLab and Greptile on the
LLM key.**
- **Same model as pergent:**
  - Ellipsis reviewers get read-only tokens, and the platform posts.
    [code review](https://www.ellipsis.dev/docs/code-review)
  - GitHub Agentic Workflows do writes only through safe outputs in downstream jobs.
    [agentic workflows](https://docs.github.com/en/copilot/concepts/agents/about-github-agentic-workflows)
- **pergent goes further.** Its review container doesn't even hold a **read** platform token.
  Ellipsis reviewers do.
- **Worse than pergent:**
  - PR-Agent keeps one process with the write token and the LLM key.
    [github_provider.py](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/git_providers/github_provider.py)
  - Kodus's worker holds the write token and drives the LLM.
    [finish-process-review.stage.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/code-review/pipeline/stages/finish-process-review.stage.ts)
  - Greptile's worker holds the App private key by default.
    [deployment template](https://raw.githubusercontent.com/greptileai/akupara/main/deploy/kubernetes/charts/greptile/templates/deployment-components.yaml)
  - Bito's container holds a classic PAT.
    [prerequisites](https://docs.bito.ai/ai-code-reviews-in-git/install-run-as-a-self-hosted-service/prerequisites.md)
  - GitLab's job holds a 1-hour composite token that can comment.
    [code_review.rb API](https://gitlab.com/gitlab-org/gitlab/-/blob/master/ee/lib/api/ai/duo_workflows/code_review.rb)
- **On the LLM key, the field is ahead:**
  - GitLab keeps model keys in the AI Gateway, off the runner.
    [flow execution](https://docs.gitlab.com/user/duo_agent_platform/flows/execution/)
  - Greptile routes calls through an LLM proxy that alone holds the keys.
    [deployment template](https://raw.githubusercontent.com/greptileai/akupara/main/deploy/kubernetes/charts/greptile/templates/deployment-components.yaml)
  - Ellipsis rejects user secrets named like model keys.
    [environment schema](https://www.ellipsis.dev/docs/environments/schema)
  - In pergent, the provider key sits in the pod that reads untrusted code. That pod is read-only
    and has no shell, but the key is still there.
- **The single-use, hashed result token** has no direct equivalent in the docs reviewed.
  - The closest is GitLab's one-time, 1-hour, revocable composite token.
    [composite identity](https://docs.gitlab.com/user/duo_agent_platform/composite_identity/)
  - pergent's is narrower: it can only deliver one result.
- **Gap (inference): the token can leak through the shared volume.** If the init container clones
  with the token in the remote URL or a credential store, it persists into `.git/config` on the
  shared `emptyDir`, and the review container can read it.
  - Kodus and PR-Agent pass the token through env-only `http.extraHeader` / `GIT_CONFIG_*`.
    [git-auth-header.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/sandbox/infrastructure/providers/git-auth-header.ts),
    [git_provider.py](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/git_providers/git_provider.py)
  - GitLab keeps tokens out of clone URLs.
    [start_workflow_service.rb](https://gitlab.com/gitlab-org/gitlab/-/blob/master/ee/app/services/ai/duo_workflows/start_workflow_service.rb)
- **The GitLab side is weaker than the GitHub side.** GitHub gets a 1-hour installation token. On
  GitLab, a long-lived deploy token, even encrypted and rotated, is weaker than GitLab's own
  1-hour composite token. Consider short-lived tokens if the platform allows it **(inference)**.

### 4. Trusted inputs from the target: instructions and skills from the caller; `.gitattributes` and generated rules from the base

**Verdict: as strict as the strictest (Kodus, PR-Agent, GitLab, Ellipsis), and stricter on
generated-file rules. One subtlety to settle.**
- **Same boundary:**
  - Kodus reads skills, docs and instructions from the target branch.
    [base-branch-reader.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/code-review/infrastructure/agents/knowledge/base-branch-reader.ts)
  - PR-Agent reads config from the default or config branch.
    [github_provider.py](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/git_providers/github_provider.py)
  - GitLab reads from the default branch.
    [custom_instructions_resolver.rb](https://gitlab.com/gitlab-org/gitlab/-/blob/master/ee/lib/gitlab/duo/code_review/custom_instructions_resolver.rb)
  - Ellipsis reads from the default branch.
    [which PRs get reviewed](https://www.ellipsis.dev/docs/code-review/which-prs-get-reviewed)
  - Cursor reads `bugbot.yaml` from base.
    [Bugbot docs](https://cursor.com/docs/bugbot)
- **Weaker than pergent:**
  - Copilot reads from the head since July 2026.
    [changelog 2026-07-17](https://github.blog/changelog/2026-07-17-copilot-code-review-customization-and-configurability-improvements/)
  - CodeRabbit reads from the head.
    [YAML config](https://docs.coderabbit.ai/getting-started/yaml-configuration.md)
  - Greptile reads from the head, except `autoApprove`.
    [auto-approve](https://www.greptile.com/docs/code-review/auto-approve-prs.md)
  - Bito reads from the head.
    [repo-level settings](https://docs.bito.ai/ai-code-reviews-in-git/agent-settings/repo-level-settings.md)
- **Generated files:**
  - Graphite and Cursor honour `linguist-generated`, but don't say from which branch.
    [Graphite customization](https://graphite.com/docs/ai-review-customization),
    [Bugbot docs](https://cursor.com/docs/bugbot)
  - No product documents pergent's rule that a generated-code header counts only if the base
    version had it.
  - PR-Agent's ignore filter fails **open** on errors.
    [file_filter.py](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/algo/file_filter.py)
    pergent should keep its rules failing closed, so that a file is reviewed when in doubt.
- **The subtlety (inference): which branch counts as trusted?**
  - pergent reads skip rules from the merge-base commit. pergent.sh's skills and instructions
    come from "the target branch".
  - Kodus uses the target branch. PR-Agent, GitLab and Ellipsis use the **default** branch.
  - If an MR targets a branch its author can push to, "target branch" rules are author-controlled.
  - For approvals, pergent.sh could require a protected target branch, or fall back to
    default-branch rules.
  - CodeAnt's approach is related: any PR that touches the policy file goes to a human.
    [auto-approve](https://docs.codeant.ai/pull_request/features/auto_approve_pr.md)
- **Prompt hygiene** is documented by PR-Agent, Kodus and GitLab.
  [pr_reviewer_prompts.toml](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/settings/pr_reviewer_prompts.toml),
  [prompt-builder.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/code-review/infrastructure/agents/prompts/prompt-builder.ts),
  [TOOL_RESPONSE_SECURITY.md](https://gitlab.com/gitlab-org/modelops/applied-ml/code-suggestions/ai-assist/-/blob/main/duo_workflow_service/security/TOOL_RESPONSE_SECURITY.md)
  The design states that the model may read `AGENTS.md` as content, but doesn't say how that
  content is framed.

### 5. Merge-base, two-commit diff

**Verdict: same as the products that document it, and more exact than GitLab Duo. Behind on
incremental and no-op push handling.**
- **Same diff:** PR-Agent diffs merge base to head on GitHub and uses `diff_refs` on GitLab.
  [github_provider.py](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/git_providers/github_provider.py),
  [gitlab_provider.py](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/git_providers/gitlab_provider.py)
- **The head-SHA check matches PR-Agent's guards:** it re-validates head and base while paging, and
  refuses truncated diffs.
- **Incremental review:** pergent reviews the full diff each time and attaches previous findings.
  That is close to PR-Agent's Active/Resolved/Dismissed feedback, and to GitLab reading the whole
  diff while focusing on new lines.
  [Code Review Flow](https://docs.gitlab.com/user/duo_agent_platform/flows/foundational_flows/code_review/)
- **What pergent lacks:**
  - Skipping rebases that don't change the patch: GitLab compares `patch_id`, and Kodus has
    `REBASE_NO_CHANGES`.
  - Telling the model which lines are new since the last reviewed head: GitLab's
    `since_last_review` tag.
  - Sources:
    [build_review_merge_request_context.py](https://gitlab.com/gitlab-org/modelops/applied-ml/code-suggestions/ai-assist/-/blob/main/duo_workflow_service/tools/code_review/build_review_merge_request_context.py),
    [validate-new-commits.stage.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/code-review/pipeline/stages/validate-new-commits.stage.ts)
- **These belong in pergent.sh, not the CLI (inference).** For example, compare
  `git patch-id` of the new merge-base diff with the last reviewed one, and reuse the result.

### 6. JSON result with error results, exit codes, HTTP delivery and retry semantics

**Verdict: better than the field. No product publishes a versioned per-run result schema.**
- **Closest matches:**
  - GitLab v4 hands schema-validated findings to a deterministic publisher.
    [post_duo_code_review_findings.py](https://gitlab.com/gitlab-org/modelops/applied-ml/code-suggestions/ai-assist/-/blob/main/duo_workflow_service/tools/code_review/post_duo_code_review_findings.py)
  - Ellipsis has typed `output.result` and HMAC-signed webhooks with retries.
    [agent schema](https://www.ellipsis.dev/docs/agents/schema),
    [webhooks](https://www.ellipsis.dev/docs/webhooks)
  - Greptile's CLI has exit codes 0, 3, 4 and 5.
    [CLI](https://www.greptile.com/docs/code-review/greptile-cli.md)
  - CodeAnt's CI writes `results.json` and SARIF and exits on `--fail-on`.
    [CI GitHub](https://docs.codeant.ai/control_center/ci/ci_github.md)
  - PR-Agent's JSON includes `files_not_reviewed`.
    [pr_reviewer.py](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/tools/pr_reviewer.py)
- **pergent's "a failed review is still a result" with typed error codes** matches GitLab's DCR
  codes and Ellipsis's error reasons, and is more machine-friendly.
  [troubleshooting](https://docs.gitlab.com/user/duo_agent_platform/flows/foundational_flows/code_review/troubleshooting/),
  [lifecycle](https://www.ellipsis.dev/docs/sessions/lifecycle)
- **Treating 409 as delivered** matches Ellipsis's warning that webhooks may arrive more than
  once. Kodus's inbox-claim idempotency is the same pattern.
  [workflow-job-consumer.service.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/core/workflow/infrastructure/workflow-job-consumer.service.ts)
- **pergent.sh's fallback for "no result"** reads the Job status. That mirrors Kodus's watchdog,
  which finalises the check if the worker dies.
  [code-review-job-processor.service.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/code-review/workflow/code-review-job-processor.service.ts)
- **Gaps in pergent.sh's posting step (inference):**
  - CodeRabbit, Greptile, Kodus, CodeAnt and Copilot all re-check that the live head is still the
    reviewed head before approving, and dismiss approvals on a new push.
    [CodeRabbit approvals](https://docs.coderabbit.ai/pr-reviews/automatic-approvals.md),
    [Greptile auto-approve](https://www.greptile.com/docs/code-review/auto-approve-prs.md),
    [decide-pr-approval.stage.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/code-review/pipeline/stages/decide-pr-approval.stage.ts),
    [Copilot changelog 2026-10-28](https://github.blog/changelog/2026-10-28-copilot-code-review-merge-with-confidence-with-max-and-approvals/)
  - The contract in section 8 of the design should add that check before approving.

### 7. Deadline = timeout + 2 minutes; SIGTERM gives a cancelled result

**Verdict: on par with the best on cancellation, and ahead on "always deliver something". Two
timing details to fix.**
- **Cancellation on a new push:**
  - GitLab stops and replaces the review.
    [cancel_review_service.rb](https://gitlab.com/gitlab-org/gitlab/-/blob/master/ee/app/services/ai/duo_workflows/code_review/cancel_review_service.rb)
  - Cursor ends a cancelled run as `neutral`.
    [Bugbot docs](https://cursor.com/docs/bugbot)
  - CodeRabbit shows "Review cancelled" and aborts on close.
    [status check](https://docs.coderabbit.ai/pr-reviews/status-check.md)
  - Kodus drains for 25 s on shutdown.
    [workflow-job-consumer.service.ts](https://github.com/kodustech/kodus-ai/blob/main/libs/core/workflow/infrastructure/workflow-job-consumer.service.ts)
  - PR-Agent and Kodus never cancel a running review on push.
    [servers/utils.py](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/servers/utils.py)
  - pergent's SIGTERM path delivers a structured "cancelled" result, which only Greptile's CLI
    (exit 5) comes close to.
- **Ellipsis has a related rule:** a check for a non-head commit starts nothing.
  [triggers](https://www.ellipsis.dev/docs/agents/triggers)
  pergent.sh can apply this before creating a Job.
- **Timing gaps (inference from Kubernetes semantics):**
  - `activeDeadlineSeconds` counts from Job start. That includes scheduling, image pull and the
    init container's clone, so a slow clone eats pergent's 2-minute margin.
    - Fix: add the clone time to the margin, or bound the init container separately.
  - On deadline expiry, Kubernetes terminates the pod with SIGTERM, so pergent will report
    "cancelled" for a deadline kill too.
    - pergent.sh has to tell "superseded" from "deadline" using the Job's condition.
  - The HTTP retry schedule (2 + 4 + 8 s plus request time) has to fit into the 30 s
    `terminationGracePeriodSeconds` after a SIGTERM.
    - Fix: shorten retries in the SIGTERM path.

### 8. 10 MB result cap

**Verdict: reasonable. The field caps inputs, not results.**
- **Result-size limits in the field:**
  - Kodus's webhook body limit is 25 MB.
    [apps/webhooks/src/main.ts](https://github.com/kodustech/kodus-ai/blob/main/apps/webhooks/src/main.ts)
  - PR-Agent caps comments at 65,000 characters.
    [github_provider.py](https://github.com/qodo-ai/pr-agent/blob/main/pr_agent/git_providers/github_provider.py)
  - pergent's design keeps verdicts, findings and failures, and drops checks and then summaries
    with a note. That is safer than truncating.
- **Input caps in the field:**
  - CodeRabbit: 150 or 300 files.
    [plans](https://docs.coderabbit.ai/management/plans.md)
  - Cursor: 3M characters, skipped neutrally.
    [release notes](https://cursor.com/docs/release-notes/bugbot)
  - Graphite: 200k characters.
    [AI review comments](https://graphite.com/docs/ai-review-comments)
  - GitLab: 1 MiB, with an honest partial-review notice.
    [Code Review Flow](https://docs.gitlab.com/user/duo_agent_platform/flows/foundational_flows/code_review/)
  - Ellipsis: dollar budgets.
    [budgets](https://www.ellipsis.dev/docs/budgets)
  - pergent instead batches everything within a time budget and reports `not_reviewed`. That is
    more complete, but the cost of a review has no ceiling **(inference)**.

---

## (e) Gaps and ideas worth adopting

All of these are **inferences** drawn from the products named.

1. **Keep the git token out of the shared volume.** *(Kodus, PR-Agent, GitLab Duo)*
   - In the init container, pass the token through env-only `GIT_CONFIG_COUNT` and
     `http.extraHeader`, never in the remote URL or a credential file.
   - Set the push URL to an invalid value and `core.hooksPath=/dev/null`, as Kodus does.
   - Fetch submodules only if the base branch declares them.
   - Add a test that greps the finished `emptyDir` for the token.
2. **Add an egress NetworkPolicy and fail closed.** *(Copilot firewall, GitLab sandbox allowlist,
   Greptile TREX, Kodus SNI relay; the Kudelski recommendation)*
   - The review container should reach only the LLM endpoint (or proxy) and the result URL.
   - The init container should reach only the git host.
   - pergent.sh should refuse to start Jobs when the policy isn't enforced. Don't follow GitLab's
     silent unsandboxed fallback.
3. **Put an LLM proxy in front of the provider.** *(Greptile `llmproxy`, GitLab AI Gateway,
   Ellipsis)*
   - Give the Job a per-Job proxy credential that expires at the deadline, like the result token,
     instead of `OPENCODE_API_KEY`.
   - The proxy can also enforce a per-review token or dollar ceiling, which covers the missing
     cost cap from (d) 8.
4. **Harden the pod, and enforce it at admission time.** *(CodeRabbit microVM plus jail and its
   post-incident deploy-time sandbox enforcement; Cursor Firecracker; Kodus privilege drop)*
   - Use restricted Pod Security: `runAsNonRoot`, `allowPrivilegeEscalation: false`, drop all
     capabilities, read-only root filesystem.
   - Offer an optional gVisor or Kata `RuntimeClass`.
   - Run with no service-account token.
   - Use an admission policy that rejects review Jobs which don't meet this.
5. **Check the head again before posting or approving.** *(CodeRabbit, Greptile, Kodus, CodeAnt,
   Copilot)*
   - pergent.sh compares `result.head` with the live MR head before approving, and only comments
     on a stale result.
   - It dismisses its own approval on a new push.
   - It skips creating a Job for a non-head commit, as Ellipsis does.
6. **Skip pushes that don't change the patch, and point the model at new lines.** *(GitLab Duo
   `patch_id` and `since_last_review`, Kodus rebase interdiff)*
   - pergent.sh reuses the last result when the merge-base diff's patch-id is unchanged, for
     example after a rebase onto an unrelated target change.
   - Optionally, an agent-mode flag tells the model which hunks are new since the last reviewed
     head, while it still reviews the whole diff.
7. **Use the protected or default branch as the trusted source for approvals.** *(PR-Agent,
   GitLab, Ellipsis use the default branch; CodeAnt sends policy edits to a human)*
   - When the MR targets an unprotected branch, take skills, instructions and skip rules from the
     default branch, or never approve.
   - Send MRs that edit `.gitattributes` or pergent.sh's own config path to a human.
8. **Fix the timing of the deadline and the grace period.** *(design issue; compare Kodus's 25 s
   drain and Cursor's neutral cancellation)*
   - Bound the init container separately from `--timeout`.
   - Use a shorter HTTP retry schedule after SIGTERM.
   - pergent.sh distinguishes `DeadlineExceeded` from "superseded" using the Job condition, not
     from pergent's "cancelled" cause.
9. **Treat checkout content as untrusted in the prompt.** *(PR-Agent, Kodus, GitLab
   PromptSecurity, Cursor)*
   - Mark `AGENTS.md` and other checkout text the agent reads as untrusted.
   - Strip hidden Unicode and HTML comments from tool output where opencode allows it.
   - Extend the `*.env` read deny to `*.pem`, `*.key` and `*.tfvars`, as Bugbot does.
10. **Choose the commit-status conclusion on purpose.** *(CodeRabbit neutral by default, Cursor
    `neutral`, Sourcery "never blocks a merge", Kodus NEUTRAL on partial error)*
    - The design sets the status to **failed** on every error, which blocks merges whenever the
      LLM provider is down.
    - The field defaults to neutral and makes blocking opt-in.
    - pergent.sh could offer both, keeping "never passed" as the invariant.
