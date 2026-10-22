# Package Oriented Design — Go Project Layout

## Contents

- The core principle
- What a package is (and is not)
- Canonical project structure: `cmd/`, `internal/`, bounded contexts, `platform/`, `app/`
- Package Oriented Design in practice (worked example)
- Anti-patterns
- Avoid nested packages; `internal/` as the only real boundary
- File names
- Dependency direction (DAG)
- Rules summary

Bounded-context internals (one use case per file, the `NewX` + `Execute(ctx, XInput) (XOutput, error)`
shape, ports, value objects) are owned by the **ddd-go-idiomatic** skill. This file covers the
project layout around them.

---

## The Core Principle

> **A package is not a namespace. A package is a unit of design.**

In Java or C#, packages are folders that group related things. In Go, a package defines what it
*is*: its purpose, its data and its behavior belong together.

The question is never *"where should I put this file?"* but *"what single concept does this
package represent?"*

---

## What a Package Is (and Is Not)

A package is:
- a single, self-contained concept with a clear boundary;
- the owner of its own data — it does not share raw types through a common `models` package;
- named after what it represents, not what it does;
- small enough that its API can be understood in one reading.

A package is not a folder to dump related files, a Java-style namespace (`com.company.utils`), or
a collection of helpers for other packages.

### The Naming Test

If you cannot name the package with a single, specific noun, it has no identity yet. A useful check:
describe it in one sentence that starts with its name — "Package user models accounts and their
lifecycle." If the sentence needs "and also", the package is two packages.

| Bad name | Why it fails | What you are really designing |
|---|---|---|
| `util` | A drawer, not a concept | Split by the concept each function serves |
| `helper` | Helps what? | Nothing with an identity |
| `common` | Common to whom? | Shared coupling disguised as reuse |
| `types` | Types of what? | An anemic data layer |
| `models` | ORM thinking | Domain types belong inside their domain package |
| `base` | Base of what? | A missing abstraction |
| `shared` | Forced coupling | Rethink the boundaries |
| `manager` | Manages what? | A god object disguised as a package |

---

## Canonical Project Structure

```
myservice/
├── cmd/
│   └── myservice/
│       └── main.go            ← thin: load config, call app.Run, exit
├── internal/
│   ├── user/                  ← bounded context: domain + use cases, no infra imports
│   │   ├── user.go            ← the User entity and its errors
│   │   ├── get_user.go        ← use case GetUser + its DTOs and port
│   │   ├── register_user.go   ← use case RegisterUser
│   │   ├── user_repository.go ← port shared by several use cases
│   │   ├── pgstore/           ← infra: implements the ports with pgx
│   │   │   └── pgstore.go
│   │   └── userhttp/          ← transport: one file per endpoint + routes.go
│   │       ├── get_user.go
│   │       ├── register_user.go
│   │       └── routes.go
│   ├── platform/              ← cross-cutting infrastructure (not "util")
│   │   ├── postgres/          ← pool setup
│   │   └── config/            ← config loading and validation
│   └── app/                   ← composition root: wires the graph
│       └── run.go
├── go.mod
└── go.sum
```

### `cmd/` — Entry Points Only

Each subdirectory of `cmd/` is a `main` package whose job is to parse flags/environment, build the
dependency graph (or call something that does), run, and turn the result into an exit code. When
`main.go` grows past roughly 50 lines, business logic has usually leaked into it.

**cmd/myservice/main.go**
```go
package main

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, cfg); err != nil {
		log.Printf("run: %v", err)
		stop()
		os.Exit(1)
	}
}
```

`log.Fatal` calls `os.Exit`, which skips deferred calls; that is fine before anything needs
cleanup, but after `defer stop()` the example releases resources explicitly before exiting.

### `internal/` — The Compiler-Enforced Boundary

`internal/` is enforced by the Go toolchain: code under `a/b/internal/` can only be imported by
code rooted at `a/b/`. Use it for all business logic that should not be a public API, for domain
packages, and for infrastructure wrappers. Create a public package (with or without `pkg/`) only
when another module genuinely needs to import it. When in doubt, `internal/` first.

---

## Package Oriented Design in Practice

### The bounded context owns its world

The `user` package owns the `User` type — not `models.User`, not `types.User`. The type, its use
cases and the ports they need live together; persistence and transport live in subpackages that
depend on `user`, never the other way round. The domain package imports no `database/sql`, `pgx`,
`net/http` or `encoding/json`.

**internal/user/user.go**
```go
// Package user models accounts and the use cases that operate on them.
package user

// ID identifies a User.
type ID int64

// ErrNotFound is returned when no user matches the requested ID.
var ErrNotFound = errors.New("user: not found")

// User is an account; build it with NewUser so the email is always validated.
type User struct {
	id        ID
	email     string
	createdAt time.Time
}

// NewUser returns an error when email has no "@".
func NewUser(id ID, email string, now time.Time) (*User, error) {
	if !strings.Contains(email, "@") {
		return nil, fmt.Errorf("user: invalid email %q", email)
	}
	return &User{id: id, email: email, createdAt: now}, nil
}

// ID returns the user's identifier.
func (u *User) ID() ID { return u.id }

// Email returns the validated email address.
func (u *User) Email() string { return u.email }
```

**internal/user/get_user.go**
```go
package user

type GetUserInput struct{ ID ID }

type GetUserOutput struct {
	ID    ID
	Email string
}

type userFinder interface {
	FindByID(ctx context.Context, id ID) (*User, error)
}

// GetUser loads one user by ID.
type GetUser struct{ users userFinder }

// NewGetUser wires GetUser to any store that can find users by ID.
func NewGetUser(users userFinder) *GetUser { return &GetUser{users: users} }

// Execute returns ErrNotFound when the user does not exist.
func (uc *GetUser) Execute(ctx context.Context, in GetUserInput) (GetUserOutput, error) {
	u, err := uc.users.FindByID(ctx, in.ID)
	if err != nil {
		return GetUserOutput{}, fmt.Errorf("get user %d: %w", in.ID, err)
	}
	return GetUserOutput{ID: u.ID(), Email: u.Email()}, nil
}
```

The port is declared where it is consumed and holds only the method this use case needs. A port
shared by several use cases gets its own file (`user_repository.go` → `UserRepository`), still in
the consumer package and still small.

**internal/user/pgstore/pgstore.go**
```go
// Package pgstore persists users in PostgreSQL.
package pgstore

// Store implements the user ports with a pgx pool.
type Store struct{ pool *pgxpool.Pool }

// New returns a Store backed by pool; the caller owns the pool's lifecycle.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// FindByID translates pgx.ErrNoRows into user.ErrNotFound.
func (s *Store) FindByID(ctx context.Context, id user.ID) (*user.User, error) {
	var (
		email     string
		createdAt time.Time
	)
	err := s.pool.QueryRow(ctx,
		`SELECT email, created_at FROM users WHERE id = $1`, id,
	).Scan(&email, &createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, user.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("pgstore: find user %d: %w", id, err)
	}
	return user.NewUser(id, email, createdAt)
}
```

Rebuilding the entity through its constructor means a corrupt row is detected on load instead of
leaking an invalid `User` into the domain. Driver errors never cross the boundary: callers see
`user.ErrNotFound`, not `pgx.ErrNoRows` or `*pgconn.PgError`.

**internal/user/userhttp/get_user.go**
```go
package userhttp

func getUser(uc *user.GetUser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		out, err := uc.Execute(r.Context(), user.GetUserInput{ID: user.ID(id)})
		if errors.Is(err, user.ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}
}
```

**internal/user/userhttp/routes.go**
```go
// Package userhttp exposes the user use cases over HTTP.
package userhttp

// Deps lists the use cases the routes need.
type Deps struct {
	GetUser *user.GetUser
}

// Routes registers the user endpoints on mux.
func Routes(mux *http.ServeMux, d Deps) {
	mux.Handle("GET /users/{id}", getUser(d.GetUser))
}
```

`ServeMux` patterns with a method and wildcards (`"GET /users/{id}"`, read with `r.PathValue`)
exist since Go 1.22; a third-party router is no longer needed for this.

### Platform packages — not "util"

Infrastructure that several contexts need is a specific named concept, not `util/`:

```
internal/platform/
├── postgres/    ← opens a tuned pool and pings it
├── config/      ← loads and validates Config
└── ...          ← logger, httpserver, telemetry: one concept each
```

**internal/platform/postgres/postgres.go**
```go
// Package postgres opens PostgreSQL connection pools.
package postgres

// Open returns a tuned pool that already answered a ping.
func Open(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse config: %w", err)
	}
	cfg.MaxConns = 25
	cfg.MinConns = 5
	cfg.MaxConnLifetime = 5 * time.Minute
	cfg.MaxConnIdleTime = time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: open: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}
	return pool, nil
}
```

### `app/` — the composition root

`app.Run` is the one place that knows every package at once. Its job is to wire the graph and run
it; it holds no business rules.

**internal/app/run.go**
```go
// Package app wires the service's dependency graph.
package app

// Run serves HTTP until ctx is canceled, then shuts down gracefully.
func Run(ctx context.Context, cfg config.Config) error {
	pool, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("app: %w", err)
	}
	defer pool.Close()

	users := pgstore.New(pool)

	mux := http.NewServeMux()
	userhttp.Routes(mux, userhttp.Deps{GetUser: user.NewGetUser(users)})

	srv := &http.Server{Addr: cfg.Addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()

	select {
	case err := <-errc:
		return fmt.Errorf("app: serve: %w", err)
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
```

---

## Anti-Patterns

### The god package

```
internal/service/          ← BAD: one package that knows everything
├── user_service.go
├── auth_service.go
├── payment_service.go
└── email_service.go
```

### The anemic type layer

```
internal/models/models.go             ← BAD: type User struct{...}; type Order struct{...}
internal/repositories/user_repo.go    ← func GetUser(id int64) (*models.User, error)
internal/services/user_service.go     ← func CreateUser(u *models.User) error
```

The `User` type is owned by nobody, and three packages must coordinate to do anything with a user.

### The circular import smell

If packages A and B need to import each other, one of three things is true:
1. they are one concept and should be merged;
2. a third package C should be extracted that both depend on;
3. an interface (defined by the consumer) should replace the direct dependency.

---

## Avoid Nested Packages

Go has no package hierarchy. Nesting `internal/user/profile` inside `internal/user` creates no
parent–child relationship, grants no access to the parent's unexported identifiers and
communicates no encapsulation: they are two independent packages that happen to sit in nested
folders. Visibility is per package, never per folder.

Nesting without a reason costs you:
- noisier import paths with no design gain;
- the false belief that the subpackage sees the parent's unexported names (it does not);
- deep trees like `internal/domain/user/service/impl/...`, which are Java/C# layering transplanted.

Default to a flat layout: one level of domain packages under `internal/`. Nest only when the
subpackage is a distinct, subordinate concept. The infra and transport adapters of a context
(`user/pgstore`, `user/userhttp`) are the standard case: they depend on `user`, and `user` never
imports them.

```
internal/                  ← BAD: technical layers disguised as packages
└── user/
    ├── service/           ← "service" is not a concept
    ├── repository/        ← a technical layer, not a domain
    └── model/             ← the User type separated from its behavior

internal/                  ← GOOD: each folder is a named concept
├── user/
│   ├── user.go
│   ├── get_user.go
│   └── pgstore/           ← adapter that depends on user
└── billing/
    ├── billing.go
    └── pgstore/
```

### `internal/` is the only real boundary

The toolchain enforces encapsulation between folders only through `internal/`. Nesting `internal/`
at different levels gives you fine-grained boundaries when you actually need them:

```
myservice/
└── internal/              ← private to myservice
    └── billing/
        └── internal/      ← private to billing: user and auth cannot import it
            └── ledger/
```

Use this to hide a subdomain's details from the other subdomains, not to simulate layers.

---

## File Names

The package names the concept; the file names what it contains. File names are `snake_case`
(`get_user.go`, `routes.go`) and no file is a grab-bag: no `utils.go`, `helpers.go`, `common.go`
or `misc.go` — put each function in the file of the concept it serves. Inside a bounded context,
one use case per file named after the action; the full rule set lives in the
**ddd-go-idiomatic** skill.

---

## Dependency Direction (DAG)

Package dependencies form a directed acyclic graph that points inward: composition root → adapters
→ context. The context (`user`) never imports infrastructure; infrastructure implements the ports
the context declares.

```
cmd/myservice ──▶ internal/app ──▶ internal/user/userhttp ──▶ internal/user
                        │                                        ▲
                        ├──▶ internal/user/pgstore ──────────────┘  (implements user's ports)
                        └──▶ internal/platform/postgres
```

- Arrows never point back: `pgstore` depends on `user` to implement its ports; `user` never
  imports `pgstore`.
- The compiler rejects import cycles. Treat that as a feature: it forces you to fix the design
  (see "The circular import smell").
- `go list -deps` and `go mod graph` show the graph; a linter such as `depguard` can forbid
  `pgx`/`net/http` imports in domain packages.

---

## Rules Summary

1. Name packages after what they represent, not what they do.
2. Each package owns its types — no shared `models/` or `types/`.
3. Define interfaces where they are consumed, small, not next to the implementation.
4. `internal/` first; expose a public API only when another module needs it.
5. `cmd/` is wiring only; business rules live in packages.
6. Platform packages are named concepts — `postgres`, `config`, never `util`.
7. Infra and transport live in subpackages of the context (`pgstore`, `userhttp`); the domain
   package imports no driver, HTTP or JSON packages.
8. Flat by default: nesting creates no hierarchy or encapsulation; only `internal/` is a boundary.
9. Dependencies form a DAG pointing inward.
10. Files are named by what they contain; bounded-context file rules live in ddd-go-idiomatic.
