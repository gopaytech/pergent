# 🧩 Skills

Skills are markdown files that define review behavior. The `--skill` flag accepts either a **preset name** or a **file path**.

## Preset Skills

Built-in skills embedded in the binary:

| Name | Description |
|---|---|
| `code-review` | General code quality, bugs, clarity, performance, security |

```bash
pergent --skill code-review
```

## Custom Skills

Create your own `.md` file with optional YAML frontmatter:

```yaml
---
name: security-review
---

You are a security engineer reviewing a pull request.
Focus on authentication, authorization, injection vulnerabilities,
and sensitive data exposure.
...
```

```bash
pergent --skill ./skills/security-review.md
```

## Scope: `paths`, `exclude` and `cross_batch`

The frontmatter can narrow which changed files a skill reviews and ask for an extra check across batches:

```yaml
---
name: api-review
paths:
  - "api/**"
  - "*.proto"
exclude:
  - "**/*_test.go"
cross_batch: true
---
```

| Field | Meaning |
|---|---|
| `paths` | Glob patterns. When set, the skill reviews only changed files matching one of them. Files that match none are listed in the comment's Files list as skipped, out of scope. Unset means every changed file. |
| `exclude` | Glob patterns for files the skill skips even though they are in scope. |
| `cross_batch` | `true` adds one extra run, after all batches, that looks for problems spanning batches (for example conflicting changes in different directories). It only runs when the change needed more than one batch. Default `false`. |

Patterns use `/`-separated paths from the repo root. `*` matches within one path segment and `**` matches any number of segments. A pattern with no `/` (such as `*.proto`) matches the file name at any depth. A pattern with bad glob syntax, or one that can never match a file (`api/`, `/api/**`, `./api/**`), is an error when the skill loads, because the skill would otherwise report a clean review on every change. For a directory, use `api/**`.

## Default exclusions

Every skill skips these files, and the comment's Files list shows them as skipped, with the reason:

- **Always skipped:** deleted files, binary files, and files with no content changes (for example a pure rename).
- **Skipped unless a skill's `paths` match them:** lockfiles (`package-lock.json`, `go.sum`, `yarn.lock`, `pnpm-lock.yaml`, `Cargo.lock`, `composer.lock`, `Gemfile.lock`, `poetry.lock`, `Pipfile.lock`), vendored code (anything under a `vendor`, `node_modules` or `third_party` directory), and generated files (paths marked `linguist-generated` in the repo's `.gitattributes`, unless a later matching line unsets it with `-linguist-generated`, `linguist-generated=false` or `!linguist-generated`, or files whose first lines say the code is generated and not to be edited).

A file larger than a batch can hold (about a quarter of the model's context window, 40,000 tokens when the window is unknown) is not reviewed and is reported as a failure for that file.

## The `## Blocking` section

A review has two kinds of findings: **blocking** ones, which decide the verdict, and **suggestions**. Tell the skill what blocks by adding a `## Blocking` heading (any level) to its body and listing the problems that must be fixed before merging:

```markdown
## Blocking

- Logic errors that cause wrong behaviour, crashes, or data loss.
- Security vulnerabilities.

Everything else is a suggestion.
```

When a skill has no `## Blocking` section, pergent appends a default one: bugs, security vulnerabilities and broken configuration are blocking, and everything else is a suggestion. Before a blocking finding is reported, pergent runs a separate check that opens the code again and drops the finding if the problem is not real.

## Checklists become checks by area

If a skill contains a checklist (section headings with items under them, as bullets or checkboxes), the model reports every item as a check: pass, fail or n/a, with the evidence it compared. The comment shows the checks by area: each checklist heading is a heading in the comment with a small table of `Check`, `Status` and `Detail`, failed checks first. No special syntax is needed; write the checklist as you would for a human reviewer. The `## Blocking` section is not treated as a checklist. Findings still decide the verdict: a failed check should come with a finding, and the comment notes it when one doesn't.

## Don't put output-format instructions in skills

pergent appends its own output-format instructions to every skill: the model must end its answer with one JSON block holding a short `summary` of what it checked and concluded, its `checks` (the skill's checklist, if it has one), and its findings. The comment shows the summary, and the agent result carries it too (`summaries`). A missing summary is not an error; the review still counts. Don't tell a skill how to format its answer (no "use bullet points", "start with a summary", or JSON schemas of your own). Instructions like that conflict with the required format and make runs fail. Write what to look for and what counts as blocking; pergent handles the rest.
