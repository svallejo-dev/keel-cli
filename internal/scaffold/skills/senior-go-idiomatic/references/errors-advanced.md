# Advanced Error Handling — Go Idiomatic Reference

The base rules live in the error-handling section of `SKILL.md`: wrap with `%w` when it
adds context, inspect with `errors.Is` / `errors.As`, sentinel errors, and `errgroup` for
goroutines. They are not repeated here. This file covers the decisions on top of them:
opaque vs sentinel vs typed errors, behavior interfaces, aggregation with `errors.Join`,
and `panic` / `recover` (including goroutine boundaries).

---

## Opaque vs Sentinel vs Typed

Three ways to expose an error, in increasing order of coupling with the caller. This is an
API decision, not a style one: it fixes how much the caller may depend on your internals.

| Form | Exposes | The caller can… | Use it when |
|---|---|---|---|
| **Opaque** (default) | Only the message | Log it or propagate it | The caller has no reason to tell this error apart |
| **Sentinel** | An identity (`var ErrX`) | Compare with `errors.Is` | There is a discrete condition the caller branches on (`ErrNotFound`) |
| **Typed** | A type with data | Extract fields with `errors.As` | The caller needs **data** from the error (invalid field, code, retry-after) |

Start opaque. Promote to sentinel or typed only when a real caller needs to branch on the
error: every identity and every field you expose becomes public API and ties your
consumers to your implementation.

### Opaque — the default

```go
// Callers can log or propagate this, not inspect it, so the internals can change freely.
func loadConfig(path string) (*Config, error) {
	if _, err := os.ReadFile(path); err != nil {
		return nil, fmt.Errorf("load config %s: %v", path, err)
	}
	return &Config{}, nil
}
```

The verb is the decision: `%w` makes the wrapped error part of your contract (callers can
`errors.Is` it), `%v` keeps only its text. Wrap with `%w` when you are willing to support
the cause as API — for example a sentinel of your own package — and use `%v` for
implementation details you may swap later.

### Sentinel — a condition the caller compares

```go
// ErrNotFound is returned when no user matches; compare with errors.Is.
var ErrNotFound = errors.New("user: not found")
```

### Typed — when the caller needs data

```go
type ValidationError struct {
	Field string
	Msg   string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation: field %q: %s", e.Field, e.Msg)
}

// The caller extracts data, not just identity.
var vErr *ValidationError
if errors.As(err, &vErr) {
	respondBadRequest(w, vErr.Field)
}
```

---

## Behavior Interfaces (Behavior, Not Type)

Exposing a concrete type (`errors.As` into `*MyError`) ties the caller to your struct.
Exposing **behavior** does not: the caller runs `errors.As` against a small interface it
defines itself, and you implement that method on an unexported type. For cross-cutting
conditions — retryable, timeout — expose a method, not a type; any error from any package
that implements the method then classifies.

Do not model new code on `net.Error`'s `Temporary()`: it has been deprecated since Go 1.18
because "temporary" was never well defined. `Timeout() bool` is still fine; for retry
decisions, define the behavior you actually mean.

```go
// The package that INSPECTS declares the minimal interface it cares about.
type retryable interface {
	Retryable() bool
}

func IsRetryable(err error) bool {
	var r retryable
	return errors.As(err, &r) && r.Retryable()
}
```

```go
// The package that PRODUCES implements the behavior without exporting the type.
// Unwrap keeps the chain intact for errors.Is / errors.As.
type transientError struct {
	err error
}

func (e *transientError) Error() string   { return e.err.Error() }
func (e *transientError) Unwrap() error   { return e.err }
func (e *transientError) Retryable() bool { return true }
```

`errors.As(err, &r)` with an interface-typed target walks the chain and matches the first
error that has the method. Neither side knows the other's concrete type.

---

## errors.Join — Aggregating Several Errors

`errors.Join` (Go 1.20) combines errors into one. It returns `nil` when every argument is
`nil`, so passing a possibly empty slice is safe. The result implements
`Unwrap() []error`, so `errors.Is` / `errors.As` inspect **every** joined error, not only
the first.

### Validating N fields — collect instead of failing on the first

```go
func validateUser(u User) error {
	var errs []error
	if u.Name == "" {
		errs = append(errs, errors.New("name is required"))
	}
	if u.Age < 0 {
		errs = append(errs, errors.New("age must be non-negative"))
	}
	return errors.Join(errs...) // nil when errs is empty
}
```

### Closing N resources — keep every cleanup error

```go
// Do not stop at the first failing Close: try them all.
func closeAll(closers []io.Closer) error {
	var err error
	for _, c := range closers {
		err = errors.Join(err, c.Close()) // nil arguments are dropped
	}
	return err
}
```

### Inspecting — errors.Is sees all of them

```go
err := errors.Join(ErrNotFound, io.EOF)
errors.Is(err, ErrNotFound) // true
errors.Is(err, io.EOF)      // true — both, not only the first
```

`errors.Join` does not replace `%w`: wrap **one** cause with `%w`; join **sibling** errors.

---

## panic / recover — Correct Use

In library code, `panic` is not for expected failures. Invalid input, a missing resource
or a network failure is returned as an `error`, because the caller is the one who knows
how to react. `panic` is for:

1. **Programming bugs** — a broken invariant that means the code itself is wrong (an
   impossible state, the `default:` of a switch that should be exhaustive).
2. **Impossible configuration at startup** — the process cannot work and should stop
   immediately (`regexp.MustCompile`, `template.Must` on a literal).

```go
// Acceptable: an invalid literal pattern is a programmer bug, caught at process start.
var reEmail = regexp.MustCompile(`^\S+@\S+$`)
```

### BAD — panic as control flow

```go
// Invalid input is an expected condition, not a bug; this takes the caller down.
func parsePort(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		panic("invalid port")
	}
	return n
}
```

### GOOD — return the error

```go
func parsePort(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("parse port %q: %w", s, err)
	}
	return n, nil
}
```

### recover only at boundaries

Put `recover` at a boundary where one panic must not take down the process: an HTTP
handler, a goroutine supervisor, the root of a worker. Using it in the middle of business
logic to "handle" expected errors hides bugs.

```go
// Turns a panic into a 500 plus a log line instead of crashing the server.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				log.Printf("panic recovered: %v\n%s", v, debug.Stack())
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
```

`net/http` already recovers panics in handlers and logs them, but it drops the
connection without a response; a middleware like this one returns a proper 500.

---

## Errors Crossing Goroutines

`recover` only catches panics from **its own** goroutine. A panic that escapes a goroutine
without a `recover` crashes the whole process — the middleware's or the caller's `recover`
cannot reach it, because it runs on a different stack.

So every goroutine that runs arbitrary work needs its own `recover` at its boundary, or a
wrapper that turns a panic into an error before it crosses.

```go
// BAD: if process panics, the process dies; no outer recover can catch it.
go func() {
	process(job)
}()

// GOOD: recover inside the goroutine; the panic becomes an error sent on a channel with
// buffer 1, so the send never blocks and the goroutine cannot leak.
func safeGo(fn func() error) <-chan error {
	errc := make(chan error, 1)
	go func() {
		defer func() {
			if v := recover(); v != nil {
				errc <- fmt.Errorf("panic in goroutine: %v", v)
			}
		}()
		errc <- fn()
	}()
	return errc
}
```

To propagate **errors** (not panics) from several goroutines with cancellation on the
first failure, use `errgroup` — see the Errgroup section of
`references/concurrency-patterns.md`. `errgroup` does not recover panics: if a function
passed to `g.Go` can panic, wrap it with a `recover` like the one above.

---

## Checklist

- [ ] Errors are opaque by default; sentinel or typed only when a real caller branches on them
- [ ] `%w` only for causes you are willing to support as API; `%v` for implementation details
- [ ] Sentinels (`var ErrX`) for conditions compared with `errors.Is`; typed errors (`errors.As`) only when the caller needs data
- [ ] Cross-cutting conditions (retryable, timeout) exposed as behavior, not as a concrete type; no new `Temporary()`
- [ ] Error types that wrap a cause implement `Unwrap()` to keep the chain
- [ ] `errors.Join` for multi-field validation and multi-resource cleanup; it does not replace `%w`
- [ ] `panic` only for programming bugs or impossible startup configuration, never as control flow
- [ ] `recover` only at boundaries (HTTP handler, goroutine supervisor)
- [ ] Every goroutine running arbitrary work has its own `recover` or converts panics into errors; errors across goroutines go through `errgroup`
