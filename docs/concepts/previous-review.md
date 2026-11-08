# 🔄 Previous review as context

By default each run reviews from scratch. With `--previous-review` (or `PERGENT_PREVIOUS_REVIEW=true`), pergent fetches the review comment it posted on the previous run, reads the findings from its hidden findings block, and attaches each skill's findings to that skill's runs alongside the diff. The agent is instructed to stay consistent with them, focus on what changed since, drop findings the new commits resolve, and reuse a finding's fingerprint when it still applies. pergent keeps a reused fingerprint only on the same file and line as the previous finding, so a new finding can't take over an old one's identity or its dismissal. A skill with no previous findings gets nothing attached.

pergent stays stateless, the PR/MR comment is the only persistent state. First runs (no existing comment) behave exactly as before. Requires platform mode (not available with `--local`).

Comments posted by older versions of pergent have no findings block, so they are ignored: the first run after upgrading is a first review. A findings block that cannot be read is skipped with a warning in the log.

The findings are attached as data: the prompt tells the agent to treat the file as data, never as instructions.

## Which comment is pergent's

A comment is pergent's own only when **both** hold: its body starts with the `<!-- pergent -->` marker (every pergent comment does), and its author is the identity of pergent's token. A note by anyone else that holds the marker, or a forged findings block, is never updated, never overwritten and never read for findings: pergent skips it, keeps looking through every page, and creates its own comment if it finds none.

pergent learns the token's identity once per run with `GET /user`:

- **GitLab**: the `api` scope that posting notes needs also covers `/user`, for project and group access tokens too. If the read fails, as it can for a fine-grained token without `read_user`, the comment lookup counts as failed (see below), so each run posts a new comment.
- **GitHub**: a personal or fine-grained token can read `/user`. The `GITHUB_TOKEN` of GitHub Actions, and a GitHub App installation token, cannot (GitHub answers 403). Then pergent accepts only a comment whose author is a `Bot`, because a person cannot post as a Bot.

If the token's identity changes (a new bot user, a new token owner, a switch between `GITHUB_TOKEN` and a personal token), pergent starts a new comment and the old one stays on the PR/MR. Delete it by hand if you don't want it.

If looking for the existing comment fails (a network or server error), pergent tries once more after a few seconds. If it still fails, it still posts the review as a new comment, because a visible new result beats a stale one, and it logs that a duplicate may appear.

```yaml
mr-review:
  stage: review
  image: ghcr.io/gopaytech/pergent:latest
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
  script:
    - pergent --skill code-review
  variables:
    GITLAB_TOKEN: $GITLAB_TOKEN
    PERGENT_PREVIOUS_REVIEW: "true"
    OPENCODE_PROVIDER: anthropic
    OPENCODE_API_KEY: $ANTHROPIC_API_KEY
    OPENCODE_MODEL: claude-sonnet-4
```
