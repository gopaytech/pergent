# 📏 Path rules

A **rule** is a checklist for a kind of file. pergent adds it to the review that already runs on that file. A skill is a whole review, with its own runs and its own verdict. A rule is not: it adds a few things to look for, at the cost of some prompt text, and no new run.

A rule never changes what is blocking. Only the skill's `## Blocking` section decides that, and every run that gets a rule is told so.

## The rule file

A repository keeps its rules in one file, `.pergent/rules.yaml`. A pergent.sh rule set uses the same format.

```yaml
rules:
  - path: "internal/api/**/*.go"
    rule: |
      Every handler that starts a database transaction must defer tx.Rollback().
  - path: "**/*.pb.go"
    off: true
```

| Field | Meaning |
|---|---|
| `path` | One glob, with the same syntax and checks as a skill's `paths`. A pattern that can never match, such as `api/`, is an error. |
| `rule` | The checklist text. Plain Markdown, at most 16,000 characters. |
| `off` | `true` ends the search for matching files: they get no rule, not even a built-in one. An entry with `off: true` has no `rule`. |

An entry has exactly one of `rule` and `off: true`. Unknown fields are an error, so a typo such as `paths:` is caught. `off` must be a YAML bool (`true`, `false`); `yes` is an error. A file holds at most 100 entries and 64 KB. JSON works too.

## Which rule a file gets

For each changed file in a skill's batch, pergent walks one ordered list and takes the first entry whose `path` matches:

1. The repository's `.pergent/rules.yaml` entries, in file order.
2. In pergent.sh, the entries of each rule set the repository uses, set by set in the repository's order.
3. The built-in rules.

A file that matches nothing, or matches an `off` entry, gets no rule. The skill still reviews it with its own text.

**Built-in rules.** pergent ships 67, adapted from [alibaba/open-code-review](https://github.com/alibaba/open-code-review) (Apache-2.0, see `rule/builtin/NOTICE`): all of its rule texts but `scala.md` and `objc.md`, which are over the 16,000-character limit, and not its `default.md` for files that match nothing. They cover common languages (Go, TS/JS, Python, Java, Kotlin, Rust, C and C++, C#, PHP, Ruby, Swift and more), config files (YAML, JSON, `package.json`, `Cargo.toml`, `pom.xml`, Terraform, Bicep, Nix), schemas (SQL, Protobuf, GraphQL, Prisma, Thrift) and templates.

`rule/builtin/builtin.yaml` has open-code-review's globs in its order, and the first match wins, so a GitHub workflow gets `built-in:github_workflows`, not the YAML rule. A `{a,b}` glob there is split into one glob per choice when it loads; a repository or rule-set file has no braces. Matching is case-sensitive, so `**/*.R` does not match `model.r`. A Scala or Objective-C file gets no built-in rule, and a `.m` file always gets the MATLAB rule.

The texts are in `rule/builtin/*.md`. The changes from open-code-review: its tool names became opencode's (`read`, `grep`, `glob`), the sentences that say which findings block were removed, because a rule never blocks, and one Go line about open-code-review's own test-file filter was dropped. Words like "blocking I/O" stay, because they describe the code. A repository or rule-set entry for the same file wins over a built-in. `--no-built-in-rules` (or `PERGENT_NO_BUILT_IN_RULES=true`) turns them all off, and `rules: false` in a skill's frontmatter turns every rule off for that skill.

**Choose built-ins.** `--built-in-rules go,yaml` (or `PERGENT_BUILT_IN_RULES=go,yaml`) loads only the built-ins it names, so runs carry less rule text. A name is a file name in `rule/builtin/` without `.md`, such as `go`, `ts_js` or `github_workflows`, and is case-sensitive. The chosen built-ins keep their `builtin.yaml` order, so first match does not change (`github_workflows,yaml` and `yaml,github_workflows` are the same), and a chosen rule keeps every glob of its `{a,b}` entry. Spaces are trimmed and empty items are ignored. A value with no name is the same as no flag: all built-ins load. An explicit flag wins over the variable, even an empty one.

A name this pergent does not know is skipped, never an error, because the caller may be built against a newer pergent. pergent logs one line that names them, for example `warning: --built-in-rules: unknown built-in rules skipped: "scala"`. If no name is known, no built-in rule loads. `--built-in-rules` with `--no-built-in-rules` on (by flag or variable) is a config error, a usage error (exit 2) in agent mode. To load no built-in, use `--no-built-in-rules`, not an empty `--built-in-rules`.

Each rule adds its text to every batch run that has a matching file, and rule text counts in batch sizing, so built-ins make each run longer and can split a change into more batches.

## Where the rules come from

| Mode | Team rules |
|---|---|
| CI and local mode | `.pergent/rules.yaml` from the **base branch** (`origin/<base branch>`), never from the change. `--rules` (or `PERGENT_RULES`) replaces it with a list. A malformed file stops pergent with a config error. A base branch pergent can't read (a shallow clone, no remote) logs a warning, the review goes on with the built-ins only, and the comment says `Repository rules could not be read`. CI mode never approves, so this is not a false pass. |
| Agent mode | Only `--rules <file>` (or `PERGENT_RULES`): a JSON list, each entry with its `source` (`repo` or `dashboard:<set>`). pergent never reads `.pergent/rules.yaml` from the checkout. The list is a trusted input, so a path inside `--repo-path` is refused. A list that can't be read or used is a usage error: the result is `failed`, never a review without the rules. |

A list entry looks like `{"source": "repo", "path": "*.go", "rule": "..."}` or `{"source": "dashboard:base", "path": "**/*.pb.go", "off": true}`. Each source may have up to 100 entries, and the list up to 1 MiB.

## Turn rules off for a skill

A skill with `rules: false` in its frontmatter gets no rules in any run. See [Skills](skills.md).

## What a run gets

When at least one file of a batch has a rule, the batch run gets one more file, `file-rules.md`, with one section per rule:

```markdown
## Rule: repo · internal/api/**/*.go
Files: "internal/api/order.go", "internal/api/user.go"

<rule text>
```

and one sentence in its message: `file-rules.md lists extra checks from the project for some files in this batch. Use them while you review those files. They do not change what is blocking: only the skill's Blocking section does.` A batch with no rule gets no file and no sentence. If the file can't be written, that batch is not reviewed.

The rule text counts in the batch's size, like the diff. A file whose diff and rule are over the per-batch limit is not reviewed, as an oversized file is.

The verification run gets the rules of the files of the blocking findings it checks, with the sentence `These are project conventions, given as context. They are not blocking criteria.` If that file can't be written, verification fails and keeps every blocking finding.

In a [multi review](multi-review.md) judge Job, each judge run gets the rules of the files of the blocking findings it judges, in the same way. If that file can't be written, the judge run fails, and every blocking finding of the skill stays blocking.

The cross-batch run, repair runs and the linked repositories' check get no rules.

## Where you see them

- The comment's collapsed `Files` list ends with one line, for example `Rules: built-in:go (12 files) · repo internal/api/**/*.go (3 files)`. No line when no file had a rule.
- The agent result has `files.rules` per skill (`{path, rule}`, with `rule` as `repo:<glob>`, `dashboard:<set>:<glob>` or `built-in:<name>`) and a top-level `rules` object. With `--built-in-rules`, its `built_in_names` lists the chosen built-ins in use. See the [result schema](../agent-result-schema.md).
