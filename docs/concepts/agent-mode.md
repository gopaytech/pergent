# 🤖 Agent mode

`pergent agent` is for a service that runs reviews on code it does not trust, such as a merge request from a fork. It reviews a checkout that someone else prepared, between two commits, and **produces a result** (versioned JSON). It posts nothing.

What it is:

- **No platform token.** It never calls GitHub or GitLab. The caller reads the result, posts the comment, sets the status and approves.
- **Trusted inputs only.** Skills, project instructions and the previous result come from paths you pass, never from the checkout: the checkout is the change under review, and a change could rewrite its own review rules. The parts of the default skip rules a change could rewrite come from the base commit: `.gitattributes` and generated-file headers, so a change can't mark its own files as generated or binary to hide them. `lockfile` and `vendored` are fixed path rules, and `binary` and `deleted` follow the change itself. A skill's verdict covers only the files it reviewed: the caller's approval policy must decide what skipped files mean (see [What a verdict covers](../agent-result-schema.md#what-a-verdict-covers)).
- **A result for every outcome.** A review where every skill failed is still a result (exit 0). A review that could not run is an error result with a code and a ready-to-post comment.

What it isn't: it does not clone or fetch (the caller prepares the checkout), approve, post comments or set statuses. CI mode (`pergent --skill ...`) is unchanged.

```bash
pergent agent \
  --repo-path /work/repo \
  --base 4f1c2a9e8b7d6c5b4a3f2e1d0c9b8a7f6e5d4c3b \
  --head 9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b \
  --skills-dir /in/skills \
  --instructions /in/instructions.md \
  --scrub-instructions \
  --result result.json
```

stdout carries only the result, and only with `--result -`; all logs go to stderr, with each progress line prefixed by the skill and batch (`[code-review 2/3]`).

## Agent flags

| Flag | Description | Default |
|---|---|---|
| `--repo-path` | The checkout of the head commit. Must be the top of the git work tree | required |
| `--base` | The change's merge base: a full 40- or 64-character lowercase commit SHA | required |
| `--head` | The change's head: a full lowercase commit SHA. The checkout's `HEAD` must equal it | required |
| `--skills-dir` | Directory of skills: flat `*.md` files and `<name>/SKILL.md` folders. Symlinked files and folders count (as in a ConfigMap volume); a folder without a `SKILL.md` is `skills_invalid`; entries starting with `.` are skipped | required unless `--skill` is given |
| `--skill` | Preset name or skill file path (repeatable); can be combined with `--skills-dir` | — |
| `--instructions` | One file passed to the agent as project instructions. Without it the agent gets none; `AGENTS.md` / `CLAUDE.md` are never discovered | unset |
| `--previous` | A previous agent result: its findings are attached per skill, as `--previous-review` does in CI mode. A missing, unreadable or other-version file logs a warning and the review runs as a first review | unset |
| `--carried` | A JSON file `{"<skill>": ["path", ...]}` of files a previous review covers. Those files are left out of the skill's batches (they stay in the changed-file list the model sees) and are listed in `files.carried`; a skill with only carried files runs nothing and gets `no_blocking_issues`, unless it has entries on the reference list of `--check-deleted`. Findings on them are not attached as previous findings. A missing, unreadable or malformed file logs a warning and carries nothing | unset |
| `--dismissals` | A JSON file `{"<skill>": [{"file": "...", "title": "...", "severity": "blocking", "reason": "..."}, ...]}` of findings people rejected in earlier reviews, with their reasons. A skill's entries are attached to each of its batch runs and its cross-batch run as `rejected-findings.json`, with a note to treat the file as data; its verification run gets only the entries with severity `blocking`. Entries for a skill that is not run are ignored. A missing, unreadable or malformed file logs a warning and attaches nothing; a run the file cannot be written for goes on without it and the skill gets the note `Past dismissals could not be attached to every run.` A finding the model leaves out because of an entry is named in a note on the skill. The log line `dismissals: <skill>=<n> ...` shows what was read | `$PERGENT_DISMISSALS`, else unset |
| `--change-description` | A file holding the change's title and description. It is attached to each batch run and the cross-batch run as `change-description.md`, with a note to treat it as data; verification and repair runs never get it. HTML comments are removed and only the first 16 KiB is attached. A missing or unreadable file logs a warning and the review goes on without it | `$PERGENT_CHANGE_DESCRIPTION` |
| `--linked-repos` | A JSON file `[{"name": "billing", "path": "/work/linked-1", "watched": ["api/**"], "can_block": true, "error": ""}, ...]` of repositories the caller cloned for an impact check, at most 3. `path` is an absolute, clean directory with no wildcard character in it, outside `--repo-path` and not the temp directory, `$HOME`, `$XDG_CONFIG_HOME`, a system directory, or anything that holds or sits in another trusted input. An entry with an `error` (its clone failed, or the caller refused it) needs no path and is never read. Before any run starts, pergent scrubs every other clone: it deletes instruction files (`AGENTS.md`, `CLAUDE.md`, `CONTEXT.md`, any depth, any case), symlinks that lead out of the clone or to a file the read rules deny, and every file those rules deny, in any letter case (`*.env`, `*.env.*`, `*.pem`, `*.key`, `*.tfvars`, at any depth; the clone's own `.git` is left alone). grep and glob don't honour read rules, so a denied file has to be gone. A clone that can't be scrubbed gets an `error` and is not read. **A file that can't be used does not fail the review:** an unreadable or malformed file, more than 3 entries, a duplicate or missing name, a bad `path` or a refused one only make the `impact` check `failed` (see "Impact check across linked repositories"); the skills run as usual. The one usage error is a clone that overlaps `--repo-path`. With no file, nothing changes | `$PERGENT_LINKED_REPOS`, else unset |
| `--scrub-instructions` | Declares `--repo-path` a disposable checkout and deletes the files below before reviewing | `false` |
| `--max-comment-size` | Byte limit for the rendered comment (`0`: no limit) | `0` |
| `--result` | Write the result to this file, or `-` for stdout. A file is written to `<path>.tmp`, fsynced, then renamed | — |
| `--result-url` | `POST` the result here, with `PERGENT_RESULT_TOKEN` as the bearer token | — |
| `--timeout`, `--concurrency`, `--max-turns`, `--context-limit` | As in CI mode | as in CI mode |
| `--check-deleted` | As in CI mode: review the lines a change deletes, and check what still uses deleted files and the old paths of renamed files (see [Deleted files](deleted-files.md)). Off by default; `PERGENT_CHECK_DELETED=true` turns it on. With it on, a deleted file whose batch finished is in `files.reviewed` and `files.deleted`, a pure rename a finished run checked is in `files.reviewed` and `files.renamed`, and a deletion that cannot be batched stays in `files.skipped` with the rule `deleted`. With it off, every deleted file is skipped as `deleted` and the result has neither field | `false` |

At least one of `--result` and `--result-url` is required; both can be used together. `--local`, `--platform`, `--previous-review` and `--base-branch` are CI-mode flags and are a usage error here.

Environment: `OPENCODE_PROVIDER` and `OPENCODE_MODEL` are **required** (a usage error otherwise), so pergent can generate the sandboxed opencode config. `OPENCODE_API_KEY` and `OPENCODE_BASE_URL` work as in CI mode. `PERGENT_RESULT_TOKEN` is required with `--result-url`; it is removed from opencode's environment and redacted from the logs and the result (a value shorter than 8 characters is not redacted). `GITHUB_TOKEN` and `GITLAB_TOKEN` are not used in agent mode and are removed from opencode's environment too. opencode's own stderr is passed to pergent's with credentials redacted and the run's prefix on each line.

## Impact check across linked repositories

With `--linked-repos`, `pergent agent` adds one run after the skills, reported as a skill named `impact`. It looks in the linked clones for callers that this change breaks. It is not a skill: its prompt is fixed in pergent and carries no skill text, no instructions and no change description. Only this run (and its verification) can read the clones.

- **It always answers.** If any linked entry is passed, the result has an `impact` skill: the host decided the check applies, and pergent does not overrule it by leaving it out.
- **What it sees.** The diff of the changed files, read access to the clones, and its own earlier findings from `--previous` (the entry named `impact`) and its rejected findings from `--dismissals` (also under `impact`). Every changed file counts, generated, vendored, lockfile and rename-only ones too, except files the read rules deny. A link's `watched` paths narrow the diff when at least one file matches (an old path of a rename counts); the clones read are then the links that matched. If none matches, as on a later push that re-runs an open check, the whole change is checked against every usable link. A `watched` pattern that can never match (`scope.CheckPattern`) fails its entry.
- **Severity is enforced in code.** A `blocking` finding must name a caller in `linked` (`repo`, `file`, `line`). It becomes a `suggestion` when it has none, the repository is not one of the matched links, the file is not a regular file inside that clone, the line is past the file's end, or the link has `can_block` false. A caller that fails these checks is dropped from the finding. A finding's own `file` and `line` must be a changed file of this repository; any other location keeps the finding with line 0 and no suggestion.
- **Verification is the other way round.** A skill's blocking finding is kept when verification fails. An impact finding is not: one the verifier does not confirm (only the last JSON block counts, and a `false` for an id wins), and every blocking finding when verification fails or its answer is unusable, becomes a suggestion. A cancel, or a budget that runs out or has under 90 seconds left when verification is due (the impact run comes last, so a slow review lands here), is different: it makes `impact` `failed`, and its blocking findings are shown as suggestions. A finding never carries a `suggestion` or `suggested_fix`: pergent clears both.
- **It fails closed.** `impact` is `failed` when a link has an `error`, the file could not be used, the run fails, its answer is not in the format after one repair run, the run completed without a successful `read`, `grep`, `glob` or `list` on a path in a clone (so a wrong permission cannot read as clean), nothing is left to show it (an empty change, or only files the read rules deny), or the diff is more than one run can hold (the same budget a batch has). The failure names the repository. It never holds text from a clone or the model (no tool target, no part of the answer, no verifier reason, no opencode error text): the cause is a fixed sentence per kind and the details go to the log, redacted. `files.not_reviewed` holds no repository. The skills' results stand. `files.reviewed` stays empty, so a host does not count `impact` as coverage.
- **A clean run says so.** When it ran and found nothing, it adds the note `Impact: checked <names>, nothing found`.
- **The name is reserved.** While linked repositories are given, a skill named `impact` is a usage error.

## Rules for the inputs and the checkout

- **git 2.40 or newer.** The diff reads `.gitattributes` from the base commit through `GIT_ATTR_SOURCE`, which older git ignores. Below 2.40 pergent refuses with the `internal` error code. The Docker image (`debian:trixie-slim`) has a recent enough git.
- **Commits and checkout.** `--base` and `--head` must be full commit SHAs, not branch names or short SHAs. Both commits must exist in the checkout, and its `HEAD` must be `--head`. The diff is `git diff <base> <head>`, so no history between them is needed.
- **Trusted inputs outside the checkout.** `--instructions`, `--skills-dir`, a path given to `--skill`, `--previous`, `--carried`, `--dismissals`, `--change-description` and `--linked-repos` must not resolve inside `--repo-path` (a usage error). Nor may a `--result` file (the change could plant a symlink at its `.tmp` file; the error result is not written there), `$HOME` or `$XDG_CONFIG_HOME`, from which opencode loads its global config and plugins whatever the project-config switch says. Duplicate skill names are a usage error too: the caller decides which skill wins.
- **Instruction files and symlinks.** opencode loads a nested `AGENTS.md`, `CLAUDE.md` or `CONTEXT.md` whenever the agent reads a file next to it, and no setting turns that off, so a change could plant instructions there. A symlink pointing outside the checkout would let the read-only agent read files beyond it, and so would a symlink inside the checkout to a file the read rules deny (`safe.txt -> prod.env`): the rules match the name being read, not where the link leads. Pass `--scrub-instructions` and pergent deletes every file with those names (any case, the root included, `.git` skipped), every symlink whose target resolves outside the checkout (a dangling one counts as outside unless its target can be shown to stay inside) and every symlink whose real target the read rules deny (agent mode's rules, so `*.pem`, `*.key` and `*.tfvars` too), and logs each path. Without the flag, pergent refuses with `untrusted_instructions` when such a file exists below the root, when a symlink resolves to the checkout root, when a symlink resolves outside the checkout, or when a symlink's real target is a file the read rules deny. A directory pergent cannot read is an `internal` error: it could hold a link the scan did not see. A service that reviews untrusted changes should always pass the flag. Changes to these files still show up in the diff and are reviewed as content.
- **Secrets in the checkout.** In agent mode the agent is also denied reading `*.pem`, `*.key` and `*.tfvars`, on top of `*.env` / `*.env.*`. The checkout must not hold credentials in `.git/config`, which the agent can read: see the init container below.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | A result was produced and delivered, even if every skill failed. |
| `2` | Usage error. An error result is delivered only when a delivery target could be parsed. |
| `3` | No review was possible (for example a missing commit or an untrusted checkout). The error result was delivered. |
| `4` | A delivery failed. |

HTTP delivery never follows redirects. A `409` counts as delivered (the receiver already has the result); `401`, `403`, `413` and other `4xx` replies fail without a retry; `429`, `5xx` and connection errors are retried after 2s, 4s and 8s. Each request times out after 30 seconds, which counts as a connection error. The full table is in the [result schema](../agent-result-schema.md#delivery).

After `SIGTERM` or `SIGINT`, pergent stops its opencode runs and fails the unfinished skills with a cause (cancelled, or stopped at the Job deadline if `--timeout` had already run out). A run already in flight gets the cause `cancelled` with the same reason, never `exit_error`. pergent still delivers a `completed` result. Delivery then gets 20 seconds with one retry after 1s, so it fits Kubernetes' 30-second grace period.

## Running as a Kubernetes Job

A Job prepares the checkout in an init container and runs pergent in the main container. The two get different secrets: the init container holds only a read-only git token, and pergent holds the LLM key and the result token, never a platform token.

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: review-7f3a9c
spec:
  backoffLimit: 0
  # --timeout (10m) + 2 minutes to finish and deliver + allowance for the clone
  activeDeadlineSeconds: 900
  template:
    spec:
      restartPolicy: Never
      terminationGracePeriodSeconds: 30
      volumes:
        - name: work
          emptyDir: {}
        - name: inputs
          configMap:
            name: review-7f3a9c-inputs
            # ConfigMap keys can't contain "/", so items are required to put
            # each skill under skills/. List every key.
            items:
              - key: instructions.md
                path: instructions.md
              - key: code-review.md
                path: skills/code-review.md
      initContainers:
        - name: checkout
          image: ghcr.io/gopaytech/pergent:latest
          command:
            - sh
            - -ec
            - |
              # The token goes through git's environment config, so it is never
              # in the remote URL or written to .git/config. Use the username your git
              # host expects (x-access-token is GitHub's).
              export GIT_CONFIG_COUNT=1
              export GIT_CONFIG_KEY_0=http.extraHeader
              export GIT_CONFIG_VALUE_0="Authorization: Basic $(printf 'x-access-token:%s' "$GIT_TOKEN" | base64 -w0)"
              git init -q /work/repo
              cd /work/repo
              git remote add origin "$REPO_URL"
              git fetch -q --depth=1 origin "$BASE_SHA" "$HEAD_SHA"
              git checkout -q --detach "$HEAD_SHA"
          env:
            - name: REPO_URL
              value: https://git.example.com/acme/widgets.git
            - name: BASE_SHA
              value: 4f1c2a9e8b7d6c5b4a3f2e1d0c9b8a7f6e5d4c3b
            - name: HEAD_SHA
              value: 9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b
            - name: GIT_TOKEN
              valueFrom:
                secretKeyRef:
                  name: review-7f3a9c-git
                  key: token
          volumeMounts:
            - name: work
              mountPath: /work
      containers:
        - name: pergent
          image: ghcr.io/gopaytech/pergent:latest
          command:
            - pergent
            - agent
            - --repo-path=/work/repo
            - --base=4f1c2a9e8b7d6c5b4a3f2e1d0c9b8a7f6e5d4c3b
            - --head=9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b
            - --skills-dir=/in/skills
            - --instructions=/in/instructions.md
            - --scrub-instructions
            - --timeout=10m
            - --result-url=https://reviews.example.com/runs/7f3a9c/result
          env:
            - name: OPENCODE_PROVIDER
              value: anthropic
            - name: OPENCODE_MODEL
              value: claude-sonnet-4
            - name: OPENCODE_API_KEY
              valueFrom:
                secretKeyRef:
                  name: review-7f3a9c-llm
                  key: api-key
            - name: PERGENT_RESULT_TOKEN
              valueFrom:
                secretKeyRef:
                  name: review-7f3a9c-llm
                  key: result-token
          volumeMounts:
            - name: work
              mountPath: /work
            - name: inputs
              mountPath: /in
              readOnly: true
```

Notes:

- `activeDeadlineSeconds` counts from the Job's start, so image pulls and the clone use it up. Keep it above `--timeout` plus time to finish and deliver, so pergent's own budget always ends first.
- The Job's deadline and a cancelled Job both send `SIGTERM`; `terminationGracePeriodSeconds: 30` is what lets pergent deliver afterwards.
- If the init container and the pergent container run as different users, git refuses the checkout as "dubious ownership" and pergent reports `checkout_missing`. Run both as the same user, or add `safe.directory` for the checkout (for example `GIT_CONFIG_COUNT=1`, `GIT_CONFIG_KEY_0=safe.directory`, `GIT_CONFIG_VALUE_0=/work/repo` in the pergent container's environment).
- Fetching a commit by SHA depends on the git host allowing it. If the merge base can't be fetched that way, fetch the target branch with a larger `--depth` instead; pergent reports `commit_missing` when a commit is absent.
- A crash, an out-of-memory kill or a deadline kill delivers no result. The service must read the Job's status for those.

The result's fields, error codes and examples are in [docs/agent-result-schema.md](../agent-result-schema.md).
