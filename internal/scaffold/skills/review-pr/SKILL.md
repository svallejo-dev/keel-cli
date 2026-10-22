---
name: review-pr
description: >
  Review a GitHub pull request (number, URL or branch) with a severity rubric plus a
  Go/DDD layer for .go files, reading the PR's own head rather than the local checkout.
  Returns the review in the conversation; posts to GitHub only when explicitly asked
  ("post the review", "publica el review", "comenta en el PR") and never approves.
  Triggers: "review PR #123", "revisa este PR", "code review this pull request",
  "revisión de PR". For a bug hunt of local uncommitted changes use /code-review; for a
  security audit, /security-review.
---

# Review PR

Review a GitHub pull request end to end: understand its intent, run the diff through a
universal rubric (plus a Go/DDD layer for `.go` files), and hand back findings with
severity, location, reason and a concrete fix. What sets this apart from a generic bug
hunt is the rubric, the Go/DDD layer, and the GitHub PR as the unit of review.

The default output is the review in the conversation. Posting under the user's GitHub
identity is a separate, explicit step (step 6), because a review comment is public and
notifies the author.

## 1. Environment

```bash
gh auth status                 # installed and logged in?
gh api user --jq .login        # the current user, needed in step 6
```

Without `gh` (Claude Desktop, claude.ai, or not logged in), ask the user to paste the diff
or the PR description and review what they give you; mention `gh auth login` if they want
the full flow. For a PR URL, pass `--repo <owner>/<repo>` to every `gh` call; for a bare
number, `gh` uses the current repo (in a fork it may need `--repo <parent>`).

## 2. Fetch the PR

```bash
gh pr view <n> --json number,url,title,body,author,isDraft,baseRefName,headRefName,headRefOid,isCrossRepository,additions,deletions,files
gh pr diff <n>                 # read all of it; for large PRs start with --name-only
gh pr checks <n>
```

The local working tree is usually **not** the PR head (another branch, uncommitted edits),
so never read PR files from it. When the diff is not enough context, read files at the
PR's head commit:

```bash
git fetch <remote> "pull/<n>/head"        # updates FETCH_HEAD only; nothing checked out
git show "<headRefOid>:path/to/file.go"
# without a local clone:
gh api "repos/<owner>/<repo>/contents/path/to/file.go?ref=<headRefOid>" -H "Accept: application/vnd.github.raw"
```

To build or test the PR, use a throwaway worktree instead of `gh pr checkout`, which would
switch the user's branch:

```bash
wt=$(mktemp -d "${TMPDIR:-/tmp}/pr-review.XXXXXX")
git worktree add --detach "$wt" "<headRefOid>"
# ... go test ./... inside "$wt" ...
git worktree remove --force "$wt"
```

Before judging code, pin down three things:

- **Intent** — what problem it solves (title, body, linked issue). Correct code that misses
  the intent is still wrong.
- **Scope** — modules and consumers touched; any public contract change.
- **Size** — if it is too large to review well or mixes concerns, asking for a split is the
  most valuable finding and goes first.

## 3. Universal rubric

Apply [references/review-rubric.md](references/review-rubric.md) to the whole diff:
correctness and edge cases, security, tests, error handling, API compatibility,
performance and concurrency, size and readability, docs and migrations, observability. It
also defines the severities and the output format.

## 4. Go/DDD layer (only `.go` files)

Read [references/go-review-layer.md](references/go-review-layer.md). In short: general Go
correctness findings (swallowed errors, leaks, races) apply to every repo; the house
layout rules (one use case per file, no grab-bag files, comment density) count as
**major** only when the repo adopts them, and are otherwise offered as minor or nit
suggestions. When the companion skills `senior-go-idiomatic`, `ddd-go-idiomatic` or
`go-design-rules` are available, use them for depth.

## 5. Findings and verdict

Each finding: **severity** (blocker / major / minor / nit), `file:line`, why it is a
problem, and a concrete fix. Order from most to least severe; criticize code, not people;
also say what the PR does well.

Recommended verdict: any blocker or major → request changes; only minor/nit → comment;
nothing found → say it looks ready, while approval stays with a human.

Return the review in the conversation using the format in the rubric, plus a one-line
count by severity. Offer to post it.

## 6. Post to GitHub (only when asked)

Post only when the user explicitly asks to post, publish or comment on the PR. Then:

- **Event**: if the PR author is the current user, use `--comment` (GitHub rejects
  "request changes" on your own PR). Otherwise `--request-changes` when there is a blocker
  or major, `--comment` for the rest.
- **Never approve**, even when asked: approval is a human sign-off, so tell the user to
  approve in GitHub themselves.
- Write payloads to temp files outside the repo so they cannot be committed by accident.

```bash
body=$(mktemp "${TMPDIR:-/tmp}/review.XXXXXX")
# write the review markdown into "$body"
gh pr review <n> --comment --body-file "$body"      # or --request-changes
rm -f "$body"
```

Inline comments go through the reviews API in one request. Each `line` must fall inside a
diff hunk on the `RIGHT` (new) side, or GitHub answers 422; put anything else in the
summary body.

```bash
payload=$(mktemp "${TMPDIR:-/tmp}/review-json.XXXXXX")
cat > "$payload" <<'JSON'
{
  "commit_id": "<headRefOid>",
  "body": "Summary of the review. See inline comments.",
  "event": "COMMENT",
  "comments": [
    { "path": "internal/user/register_user.go", "line": 42, "side": "RIGHT",
      "body": "major: the error is dropped; wrap it: `return fmt.Errorf(\"find user %d: %w\", id, err)`" }
  ]
}
JSON
gh api --method POST "repos/<owner>/<repo>/pulls/<n>/reviews" --input "$payload"
rm -f "$payload"
```

`event` is `COMMENT` or `REQUEST_CHANGES`, chosen by the same rule. Report the review URL.

## Guardrails

- **Never approve** a PR (`--approve`, `event: APPROVE`).
- **Never reproduce a secret** found in the diff; describe the pattern and location and
  mark it blocker.
- **Review only**: no push, merge, close, label or edit of the PR, and no checkout in the
  user's working tree.
