# Comment Sections and Custom Layouts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Implementers run on **Sonnet** with the `pergent-developer` agent. Task reviews and the final branch review run on **Opus**.

**Goal:** The review comment gets readable sections by default, and a repository can choose which blocks it shows, in what order, open or collapsed.

**Architecture:** `render` draws blocks from `Options.Layout`, and owns parsing and checking a layout, so no other code knows the block names. pergent.sh carries a layout as a list of `block:state` strings, the same way it carries `skills`: `text[]` columns and `[]string` fields, nil meaning the default. It reads the setting at dispatch, records it on the run and hands it to `render` at publish.

**Tech Stack:** Go (`render` stays stdlib only), Postgres migrations, React with Vite and vitest.

**Spec:** `docs/superpowers/specs/2026-11-07-comment-sections-design.md`. Read its anti-goals first. They are the failure bar.

## Hand-offs

Implementers have no network, so these steps belong to the owner's main session.

| When | Who | What |
|---|---|---|
| **H0**, before Task 1 | Main session | In `pergent-incremental`, cut `feat/comment-layout` from `docs/comment-sections`. |
| **H1 (STOP)**, after Task 2 | Owner, then main session | Push and open the pergent PR when the owner says so. After the merge, run the local gate on the merge commit and tag `v0.6.0` on the owner's word. Check the image. |
| **H2**, before Task 3 | Main session | In `pergent.sh-quiet`, cut `feat/comment-layout` from `origin/main`, then cherry-pick the spec and this plan. Run `go get github.com/gopaytech/pergent@v0.6.0` and `go mod tidy`, set `defaultImage` and the assertion in `internal/config/config_test.go`, and **leave them uncommitted**: Task 3 commits them with its test changes, so the pin and what it changes stay one commit. |
| **H3**, after the final review | Owner | Push and open the PR on the owner's word. Postgres-backed tests first run here. Check the `internal/store`, `internal/dashboard` and `internal/engine` timings in the `test` job log. |
| **H4**, after the merge | Owner, then main session | The usual chart release for 0.10.0, including `info.version` in `openapi.yaml`. |

## Global Constraints

- Never run docker, orbctl or anything that starts a container. No `gh`, `glab`, `curl` or other network step in a task. No push, tag or PR. No disk or cache cleanup. Delete nothing outside your own scratch.
- Tests marked **[CI]** need Postgres. Write them with the same care, confirm they compile with `go vet ./...`, and report them as not run.
- **pergent gate:** `gofmt -l .`, `go vet ./...`, `go test ./...`. **pergent.sh Go gate:** `go vet ./...`, `go test ./...`. **Web gate:** `cd web && npm run typecheck && npm run lint && npx vitest run && npm run build`, not while `go test` runs.
- **Private-name scan** before every commit, in each repo, must print nothing: `git grep -niE 'gol[a]bs|goj[e]k|gop[a]y[^t]|got[o]\b' -- . ':!go.sum' ':!docs/superpowers/plans/*' ':!web/package-lock.json'`
- Commit with the owner's email as the agent definition says, staged by path, ending with a `Co-Authored-By:` line naming the model that wrote the change. Gate the commit on its checks with `&&`.
- `render` imports no pergent `internal/` package and stays stdlib only. Its existing exported names keep their meaning.
- Goldens change on purpose in Tasks 1 and 3. Read each changed golden before committing it; a regenerated file nobody read is not a passing test. Elsewhere, never edit a test or golden to make a change pass, except to add a new field to a fixture.
- The README is the behaviour contract: change it in the same commit as the behaviour.
- Names: config key `comment_layout`, dashboard key `commentLayout`, migration `0026`, column `comment_layout`.
- No decision, verdict, score, status or exit code may depend on the layout.

## Review Focus

1. **A stored layout this version can't read** (a block renamed later, a rollback and upgrade): `Comment` falls back to the default, never panics and never loses a finding.
2. **A minimal layout, `findings:collapsed` alone, on a review with a failed check and several skills:** the failed check is visible, under its skill.
3. **A republish after the setting changed:** the comment follows the run in both directions, and a run with NULL gets the default.
4. **An incremental merge or a `review failed` subset:** the run's layout survives to the merged comment.
5. **A custom order over the size limit:** with `checks` above `findings`, blocking findings still outlast every other block, and a failed-check cut leaves its note even when `checks` is left out.

---

### Task 1: The new default comment (pergent)

**Files:**
- Create: `render/sections_test.go`
- Modify: `render/render.go`, `render/layout.go`, `render/details.go`, `render/testdata/golden/*`, `internal/output/testdata/*` as generated, `render/short_test.go`, `render/layout_test.go`, `README.md`, `CLAUDE.md`

**Interfaces:**
- Produces: unexported `writeSummary`, `writeDetails(b, skills, o, l)` (batch lists, skill sections and checks by area), `layout.noSummaries`, const `summariesDroppedNote = "> Summaries omitted to fit the comment size limit."`. Task 2 calls these from the block loop.

The order is fixed in this task, as the spec's "The default comment": header (reason, skills table, failures), Summary, Findings, Dismissed, details, Files, Notes, footer, hidden block.

- [ ] **Step 1: Write the failing tests in `render/sections_test.go`.**
  - `TestSummaryOneBatch`: `**Summary:** text` is after the failures and before `#### Findings`.
  - `TestSummaryBatchList`: `Batches: 3`, two summaries: `- **Batch 1 of 3:** ` and `- **Batch 3 of 3:** ` appear after the findings and before the first area heading; no `**Summary:**`.
  - `TestBatchesThreeOneSummary`: `Batches: 3`, one summary is a batch list.
  - `TestNoSummaryWritesNothing`: no `Summary` string.
  - `TestSummaryIsInert`: text `# x\n> y @alice <img src=a> [l](u)` is one line after the `**Summary:**` prefix, with no line starting `#` or `>`, no live `@alice`, no live `<img`.
  - `TestAreaHeadingIsInert`: `Section: "# x\n@bob <b>"` writes one heading line, with no live `@bob` or `<b>`.
  - `TestReasonOnlyBelowFive`: score 5 uncapped has no `> ` reason; score 4 and a capped score have it; a model giving no score has none.
  - `TestChecksByArea`: `#### Plan` then `| Check | Status | Detail |`; no `<summary>Checks:`, no `Failed checks (`, no `| Area |`.
  - `TestFailedFirstInArea`: pass, fail, n/a in one area render fail first, then pass, then n/a; areas keep first-appearance order.
  - `TestChecksWithNoAreaLast`.
  - `TestBatchesStayOneRow`: a check two batches answered is one row with the worst status.
  - `TestSkillSections`: two skills: `#### ` sections follow the skills table's order, each Summary then `##### Plan`; a skill with no summary and no checks has no section.
  - `TestNotesAfterFiles`.
  - `TestSummariesCutLast`: at a limit that forces step 8 the note `summariesDroppedNote` is there and the blocking title stays; at a looser limit the file list is cut and the summary stays.
  - `TestEmptyAreaLosesItsHeading`: at step 4, an all-passed area has no heading and a failed area keeps its own.
  - `TestEmptySectionIsNotWritten`: at step 5, a skill with nothing left has no `#### ` section.
  - `TestShortKeepsSummaryAndFailedChecks`: `Short`: a Summary, failed rows under their area; no passed row, no heading for an all-passed area, no `omitted` note.

- [ ] **Step 2: Run** `go test ./render -run 'Summary|Batch|Area|Reason|Checks|Skill|Notes|Empty|Short'`. Expected: FAIL.

- [ ] **Step 3: Implement.** `render()` calls the writers in the new order. `writeReason` returns early when the score is 5 and uncapped. A single summary is `Batches <= 1 && len(Summaries) == 1`. All model text goes through `lineText` after a prefix; don't restore `summaryText`. Check rows come from `mergeChecks([]Skill{s})` per skill and are grouped by `Section`. Add `{..., noSummaries: true}` as the last entry of the steps in `Comment`, and update its doc comment. `writeChecks`, `writeCheckTable`'s Skill column and the `Checks:` block go, except what Task 2 needs for `checks: collapsed`: keep the label format `Checks: %s` in a helper.

- [ ] **Step 4: Regenerate the goldens** with `go test ./internal/output -update-goldens -update-golden-inputs` and `go test ./render -update-goldens`. Read every changed file. `render/short_test.go` and `render/layout_test.go` assertions that named the old layout change to what they meant; say which in the report.

- [ ] **Step 5: Update the README** review-comment section (list, example, and every line on the old layout, including "not shown in the comment") and the `render/` line in `CLAUDE.md`.

- [ ] **Step 6: Run the pergent gate.** Expected: PASS.

- [ ] **Step 7: Scan and commit:** `feat(render): sections for summaries and checks, findings first`.

---

### Task 2: `Options.Layout` (pergent)

**Files:**
- Create: `render/blocks.go`, `render/blocks_test.go`
- Modify: `render/render.go`, `render/layout.go`, `render/details.go`, `README.md`, `CLAUDE.md`

**Interfaces:**
- Consumes: Task 1's writers.
- Produces, in `render/blocks.go`:
  - `const BlockSummary, BlockFindings, BlockDismissed, BlockChecks, BlockFiles` = `"summary"`, `"findings"`, `"dismissed"`, `"checks"`, `"files"`; `const StateOpen, StateCollapsed` = `"open"`, `"collapsed"`.
  - `type Block struct{ Name, State string }`, with `func (b Block) String() string` giving `"summary:open"`.
  - `func DefaultLayout() []Block`: summary open, findings collapsed, dismissed collapsed, checks open, files collapsed.
  - `func ParseLayout(entries []string) ([]Block, error)`: parses `block:state`. Errors name the bad entry. Refuses an unknown block, an unknown state, a duplicate, an empty list and a list without `findings`.
  - `Options.Layout []Block`: nil means the default.

- [ ] **Step 1: Write the failing tests in `render/blocks_test.go`.**
  - `TestParseLayout`: a table of the five refusals plus a valid list round-tripping through `String`.
  - `TestDefaultLayoutIsTheNilLayout`: `Comment` with `DefaultLayout()` equals `Comment` with nil, on the golden fixtures.
  - `TestLayoutOrder`: `checks:open, findings:collapsed`: the first area heading comes before `#### Findings`; the headline is still first.
  - `TestFindingsOpen`: `findings:open`: suggestions' `<details open>`. `findings:collapsed`: blocking open, suggestions not.
  - `TestSummaryCollapsed`: a `<details>` labelled Summary that still holds the `**Summary:**` prefix, for a single summary, a batch list and each skill section.
  - `TestChecksCollapsed`: failed rows outside the `<details>`, passed rows inside it, still by area, summary line `Checks: 3 of 4 passed, 1 n/a`; with two skills, one block per skill with that skill's counts, and the Summary outside.
  - `TestDismissedAndFilesOpen`: `dismissed:open` and `files:open` write `<details open>` around each list.
  - `TestLeftOutBlocks`: leaving out `dismissed`, `files` or `summary` writes none of it and no `omitted` note. A ✅ headline still says `(1 dismissed)`.
  - `TestChecksLeftOutKeepsFailed` (Review Focus 2): `findings:collapsed` alone, two skills, one failed check: the row is shown under its skill, after the findings, with no passed rows.
  - `TestDetailsFallBack`: `summary:open, findings:collapsed`, one skill, one summary, a failed check: the failed check follows the summary. With a batch list and `checks` left out, the list sits at `summary`'s place.
  - `TestQuietWinsOverLayout`: `Short` with `files:open, checks:open`: no Files list, no passed rows, no `omitted` note.
  - `TestInvalidLayoutFallsBack` (Review Focus 1): `Options.Layout` with an unknown block, a duplicate, and no `findings`: the output equals the default comment, with no panic.
  - `TestCustomOrderAtEverySize` (Review Focus 5): `checks:open, findings:collapsed`, limits from 100 to full size in steps: every output is within the limit, and at every limit where the default layout keeps the blocking title, this layout keeps it too.
  - `TestFailedCutLeavesItsNote` (Review Focus 5): `checks` left out, a limit forcing step 5: `Check tables omitted` is there. `Short` with no failed checks: no note.

- [ ] **Step 2: Run** `go test ./render -run 'Layout|Findings|Collapsed|LeftOut|Quiet|Details|Custom|Failed'`. Expected: compile failure on `Block`.

- [ ] **Step 3: Implement.** `render()` loops over the blocks. A private `checkLayout(blocks []Block) error` holds the rules, shared with `ParseLayout`; `Comment` uses `DefaultLayout()` when it fails or the list is empty. Details sit at `checks`, else `summary` after any single summary, else right after `findings`. Size-limit steps don't depend on the order.

- [ ] **Step 4: Update `README.md`** (a short "Layouts" note: `render.Options.Layout` is for hosts, the CLI uses the default) and the `render/` line in `CLAUDE.md`.

- [ ] **Step 5: Run the pergent gate.** Expected: PASS, with no golden changed in this task.

- [ ] **Step 6: Scan and commit:** `feat(render): Options.Layout, blocks a caller can order and fold`.

**STOP: H1, then H2.** Do not start Task 3 until `v0.6.0` is released and H2's changes are in the working tree. Confirm with `grep 'gopaytech/pergent ' go.mod` and `git status --short`.

---

### Task 3: Move to pergent v0.6.0 (pergent.sh)

**Files:**
- Modify: `go.mod`, `go.sum`, `internal/config/config.go`, `internal/config/config_test.go` (all from H2), `internal/policy/testdata/quiet-off.md`, `internal/policy/policy_test.go`, `internal/engine/publish_test.go`, `README.md`

- [ ] **Step 1: Run** `go test ./...` and list what fails. Expected: tests that depend on the old layout, and `TestCommentOffMatchesBeforeQuiet`.

- [ ] **Step 2: Regenerate `quiet-off.md`** with `-update-quiet-off`, once, and read it against the spec's default comment. The never-regenerate rule has one exception, and this is it.

- [ ] **Step 3: Rewrite the assertions.** Any `<summary>Checks:` check becomes a check for a passed check's name, in both directions: `TestDecideQuietComment` must fail if a passed check is shown. Tests naming the failed-checks table follow the new layout. Don't touch `TestDecideQuietAtTheCommentLimit` unless it fails.

- [ ] **Step 4: Update `README.md`.** Quiet reviews: **The comment** says the Summary is kept and drops "the table of failed checks"; **The size limit** gets the eight steps. Fix any other sentence describing the old layout.

- [ ] **Step 5: Run the Go gate.** Expected: PASS.

- [ ] **Step 6: Scan and commit,** staging H2's files too: `chore(deps): pergent v0.6.0`.

---

### Task 4: The setting: migration, config, resolver, store (pergent.sh)

**Files:**
- Create: `internal/store/migrations/0026_comment_layout.sql`
- Modify: `internal/config/config.go`, `internal/settings/settings.go`, `internal/store/dashboard.go`, `internal/store/instances.go`, `internal/store/runs.go`, `README.md`
- Test: `internal/config/config_test.go`, `internal/settings/settings_test.go`, `internal/store/dashboard_test.go`, `internal/store/instances_test.go`, `internal/store/rerun_test.go`

**Interfaces:**
- Consumes: `render.ParseLayout`.
- Produces: `settings.KeyCommentLayout = "comment_layout"`; `CommentLayout []string` on `settings.Effective`, `config.Repository`, `config`'s defaults, `store.DashboardRepo`, `store.RepoDefaults` and `store.Run`, nil meaning the default; `Store.SetRunLayout(ctx, runID, layout []string) error`, accepting a `running` run as `SetRunQuiet` does; `config.LayoutEntries` type with `UnmarshalYAML` turning `- summary: open` into `"summary:open"`.

Follow `quiet_reviews` for where each field goes, and `skills` for how a list is scanned and stored. The migration adds `comment_layout text[]` to `dashboard_repos`, `repo_defaults` and `runs`, with comments as in `0025`.

- [ ] **Step 1: Write the failing tests.**
  - `TestCommentLayoutYAML`: `- summary: open` and `- findings: collapsed` load as `["summary:open", "findings:collapsed"]`; an entry with two keys is an error.
  - `TestCommentLayoutRefused`: an unknown block, an unknown state, a duplicate, an empty list and no `findings` each fail config load, on a repository and in the defaults, and name the entry.
  - `TestResolveCommentLayout`: nil falls through to the admin default, then to nil; a repository value sets `Overridden[KeyCommentLayout]`.
  - **[CI]** `TestDashboardRepoCommentLayout`, `TestRepoDefaultsCommentLayout`: insert, update and upsert keep the list and its order; nil stays NULL.
  - **[CI]** `TestSetRunLayout`: a running run stores the list; an unset run reads nil; `Run.CommentLayout` survives `Run` and the rerun copy paths that `SetRunQuiet` covers.

- [ ] **Step 2: Run** `go test ./internal/config ./internal/settings`. Expected: FAIL.

- [ ] **Step 3: Implement** as described above. Config load calls `render.ParseLayout` and wraps its error.

- [ ] **Step 4: Update `README.md`:** the settings table row and config reference, with the five refusals.

- [ ] **Step 5: Run the Go gate.** Expected: PASS, [CI] tests compiling.

- [ ] **Step 6: Scan and commit:** `feat(settings): add comment_layout, recorded on the run`.

---

### Task 5: The run uses its layout (pergent.sh)

**Files:**
- Modify: `internal/engine/dispatch.go`, `internal/engine/publish.go`, `internal/policy/policy.go`, `README.md`, `CLAUDE.md`
- Test: `internal/policy/policy_test.go`, `internal/engine/dispatch_test.go`, `internal/engine/publish_test.go`, `internal/engine/e2e_dashboard_test.go`

**Interfaces:**
- Consumes: `store.SetRunLayout`, `store.Run.CommentLayout`, `render.ParseLayout`.
- Produces: `policy.Outcome.Layout []string`.

- [ ] **Step 1: Write the failing tests.**
  - `TestRenderOptionsLayout`: `Layout: ["checks:open", "findings:collapsed"]` gives `opts.Layout` of those two blocks. Nil gives nil.
  - `TestRenderOptionsBadLayout` (Review Focus 1): a list `ParseLayout` refuses gives nil, so the default.
  - `TestDecideLayoutChangesOnlyTheComment`: for blocking, clean, failed skill, nothing reviewed, draft and approval off, `Status`, `Description`, `Approve` and `Kind` equal the default's, under a layout of `findings:collapsed` alone.
  - `TestOutcomeCarriesTheRunsLayout` (engine, no database).
  - **[CI]** `TestDispatchRecordsTheLayout`: the run holds the repository's effective layout before `Launch`, and a default repository holds NULL.
  - **[CI]** `TestPublishLayoutFollowsTheRun` (Review Focus 3): a run with a layout, the setting changed: the comment follows the run; a NULL run gets the default, with `Summary` first.
  - **[CI]** `TestDismissRepublishKeepsTheLayout` (Review Focus 3), in the dashboard e2e: a run with `findings:collapsed` alone, the setting cleared, a dismissal: the republished comment still has no Files list.
  - **[CI]** `TestPublishMergedKeepsTheLayout` (Review Focus 4): an incremental run with a layout, over a base run with none: the merged comment follows it. Also a `review failed` subset.

- [ ] **Step 2: Run** `go test ./internal/policy`. Expected: compile failure on `Layout`.

- [ ] **Step 3: Implement.** In `dispatch`, call `SetRunLayout` beside `SetRunQuiet`, outside `if push` and before `Launch`. `Engine.outcome` sets `Layout: run.CommentLayout`. `renderOptions` parses it and leaves `opts.Layout` nil on an error, logging nothing: a bad value here means a later release's block, and the fallback is the design.

- [ ] **Step 4: Update `README.md`:** a **Comment layout** section (the blocks and states, what no layout can change, quiet wins, the run keeps its layout, the accepted costs) and `### Upgrading to 0.10.0` at the top of Upgrading: the default comment changes for every repository, the default `jobs.image` is `ghcr.io/gopaytech/pergent:v0.6.0`, migration `0026` adds three nullable columns and 0.9.0 keeps working on the migrated database, and to roll back first remove `comment_layout` from the config, because the previous release refuses unknown keys. Add the setting to `CLAUDE.md`.

- [ ] **Step 5: Run the Go gate.** Expected: PASS.

- [ ] **Step 6: Scan and commit:** `feat(engine): a review's comment follows its recorded layout`.

---

### Task 6: Dashboard API and control (pergent.sh)

**Files:**
- Create: `web/src/components/CommentLayoutEditor.tsx`, `web/src/components/CommentLayoutEditor.test.tsx`
- Modify: `internal/dashboard/api_operator.go`, `api_read.go`, `api_write.go`, `view.go`, `openapi.yaml`, `web/src/api.ts`, `web/src/pages/AdminSettings.tsx`, `web/src/pages/RepositorySettings.tsx`, `web/src/test/fixtures.ts`, `README.md`
- Test: `internal/dashboard/api_operator_test.go`, `api_read_test.go`, `api_write_test.go`, `web/src/pages/AdminSettings.test.tsx`, `web/src/pages/RepositoryDetail.test.tsx`

**Interfaces:**
- Consumes: `settings.KeyCommentLayout`, `CommentLayout` fields, `render.ParseLayout`, `render.DefaultLayout`.
- Produces: dashboard key `commentLayout`, a `string[]` of `block:state`, null meaning the default, in settings views, `DefaultValues`, `DefaultsView`, both request bodies and `msgBadSettings`, `msgBadDefaults`; `<CommentLayoutEditor value={string[] | null} onChange={...} />`.

- [ ] **Step 1: Write the failing tests.**
  - **[CI]** `TestPutCommentLayout`, `TestPutDefaultsCommentLayout`: a valid list saves and reads back in order; null resets; each of the five refusals is 400 with `msgBadSettings` or `msgBadDefaults`.
  - The exact key-set tests in `api_read_test.go` and `api_operator_test.go` gain the key.
  - `CommentLayoutEditor.test.tsx`: renders five rows in order; the up button moves a row and calls `onChange`; `findings` has no show checkbox; unticking `files` removes it from the value and keeps its row at the end; the state select writes `block:collapsed`; reset calls `onChange(null)`.
  - `AdminSettings.test.tsx`, `RepositoryDetail.test.tsx`: the setting is sent only when changed, as `quietReviews` is.

- [ ] **Step 2: Run** `cd web && npx vitest run src/components/CommentLayoutEditor.test.tsx`. Expected: FAIL.

- [ ] **Step 3: Implement.** Follow `quietReviews` through the Go handlers and `skills` for a list value. The server sends the effective default list, built from `render.DefaultLayout()`, so the web code holds no block names beyond their labels. Add `commentLayout` to `openapi.yaml`, in the responses and in both request bodies, which the drift test doesn't check.

- [ ] **Step 4: Update `README.md`:** the dashboard description of the setting.

- [ ] **Step 5: Run the Go gate, then the web gate.** Expected: PASS, with `TestOpenAPISchemasMatchGoTypes` green.

- [ ] **Step 6: Scan and commit:** `feat(dashboard): choose a repository's comment layout`.

---

## Finish

Final review on Opus across `origin/main..HEAD` in pergent.sh, with the pergent diff available. One fix wave. List every ruling in the final message. Ask before pushing (H3).
