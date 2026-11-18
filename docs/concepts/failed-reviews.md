# 🩺 When a review fails

Each opencode run (one per batch) ends in one of two ways: the model's own final answer, in the required format, is parsed into findings, or the batch is reported as **Review failed:** with the cause and the files it covered. pergent never posts opencode's internal messages or the model's in-progress text ("Let me check...") as a review. A skill with any failed batch has the verdict **failed**, never "no blocking issues", even if the other batches found nothing. With `--check-deleted`, a batch that carries a deleted file is a batch like any other: if it fails, the skill is **failed**, and the deleted file is not counted as reviewed. The same holds for the [reference check](deleted-files.md) when it runs on its own, for a skill with a list of deleted or renamed files and no batch: if that run fails or never starts, the skill is **failed**, with a failure that names no file.

| Cause | Example |
|---|---|
| Context overflow | The model's context window overflowed after 14 steps: the last completed step used 186,400 tokens (limit: the model's context window as known to opencode). provider.invalid-request: prompt is too long: 201234 tokens > 200000 maximum. |
| Timeout | Timed out after 10m0s (review budget 10m0s) during step 9; the last tool call was `read` on `service/store.go`. |
| opencode error | opencode reported unknown: fetch failed. Gave up after 4 attempts. |
| Step limit | Reached the 20-step limit before the model finished the review (raise --max-turns to allow more steps). |
| opencode exited non-zero | opencode exited with code 1 at step 2 without producing a review. (The last lines of opencode's stderr follow in a code block.) |
| No answer | opencode exited normally after 6 steps, but the model did not produce a final answer (last step finished with reason "tool-calls"). |

- **The CI job stays green** when a review fails; the cause is in the comment and the job log. Configuration errors found before any review starts (missing token, bad flag) still fail the job.
- **Transient errors** (HTTP 429, 5xx, connection errors) are retried within the review's `--timeout` budget with exponential backoff starting at 5s. opencode already retries provider errors itself before giving up (a persistent 500 holds a run for about 86 seconds), so pergent starts a run again only once after one. Other transient errors, such as a failed connection, are retried up to 3 times (5s, 10s, 20s). If every attempt fails, the last error is reported as is. An error event pergent cannot read fails the run.
- **Step limit:** on the last allowed step opencode asks the model to stop and summarise its progress. That summary is not a review, so it is reported as a step-limit failure. If the model ignores the instruction and keeps calling tools, pergent stops opencode.
- **An answer in the wrong format** (no JSON block, or a block that does not parse) gets one repair run that rewrites it; if that fails too, the batch is reported as failed with an excerpt of the answer.
- **Secrets are redacted** from the comment and the log: the values of `OPENCODE_API_KEY`, `GITHUB_TOKEN` and `GITLAB_TOKEN`, bearer tokens, and API keys in URLs.
