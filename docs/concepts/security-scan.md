# Security scan

`pergent scan` runs one security audit of a whole repository at one commit, and sends the result as a JSON **envelope** with a SARIF 2.1.0 log inside. pergent.sh starts it as a Kubernetes Job in the `pergent-security` image. It is not a review: there is no diff, no comment and no verdict.

What a scan does, in order:

1. Gets the source: it downloads it from pergent.sh, or uses a local clone.
2. Checks the skill: its `pergent-scan.yaml`, its entry file (the agent prompt) and, for a `cloudflare-findings` skill, its `report-schema.json`.
3. Deletes every instruction file (`AGENTS.md`, `CLAUDE.md`, `CONTEXT.md`, the root's too), every symlink that leaves the folder or points to a denied file, and every file the read rules deny (`*.env`, `*.pem`, `*.key`, `*.tfvars`). The model reads the whole tree, so a nested `AGENTS.md` would be an injection path.
4. Writes the prior run (the last complete scan's state and the dismissed findings) for the skill to read.
5. Runs opencode **once**, with no retry: a retry would start a 3-hour audit again.
6. Converts the skill's output files to SARIF, decides the status and delivers the envelope.

```bash
pergent scan \
  --repo-path /work/src \
  --source-url https://pergent.example.com/security-scans/42/source \
  --head 9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b \
  --prior /in/prior.json \
  --result-url https://pergent.example.com/security-scans/42/result
```

stdout carries only the envelope, and only with `--result -`. All logs go to stderr. They are redacted and hold no model text.

## Inputs

| Input | Rule |
|---|---|
| `--repo-path` | Required. With `--source-url`: an existing **empty** folder that pergent fills. Without it: the top of a local git clone (for local use and tests). |
| `--source-url` / `$PERGENT_SCAN_SOURCE_URL` | pergent.sh's `/security-scans/{id}/source`. Needs `--head` and `PERGENT_RESULT_TOKEN`. |
| `--head` | A full lowercase commit SHA. Required with `--source-url`. Without it, the clone's `HEAD` must equal it when it is set, and pergent reads `HEAD` when it is not. Either way it is the envelope's `commit`. |
| `--skill` | The skill folder. Default `/opt/pergent/security-skills/security-audit`. Trusted, must be outside `--repo-path`. |
| `--prior` / `$PERGENT_SCAN_PRIOR` | Optional JSON file, trusted, outside `--repo-path`, at most 800 KiB. A file that can't be read or is malformed is a warning: the scan runs with no prior. |
| `--max-turns` | The main agent's step limit, 5 to 1000. Default: the manifest's `steps.main`. |
| `--max-subagent-turns` | Each sub-agent's step limit, 5 to 300. Default: the manifest's `steps.subagent`. |
| `--timeout` | 10m to 6h, default 3h. The download counts against it. Give the Job a deadline of at least `--timeout` + 2 minutes. |
| `--result` / `--result-url` | As in `pergent agent`: a file (or `-` for stdout), or a URL that gets a POST with `PERGENT_RESULT_TOKEN` as the bearer token. At least one is required. A `--result` file inside `--repo-path` is refused. |
| `OPENCODE_PROVIDER`, `OPENCODE_MODEL` | Required. `OPENCODE_API_KEY`, `OPENCODE_BASE_URL` and `OPENCODE_NPM` work as for a review. |
| `PERGENT_TOKEN_BUDGET` | Optional positive whole number. See [Usage and the token budget](#usage-and-the-token-budget). Empty means no budget. `0`, a negative number or text is a usage error. |
| `PERGENT_SCAN_KEEP_PROXY` | `true` keeps `HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY`, `NO_PROXY` (both cases) and `NODE_USE_ENV_PROXY` in opencode's environment. Any other value drops them. |

`$HOME` and `$XDG_CONFIG_HOME` inside `--repo-path` are a usage error too, since opencode reads its global config from there.

Exit codes: 0 the envelope was delivered (any `status`), 2 usage error (a `failed` envelope is still sent when a target is known), 3 no scan was possible (a `failed` envelope was delivered), 4 delivery failed.

## What the agent can do

The scan writes its own `opencode.json`. Every agent gets the review's read-only block (see [Agent sandbox](sandbox.md)), and the agents the manifest's `tools` name get more:

| Manifest tool | opencode action | Where |
|---|---|---|
| `bash` | `shell` | Any command. |
| `write` | `edit` | Only inside the output folder, `$TMPDIR/pergent-scan-*/out`. Every other path is denied. |
| `task` | `subagent` | The main agent may start `general` and `explore`, and no other agent. |

The sub-agents `general` and `explore` get the same block as the main agent, their own step limit, and `subagent: deny`, so they can't start more agents. The skill folder is readable. The top-level permission stays the read-only block, so opencode's hidden agents (title, summary, compaction) get no shell.

Each sub-agent's step limit is opencode's own: opencode stops the sub-agent, and pergent can't see that it did. pergent only sees the main agent's step limit (`step limit reached`).

**The network is the real guard.** A model with a shell can read what the process can read, also outside the folder: `/proc/<pid>/environ` holds opencode's environment, with the model key in it. The permissions above keep the model's tools on task; they do not keep a secret from it. Run the Job with a network policy that lets it reach only the model's endpoint and pergent.sh. pergent removes `PERGENT_RESULT_TOKEN`, `GITHUB_TOKEN` and `GITLAB_TOKEN` from opencode's environment, and the proxy variables unless `PERGENT_SCAN_KEEP_PROXY=true`.

## How the source arrives

With `--source-url`, pergent GETs the URL with `Authorization: Bearer $PERGENT_RESULT_TOKEN` and follows no redirect. The answer is a `.tar.gz` of the repository at `--head`, in the GitLab archive or GitHub tarball layout (one top folder). pergent strips the top folder and unpacks the rest into `--repo-path`. A connection error or a 5xx is tried again, up to 3 tries (after 2 s and 4 s); a 4xx is final.

So the scan pod holds no git token and runs no git: there is no `.git` folder, and the skill gets the commit in its task line (`Source commit: <sha>.`). **Git LFS files and submodules are not in the archive**: an LFS file is at most its small pointer file, and a submodule's files are missing.

The archive is treated as hostile. The limits are 200,000 files, 2 GiB unpacked and 500 MiB downloaded, so `--repo-path` needs room for 2 GiB of files plus folder overhead: give the pod more than 2 GiB of disk. Files get mode `0644`, or `0755` when any execute bit is set; folders `0755`; no owner, time or setuid bit is kept. pergent refuses the whole archive on the first entry that breaks a rule, with one of these fixed causes:

| Cause | When |
|---|---|
| `not a gzip tar archive` | The body is not a gzip tar stream, or it is broken. |
| `not one top folder` | No top folder, a file at the top, or a second top folder. |
| `an absolute path` | An entry name starts with `/`. |
| `a path with ..` | An entry name has a `..` part. |
| `a link that leaves the folder` | A symlink whose target is outside the folder. |
| `a hard link` | Any hard link. |
| `a device file or FIFO` | A character or block device, or a FIFO. |
| `an entry type pergent does not take` | Any other entry type. |
| `a path twice` | Two entries with the same path. |
| `more than 200000 files` | Files, folders and links together. |
| `more than 2 GiB unpacked` | File bytes and link targets together. |
| `more than 500 MiB downloaded` | The bytes read from pergent.sh. |
| `a file could not be written` | The disk is full, or another write to the folder failed. |

## The envelope

```json
{
  "kind": "security",
  "status": "complete",
  "reason": "",
  "commit": "<full sha>",
  "skill": { "name": "security-audit", "commit": "<the skill's pinned commit>" },
  "usage": { "source": "stats", "sessions": 1, "subagents": 12, "steps": 840,
             "tokens": { "input": 0, "output": 0, "reasoning": 0, "cache_read": 0, "cache_write": 0 },
             "models": [ { "provider": "anthropic", "model": "m", "steps": 840, "tokens": { "...": "the same 5 keys" } } ] },
  "coverage": { "done": 48, "planned": 48 },
  "state": { "findings.json": "...", "coverage-ledger.json": "..." },
  "sarif": { "$schema": "...", "version": "2.1.0", "runs": [ { "tool": { "driver": { "name": "security-audit", "version": "<the skill's pinned commit>" } }, "results": [] } ] }
}
```

| Field | Rule |
|---|---|
| `commit` | Always set: `--head`, or the clone's `HEAD`. |
| `skill`, `sarif.runs[0].tool.driver` | The manifest's `name` and its pinned `commit`. |
| `status`, `reason` | See [Status](#status). `reason` is `""` for `complete`, else one or more fixed texts joined by `; `. |
| `usage` | See [Usage and the token budget](#usage-and-the-token-budget). |
| `coverage` | From `coverage-ledger.json`: `planned` counts the units that are not `not_applicable` or `out_of_scope`, `done` the units that are `covered` or `candidate`. Both 0 when the ledger is missing or bad. A scan stopped early still reports it. |
| `state` | Each `state` file of the manifest found in the output folder: a regular file (a link is left out), valid UTF-8, at most 512 KiB, and at most 512 KiB together (later files in manifest order are left out first). pergent.sh passes it back in the next prior. |
| `sarif` | Always one valid run, also for `failed`. At most 8 MiB and 5000 results: past either, results are cut from the lowest severity and `status` becomes `partial`. |

The whole envelope is at most 12 MiB. Every string in it is redacted first.

## Status

Decided in this order. The texts are exact.

| Condition | `status` | `reason` |
|---|---|---|
| The download failed or did not answer 200 | `failed` | `the source could not be downloaded` |
| The archive broke an unpack rule or limit | `failed` | `the source archive was refused: <cause>` |
| Another failure before opencode ran | `failed` | `the scan could not start: <cause>` |
| The SARIF pergent built fails the SARIF schema | `failed` | `the SARIF did not pass the schema check` |
| A stop: budget, step limit, time limit, signal | `partial` | `token budget reached`, `step limit reached`, `time limit reached` or `cancelled` |
| opencode failed on its own, `findings.json` valid | `partial` | `the model run failed: <kind>` |
| opencode failed on its own, `findings.json` missing or bad | `failed` | the same |
| The run completed, `findings.json` missing or bad | `failed` | `findings.json is missing` or `findings.json did not pass the skill's schema check` |
| The run completed, `run_status` is not `complete` | `partial` | `the skill did not mark the run complete`, plus `: <incomplete_reason>` when it matches `^[a-z0-9_]{1,64}$` |
| The SARIF was cut to fit (bytes or count) | `partial` | `the SARIF was cut to fit the size limit` |
| Otherwise | `complete` | `""` |

The causes of a scan that could not start: `the inputs are not valid` (a usage error), `the skill is not usable` (the manifest, the entry file or the report schema), `the checkout is not usable`, `the checkout could not be cleaned` (an instruction or denied file could not be deleted), `opencode is not usable` (the version check), `the work folder could not be made` and `the opencode config could not be made`. The log line before the envelope has the detail.

A `partial` with no valid `findings.json` has no results, but `coverage` and `state` still hold what exists. A failed or partial scan is never `complete`.

## SARIF results

The `cloudflare-findings` converter makes one result per `confirmed` or `needs_validation` record of `findings.json`. A `rejected` record is not a result. The first record of a fingerprint wins.

| SARIF | From |
|---|---|
| `fingerprints["security-audit/v1"]` | `fingerprint` |
| `ruleId` | The `attack_class` of the first ledger unit (by `coverage_id`) whose `result_fingerprints` holds the fingerprint, else `security-audit`. It is not a CWE. `driver.rules` has one entry per `ruleId`, with the tags `security` and the `ruleId`. |
| `level`, `properties.severity`, `properties.security-severity` | `confirmed`, by `severity.overall_severity`: critical is `error`, `critical`, `"9.5"`; high is `error`, `high`, `"8.0"`; medium is `warning`, `medium`, `"5.5"`; low is `note`, `low`, `"3.0"`; informational is `note`, `low`, `"1.0"`. `needs_validation` is `warning` with no severity keys. |
| `properties.needs_validation` | `true` for `needs_validation`, else absent. |
| `message.text` | `title` |
| `properties.description`, `exploit_scenario`, `fix`, `confidence` | `description`; `execution.attacker_perspective`, a blank line, `execution.observed_result`; `remediation.strategy` (the `validation_plan` texts for `needs_validation`); `confidence.score`. |
| `locations[0]` | The trace's `sink` entry, else its last entry, else the first `evidence`: `uri` the file with `uriBaseId` `%SRCROOT%`, and `region.startLine`. A file that is absolute, holds `..` or a backslash gets no location, and the result stays. |

Every output file is read as hostile: a link or a FIFO in its place is read as missing, never followed.

## Usage and the token budget

`opencode run` prints the main session only, so pergent also reads `opencode stats` from the run's data folder, which counts the sub-agents too. It reads it every 30 seconds while opencode runs (each read at most 20 seconds, never two at once) and once more after the run (at most 60 seconds, or 5 seconds after a signal).

| `usage.source` | Meaning |
|---|---|
| `stats` | The last read after the run worked. |
| `stats_poll` | That read failed: the numbers are from the last read during the run. |
| `stream` | No read worked: the main session only, sub-agents missing. |

Each token count is never lower than the main session's own count. A failing read is logged once.

`PERGENT_TOKEN_BUDGET` is a limit on the 5-part total (input + output + reasoning + cache read + cache write). After each read pergent takes the higher of the stats total and the main session's total, and stops opencode when it reaches the budget: the status is `partial`, `token budget reached`. The check runs on the 30-second reads, so a scan can go past the budget by what it uses in one 30-second poll at most.

## Signals

SIGTERM or SIGINT stops opencode. The envelope is still sent: `partial`, `cancelled`, with the findings the run wrote so far, the last usage (5 seconds for the last stats read, then the last poll) and a short delivery (one quick retry within 20 seconds), so it fits a Job's 30-second grace period. A signal during the download makes the download fail (`the source could not be downloaded`).
