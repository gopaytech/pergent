# Confidence and one-click suggestions (pergent v0.2.0) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The model's answer carries a 1–5 merge-confidence score and, for small fixes, an exact replacement (`suggestion`) over `line`..`end_line`; pergent carries both through the review, the hidden findings block, the agent result and `render`, which applies the confidence caps and shows them in the comment.

**Architecture:** `internal/finding` reads the new fields loosely (a bad value is dropped, never a format error). `internal/review` folds batch scores into one model score per skill. `render` (public) owns the caps (`SkillScore`, `ReviewScore`) so CI mode and pergent.sh compute the same number, and draws a confidence line plus a skills table at the top of the comment, a suggestion block under a finding, and two options pergent.sh needs (`Inline`, `Footer`). A result with no model score anywhere renders byte for byte as today.

**Tech Stack:** Go (stdlib only), existing golden tests in `render/testdata/golden`.

**Spec:** `../pergent.sh/docs/superpowers/specs/2026-11-03-inline-findings-design.md` (in the pergent.sh repository, sections "How it works" 1–2, the Decisions table and "Settled" 5). Only the pergent part is in this plan; pergent.sh's inline threads get their own plan.

## Global Constraints

- Public repository: no employer, internal hostnames, people or screenshots in code, docs, tests or commits.
- Work on a branch `feat/confidence-suggestions` cut from `origin/master`. Never push, tag or open a PR without the owner's go-ahead.
- `render` imports no pergent `internal/` package and redacts nothing; redaction happens in `internal/output` and `internal/result`.
- Confidence is information only: no verdict, approval or exit code may depend on it.
- A missing or invalid `confidence`, `confidence_reason`, `end_line` or `suggestion` is never a format error and never triggers a repair run.
- A suggestion may replace at most **10** lines (`end_line - line + 1 <= 10`); `line` must be set.
- Caps (applied by `render`, after dismissals): a blocking finding ⇒ at most **3**; a failed skill ⇒ not scored; a skipped changed file under `deleted`, `binary`, `vendored`, `lockfile` or `generated` ⇒ at most **4**. The review's score is the lowest scored skill's.
- The result and the hidden block store the **model's own** score; caps are applied only when rendering.
- Agent result stays `schema_version: 1` (the schema doc allows adding fields); the hidden block stays version 1.
- Fingerprints don't change: `end_line` and `suggestion` are not part of them.
- A result, comment or block that carries no confidence and no suggestion renders byte for byte as today: the existing goldens in `render/testdata/golden` must not change.
- Version becomes `0.2.0` (`cmd/main.go`).

## Review Focus

1. The model sends `"confidence": "4"`, `4.5`, `0` or `7` → that batch is "not scored", the findings are kept, no repair run (Task 1).
2. `"end_line": "45"` or `"suggestion": 123` or `"suggestion"` with no `line` → the finding is kept without the bad field, no format error (Task 1).
3. A suggestion holding ```` ``` ````, `<!-- pergent:findings ... -->` or a line starting with `/` → the comment's fences stay balanced, `ParsePreviousFindings` still reads the real block, no quick action runs (Task 5).
4. A token in `suggestion` or `confidence_reason` → redacted in the comment, the hidden block and the agent result (Tasks 6, 7).
5. A two-batch skill where one batch omits `confidence` → the skill is "not scored", not scored from the other batch alone (Task 3).

---

### Task 1: Read confidence, end line and suggestion from the answer

**Files:**
- Modify: `internal/finding/finding.go` (the `Finding` struct)
- Modify: `internal/finding/parse.go` (`Answer`, `answer`, `parseBlock`)
- Test: `internal/finding/parse_test.go`

**Interfaces:**
- Produces:
  - `finding.Finding` gains `EndLine int \`json:"end_line,omitempty"\`` and `Suggestion string \`json:"suggestion,omitempty"\`` (after `SuggestedFix`, before `Fingerprint`).
  - `finding.Answer` gains `Confidence int` (1–5; 0 = not scored) and `ConfidenceReason string`.
  - `const MaxSuggestionLines = 10` in `internal/finding`.

- [ ] **Step 1: Write the failing tests** in `parse_test.go`:

```go
func TestParseAnswer_Confidence(t *testing.T) {
	for _, c := range []struct {
		raw        string
		want       int
		wantReason string
	}{
		{`4`, 4, "Looks fine."},
		{`4.0`, 4, "Looks fine."},
		{`1`, 1, "Looks fine."},
		{`5`, 5, "Looks fine."},
		{`0`, 0, ""},
		{`6`, 0, ""},
		{`4.5`, 0, ""},
		{`"4"`, 0, ""},
		{`null`, 0, ""},
	} {
		text := "```json\n{\"confidence\": " + c.raw + ", \"confidence_reason\": \"Looks  fine.\", \"findings\": []}\n```"
		a, err := ParseAnswer(text)
		if err != nil {
			t.Fatalf("%s: unexpected error %v", c.raw, err)
		}
		if a.Confidence != c.want || a.ConfidenceReason != c.wantReason {
			t.Errorf("%s: got (%d, %q), want (%d, %q)", c.raw, a.Confidence, a.ConfidenceReason, c.want, c.wantReason)
		}
	}
	a, err := ParseAnswer("```json\n{\"findings\": []}\n```")
	if err != nil || a.Confidence != 0 {
		t.Errorf("missing confidence: got (%d, %v), want (0, nil)", a.Confidence, err)
	}
}

func TestParseAnswer_ConfidenceReasonCappedAndRedacted(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_secretvalue123")
	long := strings.Repeat("x", 400)
	a, err := ParseAnswer("```json\n{\"confidence\": 3, \"confidence_reason\": \"ghp_secretvalue123 " + long + "\", \"findings\": []}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(a.ConfidenceReason, "ghp_secretvalue123") {
		t.Error("reason not redacted")
	}
	if n := len([]rune(a.ConfidenceReason)); n != 300 {
		t.Errorf("reason is %d runes, want 300", n)
	}
}

func TestParseAnswer_EndLineAndSuggestion(t *testing.T) {
	for _, c := range []struct {
		name, fields   string
		wantEnd        int
		wantSuggestion string
	}{
		{"both valid", `"line": 42, "end_line": 43, "suggestion": "a := 1\nb := 2\n"`, 43, "a := 1\nb := 2"},
		{"single line", `"line": 42, "suggestion": "x"`, 0, "x"},
		{"ten lines", `"line": 1, "end_line": 10, "suggestion": "x"`, 10, "x"},
		{"eleven lines", `"line": 1, "end_line": 11, "suggestion": "x"`, 11, ""},
		{"end before line", `"line": 42, "end_line": 41, "suggestion": "x"`, 0, "x"},
		{"no line", `"suggestion": "x"`, 0, ""},
		{"end_line string", `"line": 42, "end_line": "43", "suggestion": "x"`, 0, "x"},
		{"suggestion number", `"line": 42, "suggestion": 123`, 0, ""},
		{"empty suggestion", `"line": 42, "suggestion": ""`, 0, ""},
	} {
		text := "```json\n{\"findings\": [{\"file\": \"a.go\", \"severity\": \"suggestion\", \"title\": \"T\", \"explanation\": \"E\", " + c.fields + "}]}\n```"
		a, err := ParseAnswer(text)
		if err != nil {
			t.Fatalf("%s: unexpected error %v", c.name, err)
		}
		f := a.Findings[0]
		if f.EndLine != c.wantEnd || f.Suggestion != c.wantSuggestion {
			t.Errorf("%s: got (%d, %q), want (%d, %q)", c.name, f.EndLine, f.Suggestion, c.wantEnd, c.wantSuggestion)
		}
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/finding/ -run 'TestParseAnswer_(Confidence|EndLine)'`
Expected: FAIL (compile errors: unknown fields `Confidence`, `EndLine`, `Suggestion`).

- [ ] **Step 3: Implement**

- `answer` gains `Confidence json.RawMessage \`json:"confidence"\`` and `ConfidenceReason json.RawMessage \`json:"confidence_reason"\``; its findings decode with `end_line` and `suggestion` as `json.RawMessage` (for example a wrapper struct embedding `Finding` whose two `json.RawMessage` fields shadow `Finding`'s), so a wrong type never fails the block. Existing `validate` rules are unchanged.
- Confidence: a JSON number with an integer value in 1..5, else 0. `ConfidenceReason` is `cleanText(stringValue(raw), 300)` (`const maxConfidenceReason = 300`), and is `""` whenever `Confidence` is 0.
- `EndLine`: kept only when it is a JSON integer and `line > 0 && end_line >= line`; else 0.
- `Suggestion`: a JSON string with one trailing `"\n"` removed (and `"\r\n"` line endings turned into `"\n"`); dropped (`""`) when `line == 0`, or when `last - line + 1 > MaxSuggestionLines` where `last` is `EndLine` if set, else `line`. It is not trimmed or collapsed otherwise: indentation is part of the fix. It is not redacted here; `internal/output` and `internal/result` redact it.
- Keep the 11-line case's `EndLine` (it is a valid range); only the suggestion goes.

- [ ] **Step 4: Run the package**

Run: `go test ./internal/finding/`
Expected: PASS (old tests included).

- [ ] **Step 5: Commit**

```bash
git add internal/finding
git commit -m "feat(finding): read confidence, end_line and suggestion loosely"
```

---

### Task 2: Ask for them in the prompt

**Files:**
- Modify: `internal/skill/prompt.go` (`outputFormatSection`)
- Test: `internal/skill/prompt_test.go`

**Interfaces:**
- Consumes: `finding.MaxSuggestionLines` is 10 (keep the prompt's "at most 10 lines" in step with it; a test pins both).

- [ ] **Step 1: Write the failing test**

```go
func TestPrompt_AsksForConfidenceAndSuggestion(t *testing.T) {
	p := Skill{Name: "a", Body: "Review it."}.Prompt()
	for _, want := range []string{
		`"confidence": 4`,
		`"confidence_reason"`,
		`"end_line": 43`,
		`"suggestion"`,
		"how safe this change is to merge, judged only from what you reviewed",
		"at most 10 lines",
		"Otherwise describe the fix in `suggested_fix`",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
	if !strings.Contains(p, fmt.Sprintf("at most %d lines", finding.MaxSuggestionLines)) {
		t.Error("prompt's line limit differs from finding.MaxSuggestionLines")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/skill/ -run TestPrompt_AsksForConfidenceAndSuggestion`
Expected: FAIL with `prompt missing "\"confidence\": 4"`.

- [ ] **Step 3: Extend `outputFormatSection`**

In the JSON example, add `"confidence": 4, "confidence_reason": "Why you chose this score, in one sentence."` after `summary`, and to the finding `"end_line": 43` after `line` and `"suggestion": "Optional exact replacement for lines line to end_line"` after `suggested_fix`. Add these bullets after the `line` bullet (wording may be polished; the asserted substrings must stay):

- "`confidence` is 1-5: how safe this change is to merge, judged only from what you reviewed. 5: nothing you found should stop a merge. 4: minor concerns only. 3: a problem that should be fixed before merging. 2: serious problems. 1: merging would very likely break something. `confidence_reason` says why, in one sentence."
- "`end_line` is the last line the finding covers, when it covers more than `line`. Omit it otherwise."
- "`suggestion` is optional: the exact text that replaces lines `line` to `end_line` (or only `line`) of the new version, with the original indentation. Give it only when the whole fix fits in those lines, changes nothing else, and covers at most 10 lines. Otherwise describe the fix in `suggested_fix` and leave `suggestion` out."

- [ ] **Step 4: Run the package**

Run: `go test ./internal/skill/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/skill
git commit -m "feat(skill): ask for confidence and small-fix suggestions"
```

---

### Task 3: One model score per skill

**Files:**
- Modify: `internal/review/review.go` (`SkillResult`, `batchOutcome`, `answered`, `reviewSkill`)
- Test: `internal/review/review_test.go`

**Interfaces:**
- Consumes: `finding.Answer.Confidence`, `.ConfidenceReason` (Task 1).
- Produces: `review.SkillResult` gains `Confidence int` (1–5; 0 = not scored) and `ConfidenceReason string`: the model's own score, no caps.

Rule: the skill's score is the lowest score among the batches that finished; the reason is that batch's (the first such batch in batch order on a tie). The skill is not scored (0, `""`) when no batch finished, or when any finished batch has no score. The cross-batch run's and the verification run's answers don't change it. A repaired answer's score counts like any other.

- [ ] **Step 1: Write the failing tests**

A test-local helper `scored(t, score int, reason string) string` returning ```` "```json\n{\"confidence\": N, \"confidence_reason\": \"...\", \"findings\": []}\n```" ````, then:

```go
func TestRun_ConfidenceIsLowestBatch(t *testing.T) {
	f := &fake{respond: func(_ int, c call) runner.RunResult {
		if strings.Contains(c.opts.Message, "batch 1 of 2") {
			return completed(scored(t, 4, "Minor naming."))
		}
		return completed(scored(t, 2, "Retry loop never ends."))
	}}
	req := newRequest(t, f, twoBatchDiff())
	req.ContextLimit = twoBatchContextLimit
	res := Run(context.Background(), req)[0]
	if res.Confidence != 2 || res.ConfidenceReason != "Retry loop never ends." {
		t.Errorf("got (%d, %q), want (2, %q)", res.Confidence, res.ConfidenceReason, "Retry loop never ends.")
	}
}

func TestRun_ConfidenceUnscoredWhenABatchHasNone(t *testing.T) {
	// batch 1 answers with a score, batch 2 with answer(t) (no confidence)
	// want res.Confidence == 0 && res.ConfidenceReason == ""
}

func TestRun_ConfidenceUnscoredWhenNoFilesInScope(t *testing.T) {
	// as TestRun_NoFilesInScope; want res.Confidence == 0
}
```

(Match how the existing two-batch tests pick a batch; if they use another field than `Message`, use that. Write the second and third bodies in the same style.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/review/ -run TestRun_Confidence`
Expected: FAIL (compile error: `res.Confidence` undefined).

- [ ] **Step 3: Implement** `batchOutcome` gains `confidence int` and `confidenceReason string`, set by `answered`; `reviewSkill` folds them as above while it collects outcomes.

- [ ] **Step 4: Run the package**

Run: `go test ./internal/review/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/review
git commit -m "feat(review): one model confidence per skill, lowest batch wins"
```

---

### Task 4: render — scores, caps, the confidence line and the skills table

**Files:**
- Create: `render/confidence.go`
- Modify: `render/render.go` (`Finding`, `Skill`, `render`, `blockFinding`, `findingsBlock`, `FindingsBlock`)
- Test: `render/confidence_test.go`

**Interfaces:**
- Produces:
  - `render.Finding` gains `EndLine int` and `Suggestion string`.
  - `render.Skill` gains `Confidence int` (the model's score, 1–5; 0 = none) and `ConfidenceReason string`.
  - `type Score struct { Value int; Capped string }`: `Value` 1–5 or 0 for not scored; `Capped` says why `Value` is below the model's score, e.g. `"1 blocking finding"`, `""` when not capped.
  - `func SkillScore(s Skill) Score`
  - `func ReviewScore(skills []Skill) (Score, int)`: the lowest scored skill's `Score` and its index (first on a tie); `(Score{}, -1)` when none is scored.
  - Hidden block: `blockFinding` gains `EndLine int \`json:"end_line,omitempty"\`` and `Suggestion string \`json:"suggestion,omitempty"\``; `findingsBlock` gains `Confidence map[string]blockConfidence \`json:"confidence,omitempty"\`` with `type blockConfidence struct { Score int \`json:"score"\`; Reason string \`json:"reason,omitempty"\` }`, holding only skills with a model score. Version stays 1.

`SkillScore` rules, in order: `Verdict == VerdictFailed` or `Confidence` outside 1..5 → `Score{}`. Let `b` = findings in `s.Findings` that are not `Suggestion` (dismissed ones are already in `s.Dismissed`, so `ApplyDismissals` first restores the model's score). If `b > 0` and `Confidence > 3` → `{3, plural(b, "blocking finding")}`. Else let `k` = entries of `s.Skipped` whose `Rule` is one of `deleted`, `binary`, `vendored`, `lockfile`, `generated`; if `k > 0` and `Confidence > 4` → `{4, plural(k, "skipped file") + " not reviewed"}`. Else `{Confidence, ""}`.

Comment layout: when **no** skill has `Confidence` in 1..5, `render` writes exactly what it writes today. Otherwise, right after `## pergent review\n\n` and before the changed-files block, it writes this, kept in every shrink layout:

```
**Confidence: 3/5** (capped: 1 blocking finding) — `code-review`: One blocking issue: a nil map write.

| Skill | Verdict | Confidence | Findings |
|---|---|---|---|
| `code-review` | ❌ Has blocking issues | 3/5 (capped) | 1 blocking · 2 suggestions |
| `security` | ✅ No blocking issues | 5/5 | none |
| `docs` | ⚠️ Failed | not scored | none |

```

- First line: `**Confidence: N/5**`, then ` (capped: <Capped>)` when capped, then ` — ` + `codeSpan(name)` + `: ` + `mentionSafe(reason)` when the lowest skill's reason is non-empty. With no skill scored (all failed, say): `**Confidence: not scored**` alone.
- Table: one row per skill, in order. Skill cell `codeSpan(name)` passed through `checkCell`. Verdict cell `✅ No blocking issues`, `❌ Has blocking issues` or `⚠️ Failed`. Confidence cell `N/5`, `N/5 (capped)` or `not scored`. Findings cell counts `s.Findings` (dismissed ones are not in it): `none`, or `"%d blocking"` and `plural(n, "suggestion")`, whichever are non-zero, joined by ` · `, blocking first.

- [ ] **Step 1: Write the failing tests** in `render/confidence_test.go`:

```go
func TestSkillScore(t *testing.T) {
	blocking := Finding{File: "a.go", Severity: Blocking, Title: "B"}
	sugg := Finding{File: "a.go", Severity: Suggestion, Title: "S"}
	for _, c := range []struct {
		name string
		s    Skill
		want Score
	}{
		{"model score kept", Skill{Verdict: VerdictNoBlockingIssues, Confidence: 5, Findings: []Finding{sugg}}, Score{5, ""}},
		{"not scored", Skill{Verdict: VerdictNoBlockingIssues}, Score{}},
		{"out of range", Skill{Verdict: VerdictNoBlockingIssues, Confidence: 9}, Score{}},
		{"failed", Skill{Verdict: VerdictFailed, Confidence: 5}, Score{}},
		{"blocking caps at 3", Skill{Verdict: VerdictHasBlockingIssues, Confidence: 5, Findings: []Finding{blocking}}, Score{3, "1 blocking finding"}},
		{"two blocking", Skill{Verdict: VerdictHasBlockingIssues, Confidence: 4, Findings: []Finding{blocking, blocking}}, Score{3, "2 blocking findings"}},
		{"blocking under cap", Skill{Verdict: VerdictHasBlockingIssues, Confidence: 2, Findings: []Finding{blocking}}, Score{2, ""}},
		{"skipped generated caps at 4", Skill{Verdict: VerdictNoBlockingIssues, Confidence: 5, Skipped: []Skipped{{"gen.go", "generated"}}}, Score{4, "1 skipped file not reviewed"}},
		{"out of scope doesn't cap", Skill{Verdict: VerdictNoBlockingIssues, Confidence: 5, Skipped: []Skipped{{"x.md", "out of scope"}, {"y.go", "excluded by skill"}, {"z.go", "no content changes"}}}, Score{5, ""}},
		{"blocking cap wins", Skill{Verdict: VerdictHasBlockingIssues, Confidence: 5, Findings: []Finding{blocking}, Skipped: []Skipped{{"go.sum", "lockfile"}}}, Score{3, "1 blocking finding"}},
	} {
		if got := SkillScore(c.s); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestSkillScore_DismissalRestoresModelScore(t *testing.T) {
	s := Skill{Name: "a", Verdict: VerdictHasBlockingIssues, Confidence: 5,
		Findings: []Finding{{Fingerprint: "fp1", File: "a.go", Severity: Blocking, Title: "B"}}}
	got := SkillScore(ApplyDismissals([]Skill{s}, []Dismissal{{Fingerprint: "fp1", Severity: "blocking", By: "x", Reason: "r"}})[0])
	if got != (Score{5, ""}) {
		t.Errorf("got %+v, want {5 }", got)
	}
}

func TestReviewScore(t *testing.T) {
	skills := []Skill{
		{Name: "a", Verdict: VerdictNoBlockingIssues, Confidence: 4},
		{Name: "b", Verdict: VerdictFailed, Confidence: 1},
		{Name: "c", Verdict: VerdictNoBlockingIssues, Confidence: 4},
	}
	if got, i := ReviewScore(skills); got != (Score{4, ""}) || i != 0 {
		t.Errorf("got %+v at %d, want {4 } at 0", got, i)
	}
	if _, i := ReviewScore([]Skill{{Verdict: VerdictFailed, Confidence: 3}}); i != -1 {
		t.Errorf("all failed: index %d, want -1", i)
	}
}

func TestComment_ConfidenceHeaderAndTable(t *testing.T) {
	skills := []Skill{
		{Name: "code-review", Verdict: VerdictHasBlockingIssues, Confidence: 4, ConfidenceReason: "One blocking issue: a nil map write.",
			Findings: []Finding{
				{File: "a.go", Line: 1, Severity: Blocking, Title: "B"},
				{File: "a.go", Line: 2, Severity: Suggestion, Title: "S1"},
				{File: "a.go", Line: 3, Severity: Suggestion, Title: "S2"},
			}},
		{Name: "security", Verdict: VerdictNoBlockingIssues, Confidence: 5, Findings: []Finding{}},
		{Name: "docs", Verdict: VerdictFailed, Confidence: 5, Findings: []Finding{}},
	}
	out := Comment(skills, Options{})
	want := "## pergent review\n\n" +
		"**Confidence: 3/5** (capped: 1 blocking finding) — `code-review`: One blocking issue: a nil map write.\n\n" +
		"| Skill | Verdict | Confidence | Findings |\n|---|---|---|---|\n" +
		"| `code-review` | ❌ Has blocking issues | 3/5 (capped) | 1 blocking · 2 suggestions |\n" +
		"| `security` | ✅ No blocking issues | 5/5 | none |\n" +
		"| `docs` | ⚠️ Failed | not scored | none |\n\n"
	if !strings.Contains(out, want) {
		t.Errorf("comment does not start with the confidence header and table:\n%s", out)
	}
}

func TestComment_ConfidenceKeptAtSmallestLimit(t *testing.T) {
	// one skill with Confidence 4 and a long blocking explanation;
	// Comment(skills, Options{MaxLen: 400}) still contains "**Confidence: 4/5**"
}

func TestComment_NoConfidenceUnchanged(t *testing.T) {
	// a skill with Confidence 0: the comment contains neither "Confidence" nor "| Skill |"
}

func TestFindingsBlock_CarriesModelScoreAndSuggestion(t *testing.T) {
	// a skill {Confidence: 5, one blocking finding with EndLine 43, Suggestion "x"}:
	// decode the block's base64 JSON; want confidence["s"] == {score 5} (the model's,
	// not the capped 3), and the finding's end_line 43 and suggestion "x".
	// A skill with Confidence 0 has no entry in "confidence".
}
```

Write the four stubbed bodies in the same style.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./render/ -run 'TestSkillScore|TestReviewScore|TestComment_Confidence|TestComment_NoConfidence|TestFindingsBlock_Carries'`
Expected: FAIL (compile errors: `SkillScore`, `Score`, `Confidence` undefined).

- [ ] **Step 3: Implement** `SkillScore`, `ReviewScore` and the header writer in `render/confidence.go`; call the writer from `render`; add the block fields.

- [ ] **Step 4: Run the whole package, goldens included**

Run: `go test ./render/`
Expected: PASS, and `git status render/testdata` shows no change (old inputs have no confidence, so their comments and blocks are byte for byte the same).

- [ ] **Step 5: Commit**

```bash
git add render
git commit -m "feat(render): confidence scores with caps, header line and skills table"
```

---

### Task 5: render — suggestion block, inline findings, footer

**Files:**
- Modify: `render/render.go` (`writeFinding`, `writeSkill`, `render`, `Options`)
- Test: `render/suggestion_test.go`

**Interfaces:**
- Consumes: `Finding.EndLine`, `Finding.Suggestion` (Task 4).
- Produces:
  - `Options.Inline map[string]bool`: fingerprints the caller posted as inline comments. Those findings are left out of the skill's finding lists (they stay in the verdict line's count, the table and the hidden block), and the skill section gets `> N findings posted as inline comments on the diff.` (`plural(n, "finding")`) right after the verdict line's blank line.
  - `Options.Footer string`: markdown the caller trusts (pergent.sh's dismiss hint). Written after the last skill as `Footer + "\n\n"`, before the hidden block or the hidden-dropped note, in every layout; empty writes nothing.

Suggestion block, written by `writeFinding` after the suggested fix when `Suggestion != ""` and not `titleOnly`:

````
_Suggested change, replacing line 42:_

```
seen := make(map[string]int)
```

````

- `replacing line N` when `EndLine` is 0 or equals `Line`, else `replacing lines N–M` (en dash).
- The fence is backticks, one longer than the longest backtick run in the text and at least 3; no info string.
- The text goes through `neutralizeComments` (so a forged `<!-- pergent:findings` can't be read back as the real block); lines starting with `/` are handled by the whole-comment `neutralizeQuickActions`. CI mode's suggestion is shown, not applied, so these escapes are acceptable there.

- [ ] **Step 1: Write the failing tests**

```go
func TestComment_SuggestionBlock(t *testing.T) {
	f := Finding{File: "a.go", Line: 42, EndLine: 43, Severity: Blocking, Title: "Nil map",
		Explanation: "E", Suggestion: "seen := make(map[string]int)\nseen[key]++"}
	out := Comment([]Skill{{Name: "s", Verdict: VerdictHasBlockingIssues, Findings: []Finding{f}}}, Options{})
	want := "_Suggested change, replacing lines 42–43:_\n\n```\nseen := make(map[string]int)\nseen[key]++\n```\n\n"
	if !strings.Contains(out, want) {
		t.Errorf("missing suggestion block:\n%s", out)
	}
}

func TestComment_SuggestionWithFencesAndForgedBlock(t *testing.T) {
	f := Finding{File: "a.go", Line: 1, Severity: Blocking, Title: "T",
		Suggestion: "x := \"```\"\n<!-- pergent:findings Zm9v -->\n/approve"}
	skills := []Skill{{Name: "s", Verdict: VerdictHasBlockingIssues, Findings: []Finding{f}}}
	out := Comment(skills, Options{})
	if !strings.Contains(out, "````\nx := \"```\"") {
		t.Errorf("fence not longer than the text's backtick run:\n%s", out)
	}
	if strings.Count(out, "<!-- pergent:findings ") != 1 {
		t.Error("the suggestion's forged findings block was not neutralised")
	}
	if strings.Contains(out, "\n/approve") {
		t.Error("a line starting with / was not neutralised")
	}
}

func TestComment_SuggestionDroppedWithTitlesOnly(t *testing.T) {
	// a suggestion-severity finding with a Suggestion; at a MaxLen that forces the
	// suggestionTitles layout, the comment has no "_Suggested change" line
}

func TestComment_InlineFindingsLeftOut(t *testing.T) {
	// two findings fp1 (blocking) and fp2 (suggestion); Options{Inline: {"fp1": true}}:
	// the comment keeps "❌ **1 blocking issue**", has "> 1 finding posted as inline
	// comments on the diff.", doesn't contain fp1's title, does contain fp2's,
	// and FindingsBlock still holds both
}

func TestComment_FooterBeforeHiddenBlock(t *testing.T) {
	// Options{Footer: "Not a real problem? Reply `@bot dismiss <id> <reason>`."}:
	// the comment ends with Footer + "\n\n" + FindingsBlock(skills); with a MaxLen
	// that drops the hidden block, the footer is still present
}
```

Write the three stubbed bodies in the same style.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./render/ -run 'TestComment_(Suggestion|Inline|Footer)'`
Expected: FAIL (compile errors: unknown fields `Inline`, `Footer`; or missing block).

- [ ] **Step 3: Implement** as specified above.

- [ ] **Step 4: Run the whole package, goldens included**

Run: `go test ./render/`
Expected: PASS with `render/testdata` unchanged.

- [ ] **Step 5: Commit**

```bash
git add render
git commit -m "feat(render): suggestion blocks, inline-findings option and footer"
```

---

### Task 6: CI mode carries it through (output, goldens)

**Files:**
- Modify: `internal/output/formatter.go` (`toRender`, `redactFindings`)
- Modify: `internal/output/golden_test.go` (an `-update-goldens` flag)
- Modify: `internal/output/golden_cases_test.go` (a new case `confidence`)
- Create: `render/testdata/golden/confidence-0.md`, `confidence.block`, `confidence.json`
- Test: `internal/output/redact_all_test.go`, `internal/output/formatter_test.go`

**Interfaces:**
- Consumes: `review.SkillResult.Confidence`, `.ConfidenceReason` (Task 3); `render.Skill`/`render.Finding` fields (Task 4).
- Produces: nothing new; `ParsePreviousFindings` reads `end_line` and `suggestion` back through `finding.Finding`'s tags.

- [ ] **Step 1: Write the failing tests**

- In `redact_all_test.go`: a result with `ConfidenceReason` and a finding `Suggestion` both holding the planted `GITHUB_TOKEN` value; neither the comment from `FormatReviewWithinOptions` nor `FindingsBlock` contains it.
- In `formatter_test.go`, `TestFormatReview_ConfidenceAndSuggestion`: a `SkillResult{Confidence: 4, ConfidenceReason: "R", Findings: [blocking with Line 42, EndLine 43, Suggestion "x"]}`; the comment contains `**Confidence: 3/5** (capped: 1 blocking finding)` and `_Suggested change, replacing lines 42–43:_`.
- `TestParsePreviousFindings_KeepsSuggestion`: `ParsePreviousFindings(FormatReview(...))` returns the finding with `EndLine 43` and `Suggestion "x"`.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/output/ -run 'Confidence|Suggestion|Redact'`
Expected: FAIL (the comment has no confidence line; fields not copied).

- [ ] **Step 3: Implement**

- `toRender` copies `Confidence`, `redact.String(ConfidenceReason)`, `EndLine`, and the suggestion; `redactFindings` also redacts `Suggestion`.
- `golden_test.go`: add `var updateGoldens = flag.Bool("update-goldens", false, "rewrite the expected comments and blocks in render/testdata/golden")`; when set, `TestFormatMatchesGoldens` writes each `<case>-<n>.md` and `<case>.block` instead of comparing.
- `golden_cases_test.go`: add case `confidence` with `maxLens: []int{0}`: skill `code-review` (Confidence 4, a reason, one blocking finding at lines 42–43 with a two-line suggestion, one suggestion finding without one), skill `security` (Confidence 5, one `generated` skip), skill `docs` (one failed batch).
- Generate: `go test ./internal/output/ -run TestFormatMatchesGoldens -update-goldens -update-golden-inputs`.

- [ ] **Step 4: Check the goldens and run everything**

Run: `git status --short render/testdata/golden`
Expected: only the three new `confidence*` files are added; no existing golden file is modified. Read `confidence-0.md` and check it against Tasks 4–5's layout.

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/output render/testdata/golden
git commit -m "feat(output): confidence and suggestions in the CI comment"
```

---

### Task 7: Agent result fields and schema doc

**Files:**
- Modify: `internal/result/result.go` (`Skill`, `Finding`, the skill builder, `buildFinding`, `redactSkill`)
- Modify: `docs/agent-result-schema.md`
- Test: `internal/result/result_test.go`

**Interfaces:**
- Produces (JSON, schema version 1):
  - each skill: `confidence` (number 1–5, omitted when not scored) and `confidence_reason` (string, omitted when empty): the model's own score, **before** caps.
  - each finding: `end_line` (omitted when 0) and `suggestion` (omitted when empty).

- [ ] **Step 1: Write the failing tests**

- `TestBuild_ConfidenceAndSuggestion`: build a result from a `SkillResult{Confidence: 5, ConfidenceReason: "R", Findings: [blocking, EndLine 43, Suggestion "x"]}`; the encoded JSON has `"confidence":5` (not 3), `"confidence_reason":"R"`, `"end_line":43`, `"suggestion":"x"`.
- `TestBuild_NoConfidenceOmitted`: with `Confidence: 0` the encoded skill has no `confidence` or `confidence_reason` key and findings have no `end_line`/`suggestion` key.
- Extend the redaction test: a planted token in `ConfidenceReason` and `Suggestion` is absent from the encoded result.

(Use the existing builder entry point the other `result_test.go` tests call.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/result/ -run 'Confidence|Redact'`
Expected: FAIL.

- [ ] **Step 3: Implement** the fields, copy them in the builders, redact `ConfidenceReason` and `Suggestion` in `redactSkill`. In `docs/agent-result-schema.md`: add the four rows to the skill and finding tables, `confidence` to the completed-result example, and one paragraph under the skill table: "`confidence` is the model's own score. A caller showing it applies the caps first: a blocking finding caps it at 3, a failed skill is not scored, and a `deleted`, `binary`, `vendored`, `lockfile` or `generated` skip caps it at 4 (`render.SkillScore` does this). It is information only and says nothing about approval."

- [ ] **Step 4: Run the package**

Run: `go test ./internal/result/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/result docs/agent-result-schema.md
git commit -m "feat(result): confidence, end_line and suggestion in the agent result"
```

---

### Task 8: Docs and version

**Files:**
- Modify: `README.md` ("💬 The review comment")
- Modify: `CLAUDE.md` (Key Concepts "Structured findings", Architecture `render/` line)
- Modify: `cmd/main.go` (`const version`)

- [ ] **Step 1: Edit**

- README: before the per-skill list, say the comment starts with a confidence line and a skills table when the model scored the review, with the caps in one sentence and "It is information only: it never changes a verdict." Add a bullet for the suggestion block ("for a small fix, the exact replacement code, as a block under the finding"). Update the shrink paragraph: the confidence line and the table are always kept.
- CLAUDE.md: "Structured findings" adds `confidence` 1–5 with `confidence_reason`, and per finding optional `end_line` and `suggestion` (at most 10 lines; bad values dropped, never a repair). The `render/` line adds `SkillScore`/`ReviewScore` (caps) and the `Inline`/`Footer` options.
- `cmd/main.go`: `const version = "0.2.0"`.

- [ ] **Step 2: Verify**

Run: `make build && make test && gofmt -l . && go vet ./...`
Expected: build succeeds, tests PASS, `gofmt -l` prints nothing, vet is clean.

Run the owner's private keyword scan (the employer names are kept out of this public repository).
Expected: no matches.

- [ ] **Step 3: Commit**

```bash
git add README.md CLAUDE.md cmd/main.go
git commit -m "docs: confidence and suggestions; v0.2.0"
```
