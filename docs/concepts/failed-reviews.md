# 🩺 When a review fails

Each opencode run (one per batch) ends in one of two ways: the model's own final answer, in the required format, is parsed into findings, or the batch is reported as **Review failed:** with the cause and the files it covered. pergent never posts opencode's internal messages or the model's in-progress text ("Let me check...") as a review. A skill with any failed batch has the verdict **failed**, never "no blocking issues", even if the other batches found nothing.

| Cause | Example |
|---|---|
| Context overflow | The model's context window overflowed after 14 steps: the last completed step used 186,400 tokens (limit: the model's context window as known to opencode). ContextOverflowError: prompt is too long: 201234 tokens > 200000 maximum. |
| Timeout | Timed out after 10m0s (review budget 10m0s) during step 9; the last tool call was `read` on `service/store.go`. |
| opencode error | opencode reported APIError (HTTP 429): Rate limit exceeded. Gave up after 4 attempts. |
| Step limit | Reached the 20-step limit before the model finished the review (raise --max-turns to allow more steps). |
| opencode exited non-zero | opencode exited with code 1 at step 2 without producing a review. (The last lines of opencode's stderr follow in a code block.) |
| No answer | opencode exited normally after 6 steps, but the model did not produce a final answer (last step finished with reason "tool-calls"). |

- **The CI job stays green** when a review fails; the cause is in the comment and the job log. Configuration errors found before any review starts (missing token, bad flag) still fail the job.
- **Transient provider errors** (HTTP 429, 5xx, connection errors) are retried up to 3 times with exponential backoff (5s, 10s, 20s) within the review's `--timeout` budget. opencode already retries these itself before giving up, so pergent's retries cover longer outages. If every attempt fails, the last error is reported as is.
- **Step limit:** on the last allowed step opencode asks the model to stop and summarise its progress. That summary is not a review, so it is reported as a step-limit failure. If the model ignores the instruction and keeps calling tools, pergent stops opencode.
- **An answer in the wrong format** (no JSON block, or a block that does not parse) gets one repair run that rewrites it; if that fails too, the batch is reported as failed with an excerpt of the answer.
- **Secrets are redacted** from the comment and the log: the values of `OPENCODE_API_KEY`, `GITHUB_TOKEN` and `GITLAB_TOKEN`, bearer tokens, and API keys in URLs.
