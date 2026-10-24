# pergent Review Summary Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every completed review states what the model checked and lists the files it reviewed, directly under each skill's verdict.

**Architecture:**
- The model's answer gains an optional `summary`.
- `finding.ParseAnswer` returns it alongside the findings.
- `review` collects one summary per completed batch, plus the reviewed file list.
- `output` renders both under the verdict and drops them in order when the comment is over the platform size limit.

**Tech Stack:** Go 1.24.8, stdlib + `gopkg.in/yaml.v3`.

**Spec:** `docs/superpowers/specs/2026-10-24-pergent-review-summary-design.md`

## Global Constraints

- No new Go dependencies.
- A missing or empty `summary` is never an error and never triggers a repair run. Only `findings` stays required.
- A summary is collapsed to one line and capped at 600 characters; when it is cut, it ends with `…`.
- The hidden findings block format is unchanged. Comments from older pergent versions still parse.
- Summary text is model text. It is redacted, `<details>`, `<summary>` and comment markers are neutralised, and unbalanced fences are closed, the same as finding titles.
- Generic examples only in code, tests and docs: no real project, repo, MR, person or bot names.

## Review Focus

- **An answer that omits `summary`** (an older prompt, or a model that ignores the instruction) must still complete with its findings, and the comment shows no summary. Tested in Task 1 and Task 4.
- **A summary containing `</details>`, a code fence or a secret** must not break the layout or leak. Tested in Task 4.
- **A multi-batch skill with one failed batch** shows summaries only for the batches that finished, labelled with their batch numbers. Tested in Task 3 and Task 4.
- **A 2,000-character summary** is capped at 600 characters ending with `…`, so the comment doesn't balloon. Tested in Task 1.

---

### Task 1: Parse `summary` from the answer

**Files:**
- Modify: `internal/finding/parse.go`
- Modify: `internal/review/review.go` and `internal/review/crossbatch.go` (callers of `ParseAnswer`; they use `.Findings`, and the cross-batch check ignores the summary)
- Test: `internal/finding/parse_test.go`

**Interfaces:**
- Produces:
  - `type Answer struct { Summary string; Findings []Finding }`
  - `func ParseAnswer(text string) (Answer, error)`. Validation is the same as today. `Summary` is trimmed, collapsed to one line (runs of whitespace become a single space), and capped at `maxSummaryLen = 600` runes, ending with `…` when cut.

- [ ] **Step 1: Write the failing tests** in `parse_test.go`:
  - `TestParseAnswer_Summary`: `{"summary":"Checked a.go.","findings":[]}` → `Summary == "Checked a.go."`, no findings, nil error.
  - `TestParseAnswer_MissingSummaryIsFine`: `{"findings":[]}` → `Summary == ""`, nil error.
  - `TestParseAnswer_SummaryCollapsedAndCapped`: a summary with newlines becomes one line. A 2,000-rune summary becomes exactly 600 runes and ends with `…`.
  - Update the existing `TestParseAnswer` cases to read `.Findings`.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/finding/ -v`
Expected: FAIL (compile error: `ParseAnswer` returns `[]Finding`)

- [ ] **Step 3: Implement.** Add `Summary *string \`json:"summary"\`` to the internal `answer` struct, and update the three callers to use `.Findings`.

- [ ] **Step 4: Run to verify they pass**

Run: `go vet ./... && go test ./...`
Expected: all `ok`

- [ ] **Step 5: Commit**

```bash
git add internal/finding internal/review
git commit -m "feat: parse a summary from the review answer"
```

---

### Task 2: Ask for the summary in the output format

**Files:**
- Modify: `internal/skill/prompt.go` (`outputFormatSection`)
- Test: `internal/skill/prompt_test.go`

- [ ] **Step 1: Write the failing test.** Add `TestPrompt_AsksForSummary`: the prompt contains `"summary"` and `including when you found nothing`, and does **not** contain `Text before the code block is not shown to anyone`.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/skill/ -v`
Expected: FAIL

- [ ] **Step 3: Replace `outputFormatSection`** with this exact copy:

~~~markdown
## Output format

End your final answer with exactly one fenced code block tagged `json`, with nothing before or after it, in this shape:

```json
{"summary": "What you checked and what you concluded, in 1-3 sentences.", "findings": [{"file": "path/from/repo/root", "line": 42, "severity": "blocking", "title": "One-line summary", "explanation": "Why it matters", "suggested_fix": "Optional fix"}]}
```

- `summary` is 1-3 sentences on what you checked and what you concluded, including when you found nothing. Name the files or resources you looked at.
- `severity` is `blocking` or `suggestion`. Only problems described under Blocking are `blocking`; everything else is a `suggestion`.
- `line` is the line number in the new version of the file. Omit it when the finding is not about one line.
- `suggested_fix` is optional.
- If you find nothing, answer with your summary and `"findings": []`.
~~~

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/skill/ -v`
Expected: PASS. `TestPrompt_AlwaysAppendsOutputFormat` checks for `{"findings": []}`; update it to check for `"findings": []` instead.

- [ ] **Step 5: Commit**

```bash
git add internal/skill
git commit -m "feat: ask every review for a summary of what was checked"
```

---

### Task 3: Collect summaries and reviewed files per skill

**Files:**
- Modify: `internal/review/review.go` (`batchOutcome`, `reviewBatch`, `reviewSkill`, `SkillResult`)
- Test: `internal/review/review_test.go`

**Interfaces:**
- Consumes: `finding.Answer` (Task 1).
- Produces:
  - `type BatchSummary struct { Batch int; Text string }`, where `Batch` is 1-based.
  - `SkillResult` gains `Summaries []BatchSummary`: non-empty summaries of completed batches, in batch order. The repaired answer's summary is used when a repair ran.
  - `SkillResult` gains `Reviewed []string`: the paths of the files in completed batches, in batch order.

- [ ] **Step 1: Write the failing tests**
  - `TestRun_SummaryAndReviewedFiles`: a single batch whose answer is `{"summary":"Checked a.","findings":[]}` gives `Summaries == [{1, "Checked a."}]` and `Reviewed` equal to the batch's paths.
  - `TestRun_SummariesSkipFailedBatches`: of two batches, batch 1 fails and batch 2 succeeds with summary "B". The result is `Summaries == [{2, "B"}]`, and `Reviewed` holds only batch 2's files.
  - `TestRun_RepairedAnswerSummary`: the first answer has no json block, and the repair answer has summary "R". The result is `Summaries == [{1, "R"}]`.
  - `TestRun_NoSummaryIsFine`: an answer without `summary` gives `Summaries` empty and the verdict unchanged.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/review/ -run 'TestRun_(SummaryAndReviewedFiles|SummariesSkipFailedBatches|RepairedAnswerSummary|NoSummaryIsFine)' -v`
Expected: FAIL

- [ ] **Step 3: Implement.** Carry `summary string` in `batchOutcome`. In the collect loop (completed batches only, in batch order), append the summary when it is non-empty and append the batch's paths to `Reviewed`.

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/review/ -race -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/review
git commit -m "feat: keep each batch's summary and the reviewed files"
```

---

### Task 4: Show summaries and reviewed files in the comment

**Files:**
- Modify: `internal/output/formatter.go` (`writeSkill`, `layout`, `FormatReviewWithin`)
- Modify: `README.md` (output format and comment layout sections)
- Test: `internal/output/formatter_test.go`

**Interfaces:**
- Consumes: `review.SkillResult.Summaries`, `review.SkillResult.Reviewed` (Task 3).
- Produces:
  - `layout` gains `noSummaries bool`.
  - The `FormatReviewWithin` steps become:
    1. hidden block;
    2. suggestion bodies;
    3. suggestions;
    4. **summaries**, adding the note `> Summaries omitted to fit the comment size limit.`;
    5. lists capped at 20;
    6. lists capped at 0.

    The lists capped in steps 5 and 6 include `Reviewed`.

- [ ] **Step 1: Write the failing tests**
  - `TestFormatReview_SummaryUnderVerdict`: one batch with summary "Checked a." The text `Checked a.` appears after `✅ **No blocking issues**` and before any `<details>`.
  - `TestFormatReview_MultiBatchSummaries`: `Batches: 3` with summaries for batches 1 and 3 renders the bullets `- **Batch 1 of 3:** …` and `- **Batch 3 of 3:** …`, in that order.
  - `TestFormatReview_ReviewedFiles`: two reviewed files render `<details><summary>Reviewed files (2)</summary>` before `Skipped files`. With no reviewed files there is no block.
  - `TestFormatReview_SummaryCannotBreakLayout`: a summary containing `</details>`, an unbalanced ```` ``` ```` and `glpat-secretvalue` (set as `GITLAB_TOKEN` with `t.Setenv`). The `<details>` tags stay balanced, no fence is open before the hidden line, and the secret is absent.
  - `TestFormatReviewWithin_DropsSummariesBeforeLists`: pick a `maxLen` that fits only once summaries are dropped. The output has no summary text, includes the note, and still lists reviewed files.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/output/ -v`
Expected: FAIL

- [ ] **Step 3: Implement**
  - Render summaries with the same helpers as finding titles (`oneLine`, `neutralizeComments`, fence closing).
  - Place them right after the verdict line and before blocking findings.
  - Place the reviewed-files block before the skipped-files block, using `writeList` with the layout's `listLimit`.
  - In `README.md`, document the `summary` field and the new layout lines, and add a short example comment showing the summary under ✅.

- [ ] **Step 4: Verify everything**

Run: `go vet ./... && make test && make build`
Expected: all `ok`, `bin/pergent` built

- [ ] **Step 5: Commit**

```bash
git add internal/output README.md
git commit -m "feat: show what each review checked and the files it reviewed"
```
