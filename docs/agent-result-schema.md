# Agent result schema (version 1)

`pergent agent` reviews a checkout and delivers one JSON document: the **result**. This page describes that document, the error codes, the exit codes and how HTTP delivery treats the receiver's replies. For the command itself, see [Agent mode](../README.md#agent-mode) in the README.

A failed review is still a result. A review that could not run at all is an **error result** with the same envelope.

## Completed result

`status: "completed"` means a review ran, including one where every skill failed. Look at each skill's `verdict`, never only at `status`.

```json
{
  "schema_version": 1,
  "status": "completed",
  "pergent_version": "0.1.0",
  "base": "4f1c2a9e8b7d6c5b4a3f2e1d0c9b8a7f6e5d4c3b",
  "head": "9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b",
  "started_at": "2026-10-29T10:00:00Z",
  "finished_at": "2026-10-29T10:01:24Z",
  "budget_seconds": 600,
  "skills": [
    {
      "name": "code-review",
      "verdict": "has_blocking_issues",
      "findings": [
        {
          "id": "a1b2c3",
          "fingerprint": "a1b2c3d4e5f6",
          "file": "api/handler.go",
          "line": 42,
          "severity": "blocking",
          "title": "Request body is used before validation",
          "explanation": "handleCreate passes req.Name to db.Exec without calling validate(req), so an empty or oversized name reaches the query.",
          "suggested_fix": "Call validate(req) right after decoding the body and return a 400 on error."
        },
        {
          "id": "d4e5f6",
          "fingerprint": "d4e5f6a7b8c9",
          "file": "api/handler.go",
          "severity": "suggestion",
          "title": "Error from json.Encoder is ignored",
          "explanation": "The response encoding error is dropped, so a failed write goes unnoticed."
        }
      ],
      "summaries": [
        {
          "batch": 1,
          "text": "Checked the handler and its tests in api/: the new query is parameterised, but the request body is not validated."
        }
      ],
      "checks": [
        {
          "batch": 1,
          "dropped": 0,
          "findings": 2,
          "checks": [
            {
              "section": "Input handling",
              "name": "Request body is validated",
              "status": "fail",
              "detail": "api/handler.go:42 uses req.Name without validate(req)"
            },
            {
              "section": "Input handling",
              "name": "Query is parameterised",
              "status": "pass",
              "detail": "db.Exec(\"INSERT ... VALUES ($1)\", req.Name)"
            }
          ]
        }
      ],
      "files": {
        "reviewed": ["api/handler.go", "api/handler_test.go"],
        "skipped": [
          { "path": "go.sum", "rule": "lockfile" }
        ],
        "not_reviewed": []
      },
      "failures": [],
      "notes": [],
      "batches": 1,
      "batches_finished": 1,
      "duration_seconds": 84.2,
      "tokens": { "input": 52310, "output": 1840 }
    },
    {
      "name": "security-review",
      "verdict": "failed",
      "findings": [],
      "files": {
        "reviewed": [],
        "skipped": [],
        "not_reviewed": [
          {
            "path": "api/router.go",
            "cause": "Timed out after 10m0s (review budget 10m0s) during step 9; the last tool call was read on api/router.go."
          }
        ]
      },
      "failures": [
        {
          "files": ["api/router.go"],
          "message": "Timed out after 10m0s (review budget 10m0s) during step 9; the last tool call was read on api/router.go.",
          "cause": {
            "kind": "timeout",
            "message": "Timed out after 10m0s (review budget 10m0s) during step 9; the last tool call was read on api/router.go."
          }
        }
      ],
      "notes": [],
      "batches": 1,
      "batches_finished": 0,
      "duration_seconds": 600.0,
      "tokens": { "input": 98120, "output": 2210 }
    }
  ],
  "comment": {
    "markdown": "### code-review\n\n**1 blocking issue**\n\n...\n\n<!-- pergent:findings ... -->",
    "findings_block": "<!-- pergent:findings ... -->"
  }
}
```

The `markdown` and `findings_block` values are shortened here. `comment.markdown` is the whole comment, ready to post.

## Error result

`status: "error"` means no review was produced. It has `error` and a ready-to-post comment, and no `skills`, timing or budget.

```json
{
  "schema_version": 1,
  "status": "error",
  "pergent_version": "0.1.0",
  "base": "4f1c2a9e8b7d6c5b4a3f2e1d0c9b8a7f6e5d4c3b",
  "head": "9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b",
  "error": {
    "code": "commit_missing",
    "message": "commit 4f1c2a9e8b7d6c5b4a3f2e1d0c9b8a7f6e5d4c3b is not in the checkout"
  },
  "comment": {
    "markdown": "<!-- pergent -->\n⚠️ **pergent couldn't review this MR:** commit 4f1c2a9e8b7d6c5b4a3f2e1d0c9b8a7f6e5d4c3b is not in the checkout"
  }
}
```

The comment is exactly a `<!-- pergent -->` line (the marker a completed comment starts with too, so the caller can update the one pergent comment), then `⚠️ **pergent couldn't review this MR:** ` followed by `error.message` with a backslash before every ASCII punctuation character. The message can name files from the checkout, and the escaping keeps it from rendering as a link, HTML or other markup; it renders as the message itself. pergent adds no trailing period. `error.message` is one line of plain text, unescaped, with credentials redacted. `base` and `head` are the raw flag values, omitted only when the flag was not given (or came after the flag that stopped parsing). On a `usage` error they may hold the malformed value that caused it.

## Fields

Top level:

| Field | Type | Meaning |
|---|---|---|
| `schema_version` | number | Always `1` in this version. |
| `status` | string | `completed` or `error`. |
| `pergent_version` | string | The pergent version that produced the result. |
| `base`, `head` | string | The raw `--base` and `--head` values: the commits that were reviewed. Omitted when the flag was not given; on a `usage` error they may be malformed. |
| `started_at`, `finished_at` | string | RFC 3339, UTC. Completed results only. |
| `budget_seconds` | number | The review's `--timeout` in seconds. Completed results only. |
| `skills` | array | One entry per skill, in the order loaded. Completed results only. A completed result with no skills has no `skills` key. |
| `error` | object | `code` (one of the [error codes](#error-codes)) and `message`. Error results only. |
| `comment` | object | The rendered comment. See below. |

`comment`:

| Field | Meaning |
|---|---|
| `markdown` | The full comment, rendered with `--max-comment-size`. Each finding's ID is shown in backticks after its title. For an error result it is the error comment. |
| `findings_block` | The hidden `<!-- pergent:findings ... -->` block on its own. Completed results only. |

Each entry of `skills`:

| Field | Type | Meaning |
|---|---|---|
| `name` | string | The skill's name. |
| `verdict` | string | `no_blocking_issues`, `has_blocking_issues` or `failed` (any batch failed). |
| `findings` | array | The skill's findings, from the batches that finished. |
| `summaries` | array | `{batch, text}`: the model's summary of each batch. Omitted when empty. |
| `checks` | array | One entry per batch that reported a checklist: `batch`, `dropped` (valid checks beyond the per-answer limit), `findings` (the number of findings in that batch) and `checks`, a list of `{section, name, status, detail}` where `status` is `pass`, `fail` or `n/a`. Omitted when no batch reported checks. |
| `files.reviewed` | array | Paths the skill reviewed. The verdict covers these files and nothing else. |
| `files.skipped` | array | `{path, rule}`: files left out, the machine-readable record of what was not reviewed. `rule` is one of the values in [What a verdict covers](#what-a-verdict-covers). |
| `files.not_reviewed` | array | `{path, cause}`: in-scope files whose batch failed. `cause` is plain one-line text. |
| `failures` | array | One entry per batch that did not finish: `files`, `message` (plain one-line text: the failure's own message, if any, followed by the cause's message) and, when known, `cause` with `kind` and `message`. `kind` is `context_overflow`, `timeout`, `opencode_error`, `step_limit`, `exit_error` or `no_answer`. |
| `notes` | array | Plain-text notes, such as "Checks omitted to fit the 10 MB result limit." |
| `batches`, `batches_finished` | number | Batches the skill needed, and how many finished. |
| `duration_seconds` | number | Wall-clock time for the skill, to a tenth of a second. |
| `tokens` | object | `input` and `output` token counts, summed over every run of the skill: batch, repair, verification and cross-batch runs. A provider that reports no usage gives zeros. |

Each entry of `findings`:

| Field | Type | Meaning |
|---|---|---|
| `id` | string | Short ID, unique within this result. See below. |
| `fingerprint` | string | Stable identity of the finding across runs, 12 hex characters. Derived from the skill, the file and the normalised title, so it survives line moves. |
| `file` | string | Path from the repo root. |
| `line` | number | Line in the head version. Omitted when the finding has none. |
| `severity` | string | `blocking` or `suggestion`. |
| `title` | string | One-line title. |
| `explanation` | string | Why it is a problem. |
| `suggested_fix` | string | A proposed fix. Omitted when empty. |

`failures[].message` and `files.not_reviewed[].cause` are one line of plain text with no markdown and no opencode stderr, made for machines. The markdown version is in `comment.markdown`.

### What a verdict covers

A skill's `verdict` covers only its `files.reviewed`. A file in `files.skipped` was never shown to the model, and its `rule` says why:

| `rule` | Applies when |
|---|---|
| `deleted` | The change deletes the file. |
| `binary` | git shows the file as binary (its own NUL-byte check, with `.gitattributes` read from the base commit). |
| `no content changes` | The diff has no content hunks, for example a rename or mode change only. |
| `lockfile` | The file name is a known lockfile (`go.sum`, `package-lock.json`, `yarn.lock` and the like). A skill's `paths` can opt it back in. |
| `vendored` | A path segment is `vendor`, `node_modules` or `third_party`. A skill's `paths` can opt it back in. |
| `generated` | The base commit's `.gitattributes` marks it `linguist-generated`, or the change shows a generated-file header ("Code generated ... DO NOT EDIT" and the like) that the base version of the file already had. A skill's `paths` can opt it back in. |
| `out of scope` | The skill has `paths` and none matches. |
| `excluded by skill` | One of the skill's `exclude` patterns matches. |

Only `.gitattributes` and the generated header come from the base commit. `lockfile` and `vendored` are fixed path rules, and `binary`, `deleted` and `no content changes` follow the change itself, so a change decides on its own whether those apply to it. A change whose files are all skipped gets `files.reviewed: []` and `no_blocking_issues`.

A caller deciding on approval must therefore not treat `no_blocking_issues` as approval when `files.reviewed` is empty, or when `files.skipped` holds `deleted`, `binary`, `vendored`, `lockfile` or `generated` entries, unless its own policy accepts those changes unreviewed.

### Finding IDs

An ID is the first 6 characters of the fingerprint. If two different fingerprints in the same result share those 6 characters, both are grown to 8 characters, then 9, 10 and so on until every ID is unique. Findings with the same fingerprint (the same finding in two places of one result) share one ID.

IDs are only unique within one result and can change between pushes. Store a dismissal by **fingerprint**, and resolve a short ID against the latest result to get it.

### Size limit

A result is at most 10 MB. If the encoded JSON is larger, pergent drops every skill's per-batch `checks` first, and if that is not enough every skill's `summaries`, adding a note to each skill that lost something. Verdicts, findings, failures, files and the rendered comment are always kept. If the result is still too large, pergent delivers an `internal` error result instead.

Credentials are redacted from every string of the result before it leaves pergent. That includes the value of `PERGENT_RESULT_TOKEN`, if it is at least 8 characters long.

## Error codes

| Code | Exit | Meaning |
|---|---|---|
| `usage` | 2 | A bad flag, a missing input, or two skills with the same name. Also a trusted input, a `--result` file, `$HOME` or `$XDG_CONFIG_HOME` that resolves inside `--repo-path`. |
| `checkout_missing` | 3 | `--repo-path` is not a git work tree, or is not the top of one. |
| `commit_missing` | 3 | `--base` or `--head` is not in the checkout. |
| `head_mismatch` | 3 | The checkout's `HEAD` is not `--head`. |
| `diff_failed` | 3 | `git diff` between the two commits failed. |
| `skills_invalid` | 3 | A skill file or `--skills-dir` could not be loaded, including a folder in `--skills-dir` without a `SKILL.md`. |
| `untrusted_instructions` | 3 | The checkout has instruction files below its root, a symlink to its root, or a symlink to outside it, and `--scrub-instructions` was not passed. |
| `internal` | 3 | Anything else: git older than 2.40, a failure while scrubbing, or a result over 10 MB that checks and summaries could not shrink enough. |

The list is fixed in version 1.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | A result was produced and every requested delivery succeeded. This includes a review where every skill failed: read the verdicts. |
| `2` | Usage error. pergent delivers an error result with code `usage` only when it could parse a delivery target (`--result`, or `--result-url` with `PERGENT_RESULT_TOKEN` set). |
| `3` | No review was possible. The error result was delivered. |
| `4` | At least one delivery failed. This is also the code for an error result (other than `usage`) that could not be delivered. |

`--result` and `--result-url` can be combined. Both are attempted, and a failure of either gives exit 4.

A crash, an out-of-memory kill, a deadline kill or an image pull failure delivers no result at all. The caller must treat "no result arrived" as an error too, for example by reading the Job's status.

## Delivery

| Target | Behaviour |
|---|---|
| `--result <path>` | Writes `<path>.tmp`, fsyncs it, then renames it to `<path>`, so a reader never sees a partial file. |
| `--result -` | Writes the JSON to stdout. stdout carries nothing else in agent mode; all logs go to stderr. |
| `--result-url <url>` | `POST` with `Content-Type: application/json` and `Authorization: Bearer $PERGENT_RESULT_TOKEN`. |

Each HTTP request times out after 30 seconds; a timeout is a connection error and is retried. HTTP replies:

| Reply | Meaning | pergent does |
|---|---|---|
| 2xx | Accepted | Done. |
| 409 | The receiver already has a result for this run | Not retried. **Counts as delivered.** |
| 401, 403 | Bad or expired token | Not retried. Delivery failed. |
| 413 | Body too large | Not retried. Delivery failed. |
| Other 4xx | Rejected | Not retried. Delivery failed. |
| 429, 5xx, connection error | Transient | Retried 3 times, after 2s, 4s and 8s. Then delivery failed. |
| Redirect (3xx) | | Never followed. The endpoint must answer the POST itself, so a redirecting endpoint fails. |

After a `SIGTERM` the whole HTTP delivery gets 20 seconds, with a single retry after 1s, so it fits Kubernetes' 30-second grace period. A delivery already under way when the signal arrives is stopped 20 seconds after it.

## Compatibility

Version 1 changes only by adding fields. A reader must ignore fields it does not know. Renaming a field, removing one, or changing the meaning of one requires `schema_version: 2`.

`--previous` reads a result with this schema. A file with another `schema_version`, or one that cannot be read, is ignored with a warning and the review runs as a first review.
