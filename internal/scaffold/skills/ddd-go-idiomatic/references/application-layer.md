# Application Layer — Use Cases, Ports and Files

## Contents

1. Business vs application, and the import purity test
2. One use case = one type + one file (shape, verb forms, forbidden names, port placement)
3. The repository interface is consumer-defined; infrastructure translates driver errors
4. DTOs without ceremony
5. Domain errors → HTTP status at the edge; transport layout
6. The full flow and who knows whom

The application layer is the seam between the outside world (HTTP, queues, cron) and the
pure domain. It holds no business rules; it **orchestrates** them. Use cases live here, with
the ports they need and the translation between external formats and domain types.

A use case is a concrete type in its own file; a DTO is a struct; an interface is declared
where it is consumed. No `mapper/` layer, no interface per struct, no
`AbstractServiceFactory`. If a "canonical" DDD pattern pushes you towards Go a reviewer
would reject, the pattern was translated badly and the Go idiom wins.

Examples use the `identity` context: domain and use cases share the package; infrastructure
(`pgstore`, `memstore`) and transport (`identityhttp`) are subpackages. The complete,
compiled version is [worked-example.md](worked-example.md).

## 1. Business vs application

**Business** logic (entities + value objects) is pure: it validates invariants, computes,
decides. It knows nothing about SQL or HTTP. **Application** logic orchestrates: it takes
external input, builds domain types, calls ports, coordinates one aggregate and returns
output.

| Role | Where | Responsibility | Must not import |
|---|---|---|---|
| Domain | `identity/user.go`, `email.go`... | invariants, computation | `database/sql`, `net/http`, drivers |
| Use case | `identity/register_user.go`... | orchestrate domain + ports | `net/http`, concrete drivers |
| Infra / transport | `identity/pgstore`, `identity/identityhttp` | implement ports, translate | — |

Domain and use cases share a package: the split is **by file**, not by `domain/` + `app/`
folders (see [bounded-contexts.md](bounded-contexts.md)).

### The import purity test is a real architecture check

The context package must not import `database/sql`, `net/http` or infrastructure libraries.
It is an executable check you can put in CI:

```bash
go list -deps ./internal/identity | grep -E '^(database/sql|net/http)$|jackc|redis' \
  && { echo "infrastructure leaked into the domain"; exit 1; } || echo "domain is pure"
```

`go list -deps` expands the transitive closure, so it also catches indirect leaks (a VO that
imports a helper that imports `pgx`). List the context package itself, not
`./internal/identity/...`: the infrastructure subpackages are supposed to import drivers.

## 2. One use case = one type + one file

A use case is **a concrete type named after the action**, in a file with the same name in
snake_case. It is not one more method on a `Service` that groups the whole context, nor a
`UseCase → AbstractUseCase → Command` tree.

Why: a file name should say what the file does. `service.go` says nothing, and a `Service`
with `LogIn()` and `SignUp()` mixes two intents, two dependency sets and two reasons to change
in one place. With one type per use case:

- The package listing is the index of what the context can do; `ls internal/identity` answers "which use cases exist?".
- Each use case declares **only** the dependencies it uses, so its tests build only those.
- Adding a use case adds a file; it never edits someone else's.

### Canonical shape

| Piece | Shape |
|---|---|
| File | `<action>.go` in snake_case: `register_user.go` |
| Type | `type RegisterUser struct{ ...dependencies }` |
| Constructor | `func NewRegisterUser(deps...) *RegisterUser` |
| Method | exactly one exported: `Execute(ctx context.Context, in RegisterUserInput) (RegisterUserOutput, error)` |
| DTOs | `RegisterUserInput` / `RegisterUserOutput`, in the same file; input is a struct even for a single ID |
| Narrow ports | declared in the same file when only this use case uses them (`IDGenerator`) |

Verb forms follow the action as people say it: `sign_up.go` → `SignUp`, `log_in.go` →
`LogIn`, `change_email.go` → `ChangeEmail`, `search_users.go` → `SearchUsers`. One file never
holds two use cases, however short.

```go
// RegisterUser signs up a user whose email nobody else owns.
type RegisterUser struct {
	users UserRepository
	ids   IDGenerator
	now   func() time.Time
}

// NewRegisterUser takes the clock as a dependency so tests can freeze time; nil means time.Now.
func NewRegisterUser(users UserRepository, ids IDGenerator, now func() time.Time) *RegisterUser

// Execute fails with ErrInvalidEmail, ErrInvalidName or ErrEmailTaken when the input is rejected.
func (uc *RegisterUser) Execute(ctx context.Context, in RegisterUserInput) (RegisterUserOutput, error)
```

`Execute` is the **integration** column: parse VOs (`NewEmail`, `NewName`) → ask the port
(`ExistsByEmail`) → build the entity (`NewUser(id, email, name, uc.now())`) → `Save` → map to
`RegisterUserOutput`. The full file is in [worked-example.md](worked-example.md) §4. The use
case **knows** the repository; the domain does not. Invariants are built by the domain; the
use case invokes them and decides the flow.

### Forbidden names inside a context

| Forbidden | Why | Instead |
|---|---|---|
| `service.go`, `services.go` + `type Service` | a drawer of use cases | `register_user.go` + `type RegisterUser` |
| `usecase.go`, `usecases.go`, `use_cases.go` + `type UseCase` | names the layer, not the action | one file per use case |
| `application.go`, `app.go` | names the layer | the action's name |
| `manager.go` + `type Manager` | "manages" is not an intent | the action's name |
| `helpers.go`, `utils.go`, `common.go` | a drawer without a concept | the concept it belongs to |
| `handler.go`/`handlers.go` with several endpoints | mixes use cases in the transport | one file per endpoint + `routes.go` |

Allowed because they name something concrete: concepts (`user.go`, `email.go`), `errors.go`
(the context's sentinels), shared ports (`user_repository.go`), `routes.go`. Outside the
contexts, a composition root such as `internal/handlers/handlers.go` or `cmd/<app>/main.go` is
fine: its single intent is wiring.

What this shape is **not**: ceremonial CQRS. No `RegisterUserCommand` + `RegisterUserHandler`
+ bus + generic `UseCase[I, O]`. A use case is a concrete struct with one method; wiring builds
it and the transport calls it directly.

## 3. The repository interface is consumer-defined

**The interface is defined by whoever uses it (the use cases), not by whoever implements it
(infrastructure).** That is Go's "accept interfaces, return structs" and Clean
Architecture's dependency inversion at once. Where to declare it depends on how many use
cases consume it:

- **One** → in that use case's file, with exactly the methods it needs (`IDGenerator` in `register_user.go`).
- **Several** → in its own file named after the port: `user_repository.go` → `type UserRepository interface`.

```go
// UserRepository is shared by RegisterUser and ChangeEmail; Save reports ErrEmailTaken like a unique index.
type UserRepository interface {
	FindByID(ctx context.Context, id UserID) (*User, error)
	ExistsByEmail(ctx context.Context, email Email) (bool, error)
	Save(ctx context.Context, u *User) error
}
```

Keep it **narrow**: only the methods its consumers call. A use case that only reads may
declare its own one-method interface in its file even though `UserRepository` exists.

### Infrastructure translates driver errors at the border

The Postgres implementation lives in a subpackage and **never lets a `*pgconn.PgError` or
`pgx.ErrNoRows` escape**: `23505` (unique violation) becomes `identity.ErrEmailTaken`, no rows
becomes `identity.ErrUserNotFound`. Rows are rebuilt through the domain constructors, so a
corrupt row is caught on load.

**internal/identity/pgstore/pgstore.go**

```go
// Package pgstore implements the identity ports on PostgreSQL.
package pgstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"example.com/shop/internal/identity"
)

const uniqueViolation = "23505"

// Store is safe for concurrent use; the pool handles connections.
type Store struct {
	pool *pgxpool.Pool
}

// New wraps an existing pool; main owns its lifecycle.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

var _ identity.UserRepository = (*Store)(nil)

// Save upserts u and maps the unique index on email to identity.ErrEmailTaken.
func (s *Store) Save(ctx context.Context, u *identity.User) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO users (id, email, name, created_at) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (id) DO UPDATE SET email = EXCLUDED.email, name = EXCLUDED.name`,
		u.ID(), u.Email().String(), u.Name().String(), u.CreatedAt())
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return identity.ErrEmailTaken
	}
	if err != nil {
		return fmt.Errorf("pgstore: save user %s: %w", u.ID(), err)
	}
	return nil
}

// FindByID returns identity.ErrUserNotFound when no row matches.
func (s *Store) FindByID(ctx context.Context, id identity.UserID) (*identity.User, error) {
	var rawEmail, rawName string
	var createdAt time.Time
	err := s.pool.QueryRow(ctx, `SELECT email, name, created_at FROM users WHERE id = $1`, id).
		Scan(&rawEmail, &rawName, &createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, identity.ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("pgstore: find user %s: %w", id, err)
	}
	email, err := identity.NewEmail(rawEmail)
	if err != nil {
		return nil, fmt.Errorf("pgstore: corrupt email for user %s: %w", id, err)
	}
	name, err := identity.NewName(rawName)
	if err != nil {
		return nil, fmt.Errorf("pgstore: corrupt name for user %s: %w", id, err)
	}
	return identity.NewUser(id, email, name, createdAt)
}

// ExistsByEmail is a cheap pre-check; Save still enforces uniqueness.
func (s *Store) ExistsByEmail(ctx context.Context, email identity.Email) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE email = $1)`, email.String()).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("pgstore: check email: %w", err)
	}
	return exists, nil
}
```

Returning the raw driver error instead (`return err` from `Exec`) would force every use case
to import `pgconn` to recognise a duplicate: the driver would leak through the port.

`var _ identity.UserRepository = (*Store)(nil)` makes the compiler check the direction:
`pgstore` depends on `identity`, never the reverse. There is no `IUserRepository` duplicated
next to the implementation; that is interface-per-class (see [antipatterns.md](antipatterns.md) §2).

## 4. DTOs without ceremony

The `XInput`/`XOutput` structs **are** the DTOs. They live in the use case's file, not in a
`dto/` or `mapper/` package, and carry the use case's name as a prefix because they share
the package with the others (`RegisterUserInput`, `ChangeEmailInput`). They hold
transport-friendly primitives, so transports never depend on value objects.

Translating `HTTP request → Input` and `Output → HTTP response` is a **thin** function in
the endpoint. No reflection-based auto-mappers (a dependency plus runtime errors the
compiler cannot see), no `mapper/` layer that only copies fields.

```go
// BAD: mapper layer + reflection; a renamed field breaks at runtime.
out := mapper.Map[RegisterUserOutput](entity)

// GOOD: two explicit fields, checked by the compiler.
resp := registerUserResponse{ID: out.ID, Email: out.Email}
```

If the mapping is non-trivial (flattening an aggregate, formatting dates), it is still an
ordinary named function, not a framework. A `mapper/` "in case it grows" is speculative design.

## 5. Domain errors → HTTP status at the edge

Mapping domain errors to HTTP status happens at the **edge** (the transport), with
`errors.Is`/`errors.As`. The domain exposes sentinels; the transport maps them. Anything
unknown becomes 500 **without leaking internal details**.

| Domain error | Status | Meaning |
|---|---|---|
| `ErrInvalidEmail`, `ErrInvalidName` | 400 Bad Request | invalid input |
| `ErrUserNotFound` | 404 Not Found | missing resource |
| `ErrEmailTaken` | 409 Conflict | uniqueness invariant violated |
| anything else | 500 Internal Server Error | generic message; details go to the log |

The transport follows the same naming rule as the context: one file per endpoint named after
the use case it exposes, a `routes.go` that mounts them with `net/http` method patterns
(Go 1.22+), and the error mapping in one place.

```
internal/identity/identityhttp/
├── routes.go          # Mount: mux.Handle("POST /users", ...), mux.Handle("PUT /users/{id}/email", ...)
├── register_user.go   # POST /users
├── change_email.go    # PUT /users/{id}/email
└── responses.go       # writeJSON + writeError
```

The code for each file is in [worked-example.md](worked-example.md) §6. Use `errors.Is` for
sentinels and `errors.As` when you need data from the error (a `*ValidationError` with the
failing field). A `default` branch that echoes the internal error text is an information
leak: return a generic message and log the detail.

## 6. The full flow and who knows whom

```
HTTP request
   │ decode (identityhttp/register_user.go)
   ▼
RegisterUserInput ──► RegisterUser.Execute (identity/register_user.go)
                        1. NewEmail, NewName            ← domain (VOs)
                        2. users.ExistsByEmail(...)     ← UserRepository
                        3. NewUser(id, email, name, now) ← domain (entity)
                        4. users.Save(ctx, u)           ← UserRepository
                        ▼
                     RegisterUserOutput ──► response   (error ──► writeError ──► 4xx/5xx)
```

Dependencies always point inwards:

```
cmd/ ──► identityhttp ──► identity   (defines UserRepository, IDGenerator)
                             ▲
         pgstore ────────────┘       (implements UserRepository)
```

- **Use cases → domain**: same package; they use VOs/entities and return their errors.
- **Infrastructure and transport → context**: `pgstore` implements its ports; `identityhttp` calls its use cases.
- **The context knows nobody outside**: `identity` imports neither `pgstore`, `identityhttp` nor `net/http`. An import cycle is rejected by the compiler, and that is a feature.

Wiring lives in `cmd/` and is the only code that knows every package. It builds each use
case separately:

```go
store := pgstore.New(pool)
identityhttp.Mount(mux, identityhttp.UseCases{
	RegisterUser: identity.NewRegisterUser(store, uuidgen.New(), time.Now),
	ChangeEmail:  identity.NewChangeEmail(store),
})
```

Bounded contexts ([bounded-contexts.md](bounded-contexts.md)) are assembled here too: each
exposes its use cases and `cmd/` wires them without the contexts importing each other.

## Checklist

- [ ] The context package imports no `database/sql`, `net/http` or drivers, checked with `go list -deps` in CI.
- [ ] Each use case is its own type (`RegisterUser`) in a file with its name (`register_user.go`), with `NewRegisterUser` and exactly one `Execute(ctx, RegisterUserInput) (RegisterUserOutput, error)`.
- [ ] Input is always a struct, even for a single ID; DTOs and single-use ports sit in the use case's file.
- [ ] No file holds two use cases; no `service.go`, `usecase.go`, `app.go`, `manager.go`, and no `Service`/`UseCase`/`Manager` types.
- [ ] No Command/Handler/`UseCase[I, O]` hierarchies: one concrete struct, one method.
- [ ] Ports are declared by the consumer: in the use case's file if only it uses them, in `<port>.go` if shared; the implementation lives in an infrastructure subpackage.
- [ ] Infrastructure translates driver errors (`pgconn.PgError`, `pgx.ErrNoRows`) into domain errors and rebuilds entities through constructors.
- [ ] Transport: one file per endpoint + `routes.go` with method patterns; one error → status mapping; unknown → 500 with a generic message.
- [ ] `var _ identity.UserRepository = (*Store)(nil)` checks at compile time that infrastructure satisfies the port.
- [ ] Dependencies form a DAG pointing inwards; wiring lives only in `cmd/`.
