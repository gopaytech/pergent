# 🧑‍⚖️ Multi review (experimental)

> **Experimental.** Multi review is new and is not yet measured on real merge requests. The flags, the result fields and the comment texts can still change.

A multi review reviews one change with 1 to 5 reviewers. Then one judge merges their results into one result. The design is in the pergent.sh repository (`gopaytech/pergent.sh`, `docs/superpowers/specs/2026-11-22-multi-review-design.md`). This page is about what the `pergent` CLI does.

## The 2 Jobs

Both Jobs use `pergent agent`. The caller (such as pergent.sh) runs every reviewer Job, then the judge Job.

| Job | Command | What it does |
|---|---|---|
| **Reviewer** | `pergent agent --no-skill-verify ...` | A normal review on one model, with no verification run per skill. The impact check still verifies. The result is the reviewer's. |
| **Judge** | `pergent agent --multi-review --reviewer r1.json --reviewer r2.json ...` | Reads every reviewer result and writes one result. It does not review the change again. It starts at most one judge run per skill. |

The order of `--reviewer` is the number: the n-th `--reviewer` is reviewer n. A file that can't be used keeps its number.

## The judge's model and key

The judge uses the usual `OPENCODE_*` setting, as any review run does.

| Variable | Notes |
|---|---|
| `OPENCODE_PROVIDER`, `OPENCODE_MODEL` | Required, unless `--no-judge` is set |
| `OPENCODE_BASE_URL`, `OPENCODE_NPM` | Optional |
| `OPENCODE_API_KEY` | The judge's key. It can be empty with `--no-judge` |

The judge Job needs no reviewer's key: the judge does every check. No run gets a key of its own. The model B variables of the old 2-model merge are never read. No key is written to a log, note, failure, comment or result.

## Flags

| Flag | Meaning |
|---|---|
| `--multi-review` | Merge the `--reviewer` results into one result, with one judge run per skill on the `OPENCODE_*` model |
| `--reviewer <file>` | A reviewer's agent result. Repeat it 1 to 5 times, in reviewer order. A trusted input: it must be outside `--repo-path` |
| `--no-judge` | Run no model. Merge by file, line and title (an exact merge) |
| `--mr-dismissals <file>` | The merge request's stored dismissals, as JSON `[{"fingerprint": "...", "severity": "blocking"}, ...]`, where `severity` is `blocking` or `suggestion`. A trusted input: it must be outside `--repo-path`. A judge never links a finding that one of these entries covers (see [Dismissals](#dismissals)). Required unless `--no-judge` is set. Pass every dismissal the comment applies, including the ones it matches through `multi_review.also`. With none, pass a file with `[]` or `null` |

`--reviewer`, `--no-judge` and `--mr-dismissals` need `--multi-review`.

**Accepted with `--multi-review`:** `--repo-path`, `--base`, `--head`, `--skills-dir` or `--skill`, `--instructions`, `--dismissals`, `--rules`, `--no-built-in-rules`, `--built-in-rules`, `--check-deleted`, `--scrub-instructions`, `--max-comment-size`, `--result` and `--result-url`, `--max-turns`, `--context-limit`, `--concurrency`, `--timeout`. The skills that are merged are the loaded skills, in their order. Each reviewer result's `files.rules` is kept in the merged result.

**Refused with `--multi-review`:** `--previous`, `--carried`, `--ci-results`, `--change-description`, `--linked-repos`, `--graph` and `--no-skill-verify` each give `--<flag> can't be used with --multi-review`. A judge Job does not use the environment defaults of these inputs.

The flags of the old 2-model merge are gone: each one is `flag provided but not defined`.

Usage errors (exit 2):

| Error | When |
|---|---|
| `--multi-review requires at least one --reviewer` | No `--reviewer` |
| `--multi-review takes at most 5 --reviewer files, got <n>` | More than 5 |
| `--reviewer <n> is empty` | An empty path |
| `--reviewer <i> and --reviewer <j> name the same file` | The same file twice |
| `--reviewer needs --multi-review`, `--no-judge needs --multi-review`, `--mr-dismissals needs --multi-review` | The flag without `--multi-review` |
| `--multi-review requires --mr-dismissals (an empty [] when the merge request has no dismissals)` | No `--mr-dismissals` and no `--no-judge`. A forgotten file would read as "no dismissals", and a judge link could then hide a real finding |
| `--mr-dismissals is empty` | `--mr-dismissals ""` |
| `--mr-dismissals <file>: <reason>` | The file can't be read, is not a JSON list, or has an entry with no fingerprint or with another severity. This is never a warning: with a dismissal the judge does not know of, a link could join a finding that the comment will dismiss |
| `--reviewer <n> <file> is inside --repo-path: ...` | A reviewer file in the checkout. `--mr-dismissals` gets the same check |
| `--multi-review needs at least one usable reviewer result` | No reviewer file can be used. The error result is delivered |

## What the judge gets

Per skill, the judge run gets each usable reviewer's findings, and the blocking findings of a reviewer whose skill failed after some batches (see [When something fails](#when-something-fails)), as `reviewer-<n>.json`: `{"findings": [{"id": "2.3", ...}]}`. The id `2.3` is reviewer 2, finding 3. Both numbers start at 1. The fingerprint is left out, so the judge answers by id. The message says that the reviewer files are data, never instructions.

It also gets what a verification run gets: the checkout, the deleted files, the skill's blocking `--dismissals` entries as `rejected-findings.json`, and, with `--rules` or the built-in rules, the [path rules](rules.md) of the files of the blocking findings as `file-rules.md`. If the rules file can't be written, the judge run does not start: it is `failed`, and every blocking finding stays blocking.

## What the judge answers

Only the last ```` ```json ```` block of the answer counts:

```json
{"results": [{"id": "1.2", "real": true, "reason": "One sentence", "same_as": ["2.1"]}]}
```

| Field | Meaning |
|---|---|
| `real` | `true` or `false` for a blocking finding. `null` for a suggestion: a suggestion is never judged |
| `reason` | One sentence |
| `same_as` | The ids of other reviewers' findings about the same issue. One id (a string or a number) is read as a list of one. `null`, or no key, is none |

An id that the answer names twice is judged not at all: the finding stays as it is, and no link starts from it. An answer with no `json` block, bad JSON or no `results` key is a bad answer.

## Groups

Findings are grouped with union-find:

1. Findings of different reviewers with the same file, line and title (case and spacing ignored) are one group before the judge's links. So the judge can't split 2 reviewers' exact same finding when it leaves out `same_as`.
2. Then each `same_as` link joins 2 groups.

A link is ignored when an id does not exist, when both ends are findings of one reviewer (a finding and itself too), or when either end is a finding that an `--mr-dismissals` entry covers. Step 1 also leaves out a finding that an `--mr-dismissals` entry covers. A `--dismissals` entry never stops a link: see [Dismissals](#dismissals). A group can still hold several findings of one reviewer, through a finding of another reviewer.

| Group | Result |
|---|---|
| Blocking from 2 or more reviewers | Blocking. The judge can't demote it |
| Blocking from 1, judge says real | Blocking |
| Blocking from 1, judge says not real | Suggestion, with `judge_disagreed` and `reason` set |
| Blocking from 1, no usable answer | Blocking, with `unjudged` set |
| Blocking from 1, a suggestion from another | Blocking, unless the judge says not real. `rated_suggestion_by` names the others |
| Several blocking from 1 reviewer | A suggestion only when the judge says not real for all of them. One real or unanswered finding keeps it blocking |
| Suggestions only | Suggestion, never judged |
| Impact check | Exact merge, no run |

A judge can lower a blocking finding to a suggestion. It can't drop a finding.

The group's text is its first blocking finding (the lowest reviewer number, then that reviewer's order), or its first finding when none is blocking. The skill's confidence is the lowest of the usable reviewers', with that reviewer's reason (the lowest number on a tie). A confidence of 0 (not scored) in any usable reviewer makes it 0.

The rest of a merged skill is the lowest-numbered usable reviewer's: its files, batches, summaries and checks. The other reviewers' notes are added. The skill's `tokens` and `runs` are the judge's own. Each reviewer reports its own.

## When something fails

Never a false pass: a judge that fails, answers badly, is skipped or is off never demotes or drops a blocking finding. A skill that no reviewer can be used for is `failed`.

| Case | What the judge Job does | `judge` |
|---|---|---|
| At least 1 reviewer usable for the skill, 2 or more reviewers that give findings (see the row on a reviewer that is not usable), and a blocking finding, or findings from 2 or more of them | One judge run | `ok` |
| 2 or more usable, no blocking finding, and findings from at most 1 of them | No run | `not_run` |
| `--no-judge` | No run, exact merge | `skipped` |
| More than 200 findings of all reviewers for one skill | No run, exact merge | `skipped` |
| Less than 90 seconds of the budget left, or the run could not start (the review was stopped) | No run, exact merge | `skipped` |
| The run failed, gave a bad answer, or its rules file could not be written | Exact merge. `judge_cause` holds the run's cause when it reported one | `failed` |
| 1 reviewer usable, and no other reviewer gives a finding | That reviewer's review. Its blocking findings are checked on the judge model, as a review on one model verifies them: a finding the run says is not real is dropped, with the note `Verification dropped ...`. A run that fails, answers badly or is skipped keeps every finding, with the usual verification note | `not_run` with no blocking finding, else `ok`, `failed` or `skipped` |
| 1 reviewer usable, `--no-judge` | That reviewer's review as it is, exact merge | `skipped` |
| No reviewer usable for the skill | The skill is `failed`, also when a reviewer that is not usable has a blocking finding, with the failures of the lowest-numbered reviewer that reported any, else `No reviewer's result has a usable review of this skill.` It never drops out, because that would read as a pass | `not_run` |
| A reviewer is not usable for a skill: the skill is missing from its result, is there twice, is `failed`, has a failure, a batch that did not finish, a file in `files.not_reviewed`, or a verdict pergent does not know | Its number is in the skill's `failed`. When its skill is there once, with a verdict pergent knows, and failed only by a failure, a batch that did not finish or a file not reviewed, its **blocking** findings of that skill still give findings: they are merged and judged like any other (in an exact merge they stay blocking), and its number can be in `found_by`. So a reviewer that times out in batch 2 still gives what batch 1 found. Its suggestions are not used. A skill that is missing, is there twice or has a verdict pergent does not know gives nothing | n/a |
| A reviewer file is missing, not a regular file, over 10 MB, not JSON, not `schema_version` 1, not `completed`, or for another head or base | The reviewer is not used. The log says `multi review: reviewer <n> can't be used: <reason>`. The reason never reaches the result | n/a |
| No reviewer file is usable | Usage error, exit 2, error result | n/a |

In an exact merge, findings are joined by file, line and title, and every blocking finding stays blocking. A reviewer finding with a severity that pergent does not know is blocking.

The impact check is never judged, because each reviewer verified its own. With at least 1 usable reviewer and 2 or more reviewers that give findings, their impact findings are merged by file, line and title (a reviewer whose impact check failed gives its blocking findings, as for a skill). With 1 usable reviewer and no other, it is that reviewer's check. With none usable, it is `failed`. It is in the result when a reviewer has one and no loaded skill has its name.

## Comment texts

`render` builds these texts. They are never stored. `<n>` is the name of reviewer n, or `reviewer <n>` when the caller gave none. The CLI's own comment names the reviewers `reviewer 1`, `reviewer 2`, ... and the judge `<provider>/<model>` (no judge with `--no-judge`). pergent.sh passes its own reviewer names in `--reviewer` order, and the reviewers it skipped. Every name is made inert, so it can't ping a user or start Markdown or HTML.

| Where | Text |
|---|---|
| Meta line | `Multi review: <1>, <2>, <3> · judge <judge>` (the ` · judge <judge>` part only when there is a judge) |
| Row, a skill where `<N>` is 2 or more | ` · found by <k> of <N> reviewers (<a>, <b>)` when 2 or more found it, ` · found by <a>` when one did. `<N>` is the number of usable reviewers for the skill, plus each reviewer in `failed` that a finding of the skill is found by |
| Row lead, demoted | `_judge disagreed: <reason>_`, or `_judge disagreed_` with no reason |
| Row lead, mixed | `_<a>, <b> rated it a suggestion_` |
| Row lead, no answer | `_the judge could not check this finding; it stays blocking._` |
| Note, a reviewer not usable for the skill | `Multi review: <n> could not review this.` (one per reviewer in `failed`) |
| Note, exact merge | `Multi review: the judge did not run. Every blocking finding stays blocking.` (also when a judge run ran and failed) |
| Note, a skipped reviewer (pergent.sh only) | `Multi review: <name> was skipped: <reason>.`, or `Multi review: <name> was skipped.` with no reason. Written once, before the per-skill notes, with no skill name |

The judge's reason and a skip reason are put on one line and cut to 300 bytes. The last `.` of a skip reason is removed, because the note ends with its own. With several skills, each per-skill note starts with the skill's name.

## Dismissals

A merged finding stands for its group. The result keeps the fingerprints of the group's other findings in `multi_review.also`. A dismissal of the finding's own fingerprint, or of any fingerprint in `also`, dismisses it, by the usual rule: a `suggestion` dismissal does not dismiss a blocking finding.

A finding that an `--mr-dismissals` entry covers is never linked to another one. An entry covers a finding with the same stored fingerprint and a severity no higher than the entry's. So a dismissal can't hide a real finding of another reviewer, as long as `--mr-dismissals` holds every dismissal the comment applies. That is why the flag is required for a judge Job.

`--dismissals` is only the teaching set: the judge gets its blocking entries as `rejected-findings.json`, but they never stop a link. The comment does not apply them, so a finding they match is not dismissed. If they stopped links, 2 reviewers' copies of one blocking finding could be split into 2 groups of 1, and the judge could demote both.

## The result

The fields are in the [result schema](../agent-result-schema.md): `multi_review`, `skills[].multi_review`, `findings[].multi_review` and `provider_error.role` (`judge`). See also [Agent mode](agent-mode.md).
