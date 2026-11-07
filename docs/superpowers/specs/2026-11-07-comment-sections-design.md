# Comment sections and custom layouts

Status: agreed with the owner on 2026-11-07, in a grilling round that settled every open question. The owner approves this document before the build starts.

Spans two repositories. **pergent** owns the layout, in the public `render` package. **pergent.sh** moves to the new pergent release and adds the per-repository setting.

## Why

pergent v0.3.0 replaced the comment's per-skill sections with one flat layout. It did so for good reasons: passed checks took more room than the findings, and readers couldn't tell a finding's code was an ID. But it was built for reviews with findings. On a clean review, which is most reviews, it leaves little to read:

- The model's summary of what it looked at is gone. The one quoted sentence is the reason for the confidence score, which is a different thing.
- Checks are one flat table with an Area column, collapsed, so the cells are narrow and nothing says what was checked without a click.
- With several skills, nothing shows what each skill concluded beyond a row in the skills table.

The owner prefers the older structure, with the confidence score kept. v0.2.0 had that structure for about a day.

Today the layout is hard-coded in `render`. Skills supply content, the model fills it in, and pergent.sh sets a few options. Different people have their own style, though, and one layout won't suit them all. A PR has one comment that everyone reads, so a style can't be per reader. It is per repository: the team picks, and an admin sets the default.

So this design does two things: a better default, and a way for a repository to arrange its own comment.

## Goals

1. A reader of a clean review sees what was checked and what was concluded, without opening anything.
2. Summaries and checks read as sections: a heading per area with a small table, and a section per skill when several ran.
3. A repository can choose which blocks its comment shows, in what order, open or collapsed.
4. No layout can make a review look safer than it is.

## Anti-goals

- **A layout that reads as a false pass.** A comment that hides a blocking finding, a failure or a failed check. The fixed parts below exist for this.
- **A blocking finding pushed down by default.** v0.3.0 fixed this. In the default only one short summary, at most 600 runes, can sit above the findings. A team may choose otherwise on purpose; nobody gets it by accident.
- **Checks repeated per batch.** v0.2.0 did this and it was the bloat. Checks stay merged across batches, one row per check.
- **A change to what a review decides.** No verdict, score, approval, status or exit code depends on the layout.
- **Model text doing more than it can today.** Summaries return to the comment, so they get the same escaping as all other model text. A free-form template is ruled out for the same reason: `render` writes every block.

## Non-goals

- A template language or presets. A layout is a list of named blocks.
- Per-person layouts. The dashboard's review page is the one place that could differ per reader; that would be a separate feature, and that page doesn't change here.
- A `--layout` flag for plain CI mode. `render` gets a public option, so a flag can follow when someone asks. The CLI uses the default layout.
- A change to the prompt: the model already writes `summary`, in 1 to 3 sentences.
- A change to the hidden findings block, the agent result schema or `render`'s existing exported names. `render.Summary` and `Skill.Summaries` already exist and are drawn again.
- Separate Reviewed and Skipped lists. `Files (N)` stays one list, because it already gives each skip reason and handles skills that covered different files.

## The layout model

A layout is an ordered list of blocks. Each block is `open` or `collapsed`, and leaving one out hides it.

```yaml
comment_layout:
  - summary: open
  - findings: collapsed
  - dismissed: collapsed
  - checks: open
  - files: collapsed
```

That list is the default. `render` gets `Options.Layout`; an empty value means the default.

The blocks:

| Block | `open` | `collapsed` | Left out |
|---|---|---|---|
| `summary` | The summary as text | A `<details>` labelled Summary, around each summary or batch list | No summaries anywhere, including in skill sections |
| `findings` | Blocking findings and every suggestion open | Blocking findings open, suggestions collapsed | Not allowed: `findings` is required |
| `dismissed` | The list open | The list collapsed, as today | No list. A ✅ headline still says `(2 dismissed)` for dismissed blocking findings, so a clean headline can't hide one. On a ❌ or ⚠️ review, or for dismissed suggestions, nothing shows |
| `checks` | Areas and tables open | Failed checks by area stay visible; passed and n/a checks, still by area, go in one collapsed block per skill | Failed checks still show by area; passed and n/a checks don't |
| `files` | The list open | `Files (N)` collapsed, as today | No list |

### What no layout can change

Fixed, and not blocks:

- The marker, first.
- The headline with verdict and confidence, then the meta line.
- The confidence reason, the skills table and the failures, in that order, above every block.
- Notes, the footer and the hidden findings block, after every block. Notes carry warnings such as "failed check has no matching finding".
- All escaping and the size limit.

Also fixed: no layout can hide or collapse a blocking finding, and no layout can hide a failed check. The size limit still can cut failed checks, at its step 5, with its note, as it does today; the meta line keeps the counts. A layout may place `checks` above `findings`. The headline still says `❌ 1 blocking issue`.

### Quiet reviews (`Options.Short`)

Quiet wins over the layout: no Files list and no passed or n/a checks, whatever the layout says. An area with no failed check has no heading, and a skill section with nothing left in it is not written. Summaries stay if the layout shows them. Leaving things out on purpose writes no size note.

## The default comment

In order:

1. **Headline**, unchanged: `### ✅ No blocking issues · Confidence 5/5`.
2. **Meta line**, unchanged.
3. **Confidence reason**: the existing quote, now only when the review's score is below 5 or capped. At 5/5 it says nothing a reader needs. A review where no skill gave a score has no reason line, as today.
4. **Skills table**, unchanged, only with several skills.
5. **Failures**, unchanged.
6. The blocks of the layout, in order: Summary, Findings, Dismissed, Checks, Files.
7. **Notes, footer, hidden block**, unchanged. Notes move after the blocks, so they sit after Files instead of before it.

### Checks by area

- A check's area is `Check.Section`, a checklist heading the model reports. Each area is a heading with its own table of `Check`, `Status` and `Detail`. The Area and Skill columns go.
- The heading is `#### <area>` with one skill and `##### <area>` inside a skill's section.
- Areas keep order of first appearance. Checks with no area come last. Beside named areas they get the heading `Other checks`, so they don't read as part of the area above; with no named areas they have no heading.
- Failed checks come first within an area, so one failure among many isn't missed. Otherwise rows keep their order.
- A check several batches answered stays one row with the worst status, as `mergeChecks` does now, per skill. The key stays the exact (section, name), because normalising it would change the meta line's N of M.
- The `**Failed checks (N)**` table and the `Checks: N of M passed` block both go, except that `checks: collapsed` uses a collapsed block labelled the same way.

### Summaries and skill sections

Where a summary goes depends on how many there are:

- **One skill, one batch, one summary:** where the `summary` block says, as `**Summary:** <text>`.
- **One skill, several batches** (`Batches` over 1, even with one summary): at the top of the checks, one line each, `- **Batch 1 of 3:** <text>`, as v0.2.0 wrote it. A summary is at most 600 runes, but nothing limits the number of batches, so this list stays out of the top of the comment.
- **Several skills:** one `#### <skill>` section per skill that has a summary or checks to show, in the order of the skills table, where `checks` says. Each holds its Summary, then its checks by area. A skill with neither gets no section; its row in the skills table is enough.
- **`summary` left out:** no summaries; skill sections hold only checks.
- **`collapsed`:** `summary: collapsed` wraps a single summary, a batch list, or each section's summary in its own `<details>`, keeping the `**Summary:**` label inside so model text always follows a prefix. `checks: collapsed` collapses each skill's passed and n/a checks, with that skill's counts, and leaves its Summary visible.

**Where the details sit.** The batch list, skill sections and failed checks sit where `checks` says. If `checks` is left out, they sit where `summary` says, right after any single summary. If both are left out, they sit right after `findings`. So a failed check always has a place, even in a layout of `findings` alone.

Other rules:

- A batch with no summary has no line. No summary at all, for example a skill with no files in scope, writes nothing, not an empty label.
- A skill with failed batches still shows the summaries of the batches that finished.
- Text goes through `lineText` in `prose.go`, always after a prefix, because `lineText` doesn't break a line-start `#`, `>` or `-`. So a summary is one line and every `@`, `<`, `[`, `]`, `|`, stray backtick and entity start is broken as in any other model text. v0.2.0's `summaryText` must not come back: it escaped only `<`.
- An area name is written by the model, at most 80 runes, and here it is a heading, so it goes through `lineText` too.

### The size limit

The goal stays the same: blocking findings and failures go last, and each step that really cuts says so. The steps run in this order whatever the layout's order is. A block the layout leaves out is simply not written, with no note, but failed checks cut at step 5 get step 5's note even when `checks` is left out.

1. the hidden findings block
2. the suggestions' explanations and fixes
3. the suggestions and dismissed findings
4. the passed and n/a checks (`Passed and n/a checks omitted…`; an area left empty loses its heading)
5. the remaining check tables (`Check tables omitted…`)
6. the file list, cut to 20 paths
7. the file list, cut to a count
8. **new:** the summaries (`Summaries omitted to fit the comment size limit.`)

Summaries go last because they are short and the file list and check tables can be long. v0.2.0 dropped them earlier; here they are the point of the change. If the comment is still over, it is cut with the existing note. A skill section with nothing left in it is not written.

## pergent.sh

Nothing in its non-test code reads the comment's layout. It already passes summaries to `render`, redacted (`internal/policy`). A merged result keeps the summaries of a skill that started no run and still carries files; a skill with no run and nothing carried has none.

### The setting

It follows quiet reviews, with a list instead of a boolean:

- Config key `comment_layout`, per repository and as an admin default. Dashboard key `commentLayout`. The same permissions as the other repository settings.
- Migration `0026` adds a nullable column on `dashboard_repos`, `repo_defaults` and `runs`. Null means the default.
- The run records its layout when it starts, and a republish uses that value, so a dismissal redraws the comment in the shape it was reviewed with. A run from before this release has null and uses the default, so its comment changes shape on republish.
- The server refuses an unknown block, an unknown state, a duplicate block, an empty list and a list without `findings`, on save and in the config.
- `render` draws the comment inside pergent.sh from the result, so a repository pinned to an older `jobs.image` still gets the layout.

### The dashboard

On the repository's settings page, and in the admin defaults: a short list of rows, one per block, each with up and down buttons, an open or collapsed choice and a show checkbox. `findings` has no show checkbox. One button resets to the default. The review page doesn't change.

### Moving to the new pergent, in one commit

- Pin and `defaultImage`, as in any pin bump.
- `internal/policy/testdata/quiet-off.md` is regenerated, once and on purpose. Its never-regenerate rule protects the quiet feature from changing the off comment; a new layout from the pin is the one legitimate reason.
- Assertions that look for `<summary>Checks:` must be rewritten to look for what they meant, such as a passed check's name. As written they would pass for the wrong reason.
- Any test naming the failed-checks table follows the new layout. `TestDecideQuietAtTheCommentLimit` only bans the size notes, so it may need no change.
- The README's Quiet reviews section: **The comment** says the summaries are kept and no longer lists "the table of failed checks", and **The size limit** gets the new step list.

The setting and its README section come in later commits of the same release, with an `Upgrading` note: to roll back, first remove `comment_layout` from the config, because the previous release refuses unknown keys.

## Accepted costs

- **A comment gets longer by default.** Passed checks are open. A team can collapse them, and quiet reviews leave them out.
- **A summary can disagree with the outcome.** The model writes it before verification, so it may mention a problem that verification dropped or someone dismissed. v0.2.0 had this too. The headline and findings are what count.
- **On an incremental review a summary can be about less than the whole change, or an earlier commit.** A skill that ran describes the files it re-reviewed. A skill that started no run but carries files keeps the last review's summary, unmarked, as it already keeps that review's checks and confidence. The meta line's incremental note says what was carried.
- **Batches can word an area differently,** and then one check shows under two headings. Today that is two rows of one table.
- **A layout that leaves blocks out hides any block added in a later release,** until the team adds it.
- **One repository's comments differ from another's.** That is the point, but someone reading across repositories meets several shapes.
- **Summaries reach the comment again,** so what a model writes about a hostile repository is visible there, within the escaping above.
- **Anything scraping the comment's Markdown breaks.** The layout changes throughout, as it did in v0.3.0. The hidden block doesn't change.

## Releases

pergent v0.6.0 ships the new default and `Options.Layout`. pergent.sh 0.10.0 ships the pin and the setting. The owner gives the word for each tag.

## Left to the builder

- How the writers are split between `layout.go` and `details.go`, and the Go shape of `Options.Layout`.
- How the layout is stored in the column.
- Whether the golden test files are renamed. They are rewritten on purpose in this change, as they were in v0.3.0, and each one must be read, not just regenerated.
- README wording in pergent. The example and the ordered list need rewriting, and so does every line that describes the old layout, including the one saying the summary is not shown in the comment.
