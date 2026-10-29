# Product surfaces of hosted and self-hosted AI code-review services

Research date: 2026-10-29. Question: what do the web dashboards, menus, settings, analytics,
admin features and self-hosted deployments of AI code-review services look like? This is input
for **pergent.sh**, a self-hosted frontend and backend that receives GitHub/GitLab webhooks and
triggers pergent review jobs. pergent itself is an open-source Go CLI that reviews an MR with an
LLM agent and posts one comment with per-skill verdicts, findings, summaries and checklist tables.

Sources are primary only: vendor docs, vendor changelogs, official Helm charts, compose files and
install guides, and source code in official repositories. Every claim carries the URL it came
from. Anything seen only on a marketing page or blog, or not found at all, is marked
**(unverified)** or **not found in docs**.

Some caveats about the sources:
- Pages were read through a fetch tool that returns a summarised rendering, not raw HTML. Check
  any exact UI label against the live page before copying it into a spec.
- Vendor docs move fast. Several features below are labelled beta, preview or research preview.
- Nothing was logged into, signed up for or submitted. The surfaces described are the ones that
  docs, changelogs and source code show.

Related note: [2026-10-23-auto-approve-prior-art.md](2026-10-23-auto-approve-prior-art.md)
covers bot approval in depth, so section 9 of each product here stays short.

## (a) Summary of findings

### The common pattern

Most products with a real dashboard converge on the same skeleton.

- **Onboarding.** On GitHub this is a GitHub App install, with "all repositories" or "selected
  repositories" chosen on GitHub's side. On GitLab it is a token plus a webhook: a PAT or service
  account with the `api` scope, and a webhook the vendor either creates or hands you as a URL and
  secret. Self-managed GitLab works the same way, plus an instance URL and often an instance OAuth
  app. Examples: CodeRabbit, Greptile, Qodo, Sourcery, Cursor Bugbot, Bito, CodeAnt, Kodus (all
  cited per product below).
- **A Repositories page** that enables or disables review per repo. This page is universal.
- **A settings hierarchy.** It runs org/workspace, then team/group, then repository, and some
  products add a directory level. Each level inherits, with an "Overridden" badge or a "Use
  organization settings" toggle, and a reset (CodeRabbit, Qodo, Kodus, Sourcery, Greptile).
  Settings are also expressible **as a file in the repo**, and in most products the file beats
  the UI.
- **A recurring set of review-behaviour knobs:**
  - auto-review on/off
  - review on every push or incrementally
  - review drafts (off by default)
  - base-branch, label, title-keyword and author filters
  - path include/exclude globs
  - path-scoped instructions
  - a strictness or profile setting (quiet/chill/assertive, Low/Medium/High, Lite/Balanced/Max)
  - review language
  - chat commands in PR comments (`@bot review`, `full review`, `pause`, `resolve`)
- **An analytics page.** The shared core metrics are PRs reviewed, findings or comments posted,
  **acceptance / addressed rate** (the headline metric nearly everywhere), findings by severity
  and category, time to merge, and per-repo and per-author breakdowns. Filters are time range,
  repo and author/team. CSV export is common (CodeRabbit, Qodo, Greptile, Bito).
- **A rules/learnings page.** Natural-language rules scoped by path globs, plus "learnings" mined
  from PR feedback (replies, 👍/👎, whether a comment was addressed), with admin edit/delete and
  often a **pending-approval queue** (CodeRabbit, Greptile, Qodo, Kodus).
- **Org admin pages:** members and roles (typically Admin / Member / Billing), SSO/SAML and SCIM
  (Enterprise tier), audit log (Enterprise tier), API keys, billing and usage.
- **Merge gating** through the provider's own primitives: a commit status/check run that branch
  protection can require, and opt-in Approve / Request-changes reviews. Nobody builds a separate
  merge API.

### What is rare

- **Per-run review history with status, timing and failure detail in the dashboard.** Only these
  products have it:
  - **Kodus**: PR list → expandable per-review timeline with status, duration, commit SHA,
    stages, failed files and tool calls.
  - **Ellipsis**: sessions with statuses and failure reasons.
  - **GitLab Duo**: AI → Sessions.
  - **CodeRabbit**: a read-only "Review Usage" event log.
  - **Copilot**: session logs linked from the PR.

  No product documents a **re-run button in the dashboard** except Greptile's in-app PR list
  ("Re-review"). Everyone else re-runs through PR comment commands.
- **A findings-centric table across all PRs** with a per-finding state (Pending / Resolved /
  Ignored / Dismissed / Outdated) and a dismiss-with-reason action. Qodo has this. Greptile's
  "Bugs Caught" tab and CodeRabbit's quality drill-down come close.
- **Showing where each resolved setting came from.** CodeRabbit's `@coderabbitai configuration`
  does this, and Kodus shows "Overridden" badges and file-applied status.
- **Self-hosted editions with a documented architecture.** Only Greptile, Qodo (on-prem), Kodus
  (open source) and GitLab Duo Self-Hosted document theirs. CodeRabbit's self-hosted edition is a
  single container, with install details "provided during onboarding". Sourcery and CodeAnt keep
  theirs behind sales or do not document one. Graphite, Cursor Bugbot and Ellipsis have none.

### What differentiates

- Graphite's **PR inbox** and Greptile's **in-app PR review UI**. Both turn the dashboard into a
  place to do review, not only to configure it.
- Qodo's **PR Triage** queue (priority, blast radius, SLA) and **Rule Miner**, which mines rules
  from accepted human review comments.
- CodeRabbit's **scheduled natural-language reports** (Slack/email/Teams) and its **pre-merge
  checks** (natural-language checks with off/warning/error modes).
- Greptile's and Qodo's **auto-deactivation / suggestion lifecycle for rules**, driven by
  addressed rate.
- Ellipsis's fully **config-as-code** model (`.ellipsis/code-review.yaml` with up to 64 named
  reviewers). Its UI is mostly sessions and billing.
- Where repo config is read from, which matters for security:
  - Cursor Bugbot reads settings from the **base** branch, so a PR cannot change its own settings.
  - Greptile reads its approval policy from the base branch.
  - Copilot reads instructions from the **head** branch.
  - Bito reads `.bito.yaml` from the head (source) branch.

## (b) Comparison table

| Product | Onboarding | Top-level menu (as documented) | Analytics | Review history in UI | Learnings UI | Self-hosted (how) | Source |
|---|---|---|---|---|---|---|---|
| CodeRabbit | GitHub App plus OAuth login. Self-managed GitLab: bot user and PAT, OAuth app, webhooks (manual or script); optional reverse tunnel | Dashboard (Summary, Quality/Time Metrics, Knowledge Base, Org Trends, Pre-merge Checks, Reporting, Data Metrics, Review Usage, Data Export); Reviews › Review settings; Learnings; Reports; Settings › Audit Logs; Subscription | Rich: acceptance by severity and category, time-to-merge percentiles, tool findings, CSV export, scheduled reports, Enterprise metrics API | Read-only Review Usage event log (per PR or timeline); no re-run | Yes: table, semantic search, edit/delete, pending approvals, API | Enterprise (500+ seats): single "CodeRabbit agent" container, BYO LLM | [dashboard](https://docs.coderabbit.ai/guides/dashboard.md), [self-hosted](https://docs.coderabbit.ai/self-hosted/overview.md) |
| Greptile | GitHub App. GitLab: PAT plus manually added webhook. GHES and self-managed GitLab on Enterprise | Analytics, Memory, Pull Requests, Settings (since 2026-07-24) | PRs reviewed, bugs caught by P0–P2, addressed rate, comment ratings, export | In-app PR list with Re-review; run status via MCP only | Memory › Custom rules: Active/Inactive/Suggested, addressed rate | Docker Compose, Helm or AWS Terraform; web/api/webhook/workers; Hatchet + RabbitMQ; Postgres+pgvector; LiteLLM proxy | [changelog](https://www.greptile.com/docs/changelog.md), [arch](https://www.greptile.com/docs/self-hosting/system-architecture.md) |
| Qodo | GitHub App from a portal wizard. GitLab: Owner PAT used once to create a service account; Qodo creates group hooks | Findings, Repositories, Review Standards, Configurations, Analytics, PR Triage, Account management | Impact / By repository / By PR author; acceptance rates; CSV export (10k rows); rule analytics | Findings table and PR Triage; no per-run log | Review Standards: generate/import/mined rules, Pending tab, lock, auto-deactivate | On-prem Helm on K8s 1.25+; Postgres+pgvector, Redis, RabbitMQ; OpenAI-compatible LLM. OSS PR-Agent (MIT) has no UI | [analytics](https://docs.qodo.ai/governance/code-review-analytics), [on-prem](https://docs.qodo.ai/on-prem/qodo-on-premise/infrastructure-requirements.md) |
| Graphite | GitHub App only; GHES via allowlisted static IPs (Enterprise) | PR inbox; AI reviews (Overview, Rules & exclusions); Insights; Settings (Mergeability checks) | Issues found/accepted, acceptance and downvote rates, per-rule stats; Insights (cycle times) | Per-PR status only (Running/Completed/Not running) | Rules and exclusions with per-rule metrics; edit/delete not documented | None | [ai-reviews-setup](https://graphite.com/docs/ai-reviews-setup), [GHES](https://graphite.com/docs/github-enterprise-server) |
| GitHub Copilot code review | Built in; enabled by policy; automatic review through branch rulesets | No dashboard; Settings › Copilot › Code review at repo/org; enterprise AI controls | Usage-metrics API/NDJSON (active/passive code-review users, PR totals) | PR timeline links to session logs; re-request button | None (doesn't learn) | Not offered (self-hosted Actions runners only) | [concepts](https://docs.github.com/en/copilot/concepts/agents/code-review), [config](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/request-a-code-review/configure-automatic-review) |
| GitLab Duo Code Review | Native; `@GitLabDuo` reviewer; agentic flow runs as a CI job on a runner as a service account | Settings › Merge requests › GitLab Duo Code Review (project/group); AI › Sessions | Duo and SDLC trends panels (requests, comments, sentiment, per-user); GraphQL | AI › Sessions | None (feedback doesn't carry over) | GitLab Duo Self-Hosted: your own AI Gateway plus models (vLLM, Bedrock, Azure) | [flow](https://docs.gitlab.com/user/duo_agent_platform/flows/foundational_flows/code_review/), [self-hosted](https://docs.gitlab.com/administration/gitlab_duo_self_hosted/) |
| Cursor Bugbot | GitHub App (incl. GHES). Self-hosted GitLab via instance OAuth app; Cursor creates webhooks | Automations › Bugbot (installations, per-repo toggles, coverage table, team rules) | Rule analytics (issues found, accepted, acceptance rate); admin API | Check run only; no run log | "Automatically Learn Rules", `@cursor remember` | None | [bugbot](https://cursor.com/docs/bugbot), [gitlab](https://cursor.com/docs/integrations/gitlab) |
| Bito | Cloud: Code Review › Repositories; self-managed GitLab PAT; per-repo toggle | Code Review › Repositories (agents); agent tabs Review / Custom Guidelines / Filters / Tools / Chat / Functional Validation; Learned Rules; Analytics | Overview, Submitter, Repository, PR Analytics; PDF/PPT export | PR Analytics lists reviewed and skipped PRs with skip reason | Learned Rules with status toggle | Docker agent (`bito-cra.sh`) in CLI or webhook-server mode | [analytics](https://docs.bito.ai/ai-code-reviews-in-git/code-review-analytics), [webhooks](https://docs.bito.ai/ai-code-reviews-in-git/install-run-as-a-self-hosted-service/install-run-via-webhooks-service) |
| CodeAnt AI | GitHub App. Self-managed GitLab: OAuth app plus token, then "Create Webhooks (N)" | Pull Requests, Control Center, Scan Center, Engineering Productivity, Pentesting, Cloud Security, IDE, CLI/MCP, Settings (from docs structure) | DORA and developer metrics; scan reports | Not found in docs | Settings › Learnings (view/edit/delete) | Not found in docs | [llms.txt](https://docs.codeant.ai/llms.txt), [learnings](https://docs.codeant.ai/pull_request/features/learnings.md) |
| Ellipsis | GitHub App only | Home (sessions), Code Review, Agents › Settings, Billing, API, Webhooks, Alerts | Spend and usage only | Sessions with status and failure reason; outbound webhooks | None (prompt/skills in YAML) | None | [code-review](https://www.ellipsis.dev/docs/code-review), [sessions](https://www.ellipsis.dev/docs/sessions/lifecycle) |
| Sourcery | GitHub App. Self-managed GitLab: admin PAT creates bot user and OAuth app; per-project webhook | Review Settings, Repo settings, Analytics, Members, Billing, API Keys, IDE, Integrations, Security findings | Team plan: Overview, PR Lifecycle, Code Reviews, Developers, Repositories, Trends | Not found in docs | Review rules (create only documented) | Enterprise, "contact sales" | [configure](https://docs.sourcery.ai/reviews/configure/), [self-hosted](https://docs.sourcery.ai/trust/self-hosted-sourcery/) |
| Kodus (OSS) | Self-hosted: token, GitHub App or OAuth; Kodus registers webhooks; self-managed GitLab toggle | Pull requests, Cockpit, Issues, Library, Code review settings, Git, Integrations, Plugins, BYOK, Token usage, User logs, Organization (members, SSO, CLI keys) | Cockpit: implementation rate, negative feedback, rule health, DORA-like (Enterprise) | **Yes**: per-PR expandable review timeline with stages and errors; no re-run | Kody Rules plus Memories, rules library, approval queue | AGPLv3 core plus EE; compose or Helm; web/api/webhooks/worker; RabbitMQ, Postgres (pgvector), MongoDB | [repo](https://github.com/kodustech/kodus-ai), [arch](https://docs.kodus.io/en/how_to_deploy/kodus_architecture.md) |
| AI-Codereview-Gitlab (OSS) | Webhook plus token in `.env` | Streamlit dashboard: Merge requests / Push tabs | Score and line charts per project and developer | Table of reviewed MRs with score; no status or re-run | None | One container: Flask API plus Streamlit plus SQLite | [repo](https://github.com/sunmh207/AI-Codereview-Gitlab) |

## (c) Per-product details

### CodeRabbit

**1. Onboarding**
- **GitHub:**
  - You log in with GitHub OAuth, pick an org, and install the GitHub App on "All repositories"
    or "Only select repositories".
  - The app gets read/write on checks, code, commit statuses, issues, PRs and workflows.
  - [github-com](https://docs.coderabbit.ai/platforms/github-com.md)
- **Self-managed GitLab (16.x+):**
  - You enter the instance URL. For the bot user and OAuth app you choose between automated
    setup, which uses a one-time admin token, and manual setup.
  - The PAT needs `api`, `read_api` and `read_user`.
  - Webhooks are added manually from the URL and secret CodeRabbit shows, or in bulk with a
    script.
  - Repos are then enabled per project or per group on the Repositories page.
  - [self-hosted-gitlab](https://docs.coderabbit.ai/platforms/self-hosted-gitlab.md)
- **GitLab with no inbound access:** a customer-run Reverse Tunnel Connector dials out over WSS.
  [reverse-tunnel](https://docs.coderabbit.ai/self-hosted/coderabbit-reverse-tunnel.md)

**2. Navigation and information architecture**
- **Dashboard sidebar:**
  - "Git platform reviews": Summary, Quality Metrics, Time Metrics, Knowledge Base, Organization
    Trends, Pre-merge Checks and Finishing Touches, Reporting, Data Metrics, Review Usage, Data
    Export.
  - "IDE/CLI reviews": three pages.
  - Global filters: Timeframe, Org Name (self-hosted only), Repository, Username, Team.
  - [dashboard](https://docs.coderabbit.ai/guides/dashboard.md)
- **Settings:** Reviews › Review settings lists "Organization defaults" and "Connected
  repositories". [repository-settings](https://docs.coderabbit.ai/guides/repository-settings.md)
- **Other pages:**
  - Learnings: `app.coderabbit.ai/learnings`
    ([learnings](https://docs.coderabbit.ai/knowledge-base/learnings.md))
  - Recurring reports: `app.coderabbit.ai/reports/recurring`
    ([scheduled-reports](https://docs.coderabbit.ai/guides/scheduled-reports.md))
  - Settings › Audit Logs ([audit-logs](https://docs.coderabbit.ai/management/audit-logs.md))
  - Subscription, where roles are changed ([roles](https://docs.coderabbit.ai/management/roles.md))

**3. Settings**
- **Org vs repo:** every repo has a "Use organization settings" toggle, and changes need "Apply
  changes". [organization-settings](https://docs.coderabbit.ai/guides/organization-settings.md)
- **Settings editor:**
  - Concise, All Settings and YAML Editor views. The YAML view shows only non-default values and
    is schema-validated.
  - A Cmd/Ctrl+K palette.
  - A **preview panel** that renders a mock review comment.
  - Bulk-edit across several repos.
  - [repository-settings](https://docs.coderabbit.ai/guides/repository-settings.md)
- **Precedence, highest first:**
  1. Workspace/org global overrides
  2. Repo `.coderabbit.yaml`
  3. Central `coderabbit` repo file
  4. Repo UI
  5. Org UI
  6. Workspace settings
  7. Defaults

  Sources do not merge unless `inheritance: true` is set, and `@coderabbitai configuration`
  shows the resolved config.
  [configuration-overview](https://docs.coderabbit.ai/guides/configuration-overview.md),
  [configuration-inheritance](https://docs.coderabbit.ai/configuration/configuration-inheritance.md)
- **Keys:**
  - `reviews.profile`: quiet / chill / assertive
  - `reviews.auto_review`: `enabled`, `drafts`, `base_branches`, `labels`,
    `ignore_title_keywords`, `auto_incremental_review`, `auto_pause_after_reviewed_commits`,
    `ignore_usernames`
  - `path_filters` and `path_instructions`
  - `reviews.tools.<linter>`: ruff, eslint, semgrep, gitleaks, golangci-lint, trivy and others
  - `language` and `tone_instructions`
  - [configuration](https://docs.coderabbit.ai/reference/configuration.md)
- **Chat commands:** `review`, `full review`, `pause`, `resume`, `resolve`, `approve`, `summary`,
  `configuration`, `run pre-merge checks` and more.
  [review-commands](https://docs.coderabbit.ai/reference/review-commands.md)
- **BYOK on SaaS:** not found in docs.

**4. Analytics**
- **Summary:**
  - Active repos, merged PRs, active users, reviewer time saved, review comments, acceptance rate
    and comments per PR.
  - Severity and category charts, tool findings (SAST/linter) and pipeline failures.
  - [dashboard-metrics](https://docs.coderabbit.ai/guides/dashboard-metrics.md)
- **Quality drill-down:**
  - Columns: PR, comment, repo, author, severity, category, accepted (Y/N), created.
  - A comment counts as "accepted" only when there is a verified fix.
  - [dashboard-metrics](https://docs.coderabbit.ai/guides/dashboard-metrics.md)
- **Time metrics:** time to merge, last commit, first and last human review, each as avg, median,
  P75 and P90. [dashboard-metrics](https://docs.coderabbit.ai/guides/dashboard-metrics.md)
- **Export and API:**
  - CSV export has one row per PR, with complexity, estimated review minutes, and comments posted
    and accepted by severity and category.
  - The metrics data API is Enterprise only.
  - [data-export](https://docs.coderabbit.ai/guides/data-export.md)
- **Scheduled reports:**
  - Weekly or monthly, filtered by repos, labels, users and teams.
  - Templates or a custom prompt.
  - Delivered by email, Slack, Discord or Teams.
  - [scheduled-reports](https://docs.coderabbit.ai/guides/scheduled-reports.md)

**5. Review history**
- **Review Usage** has "My usage" and "Team usage" tabs:
  - Cards: Review events, Rate limited, Pull Requests.
  - A daily activity chart.
  - A Review Log in "Pull requests" or "Timeline" view. Columns include outcome, review kind,
    files reviewed and limit.
  - At most 100 events load.
  - [review-usage-dashboard](https://docs.coderabbit.ai/management/review-usage-dashboard.md)
- No re-run in the dashboard and no per-run logs were found. Re-runs are done with PR commands.

**6. Learnings**
- **Creation:**
  - You reply to the bot, or tag `@coderabbitai`, with a preference, and it confirms with
    "Learnings Added".
  - Scope is `local`, `global` or `auto`.
  - An optional `approval_delay` holds new learnings as pending.
- **UI:**
  - KPI cards: Total, Active, Never Used, Created This Week.
  - Table columns: Learning, Usage, Last Used, Created, Updated.
  - Semantic search and filters by repo, path and user.
  - Admins can edit and delete; there is a Pending approvals tab.
- **API:** `GET/PATCH/DELETE /v1/learnings`.
- [learnings](https://docs.coderabbit.ai/knowledge-base/learnings.md)

**7. Teams, SSO, audit and retention**
- **Roles:** Admin, Member and Billing Admin, with a permission matrix. Custom roles are
  Enterprise only. [roles](https://docs.coderabbit.ai/management/roles.md)
- **SSO:** SAML 2.0 and SCIM (Okta, Entra), Enterprise only.
  [sso](https://docs.coderabbit.ai/management/sso/index.md)
- **Audit logs** (Enterprise):
  - Events: config, role, seat and API-key changes.
  - Columns: User, Action, Resource Summary, Time.
  - Export is through the API only.
  - [audit-logs](https://docs.coderabbit.ai/management/audit-logs.md)
- **Retention:** a data-retention toggle covers the cache, code index and learnings.
  [faq](https://docs.coderabbit.ai/faq.md)

**8. Self-hosted edition**
- **What the current docs say:**
  - Enterprise only, 500+ seats.
  - One "CodeRabbit agent" container that runs on a server, a container platform or serverless.
  - Supports GHES, GitLab self-managed, Azure DevOps and Bitbucket DC.
  - You bring your own LLM.
  - Install details are given during onboarding.
  - [self-hosted overview](https://docs.coderabbit.ai/self-hosted/overview.md)
- **An older preview deployment of the official docs** (historical, possibly outdated):
  - `docker run --env-file .env -p 8080:8080` with a `/health` endpoint and a webhook at
    `/gitlab_webhooks`.
  - `LLM_PROVIDER` takes openai, azure-openai, bedrock-anthropic or anthropic.
  - Other settings: `CODERABBIT_LICENSE_KEY`, `ENABLE_LEARNINGS`, `ENABLE_METRICS`.
  - No database or queue is documented.
  - [preview docs](https://068aee66.coderabbit-docs.pages.dev/self-hosted/gitlab)
- **Self-hosted config precedence:** repo YAML, then central YAML, then the `YAML_CONFIG` env var,
  then defaults. There is no UI.
  [configuration-inheritance](https://docs.coderabbit.ai/configuration/configuration-inheritance.md)

**9. Gating**
- **Pre-merge checks:**
  - Built-in checks plus custom natural-language ones.
  - Modes: off, warning or error.
  - "Ignore failed checks" override, and `@coderabbitai ignore pre-merge checks`.
  - [pre-merge-checks](https://docs.coderabbit.ai/pr-reviews/pre-merge-checks.md)
- **Configuration keys:**
  - `reviews.commit_status` and `fail_commit_status`.
  - `reviews.auto_approvals`, with risk ceilings.
  - [configuration](https://docs.coderabbit.ai/reference/configuration.md)

### Greptile

**1. Onboarding**
- **GitHub:** install the GitHub App, link the org, then enable repos.
  [quickstart](https://www.greptile.com/docs/quickstart.md)
- **GitLab:**
  - A service-account PAT with the `api` scope.
  - You manually add a webhook from the URL and secret Greptile shows (Comments, Issues, MR and
    Emoji events) and click "Done, I've made the changes".
  - [quickstart](https://www.greptile.com/docs/quickstart.md)
- **Self-managed GitLab and GHES:** Enterprise only.
  [code-providers](https://www.greptile.com/docs/integrations/code-providers.md)
- **Reverse proxies:** GitLab instances behind a reverse proxy are supported (2025-10-29).
  [changelog](https://www.greptile.com/docs/changelog.md)

**2. Navigation**
- **Top bar:** simplified on 2026-07-24 to four tabs: **Analytics, Memory, Pull Requests,
  Settings**. [changelog](https://www.greptile.com/docs/changelog.md)
- **Settings pages:**
  - Organization, People, Add/Remove Repos (with a Status column), Repo Settings (auto-enable new
    repos), Code Providers.
  - Code Review: Review Configuration, Comments, PR Summaries, Auto-approve, and the review-tier
    card.
  - [team-setup-basics](https://www.greptile.com/docs/account/team-setup-basics.md)
- **Pull Requests page:**
  - Open/Closed toggle, search, J/K keyboard navigation and a "Re-review" action.
  - The PR detail view has Approve, Merge, Close, Request changes and "Chat with PR".
  - [review-interface](https://www.greptile.com/docs/code-review/review-interface.md)

**3. Settings**
- **Automatic reviews:** Never, On PR opened, On new pushes or On all events.
- **Other controls:** a draft toggle, author filters and a Low/Medium/High strictness level.
- **Teams** inherit org settings, with "Inheritance & Sync".
- [controlling-nitpickiness](https://www.greptile.com/docs/code-review/controlling-nitpickiness.md)
- **Review tiers:**
  - Base (1 credit), Plus (3), Apex (10) and Auto.
  - `@greptileai review this at apex` raises the tier for one review.
  - [review-tiers](https://www.greptile.com/docs/code-review/review-tiers.md)
- **Config as code:**
  - A cascading `.greptile/` folder (`config.json`, `rules.md`, `files.json`). The closest config
    wins for settings, but rules from every level still apply.
  - Precedence: nested config, then root config, then dashboard.
  - [greptile-config](https://www.greptile.com/docs/code-review/greptile-config.md),
    [customization-overview](https://www.greptile.com/docs/code-review/customization-overview.md)
- **Config keys:** `strictness`, `commentTypes`, label/author/branch filters, `ignorePatterns`,
  `autoReview`, `statusCheck` and `rules[]`.
  [greptile-config-reference](https://www.greptile.com/docs/code-review/greptile-config-reference.md)

**4. Analytics**
- **Cards:** Total PRs, Total Reviews, Avg Merge Time and bugs caught.
- **Charts:** PRs Reviewed, Bugs Caught (P0/P1/P2), Avg Time to Merge, Top Contributors,
  Addressed Rate and Comment Ratings.
- **Filters:** teams, repos, authors and period.
- **Bugs Caught table:** Finding, Severity, PR, Status (Open/Addressed) and Date. An Export button
  is available.
- [analytics](https://www.greptile.com/docs/account/analytics.md)
- **Spend cap:** admins can set a dollar cap on extra review spend (2026-04-30).
  [changelog](https://www.greptile.com/docs/changelog.md)

**5. Review history**
- The Pull Requests page has a Re-review action.
  [review-interface](https://www.greptile.com/docs/code-review/review-interface.md)
- Run status (COMPLETED/FAILED/SKIPPED) is exposed through MCP `list_code_reviews`; no REST API
  is documented. [mcp/reports](https://www.greptile.com/docs/mcp/reports.md)
- The footer of the PR comment has a "Re-trigger Greptile" control (2026-03-25).
  [changelog](https://www.greptile.com/docs/changelog.md)

**6. Learnings**
- **Signals:** 👍/👎, replies, and whether comments were addressed in later commits.
  [memory-and-learning](https://www.greptile.com/docs/how-greptile-works/memory-and-learning.md)
- **Memory › Custom rules:**
  - Add Context as a Rule (text plus glob scope) or a File.
  - Columns: Usage, Addressed Rate, Status (Active/Inactive/Suggested).
  - Suggested rules appear after about 10 PRs, and admins approve them.
  - [custom-standards](https://www.greptile.com/docs/code-review/custom-standards.md)

**7. Admin**
- **Org roles:** Admin, Member, Viewer and custom (Enterprise). **Team roles:** Team Admin and
  Team Member. [team-setup-basics](https://www.greptile.com/docs/account/team-setup-basics.md)
- **SSO:** SAML with domain verification, plus SCIM with group-to-team mapping, Enterprise only.
  [sso-and-identity](https://www.greptile.com/docs/account/sso-and-identity.md)
- **Audit log and retention:** not found in docs.

**8. Self-hosted** (the best-documented commercial reference architecture)
- **Positioning:**
  - Air-gapped installs are possible, on annual contracts.
  - Any OpenAI-compatible API, or Bedrock.
  - Postgres with pgvector is required; Redis is recommended.
  - [self-hosted-greptile](https://www.greptile.com/docs/self-hosting/self-hosted-greptile.md)
- **Components:**
  - Services: web (3000), api (3001), auth (3002), webhook (3007) and saml-jackson.
  - Workers: indexer-chunker, indexer-summarizer, reviews, jobs and llmproxy.
  - The Hatchet workflow engine, with RabbitMQ and Postgres.
  - Flow: the webhook is validated and queued, Hatchet dispatches it, workers call LLMs through
    llmproxy, and results are stored in Postgres and posted to the provider.
  - [system-architecture](https://www.greptile.com/docs/self-hosting/system-architecture.md)
- **Deployment options and sizing:**
  - Docker Compose for up to about 100 developers.
  - AWS Terraform (EC2, RDS, ElastiCache).
  - Helm for more than 100 developers.
  - [deployment-options](https://www.greptile.com/docs/self-hosting/deployment-options.md)
- **Helm chart:**
  - Lives in `greptileai/akupara` and needs a separate `hatchet-stack` release.
  - The worker pod is privileged (SYS_ADMIN) to sandbox reviews.
  - The license comes from sales.
  - [kubernetes](https://www.greptile.com/docs/self-hosting/kubernetes.md)
- **LLM proxy:** `llmproxy-config.yaml` is a LiteLLM config whose task aliases (review,
  review-deep, review-light, embeddings) map to models.
  [llmproxy-config.yaml](https://github.com/greptileai/akupara/blob/main/deploy/docker-compose/llmproxy-config.yaml)
- **Feature flags** in `.env.example` include `ANALYTICS`, `BILLING='false'`, `ONPREM='true'` and
  `AUTO_APPROVE_ENABLED`.
  [.env.example](https://github.com/greptileai/akupara/blob/main/deploy/docker-compose/.env.example)

**9. Gating**
- `statusCheck` is on by default.
  [greptile-json-reference](https://www.greptile.com/docs/code-review/greptile-json-reference.md)
- **Auto-approve (beta):**
  - Requires a 5/5 confidence score plus a risk ceiling.
  - The policy is read from the base branch.
  - The stricter of the dashboard and the file wins.
  - [auto-approve-prs](https://www.greptile.com/docs/code-review/auto-approve-prs.md)

### Qodo (Qodo platform, and the open-source PR-Agent)

Naming: the old Qodo Merge docs now redirect to docs.qodo.ai/code-review, and the PR-Agent repo
is described as a "community-maintained legacy project".
[pr-agent](https://github.com/qodo-ai/pr-agent)

**1. Onboarding**
- **GitHub:** install the GitHub App from a portal wizard, then turn repos on or off on the
  Repositories page.
  [qodo-multi-tenant](https://docs.qodo.ai/install-qodo/github/qodo-multi-tenant)
- **GitLab:**
  - An Owner gives a PAT once. Qodo uses it to create a service account and then discards it.
  - Qodo creates group hooks.
  - [install-qodo-on-gitlab](https://docs.qodo.ai/install-qodo/gitlab/install-qodo-on-gitlab)
- **Self-managed GitLab:**
  - An instance admin PAT, or a manual service account.
  - Qodo creates the webhooks and starts PR-history indexing.
  - [on-prem gitlab](https://docs.qodo.ai/on-prem/git-integration/install/gitlab.md)
- **OSS PR-Agent:** runs as a GitHub Action, a self-run GitHub App or webhook server, or a GitLab
  webhook server at `/webhook`.
  [github install](https://raw.githubusercontent.com/The-PR-Agent/pr-agent/main/docs/docs/installation/github.md),
  [gitlab install](https://raw.githubusercontent.com/The-PR-Agent/pr-agent/main/docs/docs/installation/gitlab.md)

**2. Navigation.** The portal left nav, as seen across the docs: **Findings, Repositories, Review
Standards, Configurations, Analytics, PR Triage, Account management**.
- **Findings:**
  - Cards for critical findings, % resolved before merge and average per PR.
  - Table columns: Findings, PR, Repository, State (Pending/Resolved/Ignored/Dismissed/Outdated),
    Action level, Category, Type, PR author.
  - "Dismiss" takes a reason.
  - [findings-page](https://docs.qodo.ai/code-review/get-started/findings-page)
- **Repositories:** Installations, Repositories (health, indexing progress, review on/off) and
  Relationships. [repositories](https://docs.qodo.ai/governance/repositories)
- **PR Triage (research preview):** a queue with Priority, Blast radius, SLA, Time open and Next
  action. [pr-triage](https://docs.qodo.ai/governance/pr-triage)
- **Account management:** SSO, roles, restricting code reviews, and API keys.
  [administration](https://docs.qodo.ai/administration)

**3. Settings**
- **Configurations page:**
  - Tabs: Finding types, Display, Workflow, Context, Remediation, Prompts, Agentic toolbox.
  - Levels: Workspace, then Git organization, then Repository, with an "Overridden" badge, Reset,
    and "Revert to preset".
  - Portal settings have the **lowest** precedence. A repo wiki page, a repo root
    `.pr_agent.toml` and an org `pr-agent-settings` repo all override them.
  - [portal-configuration](https://docs.qodo.ai/configuration/portal-configuration),
    [configuration-file](https://docs.qodo.ai/configuration/configuration-file)
- **Triggers:**
  - Automatic on opened, reopened and ready-for-review.
  - Optional review of every push, or manual only.
  - `/agentic_review` re-runs a review and strikes through earlier findings.
  - [use-qodo-in-prs](https://docs.qodo.ai/code-review/use-qodo-in-prs)
- **OSS filters:** title, labels, authors, and source/target branches (regex), plus ignore globs.
  [additional_configurations](https://raw.githubusercontent.com/The-PR-Agent/pr-agent/main/docs/docs/usage-guide/additional_configurations.md)

**4. Analytics**
- **Code review analytics** (admins only):
  - Tabs: Impact, By repository, By PR author.
  - KPIs: PRs impacted, Accepted findings, High-severity acceptance rate, PR authors, Median time
    to merge.
  - Filters: time (up to 365 days), severity, category, type, repo, author.
  - The ⋯ menu has "Export CSV" (up to 10,000 findings) and "Share dashboard".
  - [code-review-analytics](https://docs.qodo.ai/governance/code-review-analytics)
- **Rule analytics:** Passed, Detected violations, Merged violations (CSV) and Deactivated.
  [rule analytics](https://docs.qodo.ai/governance/rule-enforcement/analytics)

**5. Review history.** A per-run log, status or re-run page was not found in docs. Findings and
PR Triage are the finding-centric and PR-centric views.

**6. Learnings and rules**
- **Review Standards:**
  - Rules are generated from a description, imported from files such as AGENTS.md, mined, or
    suggested by members (these go to a "Pending rules" tab).
  - Fields: severity, scope and status (Active, Active (locked), Deactivated by Qodo, Deactivated
    by user).
  - Qodo auto-deactivates rules whose findings are rarely fixed.
  - [generate-and-manage-rules](https://docs.qodo.ai/governance/rule-enforcement/generate-and-manage-rules)
- **Rule Miner:** mines accepted human review comments into up to 10 rules per repo, then up to 5
  every 2 weeks. [rule-miner](https://docs.qodo.ai/governance/rule-enforcement/rule-miner)

**7. Admin**
- **Roles:** Organization admin and member, Team owner and member.
  [roles](https://docs.qodo.ai/account-management/roles-and-permissions)
- **SSO:** SAML and OIDC through WorkOS, Enterprise.
  [sso](https://docs.qodo.ai/account-management/single-sign-on-sso)
- **Audit log:** not found in docs.

**8. Self-hosted**
- **Deployment models:** multi-tenant SaaS, single-tenant in your VPC, or on-prem.
  [deployment-models](https://docs.qodo.ai/deployment-models)
- **On-prem infrastructure:**
  - Kubernetes 1.25+ with Helm 3, or OpenShift.
  - External PostgreSQL 15/16 with pgvector.
  - Redis and RabbitMQ are bundled, and so is Zitadel for SSO.
  - Separate hostnames for portal, API, auth and webhooks.
  - An OpenAI-compatible LLM endpoint is required.
  - Indexer and code-review workers.
  - Images come through Replicated.
  - [infrastructure-requirements](https://docs.qodo.ai/on-prem/qodo-on-premise/infrastructure-requirements.md),
    [architecture](https://docs.qodo.ai/on-prem/qodo-on-premise/architecture.md),
    [prerequisites](https://docs.qodo.ai/on-prem/qodo-on-premise/critical-prerequisites.md)
- **OSS PR-Agent:** MIT licence, with no UI.
  [pr-agent](https://github.com/qodo-ai/pr-agent)

**9. Gating.** No true auto-approve key exists in the current reference.
`approve_pr_on_self_review` exists.
[config reference](https://docs.qodo.ai/configuration/configuration-and-command-reference)
The legacy `enable_auto_approval` page now redirects, so it is **(unverified)**.

### Graphite (Graphite Agent, formerly Diamond)

**1. Onboarding**
- **GitHub App only.** Org owners install it, and OAuth is no longer offered to new accounts.
  [authenticate-with-github-app](https://graphite.com/docs/authenticate-with-github-app)
- **Enabling AI reviews:** on the "AI code review settings" page, pick repos or the whole org.
  [ai-reviews-setup](https://graphite.com/docs/ai-reviews-setup)
- **GHES:** Enterprise only, through static IPs. There is no self-hosted version.
  [github-enterprise-server](https://graphite.com/docs/github-enterprise-server)
- **GitLab:** not found in docs.

**2. Navigation**
- **AI reviews:** Overview and "Rules & exclusions" tabs.
  [ai-reviews-setup](https://graphite.com/docs/ai-reviews-setup)
- **PR inbox:**
  - Sections: Needs your review, Approved, Returned to you, Merging and recently merged, Drafts,
    Waiting for review.
  - You can add custom filter sections.
  - [use-pr-inbox](https://graphite.com/docs/use-pr-inbox)
- **Insights** (beta). [insights](https://graphite.com/docs/insights)
- **Settings › Mergeability checks.**
  [mergeability-status-check](https://graphite.com/docs/mergeability-status-check)

**3. Settings**
- **Exclusions** are free-text statements of where not to comment.
- **Custom rules** are either UI prompts (rule, bad example, good example, reasoning) or
  file-based globs such as `.github/rules/*.md`.
- [ai-review-customization](https://graphite.com/docs/ai-review-customization)

**4. Analytics**
- **Overview:**
  - Metrics: issues found by category, issues accepted, acceptance rate over time, PRs reviewed,
    downvote rate.
  - Time filters: 4, 8 or 12 weeks, or all time.
  - [ai-review-comments](https://graphite.com/docs/ai-review-comments)
- **Per-rule and per-exclusion stats.**
  [ai-review-customization](https://graphite.com/docs/ai-review-customization)
- **Insights:** median publish-to-merge, review response time, review cycles, and saved views.
  [insights](https://graphite.com/docs/insights)

**5. Review history.** Only a per-PR status (Running/Completed/Not running).
[ai-review-comments](https://graphite.com/docs/ai-review-comments)

**6. Learnings.** "Improves based on how your team responds"
([ai-reviews](https://graphite.com/docs/ai-reviews)). There is no learnings editor in the docs.

**7. Admin**
- **Roles:** admin and member; admins can make members read-only.
  [graphite-admin](https://graphite.com/docs/graphite-admin)
- **Enterprise plan:** SSO, audit log (SIEM) and GHES. The column placement comes from a
  flattened table, so it is **(unverified)**.
  [billing-plans](https://graphite.com/docs/billing-plans)

**8. Self-hosted.** None. [github-enterprise-server](https://graphite.com/docs/github-enterprise-server)

**9. Gating**
- **Merge queue** (stack-aware). [merge-queue](https://graphite.com/docs/graphite-merge-queue)
- **Merge When Ready.** [merge-when-ready](https://graphite.com/docs/merge-when-ready)
- **Mergeability status check.**
  [mergeability-status-check](https://graphite.com/docs/mergeability-status-check)
- **AI-review blocking:** not found in docs.

### GitHub Copilot code review

**1. Onboarding.**
- There is nothing to install: Copilot is a built-in reviewer that org and enterprise owners
  enable by policy.
  [concepts](https://docs.github.com/en/copilot/concepts/agents/code-review)
- You request a review from the Reviewers list, from `gh pr edit --add-reviewer @copilot`, or
  through the REST API.
  [use-code-review](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/request-a-code-review/use-code-review)

**2–3. Settings surfaces**
- **Repo, Settings › Copilot › Code review:**
  - Review effort level (Lite/Balanced/Max) and maximum effort.
  - Auto-approval, with optional path globs.
  - "Only allow … triggered by authorized users".
- **Org:**
  - Default and maximum effort.
  - Billing mode.
  - "Count Copilot approvals toward merge requirements".
- **Automatic review** is set in **branch rulesets** ("Automatically request Copilot code
  review", with "Review new pushes" and "Review draft pull requests"). Org rulesets target repos
  by pattern.
- [configure-automatic-review](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/request-a-code-review/configure-automatic-review)
- **Instructions:**
  - `.github/copilot-instructions.md`, plus `.github/instructions/**/*.instructions.md` with
    `applyTo` globs and `excludeAgent`.
  - All matching sets are sent to the model.
  - [add-repository-instructions](https://docs.github.com/en/copilot/how-tos/configure-custom-instructions/add-repository-instructions)
- **Model:** "Model switching is not supported". Agentic context gathering runs on Actions runners.
  [concepts](https://docs.github.com/en/copilot/concepts/agents/code-review)

**4. Analytics.** The usage-metrics API exposes active and passive code-review users and
`pull_requests` totals (created, reviewed, merged, median minutes to merge), as NDJSON reports.
[usage-metrics](https://docs.github.com/en/copilot/reference/copilot-usage-metrics/copilot-usage-metrics)

**5. History.** The PR timeline links to session logs. Re-run is the re-request button.
[use-code-review](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/request-a-code-review/use-code-review)

**6. Learnings.** None. Copilot "may repeat comments you dismissed or downvoted".
[use-code-review](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/request-a-code-review/use-code-review)

**7. Audit.** Audit log events are found with `action:copilot` and kept for 180 days.
[review-audit-logs](https://docs.github.com/en/copilot/how-tos/administer-copilot/manage-for-enterprise/review-audit-logs)

**8. Self-hosted.** Not offered.

**9. Gating.**
- Copilot leaves a "Comment" review by default.
  [use-code-review](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/request-a-code-review/use-code-review)
- Optional approvals can count toward required approvals.
  [configure-automatic-review](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/request-a-code-review/configure-automatic-review)

### GitLab Duo Code Review

**1. Onboarding.**
- Duo is native: you assign `@GitLabDuo` or use `/assign_reviewer @GitLabDuo`.
- The agentic Code Review Flow runs as a **CI/CD job on a runner** (tagged `gitlab--duo`, or a
  GitLab-hosted runner) under a service account.
- [code_review flow](https://docs.gitlab.com/user/duo_agent_platform/flows/foundational_flows/code_review/)

**2–3. Settings**
- **Project, Settings › Merge requests › GitLab Duo Code Review:**
  - "Enable automatic reviews by GitLab Duo"
  - "Start a new review on push"
  - "GitLab Duo can approve and request changes"
- **Group** has the same section, with inheritance.
- **Exclusion rules:** `.gitlab/duo/mr-review-automated-rules.yaml` excludes target branches,
  source branches and authors.
- [code_review flow](https://docs.gitlab.com/user/duo_agent_platform/flows/foundational_flows/code_review/)
- **Custom instructions:**
  - `.gitlab/duo/mr-review-instructions.yaml` holds `{name, fileFilters, instructions}`.
  - Group and instance levels point at a template project.
  - Instructions from all levels are combined.
  - [review_instructions](https://docs.gitlab.com/user/duo_agent_platform/customize/review_instructions/)

**4. Analytics.**
- **Explore › GitLab Intelligence › GitLab Duo and SDLC trends:**
  - Code Review requests and comments.
  - Requests by role.
  - Comment sentiment from 👍/👎.
  - Usage by user.
  - GraphQL `AiMetrics`.
  - [duo_and_sdlc_trends](https://docs.gitlab.com/user/analytics/duo_and_sdlc_trends/)
- **DAP Impact** (beta). [dap_impact_dashboard](https://docs.gitlab.com/user/analytics/dap_impact_dashboard/)

**5. History.** Sessions are listed under **AI › Sessions**.
[code_review flow](https://docs.gitlab.com/user/duo_agent_platform/flows/foundational_flows/code_review/)

**6. Learnings.** None. "Feedback provided to GitLab Duo does not influence later reviews".
[duo_in_merge_requests](https://docs.gitlab.com/user/project/merge_requests/duo_in_merge_requests/)

**7. Data.** Inputs and outputs are not retained without consent, and an opt-in setting logs
prompts. [data_usage](https://docs.gitlab.com/user/gitlab_duo/data_usage/)

**8. Self-hosted.**
- **GitLab Duo Self-Hosted:** you run the AI Gateway and models yourself (vLLM, Bedrock, Azure
  OpenAI), and each feature is routed to it.
- Requires the Duo Enterprise add-on on Premium or Ultimate.
- Code Review is GA on self-hosted from 18.3.
- [gitlab_duo_self_hosted](https://docs.gitlab.com/administration/gitlab_duo_self_hosted/)

**9. Gating.** In beta, behind the `duo_code_review_decisions` flag:
- With no findings, Duo approves. That approval does not count toward required approvals.
- With findings, Duo requests changes, which blocks the merge.
- [code_review flow](https://docs.gitlab.com/user/duo_agent_platform/flows/foundational_flows/code_review/)

### Cursor Bugbot

**1. Onboarding**
- **GitHub App:** install from Integrations, with all or selected repos. GHES is supported.
  [github](https://cursor.com/docs/integrations/github)
- **Self-hosted GitLab:**
  - Create an instance OAuth app (scopes `api` and `write_repository`). Cursor creates the
    webhooks itself.
  - Networking: an IP allowlist, PrivateLink or Cloudflare Tunnel.
  - [gitlab](https://cursor.com/docs/integrations/gitlab)

**2. Navigation.** Bugbot lives under **Automations**: installations, per-repo toggles, a coverage
table and Team Rules. [bugbot](https://cursor.com/docs/bugbot),
[release-notes](https://cursor.com/docs/release-notes/bugbot)

**3. Settings**
- **Personal:** "Run only when mentioned", "Run only once" and "Enable reviews on draft PRs".
- **Team:** reviewer allow/deny lists, plus a switch for public repos.
- **Other settings:** incremental review, PR summaries, and Low/Default/High/Smart effort.
- **Repo files:**
  - `.cursor/config/bugbot.yaml` is read from the **base branch**.
  - `.cursor/BUGBOT.md` holds rules, and nested files apply too.
- [bugbot](https://cursor.com/docs/bugbot)

**4. Analytics.** Rule analytics (issues found, PRs reviewed, accepted, acceptance rate) and an
admin API (`GET /analytics/team/bugbot-reviews`, `POST /bugbot/review`).
[bugbot](https://cursor.com/docs/bugbot)

**5. History.** A `Cursor Bugbot` check run (success, neutral or failure). A run log was not found
in docs. [bugbot](https://cursor.com/docs/bugbot)

**6. Learnings.** "Automatically Learn Rules" learns from merged and closed PRs, and
`@cursor remember` adds a fact. [bugbot](https://cursor.com/docs/bugbot)

**7–8.** No self-hosted version. SSO and audit details were not found in the Bugbot docs.
[help](https://cursor.com/help/integrations/github-gitlab)

**9. Gating.** The check conclusion is `failure` only when configured. Bugbot does not approve.
[bugbot](https://cursor.com/docs/bugbot)

### Bito AI Code Review Agent

**1. Onboarding.**
- **Bito Cloud, self-managed GitLab:**
  - Go to CODE REVIEW › Repositories, enter the URL and a Maintainer PAT, then **Validate**.
  - Pick a group, then enable repos with toggles in the "Code Review Status" column.
  - [guide-for-gitlab-self-managed](https://docs.bito.ai/ai-code-reviews-in-git/install-run-using-bito-cloud/guide-for-gitlab-self-managed)
- Manual webhook setups are no longer supported for new configurations (January 2025).
  [changelog](https://docs.bito.ai/changelog/previous-releases)

**2–3. Navigation and settings.**
- **Agent settings tabs:**
  - Review: language, Essential/Comprehensive mode, automatic and incremental review, batch time,
    drafts, "Request changes comments", "Allow config file settings".
  - Custom Guidelines.
  - Filters: files and folders, branches, labels.
  - Tools: secret scanner.
  - Chat.
  - Functional Validation.
  - [create-or-customize-an-agent-instance](https://docs.bito.ai/ai-code-reviews-in-git/install-run-using-bito-cloud/create-or-customize-an-agent-instance)
- **Repo `.bito.yaml`:**
  - Overrides the agent settings and is read from the source branch.
  - An invalid value rejects the whole file.
  - [repo-level-settings](https://docs.bito.ai/ai-code-reviews-in-git/agent-settings/repo-level-settings)

**4. Analytics.**
- Views: Overview, Submitter, Repository, and PR Analytics (Reviewed with feedback, Reviewed
  without feedback, Skipped, with skip reason).
- Filters: submitter, date and PR status.
- Export to PDF or PowerPoint.
- [code-review-analytics](https://docs.bito.ai/ai-code-reviews-in-git/code-review-analytics)

**5. History.** PR Analytics lists reviewed and skipped PRs. Self-hosted mode has only
`bito-cra.sh service status`.
[webhooks service](https://docs.bito.ai/ai-code-reviews-in-git/install-run-as-a-self-hosted-service/install-run-via-webhooks-service)

**6. Learnings.** Negative feedback creates a disabled rule, which auto-enables after 3 negative
reactions. Rules can be toggled on the Learned Rules page.
[implementing-custom-code-review-rules](https://docs.bito.ai/ai-code-reviews-in-git/implementing-custom-code-review-rules)

**7. Admin.** SSO, roles and audit log were not found in docs.

**8. Self-hosted.**
- **Host and modes:**
  - A Docker container driven by `bito-cra.sh`, on a host with 4 CPU, 8 GB RAM and 80 GB disk.
  - Modes: CLI, webhook server or GitHub Actions.
  - Requires a Bito Access Key and a Git PAT.
  - [prerequisites](https://docs.bito.ai/ai-code-reviews-in-git/install-run-as-a-self-hosted-service/prerequisites),
    [self-hosted](https://docs.bito.ai/ai-code-reviews-in-git/install-run-as-a-self-hosted-service)
- **`bito-cra.properties`:**
  - `mode=cli|server` and `server_port`.
  - `git.domain`, for self-hosted Git.
  - `exclude_draft_pr`.
  - `post_as_request_changes`.
  - [bito-cra.properties](https://docs.bito.ai/ai-code-reviews-in-git/install-run-as-a-self-hosted-service/agent-configuration-bito-cra.properties-file)
- **Webhook endpoints:** `/api/v1/github_webhooks` and `/api/v1/gitlab_webhooks`.
  [webhooks service](https://docs.bito.ai/ai-code-reviews-in-git/install-run-as-a-self-hosted-service/install-run-via-webhooks-service)
- **LLM:** where it runs was not found in docs.

**9. Gating.** An opt-in "Request changes" review.
[bito-cra.properties](https://docs.bito.ai/ai-code-reviews-in-git/install-run-as-a-self-hosted-service/agent-configuration-bito-cra.properties-file)

### CodeAnt AI

**1. Onboarding.**
- **GitHub:** install the GitHub App. [github](https://docs.codeant.ai/setup/github.md)
- **Self-managed GitLab:**
  - Create an OAuth app and enter the base URL.
  - Settings › Configure Token, then "Save & Configure Webhooks".
  - Choose projects, then "Create Webhooks (N)".
  - [gitlab self-hosted](https://docs.codeant.ai/setup/gitlab/self-hosted/control_center.md)

**2. Navigation.**
- **From the docs structure:**
  - Pull Requests
  - Control Center (integrations, CI, coverage, quality gates)
  - Scan Center
  - Engineering Productivity (DORA, Dev Metrics)
  - Pentesting
  - Cloud Security
  - IDE
  - CLI/MCP
  - Settings (Teams, Access Control, API Tokens)
- [llms.txt](https://docs.codeant.ai/llms.txt)
- The page-level navigation was not found in docs.

**3. Settings.**
- **Global Repo Configuration:** one repo's `.codeant/` folder is merged into every repo, and repo
  values win.
  [global_repo_config](https://docs.codeant.ai/pull_request/customize/global_repo_config.md)
- **Precedence:** CI params, then `.codeant/configuration.json`, then UI.
  [analysis_configuration](https://docs.codeant.ai/repositories/analysis_configuration.md)

**4. Analytics.** A DORA dashboard (Team, Date Range and Repository filters, with drill-downs) and
developer metrics. [dora_metrics](https://docs.codeant.ai/engineering_productivity/dora_metrics.md),
[dev_metrics](https://docs.codeant.ai/engineering_productivity/dev_metrics.md)

**5. History.** Not found in docs.

**6. Learnings.** Reply to a comment ("This is not an issue because…"), then manage the result at
Settings › Learnings (view, edit, delete).
[learnings](https://docs.codeant.ai/pull_request/features/learnings.md)

**7. Admin.** Preset roles (Member, CI/CD, Admin), custom roles, Directory Sync and Settings ›
Audit Logs. [access-control](https://docs.codeant.ai/settings/access-control.md)

**8. Self-hosted.** Not found in docs or on the pricing page.
[pricing](https://www.codeant.ai/pricing)

**9. Gating.**
- **Quality gates:** metric, operator, value and scope, reported as a failing status check.
  [quality_gates](https://docs.codeant.ai/pull_request/quality_gates/setup.md)
- **Auto Approve (GitHub):** Off, Resolved comments, or Policy agent (Shadow or Enforce).
  [auto_approve_pr](https://docs.codeant.ai/pull_request/features/auto_approve_pr.md)

### Ellipsis

Ellipsis is now a general agent platform. Code review is one YAML-configured feature of it.

**1. Onboarding.** GitHub App only. [github](https://www.ellipsis.dev/docs/integrations/github)

**2. Navigation.**
- **Areas:** Home ("New session"), Code Review (every review with its comments and session),
  Agents › Settings, Billing (Limits/Credits/Usage), API, Webhooks, Alerts.
- [code-review](https://www.ellipsis.dev/docs/code-review),
  [organization](https://www.ellipsis.dev/docs/organization),
  [pricing](https://www.ellipsis.dev/docs/pricing)

**3. Settings.**
- **`.ellipsis/code-review.yaml`:**
  - PR filters: repos, base, draft, authors.
  - 1–64 named `reviewers`, each with `paths` globs, a harness (`claude_code`/`codex`), a
    `prompt` and a `model`.
  - `output` and `budget` blocks.
  - An org-wide file lives in the `.ellipsis` repo; a repo's own file wins.
  - [schema](https://www.ellipsis.dev/docs/code-review/schema)

**4. Analytics.** Spend and usage only. [pricing](https://www.ellipsis.dev/docs/pricing)

**5. History.**
- **Sessions:**
  - Statuses: queued, preparing, running, idle, closed.
  - Each message ends as completed, failed, stopped or cancelled, with `error.reason` (for example
    `budget_hit` or `tool_call_failed`).
  - [lifecycle](https://www.ellipsis.dev/docs/sessions/lifecycle)
- **Outbound webhooks:** HMAC-signed, with a 30-day delivery log.
  [webhooks](https://www.ellipsis.dev/docs/webhooks)

**6. Learnings.** Not found in docs.

**7. Retention.** Session TTL is set by the customer, from 0 hours to 1 year.
[security](https://www.ellipsis.dev/docs/security)

**8. Self-hosted.** Not found in docs.

**9. Gating.** `output.on_pass` (silent, comment or approve) and `on_fail` (comment or
request_changes). [code-review](https://www.ellipsis.dev/docs/code-review)

### Sourcery

**1. Onboarding.**
- **GitHub App** (all or selected repos).
  [quickstart-github](https://docs.sourcery.ai/start/quickstart-github/)
- **Self-managed GitLab:**
  - An admin PAT creates a `sourcery-ai` user and an instance OAuth app.
  - Projects are enabled one by one, and a per-project webhook is installed.
  - [quickstart-gitlab-self-hosted](https://docs.sourcery.ai/start/quickstart-gitlab-self-hosted/)

**2. Navigation.** Review Settings, Repo settings, Analytics, Members, Billing, API Keys, IDE,
Integrations, Security findings. [llms-full](https://docs.sourcery.ai/llms-full.txt)

**3. Settings.**
- **Org vs repo:** org Review Settings, plus a repo "Use Organization Settings" toggle.
- **Review profile:** Quiet, Balanced or Verbose.
- **Filters:** title keywords, labels, base-branch regex, drafts and path globs.
- **Language.**
- [configure](https://docs.sourcery.ai/reviews/configure/)
- **Commands:** `@sourcery-ai review`, `summary`, `guide`, `resolve`, `dismiss`.
  [commands](https://docs.sourcery.ai/reviews/commands/)
- **BYOK:** Enterprise, through sales.
  [bring-your-own-llm](https://docs.sourcery.ai/trust/bring-your-own-llm/)

**4. Analytics.** Team plan only: Overview, PR Lifecycle, Code Reviews (Sourcery vs humans),
Developers, Repositories, Trends. [analytics](https://docs.sourcery.ai/reviews/analytics/)

**5. History.** Not found in docs.

**6. Rules.** Path globs plus text, up to 3,000 characters.
[review-rules](https://docs.sourcery.ai/reviews/review-rules/)

**7. Admin.** Admin and member roles, and seats on the Members page.
[seats-and-team](https://docs.sourcery.ai/admin/seats-and-team/)
Retention policy: [code-retention](https://docs.sourcery.ai/trust/code-retention/).

**8. Self-hosted.** Enterprise only, and no technical details are public.
[self-hosted-sourcery](https://docs.sourcery.ai/trust/self-hosted-sourcery/)

**9. Gating.** A "Sourcery review" check (In progress, Success, Failure, Skipped) that can be
required in branch protection. It optionally approves.
[anatomy-of-a-review](https://docs.sourcery.ai/reviews/anatomy-of-a-review/),
[github-status-checks](https://docs.sourcery.ai/security/github-status-checks/)

### Kodus (open source, self-hostable)

**1. Onboarding**
- **Self-hosted:**
  - Run `generate-secrets.sh` and `install.sh`, then open `:3000`.
  - Create an account and add an LLM key.
  - Connect Git. Kodus registers the webhooks itself.
  - [kodus-installer](https://github.com/kodustech/kodus-installer)
- **GitLab PAT:**
  - Scopes `api`, `read_api`, `read_user`, `read_repository`, `write_repository`.
  - Self-managed GitLab: a "Self-hosted" toggle plus the instance URL.
  - [gitlab_pat](https://docs.kodus.io/en/how_to_use/code_review/general_config/gitlab_pat.md)

**2. Navigation** (from the Next.js routes in the source)
- **Top-level app routes:** `byok`, `choose-plan`, `cli-reviews`, `cockpit`, `issues`, `library`,
  `organization`, `pull-requests`, `review-suggestions`, `settings`, `token-usage`, `user-logs`.
  [(app) routes](https://github.com/kodustech/kodus-ai/tree/main/apps/web/src/app/(app))
- **`settings/`:** `code-review/[repositoryId]`, `git`, `integrations`, `plugins`, `subscription`.
  [settings](https://github.com/kodustech/kodus-ai/tree/main/apps/web/src/app/(app)/settings)
- **`organization/`:** `cli-keys`, `cockpit`, `general`, `notifications`, `sso`.
  [organization](https://github.com/kodustech/kodus-ai/tree/main/apps/web/src/app/(app)/organization)
- **Pull requests table:**
  - Columns: Pull request, Reviews, Suggestions (delivered vs held back), Status.
  - Filters: repository, authors, suggestions, date range.
  - [pr-data-table.tsx](https://raw.githubusercontent.com/kodustech/kodus-ai/main/apps/web/src/app/(app)/pull-requests/_components/pr-data-table.tsx),
    [pull-requests-filters.tsx](https://raw.githubusercontent.com/kodustech/kodus-ai/main/apps/web/src/app/(app)/pull-requests/_components/pull-requests-filters.tsx)

**3. Settings**
- **Levels:** Global, then Repository, then Directory, with per-field inheritance. A scope picker
  and "Overridden" badges with undo show the effect.
  [inheritance_overrides](https://docs.kodus.io/en/how_to_use/code_review/configs/inheritance_overrides.md)
- **General settings:**
  - Review Cadence: Automatic, Auto-pause or Manual.
  - Drafts.
  - Ignored files, with a "Test a file" box.
  - Base branches.
  - Ignored title keywords.
  - Auto-approve.
  - Request Changes.
- **`kodus-config.yml`:**
  - Opt-in per repo (`kodusConfigFileOverridesWebPreferences`).
  - Merged on top of the web settings.
  - The sidebar shows whether it was "applied", "not found" or "unavailable".
- [general](https://docs.kodus.io/en/how_to_use/code_review/configs/general.md)
- **BYOK page** (Owner only):
  - Providers tab with Test & save.
  - Routing tab: main model, fallback model, per-agent overrides.
  - Budget alerts.
  - [byok](https://docs.kodus.io/en/how_to_use/byok.md)
- **Commands:** `@kody start-review`. Status is shown as emoji reactions on the PR (🚀, 🎉, 👀,
  😕). [flow](https://docs.kodus.io/en/how_to_use/code_review/flow.md)

**4. Analytics (Cockpit).**
- **Tabs:**
  - Kodus Review: implementation rate by severity and category, negative feedback, rule health.
  - Productivity: deploy frequency, lead time, PR cycle time, PR size.
  - Issues.
- **Filters:** repo and date range.
- **License:** on self-hosted, Cockpit needs an Enterprise license and an Analytics Worker.
- [cockpit](https://docs.kodus.io/en/how_to_use/cockpit/overview.md),
  [analytics_worker](https://docs.kodus.io/en/how_to_deploy/deploy_kodus/analytics_worker.md)

**5. Review history** (the closest prior art to pergent's run model). Each PR row expands into a
per-review timeline:
- Statuses: "Success", "Review failed" (error in a tooltip), "In Progress", "Partial Error",
  "Skipped", "Pending".
- Review fields: duration, origin ("Automatic" or "User Command") and commit SHA.
- Notices for reduced context and provider fallback.
- Stages, each with message, status and duration.
- Failed files, file timings and an agent tool-call summary.
- "Open full review".
- There is **no re-run action**.
- [pr-list-item.tsx](https://raw.githubusercontent.com/kodustech/kodus-ai/main/apps/web/src/app/(app)/pull-requests/_components/pr-list-item.tsx)

**6. Learnings.** Kody Rules come in two kinds:
- **Review Rules:** path globs, severity, examples.
- **Memories:** created from `@kody` comments, scoped to directory, repo or global.

There is also a rules library and an optional approval queue for AI-generated items.
[kody_rules](https://docs.kodus.io/en/how_to_use/code_review/configs/kody_rules.md)

**7. Admin.**
- **Roles:** Owner, Billing Manager, Repository Admin and Contributor.
  [workspace_roles](https://docs.kodus.io/en/how_to_use/workspace_roles.md)
- **SSO:** SAML. [sso](https://docs.kodus.io/en/how_to_use/security/sso.md)
- **Enterprise features:** SSO, RBAC and audit logs.
  [pricing](https://docs.kodus.io/en/how_to_use/pricing.md)

**8. Self-hosted.**
- **Code and license:**
  - Monorepo: `apps/api` (NestJS), `apps/web` (Next.js), `apps/worker`, `apps/webhooks`, and
    `libs/llm`.
  - License: AGPLv3 plus `license_ee.md`.
  - [kodus-ai](https://github.com/kodustech/kodus-ai)
- **Compose services:**
  - `kodus-web`, `api`, `worker`, `webhooks`.
  - Optional `worker-analytics`.
  - `rabbitmq`, `postgres` (pgvector), `mongodb`.
  - [kodus-installer](https://github.com/kodustech/kodus-installer)
- **Flow:** webhooks, then API, then RabbitMQ, then worker.
  [kodus_architecture](https://docs.kodus.io/en/how_to_deploy/kodus_architecture.md)
- **Workers:** the role is chosen with `WORKER_ROLE=code-review`, and the LLM with env vars such
  as `API_LLM_PROVIDER_MODEL`.
  [generic_vm](https://docs.kodus.io/en/how_to_deploy/deploy_kodus/generic_vm.md)
- **Helm:** each store can be bundled, external or operator-managed.
  [kubernetes](https://docs.kodus.io/en/how_to_deploy/deploy_kodus/kubernetes.md)
- **Plans:**
  - Community: free, with caps.
  - Teams: $10 per developer per month, cloud only.
  - Enterprise: custom.
  - [pricing](https://docs.kodus.io/en/how_to_use/pricing.md)

**9. Gating.** Request Changes on critical issues, and conditional auto-approve. Status-check
gating is not described. [policy](https://docs.kodus.io/en/how_to_use/code_review/policy.md)

### Other notable: AI-Codereview-Gitlab (open source)

A small self-hosted reviewer that is close to pergent's shape: a webhook server plus a dashboard.
- **Onboarding:** a GitLab webhook to `:5001/review/webhook` (Push and MR events). Tokens are set
  in `.env`, and GitHub and Gitea are also supported.
  [repo](https://github.com/sunmh207/AI-Codereview-Gitlab),
  [.env.dist](https://raw.githubusercontent.com/sunmh207/AI-Codereview-Gitlab/main/conf/.env.dist)
- **Dashboard (Streamlit, `:5002`):**
  - A single login from env vars.
  - Merge requests and Push tabs, filtered by date, developer and project.
  - Columns: project, developer, branches, updated, commit message, ± lines, score (0–100).
  - Charts per project and per developer.
  - [ui.py](https://raw.githubusercontent.com/sunmh207/AI-Codereview-Gitlab/main/ui.py)
- **Storage:** SQLite tables `mr_review_log` and `push_review_log`. No status or re-run.
  [review_service.py](https://raw.githubusercontent.com/sunmh207/AI-Codereview-Gitlab/main/biz/service/review_service.py)
- **Deployment:** one compose service with no queue and no worker.
  [docker-compose.yml](https://raw.githubusercontent.com/sunmh207/AI-Codereview-Gitlab/main/docker-compose.yml)

Not covered beyond this: OSS PR-Agent, because it has no UI (it is noted under Qodo).

## (d) Common information architecture (synthesis)

**This section is synthesis, not a vendor's menu.** It merges the documented menus above into one
tree. Each item lists the products it comes from.

```
Overview / Analytics            CodeRabbit (Summary), Greptile (Analytics), Qodo (Analytics),
│                               Graphite (Overview), Bito (Overview), Kodus (Cockpit), Sourcery
├─ Impact: PRs reviewed, findings posted, acceptance/addressed rate, time to merge
│                               all of the above
├─ Breakdowns: by repo, by author/team, by severity, by category
│                               Qodo, CodeRabbit, Greptile, Bito
├─ Usage & cost: review events, rate-limited, tokens/credits, spend caps
│                               CodeRabbit (Review Usage), Kodus (token-usage), Greptile, Ellipsis
└─ Export (CSV) / scheduled reports
                                CodeRabbit, Qodo, Greptile, Bito (PDF/PPT)
Pull requests / Reviews         Kodus, Greptile, Qodo (PR Triage), Graphite (inbox), Ellipsis
├─ PR list: repo, author, #reviews, findings, latest status
│                               Kodus, Greptile
└─ PR detail → review runs: status, origin (auto/command), commit, duration, stages, errors
                                Kodus, Ellipsis (sessions), GitLab Duo (AI › Sessions)
Findings                        Qodo (Findings), Greptile (Bugs Caught), CodeRabbit (Quality drill-down)
└─ Table: finding, severity, category, PR, repo, state (open/resolved/dismissed), dismiss reason
Rules & Learnings               CodeRabbit (Learnings), Greptile (Memory), Qodo (Review Standards),
│                               Kodus (Kody Rules), Bito (Learned Rules), CodeAnt, Graphite, Sourcery
├─ Rules: text + path globs + severity + status (active/suggested/pending/locked)
├─ Learnings from feedback, with pending-approval queue
└─ Per-rule usage / acceptance stats   Qodo, Greptile, Graphite, CodeRabbit
Repositories                    every product with a dashboard
├─ Installations / Git connections (GitHub App, GitLab token + webhook, self-managed URL)
└─ Repo list: enabled toggle, connection health, indexing status
Configuration                   CodeRabbit, Qodo, Kodus, Sourcery, Greptile, Bito
├─ Scope picker: org → (team/group) → repo → (directory), "overridden" badges, reset
├─ Review triggers: auto, on push/incremental, drafts, branch/label/author/title filters
├─ Paths: include/exclude globs, path instructions
├─ Profile/strictness, language, output sections
├─ Model / BYOK / routing / budget   Kodus (BYOK page), Greptile self-hosted (llmproxy)
├─ Merge gating: status check, request changes, auto-approve policy
│                               CodeRabbit, Greptile, Sourcery, CodeAnt, Kodus, Copilot, GitLab Duo
└─ Config-as-code status: which file applied, resolved-value provenance
                                CodeRabbit (`configuration` command), Kodus (applied badge)
Integrations                    CodeRabbit (Slack/Jira/Linear), CodeAnt, Kodus, Sourcery (Jira)
Organization / Admin
├─ Members & roles (Admin / Member / Billing / Repo admin / Viewer)
│                               CodeRabbit, Greptile, Kodus, Qodo, CodeAnt, Graphite, Sourcery
├─ SSO (SAML/OIDC), SCIM          CodeRabbit, Greptile, Qodo, Kodus (all Enterprise-tier)
├─ Audit log                      CodeRabbit, CodeAnt, Kodus (EE), Copilot (GitHub audit log)
├─ API keys / CLI keys            Qodo, Sourcery, CodeAnt, Kodus
└─ Billing / usage limits         most SaaS products; omitted in self-hosted (Greptile BILLING='false')
```

## (e) Implications for pergent.sh (inferences)

All bullets below are **inferences** drawn from the findings. None is a vendor claim.

- **Inference: the review-run history page is pergent.sh's clearest differentiator.** Only Kodus
  and Ellipsis show per-run status, stages and failure reasons, and almost no one offers re-run
  from the dashboard. pergent already models each run as `completed` or `failed` with a
  `Cause`, per-batch results, and per-skill verdicts. A PR → review → skill → batch drill-down,
  with a "Re-run review" button, would surface data that other tools leave in logs.
- **Inference: copy the shared skeleton and skip the long tail.** The pages every product shares
  are Repositories (connect and enable), Configuration (org → repo with a file override), Pull
  requests / Reviews, Analytics (reviewed, findings by severity, acceptance rate) and Org admin
  (members, API keys). SSO, SCIM, audit logs, PR inboxes and DORA metrics are Enterprise-tier or
  out of scope for a first version.
- **Inference: copy the self-hosted shape that Greptile and Kodus document.** That shape is a
  separate webhook receiver that validates and enqueues, a queue, workers that spawn the review,
  Postgres, and the web/API. Because pergent spawns `opencode` per batch and enforces its own
  concurrency and budget, a worker can simply run the `pergent` binary as a subprocess or as a
  Kubernetes Job. Postgres alone, using its own queue table, may be enough before adding RabbitMQ
  or Redis. Plan for compose first and Helm second, as both Greptile and Kodus do.
- **Inference: make the precedence between UI and repo config explicit and visible.** Vendors
  disagree. In CodeRabbit, Qodo and Greptile the file beats the UI; in Kodus the file is opt-in;
  Bugbot reads from the base branch for safety. pergent skills are already files, so the safest
  default is to read config and skills from the **base** branch, and to show in the UI which
  source applied each value, as Kodus's "applied" badge and CodeRabbit's `configuration` command
  do.
- **Inference: build gating on provider primitives.** pergent's per-skill `has_blocking_issues`
  verdict maps directly onto a commit status or check, either one per skill or one aggregate,
  that branch protection can require. This is how Sourcery, Greptile, CodeAnt and Bugbot work.
  Approve / request-changes should be a separate opt-in policy (see the 2026-10-23 note).
- **Inference: findings need a state to make acceptance-rate analytics possible.** Every
  analytics page leads with acceptance or addressed rate, which requires tracking whether each
  finding was fixed. pergent's stable finding fingerprints and its `--previous-review`
  hidden-block parsing are a natural basis for "open / resolved / outdated" across runs. This is
  also what Qodo's Findings table is built on.
- **Inference: GitLab onboarding should be token plus webhook, with the server creating the
  webhooks.** Qodo, Kodus, Cursor and CodeAnt all create webhooks automatically from a PAT or
  service-account token, and manual setup is being retired (Bito). Self-managed GitLab only needs
  a base URL field. On GitHub, a GitHub App is the norm. A PAT-plus-webhook mode, like Bito's
  self-hosted agent and AI-Codereview-Gitlab, is a simpler first step.
- **Inference: learnings can wait, but leave room for them.** Feedback-driven learnings are
  common, but they always come with pending queues, edit/delete and usage stats, so they are a
  significant subsystem. pergent's skills are already the rules layer. A first version can list
  the skills per repo, with path scopes and per-skill verdict and acceptance stats, before
  attempting automatic learnings.
