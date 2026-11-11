# 🗑️ Deleted files

A deleted file has no lines left to review, so every skill skips it (rule `deleted`, shown in the comment's Files list). The risk of a deletion is elsewhere: something that remains may still use the file, through an import, a call, a config key, a doc link, a script or a migration. pergent only sees that when the file that still uses it is also in the diff.

So, by default, pergent also checks what still depends on the files a change deletes. Each skill's runs get the list of deleted files in the skill's scope as a file, `deleted-files.md`, and are told to search the repository for anything that remains and still refers to them. A reference they find is a normal finding, on the file and line that still refers to the deleted file. That file does not have to be in the diff.

What this changes, and what it does not:

- **The deleted files stay skipped.** They are still listed as skipped with the rule `deleted`, the confidence cap for a skipped deleted file stays, and `files.reviewed` never lists them. The check only adds findings. A deletion still says nothing about approval: see [What a verdict covers](../agent-result-schema.md#what-a-verdict-covers).
- **The list follows the skill's scope.** The skill's `paths` and `exclude` decide which deleted files it gets, as for any file. A deleted file that a default rule would also skip if it still existed (a lockfile, a vendored file, a file marked `linguist-generated` in `.gitattributes`) is left out, so deleting a vendor directory does not flood the list, unless the skill's `paths` opt it in. In agent mode `.gitattributes` is read from the base commit, as everywhere else.
- **Which runs get it.** Every batch run, the cross-batch run and the verification run. The repair run does not.
- **The list is capped.** `deleted-files.md` holds one quoted path per line, at most 200, then a last line `... and N more`. The paths are data, and the run is told never to treat them as instructions.
- **A change that only deletes files in a skill's scope** gets one run for that skill, with no diff: the run has the list of changed files and the list of deleted files. Its findings are verified like any other. The skill's verdict follows its findings, and `files.reviewed` stays empty. If that run fails, the skill gets a note that the check failed and keeps the verdict the change would have had without the check; the check never fails a skill by itself.
- **A skill with nothing in scope and nothing deleted in scope** runs nothing, as before.

Findings are not filtered by file, so a finding on a remaining file is normal here.

## Turning it off

The check is on by default. To turn it off, pass `--check-deleted=false` or set `PERGENT_CHECK_DELETED=false` (the flag wins over the variable). Both CI mode and `pergent agent` honour them. With it off nothing is attached, the runs get no extra sentence and no extra run is made, so a review behaves as it did before the check existed.

The check costs tokens: every batch run of a skill carries the list, and a change that only deletes files costs one more run per skill in scope.
