# Comment sections: findings first, details readable

Status: layout agreed with the owner on 2026-11-07. The owner approves this document before the build starts.

Spans two repositories. **pergent** owns the layout, in the public `render` package. **pergent.sh** moves to the new pergent release and updates what its tests and README say about the comment.

## Why

pergent v0.3.0 replaced the comment's per-skill sections with one flat layout. It did so for good reasons: passed checks took more room than the findings, and readers couldn't tell a finding's code was an ID. But it was built for reviews with findings. On a clean review, which is most reviews, it leaves little to read:

- The model's summary of what it looked at is gone. The one quoted sentence is the reason for the confidence score, which is a different thing.
- Checks are one flat table with an Area column, collapsed, so the cells are narrow and nothing says what was checked without a click.
- With several skills, nothing shows what each skill concluded beyond a row in the skills table.

The owner prefers the older structure, with the confidence score kept. v0.2.0 had that structure for about a day. This design brings it back without the things that made v0.3.0 necessary.

## Goals

1. A reader of a clean review sees what was checked and what was concluded, without opening anything.
2. Summaries and checks read as sections: a heading per area with a small table, and a section per skill when several ran.
3. The headline with verdict and confidence stays first.
4. Findings stay above everything else that follows the header, so nothing buries a blocking finding.

## Anti-goals

- **A blocking finding pushed down the comment.** This is what v0.3.0 fixed. It would make the change a failure even if the layout looks right.
- **Checks repeated per batch.** v0.2.0 did this and it was the bloat. Checks stay merged across batches, one row per check.
- **A change to what a review decides.** No verdict, score, approval, status or exit code depends on this.
- **A model's text doing more than it can today.** Summaries return to the comment, so they get the same escaping as all other model text.

## Non-goals

- No option or setting to choose a layout. There is one layout.
- No change to the prompt: the model already writes `summary`, in 1 to 3 sentences.
- No change to the hidden findings block, the agent result schema or `render`'s existing exported names. `render.Summary` and `Skill.Summaries` are already there and are drawn again.
- Separate Reviewed and Skipped lists don't come back (see the layout, step 10).

## The layout

In order. Steps marked *unchanged* render as they do today.

1. **Headline**, *unchanged*: `### ✅ No blocking issues · Confidence 5/5`.
2. **Meta line**, *unchanged*.
3. **Summary, one skill with one summary only.** See "Summaries" below. Several batches' summaries go in the details, step 9, so a long list never sits above a blocking finding.
4. **Confidence reason**: the existing quote, now only when the review's score is below 5 or capped. At 5/5 it says nothing a reader needs. A review where no skill gave a score has no reason line, as today.
5. **Skills table**, *unchanged*, only with several skills.
6. **Failures**, *unchanged*.
7. **Findings**, *unchanged*: one list across all skills, blocking first and open, suggestions collapsed.
8. **Dismissed list**, *unchanged*.
9. **Details**:
   - With one skill: the batch summaries if there are several, then the checks by area, with no skill heading.
   - With several skills: one `#### <skill>` section per skill that has a summary or checks, in the order of the skills table, holding its Summary and then its checks by area. A skill with neither gets no section; its row in the skills table is enough.
10. **Notes, Files, footer, hidden block**, *unchanged*. `Files (N)` stays one collapsed list, because it already gives each skip reason and handles skills that covered different files.

### Checks by area

- A check's area is `Check.Section`, a checklist heading. Each area is a heading with its own table of `Check`, `Status` and `Detail`. The Area and Skill columns go.
- The heading is `#### <area>` with one skill and `##### <area>` inside a skill's section.
- Areas keep order of first appearance. Checks with no area come last, in a table with no heading.
- Tables are open, with no `<details>` around them. Passed, failed and n/a checks share their area's table.
- Failed checks come first within an area, so one failure among many isn't missed. Otherwise rows keep their order.
- A check several batches answered stays one row with the worst status, as `mergeChecks` does now, per skill. The key stays the exact (section, name), because normalising it would change the meta line's N of M.
- The `**Failed checks (N)**` table and the `Checks: N of M passed` block both go.

### Summaries

- One summary: `**Summary:** <text>`.
- Several batches: one line each, `- **Batch 1 of 3:** <text>`, as v0.2.0 wrote it, always in the details. A batch with no summary has no line. A summary is at most 600 runes, but nothing limits the number of batches, which is why this list stays below the findings.
- No summary, for example a skill with no files in scope or a run that failed: nothing is written, not an empty label.
- A skill with failed batches still shows the summaries of the batches that finished.
- Text goes through `lineText` in `prose.go`, so a summary is one line and every `@`, `<`, `[`, `]`, `|`, stray backtick and entity start is broken as in any other model text. v0.2.0's `summaryText` must not come back: it escaped only `<`.
- An area name is written by the model, at most 80 runes, and here it is a heading, so it goes through `lineText` too.

### Quiet reviews (`Options.Short`)

The same structure, with the same rule as today: no Files list and no passed or n/a checks. An area with no failed check has no heading, and a skill section with no summary and no failed check is not written. Summaries stay, because they are short and are the point of this change. "Nothing was cut" still holds: leaving these out on purpose writes no size note.

### The size limit

The goal stays the same: blocking findings and failures go last, and each step that really cuts says so. The steps, in order:

1. the hidden findings block
2. the suggestions' explanations and fixes
3. the suggestions and dismissed findings
4. the passed and n/a checks (`Passed and n/a checks omitted…`; an area left empty loses its heading)
5. the remaining check tables (`Check tables omitted…`)
6. the file list, cut to 20 paths
7. the file list, cut to a count
8. **new:** the summaries (`Summaries omitted to fit the comment size limit.`)

Summaries go last because they are short and the file list and check tables can be long. v0.2.0 dropped them earlier; here they are the point of the change. If the comment is still over, it is cut with the existing note, as it is now. A skill section with nothing left in it is not written.

## Accepted costs

- **A comment gets longer.** Passed checks are open. Findings sit above them, and quiet reviews leave them out.
- **A summary can disagree with the outcome.** The model writes it before verification, so a summary may mention a problem that verification dropped, or that someone dismissed. v0.2.0 had this too. The headline and findings are what count.
- **On an incremental review a summary can be about less than the whole change, or an earlier commit.** A skill that ran describes the files it re-reviewed. A skill that started no run but carries files keeps the last review's summary, unmarked, as it already keeps that review's checks and confidence. The meta line's incremental note says what was carried.
- **Batches can word an area differently,** and then one check shows under two headings. Today that is two rows of one table.
- **Summaries reach the comment again,** so what a model writes about a hostile repository is visible there, within the escaping above.
- **Anything scraping the comment's Markdown breaks.** The layout changes throughout, as it did in v0.3.0. The hidden block doesn't change.

## pergent.sh

Nothing in its non-test code reads the comment's layout. It already passes summaries to `render`, redacted (`internal/policy`). A merged result keeps the summaries of a skill that started no run and still carries files; a skill with no run and nothing carried has none.

When it moves to the new pergent release, in one commit:

- Pin and `defaultImage`, as in any pin bump.
- `internal/policy/testdata/quiet-off.md` is regenerated, once and on purpose. Its rule of never regenerating protects the quiet feature from changing the off comment; a new layout from the pin is the one legitimate reason.
- Assertions that look for `<summary>Checks:` must be rewritten to look for what they meant, such as a passed check's name. As written, they would pass for the wrong reason, because that block no longer exists anywhere.
- Any test naming the failed-checks table follows the new layout. `TestDecideQuietAtTheCommentLimit` only bans the size notes, so it may need no change.
- The README's Quiet reviews section: **The comment** says the summaries are kept and no longer lists "the table of failed checks", and **The size limit** gets the new step list.

## Releases

pergent first, as a minor version, since the comment's Markdown changes throughout. Then pergent.sh moves to it. The owner names both versions and gives the word for each tag.

## Left to the builder

- How the writers are split between `layout.go` and `details.go`.
- Whether the golden test files are renamed. They are rewritten on purpose in this change, as they were in v0.3.0, and each one must be read, not just regenerated.
- README wording in pergent. The example and the ordered list need rewriting, and so does every line that describes the old layout, including the one saying the summary is not shown in the comment.
