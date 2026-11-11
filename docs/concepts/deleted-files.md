# 🗑️ Deleted files

A deleted file has no lines left to review, so every skill skips it (rule `deleted`, shown in the comment's Files list). The risk of a deletion is elsewhere: something that remains may still use the file, through an import, a call, a config key, a doc link, a script or a migration. pergent only sees that when the file that still uses it is also in the diff.

So, by default, pergent also checks what still depends on the files a change deletes. Each skill's runs get the list of deleted files in the skill's scope as a file, `deleted-files.md`, and are told to search the repository for anything that remains and still refers to them. A reference they find is a normal finding, on the file and line that still refers to the deleted file. That file does not have to be in the diff.

What this changes, and what it does not:

- **The deleted files stay skipped.** They are still listed as skipped with the rule `deleted`, the confidence cap for a skipped deleted file stays, and `files.reviewed` never lists them. The check only adds findings. A deletion still says nothing about approval: see [What a verdict covers](../agent-result-schema.md#what-a-verdict-covers).
- **The list follows the skill's scope.** The skill's `paths` and `exclude` decide which deleted files it gets, as for any file. A deleted file that a default rule would also skip if it still existed (a lockfile, a vendored file, a file marked `linguist-generated` in `.gitattributes`) is left out, so deleting a vendor directory does not flood the list, unless the skill's `paths` opt it in. In agent mode `.gitattributes` is read from the base commit, as everywhere else.
- **Which runs get it.** One run per skill carries the list: the skill's first batch (so the same references are not searched for once per batch), and its verification run. The cross-batch run and the repair run do not. The run that carries it is told it may report on a file outside its diff, as the list asks.
- **The list is capped.** `deleted-files.md` holds one quoted path per line, at most 200, then a last line `... and N more`. The paths are data, and the run is told never to treat them as instructions.
- **A skill with deleted files but nothing to batch** (the change only deletes in its scope, or what else it changes there is carried, too large or has no diff) gets one run, with no diff: the run has the list of changed files and the list of deleted files. Its findings are verified like any other. The skill's verdict follows its findings, and `files.reviewed` stays empty. A run that finds nothing leaves the note `Checked N deleted files for remaining references.`
- **A check that fails fails the skill.** If a run that carries only the deleted-files check fails, or never starts, the skill's verdict is `failed` (see [When a review fails](failed-reviews.md)), with a failure that names no file, so the file lists do not change. An unchecked deletion is not a pass. If the list cannot be written for a batch or verification run, the run goes on without it and the skill gets the note `The list of deleted files could not be attached to every run.`
- **A skill with nothing in scope and nothing deleted in scope** runs nothing, as before.

Findings are not filtered by file, so a finding on a remaining file is normal here.

## Turning it off

The check is on by default. To turn it off, pass `--check-deleted=false` or set `PERGENT_CHECK_DELETED=false` (the flag wins over the variable). Both CI mode and `pergent agent` honour them. With it off nothing is attached, the runs get no extra sentence and no extra run is made, so a review behaves as it did before the check existed.

The check costs tokens: a skill's first batch and its verification run carry the list, and a change with nothing to batch in a skill's scope costs one more run per skill.
