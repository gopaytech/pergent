# 📝 The change's title and description

In CI mode pergent reads the pull request's or merge request's title and description and attaches them to each batch run and the cross-batch run as `change-description.md`, so the model knows what the author meant to do. The log says `change description: <n> bytes` when they are read. If they can't be read, a warning is logged and the review goes on without them. `--local` has no change to read them from. In agent mode the caller passes them with `--change-description`.

What the model is told:

- A change in behaviour the description calls intended is not a finding for that reason alone. A stated intent never excuses a problem that counts as blocking.
- Code that does the opposite of what the description says is a `suggestion`, whatever a skill's `## Blocking` section says, unless the code is a blocking problem in its own right.
- Something the description promises but the run does not see is not reported: a run sees only part of the change, so it may be in another batch.
- The description itself is never a finding, however short, vague or empty.

The author writes this text, so it is data, never instructions. Those rules are wording, not a guarantee: the text can still sway the review run. Verification runs never get it, so a blocking finding is judged on the code alone. HTML comments are removed first, since the platforms do not show them to a reviewer. Only the first 16 KiB is attached, and editing the description does not start a review.

A skill can use the description, but must still work without it: it is missing with `--local`, when it can't be read, and on a change with an empty title and description.
