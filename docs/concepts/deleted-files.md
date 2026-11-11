# 🗑️ Deleted files

By default a deleted file is skipped: it has no lines left to read, so every skill skips it (rule `deleted`, shown in the comment's Files list). Two risks stay unseen. The deletion itself can be the risk (a removed auth check, a validation, a test, a migration). And something that remains may still use the file, through an import, a call, a config key, a doc link or a script. A rename has the second risk too: the old path can still be used.

An opt-in switch makes pergent look at both. It is **off by default**.

## Turning it on

Pass `--check-deleted`, or set `PERGENT_CHECK_DELETED=true`. The flag wins over the variable. Both CI mode and `pergent agent` honour them. A value in the variable other than `true` or `false` is a config error.

With the switch off, nothing below happens: no extra file is attached, the runs get no extra sentence, and a review behaves as it did before the switch existed.

It costs more tokens. Removed lines go into the batches like any other changed lines, every batch with a deleted file also searches the repository, and a change with nothing else to batch costs one more run per skill.

## What it does

- **A deleted file in a skill's scope is reviewed like a changed file.** Its removed lines go into the skill's batches, and the run is told the file is deleted and to judge whether removing that code is safe. On a deleted file, a finding's `line` is the line in the deleted file, and the run is told to give no `suggestion`.
- **Each batch with deleted files also checks references.** It is told to search the repository for anything that remains and still refers to those files, and to raise a finding on the file and line that still does. That file does not have to be in the diff. The deleted paths in the message are quoted data, never instructions.
- **One list covers what no batch carries.** It is attached as `deleted-files.md` to the skill's first batch, one entry per line, at most 200, then a last line `... and N more`. It holds:
  - pure renames, as `"old" renamed to "new"`. A rename is in scope for a skill when its old path or its new path is. It is left off the list when the old path is created again in the same change, and when it is a copy. A rename that also changes content is reviewed as any changed file.
  - deletions that cannot be batched (see below).

  The verification run gets the same list, with every deleted file in the skill's scope. The cross-batch run and the repair run do not get it.
- **A skill with nothing to batch** (the change only deletes or renames in its scope, or what else it changes there is carried, too large or has no diff) gets one run with no diff. The run has the list of changed files and `deleted-files.md`. Its findings are verified like any other. A run that finds nothing leaves a note such as `Checked 2 deleted files and the old path of 1 renamed file for remaining references.`
- **Verification sees the removed lines.** A blocking finding on a deleted file is checked by a verification run that gets that file's patch as `deleted.patch`, because the file is gone from the checkout. If the patch cannot be written, the verification runs without it and the skill gets the note `The diffs of deleted files could not be attached to the verification run.`

## What is skipped anyway

- **A deletion that cannot be batched keeps the rule `deleted`:** a binary file, an empty file, or a file the platform sent with no content. It goes on the reference list, but the check does not make it reviewed.
- **Vendored, lockfile and generated deletions keep their own rule** (`vendored`, `lockfile`, `generated`), as a changed file does, unless the skill's `paths` opt them in. A deleted file's generated header is read from its removed lines. In agent mode `.gitattributes` is read from the base commit, as everywhere else.
- **A file outside the skill's `paths` and `exclude`** is `out of scope` or `excluded by skill` for that skill, as any file.
- **A deleted file larger than a batch** is not reviewed and is reported as a failure for that file, like any file that is too large.

## What counts as reviewed

- A deleted file counts as reviewed only when its batch finished. It is then in `files.reviewed`, and agent mode also lists it in `files.deleted`.
- A pure rename counts as reviewed only when a finished run saw the list. It is then in `files.reviewed` under its new path, and agent mode also lists it in `files.renamed`.
- A failed or unstarted batch that carries deleted files, or a failed or unstarted run that carries the list, fails the skill (see [When a review fails](failed-reviews.md)). An unchecked deletion is not a pass.

## The cap

The list holds 200 entries. Renames past that stay skipped as `no content changes`, and the skill gets the note `N renamed files were not checked for references.` The same note counts the renames on a list that no finished run saw. A pure rename never blocked approval, so this is no weaker than before. Deletions past the cap keep the rule `deleted`, as before.

If the list cannot be written for a run, that run goes on without it and the skill gets the note `The list of deleted and renamed files could not be attached to every run.`

## In the comment

- A finding on a reviewed deleted file shows as `path (deleted), line N` (or `lines N–M`), with no link and no suggested change, because the line is not in the head version.
- The Files list marks `(deleted)` and `(renamed from old/path)` after the path.

For the result of `pergent agent`, see [What a verdict covers](../agent-result-schema.md#what-a-verdict-covers).
