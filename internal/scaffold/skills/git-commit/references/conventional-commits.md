# Conventional Commits (fallback format)

Use this only when the repo has no convention of its own (no commitlint/commitizen config,
nothing in CONTRIBUTING, no consistent pattern in `git log`). If the repo already writes
`ABC-123: Fix login` or `Fix login redirect`, follow that instead.

```
type(scope)!: subject

Body: the why and the context, wrapped near 72 columns.

Footer: value
```

`scope` is optional. `!` before the colon marks a breaking change.

## Types

| type | When | Example subject |
|---|---|---|
| `feat` | New behavior visible to users or API clients | `feat(auth): add token refresh endpoint` |
| `fix` | Corrects wrong behavior | `fix(parser): handle empty input without panicking` |
| `docs` | Documentation only | `docs(readme): document env vars` |
| `style` | Formatting with no logic change | `style: run gofmt` |
| `refactor` | Restructure without behavior change | `refactor(store): extract query builder` |
| `perf` | Change whose purpose is speed or memory | `perf(cache): reuse buffer per call` |
| `test` | Tests only | `test(auth): cover expired-token path` |
| `build` | Build system or dependencies | `build(deps): bump pgx to v5.7` |
| `ci` | CI configuration | `ci: cache go modules` |
| `chore` | Maintenance that fits nowhere else | `chore: ignore local .env files` |
| `revert` | Reverts an earlier commit; body cites the hash | `revert: feat(auth): add token refresh endpoint` |

Tie-breakers: if users or clients can do something new or different, it is `feat` or `fix`;
otherwise one of the others. `perf` beats `refactor` when speed is the intent. Tests that
prove a fix ride in the `fix` commit.

## Subject

- Imperative, present tense: "add", "fix", "remove".
- Lowercase after the colon, no trailing period, about 50 columns.
- Says what the commit does, not how.

```
good: feat(user): validate email on signup
bad:  feat(user): Added email validation.     # past tense, capital, period
bad:  fix: stuff                              # says nothing
```

## Body

Separated from the subject by a blank line. Explains the problem, the reason for this
approach, and side effects. It does not paraphrase the diff.

```
fix(session): expire idle tokens after 30m

Tokens lived until server restart, so a stolen token stayed valid
indefinitely. Tokens now carry an expiry checked by the middleware.
```

## Footers

One per line, `Token: value`, after a blank line:

- `Closes #142` / `Fixes #142` close the issue on merge; `Refs #142` only links it.
- `BREAKING CHANGE: <what breaks and how to migrate>` (pairs with `!` in the subject).
- Trailers the environment requires, such as `Co-Authored-By: Name <email>`, verbatim.

## Breaking change example

```
feat(api)!: return refresh token in the body instead of a cookie

The endpoint no longer sets the long-lived `sid` cookie; clients own
token storage and rotation.

BREAKING CHANGE: /session stops setting `sid`. Read access_token and
refresh_token from the JSON body.
Refs #310
```
