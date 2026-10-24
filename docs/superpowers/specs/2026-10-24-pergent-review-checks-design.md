# pergent Review Checks -- Design Document

**Date:** 2026-10-24
**Status:** Draft

---

## Problem

Before pergent took control of the output format, a skill with a checklist produced a full report. For example, an ingress-validation skill has a "Validation Checklist" with sections such as File Completeness, YAML Syntax, Cross-Reference Consistency, Route Ordering and Field Validations. The model answered with one table per section, ✅ for each check that passed, and the concrete values it compared ("VS `hosts` → `example-app.example.com` matches Gateway hosts").

Reviewers trusted that report because it showed *proof of work*: every check, done, with evidence.

Since the stable-reviews change, the answer is only a `summary` plus `findings` (problems). Checks that passed have no place to go, so the comment shows a verdict, a paragraph and a list of problems. It is accurate, but much less convincing than before.

## Goals

- **Checklist table:** a skill with a checklist gets that checklist back as a table in the comment. Each check shows its status and the evidence behind it, grouped by the skill's own sections.
- **pergent owns the layout:** model text fills table cells and cannot break the comment, as with findings and summaries.
- **No skill changes:** a skill needs no new syntax. Its existing checklist (headings and bullet or checkbox items) is what the model reports against.
- **Skills without a checklist** still work, with no table and no failure.

## Non-Goals

- Declaring checks in frontmatter or a machine-readable checklist format. The model reads the checklist from the skill's markdown.
- Deriving the verdict from checks. **Findings still decide the verdict**; checks are evidence.
- Storing checks in the hidden block, or carrying them across runs with `--previous-review`.
- A free-form markdown report (option 2 in the discussion). Not now.

## Anti-goals

- A table that says ✅ next to a ❌ verdict with no explanation, or the other way round, unless the comment says why.
- Checks parsing that costs a review its findings.
- A comment that becomes too long for the platform because of checks.

---

## Decisions

### 1. Answer format gains `checks`

```json
{
  "summary": "…",
  "checks": [
    {"section": "Cross-Reference Consistency", "name": "VirtualService gateways match the Gateway name", "status": "pass", "detail": "istio-system/example-app-gw"},
    {"section": "Field Validations", "name": "credentialName present for SIMPLE TLS", "status": "fail", "detail": "server 0 has tls.mode SIMPLE and no credentialName"},
    {"section": "File Completeness", "name": "ServiceEntry for external service", "status": "n/a", "detail": "routes stay inside the mesh"}
  ],
  "findings": [ … ]
}
```

- **Fields:**
  - `section` is optional: the checklist heading the check belongs to.
  - `name` is required: the check, in a few words.
  - `status` is one of `pass`, `fail` or `n/a`.
  - `detail` is optional: the concrete evidence, such as values compared, file and field.
- **Instructions:** pergent's output-format instructions add `checks`. "If the instructions above include a checklist, report every item in it as a check, in the checklist's order, using its section headings. Give the concrete evidence in `detail`. Every `fail` must also appear as a finding." With no checklist, the model answers `"checks": []`.
- **Parsing is tolerant**, like `summary`:
  - a missing `checks`, or one of the wrong type, means no checks;
  - an entry with no `name` or an unknown `status` is dropped;
  - none of this is ever an error, and none of it ever triggers a repair run.
- **Limits:**
  - at most **60 checks per answer** (the rest are dropped and the count is noted);
  - `name` at most 120 runes, `detail` at most 300, `section` at most 80;
  - each field is redacted before it is capped and collapsed to one line, the same as the summary.

### 2. Checks per skill

- `SkillResult` gains the checks of each finished batch, in batch order, as `[]BatchChecks{Batch int; Checks []Check}`.
- The repaired answer supplies the checks when a repair ran.
- Cross-batch and verification runs don't produce checks.

### 3. Rendering

**Placement, per skill:** verdict line, then summary, then **checks**, then blocking findings, then the collapsed blocks.

- **A count line** comes right under the summary: `**Checks:** 14 passed · 1 failed · 2 n/a`. Counts are omitted when they are zero; with no checks there is no line.
- **Single-batch skill:** one markdown table per section, in the order the sections first appear, each preceded by the section name in bold. Checks with no section go in a final group with no heading.

  ```
  **Cross-Reference Consistency**

  | Check | Status | Detail |
  |---|---|---|
  | VirtualService gateways match the Gateway name | ✅ | istio-system/example-app-gw |
  ```

  Status icons: ✅ pass, ❌ fail, ➖ n/a.
- **Multi-batch skill:** each batch's checks go in a collapsed block, `<details><summary>Checks — batch N of M (14 passed, 1 failed)</summary>`, with the same section tables inside. A batch with a failed check is rendered **open** (`<details open>`), so failures are never hidden.
- **Cell safety:**
  - newlines become spaces and `|` becomes `\|`;
  - `<` is escaped;
  - backticks are kept, so `code` renders. A cell whose backticks are unbalanced has them escaped, so no code span can run past the cell.

### 4. Checks vs findings vs verification

- The verdict is still computed from findings only (after verification).
- **A `fail` check with no matching finding** is shown as ❌ in the table, but the verdict doesn't change. The prompt requires a finding for every failure, so this should be rare. When it happens, the skill gets a note: `> 1 failed check has no matching finding; the verdict is based on findings.`
- **Verification drops a finding:** if a check's failure matched it, the table still shows ❌. The existing "Verification dropped …" note explains why, and it moves up to sit directly under the checks table for that skill, so the two are read together.
- "Matching" is approximate on purpose: a failed check counts as matched when there is at least one finding (blocking or suggestion) in the same batch. pergent doesn't try to link a specific check to a specific finding.

### 5. Comment size limit

`FormatReviewWithin` gains two steps for checks, after "drop suggestions" and before "drop summaries":

1. Collapse every checks table: render only the count line plus `<details>` blocks, closed unless they contain a failure.
2. Drop the passing and n/a rows, keeping failures, then drop the tables entirely and keep the count line. Each step notes it in the comment, e.g. `> Passed checks omitted to fit the comment size limit.`

### 6. Skills

- **No skill change is required.** The model maps the skill's existing checklist to checks.
- **The README** gets a short section: "Write a checklist with headings and items if you want a checks table; each item becomes a row."
- **A skill that already has a checklist** (for example, an ingress-validation skill with a "Validation Checklist") needs no change. Its checklist sections become the table sections.

---

## Constraints

- No new Go dependencies.
- The hidden findings block is unchanged, so comments from older versions still parse.
- Redaction, privacy and layout safety follow the same rules as findings and summaries.

## Left to the builder

- Exact wording of the new output-format bullets, within section 1.
- The exact text of size-limit notes and the "failed check has no matching finding" note.
