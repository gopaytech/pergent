# pergent --local Mode -- Design Document

**Date:** 2026-03-31
**Status:** Draft

---

## Overview

Add a `--local` mode to pergent that runs skill reviews against a local git diff and prints the formatted review to stdout. No platform interaction, no token, no PR/MR required. This enables testing skills locally before pushing to CI.

---

## Two Modes

| | Local mode | Normal mode (CI) |
|---|---|---|
| Flag | `--local` | (default) |
| Diff source | `git diff origin/<base-branch>...HEAD` | Local git diff, fallback to platform API |
| Output | Printed to **stdout** | Posted as PR/MR comment |
| Platform needed | No | Yes |
| Token needed | No | Yes |
| PR/MR needed | No | Yes |

---

## New Flags

| Flag | Type | Default | Description |
|---|---|---|---|
| `--local` | boolean | `false` | Enables local mode: diff from git, output to stdout, no platform |
| `--base-branch` | string | `main` | Base branch for `git diff origin/<branch>...HEAD`. Used in local mode by default. Also usable in normal mode as override for CI-detected base branch. |

---

## CLI Usage

```bash
# Local mode with default base branch (main)
pergent --local --skill ./skills/code-review.md

# Local mode with custom base branch
pergent --local --base-branch develop --skill ./skills/code-review.md

# Multiple skills in local mode
pergent --local --skill ./skills/code-review.md --skill ./skills/security-review.md
```

---

## Behavior

### Local Mode (`--local`)

1. Parse flags and resolve config
   - Skip platform detection and validation (no token/PR checks)
   - `BaseBranch` defaults to `main` if not provided
2. Gather diff via `platform.LocalDiff(repoPath, baseBranch)`
   - Fatal error if git diff fails (no API fallback)
3. Load skills, build opencode config, run each skill (same as normal mode)
4. Format combined comment (same as normal mode)
5. Print formatted comment to **stdout**

### Normal Mode (default, unchanged)

No changes to existing behavior.

---

## Error Handling

- `--local` combined with `--platform` = error: `"--local and --platform are mutually exclusive"`
- `--local` but `git diff` fails = fatal error with the git error message
- `--local` without `--skill` = existing error: `"at least one --skill is required"`

---

## Changes Required

### `internal/config/resolver.go`

- Add `Local` (bool) and `BaseBranch` (string) fields to `Options` struct
- Add `Local` (bool) and `BaseBranch` (string) fields to `Config` struct
- When `opts.Local` is true:
  - Error if `opts.Platform` is also set
  - Skip platform detection and validation
  - Set `BaseBranch` to `main` if not provided via flag
- When `opts.Local` is false (normal mode):
  - Existing behavior unchanged
  - `BaseBranch` flag is ignored (normal mode uses CI-detected base branch)

### `cmd/main.go`

- Add `--local` and `--base-branch` flag parsing
- Pass `Local` and `BaseBranch` to `config.Resolve`
- When `cfg.Local`:
  - Call `platform.LocalDiff(cfg.RepoPath, cfg.BaseBranch)` directly
  - Skip platform client creation (`newPlatform`)
  - Skip `FindComment` / `CreateComment` / `UpdateComment`
  - Print formatted comment to `os.Stdout` instead

### No changes to:

- `internal/skill/loader.go`
- `internal/config/opencode.go`
- `internal/runner/runner.go`
- `internal/output/formatter.go`
- `internal/platform/platform.go`
- `internal/platform/github.go`
- `internal/platform/gitlab.go`

---

## Out of Scope

- `--diff-file` flag for providing a pre-generated diff file
- Running locally but posting to a real PR
- Auto-detecting base branch from git
- `--base-branch` as override in normal (CI) mode
- Extracting shared skill-execution logic into a helper function
