# Fallback PR body

Use this only when the repo has no PR template and its recent PRs show no structure worth
copying. Write it in the repo's language: for a Spanish repo the headings become Resumen,
Motivación, Qué cambió, Cómo probar, Riesgo y reversa, Issues.

## Template

~~~markdown
## Summary

<!-- One or two lines: what this PR does. -->

## Why

<!-- The problem it solves, the issue that asked for it, design decisions taken. -->

## What changed

-

## How to test

```bash

```

## Risk and rollback

- **Blast radius:**
- **Migrations / flags:**
- **Rollback:**

## Issues

Closes #
~~~

Drop sections that do not apply (nothing to link → no Issues section) rather than leaving
them empty, and add a Screenshots section only when the PR changes UI.

## Filling each section

With `BASE_REF` set as in SKILL.md (fetched base, e.g. `origin/main`):

| Section | Source | Command |
|---|---|---|
| Summary | Synthesis of the commits, not a dump | `git log "$BASE_REF"..HEAD --format=%s` |
| Why | Commit bodies plus the linked issue | `git log "$BASE_REF"..HEAD --format=%b` |
| What changed | One bullet per meaningful change | `git log "$BASE_REF"..HEAD --format='- %s'` |
| How to test | Real commands for what was touched (tests, build, a curl) | `git diff "$BASE_REF"...HEAD --stat` |
| Risk and rollback | Files touched, migrations, flags, contract changes | `git diff "$BASE_REF"...HEAD` |
| Issues | `Closes`/`Refs` footers in the commits | `git log "$BASE_REF"..HEAD --format=%b \| grep -iE '(closes\|fixes\|refs) #'` |

Two dots for the commit log (commits on the branch), three dots for the diff (against the
merge base, so unrelated changes that landed on the base do not show up).

Guidance:

- "How to test" lists commands a reviewer can paste, not "test the app".
- When there are no migrations or flags, say so; the rollback is usually "revert the
  merge commit" unless a data migration makes that unsafe, in which case explain.
- Never include secrets, tokens or `.env` contents.

## Example

Branch `feat/auth-refresh`, commits `feat(auth): add token refresh endpoint` and
`test(auth): cover refresh happy path`.

**Title:** `feat(auth): add token refresh endpoint`

~~~markdown
## Summary

Adds `POST /auth/refresh` so clients renew access tokens without a full re-login.

## Why

Sessions expired silently after one hour and users were bounced to the login page
mid-flow. Design agreed in #142.

## What changed

- New refresh endpoint backed by the existing `sessions` table
- Tests for the happy path and an expired refresh token

## How to test

```bash
go test ./internal/auth/...
curl -sS -X POST localhost:8080/auth/refresh -H 'Authorization: Bearer <refresh-token>'
```

## Risk and rollback

- **Blast radius:** limited to `internal/auth`; if refresh fails, clients fall back to login.
- **Migrations / flags:** none.
- **Rollback:** revert the merge commit.

## Issues

Closes #142
~~~
