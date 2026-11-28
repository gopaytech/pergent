# Context window and max output per model entry, pergent CLI half: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every pergent command that runs opencode (CI and local review, `pergent agent` with its reviewers and judge, `pergent scan`) takes a context window and a max output (`PERGENT_CONTEXT_LIMIT` / `--context-limit`, `PERGENT_OUTPUT_LIMIT` / `--output-limit`), declares both on the model in the generated opencode config, caps review batches at 40,000 tokens, and reports a run that opencode fails with `output-limit` with one fixed reason.

**Architecture:** `internal/config` resolves and checks both numbers (one helper, `ResolveLimit`, that `pergent scan` also uses with its injected `getenv`) and writes `limit: {context, output}` in `providerConfig`, which the review config and the scan config share. `review.Request` carries the output limit into the one place that writes a review config (`reviewer.runWith`), so batch, repair, cross-batch, verification, impact and judge runs all get it. `internal/runner` marks an `output-limit` error event (`Cause.OutputLimited`) and gives it the fixed text; `internal/scan` uses the same text as the envelope's reason.

**Tech Stack:** Go 1.24, opencode 2.3.2 (pinned, unchanged), `encoding/json`, `flag`.

**Spec:** pergent.sh `docs/superpowers/specs/2026-11-28-model-limits-design.md`, sections "pergent", "Older pergent images" and "Tests". Its Goals and Non-goals bind this plan. pergent.sh is built apart from this plan; the env names and texts in Global Constraints are the contract with it.

## Findings (checked on `origin/master` efdcd87)

| # | Fact | Where |
|---|---|---|
| 1 | `providerConfig(m, contextLimit)` writes `limit` only when the context is > 0, with `output` always 0. Both the review config and the scan config call it. | `internal/config/opencode.go:314-345`, `internal/config/scan.go:99` |
| 2 | `ScanOptions.ContextLimit` exists, but `cmd/scan.go` never sets it and `parseScan` has no limit flag. | `internal/config/scan.go:21`, `cmd/scan.go:114-184`, `:393` |
| 3 | `resolveReview` reads `PERGENT_CONTEXT_LIMIT` with `resolveInt` (process env) and refuses a negative value: `--context-limit must be a positive number of tokens, got %d`. A non-integer env value is `%s=%q is not a whole number`. | `internal/config/resolver.go:220`, `:251`, `:357` |
| 4 | Every review run's config is made in one place, `reviewer.runWith` (`config.OpenCodeOptions`). The judge of a multi review goes through it too. | `internal/review/review.go:1096-1116` |
| 5 | `diff.BatchBudget` is `context / 4`, else 40,000. It sizes review batches (`review.go:486`, `diffs.go:67`) and the impact run's diff cap (`impact.go:128`). | `internal/diff/batch.go:8-17` |
| 6 | An error event becomes `CauseOpencodeError` with `ErrorName` = the event's `type`, unless it is a context overflow. `"Response still truncated after two output token limit continuations"` matches no overflow pattern and no `transientPattern`, and `Classify` gives it no class. | `internal/runner/outcome.go:293-315`, `overflow.go`, `provider.go` |
| 7 | A scan's failed run becomes `reason = "the model run failed: " + FailKind`. | `internal/scan/envelope.go:200-208`, `cmd/scan.go:763-785` |
| 8 | The agent result's `cause.kind` is a closed list in `docs/agent-result-schema.md:195`. | |
| 9 | pergent keeps no release notes or upgrade file in the repository. The behaviour of `--context-limit` is described in `docs/reference/configuration.md` and `docs/concepts/skills.md:68`. | |

## Global Constraints

- Names, fixed: env `PERGENT_CONTEXT_LIMIT` and `PERGENT_OUTPUT_LIMIT`; flags `--context-limit` and `--output-limit`. Both flags on CI and local mode (`cmd/main.go`), `pergent agent` (`cmd/agent.go`, which the multi review judge also parses) and `pergent scan`. The deprecated `--max-tokens` stays as it is.
- `0` or unset = not set. A flag that is not 0 wins and its variable is not read. A negative value is refused with `--<flag> must be a positive number of tokens, got <n>`. A non-integer variable is refused with `<VAR>="<value>" is not a whole number`. CI and local mode: a config error. `pergent agent` and `pergent scan`: a usage error (exit 2).
- Generated config: when either number is > 0, the model entry under `provider.<id>.models.<model>` has `"limit": {"context": <c>, "output": <o>}`, the unset one `0`. When both are 0 there is no `limit` key, so the config is byte for byte today's.
- `diff.BatchBudget(c)` is `min(c/4, 40000)` when `c > 0`, else `40000`.
- The fixed reason, exact bytes: `the model's replies were cut at the max output: set Max output on the model entry`. A scan's `reason` is exactly this text. A review's `Cause.Message()` is this text plus `.` (every cause message is a sentence). No opencode or model text is added to either.
- The `output-limit` error keeps the cause kind `opencode_error`. No new `CauseKind`, so the agent result schema and pergent.sh's readers do not change.
- No release: no version bump, no tag, no version number in any file. opencode stays pinned at 2.3.2.
- Local tests: `go test ./<pkg> -count=1 -run '^(TestA|TestB)$'` with exact names only, plus `make vet` and `gofmt -l .`. Never a whole package, `./...` or `make test`. No docker, no containers, no network, no paid model. The `OpencodeE2E` tests run on CI only.
- Fast tests: unit tests under 100 ms, table subtests on one setup. A test that calls `t.Setenv` or reads `OPENCODE_*` from the process stays serial.
- Public repository: no employer, product, customer, cloud or internal names, no local paths, no email addresses, in code, tests, docs or commits. Before each commit run the private-name scan and the email and local-path grep from the agent memory over the changed files; both print nothing.
- Commit with the owner's personal identity as the agent memory says; never write an email into a file or a command in this plan. Conventional subject; last line `Co-Authored-By: Claude <model that wrote it> <noreply@anthropic.com>`. Test and commit as separate commands.
- Easy English in comments, help texts and docs.

## Review Focus

1. **Only one limit set.** `--output-limit 64000` with no context limit must give `{"context": 0, "output": 64000}` and keep the batch budget at 40,000; `--context-limit` alone keeps `output: 0`. Pinned in Task 1 (config tables) and Task 3 (budget table).
2. **An explicit `0` flag with the variable set.** `--output-limit 0` and `PERGENT_OUTPUT_LIMIT=64000` gives 64,000, as `--context-limit` does today. Pinned in Task 2 (`TestResolve_OutputLimit`) and Task 5 (scan table).
3. **The fixed reason never carries other text.** An `output-limit` event with a message, other error events after it, or several attempts still gives exactly the fixed text. Pinned in Task 6 (`TestCauseMessage_OutputLimit`).
4. **A scan cut at the max output is never `complete`.** With valid findings it is `partial`, without them `failed`, both with the fixed reason. Pinned in Task 6 (envelope table rows and `TestScanOutputLimitReason`).
5. **No relaunch on `output-limit`.** It is not transient: starting the run again costs the same tokens and fails the same way. Pinned in Task 6 (`TestEventError_Transient` row).

---

### Task 1: The generated configs declare both limits

**Files:**
- Modify: `internal/config/opencode.go:94-101` (`OpenCodeOptions`), `:249-265` (`GenerateOpenCodeConfigWith`), `:312-345` (`providerConfig`)
- Modify: `internal/config/scan.go:17-22` (`ScanOptions`), `:99`
- Test: `internal/config/opencode_test.go:419` (`TestGenerateOpenCodeConfig_LimitsAndCompaction`), `internal/config/scan_test.go` (new `TestScanConfigLimits`)

**Interfaces:**
- Produces: `config.OpenCodeOptions.OutputLimit int`; `config.ScanOptions.OutputLimit int`; `providerConfig(m Model, contextLimit, outputLimit int) (fullModel string, p map[string]openCodeProvider)`. `GenerateOpenCodeConfig(maxTurns, contextLimit int, skillPrompt, repoPath string)` keeps its signature (output 0).

- [ ] **Step 1: Write the failing tests**

Turn `TestGenerateOpenCodeConfig_LimitsAndCompaction` into a table over `GenerateOpenCodeConfigWith(OpenCodeOptions{MaxTurns: 20, ContextLimit: c, OutputLimit: o, RepoPath: t.TempDir()})`. Keep its compaction and model-entry checks.

| name | context | output | want `limit` |
|---|---|---|---|
| `model registry limits by default` | 0 | 0 | no `limit` key |
| `context only` | 64000 | 0 | `{"context": 64000, "output": 0}` |
| `output only` | 0 | 64000 | `{"context": 0, "output": 64000}` |
| `both` | 1000000 | 64000 | `{"context": 1000000, "output": 64000}` |

Add `TestScanConfigLimits` in `scan_test.go` with the same four rows through `scanConfig(t, o)` (`o := scanOptions(); o.ContextLimit, o.OutputLimit = c, out`), reading `provider.anthropic.models.claude-sonnet-4.limit` (`setScanModel`). Both tests compare decoded numbers (`float64`), never bytes.

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/config -count=1 -run '^(TestGenerateOpenCodeConfig_LimitsAndCompaction|TestScanConfigLimits)$'`
Expected: build failure, `unknown field OutputLimit in struct literal`.

- [ ] **Step 3: Implement**

Add `OutputLimit int` to both option structs, with a comment in the style of `ContextLimit` ("the most tokens one reply may use, thinking included, when > 0; otherwise opencode's registry value or its own default"). `providerConfig` sets `modelEntry.Limit = &openCodeLimit{Context: contextLimit, Output: outputLimit}` when either is > 0. Update its doc comment and the `Output` field comment on `openCodeLimit` (0 still means opencode's own value). Both callers pass `o.OutputLimit`.

- [ ] **Step 4: Run the tests, plus the byte-identical check**

Run: `go test ./internal/config -count=1 -run '^(TestGenerateOpenCodeConfig_LimitsAndCompaction|TestScanConfigLimits|TestScanConfigAgents)$'`
Run: `go test ./internal/review -count=1 -run '^(TestRun_DefaultUnchanged|TestRun_GeneratesConfigPerRun)$'`
Expected: PASS for all.

- [ ] **Step 5: Commit**

```bash
git add internal/config/opencode.go internal/config/scan.go internal/config/opencode_test.go internal/config/scan_test.go
git commit -m "feat(config): declare the context and output limits on the model"
```

### Task 2: Resolve and check `PERGENT_OUTPUT_LIMIT`

**Files:**
- Modify: `internal/config/resolver.go:13-33` (`Options`), `:63-93` (`Config`), `:213-262` (`resolveReview`)
- Test: `internal/config/resolver_test.go` (new `TestResolve_OutputLimit`), `internal/config/agent_test.go:35`, `:59`, `internal/config/resolver_values_test.go:10`, `:45`, `:61`

**Interfaces:**
- Produces: `config.Options.OutputLimit int`; `config.Config.OutputLimit int` (0 = opencode's own value); `func ResolveLimit(flag int, envKey, flagName string, getenv func(string) string) (int, error)`: the flag when not 0, else the variable, else 0; then refuses a negative result. Error texts as in Global Constraints. `pergent scan` (Task 5) uses it with its own `getenv`.

- [ ] **Step 1: Write the failing tests**

`TestResolve_OutputLimit` (serial, `setupValidGitHub(t)`), one table on `Resolve(Options{Skills: []string{"code-review"}, Platform: "github", OutputLimit: flag})`:

| name | `PERGENT_OUTPUT_LIMIT` | flag | want |
|---|---|---|---|
| `unset` | `""` | 0 | 0 |
| `from env` | `64000` | 0 | 64000 |
| `flag wins` | `32000` | 64000 | 64000 |
| `flag 0 is unset` | `64000` | 0 | 64000 |
| `negative flag` | `""` | -1 | error containing `--output-limit must be a positive number of tokens, got -1` |
| `negative env` | `-5` | 0 | error containing `--output-limit must be a positive number of tokens, got -5` |

Add rows to existing tables:
- `TestResolve_BadEnvValuesAreErrors`: `{"output limit text", "PERGENT_OUTPUT_LIMIT", "64k", []string{"PERGENT_OUTPUT_LIMIT", `"64k"`}}`.
- `TestResolve_EmptyEnvValuesMeanTheDefault`: add `PERGENT_OUTPUT_LIMIT` to the names and `cfg.OutputLimit != 0` to the check.
- `TestResolve_FlagBeatsBadEnv`: `t.Setenv("PERGENT_OUTPUT_LIMIT", "lots")`, `OutputLimit: 2000`, check `cfg.OutputLimit == 2000`.
- `TestResolveAgent_FlagsBeatEnv`: `PERGENT_OUTPUT_LIMIT=2000` from env, then `OutputLimit: 77` from the flag.
- `TestResolveAgent_Invalid`: `{"negative output limit", Options{OutputLimit: -1}, "--output-limit must be a positive number"}`.

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/config -count=1 -run '^(TestResolve_OutputLimit|TestResolve_BadEnvValuesAreErrors|TestResolve_EmptyEnvValuesMeanTheDefault|TestResolve_FlagBeatsBadEnv|TestResolveAgent_FlagsBeatEnv|TestResolveAgent_Invalid)$'`
Expected: build failure on `OutputLimit`.

- [ ] **Step 3: Implement**

Add the fields. Write `ResolveLimit` beside `resolveInt` (same parse and message, with the injected `getenv`, then the negative check). In `resolveReview` resolve both `ContextLimit` and `OutputLimit` with `ResolveLimit(..., os.Getenv)`, and drop the separate `cfg.ContextLimit < 0` check, so the context limit keeps its exact texts. Mention `OutputLimit` in the `ResolveAgent` doc comment.

- [ ] **Step 4: Run the tests to see them pass**

Run: `go test ./internal/config -count=1 -run '^(TestResolve_OutputLimit|TestResolve_ContextLimit|TestResolve_BadEnvValuesAreErrors|TestResolve_EmptyEnvValuesMeanTheDefault|TestResolve_FlagBeatsBadEnv|TestResolveAgent_FlagsBeatEnv|TestResolveAgent_Invalid|TestResolveAgent_DefaultsWithoutPlatform)$'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/resolver.go internal/config/resolver_test.go internal/config/agent_test.go internal/config/resolver_values_test.go
git commit -m "feat(config): read PERGENT_OUTPUT_LIMIT and check both limits in one place"
```

### Task 3: Review batches stay at most 40,000 tokens

**Files:**
- Modify: `internal/diff/batch.go:8-17`
- Test: `internal/diff/batch_test.go:129` (`TestBatchBudget`)

**Interfaces:**
- Produces: `diff.BatchBudget(contextLimit int) int`, signature unchanged.

- [ ] **Step 1: Write the failing test**

Make `TestBatchBudget` a table: `0` gives `40000`, `-1` gives `40000`, `400` gives `100`, `100000` gives `25000`, `160000` gives `40000`, `200000` gives `40000`, `1000000` gives `40000`.

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./internal/diff -count=1 -run '^TestBatchBudget$'`
Expected: FAIL, `BatchBudget(200000) = 50000, want 40000`.

- [ ] **Step 3: Implement**

`min(contextLimit/4, defaultBatchBudget)` when `contextLimit > 0`. Doc comment: a quarter of the limit, at most the default, so a big window stops overflows without making batches bigger.

- [ ] **Step 4: Run it, plus the review tests that size batches from a context limit**

Run: `go test ./internal/diff -count=1 -run '^TestBatchBudget$'`
Run: `go test ./internal/review -count=1 -run '^(TestCIResultsShrinkBatchBudget|TestSecurityFindingsShrinkBatchBudget|TestRuleCountsInFullBudgetFallback|TestRun_GeneratesConfigPerRun)$'`
Expected: PASS (every review test uses a context limit of 100,000 or less).

- [ ] **Step 5: Commit**

```bash
git add internal/diff/batch.go internal/diff/batch_test.go
git commit -m "feat(diff): cap the batch budget at 40,000 tokens"
```

### Task 4: Reviews pass the output limit to every run

**Files:**
- Modify: `internal/review/review.go:53` (`Request`), `:1096-1102` (`runWith`)
- Modify: `cmd/main.go:57-77` (flag), `:118-119` (`config.Options`), `:219` (`review.Request`)
- Modify: `cmd/agent.go:355` (`agentRequest`), `:414`, `:447-448` (flag), `:578-579`
- Test: `internal/review/review_test.go` (new `TestRun_LimitsReachTheConfig`), `cmd/agent_test.go:132` (`TestParseAgent_OK`), `:163` (`TestParseAgent_Usage`), `cmd/limits_test.go` (new `TestMain_LimitsInLocalMode`)

**Interfaces:**
- Consumes: `config.OpenCodeOptions.OutputLimit` (Task 1); `config.Options.OutputLimit`, `config.Config.OutputLimit` (Task 2).
- Produces: `review.Request.OutputLimit int` (0 = opencode's own value). `runner.Options` does not change.

- [ ] **Step 1: Write the failing tests**

`TestRun_LimitsReachTheConfig` (serial: `t.Setenv("OPENCODE_PROVIDER", "anthropic")`, `t.Setenv("OPENCODE_MODEL", "test-model")`, as `TestRun_GeneratesConfigPerRun`): `req.ContextLimit = 1000000`, `req.OutputLimit = 64000`, one small file. In `f.respond`, read `c.opts.ConfigPath` and decode it. Assert `provider.anthropic.models.test-model.limit` is `{"context": 1000000, "output": 64000}` for every call.

`TestParseAgent_OK`: add `"--context-limit", "1000000", "--output-limit", "64000"` to the args. Assert `in.Review.ContextLimit == 1000000`, `in.Review.OutputLimit == 64000`, and `agentRequest(&in, nil).OutputLimit == 64000`.

`TestParseAgent_Usage`: add `{"negative output limit", with("--output-limit", "-1"), nil}`.

`TestMain_LimitsInLocalMode` in a new `cmd/limits_test.go`, built like `TestMain_UsageFileInLocalMode` (`cmd/usage_test.go:52`): a child process runs `main()` with `--local --skill code-review --repo-path <repo> --context-limit 1000000 --output-limit 64000`, against a stub `opencode` on `PATH`. The stub first copies `$OPENCODE_CONFIG` to a path the test writes into the script text (the runner's env allow-list drops any other variable), then prints the same NDJSON answer as the usage test. Assert the child exits 0 and the copied config's model entry has `limit` `{"context": 1000000, "output": 64000}`. Child env: `OPENCODE_PROVIDER=anthropic`, `OPENCODE_MODEL=claude-sonnet-4`, and its own `XDG_DATA_HOME`.

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/review -count=1 -run '^TestRun_LimitsReachTheConfig$'`
Run: `go test ./cmd -count=1 -run '^(TestParseAgent_OK|TestParseAgent_Usage|TestMain_LimitsInLocalMode)$'`
Expected: build failure on `OutputLimit`, then (after the fields exist) `flag provided but not defined: -output-limit`.

- [ ] **Step 3: Implement**

- `review.Request`: `OutputLimit int` next to `ContextLimit`. `runWith` sets `OutputLimit: r.req.OutputLimit` in `config.OpenCodeOptions`.
- `cmd/main.go` and `cmd/agent.go`: `--output-limit`, help text `Max output tokens of one model reply, thinking included, only for models opencode has no limits for; 64000 is enough for reviews (default: opencode's model registry)`. Pass it to `config.Options.OutputLimit`; `main` passes `cfg.OutputLimit` and `agentRequest` passes `in.Review.OutputLimit` to `review.Request`.

- [ ] **Step 4: Run the tests to see them pass**

Run: `go test ./internal/review -count=1 -run '^(TestRun_LimitsReachTheConfig|TestRun_DefaultUnchanged|TestRun_GeneratesConfigPerRun)$'`
Run: `go test ./cmd -count=1 -run '^(TestParseAgent_OK|TestParseAgent_Usage|TestMain_LimitsInLocalMode|TestMain_UsageFileInLocalMode)$'`
Expected: PASS. `TestMain_LimitsInLocalMode` under 2 s (a child process and a git repo); name it in the report if it is slower.

- [ ] **Step 5: Commit**

```bash
git add internal/review/review.go internal/review/review_test.go cmd/main.go cmd/agent.go cmd/agent_test.go cmd/limits_test.go
git commit -m "feat: --output-limit for reviews and pergent agent"
```

### Task 5: `pergent scan` reads both limits

**Files:**
- Modify: `cmd/scan.go:93-104` (`scanInputs`), `:114-184` (`parseScan`), `:393-396` (`config.ScanOptions`)
- Test: `cmd/scan_test.go:63` (`scanEnv`), `:759` (`TestScanUsageErrors`), new `TestScanLimits`

**Interfaces:**
- Consumes: `config.ResolveLimit` (Task 2); `config.ScanOptions.ContextLimit`, `.OutputLimit` (Task 1).
- Produces: `scanInputs.contextLimit, outputLimit int`.

- [ ] **Step 1: Write the failing tests**

`scanEnv`: add `"PERGENT_CONTEXT_LIMIT": ""` and `"PERGENT_OUTPUT_LIMIT": ""` to the defaults, so the developer's own values never reach a test.

`TestScanLimits` (serial; one `scanClone` and one `scanSkillDir` for all rows): each row runs `runScan` with `--repo-path`, `--skill`, `--result` plus its args, a `fakeScan{files: allFixtures, check: ...}` that decodes `o.ConfigPath` and reads `provider.anthropic.models.some-model.limit`. Expect exit 0.

| name | args | env | want `limit` |
|---|---|---|---|
| `none` | none | none | no `limit` key |
| `flags` | `--context-limit 1000000 --output-limit 64000` | none | `{1000000, 64000}` |
| `env` | none | `PERGENT_CONTEXT_LIMIT=1000000`, `PERGENT_OUTPUT_LIMIT=64000` | `{1000000, 64000}` |
| `flag beats env` | `--output-limit 32000` | `PERGENT_OUTPUT_LIMIT=64000` | `{0, 32000}` |
| `flag 0 is unset` | `--output-limit 0` | `PERGENT_OUTPUT_LIMIT=64000` | `{0, 64000}` |

`TestScanUsageErrors`: add `{"context limit negative", []string{"--repo-path", repo, "--context-limit", "-1"}, nil}`, `{"output limit negative", []string{"--repo-path", repo, "--output-limit", "-1"}, nil}`, `{"context limit text", []string{"--repo-path", repo}, map[string]string{"PERGENT_CONTEXT_LIMIT": "lots"}}`, `{"output limit text", []string{"--repo-path", repo}, map[string]string{"PERGENT_OUTPUT_LIMIT": "64k"}}`. The table already expects exit 2, no run and the reason `the scan could not start: the inputs are not valid`.

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./cmd -count=1 -run '^(TestScanLimits|TestScanUsageErrors)$'`
Expected: FAIL, `flag provided but not defined: -context-limit` (exit 2 where 0 is wanted), and the env rows exit 0 where 2 is wanted.

- [ ] **Step 3: Implement**

Two `flags.IntVar` lines after `--max-subagent-turns`: `context-limit` (`Model context window in tokens, only for models opencode has no limits for ($PERGENT_CONTEXT_LIMIT)`) and `output-limit` (`Max output tokens of one model reply, thinking included, only for models opencode has no limits for; 64000 is enough for a scan ($PERGENT_OUTPUT_LIMIT)`). After the `PERGENT_TOKEN_BUDGET` check, set both with `config.ResolveLimit(..., getenv)`; an error is `usage("%v", err)`. Pass both to `config.ScanOptions`.

- [ ] **Step 4: Run the tests to see them pass**

Run: `go test ./cmd -count=1 -run '^(TestScanLimits|TestScanUsageErrors|TestScanConfigWiring|TestScanComplete)$'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/scan.go cmd/scan_test.go
git commit -m "feat(scan): read the context and output limits"
```

### Task 6: One fixed reason for replies cut at the max output

**Files:**
- Modify: `internal/runner/outcome.go:20-31` (constants), `:132-151` (`Message`, the `CauseOpencodeError` case), new method near `atStep`
- Create: `internal/runner/testdata/output_limit.ndjson`
- Modify: `internal/scan/envelope.go:27-36` (reason constants), `:92-103` (`EndInput`), `:202-208` (`Build`)
- Modify: `cmd/scan.go:425` (`scan.EndInput` in `runScan`)
- Test: `internal/runner/outcome_test.go:11` (`TestClassify_Fixtures`), `:281` (`TestEventError_Transient`), new `TestCauseMessage_OutputLimit`; `internal/scan/envelope_test.go:30` (`TestBuildStatusTable`); `cmd/scan_test.go` (new `TestScanOutputLimitReason`)

**Interfaces:**
- Produces: `const runner.OutputLimitReason = "the model's replies were cut at the max output: set Max output on the model entry"`; `func (c Cause) OutputLimited() bool` (true when `Kind == CauseOpencodeError` and `ErrorName == "output-limit"`); `scan.EndInput.OutputLimited bool`.

- [ ] **Step 1: Write the failing tests**

Fixture `output_limit.ndjson`, in the shape of the other fixtures: three `step_start` / `text` (`"partial"`) / `step_finish` with `"reason":"length"` and output tokens, then one `{"type":"error",...,"error":{"type":"output-limit","message":"Response still truncated after two output token limit continuations"}}`.

`TestClassify_Fixtures` row: name `replies cut at the max output`, `attempt{runErr: exit1, exitCode: 1}`, `StatusFailed`, kind `CauseOpencodeError`, message `runner.OutputLimitReason + "."`.

`TestEventError_Transient` row: `{EventError{Name: "output-limit", Message: "Response still truncated after two output token limit continuations"}, false}`.

`TestCauseMessage_OutputLimit`, a table on `Cause.Message()` and `Cause.OutputLimited()`:
- `Cause{Kind: CauseOpencodeError, ErrorName: "output-limit", ErrorMessage: "Response still truncated...", OtherErrors: []string{"unknown: x"}, Attempts: 2}`: message exactly `OutputLimitReason + "."`, `OutputLimited()` true.
- `Cause{Kind: CauseOpencodeError, ErrorName: "provider.invalid-request"}`: `OutputLimited()` false, message unchanged (starts with `opencode reported provider.invalid-request`).
- `Cause{Kind: CauseNoAnswer, FinishReason: "length"}`: `OutputLimited()` false.

`TestBuildStatusTable` rows (`EndInput` gets `OutputLimited: true`, `FailKind: runner.CauseOpencodeError`, `Stop: StopFailed`):
- `{"output limit with findings", ..., Out: validOut()}` gives `"partial"`, reason exactly `runner.OutputLimitReason`, 1 result.
- `{"output limit no findings", ..., Out: missing}` gives `"failed"`, the same reason, 0 results.

`TestScanOutputLimitReason` in `cmd/scan_test.go`: add a `result *runner.RunResult` field to `fakeScan`; when set, `run` returns it after copying the files. Run a scan with `allFixtures` and `result = &runner.RunResult{Status: runner.StatusFailed, Cause: &runner.Cause{Kind: runner.CauseOpencodeError, ErrorName: "output-limit", ErrorMessage: "Response still truncated after two output token limit continuations"}}`. Expect exit 0, envelope `status` `partial`, `reason` exactly `runner.OutputLimitReason`.

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/runner -count=1 -run '^(TestClassify_Fixtures|TestEventError_Transient|TestCauseMessage_OutputLimit)$'`
Run: `go test ./internal/scan -count=1 -run '^TestBuildStatusTable$'`
Run: `go test ./cmd -count=1 -run '^TestScanOutputLimitReason$'`
Expected: build failures on `OutputLimitReason`, `OutputLimited` and the `result` field; the Transient row already passes (it pins today's behaviour).

- [ ] **Step 3: Implement**

- `runner`: the const (comment: pergent.sh shows it, so the words are a contract), an unexported `outputLimitType = "output-limit"` (opencode 2.3.2's error type after two continuations), and `OutputLimited`. In `Message`, the `CauseOpencodeError` case returns `OutputLimitReason + "."` first when `c.OutputLimited()`, with nothing added.
- `scan`: `reasonOutputLimit = runner.OutputLimitReason` with the other fixed reasons. In the `StopFailed` case, use it in place of `reasonModelFails + kind` when `in.OutputLimited`. Status rules stay the same.
- `cmd/scan.go`: set `OutputLimited: res.Cause != nil && res.Cause.OutputLimited()` in the `scan.EndInput`.

- [ ] **Step 4: Run the tests to see them pass**

Run: `go test ./internal/runner -count=1 -run '^(TestClassify_Fixtures|TestEventError_Transient|TestCauseMessage_OutputLimit|TestClassify_ProviderClassFromRootError)$'`
Run: `go test ./internal/scan -count=1 -run '^(TestBuildStatusTable|TestBuildRedacts)$'`
Run: `go test ./cmd -count=1 -run '^(TestScanOutputLimitReason|TestScanComplete)$'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runner/outcome.go internal/runner/outcome_test.go internal/runner/testdata/output_limit.ndjson internal/scan/envelope.go internal/scan/envelope_test.go cmd/scan.go cmd/scan_test.go
git commit -m "feat: a fixed reason for replies cut at the max output"
```

### Task 7: The real opencode reports the reason (e2e, CI only)

**Files:**
- Modify: `cmd/fakemodel_test.go:24-30` (`fakeRequest`), `:34-53` (`fakeModel`), `:66-67` (mode list comment), `:122-126` (request decode), the reply part after `:186`
- Test: `cmd/opencode_e2e_test.go` (new `TestOpencodeE2E_OutputLimitReason`)

**Interfaces:**
- Consumes: `runner.OutputLimitReason`, `Cause.OutputLimited` (Task 6); `--output-limit` (Task 4).
- Produces: fake model mode `"length"`; `fakeRequest.MaxTokens int` (the request's `max_tokens`, else `max_completion_tokens`, 0 when neither); `func (m *fakeModel) LengthReplies() int`.

- [ ] **Step 1: Write the test**

Mode `"length"`: a request with tools gets one text reply (`"partial"`) that ends with `finish_reason` `"length"` and usage, streamed or not, and adds one to the count `LengthReplies` reads. A request without tools (the title agent) answers as today.

`TestOpencodeE2E_OutputLimitReason` (`requireOpencode(t)`), two subtests:
- `review`: `newFakeModel(t, nil, "length")`, `useFakeModel`, `e2eRepo`, `runAgentE2E(t, repo, base, head, "--output-limit", "2000")`. Assert one failed skill; `Failures[0].Cause.Kind == "opencode_error"`; `Cause.Message == runner.OutputLimitReason + "."`; `m.LengthReplies() >= 3`; every request with tools has `MaxTokens == 2000` (the limit reached the provider).
- `scan`: `newFakeModel(t, nil, "length")`, `s := newScanE2E(t, m)`, `res := runner.Run(context.Background(), s.options(nil))`. Assert `res.Status == runner.StatusFailed`, `res.Cause.OutputLimited()`, and `m.LengthReplies() >= 3`.

If CI shows fewer than 3 length replies, a continuation came without tools: count and answer by the system prompt instead (the review prompt or `scanMainMarker`), never by loosening the assertion.

- [ ] **Step 2: Check it compiles and skips locally**

Run: `go vet ./cmd`
Run: `go test ./cmd -count=1 -run '^TestOpencodeE2E_OutputLimitReason$'`
Expected: vet clean; the test reports SKIP (no `PERGENT_TEST_OPENCODE`). It runs for real in CI's `e2e` job; read that job on the PR.

- [ ] **Step 3: Commit**

```bash
git add cmd/fakemodel_test.go cmd/opencode_e2e_test.go
git commit -m "test(e2e): replies cut at the max output give the fixed reason"
```

### Task 8: Docs

**Files:**
- Modify: `docs/reference/configuration.md:14` (`--context-limit` row), new `--output-limit` row, `:50` (env rows), `:72` (value rules)
- Modify: `docs/concepts/skills.md:68`, `docs/concepts/agent-mode.md:51`, `docs/concepts/multi-review.md:41`, `docs/concepts/sandbox.md:11`, `docs/concepts/failed-reviews.md:5-12`, `docs/concepts/security-scan.md:34-41` and `:129-130`, `docs/troubleshooting.md:53-58`
- Modify: `CLAUDE.md` (Key Concepts "One opencode run per batch" and "No auto-compaction"; the `cmd/scan.go` architecture line)

- [ ] **Step 1: Write the docs**

- `configuration.md`: `--context-limit` row says it also sets the batch size, a quarter of the window and at most 40,000 tokens, and marks this **Behaviour change**: a `--context-limit` above 160,000 used to give bigger batches, and now gives 40,000-token batches; a smaller one still gives smaller batches. New `--output-limit` row: the most tokens one reply may use, thinking included, for models opencode has no limits for; opencode asks for at most 256,000 itself; 64,000 is enough for reviews and scans, and a provider that counts the asked amount against a rate limit may throttle a higher value. New `PERGENT_OUTPUT_LIMIT` env row. Value rules: `PERGENT_CONTEXT_LIMIT` and `PERGENT_OUTPUT_LIMIT` not negative.
- `skills.md:68`: "about a quarter of the model's context window and at most 40,000 tokens (40,000 when the window is unknown)", with the same behaviour-change note in one sentence.
- `agent-mode.md:51` and `multi-review.md:41`: add `--output-limit` beside `--context-limit`.
- `sandbox.md:11`: "unless you set `--context-limit` or `--output-limit`".
- `failed-reviews.md`: a table row `Replies cut at the max output` with the fixed text, and one line that opencode gives up after a reply is cut 3 times and pergent does not start the run again.
- `security-scan.md`: rows for `--context-limit` / `$PERGENT_CONTEXT_LIMIT` and `--output-limit` / `$PERGENT_OUTPUT_LIMIT` (a negative value or text is a usage error); a reason-table row before `the model run failed: <kind>`: opencode stopped because replies were cut at the max output: `partial` with valid findings, else `failed`, reason the fixed text.
- `troubleshooting.md`: a new section titled with the fixed text in backticks: the model's replies hit the max output that opencode used (its catalog value, or 32,000 for a model id it does not know, shared with thinking); set `--output-limit` / `PERGENT_OUTPUT_LIMIT` (in pergent.sh: **Max output** on the model entry), and `--context-limit` too for a model id opencode does not know. The overflow section names `--output-limit` beside `--context-limit`.
- `CLAUDE.md`: batches fit a quarter of the model's context, at most 40,000 tokens; the generated config declares `limit {context, output}` from `--context-limit` / `--output-limit`; `pergent scan` reads both; an `output-limit` error is `opencode_error` with `runner.OutputLimitReason`.

pergent keeps no release notes file. The behaviour change is named in `configuration.md` and `skills.md`, where `--context-limit` and the batch size are described. The GitHub Release notes are the owner's, at release time.

- [ ] **Step 2: Check**

Run: `make vet`
Run: `gofmt -l .`
Expected: no output from either.
Run the private-name scan and the email and local-path grep from the agent memory over `git diff --name-only origin/master`. Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add docs/reference/configuration.md docs/concepts/skills.md docs/concepts/agent-mode.md docs/concepts/multi-review.md docs/concepts/sandbox.md docs/concepts/failed-reviews.md docs/concepts/security-scan.md docs/troubleshooting.md CLAUDE.md
git commit -m "docs: the output limit, the 40,000-token batch cap and the cut-reply reason"
```
