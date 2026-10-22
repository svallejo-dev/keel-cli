# Dependencies: Explicit, Injected, Wired in `main`

A component's dependencies should be **visible in its signature** and **replaceable from
outside**. In Go that needs no DI framework or container: pass values through parameters or
constructors and declare small interfaces at the consumer. The graph stays explicit, testable
and free of hidden startup magic. No rule here justifies unnatural Go (a reflective DI
container "because the principle says so"); when they clash, the Go idiom wins.

## 1. Explicit dependencies

**Rule:** pass every dependency through a parameter or the constructor. Never reach it
through a global, a singleton or a service locator.

**Why:** the signature becomes the honest documentation of what the component needs.
`NewGreetUser(users, clock)` declares its whole graph; a `NewGreetUser()` that reads
`db.Global` and calls `time.Now()` inside lies about its requirements and cannot be swapped.

**greet_user.go**

```go
package user

import (
	"context"
	"fmt"
	"time"
)

// Clock is the only source of "now" for code that must be testable.
type Clock interface {
	Now() time.Time
}

// userFinder is the one method GreetUser needs from storage.
type userFinder interface {
	FindByID(ctx context.Context, id UserID) (*User, error)
}

// GreetUserInput identifies who to greet.
type GreetUserInput struct {
	UserID UserID
}

// GreetUserOutput is the rendered greeting.
type GreetUserOutput struct {
	Message string
}

// GreetUser builds a timestamped greeting for an existing user.
type GreetUser struct {
	users userFinder
	clock Clock
}

// NewGreetUser receives storage and time explicitly; nothing is read from globals.
func NewGreetUser(users userFinder, clock Clock) *GreetUser {
	return &GreetUser{users: users, clock: clock}
}

// Execute wraps storage errors with the use case name.
func (uc *GreetUser) Execute(ctx context.Context, in GreetUserInput) (GreetUserOutput, error) {
	u, err := uc.users.FindByID(ctx, in.UserID)
	if err != nil {
		return GreetUserOutput{}, fmt.Errorf("greet user: %w", err)
	}
	return GreetUserOutput{Message: fmt.Sprintf("hi %s at %s", u.Name(), uc.clock.Now().Format(time.RFC3339))}, nil
}
```

Each collaborator is replaced by a test double without touching `GreetUser`. The narrow
`userFinder` interface is declared by its consumer, which is §4 in action. (The use-case shape
itself, one type per file with `Execute(ctx, XInput) (XOutput, error)`, is defined by the
`ddd-go-idiomatic` skill.)

## 2. No mutable globals, singletons or service locators

**Rule:** no mutable package-level `var` used as a dependency. No singletons, no service
locator (a global `Get[T]()` that resolves collaborators).

**Why, concretely in Go:** a mutable global shared between goroutines is a **real data
race**: `go test -race` reports it and fails the run. It also breaks test isolation: state
survives between cases, so results depend on **execution order** and tests cannot run in
parallel.

```go
// BAD: a mutable global + functions that use it behind your back.
var DB *sql.DB // any goroutine reads and writes it

func GetUser(id int64) (User, error) {
	var u User
	err := DB.QueryRow("SELECT id, name FROM users WHERE id = $1", id).
		Scan(&u.ID, &u.Name) // depends on someone having initialized DB first
	return u, err
}
```

```go
// Store reads users from PostgreSQL; its methods use s.db, never a global.
type Store struct {
	db *sql.DB
}

// NewStore takes the pool from main, which owns its lifecycle.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}
```

### Why the global breaks tests

Tests that set up their scenario by mutating the global become coupled to each other:

```go
var reqCount int // global state the tests must touch

// BAD: two tests mutate the same global, so the result depends on ORDER.
func TestFirst(t *testing.T) {
	reqCount++
	if reqCount != 1 {
		t.Fatalf("got %d, want 1", reqCount) // fails if TestSecond ran first
	}
}

func TestSecond(t *testing.T) {
	reqCount++
	if reqCount != 2 { // coupled to TestFirst running just before
		t.Fatalf("got %d, want 2", reqCount)
	}
}
```

With the dependency injected, each test builds its own `Store`: fresh state per case,
isolated and parallelizable.

## 3. Package-level state that IS acceptable

Not every package-level value is an anti-pattern. The line is sharp: **immutable or
init-only, never shared mutable business state**.

| Acceptable | Why it is safe |
|---|---|
| `const` | Immutable by definition; no race possible |
| A `var` table never mutated after init | Read-only; shared between goroutines without locks |
| `sync.OnceValue` / `sync.Once` for idempotent init | One writer, once, with a memory barrier |

```go
// MaxRetries is fixed at compile time.
const MaxRetries = 3

var httpVerbs = [...]string{"GET", "POST", "PUT", "DELETE"} // read-only table

// slugRegexp compiles once, on first use, safely from any goroutine.
var slugRegexp = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile(`[a-z0-9-]+`)
})
```

The honest caveat: a configuration `var` that "is only written once at startup" is a trap;
nothing in the type guarantees it, and the next commit will mutate it at runtime. If it must
change at runtime, it is an injectable dependency, not a global.

## 4. Idiomatic dependency injection without a framework

**Rule:** in Go, "injecting" is just passing **small interfaces defined by the consumer**.
No DI container, no annotation tags; implicit interface satisfaction connects everything.

**Why:** the minimal interface belongs to the side that **depends** on the abstraction. The
consumer declares the one or two methods it uses; the implementer exports a concrete type
that satisfies it without importing the consumer. `GreetUser`'s `userFinder` (§1) is
satisfied by a `*postgres.Store` that has never heard of it, and in tests by a three-line
stub with the same method.

## 5. Clock and randomness as dependencies

**Rule:** inject a `Clock` (or a `func() time.Time`) and a random source. Do not call
`time.Now()` or the global `math/rand/v2` functions inside logic.

**Why:** `time.Now()` and global randomness are non-deterministic inputs baked into the code.
A test that checks "expires in 1h" cannot pin the result if the logic reads the system clock.
Injected, the test freezes them.

**token_issuer.go**

```go
package token

import (
	"math/rand/v2"
	"time"
)

// Clock is the only source of "now" for code that must be testable.
type Clock interface {
	Now() time.Time
}

// Issuer receives time and randomness instead of taking them from globals.
type Issuer struct {
	clock Clock
	rng   *rand.Rand
}

// NewIssuer is deterministic when given a fixed clock and a seeded *rand.Rand.
func NewIssuer(clock Clock, rng *rand.Rand) *Issuer {
	return &Issuer{clock: clock, rng: rng}
}

// Issue returns a random token ID and its expiry, ttl from now.
func (i *Issuer) Issue(ttl time.Duration) (id uint64, expires time.Time) {
	return i.rng.Uint64(), i.clock.Now().Add(ttl)
}
```

**token_issuer_test.go**

```go
package token

import (
	"math/rand/v2"
	"testing"
	"time"
)

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

func TestIssueIsDeterministic(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	issue := func() (uint64, time.Time) {
		return NewIssuer(fixedClock{start}, rand.New(rand.NewPCG(1, 2))).Issue(time.Hour)
	}

	id1, exp := issue()
	id2, _ := issue()
	if id1 != id2 {
		t.Errorf("same seed gave different ids: %d, %d", id1, id2)
	}
	if want := start.Add(time.Hour); !exp.Equal(want) {
		t.Errorf("expires = %v, want %v", exp, want)
	}
}
```

`math/rand/v2` (Go 1.22+) replaces `rand.NewSource`/`Int63` with explicit generators such as
`rand.NewPCG(seed1, seed2)`; a seeded `*rand.Rand` gives a reproducible sequence. For
security-sensitive tokens use `crypto/rand` instead, injected behind the same kind of seam.
For trivial cases a `now func() time.Time` field is lighter than a `Clock` interface.

## 6. Wiring in one place

**Rule:** the graph is assembled at the entry point (`main` / `cmd/<app>`). The rest of the
code does not know `main` or how anything is built; it receives its collaborators ready.

**Why:** concentrating construction in one point makes the graph auditable at a glance and
keeps business logic independent of startup. Imports flow `main → packages`, never the
reverse.

```go
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func main() {
	db, err := sql.Open("pgx", os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	greet := user.NewGreetUser(postgres.NewStore(db), systemClock{})
	out, err := greet.Execute(context.Background(), user.GreetUserInput{UserID: 1})
	if err != nil {
		log.Fatalf("greet: %v", err)
	}
	log.Println(out.Message)
}
```

As the graph grows it stays manual composition in `main`. If it becomes unmanageable,
`google/wire` generates that same code at compile time, with no reflection or runtime
container. The rule does not change: wiring lives at the entry point.

## Checklist

- [ ] Every dependency arrives through a parameter or constructor; the signature hides no requirement.
- [ ] No mutable package-level `var` as a dependency; no singletons or service locators.
- [ ] No global shared between goroutines (passes `go test -race`, keeps tests isolated).
- [ ] Package-level state is `const`, an immutable table or `sync.OnceValue`/`sync.Once`, never mutable business state.
- [ ] Dependency interfaces are minimal and defined by the **consumer**.
- [ ] `Clock`/`func() time.Time` and a `*rand.Rand` (`math/rand/v2`) are injected; logic never calls `time.Now()` or global rand.
- [ ] All wiring lives in `main`/`cmd`; no domain package imports the entry point.
- [ ] Injection is plain Go (passing interfaces), not a reflective container.
