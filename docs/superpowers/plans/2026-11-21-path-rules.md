# Path rules, PR 1 (pergent) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** pergent matches a rule to each changed file of a batch, gives the batch run and the verification run the rules of their files, counts rule text in batch sizing, and reports the rule per file in the result and the comment. The built-in list ships empty.

**Architecture:** A new public package `rule/` (`github.com/gopaytech/pergent/rule`, like `scope/` and `graph/`) holds the rule file format, the loader, the resolver, the hash and the (empty) built-ins. pergent.sh imports it to check rule sets with the same loader. `internal/review` resolves files per batch and writes `file-rules.md`. Agent mode takes the list from `--rules`/`$PERGENT_RULES`. CI mode reads `.pergent/rules.yaml` from the base branch.

**Tech Stack:** Go 1.24, `gopkg.in/yaml.v3`, `scope`, `internal/{skill,diff,review,result,output,config}`, `render`, `cmd`.

**Spec:** pergent.sh `docs/superpowers/specs/2026-11-20-path-rules-design.md` (merged, PR gopaytech/pergent.sh#73). Read it in full, with its anti-goals. The pergent.sh plan is pergent.sh `docs/superpowers/plans/2026-11-21-path-rules.md`.

## Order across the repositories

1. This plan merges to pergent `master` first.
2. pergent.sh's plan then moves its `go.mod` pin to that merge commit (`go get github.com/gopaytech/pergent@<merge sha>`, a pseudo-version, no `replace`). Only `rule/` and `render/` changes reach pergent.sh this way. The rest reaches it through a pergent image, which is a release decision for the owner, not part of this plan.

## One change from the spec's text

The spec names the package `internal/rule` and the folder `internal/rule/builtin/`. Go does not let pergent.sh import an `internal/` package of another module, and pergent.sh must check rule sets with the same loader. So the package is the public `rule/` and the folder `rule/builtin/`. Like `scope/`, it imports no pergent `internal/` package, and its error strings are a contract.

## Checked facts (origin/master `28037b6`, 2026-11-21)

| Question | Answer | Where |
|---|---|---|
| How batch sizing counts tokens | `Batch(files, budget)` sorts by path, puts a file over budget in `oversize` (line 36), groups files by directory and sums `EstimateTokens(f.Patch)` (line 48), then packs groups with `place` (54-61). It knows only patches. | `internal/diff/batch.go:24-75` |
| Where the batch budget is set | `reviewSkill`: `budget := diff.BatchBudget(...)`, CI results shrink `packBudget` (453-455), `diff.Batch(reviewable, packBudget)` (456), a file over the reduced budget that fits the full one gets its own batch (457-469), the oversize message (471-477). | `internal/review/review.go:448-477` |
| Where a batch run's attachments are built | `reviewBatch`: `diff.patch`, `changed-files.md`, previous findings, description, CI results, rejected findings, graph. A write failure in the attachment loop is `notReviewed`. | `internal/review/review.go:751-873` |
| Where the verification run's attachments are built | `verify`: `blocking-findings.json` (161-165), the diffs note (174), the deleted list (179-187), the deleted patch (190-200), the rejected findings (201-214), then `runWithPrompt` (216). Helper write failures add a note; a findings-file failure fails verification and keeps every finding. | `internal/review/verify.go:150-216` |
| Test hooks for write failures | Package vars `writeDeleted`, `writeDeletedPatch`, `writeRejected`, `writeGraphPack`, `writeDiff` (all `os.WriteFile`). | `internal/review/deleted.go:79,83`, `review.go:727`, `graphpack.go:22`, `diffs.go:39` |
| Skill frontmatter | `frontmatter{Name, Paths, Exclude, CrossBatch}` decoded with plain `yaml.Unmarshal`, so a typed bool takes `yes`. | `internal/skill/loader.go:33-38,172-207` |
| Bool flags with env | `resolveBool(value, set, "PERGENT_CHECK_DELETED", false)`; a bad env value is a config error. Agent mode uses `config.ResolveAgent`. | `internal/config/resolver.go:221`, `cmd/agent.go:433-440` |
| Trusted agent inputs | `trustedInputs` lists each trusted path; `trustedInputsOutside` refuses one inside `--repo-path`. Inputs are read after that check. | `cmd/agent.go:585-655`, `cmd/agent.go:521-536` |
| CI-mode base branch | `cfg.GitHub.BaseBranch` / `cfg.GitLab.BaseBranch` (platform), `cfg.BaseBranch` (`--local`, default `main`); the diff uses `origin/<branch>`. | `cmd/diff.go:11-37`, `cmd/main.go:146-147` |
| Comment Files block | `writeFiles` writes `<details><summary>Files (N)</summary>`, the list, `</details>`; nothing with `Short`. | `render/details.go:504-520` |
| Result files | `Files.Reviewed` is a plain `[]string`. There is no per-file object for a reviewed file. `Deleted`/`Renamed` are `omitempty` additions. `redactSkill` rebuilds `Files`. | `internal/result/result.go:118-141,458-530` |
| Review panel | Not built: no `--panel`, no judge runs in pergent, no panel plan. | `git grep -i panel` on `origin/master` |

## Global Constraints

- Branch `feat/path-rules` from `origin/master` (`git fetch` first). No release, tag, version bump or version number in any file.
- Limits, for team rules and built-ins alike: **16,000 characters per rule** (runes), **100 entries** and **64 KB** (65,536 bytes) per file.
- `path` uses `scope` syntax and `scope.CheckPattern`. Unknown fields are an error. An entry has exactly one of `rule` and `off: true`.
- Order: repository entries, then dashboard sets in the repository's order, then built-ins (left out when off). The first match wins. An `off` match ends the search with no rule.
- Batch message sentence, verbatim: `file-rules.md lists extra checks from the project for some files in this batch. Use them while you review those files. They do not change what is blocking: only the skill's Blocking section does.`
- Verification sentence, verbatim: `These are project conventions, given as context. They are not blocking criteria.`
- Result labels: `repo:<glob>`, `dashboard:<set>:<glob>`, `built-in:<name>`. Comment line: `Rules: built-in:go (12 files) · repo internal/api/**/*.go (3 files)`, globs in code spans.
- The cross-batch run, the impact check and repair runs get no rules. A skill with `rules: false` gets none in any run.
- A batch with no rule is byte for byte today's batch: no file, no sentence. A review with no rule at all gives today's comment.
- Rules never change a verdict: nothing in `verdict()` or severity reads them.
- Every new result field is copied in `redactSkill` and tested through `Encode` (memory: `redactSkill` rebuilds structs).
- Commit with the owner's personal address (`git -c user.email="$(git -C <main checkout> config --local user.email)" commit`), conventional subjects, and the `Co-Authored-By` line of the model that wrote the change. Never write the address or a local path into a file.

## Review Focus

1. **A change edits its own rules.** Agent mode must ignore `.pergent/rules.yaml` in the checkout; CI mode must read the base branch, never the head. Tests: `TestAgentIgnoresCheckoutRulesFile` (Task 10), `TestCIRulesFromBaseNotHead` (Task 11).
2. **A rules file that turns a skill off silently.** A malformed `--rules` must give a `failed` usage result, never a review without rules. Test: `TestAgentRulesMalformedIsUsageError` (Task 10).
3. **One file's rule pushes a batch over the context.** A file whose diff fits but whose diff plus rule does not must be oversize, also in the "fits the full budget" CI-results path. Tests: `TestBatchWithRuleMakesFileOversize` (Task 4), `TestRuleCountsInFullBudgetFallback` (Task 5).
4. **A path with a newline or a backtick in `file-rules.md` or the comment line.** Paths are quoted with `strconv.Quote` in the file and escaped with `codeSpan` in the comment. Tests: `TestFileRulesQuotesPaths` (Task 6), `TestRulesLineEscapesGlob` (Task 9).
5. **A YAML bool that is not a bool.** `rules: yes`, `rules: "false"` and `off: yes` must be errors, since yaml.v3 turns `yes` into `true` for a typed bool. Tests: `TestSkillRulesNotBool` (Task 3), `TestLoadOffNotBool` (Task 1).

---

### Task 1: `rule` package: format and loader

**Files:**
- Create: `rule/rule.go`, `rule/rule_test.go`

**Interfaces:**
- Produces:
  - `const MaxRuleChars = 16000; MaxEntries = 100; MaxFileBytes = 64 << 10; MaxListBytes = 1 << 20; RepoFile = ".pergent/rules.yaml"; SourceRepo = "repo"; SourceBuiltIn = "built-in"`
  - `type Entry struct { Path string \`json:"path"\`; Rule string \`json:"rule,omitempty"\`; Off bool \`json:"off,omitempty"\`; Source string \`json:"source"\`; Name string \`json:"-"\` }` (`Name` is a built-in's name, `""` for team entries)
  - `func DashboardSource(set string) string` → `"dashboard:" + set`
  - `func Load(data []byte, source string) ([]Entry, error)`: one rule file; every entry gets `Source: source`
  - `func EncodeList(entries []Entry) ([]byte, error)` and `func DecodeList(data []byte) ([]Entry, error)`: the agent input, a JSON array of team entries with `source`

- [ ] **Step 1: Write the failing tests** in `rule/rule_test.go`:
  - `TestLoadEntries`: the spec's example file gives 2 entries, `{Path: "internal/api/**/*.go", Rule: "Every handler ...\n", Source: "repo"}` and `{Path: "**/*.pb.go", Off: true, Source: "repo"}`. The same file as JSON gives the same entries.
  - `TestLoadErrors`, table-driven, each error must equal the want string exactly:
    - unknown field `paths:` in entry 2 → `.pergent/rules.yaml: entry 2: unknown field "paths"`
    - unknown top-level key → `.pergent/rules.yaml: unknown field "rule"`
    - both `rule` and `off: true` → `.pergent/rules.yaml: entry 1: has both rule and off: true; use one`
    - neither, or `off: false` with no rule, or a blank rule → `.pergent/rules.yaml: entry 1: needs a rule or off: true`
    - `path: "api/"` → `.pergent/rules.yaml: entry 3: path: pattern "api/" can never match a file: for a directory use "api/**"` (the `scope.CheckPattern` text after `path: `)
    - missing `path` → `.pergent/rules.yaml: entry 1: path: pattern is empty`
    - a rule of 16,001 runes (multi-byte) → `.pergent/rules.yaml: entry 1: rule is 16001 characters, over the limit of 16000`; 16,000 runes loads
    - 101 entries → `.pergent/rules.yaml: 101 entries, over the limit of 100`; 100 loads
    - 65,537 bytes → `.pergent/rules.yaml: 65537 bytes, over the limit of 65536 (64 KB)`; the size check runs before parsing
    - source `dashboard:go-api` names the set: `rule set "go-api": entry 1: ...`
  - `TestLoadOffNotBool`: `off: yes` and `off: "true"` → `entry 1: off must be true or false`.
  - `TestLoadEmpty`: empty data, `rules:` and `rules: []` give no entries and no error.
  - `TestListRoundTrip`: `EncodeList` then `DecodeList` gives the same entries, with sources `repo` and `dashboard:a`. `DecodeList` refuses a source `built-in`, a source `dashboard:` with a bad set name (`^[A-Za-z0-9][A-Za-z0-9._-]*$`), more than `MaxEntries` entries of one source, and data over `MaxListBytes`, naming the entry (`rules list: entry 4: ...`). `EncodeList` refuses a built-in entry.

- [ ] **Step 2: Run and see them fail** — `go test ./rule/ -run 'TestLoad|TestList' -count=1`. Expected: build failure, package `rule` has no `Load`.

- [ ] **Step 3: Implement** in `rule/rule.go`. Package doc says it imports no pergent `internal/` package and that pergent.sh imports it. Decode with `yaml.NewDecoder` + `KnownFields(true)` into `struct{ Rules []yaml.Node }`, then each node with `KnownFields(true)` into `{Path string; Rule string; Off yaml.Node}`. `off` is accepted only when its node tag is `!!bool`. The error prefix comes from the source: `repo` → `.pergent/rules.yaml`, `dashboard:<set>` → `rule set "<set>"`, `built-in` → `builtin.yaml`, the list → `rules list`. Rewrite yaml.v3's `field X not found in type ...` to `unknown field "X"`. Errors never quote rule text.

- [ ] **Step 4: Run** — same command. Expected: PASS.

- [ ] **Step 5: Commit** — `git add rule/ && git commit -m "feat(rule): add the path rule file format and loader"`

### Task 2: `rule` package: resolver, labels, hash, built-ins, hides

**Files:**
- Create: `rule/resolve.go`, `rule/builtin.go`, `rule/builtin/builtin.yaml`, `rule/resolve_test.go`, `rule/builtin_test.go`

**Interfaces:**
- Consumes: Task 1's `Entry`, `Load`.
- Produces:
  - `func (e Entry) Label() string`: `repo:<glob>`, `dashboard:<set>:<glob>`, `built-in:<name>`
  - `func (e Entry) Heading() string`: `repo · <glob>`, `dashboard:<set> · <glob>`, `built-in:<name>`
  - `func Builtin() []Entry`: the embedded list, `Source: "built-in"`, `Name` from `rule_file` without `.md`
  - `type Resolver struct{ ... }`; `func NewResolver(team []Entry, builtIn bool) *Resolver`; `func (r *Resolver) Resolve(path string) (Entry, bool)` (false for no match and for an `off` match; a nil `*Resolver` resolves nothing); `func (r *Resolver) Entries() int` (team plus built-ins in use); `func (r *Resolver) BuiltIn() bool`
  - `func Hash(entries []Entry) string`: lowercase hex SHA-256
  - `func Hides(e Entry, builtins []Entry) []string`: names of built-ins whose files `e` may also match

- [ ] **Step 1: Write the failing tests:**
  - `TestResolveFirstMatchAcrossLayers` (uses an unexported `newResolver(team, builtins []Entry)` so it can pass a test built-in list): repo `internal/api/**/*.go` beats dashboard `**/*.go` beats built-in `*.go`; `internal/store/x.go` gets the dashboard entry; with built-ins off a `.go` file outside both gets nothing.
  - `TestResolveOffEndsSearch`: repo `**/*.pb.go` `off: true` then built-in `*.go`: `a/b.pb.go` resolves to `(Entry{}, false)`.
  - `TestResolveNil`: `(*Resolver)(nil).Resolve("a.go")` is false; `Entries()` is 0.
  - `TestLabelsAndHeadings`: the 3 label and heading forms above, exact strings.
  - `TestHashStable`: the same list gives the same hash twice; changing `Rule`, `Path`, `Off`, `Source` or the order changes it; `Hash(nil) == Hash([]Entry{})`.
  - `TestHides`: `**` hides `*.go`; `internal/**/*.go` hides `*.go`; `**/*.pb.go` hides `**/*.go`; `*.ts` does not hide `*.go`; `docs/**` hides a built-in `*.tf` (its witness `docs/x.tf` matches `*.tf`: assert `[]string{"tf"}`). The test documents that the check is a heuristic that may miss an overlap and never reports one that cannot happen.
  - `TestBuiltinLoads` (`builtin_test.go`): `Builtin()` returns 0 entries in PR 1 without a panic; `TestBuiltinTextsHaveNoBlocking`: no built-in `Rule` contains `blocking` (case-insensitive); `TestBuiltinTextsWithinLimits`: each is at most `MaxRuleChars`.

- [ ] **Step 2: Run** — `go test ./rule/ -count=1`. Expected: build failure.

- [ ] **Step 3: Implement.** `builtin.yaml` holds `rules: []` and a comment line that says the texts come in a later change. `builtin.go` embeds with `//go:embed builtin` (a `builtin/*.md` pattern fails to compile while no `.md` exists), parses `rules: [{path, rule_file}]` with `KnownFields(true)`, reads each `builtin/<rule_file>`, checks the Task 1 limits, and panics on error once at init (the tests catch it). `Hash` writes each entry as one JSON line `[source, name, path, rule, off]` into SHA-256. `Hides` builds witness paths for each pattern (`*` and `?` become `x`, a `[...]` class its first character, a `**` segment both removed and `x`) and reports an overlap when a witness of either pattern matches the other with `scope.Match`.

- [ ] **Step 4: Run** — same command, then `go vet ./rule/` and `go list -deps ./rule/ | grep gopaytech/pergent/internal` (must print nothing). Expected: PASS, no output.

- [ ] **Step 5: Commit** — `git commit -m "feat(rule): add the resolver, labels, hash and an empty built-in list"`

### Task 3: Skill frontmatter `rules`

**Files:**
- Modify: `internal/skill/loader.go:24-38,172-207`
- Test: `internal/skill/loader_test.go`

**Interfaces:**
- Produces: `Skill.NoRules bool` (true only for `rules: false`). The zero value means rules on, so every `skill.Skill{}` literal in tests keeps the spec's default.

- [ ] **Step 1: Write the failing tests:** `TestSkillRulesDefaultOn` (no field → `NoRules == false`); `TestSkillRulesFalse` (`rules: false` → `NoRules == true`; `rules: true` → false); `TestSkillRulesNotBool` (`rules: yes`, `rules: "false"`, `rules: 1`, `rules: [a]` → error `rules must be true or false`); the preset `code-review` still loads with `NoRules == false`.
- [ ] **Step 2: Run** — `go test ./internal/skill/ -run TestSkillRules -count=1`. Expected: FAIL (`NoRules` undefined).
- [ ] **Step 3: Implement:** frontmatter field `Rules yaml.Node \`yaml:"rules"\``; accept only tag `!!bool`; `NoRules = value == "false"`. Update the `internal/skill/` line in `CLAUDE.md` (frontmatter list adds `rules`).
- [ ] **Step 4: Run** — `go test ./internal/skill/ -count=1`. Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "feat(skill): read the rules frontmatter field"`

### Task 4: Batch sizing counts rule text

**Files:**
- Modify: `internal/diff/batch.go:19-75`
- Test: `internal/diff/batch_test.go`

**Interfaces:**
- Produces: `type Extra func(path string) (key string, tokens int)` and `func BatchWith(files []FileDiff, budget int, extra Extra) (batches [][]FileDiff, oversize []FileDiff)`. `Batch(files, budget)` becomes `BatchWith(files, budget, nil)`. A key `""` adds nothing. Each distinct key counts its tokens once per batch.

- [ ] **Step 1: Write the failing tests:**
  - `TestBatchWithNilIsBatch`: for the existing `Batch` cases, `BatchWith(..., nil)` gives the same output.
  - `TestBatchWithCountsRuleOncePerBatch`: 3 files of 100 tokens each in one dir, all with key `go` of 200 tokens, budget 500 → one batch (300 + 200).
  - `TestBatchWithSplitsWhenRuleWouldOverflow`: files a (key `go`, 200) and b (key `py`, 200), 100 tokens each, other dirs, budget 500 → two batches.
  - `TestBatchWithRuleMakesFileOversize`: one file of 300 tokens with a 300-token rule, budget 500 → oversize, no batch.
  - `TestBatchWithOrderIndependent`: shuffled input gives the same output.
- [ ] **Step 2: Run** — `go test ./internal/diff/ -run TestBatchWith -count=1`. Expected: build failure.
- [ ] **Step 3: Implement.** The oversize test is patch plus the file's own rule. A directory group's cost is its patches plus its distinct keys. `place` adds patches plus the keys not yet in the current batch, and resets the key set when it starts a batch.
- [ ] **Step 4: Run** — `go test ./internal/diff/ -count=1`. Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "feat(diff): count extra per-file text in batch sizing"`

### Task 5: Review request takes rules; sizing uses them

**Files:**
- Modify: `internal/review/review.go:44-100` (Request), `:390-477` (reviewSkill sizing)
- Create: `internal/review/rules.go`
- Test: `internal/review/rules_test.go`

**Interfaces:**
- Consumes: `rule.Resolver`, `rule.Entry`, `diff.BatchWith`.
- Produces: `Request.Rules *rule.Resolver` (nil: no rule support, today's review); `func (r *reviewer) ruleFor(s skill.Skill, path string) (rule.Entry, bool)` (false when `s.NoRules`); `func (r *reviewer) ruleExtra(s skill.Skill) diff.Extra` (key `Label()`, tokens `diff.EstimateTokens(entry.Rule)`; nil when the skill has no rules or the request none).

- [ ] **Step 1: Write the failing tests** (fake `Run`, as in `review_test.go`):
  - `TestRuleCountsInBatchSizing`: `ContextLimit` 4000 (budget 1000); two files of about 400 tokens each in one dir, both matched by a repo rule of about 400 tokens → 2 batches; with `NoRules` → 1 batch.
  - `TestRuleOversizeMessage`: a file of 600 tokens with a 600-token rule → failure `Not reviewed: \`a.go\` — its diff and rule are about 1,200 tokens, larger than the per-batch limit of 1,000.`; without a rule the message is today's.
  - `TestRuleCountsInFullBudgetFallback`: with CI results that shrink `packBudget`, a file whose patch alone fits the full budget but patch plus rule does not stays oversize.
- [ ] **Step 2: Run** — `go test ./internal/review/ -run TestRule -count=1`. Expected: build failure.
- [ ] **Step 3: Implement.** Call `diff.BatchWith(reviewable, packBudget, r.ruleExtra(s))`; the fallback at `review.go:457-469` compares patch plus rule with `budget`.
- [ ] **Step 4: Run** — `go test ./internal/review/ -count=1`. Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "feat(review): count rule text in batch sizing"`

### Task 6: `file-rules.md` in batch runs, and the rule per reviewed file

**Files:**
- Modify: `internal/review/rules.go`, `internal/review/review.go:127-168` (SkillResult), `:364` (batchOutcome), `:540-585` (collect), `:751-873` (reviewBatch)
- Test: `internal/review/rules_test.go`

**Interfaces:**
- Produces:
  - `const rulesFile = "file-rules.md"`, `rulesMessage` (the batch sentence, verbatim), `var writeRules = os.WriteFile`
  - `func fileRules(paths []string, resolve func(string) (rule.Entry, bool)) (text string, labels map[string]string)`: one `## Rule: <Heading()>` section per entry in order of first use, `Files: ` with `strconv.Quote`d paths joined by `, `, a blank line, the rule text; `""` when no path has a rule
  - `type FileRule struct{ Path, Rule string }`; `SkillResult.FileRules []FileRule`: for each path in `Reviewed` that had a rule, in `Reviewed` order

- [ ] **Step 1: Write the failing tests:**
  - `TestBatchFileRulesSections`: a batch with `internal/api/order.go`, `internal/api/user.go` (repo rule) and `internal/store/cache.go` (dashboard rule) gets `file-rules.md` exactly: `## Rule: repo · internal/api/**/*.go\nFiles: "internal/api/order.go", "internal/api/user.go"\n\n<text>\n\n## Rule: dashboard:base · **/*.go\nFiles: "internal/store/cache.go"\n\n<text>\n`, and the message ends with the rules sentence before the previous-findings sentence.
  - `TestBatchNoRuleNoFile`: a batch with no matching file gets no `file-rules.md`, and its message equals the message without `Request.Rules`.
  - `TestSkillNoRulesGetsNoFile`: `NoRules` skill, matching files → no file, no sentence, no `FileRules`.
  - `TestRulesWriteFailureNotReviewed`: `writeRules` fails → the batch's files are a failure `Not reviewed: could not write the file rules: disk full`, the skill is `failed`, and no run starts for that batch.
  - `TestFileRulesQuotesPaths`: a path with a newline and a backtick is written quoted on one `Files:` line.
  - `TestSkillResultFileRules`: two batches, one fails → `FileRules` lists only files of the finished batch, in `Reviewed` order, with labels `repo:internal/api/**/*.go`.
  - `TestCrossBatchGetsNoRules` and `TestImpactGetsNoRules`: the cross-batch run and the impact run get no `file-rules.md` and no rules sentence. `TestRepairGetsNoRules`: the repair run's files are only `review-answer.md`.
  - `TestRulesNeverChangeVerdict`: the same answer with and without rules gives the same verdict and severities.
- [ ] **Step 2: Run** — `go test ./internal/review/ -run 'Rules|FileRules|NoRule' -count=1`. Expected: FAIL.
- [ ] **Step 3: Implement.** In `reviewBatch`, after `changed-files.md`, build the text; when not empty, write it with `writeRules` into the batch dir (failure: `notReviewed("could not write the file rules: %v", err)`), add it after `changed-files.md` in `files`, and add `rulesMessage` after the deleted and renamed sentences. Pass the labels back on `batchOutcome` and fill `res.FileRules` in the collect loop.
- [ ] **Step 4: Run** — `go test ./internal/review/ -count=1`. Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "feat(review): give each batch run the rules of its files"`

### Task 7: Rules in the verification run

**Files:**
- Modify: `internal/review/verify.go:24-38,201-216`
- Test: `internal/review/verify_test.go`

**Interfaces:**
- Consumes: `fileRules`, `writeRules`, `r.ruleFor`.
- Produces: `verifyRulesMessage = "Also attached, as " + rulesFile + ", are the rules of the files these findings are on. These are project conventions, given as context. They are not blocking criteria."`

- [ ] **Step 1: Write the failing tests:**
  - `TestVerifyGetsRulesOfFindingFiles`: blocking findings on `a.go` (rule) and `b.py` (no rule) → the verify run gets `file-rules.md` with one section listing `"a.go"`, and the message ends with `verifyRulesMessage`.
  - `TestVerifyNoRuleNoFile`: findings on files with no rule → files and message are today's.
  - `TestVerifyRulesWriteFailureKeepsFindings`: `writeRules` fails → note `Verification failed: could not write the file rules: disk full`, no verify run, every blocking finding kept.
  - `TestVerifyNoRulesSkill`: `NoRules` → no file.
- [ ] **Step 2: Run** — `go test ./internal/review/ -run TestVerify -count=1`. Expected: FAIL.
- [ ] **Step 3: Implement** after the rejected-findings block (`verify.go:214`), with the findings' distinct `File` values in finding order. Failing closed is deliberate: running without the rules could drop a finding that rests on a project convention.
- [ ] **Step 4: Run** — `go test ./internal/review/ -count=1`. Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "feat(review): give the verification run the rules of its findings"`

### Task 8: Result fields

**Files:**
- Modify: `internal/result/result.go:41-57,119-130,198-235,288-342,458-543`
- Modify: `docs/agent-result-schema.md`
- Test: `internal/result/result_test.go`

**Interfaces:**
- Consumes: `review.SkillResult.FileRules`.
- Produces: `type FileRule struct{ Path string \`json:"path"\`; Rule string \`json:"rule"\` }`; `Files.Rules []FileRule \`json:"rules,omitempty"\``; `type Rules struct{ BuiltIn bool \`json:"built_in"\`; Entries int \`json:"entries"\` }`; `Result.Rules *Rules \`json:"rules,omitempty"\``; `Meta.Rules *Rules` (Build copies it). `schema_version` stays 1.

The spec says "each file entry of a skill gets `rule`", but `files.reviewed` is a list of strings. Adding `files.rules` (path and rule, only files that had one) keeps schema 1 and every reader working, the same way `files.renamed` was added. The owner confirms this shape when reviewing the plan.

- [ ] **Step 1: Write the failing tests:** `TestFileRulesSurviveEncode` (`Build` then `Encode` then decode: `files.rules == [{"path":"a.go","rule":"repo:**/*.go"}]`); `TestFileRulesRedacted` (a token in a path and in a glob is `[REDACTED]` in both fields); `TestRulesObject` (`Meta.Rules{BuiltIn: true, Entries: 7}` gives `"rules":{"built_in":true,"entries":7}`; `Meta.Rules == nil` gives no key; an `ErrorResult` has no key); `TestNoFileRulesNoKey` (no `rules` key under `files`).
- [ ] **Step 2: Run** — `go test ./internal/result/ -count=1`. Expected: FAIL.
- [ ] **Step 3: Implement** in `buildSkill` and `redactSkill` (copy and `redact.String` both fields). Document both fields in `docs/agent-result-schema.md`.
- [ ] **Step 4: Run** — same. Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "feat(result): report the rule of each reviewed file"`

### Task 9: The comment's Rules line

**Files:**
- Modify: `render/render.go:114-145` (Skill), `render/details.go:504-520` (writeFiles)
- Modify: `internal/output/formatter.go:90-102,144-180`
- Test: `render/rules_test.go`, `internal/output/formatter_test.go`

**Interfaces:**
- Produces: `render.FileRule{Path, Rule string}`; `render.Skill.FileRules []FileRule \`json:",omitempty"\`` (golden inputs stay valid); `output.Options.HeaderNote string` (passed to `render.Options.HeaderNote`). pergent.sh fills `FileRules` from the result.

- [ ] **Step 1: Write the failing tests:**
  - `TestRulesLine`: skill A with 12 `.go` files `built-in:go`, skill B with 3 files `repo:internal/api/**/*.go` → the Files block ends with ``Rules: built-in:go (12 files) · repo `internal/api/**/*.go` (3 files)`` before `</details>`. A file listed under two skills counts once. Order: more files first, then label. One file reads `(1 file)`. `dashboard:base:**/*.go` reads ``dashboard:base `**/*.go` ``.
  - `TestNoRulesNoLine`: no `FileRules` → output byte for byte as before (run every golden test).
  - `TestRulesLineEscapesGlob`: a glob with a backtick goes through `codeSpan`.
  - `TestRulesLineShort`: `Short` writes no line (no Files block).
  - `TestOutputPassesFileRules` (output): redacted paths and labels reach `render.Skill.FileRules`; `HeaderNote` reaches the comment.
- [ ] **Step 2: Run** — `go test ./render/... ./internal/output/... -count=1`. Expected: FAIL.
- [ ] **Step 3: Implement** `rulesLine(skills []Skill) string` in `render/details.go`.
- [ ] **Step 4: Run** — same, plus all golden files. Expected: PASS, no golden file changed.
- [ ] **Step 5: Commit** — `git commit -m "feat(render): add the Rules line to the Files block"`

### Task 10: Agent mode inputs

**Files:**
- Modify: `cmd/agent.go:43-90,320-360,433-445,521-536,605-655`, `internal/config/resolver.go:20-75,190-230`
- Create: `cmd/rules.go`
- Test: `cmd/agent_test.go`, `internal/config/rules_test.go`

**Interfaces:**
- Consumes: `rule.DecodeList`, `rule.NewResolver`, `result.Meta.Rules`.
- Produces: flag `--rules` (default `$PERGENT_RULES`), flag `--no-built-in-rules` (`config.Options.NoBuiltInRules`/`NoBuiltInRulesSet`, env `PERGENT_NO_BUILT_IN_RULES` via `resolveBool`, `Config.NoBuiltInRules`); `func readRulesList(path string) ([]rule.Entry, error)` in `cmd/rules.go`; `agentInputs.Rules *rule.Resolver`.

- [ ] **Step 1: Write the failing tests:**
  - `TestAgentRulesPassed`: a `--rules` file outside the checkout reaches `review.Request.Rules` with the same entries; the result has `"rules":{"built_in":true,"entries":2}`.
  - `TestAgentRulesFromEnv`: `PERGENT_RULES` without the flag works the same.
  - `TestAgentRulesInsideRepoRefused`: a `--rules` path inside `--repo-path` → usage error naming `--rules`.
  - `TestAgentRulesMalformedIsUsageError` and `TestAgentRulesUnreadableIsUsageError`: result `status: error`, `error.code: usage`, message `--rules <path>: rules list: entry 1: ...`; no review runs.
  - `TestAgentNoRulesFlag`: no `--rules` → a resolver with no team entries, `rules.entries` 0, `built_in` true.
  - `TestAgentNoBuiltInRules`: `--no-built-in-rules` or `PERGENT_NO_BUILT_IN_RULES=true` → `built_in` false; `PERGENT_NO_BUILT_IN_RULES=maybe` → usage error.
  - `TestAgentIgnoresCheckoutRulesFile`: a `.pergent/rules.yaml` committed at `--head` changes nothing: `rules.entries` is 0 and no batch gets `file-rules.md`.
  - `internal/config`: `TestNoBuiltInRulesResolve` (flag beats env; bad env is a config error), as `checkdeleted_test.go`.
- [ ] **Step 2: Run** — `PATH="$(go env GOROOT)/bin:/opt/homebrew/bin:$PATH" go test ./cmd/ ./internal/config/ -run 'Rules' -count=1` (agent tests need git >= 2.40). Expected: FAIL.
- [ ] **Step 3: Implement.** Add `--rules` to `trustedInputs`; read it after `trustedInputsOutside`, next to `readCIResults` (`agent.go:535`), failing with `result.CodeUsage`; pass `Rules` into `review.Request` and `meta.Rules`.
- [ ] **Step 4: Run** — same. Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "feat(agent): take the rule list from --rules"`

### Task 11: CI and local mode read the base branch

**Files:**
- Modify: `cmd/rules.go`, `cmd/main.go:44-118,140-205,297-312`
- Test: `cmd/rules_test.go`

**Interfaces:**
- Consumes: `rule.Load`, `rule.RepoFile`, `readRulesList`.
- Produces: `func baseRules(repoPath, ref string) ([]rule.Entry, error)`; `var errRulesUnreadable = errors.New("repository rules could not be read")` (wrapped with the reason); `const rulesUnreadableNote = "Repository rules could not be read"`.

- [ ] **Step 1: Write the failing tests** (a temp git repo; the base commit is set as `refs/remotes/origin/main` with `git update-ref`):
  - `TestBaseRulesRead`: the file at `origin/main` loads with source `repo`.
  - `TestCIRulesFromBaseNotHead`: head has another `.pergent/rules.yaml` → the base version is used.
  - `TestBaseRulesMissingFile`: no file at base → no entries, no error.
  - `TestBaseRulesUnreadable`: no `origin/main` ref (shallow clone, no remote) → `errors.Is(err, errRulesUnreadable)`.
  - `TestBaseRulesMalformed`: a bad file at base → an error that is not `errRulesUnreadable`, naming `.pergent/rules.yaml: entry 1:`. A blob over 64 KB is refused from `git cat-file -s` before it is read.
  - `TestCIModeRulesFlagOverrides`: with `--rules` (or `PERGENT_RULES`), the base file is not read.
  - `TestCIModeUnreadableNote`: in a child-process run of `main()` with a fake opencode and `--local`, an unreadable base logs `warning: repository rules could not be read: ...; reviewing with built-in rules only` and the comment has the italic `Repository rules could not be read` line. A malformed base file exits non-zero with `loading rules: .pergent/rules.yaml: ...` (memory: test the wiring through `main()`).
- [ ] **Step 2: Run** — `go test ./cmd/ -run 'BaseRules|CIRules|CIMode' -count=1`. Expected: FAIL.
- [ ] **Step 3: Implement.** `baseRules` runs `git -C <repo> rev-parse --verify --quiet <ref>^{commit}` (fails: unreadable), `git cat-file -e <ref>:.pergent/rules.yaml` (fails: no file), `git cat-file -s` (over `rule.MaxFileBytes`: malformed), then `git show`. The ref is `origin/` plus the platform's base branch (`--local`: `cfg.BaseBranch`); an empty branch is unreadable. In `main`, add `--rules` and `--no-built-in-rules`, build the resolver before `review.Run`, set `Rules` on the request and `HeaderNote` on `commentOptions` when unreadable.
- [ ] **Step 4: Run** — `go test ./cmd/ -count=1`. Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "feat(ci): read path rules from the base branch"`

### Task 12: Docs

**Files:**
- Create: `docs/concepts/rules.md`
- Modify: `docs/concepts/skills.md` (the `rules` field), `docs/reference/configuration.md` (`--rules`, `PERGENT_RULES`, `--no-built-in-rules`, `PERGENT_NO_BUILT_IN_RULES`), `docs/concepts/review-comment.md` (the Rules line), `README.md` (one pointer to `rules.md`), `CLAUDE.md` (Architecture: a `rule/` line like `scope/`; Key Concepts: one **Path rules** bullet)

- [ ] **Step 1:** `rules.md` covers: what a rule is (extra checks, never blocking), the file format and limits, the order and first match, `off`, agent mode (`--rules` list, trusted input), CI mode (base branch, the unreadable note, a malformed file fails), `--no-built-in-rules`, `rules: false`, the `file-rules.md` attachment, and that the built-in list is empty for now. No version numbers.
- [ ] **Step 2:** Read each changed page once for easy English and for any version number (there must be none).
- [ ] **Step 3: Commit** — `git commit -m "docs: describe path rules"`

### Task 13: Verify gate and private-name scan

- [ ] **Step 1:** `make test` — every package `ok`. Report any failure by name.
- [ ] **Step 2:** `make vet` — no output.
- [ ] **Step 3:** `go test ./render/... -count=1` — golden files unchanged (`git status` shows no `testdata` change).
- [ ] **Step 4:** `PERGENT_TEST_OPENCODE=<binary> go test ./cmd -run OpencodeE2E -count=1 -timeout 580s` when a local opencode 2.3.2 is at hand; otherwise say CI's `e2e` job covers it.
- [ ] **Step 5:** `go list -deps ./rule/ ./render/ ./scope/ | grep gopaytech/pergent/internal` — no output.
- [ ] **Step 6:** Private-name scan, must print nothing: `git grep -niE 'golabs|gojek|gopay[^t]|goto\b' -- . ':!go.sum' ':!docs/superpowers/plans/*' ':!web/package-lock.json'`, then `git diff --name-only origin/master | xargs grep -nE '@gmail\.com|/Users/|/private/tmp/'`, and the same `grep` over untracked files before `git add`.
- [ ] **Step 7:** Request the final review (`superpowers:requesting-code-review`) against `origin/master`. Push and the PR only when the owner asks.

## Review panel

The review panel spec (`2026-11-20-review-panel-design.md`) is agreed but not built. When it is built, its judge runs reuse Task 7's `fileRules` and `verifyRulesMessage` for each judge run's findings, and Job P gets the same `--rules`/`PERGENT_RULES` and built-in switch as the members. Nothing in this plan blocks that.
