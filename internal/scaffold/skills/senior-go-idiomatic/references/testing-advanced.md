# Advanced Testing — Go Idiomatic Reference

## Contents

- TestMain: package-wide setup and teardown
- Fixtures with `t.Helper`, `t.Cleanup` and `t.Context`
- Build tags: unit vs integration tests
- testcontainers-go: integration tests against real infrastructure
- `testing/synctest`: time and concurrency without real sleeps
- Benchmarks with `b.Loop`
- Fuzzing
- Golden files
- Test strategy: which test for which job

---

## TestMain — Package-Wide Setup and Teardown

`TestMain` is the entry point for a package's whole test binary. Reach for it only when
the package's tests share infrastructure that is expensive to build per test: a database
pool, a test server, temporary files.

```go
func TestMain(m *testing.M) {
	pool, cleanup, err := setupTestDatabase()
	if err != nil {
		log.Fatalf("setup database: %v", err)
	}
	testPool = pool

	m.Run() // runs every Test*, Benchmark*, Fuzz* and Example* in the package

	cleanup()
}
```

Since Go 1.15, when `TestMain` returns, the test binary exits with `m.Run`'s result, so
there is no need to call `os.Exit` yourself. Calling it is still legal, but it skips
pending `defer`s; if you do, run the teardown explicitly before it.

### When to use TestMain (and when not)

| Situation | TestMain? |
|---|---|
| Database pool shared by many tests | Yes |
| Shared HTTP test server | Yes |
| Process-wide environment for the whole package | Yes |
| Setup used by a single test | No — build it in the test and register `t.Cleanup` |
| Setup for a group of subtests | No — build it in the parent test before `t.Run` |

### Fixtures with t.Helper, t.Cleanup and t.Context

A fixture builds what one test needs and registers its own cleanup, so the test body
stays about behavior. `t.Context()` (Go 1.24) returns a context tied to the test: it is
canceled just before the `Cleanup` functions run, which is what you want for the work
inside the test and exactly what you do not want inside a cleanup. Cleanups therefore use
`context.Background()`.

**internal/user/pgstore/fixtures_test.go**
```go
func newTestUser(t *testing.T, store *pgstore.Store) *user.User {
	t.Helper() // failures are reported at the caller's line

	id := user.ID(rand.Int64()) // math/rand/v2: avoids collisions between parallel tests
	u, err := user.NewUser(id, fmt.Sprintf("user%d@example.com", id), time.Now())
	if err != nil {
		t.Fatalf("new user: %v", err)
	}
	if err := store.Save(t.Context(), u); err != nil {
		t.Fatalf("save user: %v", err)
	}

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", u.ID())
	})
	return u
}
```

Cleanup registered inside a helper belongs to the calling test and runs when that test
(including its subtests) finishes.

---

## Build Tags — Separating Unit from Integration Tests

Integration tests are slow and need Docker or a network, so they sit behind an explicit
tag and do not run on every `go test ./...`.

**internal/user/pgstore/store_integration_test.go**
```go
//go:build integration

package pgstore_test
```

```bash
go test ./...                    # unit tests only: the fast default
go test -tags integration ./...  # unit + integration (full CI, or locally on demand)
```

---

## testcontainers-go — Integration Tests Against Real Infrastructure

`testcontainers-go` starts real Docker containers from the test binary. Your persistence
code runs against a real PostgreSQL instead of a hand-written fake, which is the only way
to check real SQL, constraints and transaction behavior. The `postgres` module's `Run`
replaces the deprecated `RunContainer`.

**internal/user/pgstore/main_test.go**
```go
//go:build integration

package pgstore_test

import (
	"context"
	"log"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	ctx := context.Background()

	pg, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		log.Fatalf("start postgres: %v", err)
	}

	// The host port is random on every run; always ask the container for the DSN.
	dsn, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		log.Fatalf("connection string: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		log.Fatalf("open pool: %v", err)
	}
	if err := runMigrations(dsn); err != nil {
		log.Fatalf("migrations: %v", err)
	}
	testPool = pool

	m.Run()

	pool.Close()
	if err := testcontainers.TerminateContainer(pg); err != nil {
		log.Printf("terminate postgres: %v", err)
	}
}
```

The store under test lives in the infrastructure subpackage (`internal/user/pgstore`),
not in the domain package; the test exercises it through its concrete type.

**internal/user/pgstore/store_integration_test.go**
```go
//go:build integration

package pgstore_test

func TestStore_SaveAndFind(t *testing.T) {
	t.Parallel()

	store := pgstore.New(testPool)
	u := newTestUser(t, store)

	got, err := store.FindByID(t.Context(), u.ID())
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got.Email() != u.Email() {
		t.Errorf("email = %q, want %q", got.Email(), u.Email())
	}
}
```

---

## testing/synctest — Time and Concurrency Without Real Sleeps

`testing/synctest` (stable since Go 1.25) runs a function inside a *bubble*: goroutines
started there use a fake clock that starts at a fixed instant and jumps forward only when
every goroutine in the bubble is durably blocked. A test of a 5-second timeout finishes in
microseconds and is deterministic, instead of sleeping for real or racing a short timeout.

- `synctest.Test(t, func(t *testing.T) { ... })` runs the body in a new bubble and waits
  for all its goroutines to exit. The `t` inside is bubble-aware, including `t.Context()`.
- `synctest.Wait()` blocks until every other goroutine in the bubble is durably blocked —
  the point where it is safe to assert on state.
- Channels, timers and contexts created inside the bubble belong to it; blocking on
  something from outside (network I/O, a channel made outside) is not "durable" and the
  clock will not advance.

```go
func WaitForResult(ctx context.Context, results <-chan int, timeout time.Duration) (int, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case v := <-results:
		return v, nil
	case <-timer.C:
		return 0, ErrTimeout
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}
```

```go
func TestWaitForResultTimesOut(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		results := make(chan int) // created in the bubble: nobody sends
		start := time.Now()

		_, err := WaitForResult(t.Context(), results, 5*time.Second)

		if !errors.Is(err, ErrTimeout) {
			t.Fatalf("err = %v, want ErrTimeout", err)
		}
		if got := time.Since(start); got != 5*time.Second { // exact: the clock is fake
			t.Errorf("waited %v, want exactly 5s", got)
		}
	})
}

func TestContextDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()

		time.Sleep(time.Second - time.Nanosecond)
		synctest.Wait()
		if err := ctx.Err(); err != nil {
			t.Fatalf("expired early: %v", err)
		}

		time.Sleep(time.Nanosecond)
		synctest.Wait()
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatalf("ctx.Err() = %v, want DeadlineExceeded", ctx.Err())
		}
	})
}
```

Use it for timeouts, retries with backoff, tickers, caches with expiry and anything that
would otherwise need `time.Sleep` in a test.

---

## Benchmarks with b.Loop

Since Go 1.24, write benchmarks with `for b.Loop()` instead of `b.ResetTimer()` plus
`for range b.N`:

```go
func BenchmarkParse(b *testing.B) {
	data := loadTestData() // outside the loop: not timed, no ResetTimer needed
	b.ReportAllocs()
	for b.Loop() {
		Parse(data)
	}
}
```

Why it is better: the timer covers only the loop, so setup before it is excluded
automatically; the benchmark function runs once per `-count` rather than once per
calibration round, so expensive setup is not repeated; and the arguments and results of
calls in the loop body are kept alive, so the compiler cannot optimize the measured work
away. Run with `go test -bench=. -benchmem`; see `references/memory-pointers.md` for
reading `allocs/op`.

---

## Fuzzing — Finding Bugs Hand-Written Cases Miss

Fuzzing mutates a seed corpus to find inputs that panic, hang or break an invariant.

```go
func FuzzParseConfig(f *testing.F) { // the name must start with Fuzz
	// Seed corpus: starting points the fuzzer mutates.
	f.Add([]byte(`{"host":"localhost","port":5432}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(``))

	f.Fuzz(func(t *testing.T, data []byte) {
		cfg, err := parseConfig(data)
		if err != nil {
			return // rejecting bad input is fine; panicking is the bug
		}
		// Invariant: whatever parses can be encoded again.
		if _, err := json.Marshal(cfg); err != nil {
			t.Errorf("re-encode parsed config: %v", err)
		}
	})
}
```

### Invariant (round-trip) fuzzing

```go
func FuzzCompress(f *testing.F) {
	f.Add([]byte("hello world"))
	f.Add([]byte(""))
	f.Add(make([]byte, 1000))

	f.Fuzz(func(t *testing.T, original []byte) {
		compressed, err := compress(original)
		if err != nil {
			t.Fatalf("compress: %v", err)
		}
		recovered, err := decompress(compressed)
		if err != nil {
			t.Fatalf("decompress valid data: %v", err)
		}
		if !bytes.Equal(original, recovered) {
			t.Fatalf("round trip:\noriginal:  %q\nrecovered: %q", original, recovered)
		}
	})
}
```

Good fuzz targets: parsers of any format, functions fed by users or the network,
encode/decode pairs (round-trip invariants), and numeric code with many edge cases.

```bash
go test -fuzz=FuzzParseConfig -fuzztime=30s   # fuzz for 30s, growing the corpus
go test -run=FuzzParseConfig                  # seeds + saved corpus only: fast, for CI
```

Failing inputs are saved under `testdata/fuzz/FuzzParseConfig/`. Commit them: they become
permanent regression cases that plain `go test` replays.

---

## Golden Files — Snapshot Testing

A golden file stores the expected output on disk. It suits large or structured output
(JSON responses, rendered HTML, reports) where an inline `want` would be unreadable.

```go
var update = flag.Bool("update", false, "rewrite golden files")

func TestGenerateReport(t *testing.T) {
	got := GenerateReport(testData)
	goldenPath := filepath.Join("testdata", "report.golden")

	if *update { // go test -run TestGenerateReport -update
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with -update to create it): %v", err)
	}
	if got != string(want) {
		t.Errorf("report mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}
```

Declare the flag at package level in a test file; the test binary parses it before
running tests. Review the diff of regenerated golden files like any other code change.

---

## Test Strategy — Which Test for Which Job

| Situation | Test type | Tool |
|---|---|---|
| Pure logic, no dependencies | Unit, table-driven | `testing` |
| Code with external dependencies | Unit with a hand-written fake behind a consumer interface | small interface |
| Parser or large input space | Fuzz | `testing.F` |
| Timeouts, retries, tickers | Unit in a bubble | `testing/synctest` |
| Performance-critical path | Benchmark | `b.Loop` + `-benchmem` |
| Large or rarely changing output | Golden file | `testdata/` |
| Full flow against real infrastructure | Integration | testcontainers-go |
| Concurrency | Unit + `-race` + leak check | `testing` + goleak |

Pyramid: many fast unit tests, a few slow integration tests. Container-based tests are
slow, but nothing else verifies real SQL, transactions and network behavior.
