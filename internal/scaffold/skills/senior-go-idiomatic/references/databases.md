# Databases & Persistence — Go Idiomatic Reference

## Contents

- database/sql: the connection pool and context-aware queries
- pgx: the native PostgreSQL driver (typed scans, COPY)
- sqlc: SQL first, generated Go
- Transactions (canonical pattern, `pgx.BeginTxFunc`, retry on serialization failures)
- Ports and the store adapter (where interfaces and implementations live)
- Migrations with golang-migrate and `go:embed`
- Checklist

---

## database/sql — The Connection Pool

`database/sql` is not a driver; it is an abstraction over drivers, and the connection pool lives
here. Under load, the pool is usually the first bottleneck of a Go service.

```go
db, err := sql.Open("pgx", dsn) // driver registered by github.com/jackc/pgx/v5/stdlib
if err != nil {
	return fmt.Errorf("open db: %w", err)
}
// sql.Open only validates arguments; Ping proves a connection works.
if err := db.PingContext(ctx); err != nil {
	return fmt.Errorf("ping db: %w", err)
}

db.SetMaxOpenConns(25)
db.SetMaxIdleConns(5)
db.SetConnMaxLifetime(5 * time.Minute)
db.SetConnMaxIdleTime(time.Minute)
```

The defaults (unlimited open connections, 2 idle, no lifetime) suit development, not production:
- `MaxOpenConns` too high exhausts the server (`FATAL: too many connections`) once you run several
  replicas; size it against the database's limit divided by instances.
- `MaxIdleConns` too low means reconnecting constantly under load.
- No `ConnMaxLifetime` keeps connections that a failover or load balancer silently broke.

### Context-aware queries

```go
// The query is canceled when ctx is canceled or its deadline passes.
rows, err := db.QueryContext(ctx, "SELECT id, email FROM users WHERE active = $1", true)
if err != nil {
	return fmt.Errorf("query users: %w", err)
}
defer rows.Close() // returns the connection to the pool even if you stop early

for rows.Next() {
	var u userRow
	if err := rows.Scan(&u.ID, &u.Email); err != nil {
		return fmt.Errorf("scan user: %w", err)
	}
	// use u
}
// rows.Next returns false on error too; only rows.Err tells the two apart.
if err := rows.Err(); err != nil {
	return fmt.Errorf("iterate users: %w", err)
}
```

Use the context-aware variants (`QueryContext`, `ExecContext`, `BeginTx`) everywhere: the
context-less ones cannot be canceled, so a slow query outlives the request that started it.

---

## pgx — The Native PostgreSQL Driver

`github.com/jackc/pgx/v5` works in two modes:

- **through `database/sql`** (`pgx/v5/stdlib`): standard interface, fewer features;
- **native `pgxpool`**: its own pool, the COPY protocol, native PostgreSQL types. Prefer it when
  the service only talks to PostgreSQL.

Pool setup (`pgxpool.ParseConfig`, `MaxConns`, `MaxConnLifetime`, `Ping`) is shown in
`project-layout.md` under `internal/platform/postgres`. Use `cfg.AfterConnect` to register
custom types on each new connection.

### Typed scans

```go
rows, err := pool.Query(ctx, "SELECT id, email, created_at FROM users")
if err != nil {
	return nil, fmt.Errorf("query users: %w", err)
}
// CollectRows closes rows and checks rows.Err for you.
users, err := pgx.CollectRows(rows, pgx.RowToStructByName[userRow])
if err != nil {
	return nil, fmt.Errorf("collect users: %w", err)
}
```

`RowToStructByName` matches columns to field names (or `db:"..."` tags), case-insensitively.

### Bulk insert with COPY

```go
// For thousands of rows COPY is typically far faster than batched INSERTs.
n, err := pool.CopyFrom(ctx,
	pgx.Identifier{"users"},
	[]string{"email", "created_at"},
	pgx.CopyFromSlice(len(rows), func(i int) ([]any, error) {
		return []any{rows[i].Email, rows[i].CreatedAt}, nil
	}),
)
if err != nil {
	return fmt.Errorf("copy users: %w", err)
}
```

---

## sqlc — SQL First, Generated Go

`sqlc` reads your schema and queries and generates type-safe Go. You write real SQL and get
functions with the right types — no ORM, no runtime reflection, and a query that does not match
the schema fails at generation time instead of in production.

```
schema.sql  ──┐
queries.sql ──┼─→ go tool sqlc generate ──→ db.go, models.go, querier.go, queries.sql.go
sqlc.yaml   ──┘                              (generated: do not edit)
```

### Pinning the tool

Since Go 1.24, `go.mod` can record tools with the `tool` directive, which replaces the old
`tools.go` blank-import trick:

```bash
go get -tool github.com/sqlc-dev/sqlc/cmd/sqlc
go tool sqlc generate
```

The tool's dependencies join your module graph (they show up in `go.sum` and `go mod graph`), so
for heavy tools some teams prefer a pinned release binary in CI instead.

### sqlc.yaml

```yaml
version: "2"
sql:
  - engine: "postgresql"
    queries: "queries.sql"
    schema: "schema.sql"
    gen:
      go:
        package: "db"
        out: "internal/user/pgstore/db"
        sql_package: "pgx/v5"   # generate against pgx, not database/sql
        emit_interface: true    # also generate the Querier interface
```

`sql_package: "pgx/v5"` is what makes the generated `New` accept a `*pgxpool.Pool`, `pgx.Tx` or
`*pgx.Conn`, and what makes "no rows" surface as `pgx.ErrNoRows`. Without it, sqlc targets
`database/sql` and you would compare against `sql.ErrNoRows`. Every generated method already takes
a `context.Context` first.

### queries.sql → generated Go

```sql
-- name: GetUser :one
SELECT id, email, created_at FROM users WHERE id = $1;

-- name: CreateUser :one
INSERT INTO users (email, created_at) VALUES ($1, now())
RETURNING id, email, created_at;

-- name: ListActiveUsers :many
SELECT id, email, created_at FROM users WHERE active ORDER BY created_at DESC;
```

The generated package exposes, roughly:

```go
type Querier interface {
	GetUser(ctx context.Context, id int64) (User, error)
	CreateUser(ctx context.Context, email string) (User, error)
	ListActiveUsers(ctx context.Context) ([]User, error)
}

func New(db DBTX) *Queries                    // DBTX: anything with Exec, Query and QueryRow
func (q *Queries) WithTx(tx pgx.Tx) *Queries
```

The generated `User` is a row struct, not your domain entity; the store adapter maps one to the
other (see below). Keep generated code inside the adapter package (`pgstore/db`) so nothing else
depends on it.

---

## Transactions

### The canonical pattern

```go
func (s *Store) TransferFunds(ctx context.Context, from, to, amount int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	// After a successful Commit, Rollback is a no-op that returns pgx.ErrTxClosed.
	defer tx.Rollback(ctx)

	q := s.queries.WithTx(tx)
	if err := q.DebitAccount(ctx, db.DebitAccountParams{ID: from, Amount: amount}); err != nil {
		return fmt.Errorf("debit %d: %w", from, err)
	}
	if err := q.CreditAccount(ctx, db.CreditAccountParams{ID: to, Amount: amount}); err != nil {
		return fmt.Errorf("credit %d: %w", to, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
```

Deferring `Rollback` right after `Begin` guarantees that every early return releases the
transaction and its connection. `pgx.BeginFunc` and `pgx.BeginTxFunc` package the same pattern:
they commit when the function returns nil and roll back otherwise.

### Retry on serialization failures

With `REPEATABLE READ` or `SERIALIZABLE`, PostgreSQL aborts conflicting transactions with SQLSTATE
`40001` and expects the client to retry the whole transaction. The failure can surface on any
statement or on `COMMIT`, so retry the unit, not a single call.

```go
const serializationFailure = "40001"

func withSerializableRetry(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	const maxAttempts = 3
	var err error
	for attempt := range maxAttempts {
		err = pgx.BeginTxFunc(ctx, pool, pgx.TxOptions{IsoLevel: pgx.Serializable}, fn)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != serializationFailure {
			return err // success, or an error a retry will not fix
		}
		backoff := time.Duration(attempt+1) * 50 * time.Millisecond
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return fmt.Errorf("serializable tx: gave up after %d attempts: %w", maxAttempts, err)
}
```

`fn` must be safe to run more than once: keep side effects outside the database (emails, HTTP
calls) out of it.

---

## Ports and the Store Adapter

The interface lives in the package that uses it (the bounded context), small and shaped by its
callers; the implementation lives in an infrastructure subpackage of the context. The domain
package never imports `pgx`, `database/sql` or the generated `db` package. Use-case shape and port
placement are owned by the **ddd-go-idiomatic** skill; the layout is in `project-layout.md`.

**internal/user/user_repository.go**
```go
package user

// UserRepository is the persistence port shared by several use cases.
type UserRepository interface {
	FindByID(ctx context.Context, id ID) (*User, error)
	ExistsByEmail(ctx context.Context, email string) (bool, error)
	Save(ctx context.Context, u *User) error
}
```

A use case that only reads declares its own one-method port in its file instead of taking the
whole `UserRepository`. Resist a five-method `Store` with `Update`, `Delete` and `ListActive` "for
completeness": every method you add is one more thing every fake must implement.

**internal/user/pgstore/pgstore.go**
```go
// Package pgstore implements the user ports on PostgreSQL via sqlc-generated queries.
package pgstore

var _ user.UserRepository = (*Store)(nil)

// Store adapts the generated queries to the user ports.
type Store struct{ q *db.Queries }

// New returns a Store that runs its queries on pool.
func New(pool *pgxpool.Pool) *Store { return &Store{q: db.New(pool)} }

// FindByID translates pgx.ErrNoRows into user.ErrNotFound.
func (s *Store) FindByID(ctx context.Context, id user.ID) (*user.User, error) {
	row, err := s.q.GetUser(ctx, int64(id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, user.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("pgstore: get user %d: %w", id, err)
	}
	return user.NewUser(user.ID(row.ID), row.Email, row.CreatedAt)
}
```

The `var _ user.UserRepository = (*Store)(nil)` line earns its place here: `pgstore` never
mentions the port otherwise, so a signature drift would only show up in the composition root.
`ExistsByEmail` and `Save` follow the same shape: call the generated query, translate driver errors
(`pgx.ErrNoRows`, unique violation `23505`) into domain errors, wrap everything else.

In tests, a hand-written fake satisfies the port; no mocking framework needed:

**internal/user/get_user_test.go**
```go
package user_test

type fakeUsers struct{ byID map[user.ID]*user.User }

func (f fakeUsers) FindByID(_ context.Context, id user.ID) (*user.User, error) {
	u, ok := f.byID[id]
	if !ok {
		return nil, user.ErrNotFound
	}
	return u, nil
}

func TestGetUserNotFound(t *testing.T) {
	uc := user.NewGetUser(fakeUsers{})
	_, err := uc.Execute(t.Context(), user.GetUserInput{ID: 7})
	if !errors.Is(err, user.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
```

The store itself is tested against a real PostgreSQL (see `testing-advanced.md`,
testcontainers), because the SQL is the part most likely to be wrong.

---

## Migrations — golang-migrate

```bash
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
migrate create -ext sql -dir migrations -seq create_users_table
# creates 000001_create_users_table.up.sql and 000001_create_users_table.down.sql
```

```sql
-- 000001_create_users_table.up.sql
CREATE TABLE users (
    id         BIGSERIAL PRIMARY KEY,
    email      TEXT NOT NULL UNIQUE,
    active     BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 000001_create_users_table.down.sql
DROP TABLE IF EXISTS users;
```

### Embedding migrations in the binary

```go
import (
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres" // registers postgres://
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func RunMigrations(dsn string) error {
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("migrations source: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, dsn)
	if err != nil {
		return fmt.Errorf("migrate init: %w", err)
	}
	defer m.Close()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}
```

The `//go:embed` directive must live in a package whose directory contains `migrations/`;
embedding means the binary always ships the migrations it was built with.

---

## Checklist

- [ ] Pool sized against the database's connection limit; lifetime and idle time set
- [ ] Every query uses a context-aware call (`QueryContext`, `pool.Query(ctx, ...)`)
- [ ] `defer rows.Close()` right after a successful query and `rows.Err()` checked after the loop
      (or `pgx.CollectRows`, which does both)
- [ ] `defer tx.Rollback(ctx)` right after `Begin`, or `pgx.BeginFunc` / `BeginTxFunc`
- [ ] Serialization failures (`40001`) retried as a whole transaction, with backoff and ctx
- [ ] sqlc configured with `sql_package: "pgx/v5"` when the driver is pgx; the tool pinned in go.mod
- [ ] Driver errors translated to domain errors at the adapter (`ErrNotFound`), never leaked
- [ ] Ports small and defined in the consuming context; the store lives in `<ctx>/pgstore`
- [ ] Migrations embedded with `go:embed` and run at startup or in a dedicated job
