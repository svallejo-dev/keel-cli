# Universal review rubric

Dimensions that apply to any PR in any language. Walk each changed file through them and
record what you find with one severity.

## Dimensions

### 1. Correctness and edge cases
- Does the code do what the PR's intent says?
- Edges: empty input, nil/null, zero, negatives, single-element lists, overflow,
  off-by-one, ordering, time zones, unicode.
- Failure paths: if a network or DB call fails halfway, is state still consistent?
- Idempotency and retries where the flow needs them.

### 2. Security
- Injection (SQL, NoSQL, shell, LDAP): concatenated input instead of parameters.
- Authn/authz: permission checked at the right place? IDOR? Access decided from
  client-supplied data?
- Secrets hardcoded or logged: **blocker**, and never quote the value.
- External input (HTTP, queues, files) validated and bounded; size limits on reads; safe
  deserialization.
- Crypto: secure randomness for tokens, constant-time comparison, no broken algorithms.

### 3. Tests
- New or changed behavior has tests, including the failure path and the edges.
- Tests are deterministic (no sleeps, uncontrolled clock or network).
- A passing test actually exercises the new code.

### 4. Error handling
- Errors propagate with context instead of being swallowed or logged-and-ignored.
- Expected errors (not found) are distinguished from unexpected ones.
- No panics/uncaught exceptions in normal flows; resources released on error paths.

### 5. API and backward compatibility
- Changes to public signatures, endpoints, schemas, serialized formats, env vars.
- Breaking changes (renamed/removed fields, changed types, defaults, status codes):
  versioned, deprecated or migrated? Who already depends on the current behavior?

### 6. Performance and concurrency
- N+1 queries, avoidable O(n²), allocations in hot paths, missing pagination.
- Data races, unsynchronized shared state, deadlocks, goroutine/thread leaks, locks held
  during I/O. Would the race detector pass?

### 7. Size and readability
- Small and focused enough to review rigorously? Mixed concerns (refactor + feature + fix
  + mass formatting)?
- Clear names, cohesive functions, no dead or commented-out code.
- Files named by what they do. Grab-bag files (`utils`, `helpers`, `common`, `manager`)
  hurt navigation in any language; their severity depends on the repo's own rules, see
  the Go layer.
- Comments that narrate the obvious or banners add review noise (minor or nit).

### 8. Docs and migrations
- Behavior changes reflected in README, docs, CHANGELOG, public API docs.
- DB migrations reversible? Do they lock large tables? Compatible with the version still
  deployed during rollout (expand/contract)?
- New flags and config documented.

### 9. Observability
- Logs at the right level with context, no secrets, no spam in hot paths.
- Metrics or traces for operationally important new paths.
- Error messages actionable for whoever operates the system.

## Severity

Exactly one per finding:

- **blocker** — cannot merge as is: breaks correctness, security or a public contract,
  loses data, introduces a race.
- **major** — should be fixed before merge: a real bug in a plausible case, missing tests
  for new logic, poor error handling, or a violation of a rule the repo itself adopted.
- **minor** — worth fixing, no immediate risk: clarity, an unlikely edge, confusing names,
  house-style suggestions the repo has not adopted.
- **nit** — optional preference. Label it so the author knows it can be ignored.

Verdict: any blocker or major → request changes; only minor/nit → comment; nothing found →
"looks ready", with approval left to a human.

## Size rule

If the PR is too big to review rigorously or mixes concerns, put the split request first,
propose the cuts ("separate the rename refactor from the logic change") and offer to review
each part. A shallow review of a huge PR is worse than asking to split it.

## Output format

Write it in the language the PR uses.

```markdown
## Review — <PR title>

**Verdict:** request changes | comment | looks ready  ·  **Intent:** <one line>
**Summary:** <2-3 lines: what it does well, what is missing, blast radius>

### Blockers
- `path/file.ext:LINE` — <what is wrong and why>. **Fix:** <concrete change>

### Major
- `path/file.ext:LINE` — … **Fix:** …

### Minor
- `path/file.ext:LINE` — …

### Nits
- `path/file.ext:LINE` — …

### Good
- <one or two things the PR does well>
```

Omit empty sections.
