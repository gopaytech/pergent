# 💬 The review comment

pergent posts one comment per PR/MR, updated in place on each run. It leads with the result, the model's summary and the findings; the checks follow, by area. In order:

- a headline with the verdict over all skills (`✅ No blocking issues`, `❌ N blocking issues`, or `⚠️ N skills failed`; a clean review that has dismissed blocking findings reads `No blocking issues (2 dismissed)`) and, when a skill carries a model score, the confidence (`· Confidence 3/5`, or `· Confidence not scored` when the skill that carried the score failed)
- a small line of facts: the reviewed commit (first 7 characters), the number of files, `N of M checks passed`, and the finding counts, ending with `· N skills failed` when there are blocking findings and a skill also failed
- the model's reason for the score, as a one-sentence quote, only when the score is below 5 or capped
- a skills table (skill, verdict, confidence, findings), only when more than one skill ran
- failures, one `⚠️ Review failed:` line each, with the cause, and the files that were not reviewed when the skill had several batches or several failures
- the summary, when one skill ran in one batch and the model gave a summary: `**Summary:** …`, 1 to 3 sentences. With several batches, one line per batch (`- **Batch 1 of 3:** …`) sits at the top of the checks instead, because the number of batches is not limited. With several skills, the summaries are in the skill sections below
- the findings, one list across all skills: blocking findings first and open, suggestions collapsed. Each row shows the title, `file:line` linked to those lines, and the skill name when there are several. Open a row for the explanation, the suggested fix and, for a small fix, the exact replacement code (`Suggested change, replacing lines 42–43:`). The model gives replacement code for at most 10 lines; a suggestion that doesn't fit is dropped and the finding is kept
- the dismissed findings, collapsed, when someone set any aside
- the details: with several skills, one `#### <skill>` section for each skill that has a summary or checks, in the order of the skills table, holding its summary and then its checks. The checks, when a skill has a checklist, are by area: a `#### <area>` heading (`##### <area>` inside a skill section) with a table of `Check`, `Status` and `Detail`. Areas keep the order they first appear in, and failed checks come first in their area, so one failure among many is not missed. Checks the model gave no area come last; beside named areas they get the heading `Other checks`, so they don't read as part of the area above, and with no named areas they have no heading. A check that several batches answered is one row, with the worst status
- one collapsed `Files (N)` list of every changed file, with the reason for each file a skill skipped or did not review
- notes, such as a skill with no files in scope, a verification that was skipped, a failed check that has no matching finding, or the findings the model left out because people rejected the same mistake before (`Left out because of past dismissals: Missing nil check (Caller never passes nil)`, at most 20 listed)
- a footer with the pergent version
- the hidden findings block (last line)

A skill with failed batches still shows the findings from the batches that finished, next to the failure.

A review with one blocking finding looks like this:

````markdown
### ❌ 1 blocking issue · Confidence 3/5 (capped)

<sub>Commit `4f1c2a9` · 4 files · 2 of 3 checks passed · 1 blocking · 1 suggestion</sub>

> The retry loop in `internal/retry/retry.go` is new and untested. (capped: 1 blocking finding)

**Summary:** Read the new retry package and its two callers. The loop never checks for a cancelled context.

#### Findings (2)

▸ ❌ **Retry never stops on a cancelled context** · internal/retry/retry.go:42–47
▸ 💡 **Name the backoff constant** · internal/retry/backoff.go:9

#### Errors

| Check | Status | Detail |
|---|---|---|
| Context cancellation is honoured | ❌ | Do loops on ctx.Err() without returning |
| Backoff is capped | ✅ | maxDelay is applied in next |

#### Tests

| Check | Status | Detail |
|---|---|---|
| The failure path is tested | ✅ | TestDoGivesUp covers it |

▸ Files (4)

pergent v0.3.0
````

(In the real comment each `▸` row is a collapsed `<details>` block, and the blocking one is open.)

The confidence is the model's own judgement of how safe the change is to merge, from what it reviewed. It is the lowest score among a skill's finished batches, and pergent caps it: a blocking finding caps it at 3, and a file skipped as deleted, binary, vendored, a lockfile or generated caps it at 4. A skill that failed is not scored. It is information only: it never changes a verdict. When no skill carries a model score (older answers, failed runs), the comment shows no confidence at all.

**Links.** In GitHub CI mode, `file:line` links to the file at the pull request's head commit (`pull_request.head.sha`); in GitLab CI mode, at `CI_MERGE_REQUEST_SOURCE_BRANCH_SHA` (set in merged-results pipelines) or else `CI_COMMIT_SHA`. A line link ends in `?plain=1#L<line>`, so the anchor works for Markdown and other rendered files too. Local and agent mode have no links. `actions/checkout` checks out the merge commit of a pull request, so if the base branch changed the same file above a finding since the branch was cut, the line anchor on GitHub can be a few lines off.

**Dismissing a finding.** With pergent.sh, each finding also shows a ready-to-copy dismiss command with its ID, and a dismissed finding moves to the Dismissed list. The CLI's CI comment shows no finding IDs or dismiss command. Agent mode shows each finding's ID in its row (`id …`), and pergent.sh adds the dismiss command.

The last line of the comment is a hidden HTML comment holding the findings for the next run (see below). Don't edit it. Secrets are redacted from the comment and from that block.

A comment that would exceed the platform's size limit (65,536 characters on GitHub, 1,000,000 on GitLab) is shrunk to fit rather than failing to post. In order: the hidden findings block is dropped (the next `--previous-review` run is then a first review), then the suggestions' explanations and fixes (their titles stay), then the suggestions and dismissed findings, then the passed and n/a checks (an area left with none loses its heading), then the failed checks (the counts in the facts line stay), then the `Files` list is cut to 20 paths and then to a count, and last the summaries. The headline, facts line, reason, skills table, blocking findings and failures are always kept; each step leaves a note in the comment. As a last resort, if the blocking findings and failures alone exceed the limit, the comment is cut at the limit with a note.

## Layouts

The order above is the default. A program that builds the comment with the public `render` package can choose another one with `render.Options.Layout`, a list of blocks: `summary`, `findings`, `dismissed`, `checks` and `files`, each `open` or `collapsed` (`ParseLayout` reads `findings:collapsed`-style entries). A block the list leaves out is not written; `findings` is required. `summary:collapsed` puts the summaries in a `Summary` fold, `checks:collapsed` keeps the failed checks in view and folds the passed and n/a ones per skill (`Checks: 3 of 4 passed, 1 n/a`), and `findings:open` opens the suggestions too. The summaries by batch or skill, and the failed checks, sit where `checks` is, else where `summary` is, else right after `findings`. A list that is empty or not valid gives the default.

A layout changes what the comment shows, never what the review decides. Everything above the blocks (the headline, facts line, reason, skills table and failures) and below them (notes, footer, hidden block) stays, blocking findings are always open, a failed check is always shown (only the size limit can cut failed checks, at its step 5, with a note), and a quiet review (`Options.Short`) still leaves out the `Files` list and the passed checks. A clean headline still says `(2 dismissed)` when the `dismissed` block is left out. The size-limit steps run in the order above whatever the layout says, and a block left out leaves no note (failed checks cut for size still do). The last-resort cut, when the blocking findings and failures alone are over the limit, is made on the default order, so a custom order can't push a blocking finding out first. The CLI always uses the default layout.
