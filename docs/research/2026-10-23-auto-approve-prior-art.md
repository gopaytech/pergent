# Auto-approve prior art: how AI review bots approve PRs/MRs

Research date: 2026-10-23. Question: how do commercial AI code-review products let their bot
**approve** a GitHub PR / GitLab MR, and how do they decide when? Input for a pergent feature
that would approve an MR when the review finds no issues.

Sources are primary only: vendor docs, vendor changelogs, vendor source code, and GitHub/GitLab
platform docs. Source code was read at pinned commits:
`qodo-ai/pr-agent@f5ffd0e` (2026-10-23) and `anthropics/claude-code-action@ae68417` (2026-10-22).
Anything taken from a search-result snippet or a secondary source, or not found at all, is marked
**(unverified)**. Vendor docs change often; several of these features are labelled beta or
public preview.

## Summary

- **Most tools that approve make it opt-in, off by default, and admin-controlled.** This holds for
  Copilot (enterprise > org > repo), GitLab Duo (feature flag plus a project/group/instance
  setting), Greptile, CodeRabbit, CodeAnt, Macroscope, Cursor, Ellipsis, and the old Qodo Merge
  flags. No tool found approves by default.
- **Three approval signals are common:**
  1. **Zero findings, or zero blocking findings**, from the bot's own review. GitLab Duo ("nothing
     to comment on"), Ellipsis (no blocking comment), Sourcery (nothing "worth blocking on"),
     Qodo Merge's legacy `auto_approve_for_no_suggestions`.
  2. **All of the bot's own threads resolved**, with the latest commit reviewed and checks passing.
     CodeRabbit `request_changes_workflow`, CodeAnt "Resolved comments", Macroscope's Correctness
     criterion.
  3. **A separate risk or eligibility judgement on top of the findings.** Greptile (5/5 confidence
     plus a risk ceiling), Cursor (risk score plus `APPROVAL_POLICY.md`), CodeAnt policy agent (risk
     plus ≥80% confidence), Macroscope (eligibility agent), Copilot (an "approval assessment" whose
     criteria are not documented).
- **The newer products put the most effort into safeguards outside the LLM.** These include:
  path allow/deny globs (Copilot, Greptile, Cursor, CodeAnt, Macroscope); reading the approval
  policy from the **base branch**, so a PR cannot loosen its own policy (Greptile, Cursor, CodeAnt;
  CodeRabbit for `allow_author_approval`); pinning the head SHA at approval time (CodeRabbit,
  CodeAnt, the GitLab `sha` param); fail-closed limits (Ellipsis, CodeAnt); and dismissing the
  approval on new commits (Copilot, Greptile, CodeAnt).
- **Whether the approval counts toward required reviews varies.** Copilot and Sourcery say theirs
  can count. CodeAnt says it counts toward N approvals but not CODEOWNERS. GitLab Duo's
  approval explicitly does **not** count, though its *request changes* does block. Greptile says
  required human approvals are still enforced.
- **Several tools deliberately do not approve.** Claude Code Review ("never approves or blocks"),
  Claude Code GitHub Action (blocked "for security reasons"), the open-source PR-Agent ("Won't
  approve PRs"), Graphite Agent, and Cursor Bugbot itself only comment. Bito requests changes but
  no approve setting was found.
- **"Request changes" is rarer than approve, and stickier.** On GitHub a write-access reviewer's
  request-changes blocks merge until that same reviewer approves. GitLab Duo's request-changes has
  the same blocking effect as a human's.

## Comparison table

| Tool | Approves? | Approval signal | Opt-in config | Safeguards | Source |
|---|---|---|---|---|---|
| PR-Agent (OSS) | No (code exists but is unreachable) | n/a | `config.enable_auto_approval` (default false; its caller is commented out) | Approval keys blocked as CLI args and in per-directory config | [config](https://github.com/qodo-ai/pr-agent/blob/f5ffd0ef8c7c36f797e0ee3b762c83006247956a/pr_agent/settings/configuration.toml#L33), [FAQ](https://github.com/qodo-ai/pr-agent/blob/f5ffd0ef8c7c36f797e0ee3b762c83006247956a/docs/docs/faq/index.md#L20) |
| Qodo Merge (legacy v1 docs) | Yes | Review-effort ≤ X; or no code suggestions; or `/review auto_approve` comment; or author ticks self-review box | `enable_auto_approval`, `auto_approve_for_low_review_effort`, `auto_approve_for_no_suggestions`, `enable_comment_approval`, `approve_pr_on_self_review` | Off by default; "cannot be set as command line arguments, only in the configuration file" | [Qodo v1 improve docs](https://docs.qodo.ai/v1/tools/tools-list/improve) |
| Qodo (current product) | Not documented | n/a | n/a | n/a | [docs.qodo.ai/code-review](https://docs.qodo.ai/code-review) |
| CodeRabbit | Yes; also requests changes | Its comments resolved, latest commit reviewed, no failing pre-merge checks | `reviews.request_changes_workflow` (default false); `reviews.allow_author_approval` (default true) | Head-SHA check before approving; branch config can't re-enable author approval | [workflow](https://docs.coderabbit.ai/pr-reviews/request-changes-workflow), [config ref](https://docs.coderabbit.ai/reference/configuration) |
| GitHub Copilot code review | Yes (public preview); otherwise Comment only | "Approval assessment" (criteria undocumented) | Repo: "Allow Copilot to approve pull requests" + "…count toward merge requirements"; org/enterprise policies; enterprise default "Disabled everywhere" | Up to 15 file globs (every changed file must match); approval dismissed on new commits; Copilot-authored PRs need a non-Copilot approval | [concepts](https://docs.github.com/en/copilot/concepts/agents/code-review), [configure](https://docs.github.com/en/copilot/how-tos/copilot-on-github/set-up-copilot/configure-code-review), [changelog](https://github.blog/changelog/2026-09-01-copilot-code-review-can-now-approve-pull-requests/) |
| GitLab Duo (Code Review Flow) | Yes (beta); also requests changes | Approves when "nothing to comment on"; requests changes otherwise | Flag `duo_code_review_decisions` + "GitLab Duo can approve and request changes" (project/group/instance) | Approval does **not** count toward required approvals; follow-up review on push | [Code Review Flow](https://docs.gitlab.com/user/duo_agent_platform/flows/foundational_flows/code_review/) |
| Greptile | Yes (beta) | Clean 5/5 confidence score and risk ≤ ceiling | `autoApprove.enabled`, `autoApprove.riskCeiling` (default `"low"`), `autoApprove.filters.{includePaths,excludePaths}` + dashboard toggle | Policy read from base branch; stricter of dashboard/file wins; `do-not-merge`/`manual-review` labels block; dismissed on new commits | [auto-approve](https://www.greptile.com/docs/code-review/auto-approve-prs) |
| Cursor Bugbot | No, comments only (status check `neutral` by default) | n/a | n/a | n/a | [Bugbot](https://cursor.com/docs/bugbot) |
| Cursor PR Routing & Approval | Yes (GitHub/Origin only) | Risk score ≤ max, `APPROVAL_POLICY.md` files, Bugbot/Security findings | "Automatically Approve PRs", "Maximum Risk for Approval", "Use Bugbot Review Context", … | Base-branch policy when PR edits policy; no approval if Bugbot check times out (~8 min) | [approval agents](https://cursor.com/docs/approval-agents) |
| Sourcery | Yes | "nothing it found is worth blocking on and the change doesn't need a person's judgement" | "Let Sourcery approve pull requests" | Re-review cap of 5 per PR | [anatomy of a review](https://docs.sourcery.ai/reviews/anatomy-of-a-review/) |
| Ellipsis | Configurable | No blocking comment from any reviewer | `.ellipsis/code-review.yaml` `output.on_pass: approve`, `output.on_fail: request_changes` (default: pass silent, fail comment) | Never approves on early stop or budget exhaustion; own PRs get a comment | [code review](https://www.ellipsis.dev/docs/features/code-review) |
| Bito | No approve found; can post "Request changes" | n/a | "Request changes" agent option | n/a | [changelog](https://docs.bito.ai/changelog/previous-releases) |
| Graphite Agent | No approve found; comments only | n/a | n/a | n/a | [AI reviews setup](https://graphite.com/docs/ai-reviews-setup) |
| Claude Code Review (managed) | No; "never approves or blocks" | n/a | n/a | Check run never concludes as failure | [Code Review](https://code.claude.com/docs/en/code-review) |
| Claude Code GitHub Action | No; blocked "for security reasons" | n/a | n/a | Inline-comment MCP server without review tool; separate human-approval gate action | [capabilities](https://github.com/anthropics/claude-code-action/blob/ae68417a745a5df4b2124873632bd73a2255e6f6/docs/capabilities-and-limitations.md#L16-L19) |
| CodeAnt AI | Yes (GitHub only) | All CodeAnt threads resolved; optional policy agent (risk, ≥80% confidence) | Settings → AI Code Review → "Auto Approve PR": Off (default) / Resolved comments / Policy agent; `.codeant/approval.json` | Head SHA pinned; base-branch policy; PR editing policy never auto-approved; fail-closed over 300 files / 60k patch chars; shadow mode | [auto approve](https://docs.codeant.ai/pull_request/features/auto_approve_pr) |
| Macroscope Approvability | Yes (GitHub) | Eligibility agent + no unresolved correctness comments ≥ Minimum Blocking Severity (+ optional CODEOWNERS ownership) | Off by default; `.macroscope/approvability.md` (`neverApprove`, `alwaysApprove`, `onlyApprove`, `conclusion`, …) | Any edit to approval file holds the PR; can't approve own PRs; Preview Mode ("Would Approve") | [approvability](https://docs.macroscope.com/approvability) |

## Per-tool details

### Qodo Merge / PR-Agent

**Open-source PR-Agent (current master): approval code exists but is not reachable.**
- `enable_auto_approval` defaults to false. The config comment says the `/review` caller "is currently
  commented out" ([configuration.toml L33](https://github.com/qodo-ai/pr-agent/blob/f5ffd0ef8c7c36f797e0ee3b762c83006247956a/pr_agent/settings/configuration.toml#L33)).
  The call site is indeed commented out ([pr_reviewer.py L328-L331](https://github.com/qodo-ai/pr-agent/blob/f5ffd0ef8c7c36f797e0ee3b762c83006247956a/pr_agent/tools/pr_reviewer.py#L328-L331)).
  `auto_approve_logic()` only checks the flag, then calls the provider
  ([pr_reviewer.py L1798-L1809](https://github.com/qodo-ai/pr-agent/blob/f5ffd0ef8c7c36f797e0ee3b762c83006247956a/pr_agent/tools/pr_reviewer.py#L1798-L1809)).
- The GitHub provider's `auto_approve()` is a bare `create_review(event="APPROVE")`
  ([github_provider.py L2246-L2254](https://github.com/qodo-ai/pr-agent/blob/f5ffd0ef8c7c36f797e0ee3b762c83006247956a/pr_agent/git_providers/github_provider.py#L2246-L2254)).
  The base provider returns `False`, and no GitLab override was found
  ([git_provider.py L1280-L1281](https://github.com/qodo-ai/pr-agent/blob/f5ffd0ef8c7c36f797e0ee3b762c83006247956a/pr_agent/git_providers/git_provider.py#L1280-L1281)).
- `approve_pr_on_self_review` is documented as "no effect … PR-Agent does not approve PRs"
  ([configuration.toml L293](https://github.com/qodo-ai/pr-agent/blob/f5ffd0ef8c7c36f797e0ee3b762c83006247956a/pr_agent/settings/configuration.toml#L293)).
  The improve docs say the self-review checkbox is "a visual marker only"
  ([improve.mdx L339-L342](https://github.com/qodo-ai/pr-agent/blob/f5ffd0ef8c7c36f797e0ee3b762c83006247956a/docs/docs/tools/improve.mdx#L339-L342)).
- The FAQ lists "Won't approve PRs; approval remains reviewer's responsibility" as a built-in
  safeguard ([faq/index.md L20](https://github.com/qodo-ai/pr-agent/blob/f5ffd0ef8c7c36f797e0ee3b762c83006247956a/docs/docs/faq/index.md#L20)).
- **Gating, even though the feature is inert:**
  - The approval toggles are rejected as CLI arguments
    ([test_cli_args_security.py L42-L46](https://github.com/qodo-ai/pr-agent/blob/f5ffd0ef8c7c36f797e0ee3b762c83006247956a/tests/unittest/test_cli_args_security.py#L42-L46)).
  - The self-review approval keys are "root-controlled", so that a nested `.pr_agent.toml` cannot
    "make /improve demand a checklist and then auto-approve the pull request when the author ticks
    it" ([config_security.py L155-L157, L186](https://github.com/qodo-ai/pr-agent/blob/f5ffd0ef8c7c36f797e0ee3b762c83006247956a/pr_agent/config_security.py#L155-L186)).
  - The docs warn never to set the config branch from PR-derived input, because that would let an
    attacker "enabl[e] auto-approval of their own PR"
    ([configuration_options.md L70](https://github.com/qodo-ai/pr-agent/blob/f5ffd0ef8c7c36f797e0ee3b762c83006247956a/docs/docs/usage-guide/configuration_options.md#L70)).
- **Structured verdict, without approval.** `pr_reviewer.require_merge_recommendation` (default
  false) asks the model for one of `no_concerns_found | needs_review | changes_required`
  ([configuration.toml L184](https://github.com/qodo-ai/pr-agent/blob/f5ffd0ef8c7c36f797e0ee3b762c83006247956a/pr_agent/settings/configuration.toml#L184)).
  The docs say "The value reports the model's findings, not a guarantee about the code"
  ([review.md L204-L205](https://github.com/qodo-ai/pr-agent/blob/f5ffd0ef8c7c36f797e0ee3b762c83006247956a/docs/docs/tools/review.md#L204-L205)).
  - When a large PR is split into chunks, the merged verdict is the worst value across chunks
    ([review_merge.py L449](https://github.com/qodo-ai/pr-agent/blob/f5ffd0ef8c7c36f797e0ee3b762c83006247956a/pr_agent/algo/review_merge.py#L449)).
  - The docs put it this way: "a clean chunk cannot … soften a risk level set by another chunk"
    ([review.md L359-L366](https://github.com/qodo-ai/pr-agent/blob/f5ffd0ef8c7c36f797e0ee3b762c83006247956a/docs/docs/tools/review.md#L359-L366)).

**Qodo Merge (paid, legacy v1 docs at [docs.qodo.ai/v1/tools/tools-list/improve](https://docs.qodo.ai/v1/tools/tools-list/improve)):**
- Platforms: GitHub, GitLab, Bitbucket. "To ensure safety, the auto-approval feature is disabled by
  default."
- `[config] enable_comment_approval`: a `/review auto_approve` comment approves the PR and posts a
  comment.
- `[config] enable_auto_approval` with one of two triggers:
  - `auto_approve_for_low_review_effort = X`: approve "When the review effort score is lower or
    equal to X".
  - `auto_approve_for_no_suggestions`: approve "When no code suggestion were found for the PR".
- `[pr_code_suggestions] approve_pr_on_self_review`: approves when the author ticks the self-review
  box. Combined with 2 required approvals, this forces an author self-review.
- "the approval flags cannot be set as command line arguments, only in the configuration file."
- An `ensure_ticket_compliance` condition appears only in a search snippet of the old
  qodo-merge-docs page, which now redirects to [docs.qodo.ai/code-review](https://docs.qodo.ai/code-review)
  **(unverified)**.
- The current Qodo docs do not mention approval at all ([docs.qodo.ai/code-review](https://docs.qodo.ai/code-review)).

### CodeRabbit

Source: [request changes workflow](https://docs.coderabbit.ai/pr-reviews/request-changes-workflow)
and the [configuration reference](https://docs.coderabbit.ai/reference/configuration).

- **Behaviour.** `reviews.request_changes_workflow` (default `false`) submits a **request-changes**
  review when a review posts actionable inline comments. On the next review it resolves the threads
  that the push addressed.
- **Approval rule.** "Automatically approve when CodeRabbit's comments are resolved, the latest
  commit has been reviewed, and no pre-merge checks are failing."
- **GitLab.** "Note: In GitLab, all discussions must be resolved."
  - If resolving a discussion triggers the check, CodeRabbit waits until GitLab reports all
    blocking discussions resolved.
  - If a clean review triggers it, CodeRabbit checks only discussions it created.
- **Head SHA.** Immediately before approving, CodeRabbit verifies that the PR HEAD hasn't changed.
  Approval stays pending if HEAD is not among the commits it reviewed.
- **Overrides.**
  - `@coderabbitai approve` and `@coderabbitai resolve` can approve without a completed review or
    passing pre-merge checks. The HEAD check still applies.
  - `reviews.allow_author_approval` (default `true`) controls whether the PR author may use them.
    "The pull request's branch configuration cannot re-enable this setting."
- **Pre-merge checks.**
  - `reviews.pre_merge_checks.*.mode` set to `error` "can block the PR until the check passes" when
    the workflow is enabled.
  - `reviews.pre_merge_checks.override_requested_reviewers_only` stops authors from overriding
    failing checks.
- **Not documented on those pages:** whether CodeRabbit's approval counts toward branch protection,
  and dismissal of approvals on new commits.

### GitHub Copilot code review

- **Default.** "By default, Copilot leaves a "Comment" review, not an "Approve" review or a "Request
  changes" review" ([using Copilot code review](https://docs.github.com/en/copilot/how-tos/copilot-on-github/use-copilot-agents/copilot-code-review)).
  No request-changes mode is documented.
- **Assessment.** "Every Copilot code review includes an approval assessment in the overview
  comment, indicating whether Copilot has determined the pull request ready to approve"
  ([concepts](https://docs.github.com/en/copilot/concepts/agents/code-review)).
  - The criteria behind the assessment are **not documented**.
  - "An approval assessment alone does not count toward merge requirements"
    ([changelog 2026-09-01](https://github.blog/changelog/2026-09-01-copilot-code-review-can-now-approve-pull-requests/)).
- **Approvals (public preview).** When enabled, Copilot "can submit an approving review that
  satisfies your repository's required-approval rule … the same way a teammate's approval would."
  "If new commits are pushed after Copilot approves, the approval is dismissed"
  ([concepts](https://docs.github.com/en/copilot/concepts/agents/code-review)).
- **Config** ([configure code review](https://docs.github.com/en/copilot/how-tos/copilot-on-github/set-up-copilot/configure-code-review)):
  - Repo: Settings → Copilot → Code review → "Auto-approval". Two toggles: "Allow Copilot to approve
    pull requests" and "Allow Copilot approvals to count toward merge requirements".
  - Repo "File paths": up to 15 globs. Approvals count only where "every changed file matches one
    of the globs".
  - Org: "Count Copilot approvals toward merge requirements" (enabled everywhere / let repositories
    decide / selected repos / disabled).
  - Enterprise: "Allow Copilot to approve pull requests". The default is "Disabled everywhere".
- **Copilot-authored PRs.** A ruleset rule requires extra human approval: "at least one approval
  must cover the last push and come from someone other than Copilot"
  ([available rules for rulesets](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/available-rules-for-rulesets)).
- **Stated limits.** "Copilot is not guaranteed to spot all problems". The docs advise to
  "Supplement Copilot's feedback with a human review"
  ([concepts](https://docs.github.com/en/copilot/concepts/agents/code-review)).

### GitLab Duo Code Review / Code Review Flow

Source: [Code Review Flow](https://docs.gitlab.com/user/duo_agent_platform/flows/foundational_flows/code_review/).

- **Status.** "Introduced as a beta in GitLab 19.5 with a feature flag named
  `duo_code_review_decisions`. Disabled by default."
- **Rule.** "When GitLab Duo finds nothing to comment on, it approves the merge request. When GitLab
  Duo finds something to comment on, it requests changes."
- **Approval does not count:** "An approval from GitLab Duo does not count toward required
  approvals."
- **Request changes does block.** "If a user's request for changes blocks a merge request on your
  project, a request from GitLab Duo has the same effect." It persists "until GitLab Duo approves in
  a follow-up review" or a user bypasses it.
- **Enabling.** The setting is "GitLab Duo can approve and request changes":
  - Project: Maintainer/Owner, under Settings → Merge requests.
  - Group: Owner.
  - Instance: admin.
  - "Settings cascade from instance to group to project."
- **Re-review.** "Start a new review on push" (on by default) starts a follow-up review when a
  push changes the diff. A diff-neutral rebase does not trigger one. An in-flight review is
  cancelled and restarted.
- **Bot identity.** Code Review Flow "runs as a service account". Its permissions are not described.
- **Legacy feature.** The older Duo Code Review page does not mention approval
  ([Duo in MRs](https://docs.gitlab.com/user/project/merge_requests/duo_in_merge_requests/)).
- **Status of the change itself.** A GitLab MR titled "Allow Duo code review to approve or request
  changes" ([!254981](https://gitlab.com/gitlab-org/gitlab/-/merge_requests/254981)) appeared in
  search results. Its merge state was not checked **(unverified)**.

### Greptile

Source: [auto-approve PRs](https://www.greptile.com/docs/code-review/auto-approve-prs). The feature
is in beta and covers GitHub, GitLab, Bitbucket Cloud, and Gitea.

- **Signal.** A clean **5/5 confidence score** plus assessed risk ≤ the configured cap.
  - Risk levels: Low (docs/tests/small), Medium, High (deps/build config/core modules), Critical
    (auth, secrets, billing, migrations, infra/CI, public APIs).
  - Risk is judged from what the code does, not from its path.
- **Config.**
  - `.greptile` keys: `autoApprove.enabled`, `autoApprove.riskCeiling` (default `"low"`),
    `autoApprove.filters.excludePaths`, and `autoApprove.filters.includePaths` (every changed file
    must match).
  - Dashboard toggle: Settings → Code Review → Auto-approve PRs. It requires the Confidence Score
    section.
  - Natural-language risk definitions are dashboard-only, "so a PR cannot change them".
- **Blocks approval:** draft/closed/merged PRs, new commits since the review started, a
  `do-not-merge` or `manual-review` label, failing review filters, or Confidence Score disabled.
  Blocks are silent.
- **Safeguards.**
  - The policy is read from the PR's base branch.
  - If the dashboard and the file conflict, the stricter value wins.
  - New commits dismiss the approval.
  - The docs advise starting with a Low cap.
- **Branch protection.** Human requested-changes reviews and unresolved discussions do **not**
  block Greptile's approval. "Required human approvals, status checks, and discussion resolution
  are still enforced by your merge rules."

### Cursor (Bugbot and PR Routing & Approval)

- **Bugbot only comments.**
  - It "leaves comments with explanations and fix suggestions".
  - Its status conclusion is `neutral` for findings by default. "Requiring the status alone does not
    block merges on findings" ([Bugbot](https://cursor.com/docs/bugbot)).
- **A separate agent, "PR Routing & Approval", approves** ([approval agents](https://cursor.com/docs/approval-agents)).
  It covers GitHub and Origin only.
- **Signals.**
  - "Use Risk Score" plus "Maximum Risk for Approval".
  - Per-directory `APPROVAL_POLICY.md` files (exact name only). The closest policy wins.
  - `.cursor/approval-policies/ROUTING.md`.
  - "Use Bugbot Review Context" / "Use Security Review Context": findings needing human review
    block approval. The agent waits about 8 minutes for the Bugbot check; on timeout it does not
    approve.
- **Safeguards.**
  - If a PR edits a policy or routing file, the base-branch version is used. If that can't be
    determined, the PR goes to human review.
  - When precedence is unclear, the stricter instruction wins and auto-approval is avoided.
  - The docs state it "does not replace a full code review".
- **Not documented:** branch-protection interaction.

### Sourcery

Source: [anatomy of a review](https://docs.sourcery.ai/reviews/anatomy-of-a-review/).

- **Setting.** "Let Sourcery approve pull requests". The default value was not found on the page.
- **Rule.** "Sourcery approves when nothing it found is worth blocking on and the change doesn't
  need a person's judgement."
- **Branch protection.** "a branch protection rule that requires an approval counts it."
- **Status check.** The "Sourcery review" check fails only on blocking security findings, and
  requiring it "never blocks a merge on its own".
- **Re-reviews.** It re-reviews after every commit, capped at five automatic re-reviews per PR.
  GitLab gets no status check.
- **Not documented:** dismissal of the approval on new commits.

### Ellipsis

Source: [code review](https://www.ellipsis.dev/docs/features/code-review). GitHub only.

- **Config.** In `.ellipsis/code-review.yaml`, committed to the default branch:
  - `output.on_pass` ∈ `silent | comment | approve`.
  - `output.on_fail` ∈ `comment | request_changes`.
  - "Without `output`, a pass is silent and a fail is a comment."
- **Rule.** "A review passes when no reviewer returned a blocking comment and fails otherwise."
  Reviewers label each comment blocking or not. A blocking comment on an unchanged line is held
  back but still fails the review.
- **Fail-closed.** "A review in which a reviewer stopped early never approves". A reviewer that
  runs out of budget contributes nothing, and the review posts "never as an approval".
- **Own PRs.** A PR opened by the Ellipsis app "gets a comment in every case" because "GitHub
  refuses an approval or a request for changes from a pull request's own author."
- **Conflicting page.** [GitHub integration](https://www.ellipsis.dev/docs/integrations/github) did
  not mention approval when fetched. A search snippet claimed an Ellipsis page saying it "never
  approves" **(unverified; possibly an older page)**.

### Bito

- **Request changes only.** Changelog entries:
  - 2025-04-25: "submit comments as "Request Changes" directly within GitHub".
  - 2025-05-02: Bitbucket support for the same.
  - 2025-05-09: "fully supports GitLab's "Request Changes" feature"
  ([previous releases](https://docs.bito.ai/changelog/previous-releases)).
- **No approve setting found** in the docs searched **(absence not proven; the changelog page was
  only partially read)**.

### Graphite Agent (formerly Diamond)

- **Comments only.** The setup docs describe it as commenting on PRs and suggesting fixes,
  "alongside human reviewers". Nothing on approving or blocking
  ([AI reviews setup](https://graphite.com/docs/ai-reviews-setup)).
- **No approve feature found.**

### Anthropic: Claude Code Review and the Claude Code GitHub Action

- **Claude Code Review (managed, GitHub)** ([Code Review](https://code.claude.com/docs/en/code-review)):
  - "A review never approves or blocks the pull request, so the decision to merge stays with your
    team."
  - The check run "doesn't conclude as a failure". With no findings, its title reads "Code review
    found no issues".
  - "A review with no findings doesn't mean your code is error-free or secure."
  - `REVIEW.md` is read from the base/default branch, "never from the pull request's own changes".
  - Comment commands work only from users with write access.
- **Claude Code GitHub Action:**
  - "For security reasons, Claude cannot approve pull requests"
    ([capabilities-and-limitations.md L16-L19](https://github.com/anthropics/claude-code-action/blob/ae68417a745a5df4b2124873632bd73a2255e6f6/docs/capabilities-and-limitations.md#L16-L19)).
  - The inline-comment MCP server deliberately exposes comments "without exposing full PR review
    capabilities, so that Claude can't accidentally approve a PR"
    ([github-inline-comment-server.ts L29-L31](https://github.com/anthropics/claude-code-action/blob/ae68417a745a5df4b2124873632bd73a2255e6f6/src/mcp/github-inline-comment-server.ts#L29-L31)).
  - The system prompt repeats the restriction
    ([create-prompt/index.ts L593](https://github.com/anthropics/claude-code-action/blob/ae68417a745a5df4b2124873632bd73a2255e6f6/src/create-prompt/index.ts#L593)).
- **The repo also ships the opposite control, `agent-approval-check`**
  ([README](https://github.com/anthropics/claude-code-action/blob/ae68417a745a5df4b2124873632bd73a2255e6f6/agent-approval-check/README.md)):
  - It requires N **human** approvals when a PR has agent activity. "An `APPROVED` review from a
    bot" counts as agent activity, and "agent and excluded-bot logins never count".
  - It posts a status to be made required. "This is the same gate Anthropic runs internally on
    every agent-authored PR."
- Community workflows that make Claude approve exist on GitHub. They are not Anthropic's and were
  not reviewed.

### Other tools that auto-approve

- **CodeAnt AI** ([auto approve PR](https://docs.codeant.ai/pull_request/features/auto_approve_pr)). GitHub only.
  - **Modes:** Off (default) / "Resolved comments" (every CodeAnt-authored thread resolved; human
    threads don't block) / "Policy agent". The checks run 60 s after a review and on the
    `pull_request_review_thread` webhook.
  - **Policy agent conditions:** model returns approve, no blockers, risk ≤ max (low/medium/high),
    ≥80% confidence, head unchanged, and no matching `human_review` rule in `.codeant/approval.json`.
    The policy is read from the base branch, and a PR that edits it is never auto-approved.
  - **Fail-closed:** over 300 files, over 60,000 patch characters, or a missing patch forces human
    review. Shadow mode is advisory only.
  - **Head SHA** is pinned when the check is scheduled.
  - **Counting:** its approval "counts as one of the N required approvals" but does not satisfy
    CODEOWNERS unless the app user is listed. GitHub dismisses it on push.
- **Macroscope Approvability** ([approvability](https://docs.macroscope.com/approvability)). GitHub.
  - **Criteria:** Eligibility (an agent judging size/risk/ownership/blame/author role; refactors,
    schema, security-sensitive and breaking changes go to humans) and Correctness (no unresolved
    correctness comments ≥ Minimum Blocking Severity, default Medium). Optional CODEOWNERS
    Ownership.
  - **Verdicts:** Approved / Would Approve (Preview Mode) / Needs Human Approval / Skipped.
  - **Config:** `.macroscope/approvability.md` with front matter `neverApprove`, `alwaysApprove`,
    `onlyApprove`, `conclusion`, `waitsFor`. The file is read from the latest PR commit, but any
    edit to it holds the PR.
  - **Withdrawal:** reopening a blocking thread withdraws the approval.
  - Macroscope "cannot approve its own PRs".
  - Its status check "is not fail-closed". The docs recommend GitHub required reviewers when a PR
    must stay blocked.

## Platform mechanics

### GitHub

- **Endpoint.** `POST /repos/{owner}/{repo}/pulls/{pull_number}/reviews`
  ([REST reviews](https://docs.github.com/en/rest/pulls/reviews)):
  - `event` ∈ `APPROVE | REQUEST_CHANGES | COMMENT`; omitting it creates a `PENDING` review.
  - `body` is "Required when using REQUEST_CHANGES or COMMENT".
  - `commit_id` "Defaults to the most recent commit in the pull request" when not specified.
    Passing the reviewed SHA explicitly pins the review to it. Whether GitHub rejects an APPROVE
    whose `commit_id` is not the head was **not verified**.
  - Dismissal is `PUT …/reviews/{review_id}/dismissals`.
- **Own PRs.** "Pull request authors cannot approve their own pull requests"
  ([approving with required reviews](https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/reviewing-changes-in-pull-requests/approving-a-pull-request-with-required-reviews)).
  Ellipsis observes the same for bot-authored PRs.
- **Request changes is sticky.** When required reviews are on and a write-access collaborator
  requests changes, the PR can't merge until that same collaborator approves (same page).
- **`GITHUB_TOKEN` in Actions:**
  - It needs `pull-requests: write` ([workflow syntax](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax)).
  - Fork PRs get write permissions downgraded to read-only (same page).
  - Approving also requires the setting "Allow GitHub Actions to create and approve pull requests"
    (Settings → Actions → General → Workflow permissions). On personal-account repos "workflows are
    not allowed to create or approve pull requests" by default; org repos inherit the org setting
    ([Actions settings](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/enabling-features-for-your-repository/managing-github-actions-settings-for-a-repository)).
- **Do `github-actions[bot]` approvals count?** GitHub introduced this policy because Actions
  approvals could satisfy "Required approvals". It "protects against a user using Actions to
  satisfy the 'Required approvals' branch protection requirement"
  ([changelog 2022-01-14](https://github.blog/changelog/2022-01-14-github-actions-prevent-github-actions-from-approving-pull-requests/);
  extended to PR creation in [2022-05-03](https://github.blog/changelog/2022-05-03-github-actions-prevent-github-actions-from-creating-and-approving-pull-requests/)).
  This implies that, when allowed, an Actions approval counts.
- **Do GitHub App approvals count?** Branch protection docs only say approvals come "from people
  with write permissions … or from a designated code owner", and say nothing about apps
  ([about protected branches](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches/about-protected-branches)).
  The claim that an App approval counts toward N comes from vendors (CodeAnt, Sourcery), not GitHub
  **(GitHub-side statement not found)**.
- **Stale approvals.**
  - Optional "dismiss stale pull request approvals when commits are pushed that affect the diff".
  - "Require approval of the most recent reviewable push" requires approval "from someone other than
    the last person to push".
  - "Require review from Code Owners".
  - All three are in [about protected branches](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches/about-protected-branches)
    and [ruleset rules](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/available-rules-for-rulesets).
- **Copilot specifics:** separate count toggles, and a non-Copilot approval required on
  Copilot-authored PRs (see above).

### GitLab

- **Endpoints** ([MR approvals API](https://docs.gitlab.com/api/merge_request_approvals/)):
  - `POST /projects/:id/merge_requests/:iid/approve` — "The currently authenticated user must be an
    eligible approver."
    - Optional `sha` "must match the merge request's HEAD commit SHA"; a mismatch returns
      `409 Conflict`.
    - Optional `approval_password` applies when re-authentication is required, and approval "Always
      fails" under forced SAML.
  - `POST …/unapprove` removes the caller's approval.
  - `PUT …/reset_approvals` is "Available only to bot users with a valid project or group token".
  - Approve, unapprove and reset are available on all tiers. Approval-rule endpoints are
    Premium/Ultimate.
- **Free vs paid** ([approvals](https://docs.gitlab.com/user/project/merge_requests/approvals/)):
  - On Free, any Developer+ can approve, and approvals "are optional and don't prevent merging
    without approval".
  - Required rules are Premium/Ultimate.
- **Eligible approvers** ([approval rules](https://docs.gitlab.com/user/project/merge_requests/approvals/rules/)):
  - Only direct members (project, group, parent groups, shared groups) are eligible; "Inherited
    members are not considered approvers."
  - Developers can approve when they are listed approvers or code owners.
  - When required approvals exceed the assigned approvers, approvals from Developer/Maintainer/Owner
    count.
- **Bot users.**
  - Project/group access-token bot users "are members of the project" with the token's role
    ([project access tokens](https://docs.gitlab.com/user/project/settings/project_access_tokens/)).
  - No GitLab doc found that states whether a bot user's approval satisfies an approval rule
    **(unverified)**. It plausibly follows the membership/role rules above, and the
    `reset_approvals` endpoint shows bots are expected in approval flows.
  - `CI_JOB_TOKEN` cannot approve: its MR and Notes access is read-only GET
    ([job token](https://docs.gitlab.com/ci/jobs/ci_job_token/)).
- **Settings** ([approval settings](https://docs.gitlab.com/user/project/merge_requests/approvals/settings/), Premium/Ultimate):
  - "Prevent approval by merge request creator" (default enabled).
  - "Prevent approvals by users who add commits" (default disabled).
  - "Prevent editing approval rules in merge requests".
  - "Require user re-authentication … to approve".
  - "When a commit is added": Keep approvals / **Remove all approvals (default)** / Remove Code
    Owner approvals if their files changed. GitLab uses `git patch-id` to skip resets for rebases
    with unchanged diffs.
  - API equivalents: `merge_requests_author_approval` and `merge_requests_disable_committers_approval`
    ([MR approvals API](https://docs.gitlab.com/api/merge_request_approvals/)).
- **GitLab Duo's approval does not count toward required approvals** ([Code Review Flow](https://docs.gitlab.com/user/duo_agent_platform/flows/foundational_flows/code_review/)).
  GitLab itself treats an AI approval as advisory.

## Risks the vendors call out, and their mitigations

- **Untrusted PR content controlling the reviewer (prompt injection, config injection).**
  - PR-Agent warns that a PR-derived config branch lets an attacker enable "auto-approval of their
    own PR", and keeps approval keys root-only
    ([configuration_options.md L70](https://github.com/qodo-ai/pr-agent/blob/f5ffd0ef8c7c36f797e0ee3b762c83006247956a/docs/docs/usage-guide/configuration_options.md#L70),
    [config_security.py](https://github.com/qodo-ai/pr-agent/blob/f5ffd0ef8c7c36f797e0ee3b762c83006247956a/pr_agent/config_security.py#L155-L186)).
  - The Claude Code Action warns of "hidden instructions through HTML comments, invisible
    characters, hidden attributes". It strips these, "but new bypass techniques may emerge"
    ([security.md L76-L78](https://github.com/anthropics/claude-code-action/blob/ae68417a745a5df4b2124873632bd73a2255e6f6/docs/security.md#L76-L78)).
  - Mitigations seen elsewhere: read policy and instructions from the base branch (Greptile,
    Cursor, CodeAnt, Claude Code Review's `REVIEW.md`); hold any PR that edits the policy file
    (CodeAnt, Macroscope); keep risk definitions dashboard-only (Greptile).
- **LLM false negatives ("no findings" ≠ safe).**
  - Claude Code Review: "A review with no findings doesn't mean your code is error-free or secure."
  - Copilot: "not guaranteed to spot all problems".
  - PR-Agent: the merge recommendation is "not a guarantee about the code".
  - Mitigations: risk ceilings and path globs that confine approval to low-risk changes (Greptile
    default `low`, Copilot globs, CodeAnt/Cursor/Macroscope deny lists); a separate
    eligibility/risk judgement (Greptile, Cursor, CodeAnt, Macroscope); shadow or preview modes
    before enforcement (CodeAnt, Macroscope).
- **Bot approvals satisfying human-review requirements.**
  - GitHub created the Actions approval policy for exactly this reason
    ([changelog 2022-01-14](https://github.blog/changelog/2022-01-14-github-actions-prevent-github-actions-from-approving-pull-requests/)).
  - Mitigations: separate "count toward merge requirements" toggles (Copilot); GitLab not counting
    Duo approvals at all; Anthropic's `agent-approval-check`, which ignores bot approvals and
    requires N humans.
- **Approving a commit other than the one reviewed (race on push).** Mitigations: head-SHA check
  (CodeRabbit, CodeAnt), GitLab's `sha` param with 409 on mismatch, and dismissal on push (Copilot,
  Greptile, CodeAnt; GitHub's "dismiss stale approvals"; GitLab's default "Remove all approvals").
- **Partial or failed runs looking clean.** Ellipsis never approves when a reviewer stopped early
  or ran out of budget. CodeAnt fails closed on oversized diffs. Cursor does not approve when the
  Bugbot check times out. PR-Agent merges chunk verdicts by worst value.
- **Status checks that are not fail-closed.** Macroscope says its check is not fail-closed and
  recommends required human reviewers. Bugbot and Sourcery checks don't block by default.

## Implications for pergent (inferences, not recommendations)

- *Inference:* pergent's output today is a free-form markdown comment, with no structured verdict
  in `internal/output/`. Every tool that approves on "no issues" decides from a machine-readable
  signal: Ellipsis's per-comment blocking flag, PR-Agent's `merge_recommendation` enum, Greptile's
  5/5 score, CodeAnt's `approve` plus confidence. "No issues" for pergent would probably have to
  become an explicit per-skill verdict field, rather than parsing prose.
- *Inference:* pergent runs one opencode process per skill. The closest prior art for combining
  results is PR-Agent's worst-of chunk merge and Ellipsis's "any blocking comment fails; any
  early-stopped or out-of-budget reviewer never approves". By analogy, a skill that times out,
  errors, or returns unparseable output would count as "not approved", not as "no issues".
- *Inference:* the platform defaults differ:
  - On GitHub, approving with `GITHUB_TOKEN` needs `pull-requests: write` plus the "Allow GitHub
    Actions to create and approve pull requests" setting, and fork PRs get a read-only token.
  - On GitLab, `CI_JOB_TOKEN` cannot approve, so the existing `GITLAB_TOKEN` (PAT or
    project/group bot) would need to be an eligible approver.
  - GitLab Free approvals never block merging anyway.
- *Inference:* both platforms have a head-SHA guard: GitLab's `sha` returns 409 on mismatch, and
  GitHub's `commit_id` pins the review. CodeRabbit and CodeAnt both re-check HEAD before
  approving. pergent knows the commit it diffed, so it could pass that SHA. Whether GitHub rejects a
  non-head `commit_id` for APPROVE is unverified.
- *Inference:* the safeguard every recent product converges on is keeping the approval policy out
  of the PR's control. pergent's skills and flags come from the CI job definition. If skills or
  config can be read from the PR branch, that branch could switch approval on or weaken the skill.
  This matches the PR-Agent and Greptile/Cursor/CodeAnt warnings.
- *Inference:* vendors split on whether an AI approval should count toward required approvals.
  GitLab says no for Duo. Copilot leaves it to a separate admin toggle. Anthropic's own gate
  ignores bot approvals. Whether pergent's approval counts is decided by the repo's branch
  protection and approval rules, not by pergent, so its docs may need to say so explicitly.
