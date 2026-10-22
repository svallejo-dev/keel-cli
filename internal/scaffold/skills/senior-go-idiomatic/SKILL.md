---
name: senior-go-idiomatic
description: >
  Writes, reviews and explains idiomatic Go (baseline Go 1.26): errors, interfaces, generics,
  goroutines, channels and context, I/O, testing, benchmarks, pgx/sqlc databases, slog, go.mod and
  dependencies, project layout, security, and Go comment policy. Use whenever the user writes,
  reviews, debugs or asks about Go code or a Go snippet, mentions a goroutine, channel, context,
  go.mod, go test, benchmark, pgx, sqlc, slog or generics, asks "is this idiomatic?", or complains
  about too many comments / over-commented Go. Spanish triggers: "¿es idiomático?", "revisa este
  código Go", "mi goroutine se queda colgada", "demasiados comentarios en Go".
---

# Senior Go Idiomatic

Write and review Go the way an experienced Go reviewer would: correct first, then idiomatic, then
fast. The rules below are short on purpose; each section names the reference to open when the
question goes deeper. Read only the reference you need.

Bounded-context structure (one use case per file, `Execute(ctx, XInput) (XOutput, error)`,
ports, value objects) belongs to the **ddd-go-idiomatic** skill; design principles (LoD,
Tell-Don't-Ask, CQS, illegal states) to **go-design-rules**.

## Priorities

When generating or reviewing code, weigh concerns in this order, because a later one never
justifies breaking an earlier one:

1. **Correctness** — no data races, no goroutine leaks, no silently dropped errors.
2. **Idiomaticity** — Go conventions, not patterns transplanted from Java or JavaScript.
3. **Composability** — accept interfaces, return concrete types; play well with the stdlib.
4. **Performance** — measured, not guessed: avoid needless allocations on hot paths.
5. **Observability** — errors with context, structured logs.

## Go Version Baseline

Target Go 1.26, but read the module's `go` line first: language semantics (loop variables, timer
behavior) follow that version, and `go vet` flags stdlib APIs newer than it. Features worth
reaching for, by the version that introduced them:

- **1.21** `slices`, `maps`, `min`/`max`/`clear`, `log/slog`.
- **1.22** per-iteration loop variables (no more `x := x`), `for i := range n`, ServeMux
  patterns with methods and wildcards (`"GET /users/{id}"`, `r.PathValue`).
- **1.23** range-over-func iterators (`maps.Keys`, `slices.Collect`); unreferenced timers are
  collected without `Stop`.
- **1.24** `for b.Loop()` benchmarks, `t.Context()`, the `tool` directive in go.mod,
  `encoding/json` `omitzero`, `crypto/rand.Text`.
- **1.25** `sync.WaitGroup.Go`, `testing/synctest`, container-aware `GOMAXPROCS`.
- **1.26** `errors.AsType[E]`, `new(expr)`.

## Values, Pointers and Memory

- Go passes everything by value; slices, maps and pointers copy a small header that still shares
  the underlying data. Mutating a received slice's elements or keeping it in a field affects the
  caller — copy at the boundary (`slices.Clone`) when that is not the contract.
  → `references/value-vs-reference.md` for aliasing, `append`, shallow struct copies.
- Use pointer receivers when a method mutates, the struct is large or holds a `sync.Mutex`, and
  keep one receiver kind per type so the method set is predictable. Small immutable values
  (`time.Time`, a `Point`) take value receivers.
- Do not guess about allocations: `go build -gcflags=-m` shows what escapes, `-benchmem` shows
  allocs/op. Reorder struct fields for padding only in hot structs with many instances, backed by
  a benchmark or `fieldalignment`; otherwise order fields for readability.
  → `references/memory-pointers.md` for escape analysis, GC tuning, cache locality, pprof.

## Interfaces, Composition and Generics

- **Accept interfaces, return concrete types.** Callers get the full API; inputs stay flexible.
- **The consumer defines the interface**, with only the methods it calls. An interface next to
  its only implementation, "for mocking later", is pollution: add it when a second
  implementation or a test double actually exists.
- A typed nil pointer stored in an interface is not a nil interface; return a literal `nil` on
  the success path. → `references/interfaces-composition.md` §7.
- `var _ Port = (*Adapter)(nil)` is worth writing where a type is meant to satisfy an interface
  its own package never mentions (an adapter for a consumer's port, an `http.Handler`), so a
  signature drift fails locally instead of at the wiring site. → §8.
- Go has no inheritance: embed a struct to reuse behavior, embed an interface to build a
  decorator that overrides one method.
- Generics fit type-agnostic code (containers, algorithms over `cmp.Ordered`); interfaces fit
  behavior that varies by type. Do not add type parameters to avoid writing five lines twice.
- Optional configuration uses functional options with safe defaults:

```go
type Option func(*Server)

func WithTimeout(d time.Duration) Option { return func(s *Server) { s.timeout = d } }

func NewServer(addr string, opts ...Option) *Server {
	s := &Server{addr: addr, timeout: 30 * time.Second}
	for _, opt := range opts {
		opt(s)
	}
	return s
}
```

→ `references/interfaces-composition.md` for method sets, embedding, segregation.
→ `references/idioms-and-style.md` for naming, zero values, early return, enums with
`iota` + `go tool stringer`, defer subtleties, `slices`/`maps`.

## Packages and Layout

- A package is a unit of design, not a folder. Describe it in one sentence that starts with its
  name ("Package user models accounts…"); if you cannot, it has no identity yet.
- No `util`, `helper`, `common`, `shared`, `models`, `types`, `base` or `manager` packages: they
  collect unrelated code and become everyone's dependency. Name packages after the concept.
- Flat by default. Nesting creates no hierarchy and no access to the parent's unexported names;
  only `internal/` is a compiler-enforced boundary.
- Dependencies point inward: the domain package imports no driver, HTTP or JSON packages; infra
  lives in a subpackage of the context (`user/pgstore`) and implements the context's ports.
- Files are `snake_case` and named by what they contain; inside a bounded context, one use case
  per file — the rules live in the ddd-go-idiomatic skill.

→ `references/project-layout.md` for `cmd/`, `internal/`, `platform/`, the composition root, and
anti-patterns.

## Dependencies

The standard library and `golang.org/x/...` come first. Each dependency adds transitive modules,
CVE surface and upgrades you do not control, so it has to pay for itself. Before adding one, check
`go mod why`, `go mod graph` and `govulncheck ./...`; prefer small libraries over frameworks that
take over control flow. Pin build tools with `go get -tool` (Go 1.24) instead of a `tools.go`.
→ `references/dependencies-and-stdlib.md`.

## Concurrency

- **Know how every goroutine ends** before starting it: a closed input channel, a canceled
  context, or a `WaitGroup`/`errgroup` that someone waits on. A goroutine blocked forever is a
  leak that only shows up as slowly growing memory.
- **"Don't communicate by sharing memory; share memory by communicating."** Use channels to hand
  off ownership and coordinate lifecycles; use a `sync.Mutex` (or `atomic`) for long-lived shared
  state with invariants. Pick by the shape of the problem, not the slogan.
- The sender owns a channel and is the only one that closes it; return `<-chan T` so the
  compiler stops receivers from closing it.
- `context.Context` is the first parameter of anything that does I/O or may block, and it is
  passed down, never stored in a struct.
- Run `go test -race` on concurrent code; use `goleak` or a `testing/synctest` bubble to catch
  leaks and time-dependent bugs without real sleeps.

```go
func FetchUser(ctx context.Context, db *sql.DB, id int64) (User, error) {
	var u User
	err := db.QueryRowContext(ctx, "SELECT id, email FROM users WHERE id = $1", id).
		Scan(&u.ID, &u.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("fetch user %d: %w", id, err)
	}
	return u, nil
}
```

Concurrent work that can fail goes through `errgroup` (first error cancels the rest; `SetLimit`
bounds parallelism); fire-and-wait without errors uses `wg.Go(func() {...})` (Go 1.25).

→ `references/channels-fundamentals.md` for channel axioms, `select`, the memory model, races.
→ `references/concurrency-patterns.md` for worker pools, pipelines, fan-in/out, semaphores,
errgroup, rate limiting, singleflight, `sync.Pool`.

## I/O and Text

- `io.Reader`/`io.Writer` compose; `io.Copy(dst, src)` moves data between any pair and uses
  `ReaderFrom`/`WriterTo` fast paths. Don't wrap it in extra layers that hide those.
- Buffer (`bufio`) when you make many small reads or writes. A `bufio.Writer` reports earlier
  write failures only from `Flush`, so call it and return its error instead of a bare
  `defer bw.Flush()`; keep `Close` errors with a named result.
- Cap untrusted input: `http.MaxBytesReader` for request bodies, `io.LimitReader` plus an
  explicit size check elsewhere.
- Strings are bytes: `len` counts bytes, `range` yields runes. Build strings with
  `strings.Builder` or `strings.Join`, not `+=` in a loop; compare case-insensitively with
  `strings.EqualFold`.

→ `references/io-streams.md` for the Reader contract, `io.Pipe` with `CloseWithError`, text
handling, `strings.Builder` vs `bytes.Buffer`.
→ `references/stdlib-idioms.md` for ServeMux routing, middleware, server timeouts, graceful
shutdown, JSON, slog, timers, embed, release builds.

## Errors

- **Wrap when it adds context** the caller lacks — the operation and the key input:
  `fmt.Errorf("fetch user %d: %w", id, err)`. Returning `err` unchanged is fine when the current
  function adds nothing (a thin pass-through). Use `%v` instead of `%w` when you deliberately do
  not want callers to depend on the underlying error.
- Inspect with `errors.Is` (sentinels) and `errors.As` / `errors.AsType[E]` (Go 1.26), never
  `==` or type assertions: both see through wrapping.
- Start opaque; export a sentinel (`var ErrNotFound = errors.New("user: not found")`) or a typed
  error only when a real caller branches on it — each one becomes API.
- Handle an error once: either log it or return it, not both.
- Error strings are lowercase, without trailing punctuation or "failed to".

```go
if err := g.Wait(); err != nil { // errgroup: the first non-nil error
	return fmt.Errorf("sync accounts: %w", err)
}
var pgErr *pgconn.PgError
if errors.As(err, &pgErr) && pgErr.Code == "23505" {
	return ErrEmailTaken // translated at the adapter boundary
}
```

→ `references/errors-advanced.md` for opaque vs sentinel vs typed, behavior interfaces,
`errors.Join`, panic/recover at boundaries and across goroutines.

## Databases

- Size the pool against the database's connection limit and set lifetimes; the defaults are for
  development.
- `defer tx.Rollback(ctx)` right after `Begin` (a no-op after `Commit`), or use
  `pgx.BeginFunc`; retry `40001` serialization failures as a whole transaction.
- Translate driver errors (`pgx.ErrNoRows`, `*pgconn.PgError`) into domain errors in the store
  adapter, which lives in `<ctx>/pgstore`; ports stay small and in the consuming package.
- With pgx, configure sqlc with `sql_package: "pgx/v5"`.

→ `references/databases.md` for database/sql, pgx, sqlc, transactions, store adapters,
migrations.

## Testing

- Table-driven tests with `t.Run`; since Go 1.22 the loop variable needs no copy before
  `t.Parallel()`.
- Test against small fakes of the consumer's port; no mocking framework needed.
- Use `t.Context()` (Go 1.24) for test contexts; it is canceled before `t.Cleanup` runs.
- Benchmarks use `for b.Loop()` (Go 1.24): setup before the loop is not timed and the body is
  not optimized away.

```go
func TestParseSize(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int64
	}{
		{"bytes", "512", 512},
		{"kilobytes", "2K", 2048},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseSize(tt.in)
			if err != nil || got != tt.want {
				t.Errorf("ParseSize(%q) = %d, %v; want %d", tt.in, got, err, tt.want)
			}
		})
	}
}

func BenchmarkParseSize(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		ParseSize("64K")
	}
}
```

→ `references/testing-advanced.md` for TestMain, testcontainers, `testing/synctest`, fuzzing,
golden files, and which test fits which job.

## Security

- Security-sensitive randomness comes from `crypto/rand` (`rand.Text()` for tokens since Go
  1.24); `math/rand/v2` is for simulations and jitter, never for secrets.
- Compare secrets with `subtle.ConstantTimeCompare` to avoid timing leaks.
- Keep secrets out of code and logs: load them from the environment or a secret store, and give
  secret types a `String`/`LogValue` that redacts.

```go
token := rand.Text() // crypto/rand: 26 base32 characters, 128+ bits of entropy

type Secret string

func (Secret) LogValue() slog.Value { return slog.StringValue("[REDACTED]") }
func (Secret) String() string       { return "[REDACTED]" }
```

## Comments

This skill owns the comment policy. Well-named code already says **what** it does; a comment
earns its place by saying what the code cannot.

- One-line package doc (`// Package user models accounts.`) in a single file of the package.
- Every exported identifier gets a one-line doc comment — revive's `exported` rule requires it —
  that starts with the name and **adds information**: units, invariants, returned errors,
  concurrency safety. `// NewUser creates a new User.` adds nothing; say what can fail instead.
  Unexported identifiers usually need none.
- Inline comments explain **why**: an external constraint, a workaround with its issue link, a
  measured performance decision. Directives (`//go:generate`, `//nolint:x // reason`) are fine.
- No narration (`// call the repo`), banners (`// ---- Handlers ----`), commented-out code, or
  `TODO` without an owner or issue. Design rationale goes to the README or an ADR.
- As a rough gauge, comment lines stay around 10–15 % of a file; more usually means the names
  need work.

→ `references/idioms-and-style.md` §5 for BAD/GOOD examples and `Example` tests.

## Common Footguns

| Footgun | Symptom | Fix |
|---|---|---|
| Typed nil in an interface | `err != nil` although the pointer is nil | Return a literal `nil` on success |
| Goroutine without an exit | Memory and goroutine count grow | ctx / closed channel; `goleak` in tests |
| Unchecked `bufio.Writer.Flush` | Truncated file, no error | Return `bw.Flush()`; named result for `Close` |
| `==` on errors | Wrapped errors never match | `errors.Is` / `errors.As` / `errors.AsType` |
| `math/rand` for tokens | Predictable secrets | `crypto/rand` (`rand.Text`) |
| Write to a nil map | Panic | `make(map[K]V)` first |
| Concurrent map writes | Fatal error, not recoverable | `sync.Mutex` or `sync.Map` |
| Sub-slice `append` | Overwrites the parent's elements | Three-index slice `s[a:b:c]` or `slices.Clone` |
| `for _, v := range xs { v.X = 1 }` | Mutation silently lost | Index: `xs[i].X = 1` |
| `s[i]` / `len(s)` on UTF-8 | Bytes, not characters | `range s`, `utf8.RuneCountInString` |
| `+=` on strings in a loop | Quadratic copying | `strings.Builder` / `strings.Join` |
| `defer` in a loop | Files stay open until the function returns | Move the body into a function |
| Missing `defer tx.Rollback` | Connection held on error paths | Defer right after `Begin` |
| Loop-variable capture | Only on modules with `go` ≤ 1.21 | Upgrade the `go` line, or `x := x` |

## Review Checklist

- [ ] `context.Context` first in every function that does I/O or may block; passed, not stored
- [ ] No error dropped silently; wrapped with `%w` where it adds context; inspected with `errors.Is/As`
- [ ] Every goroutine has a clear exit; `go test -race` passes
- [ ] Interfaces small and defined by the consumer; concrete types returned
- [ ] No `sync.Mutex` copied; one receiver kind per type
- [ ] `bufio.Writer.Flush` and `Close` errors returned; untrusted input size-capped
- [ ] Strings handled as UTF-8 where it matters (`range`, `EqualFold`, `Builder`)
- [ ] No `util`/`common`/`models` packages; each package describable in one sentence
- [ ] Domain packages import no infrastructure; adapters live in subpackages
- [ ] No dependency the stdlib or `golang.org/x/...` covers; new ones checked with `govulncheck`
- [ ] Comments: one-line docs on exported identifiers that add information; no narration or banners
- [ ] Modern stdlib used where it simplifies (`slices`, `maps`, `min`/`max`, `wg.Go`, `b.Loop`)

## Reference Files

Open the one that matches the question; do not load them all.

- `references/value-vs-reference.md` — copies, slice headers and `append` aliasing, maps, shallow
  struct copies, `copylocks`, defensive copies. Read when data changes "by itself".
- `references/memory-pointers.md` — stack vs heap, escape analysis, receivers, GC knobs, cache
  locality, struct padding, profiling. Read for allocation or latency work.
- `references/interfaces-composition.md` — implicit satisfaction, consumer-defined interfaces,
  pollution, method sets, nil-interface footgun, compile-time assertions, embedding.
- `references/idioms-and-style.md` — naming, zero values, early return, enums, the full comment
  policy, defer subtleties, `slices`/`maps`, tooling.
- `references/project-layout.md` — package design, `cmd/`, `internal/`, platform packages, the
  composition root, nesting, dependency direction.
- `references/dependencies-and-stdlib.md` — stdlib-first, auditing modules, the `tool`
  directive, MVS, vendoring, when a dependency is worth it.
- `references/channels-fundamentals.md` — channel states, buffering, `select`, happens-before,
  data races, channels vs mutexes.
- `references/concurrency-patterns.md` — worker pool, pipeline, fan-in/out, semaphore, errgroup,
  or-done, heartbeat, timeouts, `sync.Cond`, backpressure, rate limiting, singleflight,
  `sync.Pool`.
- `references/errors-advanced.md` — opaque/sentinel/typed errors, behavior interfaces,
  `errors.Join`, panic and recover at boundaries.
- `references/io-streams.md` — Reader/Writer contracts, bufio, composition, `io.Pipe`,
  `io.Copy` fast paths, strings and UTF-8.
- `references/stdlib-idioms.md` — ServeMux routing, middleware, server timeouts, context,
  signals and graceful shutdown, JSON, slog, timers, embed, build constraints, release builds.
- `references/databases.md` — database/sql pool, pgx, sqlc, transactions and retries, store
  adapters, migrations.
- `references/testing-advanced.md` — TestMain, integration tests with testcontainers,
  `testing/synctest`, `b.Loop` benchmarks, fuzzing, golden files, test strategy.
