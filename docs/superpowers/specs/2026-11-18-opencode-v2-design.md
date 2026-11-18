# Move pergent to opencode 2

Status: approach agreed with the owner on 2026-11-18 (switch outright, approach A below), and the open decisions settled in a grilling round the same day (see Decisions). The owner approves this document before the plan is written.

pergent only. pergent.sh changes nothing but its default Job image, once pergent is released.

## Why

pergent pins opencode 1.19.4. New work lands on the 2.x line. In particular, the **Claude Agent** provider (opencode 2.2.3 and later) runs Claude models through a local Claude Code login, which some users want for reviews. The longer pergent stays on 1.x, the bigger the jump gets: 2.x had about 40 releases in its first month.

Using a Claude subscription through that provider is **not** part of this design. Anthropic's published terms don't clearly allow a tool like pergent to use a subscription token, and the owner has parked it until that is confirmed. This design only makes pergent run correctly and safely on opencode 2, so that piece can follow.

## Goals

1. Every review mode works on opencode 2: platform CI mode, `--local`, own-config mode and agent mode, including linked-repository impact runs.
2. The read-only sandbox holds at least as tightly as on 1.x, and tests that run the real opencode binary prove it.
3. Run outcomes are classified as before: transient provider errors are retried, context overflow is reported as overflow, and nothing passes on a failed run.
4. A user with opencode 1.x on their PATH gets one clear error before any review starts, not a confusing failure in every run.

## Non-goals

- The Claude Agent provider and subscription sign-in (later, and only once the policy question is settled).
- Supporting opencode 1.x alongside 2.x (approach B, rejected).
- Moving pergent's generated config to opencode 2's native config format beyond what correctness needs. opencode 2 reads the 1.x format.
- Changing the JSON result schema or the comment format.

## Anti-goals

- **A tool the sandbox doesn't know about.** opencode 2 added tools pergent's deny list never named (`execute`, the `opencode_*` management tools). A future release will add more. The sandbox must not depend on pergent knowing every tool name.
- **A run on someone else's config.** opencode 2's `run` connects to a shared background service when one is up, and that service ignores pergent's config. A review must never run on it.
- **A false pass from a parser that no longer understands opencode.** An error pergent can't read must still be a failure, never a clean result.
- **A sandbox claim no test checks.** The `grep` leak (fixed in #37) went unnoticed because no test ran a real opencode.

## Approaches

| | What | Trade-off |
| --- | --- | --- |
| **A. Switch outright (chosen)** | Pin 2.x, refuse 1.x at startup, one code path | Own-config and `--local` users must upgrade their own opencode |
| B. Support both | Detect the version; two sets of flags, permissions and error parsing | Nobody is forced to upgrade, but every sandbox fix is made and tested twice |
| C. Wait | Stay on 1.19.x until GitHub marks 2.x as Latest | No work now, no Claude Agent provider, and a bigger jump later |

The owner chose A: the sandbox is pergent's main promise, and one code path is the one that can be tested properly.

## What the spike found

On 2026-11-18, `pergent agent` (built from `master`) ran against opencode 2.3.2 and a scripted fake OpenAI-compatible model server, with no real model key. The fake model made tool calls chosen to probe the sandbox. The results drive the design below.

| Area | Result on 2.3.2 |
| --- | --- |
| `--format json` events | `step_start`, `step_finish` (with `reason`, `tokens`, `cost`), `text`, `tool_use` and `error` keep their 1.x shape. pergent parsed a clean run without code changes |
| 1.x config | Accepted. The `bash`, `task`, `edit`, `webfetch` and `websearch` denies also cover the renamed tools (`shell`, `subagent`, `write`, `edit`) |
| `read` rules | `*.env` refused; `/etc/hosts`, a parent directory and `session_move` refused by `external_directory` |
| New tools | `execute` (JavaScript "Code Mode") **ran**. Through it, `worktree_create` wrote a git worktree into the reviewed repo's `.git`. Denying `execute` alone exposed the same tools directly as `opencode_worktree_create` and others. `fetch` inside `execute` was refused by the `webfetch` deny |
| Deny that worked | `execute`, `opencode_*`, `experimental_image_generate` and `question` denied left only `read`, `glob`, `grep` and `skill` offered |
| `skill` | Still offered, though denied. Skills planted in the repo, `.claude/skills`, `.agents/skills`, `~/.claude/skills` and opencode's global folder were never advertised, and loading any was refused |
| `question` | Cancelled in a non-interactive run, so it can't hang |
| Shared service | With a service running, `run` without `--standalone` attached to it and failed with `Agent not found: "pergent"`. With `--standalone`, pergent's sandbox applied |
| Errors | `{"type": "provider.internal", "message": ..., "status": 500, "response": {"body": ...}}`. A context overflow came as `provider.invalid-request` with status 400 and the provider's message. pergent's parser read neither and reported "opencode reported error: (no message)". opencode retried a 500 itself: 12 requests |
| Instruction files | The global `$XDG_CONFIG_HOME/opencode/AGENTS.md` was loaded (as on 1.x). A parent directory's `AGENTS.md` and `~/.claude/CLAUDE.md` were not |
| Tool input | `read`, `glob` and `grep` take `path` (1.x `read` took `filePath`); `list` is gone and `read` lists directories |

Not tested in the spike: the linux image, `--local`, own-config mode, linked-repository impact runs, whether killing a run stops opencode's private server, where 2.x writes snapshots, and whether `"*": "deny"` works on 2.x.

## Design

### 1. Pin opencode 2 in the image

`Dockerfile`: `OPENCODE_VERSION=2.3.2`. The release's assets (`opencode-linux-x64.tar.gz`, `opencode-linux-arm64.tar.gz`) keep their 1.x names. Their GitHub digests:

- x64 `563b5d663637226b6e3bb4bb6803281bc39b0d2cd88db06d674b6420aab009b0`
- arm64 `9d10185acdb72336e086e699ca363a6065f410fd3c1caed0dc8046e95ac06ccb`

The binary is about 225 MB, against about 110 MB for 1.19.4. The tarball must still hold the binary where the `Dockerfile` expects it. The image's existing `opencode --version` check stays. The builder may pick a newer 2.3.x patch if it passes the same tests; the version and checksums then change together.

### 2. Refuse opencode 1.x before any review

At startup, before any run, pergent runs `opencode --version` (2.x prints `opencode v2.3.2`, 1.x prints `1.19.4`) and refuses anything below the floor with a config error naming the version it found and the floor. The floor is the version pinned in the image, 2.3.2.

A version newer than the tested one is allowed, with a warning in the log (`tested with opencode 2.3.2, found 2.5.0`). `--local` and own-config users update opencode on their own schedule. The allow-list (section 4) keeps any tool a newer release adds denied, and an event pergent can't parse is still a failure (section 5).

- **CI mode and `--local`:** a config error before any review, exit non-zero, as for other config errors.
- **Agent mode:** an error result with code `internal` and the same message, exit 3.
- **A version string pergent can't parse:** refused the same way. Never assumed to be new enough.

### 3. Always run a private server

Every `opencode run` gets `--standalone`, in every mode, own-config mode included (an own-config run still reads the user's global config; it only ignores a shared service, which may be running another project with other settings). A private server reads `OPENCODE_CONFIG` and the run's own data directory. Without it, a run on a machine where the shared service is up uses that service's config, permissions and model.

Killing a run (timeout, step limit, cancel) already kills opencode's whole process group. A test must show the private server dies with it, and that no opencode process outlives a review.

### 4. An allow-list sandbox

The generated permissions become an allow-list:

- everything denied (`"*": "deny"`);
- `read` allowed with today's file rules (`*.env`, `*.env.*` except `*.env.example`; in agent mode `*.pem`, `*.key`, `*.tfvars`);
- `glob` and `grep` allowed;
- `external_directory` denied, except the paths pergent allows today: the diffs folder, the run's `tool-output` folder and, for an impact run, the linked clones;
- `doom_loop` as today.

The first test of the plan checks that opencode 2 honours `"*": "deny"` in the 1.x format. The test asserts that the tools offered to the model are exactly `read`, `glob`, `grep`, plus `skill` if 2.x keeps offering it. A visible `skill` is accepted, as long as every call to it is refused and no skill is advertised. The test also asserts that `execute`, `opencode_worktree_create`, `shell`, `write`, `edit`, `webfetch`, `websearch`, `subagent`, `experimental_image_generate` and `question` are either absent or refused. If the 1.x-format wildcard doesn't work, the builder uses opencode 2's native ordered `permissions` array for this block. If that fails too, the explicit deny list the spike proved (`execute`, `opencode_*`, `experimental_image_generate`, `question` on top of today's) is the fallback, and that choice is written in `CLAUDE.md`.

### 5. Read opencode 2's errors

`internal/runner` parses the 2.x error event, `{type, message, status, retryAfterMs, response.body}`, and drops the 1.x shape (`name`, `data.message`, `data.statusCode`, `data.isRetryable`).

- **Transient:** status 429 or 5xx, or a type opencode uses for rate limits and server errors (`provider.internal` and the others the builder finds in opencode's `provider-error.ts`), or today's connection-error message pattern when there is no status. Retried as today.
- **Context overflow:** `provider.invalid-request` (or no status) whose message or response body names a context overflow. The builder takes the phrases from opencode's own `isContextOverflow` patterns rather than inventing new ones. Never a 429.
- **Not retried:** auth, quota, content policy, payload too large, aborted.
- **Unknown type or no message:** still a failed run (`opencode_error`), with the type in the message. Never a pass.

opencode 2 already retries a provider error several times inside one run: a 500 took 12 requests in the spike. So pergent retries a run that failed on a provider error (an error event with a status or a `provider.*` type) **once**, and keeps up to 3 retries for other transient failures, such as opencode dying before the first step. All retries stay within the review's time budget, as today.

### 6. Tool names

- `Transcript.Opened`, which proves an impact run looked inside a linked clone, records completed `read`, `glob` and `grep` calls. `list` no longer exists. `openedPath` already reads `path`.
- The progress lines keep printing the tool name opencode reports.

### 7. Data, state and snapshots

- Each run keeps its own `XDG_DATA_HOME`. The spike saw opencode 2 write under it (`opencode/worktree/...`). The builder confirms that 2.x still saves long tool results in `<XDG_DATA_HOME>/opencode/tool-output` (the folder pergent allows) and still reads `auth.json` from `<XDG_DATA_HOME>/opencode/`, which pergent copies in.
- Each run also gets its own empty `XDG_STATE_HOME`, removed with the run, like the data directory. Nothing opencode keeps there, such as the shared service's registration file, is then read or written by a review.
- The generated config always turns snapshots off (`snapshot: false`; 2.x reads it as `snapshots`). opencode 2's steps carry a `snapshot` hash, so by default it takes git snapshots, and a read-only reviewer has nothing to undo.

### 8. Instruction files

pergent's defences stay as they are: CI mode passes the root `AGENTS.md`/`CLAUDE.md` explicitly, agent mode refuses or scrubs nested instruction files, and the scrub deletes links and denied files. On top, every run with a generated config gets `OPENCODE_DISABLE_CLAUDE_CODE=true`, which in 2.x stops every `CLAUDE.md` fallback (per directory and `~/.claude/CLAUDE.md`). Own-config runs don't: there the user's opencode finds its instruction files itself, as on 1.x. A test checks that a nested `AGENTS.md` is still refused in agent mode and that `CLAUDE.md` files are not loaded.

The global `$XDG_CONFIG_HOME/opencode/AGENTS.md` keeps loading, as on 1.x. It belongs to whoever runs pergent: agent mode already refuses an `XDG_CONFIG_HOME` inside the checkout.

### 9. Tests against the real binary

The unit tests keep their fake opencode. A new opt-in integration test runs the real binary:

- **Opt-in:** it runs only when an env var names an opencode binary, for example `PERGENT_TEST_OPENCODE=/path/to/opencode`, and is skipped otherwise.
- **Fake model:** a small fake OpenAI-compatible server inside the Go test that scripts tool calls.
- **What it checks:**
  - the tools offered (section 4);
  - each denied read, write and escape;
  - the `grep` leak from #37 staying closed;
  - `--standalone` with a shared service running;
  - the error events from section 5 (a 500 and an overflow);
  - that no opencode process outlives a run.
- **CI:** a job on every pergent PR downloads the pinned opencode (checksum-verified, as the `Dockerfile` does, and cached by version) and runs these tests. That job is the gate for every later opencode bump.

### 10. Docs and release

- `CLAUDE.md`: the runner section (errors, `--standalone`, version floor), the read-only-agent section (allow-list, new tools) and the own-config section (2.x required).
- `README.md` and `docs/concepts/sandbox.md`: the allow-list, `--standalone`, the version floor.
- A breaking-change note: own-config and `--local` users need opencode 2.3.2 or later on their PATH.
- Release: a minor version (v0.11.0), owner's decision. pergent.sh then moves its default Job image; no other pergent.sh change.

## Decisions (grilling, 2026-11-18)

1. The version floor is the pinned 2.3.2.
2. A newer version is allowed with a warning, never refused.
3. `--standalone` on every run, own-config mode included.
4. One pergent retry for a provider error (opencode already retried it); up to 3 for other transient failures.
5. Snapshots always off.
6. Each run gets its own `XDG_STATE_HOME`.
7. The real-opencode test runs in CI on every PR, with the binary cached by version.
8. A visible but always-refused `skill` tool is accepted; the real-opencode test checks it.

## Left to the builder

- Any newer 2.3.x patch, if it passes the same tests (the floor then moves with it).
- Which fallback applies if `"*": "deny"` doesn't work (section 4, in the order given).
- The exact list of provider error types, taken from opencode's source.
- How the CI job caches the binary.

## Open questions

None.
