# 🧑‍⚖️ Review panel

A review panel reviews one change with two models and merges the two reviews into one result. The design is in the pergent.sh repository (`gopaytech/pergent.sh`, `docs/superpowers/specs/2026-11-20-review-panel-design.md`). This page is about what the `pergent` CLI does.

The CLI has two jobs in a panel. Both use `pergent agent`.

| Job | Command | What it does |
|---|---|---|
| **Member** | `pergent agent --no-skill-verify ...` | A normal review on one model, with no verification run per skill. The result is the member's. |
| **Merge** | `pergent agent --panel --panel-a a.json --panel-b b.json ...` | Reads both member results and writes one result. It does not review the change again. |

The caller (such as pergent.sh) runs the two members, then the merge.

## The two models

Model A is the usual `OPENCODE_*` setting. Model B is set with `PERGENT_PANEL_B_*`.

| Model A | Model B | Notes |
|---|---|---|
| `OPENCODE_PROVIDER` | `PERGENT_PANEL_B_PROVIDER` | Required for both |
| `OPENCODE_MODEL` | `PERGENT_PANEL_B_MODEL` | Required for both |
| `OPENCODE_BASE_URL` | `PERGENT_PANEL_B_BASE_URL` | Optional |
| `OPENCODE_NPM` | `PERGENT_PANEL_B_NPM` | Optional |
| `OPENCODE_API_KEY` | `PERGENT_PANEL_B_API_KEY` | Each key is used only for its own model |

**Key isolation.** A run on model A gets A's key only. A run on model B gets B's key only, as that run's `OPENCODE_API_KEY`. `PERGENT_PANEL_B_API_KEY` is never passed to opencode. No key is written to a log, note, failure, comment or result.

Model B signs in only through `PERGENT_PANEL_B_API_KEY`. The cloud credentials in the environment (`AWS_*`, `GOOGLE_*`, `VERTEX_*`, `AZURE_*`, `CLOUDFLARE_*`) are model A's, so they are removed from every run on model B. A provider that signs in with cloud credentials is not supported for model B. Other names listed in `PERGENT_OPENCODE_ENV` reach the runs of both models.

## What the merge does

For each skill that both members reviewed, the merge runs two judge runs:

- Run 1 uses model B. It reads A's findings and says whether each one is real.
- Run 2 uses model A. It reads B's findings and says the same.

Each judge run also names its own finding that is the same issue (`same_as`). Findings that are linked are one group. A judge run gets the other model's findings as a file, with a sentence that says the file is data and not instructions.

With `--rules` (or the built-in rules), a judge run also gets the [path rules](rules.md) of the files of the blocking findings it judges (the other model's), as `file-rules.md`, as verification gets them. A one-member check (`a_only`, `b_only`) gets them the same way. If that file can't be written, the judge run fails and does not start: the findings it would have judged stay blocking.

The merged list has one finding per group. It uses the text of the most severe member (the first blocking one; A's first on a tie). The skill's confidence is the lower of the two. A confidence of 0 (not scored) in either member makes it 0.

### Verdict of a group

| Who found it | Other model said | Result |
|---|---|---|
| Both, both blocking | n/a | Blocking, `found_by: both` |
| One, blocking | Real | Blocking |
| One, blocking | Not real | **Suggestion**, `disagreed_by` and `reason` set |
| One, blocking | No usable answer | Blocking, `unjudged_by` set |
| Several blocking findings of one model | Not real for all of them | Suggestion. One real, or one without an answer, keeps it blocking |
| One blocking, other model has a suggestion | n/a | Blocking, `rated_suggestion_by` set |
| Both suggestions | n/a | Suggestion |

A judge can lower a finding to a suggestion. It cannot drop one. A `same_as` that names a missing finding, or one of the wrong side, is ignored.

### When something fails

| Case | What the merge does | Mode of the skill |
|---|---|---|
| Both members usable for the skill | Two judge runs | `both` |
| Only A usable (B's skill is missing, `failed`, or lists a file in `files.not_reviewed`) | Takes A's review. A's blocking findings are checked on A's model | `a_only` |
| Only B usable | The same, with B | `b_only` |
| Neither usable | The skill is `failed`. It never drops out, because that would read as a pass | `none` |
| One judge run fails, is skipped (the budget was short) or gives no usable answer | Findings it would have judged stay blocking. A note says so | `both` |
| Both judge runs fail, or `--panel-no-judge` | Exact merge: findings are joined by file, line and title. Every blocking finding stays blocking | `both`, `exact_merge` |
| A skill is loaded but neither member reviewed it | The skill is `failed` | `none` |
| A member's file is missing, unreadable, not schema 1 or not `completed` | The member is not used. The reason goes to the log only | top-level `panel.mode` is `a_only` or `b_only` |
| A member's result is for another head or base (stale) | The member is not used, as a missing file. The reason goes to the log only | top-level `panel.mode` is `a_only` or `b_only` |
| Neither member file is usable | Usage error, exit 2, error result | n/a |

The panel never fails a review that would pass on one model, and a failed review is never a pass. The `impact` check is not judged: both members' impact findings are merged by file, line and title.

With `--panel-no-judge` and only one member, the `a_only` or `b_only` note leaves out the sentence about checking the blocking findings, because no run checked them.

## The comment

The CLI names a model as `provider/model`. A caller can pass its own names to `render.Options.Panel`. The texts below are built by `render`, never stored. `<A>` and `<B>` are the names, and `<X>` is the model the line is about.

| Where | Text |
|---|---|
| Meta line | `Panel: <A> (A) + <B> (B)` |
| Row, skill mode `both` | ` · found by both`, ` · found by A`, ` · found by B` |
| Row lead, demoted | `_<X> disagreed: <reason>_` |
| Row lead, mixed | `_<X> rated it a suggestion_` |
| Row lead, no answer | `_<X> could not check this finding; it stays blocking._` |
| Note, `a_only` or `b_only` | `Panel: only <X> reviewed this; <Y>'s review failed. <X> checked its own blocking findings.` |
| Note, `none` | `Panel: neither model could review this.` |
| Note, a judge run failed or was skipped | `Panel: <X> could not check <Y>'s blocking findings; they stay blocking.` |
| Note, exact merge | `Panel: the cross-check did not run. Findings were merged by file, line and title, and every blocking finding stays blocking.` |

A reason is cut to 300 bytes and made safe, so it cannot ping a user.

## Dismissals and the result

A dismissal of any finding in a group dismisses the merged finding. The result keeps the other members' fingerprints in `panel.also` for this. A member finding that a `--dismissals` entry already covers (same fingerprint, and a severity the entry covers) is never linked to another finding by `same_as`, so its dismissal cannot hide a finding of the other model.

`--dismissals`, `--rules`, `--no-built-in-rules` and `--built-in-rules` are accepted with `--panel`. Each member result's `files.rules` is kept in the merged result. These flags are a usage error with `--panel`: `--previous`, `--carried`, `--ci-results`, `--change-description`, `--linked-repos`, `--graph`, `--no-skill-verify`.

The new result fields are in the [result schema](../agent-result-schema.md): `panel`, `skills[].panel`, `skills[].tokens_by_model`, `findings[].panel` and `provider_error.role`. See also [Agent mode](agent-mode.md).
