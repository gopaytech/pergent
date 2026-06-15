# pergent Previous Review Context -- Design Document

**Date:** 2026-06-12
**Status:** Draft

---

## Overview

When a user pushes new commits to a PR/MR that pergent already reviewed, pergent currently re-reviews from scratch -- the agent never sees the comment pergent posted on the previous run. This feature feeds the previous pergent comment back to the agent as context, so re-runs produce consistent feedback, save tokens/turns, and focus on what changed since the last review.

pergent stays **stateless**: the PR/MR comment itself is the only persistent state. Nothing is stored in the repo or between CI jobs.

---

## Goals

- **Consistent feedback** -- re-runs don't flip-flop or raise new nitpicks on unchanged code
- **Save tokens/turns** -- the agent doesn't re-explore everything (matters with tight limits like 30k tokens / 7 turns)
- **Incremental focus** -- the agent concentrates on changes since the previous review
- **Stateless** -- the previous comment is fetched from the platform API at runtime; no new storage

## Non-Goals

- Diffing only the new commits (the agent still receives the full MR diff, base...HEAD)
- Tracking issue resolution state structurally (no parsing of findings; the raw markdown is the context)
- Local mode support (there is no platform comment to fetch)

---

## Activation

Opt-in via flag or env var:

| Mechanism | Form |
|---|---|
| CLI flag | `--previous-review` (bool) |
| Env var | `PERGENT_PREVIOUS_REVIEW` (`true`/`1`/`false`/`0`, via `strconv.ParseBool`) |

### Resolution semantics

Bool flags cannot use the "zero means unset" convention that `resolveInt`/`resolveDuration` use, because `false` is both "not set" and "explicitly off". Resolution is therefore:

1. If the flag was **explicitly passed** on the command line (detected via `flag.Visit` in `cmd/main.go`) -> use its value
2. Else if `PERGENT_PREVIOUS_REVIEW` parses as a bool -> use that
3. Else -> default `false`

| `--previous-review` | `PERGENT_PREVIOUS_REVIEW` | Result |
|---|---|---|
| not passed | not set | off (default) |
| not passed | `true` | on |
| not passed | `false` | off |
| `--previous-review` or `=true` | anything | on |
| `--previous-review=false` | `true` | off (explicit flag beats env) |

Unparseable env values (e.g. `yes`) fall through to the default, consistent with how bad `PERGENT_MAX_TURNS` values are silently ignored.

### Errors

- `--previous-review` combined with `--local` -> error: `"--previous-review requires platform mode"` (same style as the existing `--local`/`--platform` exclusion)

---

## Data Flow

```
1. (start of run, when enabled)
   FindComment("<!-- pergent -->")          # GitLab notes / GitHub comments API
        |
        v  returns (commentID, body)
2. Per skill:
   ExtractSkillSection(body, skillName)     # cut <!-- pergent:NAME --> ... <!-- /pergent:NAME -->
        |
        v  non-empty section
3. Write temp file pergent-prev-review-*.md # same lifecycle as the diff temp file
        |
        v
4. opencode run --file diff.patch --file prev-review.md -- "<message>"
        |
        v
5. (end of run) Post/update comment
   reuse commentID from step 1              # no second FindComment lookup
```

The previous review is never read from the repo -- it is materialized from the platform comment at runtime and deleted when the run finishes.

---

## Changes Required

### `internal/config/resolver.go`

- Add `PreviousReview bool` and `PreviousReviewSet bool` to `Options`
- Add `PreviousReview bool` to `Config`
- Add `resolveBool(value bool, explicitlySet bool, envKey string, defaultVal bool) bool` implementing the resolution semantics above. The resolver stays a pure function -- it never touches the `flag` package; `cmd/main.go` computes `PreviousReviewSet` via `flag.Visit` and passes it in
- Error when `Local && PreviousReview` resolves to true

### `internal/platform/platform.go`, `github.go`, `gitlab.go`

- `FindComment(marker string) (commentID int64, err error)` becomes `FindComment(marker string) (commentID int64, body string, err error)`
- Both implementations already decode each comment's `body` while searching for the marker -- returning it adds **zero API calls**
- Not found stays `(0, "", nil)`

### `internal/output/formatter.go` (or sibling file in the same package)

- New `ExtractSkillSection(body string, skillName string) string`
- Returns the content between `<!-- pergent:NAME -->` and `<!-- /pergent:NAME -->` (the markers `FormatComment` already writes), trimmed
- Missing start or end marker -> empty string
- Lives next to the formatter so the marker format is written and parsed in one place

### `internal/runner/runner.go`

- `BuildCommand` and `Run` gain a `prevReviewFile string` parameter
- When non-empty, append a second `--file <prevReviewFile>` to the `opencode run` args
- The runner does **not** choose the message -- `cmd/main.go` does (it already passes the message in). When a previous review is attached for a skill, main.go passes:

  > Review the attached diff. Also attached is your previous review of an earlier revision of this change. Stay consistent with it: focus on what changed since, don't re-litigate unchanged code, and drop findings that the new changes resolve.

  Without a previous review it passes `"Review the attached diff"` (unchanged)

### `cmd/main.go`

- Add `--previous-review` flag; compute `PreviousReviewSet` via `flag.Visit` after `flag.Parse()`
- When `cfg.PreviousReview` (CI mode only):
  - Call `plat.FindComment(marker)` **before** running skills; keep `commentID` and `body`
  - On lookup error: log a warning and continue without context (a review must never fail because of this feature)
  - Per skill: extract the section, write it to a temp file (same pattern as `writeTempDiff`), pass it to `runner.Run`, remove it after the run
  - At post time: reuse the `commentID` from the initial lookup instead of searching again. If the initial lookup **errored**, retry `FindComment` before posting so a transient API blip doesn't create a duplicate comment
- When disabled: existing flow untouched (single `FindComment` at post time)

### Docs

- README: add `--previous-review` to the flag table, `PERGENT_PREVIOUS_REVIEW` to the env var table, and show `PERGENT_PREVIOUS_REVIEW: "true"` in the GitLab CI example

---

## Error Handling / Edge Cases

| Case | Behavior |
|---|---|
| First run (no existing comment) | No section, no attachment -- behaves exactly as today |
| `FindComment` API error at start | Warn, run without context, retry lookup before posting |
| Skill renamed / section missing from comment | Empty extraction -> no attachment for that skill |
| Comment manually edited or deleted | Taken at face value -- whatever is (or isn't) there is the truth |
| Feature disabled | Zero behavior change |

---

## Testing

- **Resolver** (`resolver_test.go`): flag explicit true/false, env true/false/garbage, default, precedence table above, `--local` + `--previous-review` error
- **Extraction** (`formatter_test.go`): section found, section missing, multiple skill sections (extracts only the named one), missing end marker
- **Platform** (`github_test.go`, `gitlab_test.go`, httptest): `FindComment` returns the matched comment's body; not-found returns empty body
- **Runner** (`runner_test.go`): `BuildCommand` includes the second `--file` when `prevReviewFile` is set, omits it when empty

---

## Out of Scope

- Incremental diffs (diffing only commits since the last review)
- Structured finding tracking (resolved/open state)
- Previous review context in `--local` mode
- Feeding other skills' sections or non-pergent comments as context
