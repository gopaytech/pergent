# Comment layout: findings first (pergent v0.3.0) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The review comment opens with one headline line (verdict and confidence), puts the findings first as collapsible rows with a link to their lines and, when the caller allows it, a ready-to-copy dismiss command, and folds every passed check and file list into collapsed blocks.

**Architecture:** All of it lives in the public `render` package, so CI mode, agent mode and pergent.sh draw the same comment. The comment is no longer one section per skill: one header, one findings list across skills (blocking first, then file and line order), one checks block, then the file lists. Four additive `render.Options` fields carry what the renderer can't know (the commit, a file-link builder, the dismiss command, the version). CI mode builds the links from the platform's CI variables.

**Tech Stack:** Go (stdlib only), golden tests in `render/testdata/golden` (rewritten in this release on purpose).

**Spec:** `../pergent.sh/docs/superpowers/specs/2026-11-03-inline-findings-design.md` in the pergent.sh repository, §4 "The comment layout", Goal 6 and the Decisions rows "Dismiss hint", "Finding ID" and "Comment layout". pergent.sh 0.5.3 (the move to v0.3.0, the dismiss command and the footer) and 0.6.0 (inline threads) get their own plans.

## Global Constraints

- Public repository: no employer, internal hostnames, people or screenshots in code, docs, tests or commits.
- Work on branch `feat/comment-layout` (worktree `../pergent-layout`, cut from `origin/master`). Never push, tag or open a PR without the owner's go-ahead.
- `render` imports no pergent `internal/` package and redacts nothing; redaction stays in `internal/output` and `internal/result`.
- `render`'s public API only grows: every exported name and field that exists today keeps its meaning, so pergent.sh compiles unchanged. `Summary` and `Skill.Summaries` stay in the API but are no longer drawn.
- The hidden findings block (`FindingsBlock`) and its version 1 format do not change. The `<!-- pergent -->` marker stays the comment's first line; the per-skill `<!-- pergent:<name> -->` / `<!-- /pergent:<name> -->` markers go (nothing reads them).
- Model text keeps every guard it has today: `modelText` in bodies, `checkCell` in tables, `mentionSafe` for people's text, `neutralizeQuickActions` over the whole comment. Model text inside `<summary>` (the finding title) is additionally HTML-escaped (`html.EscapeString` after `oneLine`), since neither platform renders Markdown there.
- The finding ID never appears bare: either inside the dismiss command, or labelled `id <code>…</code>`.
- Confidence stays information only; no verdict, approval or exit code depends on any layout change.
- Commit trailer on every commit: `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. A file path with a space, `#`, `?`, `%` or non-ASCII characters must still link to the right file: each path segment is escaped with `url.PathEscape`, and the visible label stays the raw path (HTML-escaped). Test in Task 1.
2. A finding title containing `</summary>`, `</details>`, `<a href=…>`, `&` or backticks must not break out of its row or inject a link: the summary line shows it as text. Test in Task 2.
3. A file-level finding (`Line` 0) gets a link without a line anchor and a label without `:line`; `EndLine` equal to `Line` shows a single line. Test in Task 1.
4. A comment over the size limit with many blocking findings still fits through the new shrink steps, keeps every blocking finding's body, and every blocking row stays open. Test in Task 3 (the `big` golden and one unit test).
5. A review where no skill gave a model score (older answers, failed runs) must not mention confidence at all: the headline is the verdict alone and a single-skill review shows no table. Test in Task 2.

---

### Task 1: Options and file links

**Files:**
- Create: `render/links.go`, `render/links_test.go`
- Modify: `render/render.go` (the `Options` struct only)

**Interfaces:**
- Produces:
  - `render.Options` gains `Commit string`, `FileURL func(path string, line, endLine int) string`, `DismissCommand string`, `Version string`, `FindingsTitle string` and `FindingNote map[string]string` (fingerprint → one-line note shown at the top of that finding's body; for pergent.sh's "why this isn't inline"). Doc comments say: `Commit` is the reviewed commit, shown as its first 7 characters; `FileURL` returns the web link for a finding's lines, nil or an empty result means plain text; `DismissCommand` is the command prefix such as `@pergent-review dismiss`, empty means no dismiss line; `Version` is shown in the footer after "pergent "; `FindingsTitle` replaces the "Findings" heading when non-empty.
  - `func GitHubFileURL(repoURL, commit string) func(path string, line, endLine int) string` — `repoURL` like `https://github.com/owner/repo`; result `<repoURL>/blob/<commit>/<escaped path>` plus `#L17` or `#L17-L19`.
  - `func GitLabFileURL(projectURL, commit string) func(path string, line, endLine int) string` — result `<projectURL>/-/blob/<commit>/<escaped path>` plus `#L17` or `#L17-19`.
  - Both return nil when `repoURL`/`projectURL` or `commit` is empty, and strip one trailing `/` from the URL.
  - `func location(f Finding) string` (unexported) — `file`, `file:17` or `file:17–19` (en dash), used by Task 2's rows.

- [ ] **Step 1: Write the failing tests** in `render/links_test.go`:
  - `TestGitHubFileURL`: `GitHubFileURL("https://github.com/o/r/", "abc123")("dir/a b#c?.go", 17, 19)` == `"https://github.com/o/r/blob/abc123/dir/a%20b%23c%3F.go#L17-L19"`; line 17, end 17 → `#L17`; line 17, end 0 → `#L17`; line 0 → no `#`; `"ü/x%.go"` → `"%C3%BC/x%25.go"`.
  - `TestGitLabFileURL`: same inputs with `GitLabFileURL("https://gitlab.example.com/g/p", "abc123")` → `".../-/blob/abc123/dir/a%20b%23c%3F.go#L17-19"`, single line `#L17`.
  - `TestFileURLNeedsRepoAndCommit`: each builder with an empty URL or empty commit returns nil.
  - `TestLocation`: `{File:"a.go"}` → `a.go`; `{File:"a.go",Line:3}` → `a.go:3`; `{Line:3,EndLine:3}` → `a.go:3`; `{Line:3,EndLine:5}` → `a.go:3–5`; `{Line:5,EndLine:3}` → `a.go:5`.
- [ ] **Step 2: Run** `go test ./render -run 'FileURL|Location'` — expect FAIL (undefined).
- [ ] **Step 3: Implement** `render/links.go` and the `Options` fields. Escape with `url.PathEscape` per `/`-separated segment. A range anchor only when `endLine > line`.
- [ ] **Step 4: Run** `go test ./render ./internal/...` — expect PASS (no layout change yet, goldens untouched).
- [ ] **Step 5: Commit** `feat(render): file links and options for the new comment layout`.

### Task 2: Header and findings rows

**Files:**
- Modify: `render/render.go` (`render`, `writeSkill` split up, `writeFinding`, `writeSummaries` removed from the output), `render/confidence.go` (`writeConfidence` becomes the headline, meta, reason and table)
- Modify tests: `render/*_test.go`, `internal/output/*_test.go` assertions on the old layout
- Regenerate: `render/testdata/golden/*.md` with `go test ./internal/output -run TestFormatMatchesGoldens -update-goldens`

**Interfaces:**
- Consumes: Task 1's `Options` fields, `location`.
- Produces: the top of the comment and the findings section, in this order after `<!-- pergent -->`:
  1. **Headline** `### <icon> <verdict>[ · Confidence <n>/5[ (capped)] | · Confidence not scored]`, then a newline. Verdict over all skills: any blocking finding → `❌ 1 blocking issue` / `❌ N blocking issues`; else any failed skill → `⚠️ 1 skill failed` / `⚠️ N skills failed`; else `✅ No blocking issues`, plus ` (N dismissed)` when blocking findings were dismissed. The confidence part only when `anyModelScore(skills)`. No skills at all → `### No skills ran`.
  2. **Meta line** `<sub>…</sub>` followed by a blank line, parts joined by ` · ` and each left out when empty: `Commit ` + codeSpan(first 7 of `Commit`); `N files` (`1 file`) from `len(ChangedFiles)`, else the number of distinct `Reviewed` paths; `P of T checks passed` where T = passed + failed over every skill (left out when T is 0); the finding counts `1 blocking · 3 suggestions` or `no findings`; `N skills failed` when the headline is the blocking one and a skill also failed.
  3. **Reason** `> <reason>` + blank line: the `ConfidenceReason` of the skill `ReviewScore` picked, through `mentionSafe`; prefixed `` `skill`: `` when more than one skill; ` (capped: <why>)` appended when capped. Capped with no reason → `> Capped: <why>`. Nothing when neither.
  4. **Skills table**, only with more than one skill: today's table, with the Confidence column only when `anyModelScore`.
  5. **Failures**: today's failure lines (`⚠️ ` + optional `Files …: ` + `writeFailure`), prefixed `` `skill`: `` when more than one skill, then a blank line.
  6. **Inline note** once: `> N findings posted as inline comments on the diff.` (sum over skills).
  7. **Findings** `#### <FindingsTitle or "Findings"> (N)` + blank line, N = shown findings; nothing when N is 0. Rows across all skills, sorted stably by blocking first, then file, then line. Each row:
     ```
     <details[ open]><summary><icon> <b>TITLE</b> · [<a href="URL">]<code>LOC</code>[</a>][ · <code>SKILL</code>][ · id <code>ID</code>]</summary>

     [_NOTE_]
     EXPLANATION

     **Fix:** FIX

     _Suggested change, replacing lines 42–43:_
     (fenced block, as today)

     Not a real problem? Reply `DISMISS ID <reason>`

     </details>

     ```
     Icon `❌` blocking, `💡` suggestion. ` open` on every blocking row; suggestion rows are collapsed. SKILL only with more than one skill. `id` only when the finding has an ID and `DismissCommand` is empty; the dismiss line only when both are set, the command through `codeSpan`. NOTE from `FindingNote[fingerprint]` through `summaryText`, italic. TITLE, LOC and SKILL through `html.EscapeString(oneLine(…))`; URL through `html.EscapeString`. A multi-line FIX keeps today's blank-line rule.
  8. **Dismissed** as today (`<details><summary>Dismissed (N)</summary>`), once across skills.
  - Summaries are no longer written. Checks, file lists and notes are still written per skill by today's helpers, after the dismissed block and without a per-skill heading (Task 3 replaces them).

- [ ] **Step 1: Write the failing tests** in a new `render/layout_test.go`:
  - `TestHeadline`: one clean skill with confidence 4 → starts `"<!-- pergent -->\n### ✅ No blocking issues · Confidence 4/5\n"`; one blocking finding with confidence 5 → `"### ❌ 1 blocking issue · Confidence 3/5 (capped)\n"`; a failed skill and a clean one, no scores → `"### ⚠️ 1 skill failed\n"`; no skills → `"### No skills ran\n"`.
  - `TestHeadlineWithoutScoresHasNoConfidence` (Review Focus 5): one skill, `Confidence` 0, one suggestion → the comment contains no `Confidence` and no `| Skill |`.
  - `TestMetaLine`: `Options{Commit: "a1b2c3d4e5", ChangedFiles: 11 paths}`, 5 passed checks, 3 suggestions → contains the line `<sub>Commit ` + a code span `a1b2c3d` + ` · 11 files · 5 of 5 checks passed · 3 suggestions</sub>` followed by a blank line.
  - `TestReasonLine`: single skill reason `"Safe @alice"` → `"> Safe @‍alice\n\n"`; two skills → the line starts `` "> `b`: " `` for the lower one.
  - `TestFindingRowsOrderAndOpen`: two skills; suggestion `b.go:1`, blocking `z.go:9`, blocking `a.go:5` → rows in order `a.go:5`, `z.go:9`, `b.go:1`; both blocking rows are `<details open>` and the suggestion row is `<details>`; each row shows `<code>skillname</code>`.
  - `TestFindingRowLinkAndDismiss`: `Options{FileURL: GitHubFileURL("https://github.com/o/r","sha1"), DismissCommand: "@bot dismiss"}`, finding ID `d9461c`, lines 17–19 → contains `<a href="https://github.com/o/r/blob/sha1/role.yaml#L17-L19"><code>role.yaml:17–19</code></a>` and ``"Not a real problem? Reply `@bot dismiss d9461c <reason>`"``; no `id <code>`.
  - `TestFindingRowIDWithoutDismiss`: ID set, no `DismissCommand` → summary ends `· id <code>d9461c</code></summary>`, no "Not a real problem".
  - `TestFindingTitleIsEscapedInSummary` (Review Focus 2): title `` "x</summary></details><a href=\"https://e.vil\">&`y`" `` → the row's summary line contains `x&lt;/summary&gt;&lt;/details&gt;&lt;a href=&#34;https://e.vil&#34;&gt;&amp;` and the comment has exactly as many `</details>` as `<details` openings.
  - `TestFindingsTitleAndNote`: `FindingsTitle: "Not on the diff"`, `FindingNote{"fp1": "off the diff"}` → `"#### Not on the diff (1)"` and `"_off the diff_"` at the top of the body.
  - `TestSummariesNotShown`: a skill with `Summaries` → the summary text appears nowhere.
- [ ] **Step 2: Run** `go test ./render -run 'Headline|Meta|Reason|FindingRow|FindingTitle|FindingsTitle|SummariesNotShown'` — expect FAIL.
- [ ] **Step 3: Implement** the header and rows in `render/render.go` / `render/confidence.go` as listed under Interfaces. Keep the existing shrink layouts working: `suggestionTitles` writes a suggestion as `- 💡 <b>TITLE</b> · LOC-with-link` with today's `detailsDroppedNote` once above them; `noSuggestions` keeps today's count notes.
- [ ] **Step 4: Update old-layout assertions** in `render/*_test.go` and `internal/output/*_test.go` to the new strings (`**Fix:**` replaces `_Suggested fix:_`; no `### <skill>` headings; no `**Summary:**`). Do not weaken a security assertion: each hostile-input test keeps checking the same property against the new layout.
- [ ] **Step 5: Regenerate goldens** `go test ./internal/output -run TestFormatMatchesGoldens -update-goldens`, then `go test ./...` — expect PASS. Read `git diff --stat render/testdata/golden` and open `confidence-0.md`, `hostile-0.md` and `layout-0.md` to check they read as the spec's example; no `.json` or `.block` file may change.
- [ ] **Step 6: Commit** `feat(render): headline, meta line and findings rows first`.

### Task 3: Checks, files, footer and shrink steps

**Files:**
- Modify: `render/render.go` (`writeChecks`, `writeCheckTables`, file lists, notes, footer, `Comment`'s layouts, `layout` struct), tests as in Task 2
- Regenerate: goldens as in Task 2

**Interfaces:**
- Consumes: Task 2's layout.
- Produces, after the dismissed block:
  1. **Failed checks** across all skills, when any: `**Failed checks (N)**` + blank line + one table.
  2. **Other checks**: `<details><summary>Checks: P of T passed[, K n/a]</summary>` + blank line + one table of every passed and n/a check + `</details>`; nothing when there are none. T = passed + failed, as in the meta line.
  3. Tables: `| Area | Check | Status | Detail |`, with a leading `Skill` column when more than one skill. Area is `Check.Section` (may be empty). A check that several batches of one skill answered is one row, keyed by (skill, section, name): its status is the worst one (failed beats n/a beats passed) and its detail comes from the first batch with that worst status. The failed-check notes count merged rows. Cells through `checkCell`; status through `checkIcons`.
  4. The check notes (unmatched failed checks, dropped checks, verification drops), then the other skill notes, as `> ` lines, prefixed `` `skill`: `` when more than one skill.
  5. **Files**: one `<details><summary>Files (N)</summary>` block, N = the distinct paths in `ChangedFiles` plus every skill's `Reviewed` and `Skipped`, in `ChangedFiles` order then first appearance. One list item per path: the path as a code span, then nothing when every skill reviewed it (or the review has one skill and reviewed it); ` — skipped: <rule>` when the only skill skipped it; with more than one skill, ` — reviewed by `a`, `b`; skipped by `c` (lockfile)` naming only the skills that touched it, and ` — not reviewed` for a changed path no skill reviewed or skipped. Rules through `neutralizeComments(oneLine(…))`; `listLimit` caps the list as today. This replaces today's "Files changed", "Reviewed files" and "Skipped files" blocks.
  6. **Footer**: when `Footer` or `Version` is set, `<sub>FOOTER · pergent VERSION</sub>` + blank line (either part alone without the separator), before the hidden block.
  - `layout` loses `noSummaries`; `summariesDroppedNote` goes. Shrink order in `Comment`: full → `noHidden` → `+suggestionTitles` → `+noSuggestions` → `+checksFailOnly` (the "Checks:" details block goes, replaced by `passedChecksDroppedNote`; the failed table stays) → `+noCheckTables` (the failed table goes too, replaced by `checkTablesDroppedNote`; the meta line keeps the counts) → `listLimit: 20` → `listLimit: 0` → cut with `truncatedNote` as today. Update `Comment`'s doc comment to this order.

- [ ] **Step 1: Write the failing tests** in `render/layout_test.go`:
  - `TestChecksFailedAboveCollapsed`: one skill, checks pass/fail/n/a → `"**Failed checks (1)**"` appears before `"<details><summary>Checks: 1 of 2 passed, 1 n/a</summary>"`, and the failed check's row is not inside the details block.
  - `TestChecksMultiSkillAndBatch`: two skills, skill `a` with 2 batches that both answer check "Tests" (batch 1 passed with detail "ok", batch 2 failed with detail "missing") → header `| Skill | Area | Check | Status | Detail |`, exactly one "Tests" row for `a`, in the failed table, with detail "missing".
  - `TestFilesBlock`: one skill, `ChangedFiles` [`a.go`, `go.sum`, `x.md`], reviewed `a.go`, skipped `go.sum` (lockfile) → `"<details><summary>Files (3)</summary>"` after the checks, items `` "- `a.go`\n" ``, `` "- `go.sum` — skipped: lockfile\n" ``, `` "- `x.md` — not reviewed\n" ``. Two skills `a` (reviewed `a.go`) and `b` (skipped `a.go`, generated) → `` "- `a.go` — reviewed by `a`; skipped by `b` (generated)\n" ``.
  - `TestFooter`: `Footer` = "Run again: " + code span `@bot review`, `Version: "v0.3.0"` → `<sub>` + that footer + ` · pergent v0.3.0</sub>` and a blank line right before the hidden block; `Version` alone → `"<sub>pergent v0.3.0</sub>"`.
  - `TestShrinkKeepsBlockingBodies` (Review Focus 4): 30 blocking findings with 200-byte explanations and 30 suggestions, `MaxLen` set so the suggestions can't fit → result ≤ MaxLen, every blocking explanation present, every blocking row `<details open>`, and `"30 suggestions omitted"` present.
- [ ] **Step 2: Run** `go test ./render -run 'Checks|FilesBlock|Footer|Shrink'` — expect FAIL.
- [ ] **Step 3: Implement** as listed under Interfaces.
- [ ] **Step 4: Update old-layout assertions** (checks per batch, "Checks — batch", the three file blocks, "**Checks:**" count line, summaries shrink step) as in Task 2 Step 4; `TestDismissedShrink` keeps its intent.
- [ ] **Step 5: Regenerate goldens** as in Task 2 Step 5; the size-limit goldens (`big-*`, `checks-big-*`, `failures-*`) must show the new steps; `go test ./...` PASS; `go vet ./...` and `gofmt -l .` clean.
- [ ] **Step 6: Commit** `feat(render): one checks block, file lists last, footer and new shrink order`.

### Task 4: Links, commit and version in CI and agent mode

**Files:**
- Modify: `internal/config/resolver.go` (+ test), `internal/output/formatter.go` (+ test), `cmd/main.go`, `cmd/agent.go`

**Interfaces:**
- Consumes: `render.GitHubFileURL`, `render.GitLabFileURL`, the new `render.Options` fields.
- Produces:
  - `GitHubConfig` gains `HeadSHA` (event `pull_request.head.sha`) and `WebURL` (`GITHUB_SERVER_URL`, default `https://github.com`, + `/` + `GITHUB_REPOSITORY`; empty when the repository is empty). `GitLabConfig` gains `HeadSHA` (`CI_COMMIT_SHA`) and `WebURL` (`CI_PROJECT_URL`).
  - `output.Options` gains `Commit string`, `FileURL func(string, int, int) string`, `Version string`, passed to `render.Options`.
  - `cmd/main.go`: CI mode passes the head SHA, the platform's file-link builder (nil in local mode or when either value is missing) and `"v" + version`. `cmd/agent.go`: passes `in.Head` and `"v" + version`, no links, no dismiss command.

- [ ] **Step 1: Write the failing tests**:
  - `TestResolveGitHubHeadAndWebURL` (resolver_test): event file with `pull_request.head.sha = "h1"`, `GITHUB_REPOSITORY=o/r`, no `GITHUB_SERVER_URL` → `HeadSHA "h1"`, `WebURL "https://github.com/o/r"`; with `GITHUB_SERVER_URL=https://ghe.example.com` → `"https://ghe.example.com/o/r"`.
  - `TestResolveGitLabHeadAndWebURL`: `CI_COMMIT_SHA=h2`, `CI_PROJECT_URL=https://gitlab.example.com/g/p` → those values.
  - `TestFormatReviewLinksAndCommit` (formatter_test): `FormatReviewWithinOptions` with `Options{Commit: "abcdef1234", FileURL: render.GitHubFileURL("https://github.com/o/r", "abcdef1234"), Version: "v9.9.9"}` → contains `` "Commit `abcdef1`" ``, `href="https://github.com/o/r/blob/abcdef1234/main.go#L3"` and `"pergent v9.9.9"`; a path holding a token value in the finding is redacted in the link as well as in the label.
- [ ] **Step 2: Run** `go test ./internal/config ./internal/output -run 'HeadAndWebURL|LinksAndCommit'` — expect FAIL.
- [ ] **Step 3: Implement** as listed. The goldens call `FormatReviewWithinOptions` with options that leave the new fields empty, so they don't change.
- [ ] **Step 4: Run** `go test ./...` — expect PASS.
- [ ] **Step 5: Commit** `feat: CI-mode comment links to the reviewed lines and names the commit`.

### Task 5: Docs and version

**Files:**
- Modify: `README.md` (the review-comment section), `CLAUDE.md` (the `render/` line: one header, findings first, options), `docs/agent-result-schema.md` (only if it describes the `markdown` field's layout), `cmd/main.go` (`const version = "0.3.0"`)

- [ ] **Step 1:** Rewrite README's description of the comment to the new order (headline, meta, reason, findings rows with links, checks collapsed, files, footer), with a short made-up example, and say that pergent.sh adds the dismiss command per finding.
- [ ] **Step 2:** Update `CLAUDE.md`'s `render/` architecture line to the new layout and the new options.
- [ ] **Step 3:** Set `const version = "0.3.0"`.
- [ ] **Step 4: Verify** `go test ./...`, `go vet ./...`, `gofmt -l .` clean. Run the owner's private keyword scan over `git log -p origin/master..HEAD` (the names are kept out of this public repository). Expected: no matches.
- [ ] **Step 5: Commit** `docs: findings-first comment layout; v0.3.0`.
