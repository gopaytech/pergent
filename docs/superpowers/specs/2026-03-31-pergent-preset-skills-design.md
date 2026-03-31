# pergent Preset Skills -- Design Document

**Date:** 2026-03-31
**Status:** Draft

---

## Overview

Add preset skills embedded in the pergent binary via `//go:embed`. Users can reference preset skills by name instead of file path. The `--skill` flag auto-detects whether the value is a preset name or a file path.

---

## Auto-Detection Logic

The `--skill` flag value is treated as:
- **File path** if it contains `/` or ends with `.md`
- **Preset name** otherwise

Examples:
```bash
# Preset by name
pergent --skill code-review

# File path
pergent --skill ./my-skills/custom.md
pergent --skill /absolute/path/review.md

# Mix
pergent --skill code-review --skill ./my-skills/extra.md
```

If a preset name is not found, error: `"unknown preset skill: <name>"`.

---

## Embedded Presets

Preset skill `.md` files live in the `skills/` directory and are embedded into the binary at compile time using `//go:embed`. This means:
- The `.md` files remain readable in the repo as reference
- The Docker image needs no `skills/` directory at runtime
- Zero filesystem I/O for preset skills

**v1 preset:** `code-review` only (from existing `skills/code-review.md`).

Adding a new preset later is: (1) create `skills/<name>.md`, (2) register it in the preset lookup map.

---

## Changes Required

### `internal/skill/loader.go`

- Add `//go:embed` directive to embed `skills/*.md` from the repo root
- Add a preset name-to-filename map: `var presets = map[string]string{"code-review": "skills/code-review.md"}`
- Add `Resolve(value string) (Skill, error)` function that:
  - If value contains `/` or ends with `.md`: call existing `Load(value)` (file path)
  - Otherwise: look up in presets map, read from embedded FS, parse frontmatter + body
  - If preset name not found: return error `"unknown preset skill: <name>"`

### `internal/skill/loader_test.go`

- Test preset loading by name (`code-review`)
- Test unknown preset name error
- Test auto-detection: file path vs preset name
- Test that preset content matches expected skill name and non-empty body

### `cmd/main.go`

- Change `skill.Load(path)` call to `skill.Resolve(value)` in the skill loading loop

### No changes to:

- `internal/config/`
- `internal/runner/`
- `internal/output/`
- `internal/platform/`
- `skills/code-review.md` (content unchanged, now also embedded)

---

## Embed Constraint

Go's `//go:embed` requires the embedded files to be in or under the package directory. Since `skills/` is at the repo root and the skill package is at `internal/skill/`, we need one of:

**Option A:** Move preset `.md` files to `internal/skill/presets/` and embed from there. Keep `skills/code-review.md` at root as documentation only.

**Option B:** Put the embed directive in `cmd/main.go` (which is closer to root) and pass the embedded FS to the skill package.

**Chosen: Option A.** Cleaner separation. `internal/skill/presets/*.md` are the actual embedded files. The root `skills/code-review.md` can be removed or kept as a symlink/copy for documentation.

Updated structure:
```
internal/skill/
├── loader.go           # Modified: add Resolve() + embed logic
├── loader_test.go      # Modified: add preset tests
└── presets/
    └── code-review.md  # Embedded preset skill
```

---

## Out of Scope

- Default skill when no `--skill` is provided (still required)
- Listing available presets (`--list-skills`)
- Remote/URL-based skills
- User-defined preset directories
