# pergent Review Summary -- Design Document

**Date:** 2026-10-24
**Status:** Draft

---

## Problem

A clean review looks like pergent did nothing. The comment shows only:

> ### code-review
> ✅ **No blocking issues**

In a real run behind a comment like that, the model read 7 files across 4 steps before answering, but none of that reaches the reader. There are two reasons:

1. **The output format asks only for problems.** pergent's output format (`internal/skill/prompt.go`) asks for `{"findings": [...]}` and tells the model that "Text before the code block is not shown to anyone". So when nothing is wrong, the model's account of what it checked is thrown away.
2. **The comment never says which files were reviewed.** pergent knows exactly which in-scope files each skill reviewed, but it lists only changed files and skipped files.

A reader can't tell a careful pass from a review that looked at nothing.

## Goals

- Every completed review says, in a few sentences, **what the model checked and what it concluded**, whether or not it found problems.
- Every skill section lists **the files it actually reviewed**.
- Both appear directly under the skill's verdict line, so the reader sees them without expanding anything.

## Non-Goals

- A structured per-check result, such as a list of rules with pass or fail for each. That is possible later, but it needs the skill to declare its checks. Not now.
- Summaries for the cross-batch check or the verification pass.
- Feeding previous summaries back on `--previous-review`. Only findings are carried forward, as today.

## Anti-goals

- A summary that is a wall of text, or that repeats the findings.
- A missing summary turning a good review into a failure. The summary is informative, not required for a verdict.

---

## Decisions

### 1. Output format gains `summary`

The answer's JSON becomes:

```json
{"summary": "Checked the Gateway, VirtualService and AuthorizationPolicy for example-com: hosts match, TLS is SIMPLE with a credentialName, routes are ordered specific-first.", "findings": []}
```

- The output-format instructions ask for `summary` on every answer: 1–3 sentences on what was checked and what was concluded, **including when nothing was found**. The instructions no longer say "Text before the code block is not shown to anyone"; they ask for no text before the block instead.
- **Parsing is tolerant.** A missing or empty `summary` is not an error. The review still completes, and the comment shows no summary for that batch. Only `findings` stays required. This avoids spending a repair run on a missing summary.
- **Length cap:** a summary is collapsed to one line and capped at 600 characters, with `…` added when it is cut.

### 2. Multi-batch skills

- Each completed batch contributes its own summary.
- **One summary:** the comment shows it as a paragraph under the verdict.
- **Several summaries:** a bullet list, one bullet per batch in batch order, each starting with `**Batch N of M:**`.
- A failed batch has no summary. Its failure line already explains it.
- The repair run's answer supplies the summary when the first answer needed repair.

### 3. Reviewed files

- `SkillResult` gains the list of in-scope files from the batches that completed (in batch order).
- The comment shows them in a collapsed block, `<details><summary>Reviewed files (N)</summary>`, placed before "Skipped files".
- A skill with no files in scope shows no block. Its note already says "no files in scope".

### 4. Comment layout per skill

```
### <skill>
<failure lines, if any>
<verdict line>
<summary paragraph, or one bullet per batch>
<blocking findings>
<details> Suggestions (N) </details>
<details> Reviewed files (N) </details>
<details> Skipped files (N) </details>
<notes>
```

Summary text is model text, so it goes through the same handling as finding titles. It is redacted, collapsed to one line, has `<details>`, `<summary>` and comment markers neutralised, and has unbalanced code fences closed.

### 5. Comment size limit

The steps `FormatReviewWithin` takes to fit a platform limit gain two changes:

- **Summaries** are dropped in a new step, after "omit suggestions entirely" and before the file lists are capped. The comment notes this with `> Summaries omitted to fit the comment size limit.`
- **The reviewed-files list** is capped together with the other lists.

---

## Constraints

- No new Go dependencies.
- Existing skills need no changes. The output format is pergent's.
- Comments from older pergent versions still parse. The hidden findings block is unchanged.
- The privacy and redaction rules from the stable-reviews design still apply.

## Left to the builder

- Exact wording of the output-format bullets, within section 1.
- Whether the 600-character cap is applied in `finding` or in `output`.
