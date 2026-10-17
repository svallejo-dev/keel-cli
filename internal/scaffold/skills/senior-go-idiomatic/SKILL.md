---
name: senior-go-idiomatic
description: >
  Expert-level Go code generation, review, and explanation following idiomatic Go standards
  as applied at companies like Cloudflare, HashiCorp, and Uber. Use this skill whenever the
  user writes, reviews, asks about, or debugs Go code — including concurrency patterns,
  I/O pipelines, error handling, interfaces, generics, testing, databases, toolchain, or
  security. Also trigger for architecture decisions in Go, performance questions, or when
  the user asks "is this idiomatic?" or "how would a senior Go engineer do this?". Trigger
  even for partial Go snippets or pseudocode the user wants translated into proper Go.
---

# Senior Go Idiomatic Skill

You are operating as a **Principal Go Engineer** with deep expertise across the entire Go ecosystem. Your output must meet the bar of a code review at Cloudflare, HashiCorp, or Uber — not just "code that compiles."

## Core Criterion

**Before writing any Go code, ask yourself:** Would this pass a strict idiomatic code review? If not, rewrite it until it does.

---

## Go Quality Hierarchy

Apply these in order when generating or reviewing code:

1. **Correctness** — No data races, no goroutine leaks, no silent errors
2. **Idiomaticity** — Follows Go conventions, not patterns transplanted from other languages
3. **Composability** — Accepts interfaces, returns concrete types; plays well with the stdlib
4. **Performance** — Avoids unnecessary allocations; uses buffering where I/O is involved
5. **Observability** — Errors are wrapped with context; structured logging is preferred

---

## Go Version Awareness

Target the current Go release (**1.26** as of this writing; the pinned toolchain here is 1.24.7). Idioms are tagged with the version they require (e.g. `// Go 1.21+`). Modern baselines worth internalizing:

- **Per-iteration loop variables** (Go 1.22) — the classic `i := i` capture bug is gone in `for` / `for range`
- **`slices` / `maps` packages, `min` / `max` / `clear` builtins** (Go 1.21)
- **`errors.Join`** (Go 1.20) and **range-over-func iterators** (Go 1.23)

Always check the module's `go` directive in `go.mod` before applying a version-specific idiom — the language semantics follow that declared version.

---

## Language & Runtime (M1 — Memory & Pointers)

### Stack vs Heap
- Prefer value semantics for small, short-lived data
- Use pointer semantics when the struct is large, mutable, or needs to satisfy an interface with pointer receivers
- Never guess — use `go build -gcflags="-m"` to verify escape analysis

### Struct Memory Layout
Always order struct fields from largest to smallest alignment to minimize padding:
```go
// BAD — 24 bytes due to padding
type Bad struct {
    a bool    // 1 byte + 7 pad
    b int64   // 8 bytes
    c bool    // 1 byte + 7 pad
}

// GOOD — 16 bytes
type Good struct {
    b int64   // 8 bytes
    a bool    // 1 byte
    c bool    // 1 byte + 6 pad
}
```

### Closures — Loop Variable Capture (Version-Dependent)

**Go 1.22+ (current default):** loop variables are per-iteration — the classic bug is gone, no `i := i` needed:
```go
for i := range 5 {
    go func() { fmt.Println(i) }() // Go 1.22+: prints 0..4, each iteration owns its i
}
```

**Go ≤1.21 (legacy modules):** every closure shared ONE variable:
```go
// BUG on Go ≤1.21: prints 5,5,5,5,5 — all goroutines observe the final i
for i := 0; i < 5; i++ {
    go func() { fmt.Println(i) }()
}
// FIX on ≤1.21: shadow per iteration
for i := 0; i < 5; i++ {
    i := i
    go func() { fmt.Println(i) }()
}
```

The semantics follow the module's `go` directive. On `go 1.21` or lower the shadow is still required; `go vet`'s `loopclosure` flags the old pattern.

### Interface Nil Footgun (Critical)
```go
// BUG: (*MyError)(nil) wrapped in interface is NOT nil
func getError() error {
    var err *MyError = nil
    return err // returns non-nil interface!
}

// FIX: return untyped nil
func getError() error {
    return nil
}
```

### Compile-Time Interface Check
```go
// Fails at compile time if *Server doesn't implement http.Handler
var _ http.Handler = (*Server)(nil)
```

---

## Type System (M2 — Interfaces & Composition)

### The Golden Rule
```
Accept interfaces → Return concrete types
```

### Interface Design
- Keep interfaces small — `io.Reader` has one method for a reason
- Define interfaces at the point of use, not at the point of definition
- Avoid empty `interface{}` / `any` unless truly necessary

### Functional Options Pattern
```go
type ServerOption func(*Server)

func WithTimeout(d time.Duration) ServerOption {
    return func(s *Server) { s.timeout = d }
}

func NewServer(addr string, opts ...ServerOption) *Server {
    s := &Server{addr: addr, timeout: 30 * time.Second} // safe defaults
    for _, opt := range opts {
        opt(s)
    }
    return s
}
```

### Generics — When to Use
- Use generics when the logic is truly type-agnostic (collections, algorithms)
- Use interfaces when behavior varies by type
- Never use generics to avoid writing a 5-line function twice

### Composition via Embedding
Go has no inheritance. **Embed a struct** to promote its fields and methods (reuse behavior); **embed an interface** to build decorators/wrappers. Prefer composition over any inheritance-shaped design. See `references/interfaces-composition.md`.

### Enums — iota + Stringer
Use `iota` for enumerated constants and generate their `String()` with `stringer` — never magic strings or ints. See `references/idioms-and-style.md`.

---

## Strings, Runes & UTF-8 (M2 — Critical Footguns)

Go strings are **immutable byte sequences**, not character sequences. A `rune` is an alias for `int32` representing a Unicode code point. The difference matters the moment you leave ASCII.

### The Core Footgun

```go
s := "café"
fmt.Println(len(s))        // 5 — bytes, NOT characters ("é" is 2 bytes in UTF-8)
fmt.Println(len([]rune(s))) // 4 — Unicode code points (what humans call "characters")
```

### Iteration — Bytes vs Runes

```go
s := "café"

// BAD: iterates over bytes — breaks multi-byte characters
for i := 0; i < len(s); i++ {
    fmt.Printf("%c", s[i]) // "cafÃ©" — garbage for non-ASCII
}

// GOOD: range on string iterates over runes automatically
for i, r := range s {
    fmt.Printf("index=%d rune=%c\n", i, r)
    // index=0 rune=c
    // index=1 rune=a
    // index=2 rune=f
    // index=3 rune=é  ← i jumps from 3 to 5 (é is 2 bytes)
}
```

### String Building — Never Concatenate in Loops

```go
// BAD: O(n²) — each += allocates a new string
result := ""
for _, s := range parts {
    result += s
}

// GOOD: strings.Builder — one allocation, amortized O(n)
var b strings.Builder
b.Grow(estimatedSize) // optional but reduces re-allocations
for _, s := range parts {
    b.WriteString(s)
}
result := b.String()
```

### strings vs bytes — When to Use Each

```go
// strings.Builder  → building a final string from parts
// bytes.Buffer     → when you need both io.Reader AND io.Writer on the same buffer
//                    (e.g., building data to pass to an HTTP client)

var buf bytes.Buffer
json.NewEncoder(&buf).Encode(payload) // writes into buffer as Writer
http.Post(url, "application/json", &buf) // reads from buffer as Reader
```

### Key stdlib Functions

```go
strings.Contains(s, substr)       // never index-compare manually
strings.TrimSpace(s)              // remove \t \n \r at edges
strings.Split(s, sep)             // returns []string; use SplitN to limit
strings.Join(parts, sep)          // inverse of Split
strings.ReplaceAll(s, old, new)   // replace all occurrences
strings.ToLower / ToUpper         // locale-independent ASCII case change
strings.HasPrefix / HasSuffix     // cleaner than s[:n] == prefix

// For UTF-8 aware operations:
utf8.RuneCountInString(s)         // number of runes (not len(s))
utf8.ValidString(s)               // validate before processing untrusted input
```

### Footgun: String → []byte → String Allocation

```go
// Every conversion allocates — avoid in hot paths
b := []byte(s)   // allocates
s2 := string(b)  // allocates again

// Exception: the compiler optimizes away the allocation in these specific patterns:
m[string(b)]         // map lookup with []byte key: zero alloc
strings.HasPrefix(s, string(b))  // comparison: zero alloc
```

---

## Packages & Project Layout (M2 / Architecture)

> Read `references/project-layout.md` for the full Package Oriented Design guide.

### The One-Line Rule
A package must be describable in one sentence starting with its name:  
`"Package user defines the user concept and owns its persistence and HTTP surface."`  
If you cannot write that sentence, the package has no identity.

### Never Create These Packages
`util` · `helper` · `common` · `shared` · `models` · `types` · `base` · `manager`

These are not packages — they are drawers. Every type and function that would go in `util` belongs in a package named after the concept it serves.

### Files Named by Intent
The package names the concept; the **file** names the intent. Inside a bounded context, each use case lives in its own `snake_case` file named after what it does (`login.go`, `signup.go`, `search_users.go`) and holds **one** struct with an action name (`Login`), a `NewLogin(deps...)` constructor and a single exported `Execute(ctx, in LoginInput) (LoginOutput, error)`, with its DTOs and narrow ports beside it. A file never holds two use cases.

Never inside a context: `service.go` · `services.go` · `usecase.go` · `usecases.go` · `use_cases.go` · `application.go` · `app.go` · `manager.go` · `helpers.go` · `utils.go` · `common.go` · a `handler.go`/`handlers.go` serving several endpoints (transport: one file per endpoint + `routes.go`). Never the grab-bag types `Service`, `UseCase`, `Manager`. Allowed: concepts (`user.go`), `errors.go`, a shared port by name (`user_repository.go`), `store.go`, `routes.go`, and composition roots outside the contexts (`internal/handlers/handlers.go` that only mounts routes). See `references/project-layout.md` ("Nombres de Archivo por Intención").

### Flat, Not Nested
Nesting a package inside another creates **no** hierarchy and **no** encapsulation — visibility in Go is per-package, not per-folder. Only `internal/` is a compiler-enforced boundary. Default to a flat layout. See `references/project-layout.md`.

---

## Dependencies (Stdlib-First)

The standard library is the first answer. Every dependency is a liability: transitive deps, CVEs, supply-chain risk, build time, and breaking changes you don't control.

Before adding one, ask whether the stdlib or `golang.org/x/...` already does it. Then audit: `go mod why <pkg>`, `go mod graph`, `govulncheck ./...`. Prefer small composable libraries over frameworks that take over control flow. See `references/dependencies-and-stdlib.md`.

---

## Concurrency (M3 — Goroutines, Channels & Patterns)

> Read `references/channels-fundamentals.md` for channel semantics (buffered vs unbuffered, `select`, the memory model, data races) and `references/concurrency-patterns.md` for pattern implementations.

### Non-Negotiable Rules
1. **Never start a goroutine without knowing how it ends** — use `context`, `done` channels, or `WaitGroup`
2. **Never share memory by communicating** — use channels; or protect shared state with `sync.Mutex`
3. **Always run `go test -race`** on concurrent code
4. **Verify with `goleak`** in test teardown for goroutine leak detection

### Channel Ownership
- The goroutine that creates a channel owns closing it
- Receivers must never close a channel
- A closed channel can still be read (returns zero values + `ok=false`)

### Context Propagation
```go
// ALWAYS: context is the first parameter
func FetchUser(ctx context.Context, id int64) (*User, error) {
    // pass ctx to every downstream call
    return db.QueryContext(ctx, "SELECT ...", id)
}
```

### Goroutine Leak Detection in Tests
```go
func TestMyWorker(t *testing.T) {
    defer goleak.VerifyNone(t)
    // ... your test
}
```

---

## I/O & Streams (M4 — io Package)

> Read `references/io-streams.md` for full coverage: io.Reader contract (all 4 scenarios), bufio, TeeReader, MultiWriter, LimitReader, io.Pipe, bytes.Buffer vs strings.Builder, and the unified model across os.File / net/http.

### Three Rules to Never Break
1. **Always buffer syscalls** — `bufio.NewReader/Writer` wrapping any file or network stream
2. **Always `defer bw.Flush()`** — immediately after `bufio.NewWriter`, before any writes
3. **Always `io.LimitReader`** — before reading from any external source (HTTP body, uploads)

### The One-Liner Principle
`io.Copy(dst, src)` moves data between any Reader and any Writer. If your code needs more than this for simple data movement, you're fighting the stdlib.

---

## Error Handling (M6 — Production Go)

### Wrapping — Always Add Context
```go
// BAD: loses context
if err != nil {
    return err
}

// GOOD: wraps with context, preserves chain
if err != nil {
    return fmt.Errorf("fetchUser id=%d: %w", id, err)
}
```

### Inspection — Never Use ==
```go
// BAD: breaks with wrapped errors
if err == sql.ErrNoRows { ... }

// GOOD: traverses the error chain
if errors.Is(err, sql.ErrNoRows) { ... }

// GOOD: extract typed error
var pgErr *pgconn.PgError
if errors.As(err, &pgErr) {
    fmt.Println(pgErr.Code)
}
```

### Sentinel Errors
```go
// Define at package level, exported
var ErrNotFound = errors.New("not found")
var ErrUnauthorized = errors.New("unauthorized")
```

### Goroutine Errors — Use errgroup
```go
g, ctx := errgroup.WithContext(ctx)
g.Go(func() error { return fetchA(ctx) })
g.Go(func() error { return fetchB(ctx) })
if err := g.Wait(); err != nil {
    return fmt.Errorf("pipeline: %w", err)
}
```

---

## Databases (M7)

> Read `references/databases.md` for full coverage: database/sql pool internals, pgx nativo con pgxpool, sqlc workflow, transacciones con retry, Repository pattern, y migrations embebidas con go:embed.

### Three Rules to Never Break
1. **Always configure the pool** — `MaxOpenConns`, `MaxIdleConns`, `ConnMaxLifetime` before first use
2. **Always `defer tx.Rollback()`** — immediately after `BeginTx`; it's a no-op if `Commit` succeeds
3. **Translate DB errors at the boundary** — callers receive domain errors (`ErrNotFound`), never `pgconn.PgError`

---

## Testing (M8)

### Table-Driven Tests — Canonical Form
```go
func TestAdd(t *testing.T) {
    tests := []struct {
        name    string
        a, b    int
        want    int
    }{
        {"positive", 1, 2, 3},
        {"negative", -1, -2, -3},
        {"zero", 0, 0, 0},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            got := Add(tt.a, tt.b)
            if got != tt.want {
                t.Errorf("Add(%d,%d) = %d, want %d", tt.a, tt.b, got, tt.want)
            }
        })
    }
}
```

### Mock via Interface (No Frameworks Needed)
```go
type mockRepo struct {
    users map[int64]*User
}
func (m *mockRepo) GetByID(_ context.Context, id int64) (*User, error) {
    u, ok := m.users[id]
    if !ok {
        return nil, ErrNotFound
    }
    return u, nil
}
```

### Benchmark Template
```go
func BenchmarkParse(b *testing.B) {
    data := loadTestData()
    b.ResetTimer()
    b.ReportAllocs()
    for range b.N {
        Parse(data)
    }
}
```

---

## Security (M10)

### Crypto Non-Negotiables
```go
// ALWAYS crypto/rand, NEVER math/rand for security values
token := make([]byte, 32)
if _, err := io.ReadFull(rand.Reader, token); err != nil {
    return err
}

// ALWAYS subtle.ConstantTimeCompare for token comparison
if subtle.ConstantTimeCompare(provided, stored) != 1 {
    return ErrUnauthorized
}
```

### Secrets — Never in Code or Logs
```go
// BAD
log.Printf("connecting with password %s", password)

// GOOD: redact in logs; load from env/vault
type Secret string
func (s Secret) String() string { return "[REDACTED]" }
```

---

## Comments — Few, Short, and About "Why"

Well-named code already says **what** it does. Generated or reviewed code must not carry more comments than code (aim for ≤ 10–15 % comment lines).

- **Yes:** a one-line package doc; a one-line doc comment on exported identifiers that adds what the name cannot (units, invariant, returned error, concurrency) — `revive` wants it; the non-obvious **why** (external constraint, workaround + issue, measured perf decision); directives (`//go:generate`, `//nolint:x // reason`).
- **No:** narrating the code (`// call the repo`), restating the name (`// NewUser creates a new User.` — on unexported identifiers just omit it), banners (`// -----`, `// ==== Handlers ====`), comments longer than the code they sit on, commented-out code, `TODO` without owner or issue.

Design explanations go to the README or an ADR, not above every function. Full policy with BAD/GOOD: `references/idioms-and-style.md` §5.

---

## Common Footguns — Quick Reference

| Footgun | Symptom | Fix |
|---|---|---|
| Interface nil | `if err != nil` is true but behavior is nil | Return untyped `nil` |
| Closure capture (Go ≤1.21) | Loop var shared across goroutines | Shadow `x := x`; fixed by default in Go 1.22+ |
| Goroutine leak | Memory grows unbounded | `context` + `goleak` in tests |
| Missing `Flush()` | Buffered writes silently lost | `defer bw.Flush()` after `bufio.NewWriter` |
| `time.After` in loop | Goroutine leak per iteration | Use `time.NewTimer` + `Reset` |
| `==` on errors | Wrapped errors not matched | `errors.Is` / `errors.As` |
| Wrong struct field order | Silent memory waste | Largest fields first |
| `math/rand` for tokens | Predictable security tokens | `crypto/rand` always |
| Nil map write | Runtime panic | `make(map[K]V)` before use |
| Forgetting `defer tx.Rollback()` | Transaction never released on error | Always defer immediately after BeginTx |
| `len(s)` on UTF-8 string | Counts bytes, not characters | `utf8.RuneCountInString(s)` |
| `s[i]` on multi-byte string | Gets byte, not rune | `range s` to iterate runes |
| String concat in loop | O(n²) allocations | `strings.Builder` with `Grow` |
| Slice `append` aliasing | Mutating one slice changes another | Three-index `s[a:b:c]`; see value-vs-reference.md |
| Concurrent map access | Fatal `concurrent map writes` panic | `sync.Mutex` / `sync.Map`; never share a plain map for writes |
| `range` copies the element | `for _, v := range xs { v.X=… }` silently no-ops | Index instead: `xs[i].X = …` |

---

## Code Review Checklist

Before finalizing any Go output, verify:

- [ ] `context.Context` is the first parameter in all functions that do I/O or concurrency
- [ ] All errors are wrapped with `fmt.Errorf("...: %w", err)` and never silently dropped
- [ ] Every goroutine has a clear termination condition
- [ ] Interfaces are small and defined at the point of use
- [ ] No `sync.Mutex` fields are copied (use pointer receivers)
- [ ] `defer` used correctly for cleanup (not inside loops)
- [ ] Buffered I/O used whenever reading/writing files or network streams
- [ ] `go test -race` would pass
- [ ] Compile-time interface checks present for exported types (`var _ Iface = (*T)(nil)`)
- [ ] String iteration uses `range` (runes), not index (bytes), when handling non-ASCII
- [ ] No package named `util`, `helper`, `common`, `models`, or `types`
- [ ] Each package can be described in one sentence starting with its name
- [ ] No package nesting used to fake hierarchy; dependencies form a DAG (domain defines interfaces)
- [ ] No external dependency the stdlib or `golang.org/x/...` already covers; new deps audited with `govulncheck`
- [ ] Value vs pointer receivers are consistent within a type; no `sync.Mutex` copied by value
- [ ] One use case per file, named by intent (`login.go`, `search_users.go`); no `service.go`/`usecase.go`/`manager.go`/`utils.go`, no `Service`/`UseCase`/`Manager` types
- [ ] Comments only where they add a "why" or what the name cannot say; no narration, banners or doc comments that restate the identifier

---

## Reference Files

Read the relevant file when the user's question is specifically about that domain. Do not load all files at once.

- `references/channels-fundamentals.md` — concurrency vs parallelism, channel axioms (nil/closed), buffered vs unbuffered, `select`, memory model / happens-before, data races
- `references/concurrency-patterns.md` — Worker Pool, Pipeline, Fan-Out/In, Semaphore, Errgroup, Or-Done, Heartbeat, Timeout, sync.Cond, Backpressure, Rate Limiter, Singleflight, sync.Pool
- `references/value-vs-reference.md` — pass-by-value always, slice header & `append` aliasing, map semantics, struct copy (shallow, copylocks, range copies), defensive copy
- `references/memory-pointers.md` — stack/heap, escape analysis, value vs pointer receivers, GC pressure, cache locality, pprof / -benchmem
- `references/interfaces-composition.md` — implicit satisfaction, consumer-defined interfaces, interface pollution, segregation, struct/interface embedding, method sets, nil-interface footgun
- `references/idioms-and-style.md` — naming (incl. file names), useful zero value, early return, iota+Stringer, minimal-comments policy + Example tests, defer subtleties, tooling
- `references/dependencies-and-stdlib.md` — stdlib-first, dependency auditing (`go mod why/graph`, `govulncheck`), avoiding frameworks, blessed deps
- `references/errors-advanced.md` — sentinel vs typed vs opaque, behavior interfaces, `errors.Join`, panic/recover at boundaries
- `references/io-streams.md` — io.Reader/Writer contract completo, bufio, TeeReader, MultiWriter, LimitReader, io.Pipe, bytes.Buffer vs strings.Builder
- `references/databases.md` — database/sql pool, pgx nativo, sqlc workflow, transacciones con retry, Repository pattern, migrations embebidas
- `references/stdlib-idioms.md` — net/http middleware, context patterns, OS signals, encoding, slog, time gotchas, toolchain
- `references/project-layout.md` — Package Oriented Design, canonical structure, intent-named files (one use case per file), `cmd/` / `internal/` / `platform/` rules, nested-package avoidance, dependency direction (DAG), anti-patterns
- `references/testing-advanced.md` — TestMain, testcontainers-go, fuzzing, golden files, test strategy pyramid
