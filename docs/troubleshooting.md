# 🔍 Troubleshooting

## opencode seems slow or stuck

pergent shows step-by-step progress as opencode works. Skills and batches run in parallel, so lines from different runs interleave:
```
  Step 1: starting...
  Step 1: tool read
  Step 1: done (472 tokens)
  Step 2: starting...
  Step 2: generating text...
  Step 2: done (800 tokens)
```

If no steps appear, the model may be slow to respond. Test the connection:
```bash
pergent --test
```

If `--test` also hangs, test opencode directly:
```bash
opencode run "Say hello"
```

If that also hangs, the issue is your LLM connection, not pergent.

## `Review failed: ...`

The message is the cause (see [When a review fails](concepts/failed-reviews.md)). To see everything opencode reported, dump its raw output:
```bash
PERGENT_DEBUG_OUTPUT=/tmp/pergent-raw.json pergent --local --skill code-review
cat /tmp/pergent-raw.*.json
```

This shows the raw NDJSON events. Every opencode run (and every retry of one) writes its own file, named from your path with a number before the extension (`/tmp/pergent-raw.1.json`, `/tmp/pergent-raw.2.json`, ...), and pergent logs each path as it writes it. A file that exists is never written over. The files are readable by you alone (mode 600), and credentials such as the API key are redacted. Treat them as private anyway: they hold the code and the review. The review is the text of the last `step_start`...`step_finish` block whose `step_finish` has a reason other than `tool-calls`. `"type":"error"` events carry opencode's error, and `text` events with `"synthetic":true` are opencode's own messages, never the model's.

## `git diff failed: exit status 128`

Common causes:
- **Wrong base branch**: your repo uses `master` not `main`. Use `--base-branch master`.
- **No git repo**: run `git init` and make at least one commit.
- **No remote**: `origin/main` doesn't exist. Push to a remote or create the ref.

## `/responses` API errors

If you see errors mentioning `/responses: Invalid model name` or 404s from `/v1/responses`, your API proxy doesn't support OpenAI's Responses API. pergent handles this automatically when `OPENCODE_BASE_URL` is set — it uses `@ai-sdk/openai-compatible` which calls `/chat/completions` instead. Make sure `OPENCODE_BASE_URL` ends with `/v1`:
```bash
export OPENCODE_BASE_URL=https://your-proxy.example.com/v1
```

## `The model's context window overflowed`

The diff, skill instructions and the files the agent read no longer fit in the model's context. Either:
- Use a model with a larger context window
- Lower `--max-turns`, so the agent reads less before answering
- If opencode doesn't know your model's limits (custom `OPENCODE_BASE_URL`), set `--context-limit` to the real window

## `Configuration is invalid` from opencode

If you set `OPENCODE_PROVIDER`, pergent generates a config for opencode. If the generated config doesn't match your opencode version, unset the provider env vars and let opencode use its own config:
```bash
unset OPENCODE_PROVIDER OPENCODE_MODEL OPENCODE_BASE_URL OPENCODE_API_KEY
pergent --local --skill code-review
```
This is for your own machine. In GitHub or GitLab CI the same step needs `PERGENT_USE_OPENCODE_CONFIG=true`, and then the review has no sandbox (see below).

## Using opencode's existing config (GitHub Copilot, etc.)

If you already have opencode configured (e.g., connected to GitHub Copilot), leave `OPENCODE_PROVIDER` unset. pergent will skip config generation and opencode uses its own config:
```bash
pergent --local --skill code-review
```
In GitHub or GitLab CI, set `PERGENT_USE_OPENCODE_CONFIG=true` as well. Without it pergent stops with `OPENCODE_PROVIDER is not set`. That mode has no sandbox, so do not use it on untrusted changes (see [option B](reference/configuration.md#own-config-mode)).

## `OPENCODE_PROVIDER is not set` in CI

In GitHub or GitLab CI, an empty `OPENCODE_PROVIDER` stops the review, so a CI variable that was never defined (it expands to an empty string) can't quietly turn the sandbox off. Set `OPENCODE_PROVIDER` and `OPENCODE_MODEL` (and `OPENCODE_API_KEY`), or, if you want opencode's own config and trust every change under review, set `PERGENT_USE_OPENCODE_CONFIG=true`. If the error also says `OPENCODE_MODEL is set without OPENCODE_PROVIDER`, the provider variable is the one that is missing.

## `Ctrl+C` to cancel

Pressing `Ctrl+C` cleanly stops both pergent and the opencode subprocess.
