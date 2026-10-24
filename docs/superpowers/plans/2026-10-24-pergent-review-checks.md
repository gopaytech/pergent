# pergent Review Checks Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A skill with a checklist gets a checks table back in the review comment. Each check has a status and evidence, grouped by the skill's own sections, while findings still decide the verdict.

**Architecture:**
- The model's answer gains an optional `checks` list.
- `internal/finding` parses it tolerantly.
- `internal/review` keeps each finished batch's checks.
- `internal/output` renders a count line plus section tables under the summary, flags failed checks that have no finding, and shrinks checks before summaries when the comment is over the size limit.

**Tech Stack:** Go 1.24.8, stdlib + `gopkg.in/yaml.v3`.

**Spec:** `docs/superpowers/specs/2026-10-24-pergent-review-checks-design.md`

## Global Constraints

- No new Go dependencies.
- **Checks never cost a review its findings.** A missing `checks`, one of the wrong type, or invalid entries are never an error and never trigger a repair run. Invalid entries are dropped.
- **Limits:** at most **60** checks per answer. `name` at most **120** runes, `detail` at most **300**, `section` at most **80**. Each field is redacted first, then collapsed to one line, then capped, ending with `…` when cut.
- **Status values:** `pass`, `fail`, `n/a`. `na` is accepted as `n/a`, and matching ignores case and surrounding whitespace.
- **Icons:** ✅ pass, ❌ fail, ➖ n/a.
- **The verdict comes from findings only.** Checks never change it.
- The hidden findings block is unchanged.
- **Table cells:** newlines become spaces, `|` becomes `\|`, `<` becomes `&lt;`, and when a cell has an odd number of backticks, all of its backticks are escaped.
- Generic examples only in code, tests and docs.

**Plan correction to the spec, section 5 (Ruling P1):** the spec's first size step, collapsing tables into `<details>`, doesn't make the comment any shorter. The size steps are therefore: (1) keep only failed checks, (2) drop the tables and keep the count line. Each step adds a note.

## Review Focus

- **A check cell containing `|`, a newline, `</details>`, an unbalanced backtick or a secret** must not break the table or the comment, and must not leak. Tested in Task 4.
- **An answer with 200 checks** must show only 60, with a note saying how many weren't shown. Tested in Task 1 and Task 4.
- **A failed check in a batch whose answer had no findings** must show ❌ with the "no matching finding" note, and the verdict stays as the findings say. Tested in Task 4.
- **A multi-batch skill where only batch 2 has a failed check** must render batch 2's block open and batch 1's closed. Tested in Task 4.
- **An answer from an older prompt** with no `checks` key must render exactly as before: no count line, no table. Tested in Task 4.

---

### Task 1: Parse `checks` from the answer

**Files:**
- Modify: `internal/finding/parse.go`
- Test: `internal/finding/checks_test.go`

**Interfaces:**
- Produces (package `finding`):
  - `type CheckStatus string`, with constants `CheckPass CheckStatus = "pass"`, `CheckFail CheckStatus = "fail"` and `CheckNA CheckStatus = "n/a"`.
  - `type Check struct { Section, Name string; Status CheckStatus; Detail string }`
  - `Answer` gains `Checks []Check` and `ChecksDropped int`, the count of valid checks beyond the first 60.
  - `const maxChecks = 60`, `maxCheckName = 120`, `maxCheckDetail = 300`, `maxCheckSection = 80`.

- [ ] **Step 1: Write the failing tests** in `checks_test.go`. Use the `jsonAnswer` helper from `parse_test.go`.
  - `TestParseAnswer_Checks`: three checks (pass with section and detail, fail, `"NA"`) parse into `Checks` with statuses `CheckPass`, `CheckFail` and `CheckNA`, in order, and the fields are kept.
  - `TestParseAnswer_ChecksMissingOrWrongType`: `checks` missing, `"checks": "x"`, or `"checks": {}` all give `Checks` empty, findings kept and a nil error.
  - `TestParseAnswer_InvalidChecksDropped`: these entries are dropped while the valid ones are kept:
    - an entry with no `name`;
    - an entry with `"status": "maybe"`;
    - an entry that is a string, not an object;
    - an entry whose `name` is a number.
  - `TestParseAnswer_ChecksCappedAt60`: 200 valid checks give `len(Checks) == 60` and `ChecksDropped == 140`.
  - `TestParseAnswer_CheckFieldsCleaned`:
    - a name of 500 runes becomes 120 runes ending with `…`;
    - a detail with a newline becomes one line;
    - with `GITLAB_TOKEN=glpat-secretvalue123456` set via `t.Setenv`, a detail of 290 `x` characters plus the token contains no `glpat`.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/finding/ -run 'TestParseAnswer_(Checks|ChecksMissingOrWrongType|InvalidChecksDropped|ChecksCappedAt60|CheckFieldsCleaned)'`
Expected: FAIL (build: `Checks` undefined)

- [ ] **Step 3: Implement**
  - Decode `checks` as `json.RawMessage`. If it is a JSON array, decode each element separately into a struct whose four fields are `json.RawMessage`, then read each field with the existing string-only reader. An element that isn't an object is dropped.
  - Generalise `cleanSummary` into `cleanText(s string, max int) string`, which redacts, collapses and caps. Use it for the summary and for every check field.

- [ ] **Step 4: Run to verify they pass**

Run: `go vet ./... && go test ./...`
Expected: all `ok`

- [ ] **Step 5: Commit**

```bash
git add internal/finding
git commit -m "feat: parse a checklist of checks from the review answer"
```

---

### Task 2: Ask for checks in the output format

**Files:**
- Modify: `internal/skill/prompt.go` (`outputFormatSection`)
- Test: `internal/skill/prompt_test.go`

- [ ] **Step 1: Write the failing test.** Add `TestPrompt_AsksForChecks`: the prompt contains `"checks"`, `report every item in it as a check`, and `Every \`fail\` must also appear as a finding`.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/skill/ -run TestPrompt_AsksForChecks`
Expected: FAIL

- [ ] **Step 3: Replace `outputFormatSection`** with this exact copy:

~~~markdown
## Output format

End your final answer with exactly one fenced code block tagged `json`, with nothing before or after it, in this shape:

```json
{"summary": "What you checked and what you concluded, in 1-3 sentences.", "checks": [{"section": "Checklist heading", "name": "What was checked", "status": "pass", "detail": "Evidence: the values you compared, with file and field"}], "findings": [{"file": "path/from/repo/root", "line": 42, "severity": "blocking", "title": "One-line summary", "explanation": "Why it matters", "suggested_fix": "Optional fix"}]}
```

- `summary` is 1-3 sentences on what you checked and what you concluded, including when you found nothing. Name the files or resources you looked at.
- `checks`: if the instructions above include a checklist, report every item in it as a check, in the checklist's order, with its section heading in `section`. `status` is `pass`, `fail` or `n/a`. Put the concrete evidence in `detail`. Every `fail` must also appear as a finding. With no checklist, answer `"checks": []`.
- `severity` is `blocking` or `suggestion`. Only problems described under Blocking are `blocking`; everything else is a `suggestion`.
- `line` is the line number in the new version of the file. Omit it when the finding is not about one line.
- `suggested_fix` is optional.
- If you find nothing, answer with your summary, your checks and `"findings": []`.
~~~

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/skill/ -v`
Expected: PASS (including the existing prompt tests)

- [ ] **Step 5: Commit**

```bash
git add internal/skill
git commit -m "feat: ask every review to report the skill's checklist"
```

---

### Task 3: Keep each batch's checks

**Files:**
- Modify: `internal/review/review.go` (`batchOutcome`, `reviewBatch`, `reviewSkill`, `SkillResult`)
- Test: `internal/review/checks_test.go`

**Interfaces:**
- Consumes: `finding.Answer.Checks`, `finding.Answer.ChecksDropped` (Task 1).
- Produces:
  - `type BatchChecks struct { Batch int; Checks []finding.Check; Dropped int; Findings int }`. `Findings` is the number of findings that batch's answer reported, before dedup and verification.
  - `SkillResult` gains `Checks []BatchChecks`: one entry per finished batch that reported at least one check or dropped any, in batch order.
  - The repaired answer supplies them when a repair ran.

- [ ] **Step 1: Write the failing tests**
  - `TestRun_ChecksKept`: one batch answering 2 checks and 1 finding gives `Checks == [{Batch: 1, Checks: <the 2>, Dropped: 0, Findings: 1}]`.
  - `TestRun_ChecksOnlyFinishedBatches`: two batches, where batch 1 fails and batch 2 answers 1 check. The result has exactly one entry, with `Batch: 2`.
  - `TestRun_ChecksFromRepair`: the first answer has no json block, and the repair answers 1 check. The result has one entry with that check.
  - `TestRun_NoChecksNoEntry`: an answer without `checks` gives `Checks` empty.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/review/ -run 'TestRun_(ChecksKept|ChecksOnlyFinishedBatches|ChecksFromRepair|NoChecksNoEntry)'`
Expected: FAIL (build: `BatchChecks` undefined)

- [ ] **Step 3: Implement.** Carry `checks []finding.Check`, `checksDropped int` and `findings` (already carried) in `batchOutcome`. Append to `res.Checks` in the existing finished-batch loop.

- [ ] **Step 4: Run to verify they pass**

Run: `go test -race ./internal/review/`
Expected: `ok`

- [ ] **Step 5: Commit**

```bash
git add internal/review
git commit -m "feat: keep each finished batch's checks"
```

---

### Task 4: Render the checks

**Files:**
- Modify: `internal/output/formatter.go` (`writeSkill`, plus new `writeChecks` and `checkCell`)
- Test: `internal/output/checks_test.go`

**Interfaces:**
- Consumes: `review.SkillResult.Checks` (Task 3), `finding.Check`, `finding.CheckStatus`.

**Layout per skill:** verdict line, summary, **count line, check tables or blocks, the "no matching finding" note, then the "Verification dropped" note if there is one** (moved out of the notes at the end, and only when the skill has checks), then blocking findings and the rest as today.

- [ ] **Step 1: Write the failing tests**
  - `TestFormatReview_ChecksSingleBatch`: one batch with checks in sections A (pass, n/a) and B (fail), with a finding present.
    - The output contains `**Checks:** 1 passed · 1 failed · 1 n/a`.
    - It contains `**A**` and then a table header `| Check | Status | Detail |` with rows `| <name> | ✅ | <detail> |` and `| <name> | ➖ | … |`, then `**B**` with a ❌ row.
    - The order is: summary, count line, tables, first blocking finding.
  - `TestFormatReview_ChecksCountOmitsZeros`: only passes give `**Checks:** 3 passed`, with no "failed" and no "n/a".
  - `TestFormatReview_ChecksMultiBatch`: `Batches: 2`, with batch 1 all pass and batch 2 one fail.
    - The output has `<details><summary>Checks — batch 1 of 2 (2 passed)</summary>` and `<details open><summary>Checks — batch 2 of 2 (1 failed)</summary>`.
    - Both blocks contain tables, and the `<details>` tags are balanced.
  - `TestFormatReview_FailedCheckWithoutFinding`: a batch with a fail check and `Findings: 0` renders ❌ and the note `> 1 failed check has no matching finding; the verdict is based on findings.` The verdict line stays `✅ **No blocking issues**`.
  - `TestFormatReview_VerificationNoteNextToChecks`: a skill with checks and the note `Verification dropped 1 of 1 blocking finding: …` shows that note before the first `<details>` and after the tables.
  - `TestFormatReview_CheckCellsSafe`: a check whose name has `a|b`, a newline and an odd backtick, and whose detail has `</details>` and the secret from `GITLAB_TOKEN` (set via `t.Setenv`).
    - Every table row has exactly 4 unescaped `|` characters.
    - The `<details>` tags are balanced.
    - The secret is absent.
  - `TestFormatReview_ChecksDroppedNote`: `Dropped: 140` renders `> 140 checks beyond the first 60 per answer were not shown.`
  - `TestFormatReview_NoChecksUnchanged`: a result without `Checks` but with a summary and reviewed files contains no `**Checks:**` and no `| Check |`, and the summary line is followed directly, after one blank line, by `<details><summary>Reviewed files`.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/output/ -run 'TestFormatReview_(Checks|FailedCheck|VerificationNote|CheckCells|NoChecks)'`
Expected: FAIL

- [ ] **Step 3: Implement**
  - Add `writeChecks(b *strings.Builder, r review.SkillResult, l layout)` and `checkCell(s string) string`. `checkCell` follows the cell rules in Global Constraints.
  - **Single batch** (`r.Batches <= 1` and one entry): render the section tables inline.
  - **Several batches:** each batch's tables go in `<details>`, written `<details open>` when that batch has a fail. The summary reads `Checks — batch N of M (<counts>)`, with counts written as `x passed, y failed, z n/a` and zeros omitted.
  - **Section grouping:** sections appear in order of first appearance, and checks with an empty section go in a final group with no heading.
  - **"No matching finding" note:** the number of fail checks in batches with `Findings == 0`. The note reads `> N failed check(s) has/have no matching finding; the verdict is based on findings.`, using `plural`.
  - **Dropped note:** `> N checks beyond the first 60 per answer were not shown.`

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/output/ -v`
Expected: PASS, with all existing formatter tests still passing.

- [ ] **Step 5: Commit**

```bash
git add internal/output
git commit -m "feat: show the skill's checklist as tables in the review comment"
```

---

### Task 5: Shrink checks under the size limit, and docs

**Files:**
- Modify: `internal/output/formatter.go` (`layout`, `FormatReviewWithin`, `writeChecks`)
- Modify: `README.md`
- Test: `internal/output/checks_test.go`

**Interfaces:**
- Produces:
  - `layout` gains `checksFailOnly bool` and `noCheckTables bool`.
  - The `FormatReviewWithin` steps become: hidden block, suggestion bodies, suggestions, **failed checks only** (note: `> Passed and n/a checks omitted to fit the comment size limit.`), **count line only** (note: `> Check tables omitted to fit the comment size limit.`), summaries, lists capped at 20, lists capped at 0.
  - From the "failed checks only" step on, every later step keeps that setting, and from "count line only" on, every later step keeps that one too.

- [ ] **Step 1: Write the failing tests**
  - `TestFormatReviewWithin_ChecksShrinkBeforeSummaries`: one skill with a summary, 60 passing checks (long details) and 1 failing check.
    - With a `maxLen` that fits only once passing rows are dropped, the output keeps the ❌ row, the summary and the passed-and-n/a note, and drops the passing row names.
    - With a smaller `maxLen` that fits only once tables are dropped, the output keeps the count line, the summary and the check-tables note, and has no `| Check |`.
  - The existing `TestFormatReviewWithin_ShrinksInSteps` must still pass unchanged. Its fixture has no checks.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/output/ -run TestFormatReviewWithin`
Expected: FAIL on the new test

- [ ] **Step 3: Implement the two steps, and update the README:**
  - **`checks`:** document the field in the output-format section.
  - **Checklists:** "Write a checklist with section headings and items if you want a checks table. Each item becomes a row."
  - **Comment layout:** add the count line and tables to the layout list.
  - **Size limit:** add the two new steps to the paragraph about shrinking.
  - **Example:** extend the example comment with a two-row table.

- [ ] **Step 4: Verify everything**

Run: `go vet ./... && make test && make build`
Expected: all `ok`, `bin/pergent` built

- [ ] **Step 5: Commit**

```bash
git add internal/output README.md
git commit -m "feat: shrink check tables before summaries under the size limit"
```
