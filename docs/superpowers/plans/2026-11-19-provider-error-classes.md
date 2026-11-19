# Provider error classes (pergent) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** pergent reads opencode's whole error, sorts a provider error into `quota`, `billing`, `auth` or `rate_limit`, never starts opencode again for the first three, and reports the class in a new `provider_error` result field.

**Architecture:** `internal/runner` keeps the full error event (`responseBody`, `providerID`), a new `provider.go` holds one rule table that classifies it, `Run` skips its own relaunch for the three fallback classes, and `runAttempt` stops opencode at once on such an error event. `internal/result` carries the class per failure and once at the top of the result. pergent.sh (its own plan) decides from the top-level field only.

**Tech Stack:** Go, `internal/runner`, `internal/review`, `internal/result`, `cmd`.

**Spec:** pergent.sh `docs/superpowers/specs/2026-11-19-team-models-and-fallback-design.md`, section "The pergent CLI". Plan B (pergent.sh) is `docs/superpowers/plans/2026-11-19-team-models-and-fallback.md` there. This plan lands and is released first.

## Findings (checked 2026-11-19, opencode `v1.19.4`, the Dockerfile's `OPENCODE_VERSION`)

| Question | Answer | Evidence | Label |
|---|---|---|---|
| Does `opencode run --format json` emit an event on each of its own retries? | **No.** The retry sets `session.status {type:"retry", attempt, message, next}` on the bus, but the run loop only emits `tool_use`, `step_start`, `step_finish`, `text`, `reasoning` and `error`; it reads `session.status` only to stop on `idle`. | `packages/opencode/src/session/processor.ts` L674-686 (`Effect.retry(SessionRetry.policy({ set: status.set(... type: "retry") }))`); `packages/opencode/src/cli/cmd/run.ts` L668-790 (`emit` calls) | confirmed |
| When does the `error` event come? | Once, after the last retry: `halt` publishes `Session.Event.Error`, then sets `idle`, so the run loop ends and opencode exits 1. | `processor.ts` L613-638, `run.ts` L771-790 | confirmed |
| Is a 429 `insufficient_quota` retried? | **Yes over HTTP**: `retryable()` returns a retry when `isRetryable` is true or the message or `responseBody` matches its patterns (`429`, `rate limit`, `resource_exhausted`, ...). For an `openai*` provider `isRetryable` is the AI SDK's own flag (true for 429). **No as a stream error**: `parseStreamError` maps `insufficient_quota` to `isRetryable: false`, message `Quota exceeded. Check your plan and billing details.` | `session/retry.ts` L95-108, `provider/error.ts` L22-28, L117-123, L188 | confirmed (AI SDK's 429 default: from the AI SDK, not in this checkout: likely) |
| Billing (Anthropic 400 credit, 402) and auth (401, 403, `ProviderAuthError`) retried? | No: not retryable and no pattern match, so the error event comes at once. | `retry.ts` L95-108 | confirmed for the patterns; per-provider flag: likely |
| Attempt limit? | **5 retries** (`RETRY_MAX_RETRIES = 5`, `meta.attempt > 5` stops). Wait: 2 s doubling, +0-25 % jitter. With response headers: `retry-after-ms` / `retry-after` if sent (cap ~24.8 days), else exponential with no 30 s cap; without headers, at most 30 s each. The AI SDK's own retries are off (`maxRetries: input.retries ?? 0`). A quota 429 without `retry-after` waits about 62-78 s in opencode before pergent sees it. | `retry.ts` L26-31, L46-82, L193; `session/llm.ts` L323 | confirmed (whether quota 429s carry `retry-after`: unknown) |
| Fields on the error event | `{"name", "data"}`. `APIError.data`: `message`, `statusCode?`, `isRetryable`, `responseHeaders?`, `responseBody?`, `metadata?`. **No `providerID` on `APIError`**; only `ProviderAuthError.data` has `providerID` and `message`. | `packages/core/src/v1/session.ts` L50-64, `session/message-v2.ts` L633-640 | confirmed |
| Config knob to cap or turn off retries | **None.** `RETRY_MAX_RETRIES` is a constant; provider `timeout`/`headerTimeout`/`chunkTimeout` bound one call, not the wait between calls. | `retry.ts`, `provider/provider.ts` L94-125 | confirmed |

**Early-stop mechanism chosen: classify the one error event, stop opencode on it, and never relaunch for `quota`, `billing`, `auth`.**

- A per-retry stream event does not exist, so "stop during opencode's own retries" is not possible from the JSON stream in 1.19.4.
- A short per-call timeout (the spec's fallback) does not help: the 429 arrives fast, and the time goes into the waits between calls. A pergent-side kill during those waits loses the only error event, so pergent.sh would see a `timeout` and never fall back. That breaks the feature.
- No config knob exists.
- What the choice saves: today pergent relaunches a 429 up to 3 times (5 s, 10 s, 20 s), each with opencode's 5 retries: about 5 minutes per batch. After this plan: one opencode run, about 62-78 s for a quota 429, and seconds for billing and auth.
- Left open (parked, needs an owner ruling): a quota 429 with a long `retry-after` keeps opencode waiting until the review budget ends. The run is then a `timeout` with no `provider_error`, so no fallback. The only ways to see each retry are a pergent-written opencode plugin (`event` hook) or parsing `--print-logs` lines, which are not a contract and carry the raw error object.

## Global Constraints

- Work on a new branch `feat/provider-errors` from `origin/master` (`git fetch` first). Local `master` was pulled to `963a330` on 2026-11-19.
- The classes, exact strings: `quota`, `billing`, `auth`, `rate_limit`. Fallback classes: `quota`, `billing`, `auth`.
- The table is strict: an unknown 429 is `rate_limit`, never `quota`. A 5xx, a connection error, `ContextOverflowError` and anything else is no class.
- `rate_limit` and every other transient error keep today's 3 relaunches.
- `responseBody` is read for the class only. It never reaches a `Cause` message, the result, the comment, a log line or a usage file.
- The key env name stays `OPENCODE_API_KEY`. Redaction does not change.
- Without a provider error the result JSON, the CI comment and the render goldens are byte-identical to today. `render/` is not touched.
- `make vet` and `make test` pass. `go test ./render/...` passes with no golden change.
- Commit with `git -c user.email=zufardhiyaulhaq@gmail.com commit`, conventional subject, last line `Co-Authored-By: Claude <model that wrote it> <noreply@anthropic.com>`. Run tests and commits as separate commands. Never push, tag or release unless the owner asks.
- Public repository: before each commit run `git grep -niE 'golabs|gojek|gopay[^t]|goto\b' -- . ':!go.sum' ':!docs/superpowers/plans/*'` plus a `grep` over each new untracked file; both print nothing. Fixture bodies use the public error shapes of each provider, never a real key or account id.
- No container, no `gh`, `glab` or `curl`.

## Review Focus

1. **A body that is not JSON** (an HTML gateway page, plain text, empty): no panic, class from the status alone (a 429 is `rate_limit`). Test in Task 2.
2. **A quota body behind a non-429 status** (a proxy that turns 429 into 500, or a 200 stream error with no status): only the stream-error rule (`statusCode` 0 and `insufficient_quota`) matches; a 500 stays no class. Test in Task 2.
3. **Several error events in one run** (the real error, then opencode's generic `Unexpected server error`): the class comes from `RootError`, the same event the cause uses. Test in Task 3.
4. **A quota error in one batch while another batch passes**: the top-level field is still set, so pergent.sh falls back. A partial review is never approved anyway. Test in Task 4.
5. **A body that echoes the key or prompt text**: it never appears in the result JSON. Test in Task 4.

---

### Task 1: The parser keeps the whole error

**Files:**
- Modify: `internal/runner/parser.go` (`openCodeErrData` ~L71, `EventError` ~L78, the `"error"` case ~L292)
- Create: `internal/runner/testdata/quota_openai.ndjson`, `internal/runner/testdata/billing_anthropic.ndjson`
- Test: `internal/runner/parser_test.go`

**Interfaces:**
- Produces: `EventError.ResponseBody string`, `EventError.ProviderID string` (from `data.providerID`, set only on `ProviderAuthError`). `EventError` stays comparable (`classify` uses `!=`).

- [ ] **Step 1: Write the failing test**

```go
func TestParseOutput_ErrorKeepsBodyAndProvider(t *testing.T) {
	tr := ParseOutput(readFixture(t, "quota_openai.ndjson"))
	e := tr.RootError()
	if e == nil || e.StatusCode != 429 || !strings.Contains(e.ResponseBody, `"insufficient_quota"`) {
		t.Fatalf("got %+v", e)
	}
	tr = ParseOutput(readFixture(t, "auth_error.ndjson"))
	if e := tr.RootError(); e == nil || e.ProviderID != "anthropic" {
		t.Fatalf("got %+v", e)
	}
}
```

`quota_openai.ndjson`: one `step_start` and one `error` event `{"name":"APIError","data":{"message":"You exceeded your current quota, please check your plan and billing details.","statusCode":429,"isRetryable":true,"responseBody":"{\"error\":{\"message\":\"You exceeded your current quota...\",\"type\":\"insufficient_quota\",\"param\":null,\"code\":\"insufficient_quota\"}}"}}`. `billing_anthropic.ndjson`: status 400, body `{"type":"error","error":{"type":"invalid_request_error","message":"Your credit balance is too low to access the Anthropic API. Please go to Plans & Billing to upgrade or purchase credits."}}`, `isRetryable` false.

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/runner/ -run TestParseOutput_ErrorKeepsBodyAndProvider`
Expected: FAIL (unknown fields `ResponseBody`, `ProviderID`).

- [ ] **Step 3: Implement**

Add `ResponseBody string \`json:"responseBody,omitempty"\`` and `ProviderID string \`json:"providerID,omitempty"\`` to `openCodeErrData`, the same two fields to `EventError`, and copy them in the `"error"` case. Update the event comment to say these fields are kept for classification only.

- [ ] **Step 4: Run it to verify it passes**

Run: `go test ./internal/runner/`
Expected: PASS.

- [ ] **Step 5: Commit** `feat(runner): keep the provider's error body and provider ID`

### Task 2: The classifier table

**Files:**
- Create: `internal/runner/provider.go`, `internal/runner/provider_test.go`, `internal/runner/testdata/provider/*.json`

**Interfaces:**
- Produces:
  - `type ProviderClass string` with `ClassNone ProviderClass = ""`, `ClassQuota = "quota"`, `ClassBilling = "billing"`, `ClassAuth = "auth"`, `ClassRateLimit = "rate_limit"`.
  - `func (c ProviderClass) Fallback() bool`: true for quota, billing, auth.
  - `func Classify(e EventError) ProviderClass`.

- [ ] **Step 1: Write the failing tests**

One fixture per row, each `{"name","statusCode","message","responseBody"}`, and one table test `TestClassify` that loads them all and asserts the class. A second test `TestClassify_RuleOrder` asserts auth wins over a 429 body that also says quota.

| Fixture | name / status | body (shape) | Class |
|---|---|---|---|
| `openai_quota_429.json` | APIError / 429 | `error.code` and `error.type` `insufficient_quota` | quota |
| `openai_quota_stream.json` | APIError / 0 | `{"type":"error","error":{"code":"insufficient_quota"}}`, message `Quota exceeded. Check your plan and billing details.` | quota |
| `openai_rate_limit_429.json` | APIError / 429 | `error.type` `requests`, `code` `rate_limit_exceeded` | rate_limit |
| `openai_invalid_key_401.json` | APIError / 401 | `error.code` `invalid_api_key` | auth |
| `anthropic_credit_400.json` | APIError / 400 | `credit balance is too low` | billing |
| `anthropic_rate_limit_429.json` | APIError / 429 | `error.type` `rate_limit_error` | rate_limit |
| `anthropic_auth_401.json` | APIError / 401 | `authentication_error` | auth |
| `gemini_quota_day_429.json` | APIError / 429 | `error.status` `RESOURCE_EXHAUSTED`, a `google.rpc.QuotaFailure` violation with `quotaId` `GenerateRequestsPerDayPerProjectPerModel-FreeTier` | quota |
| `gemini_quota_minute_429.json` | APIError / 429 | same, `quotaId` `...PerMinute...` | rate_limit |
| `gemini_denied_403.json` | APIError / 403 | `PERMISSION_DENIED` | auth |
| `openrouter_credits_402.json` | APIError / 402 | `Insufficient credits` | billing |
| `deepseek_balance_402.json` | APIError / 402 | `Insufficient Balance` | billing |
| `provider_auth_error.json` | ProviderAuthError / 0 | none | auth |
| `server_503.json` | APIError / 503 | `overloaded_error` | none |
| `unknown_429_html.json` | APIError / 429 | an HTML page | rate_limit |
| `quota_body_500.json` | APIError / 500 | `insufficient_quota` | none |
| `context_overflow.json` | ContextOverflowError / 413 | none | none |

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/runner/ -run TestClassify`
Expected: FAIL (undefined `Classify`).

- [ ] **Step 3: Implement**

`provider.go` holds one ordered table `var providerRules = []providerRule{...}`, `type providerRule struct{ name string; class ProviderClass; match func(e EventError, b providerBody) bool }`. `providerBody` is the body decoded once, leniently (a body that does not decode is the zero value): `error.code`, `error.type`, `error.status`, `error.message` (as strings, any non-string ignored) and the `quotaId` values of `error.details[].violations[]`. Rules, first match wins:

1. auth: `Name == "ProviderAuthError"`, or status 401 or 403.
2. billing: status 402.
3. billing: status 400 and message or `error.message` matches `(?i)credit balance is too low`.
4. quota: status 429 and `error.code` or `error.type` is `insufficient_quota`.
5. quota: status 429, `error.status` is `RESOURCE_EXHAUSTED`, and some `quotaId` contains `PerDay`.
6. quota: status 0, `Name == "APIError"`, `error.code` is `insufficient_quota` (opencode's stream-error path).
7. rate_limit: status 429.

Doc comment on the table: the list is strict on purpose; add a provider by adding a row and a fixture.

- [ ] **Step 4: Run them to verify they pass**

Run: `go test ./internal/runner/`
Expected: PASS.

- [ ] **Step 5: Commit** `feat(runner): classify provider errors as quota, billing, auth or rate limit`

### Task 3: No relaunch, and stop on the first fallback-class error

**Files:**
- Modify: `internal/runner/runner.go` (`Run` retry condition ~L190, `runAttempt` progress hook ~L240), `internal/runner/progress.go` (`progressWriter.onError`), `internal/runner/outcome.go` (`Cause`, `classify`)
- Test: `internal/runner/runner_test.go`, `internal/runner/outcome_test.go`

**Interfaces:**
- Consumes: `Classify`, `EventError.ProviderID` (Tasks 1-2).
- Produces: `Cause.ProviderClass ProviderClass`, `Cause.ProviderStatus int`, `Cause.Provider string` (`EventError.ProviderID`, "" when unknown). `progressWriter.onError func(EventError)`.

- [ ] **Step 1: Write the failing tests**

```go
func TestRun_NoRelaunchForQuota(t *testing.T)   // fakeOpencode(quota_openai code 1, completed): failed, runCount 1, Cause.ProviderClass == ClassQuota, Cause.Attempts == 1
func TestRun_NoRelaunchForBilling(t *testing.T) // billing_anthropic: runCount 1, ClassBilling
func TestRun_NoRelaunchForAuth(t *testing.T)    // auth_error.ndjson: runCount 1, ClassAuth
func TestRun_RateLimitStillRelaunched(t *testing.T) // rate_limited x2 then completed: completed, runCount 3 (today's test, plus the class of a failed run is ClassRateLimit)
func TestRun_StopsOnQuotaError(t *testing.T) {
	// The fake prints the quota error, then hangs as if opencode kept waiting.
	count := fakeOpencode(t, fakeAttempt{fixture: "quota_openai.ndjson", sleep: "60"})
	start := time.Now()
	got := Run(context.Background(), testOptions(2*time.Minute))
	if time.Since(start) > 20*time.Second { t.Fatalf("took %s, want an early stop", time.Since(start)) }
	if got.Cause == nil || got.Cause.Kind != CauseOpencodeError || got.Cause.ProviderClass != ClassQuota { t.Fatalf("%+v", got.Cause) }
	if n := runCount(t, count); n != 1 { t.Fatalf("ran %d times", n) }
}
func TestClassify_ProviderClassFromRootError(t *testing.T) // quota error then "UnknownError: Unexpected server error": class quota
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/runner/ -run 'NoRelaunch|RateLimitStill|StopsOnQuota|ProviderClassFromRoot'`
Expected: FAIL.

- [ ] **Step 3: Implement**

- `progressWriter`: call `onError(e)` for each parsed `error` event (the writer already decodes events in `formatEvent`).
- `runAttempt`: set `pw.onError` to log `  Stopping opencode: the provider reported a %s error` (class only, never the message) and `stop()` when `Classify(e).Fallback()`. Same shape as the step-limit hook.
- `classify`: fill `ProviderClass`, `ProviderStatus`, `Provider` from `RootError()` for `CauseOpencodeError`. The stop does not end the caller's `ctx`, so it is never `cancelled` or `timeout`.
- `Run`: add `&& !Classify(*e).Fallback()` to the retry condition, with a comment that pergent.sh moves to another model for these, and opencode already waited.

- [ ] **Step 4: Run them to verify they pass**

Run: `go test ./internal/runner/`
Expected: PASS, and the existing retry tests are unchanged.

- [ ] **Step 5: Commit** `feat(runner): stop opencode and skip the relaunch on quota, billing and auth errors`

### Task 4: The `provider_error` result field

**Files:**
- Modify: `internal/result/result.go` (`Result`, `Cause`, `Meta`, `Build`, `buildSkill`), `cmd/agent.go` (where `result.Meta` is built: provider and model from `OPENCODE_PROVIDER` and `OPENCODE_MODEL`, "" when unset)
- Test: `internal/result/result_test.go`, `cmd/agent_test.go`

**Interfaces:**
- Consumes: `Cause.ProviderClass`, `ProviderStatus`, `Provider` (Task 3).
- Produces:

```go
// ProviderError is a classified provider error: Class is quota, billing,
// auth or rate_limit. Status is the HTTP status (0 when opencode gave none).
type ProviderError struct {
	Class    string `json:"class"`
	Status   int    `json:"status,omitempty"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}
// Result.ProviderError *ProviderError `json:"provider_error,omitempty"`
// Cause.ProviderError  *ProviderError `json:"provider_error,omitempty"`
// Meta.Provider, Meta.Model string
```

- [ ] **Step 1: Write the failing tests**

```go
func TestBuild_ProviderErrorOnFailureAndTop(t *testing.T)  // one quota failure: failures[0].cause.provider_error == top == {quota, 429, "openai", "gpt-x"}
func TestBuild_ProviderErrorPriority(t *testing.T)         // failures auth, quota, rate_limit across 2 skills: top class "auth"; order auth > billing > quota > rate_limit
func TestBuild_ProviderErrorRateLimitOnly(t *testing.T)    // top class "rate_limit"
func TestBuild_NoProviderError(t *testing.T)               // JSON has no "provider_error" key at all
func TestBuild_ProviderErrorAmidPasses(t *testing.T)       // skill A passes, skill B batch fails on quota: top is quota
func TestBuild_ProviderBodyNeverInResult(t *testing.T)     // Cause from an EventError whose body holds "BODY-MARKER-123": the marshalled result has no "BODY-MARKER-123"
func TestAgent_MetaProviderAndModel(t *testing.T)          // with OPENCODE_PROVIDER/OPENCODE_MODEL set, Meta carries them
```

Provider in the field: `Meta.Provider` when set, otherwise `Cause.Provider`.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/result/ ./cmd/ -run 'ProviderError|ProviderBody|MetaProvider'`
Expected: FAIL.

- [ ] **Step 3: Implement**

`buildSkill` sets `fail.Cause.ProviderError` when `f.Cause.ProviderClass != ""`. `Build` picks the top-level one by the priority above, first in skill and batch order on a tie. An error result (`status: "error"`) has none.

- [ ] **Step 4: Run them to verify they pass**

Run: `go test ./internal/result/ ./cmd/`
Expected: PASS.

- [ ] **Step 5: Commit** `feat(result): report the provider error class in provider_error`

### Task 5: Docs and the gate

**Files:**
- Modify: `CLAUDE.md` (the `internal/runner/` bullet: "classifies provider errors"; the **Retries** bullet), `docs/concepts/failed-reviews.md` L15, `docs/agent-result-schema.md` (top-level field table and `failures[].cause`), `docs/troubleshooting.md` (one line: a quota, billing or auth error is not retried)

- [ ] **Step 1: Write the docs**

The **Retries** bullet in `CLAUDE.md` becomes, in substance: a run that fails on a transient provider error (429, 5xx, connection) is started again up to 3 times with backoff, within the review budget, **except** quota, billing and auth errors (`provider.go`), which stop opencode and are never started again. opencode 1.19.4 first retries what it thinks is retryable up to 5 times (2 s doubling, the provider's `retry-after` when sent); those retries are not in `--format json`, so pergent sees one error event after the last.

`agent-result-schema.md`: `provider_error` (object, optional, additive, `schema_version` stays 1): `class` (`quota`, `billing`, `auth`, `rate_limit`), `status`, `provider`, `model`. Top level: the most severe one in the result (auth, billing, quota, rate_limit). New in the next pergent release (no version number here).

- [ ] **Step 2: Run the gate**

Run, as separate commands: `make vet`, `make test`, `go test ./render/...`, then the private-name scan.
Expected: all pass, the scan prints nothing, `git status` shows no golden change.

- [ ] **Step 3: Commit** `docs: provider error classes and the retry rules`

## Self-review notes

- Spec "Read the whole error": Task 1. "Classify" with a test per provider: Task 2. "Stop early" and "does not start opencode again": Task 3, with the mechanism argued in Findings. "A structured result field": Task 4. "The key env name stays": Global Constraints. CLAUDE.md retries note: Task 5.
- `providerID` is only on `ProviderAuthError`, so `provider` in the field comes from pergent's own `OPENCODE_PROVIDER` first. This is a fact, not a ruling change.
