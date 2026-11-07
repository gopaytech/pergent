# The change's title and description in a review

Status: agreed with the owner on 2026-11-07, in a grilling round that settled the questions below. Built on `feat/change-description` in both repositories.

Spans two repositories. **pergent** attaches the text to the review runs and reads it in CI mode. **pergent.sh** reads it from the platform and passes it to the Job.

## Why

A review saw the diff, the changed-file list, the repository and the skill, but never the merge request's title or description. Neither repository read them for a review. So the model could say what the code does, but not whether that is what the author meant:

- A deliberate change could be reported as a bug, because nothing said it was deliberate.
- Code that does not do what the description says could not be noticed.

A person reads the description first. A review should too.

## Goals

- A review knows what the author meant to do.
- It uses that to tell an intended change from a mistake, so it flags fewer deliberate changes.
- It raises a suggestion when code does the opposite of what the description says.
- It works in CI mode and in agent mode, on GitHub and GitLab.

## Non-goals

- Commit messages and linked issues. They carry more untrusted text, need more fetching, and GitHub and GitLab link issues differently.
- A setting to switch this off, per repository or globally.
- Starting a review when a title or description is edited.
- Showing the text in the comment, the result or the dashboard. The Job log says how many bytes were read, and nothing else does.
- A new finding field for mismatches. They are ordinary suggestions: they can be dismissed, a dismissal can teach, and quiet reviews filter them like any other.
- Different handling for bot pull requests. A bot's description is mostly upstream release notes, which help with a dependency bump, and a bot rule would differ by platform and mode.

## Anti-goals

- The author's text deciding whether a blocking finding counts. That would let an author talk a real problem away and get a change approved.
- The author's text creating a blocking finding. A description must not be a way to block a change.
- Findings about the description itself. A short or empty description is not a code problem, and a nag on every lazy change is noise.
- False findings from a partial view. A run sees part of the change, so it must not report what the description promises as missing.
- A review that fails, or a job that goes red, because the text could not be read. The review is still worth running.
- A long description taking the room skills and the diff need.
- A pergent.sh that breaks an older pergent image.

## Decisions

These are the owner's.

| Question | Decision |
|---|---|
| What the text is for | Both: understand intent, and catch code that contradicts it. |
| What intent can excuse | A change in behaviour that is only surprising. Never a problem that counts as blocking. |
| How serious a mismatch is | Always a `suggestion`, unless the code is a blocking problem in its own right. A skill's `## Blocking` section cannot change this. |
| What counts as a mismatch | Code that does the opposite of the text. Not something promised that the run does not see. |
| Hidden text | HTML comments are removed first. |
| The description itself | Never a finding, however short, vague or empty. |
| Bot pull requests | Treated like any other. |
| Which text reaches the model | The title and the description. Nothing else. |
| Whether verification sees it | No. A blocking finding is re-checked against the code alone. |
| Who controls it | Nobody: it is always on. |
| Whether an edit starts a review | No. The new text is used on the next push or re-run. |

The cost of keeping verification blind is accepted: a deliberate change flagged as blocking can stay blocking, and then its author dismisses it. It is also why a mismatch cannot block: verification has no description to confirm one against, so a blocking mismatch would be rejected unpredictably.

## How it works

### Trust

The author writes this text, so it is data, never instructions. It is attached as a file, like `previous-findings.json`, and the message says so.

The message (`changeMessage`) carries the rules: a change in behaviour called intended is not a finding for that reason alone; a stated intent never excuses a blocking problem; a contradiction is a `suggestion` whatever the skill says, unless the code is a blocking problem in its own right; a promise the run does not see is not reported; the file itself is never a finding.

That is wording, not a guarantee. The text can still sway the review run, as a code comment can, with one difference: a reviewer sees a code comment in the diff. So HTML comments, which the platforms do not show, are removed first, and one left open removes the rest of the text, as it does when rendered. Other Markdown that does not render, such as an unused link definition, is not removed. Keeping the text from verification protects the findings that were raised; a finding the first run never reports is not checked by anything.

### Which runs get it

| Run | Gets it |
|---|---|
| Batch | Yes |
| Cross-batch | Yes |
| Verification | No |
| Repair | No: it only rewrites an answer's format |

### pergent

- `review.Request.Description` holds the text.
- `changeText` makes it valid UTF-8, removes HTML comments, trims it and cuts it to 16 KiB, on a rune boundary, with a note that says it was cut. Blank text, or text that is only comments, attaches nothing.
- `reviewBatch` and `crossBatch` attach it as `change-description.md` and add `changeMessage` to the run's message.
- CI mode: `Platform.FetchChangeDescription` reads the pull request or merge request and joins the title and the description. A failure logs a warning and the review goes on. `--local` has no change to read.
- The log says `change description: <n> bytes` when the text was read, in both modes. A Job without that line ran an image that ignores it.
- Agent mode: `--change-description`, by default `$PERGENT_CHANGE_DESCRIPTION`. The path is a trusted input: one inside `--repo-path` is a usage error, like `--previous`. A missing or unreadable file logs a warning and the review goes on. The text in the file is still the author's.

### pergent.sh

- `forge.Change` gains `Description`, from GitLab's `description` and GitHub's `body`. `GetChange` already fetches both, so there is no new API call.
- Dispatch calls `Inputs.AddDescription` with the title and description read when the Job starts.
- `AddDescription` keeps at most 32 KiB, only so a long description cannot take the ConfigMap's budget; GitLab allows about a megabyte. pergent makes the real cut.
- The text goes in the inputs ConfigMap as `change-description.md`, mounted at `/in`.
- The Job finds it through the environment variable `PERGENT_CHANGE_DESCRIPTION`, not a flag: an older image ignores a variable it does not know, but exits 2 on an unknown flag.
- It is not part of any skill's hash, so it never changes what an incremental review carries.
- `Inputs.Fit` drops it after the dismissals and before the previous result, so it never turns an incremental review into a full one. Dropping it logs a warning.

No migration, and no dashboard, auth or store change.

### Skills

A skill can use the description, but must still work without it: it is missing with `--local`, when it cannot be read, and on an empty change. The README says so.

## Rollout

The two changes can merge in either order. pergent.sh passes the text from its first release with this change, and an older image ignores it. Reviews use it once a pergent release with this change is out and `jobs.image` moves to it.

## Testing

- pergent: the text reaches every batch and the cross-batch run; blank text attaches nothing; verification and repair get neither the file nor the text; HTML comments are removed, including one left open, before the cut; the 16 KiB cut, including a rune across the limit; a test pins each rule in `changeMessage`; both platforms, including a null description and a failed read; the flag, the variable, the log line and the checkout rule.
- pergent.sh: both forges map the field; the bound and the bad-byte handling; `Size` and `Fit`; the environment variable, ConfigMap key and volume item, and no flag; dispatch passes the text (`TestDispatchPassesChangeDescription`, which needs Postgres, so it runs only in CI).
- Mutation checks: each piece of the above was broken on purpose and a test failed, including attaching the text to the verification run.

Not yet known: how much this improves real reviews, and how closely a model follows `changeMessage`. No model has run with it.

## Left open

- Whether commit messages are worth adding when a description is empty.
- Whether the model's wording needs tuning once real reviews show how it uses the text.
