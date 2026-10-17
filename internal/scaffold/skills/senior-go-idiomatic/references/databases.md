# Databases & Persistence — Go Idiomatic Reference

## database/sql — Internals y Connection Pool

`database/sql` no es un driver — es una capa de abstracción sobre drivers. El pool de conexiones vive aquí, no en el driver. Entender el pool es entender el primer cuello de botella de cualquier servicio Go bajo carga.

### Configuración Obligatoria del Pool

```go
db, err := sql.Open("pgx", dsn)
if err != nil {
    return fmt.Errorf("open db: %w", err)
}

// Sin esto, db.Open() no verifica que la conexión funcione
if err := db.PingContext(ctx); err != nil {
    return fmt.Errorf("ping db: %w", err)
}

// NUNCA dejar estos en sus defaults — los defaults están pensados para desarrollo
db.SetMaxOpenConns(25)              // máximo de conexiones simultáneas al DB
db.SetMaxIdleConns(5)               // conexiones en el pool esperando ser reutilizadas
db.SetConnMaxLifetime(5 * time.Minute) // recicla conexiones para evitar TCP stale
db.SetConnMaxIdleTime(1 * time.Minute) // cierra conexiones idle demasiado tiempo
```

**Por qué importa:**
- `MaxOpenConns` demasiado alto → agota conexiones en Postgres, genera `FATAL: too many connections`
- `MaxIdleConns` demasiado bajo → overhead de reconexión constante bajo carga
- Sin `ConnMaxLifetime` → conexiones TCP silenciosamente rotas después de un failover de DB

### QueryContext vs Query — Siempre Context

```go
// BAD: no respeta cancelación ni timeouts
rows, err := db.Query("SELECT id, email FROM users WHERE active = $1", true)

// GOOD: se cancela si el contexto expira o se cancela
rows, err := db.QueryContext(ctx, "SELECT id, email FROM users WHERE active = $1", true)
if err != nil {
    return fmt.Errorf("query users: %w", err)
}
defer rows.Close() // siempre — aunque leas todas las filas

for rows.Next() {
    var u User
    if err := rows.Scan(&u.ID, &u.Email); err != nil {
        return fmt.Errorf("scan user: %w", err)
    }
    // procesar u
}
// Verificar errores de iteración — rows.Next() los absorbe silenciosamente
if err := rows.Err(); err != nil {
    return fmt.Errorf("rows iteration: %w", err)
}
```

---

## pgx — Driver de Producción

`pgx` es el driver PostgreSQL de producción. Tiene dos modos:

- **`pgx` con `database/sql`**: compatible con la interfaz estándar, menos features
- **`pgxpool` nativo**: el modo recomendado — pool propio, Copy protocol, tipos nativos de Postgres

```go
import "github.com/jackc/pgx/v5/pgxpool"

func Open(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
    cfg, err := pgxpool.ParseConfig(dsn)
    if err != nil {
        return nil, fmt.Errorf("parse config: %w", err)
    }

    // Configuración equivalente al pool de database/sql
    cfg.MaxConns = 25
    cfg.MinConns = 5
    cfg.MaxConnLifetime = 5 * time.Minute
    cfg.MaxConnIdleTime = 1 * time.Minute

    // Hook para configurar cada conexión al crearla
    cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
        // registrar tipos custom de Postgres, por ejemplo
        return nil
    }

    pool, err := pgxpool.NewWithConfig(ctx, cfg)
    if err != nil {
        return nil, fmt.Errorf("create pool: %w", err)
    }
    if err := pool.Ping(ctx); err != nil {
        return nil, fmt.Errorf("ping: %w", err)
    }
    return pool, nil
}
```

### Scan Tipado con pgx

```go
// pgx devuelve tipos nativos de Postgres — no necesitas conversiones manuales
rows, err := pool.Query(ctx, "SELECT id, email, created_at FROM users")
if err != nil {
    return nil, fmt.Errorf("query: %w", err)
}
defer rows.Close()

users, err := pgx.CollectRows(rows, pgx.RowToStructByName[User])
if err != nil {
    return nil, fmt.Errorf("collect: %w", err)
}
```

### Bulk Insert con Copy Protocol

```go
// Para insertar miles de filas: Copy es 10-100x más rápido que INSERT batch
_, err := pool.CopyFrom(
    ctx,
    pgx.Identifier{"users"},
    []string{"email", "created_at"},
    pgx.CopyFromSlice(len(users), func(i int) ([]any, error) {
        return []any{users[i].Email, users[i].CreatedAt}, nil
    }),
)
if err != nil {
    return fmt.Errorf("copy from: %w", err)
}
```

---

## sqlc — SQL Primero, Código Generado

`sqlc` lee tus archivos SQL y genera código Go type-safe. Escribes SQL real, obtienes funciones Go con tipos correctos — sin ORM, sin reflection en runtime.

### Flujo de trabajo

```
schema.sql  ──┐
queries.sql ──┤─→ sqlc generate ──→ db.go (generado, no editar)
sqlc.yaml   ──┘                     models.go (generado, no editar)
                                    querier.go (interface, no editar)
```

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
        out: "internal/db"
        emit_interface: true          # genera la interface Querier
        emit_context: true            # todas las funciones reciben context.Context
        emit_prepared_queries: false
```

### queries.sql → Go generado

```sql
-- name: GetUser :one
SELECT id, email, created_at FROM users WHERE id = $1;

-- name: CreateUser :one
INSERT INTO users (email, created_at)
VALUES ($1, now())
RETURNING *;

-- name: ListActiveUsers :many
SELECT id, email, created_at FROM users WHERE active = true ORDER BY created_at DESC;
```

Genera automáticamente:

```go
// interface Querier — úsala en producción y en mocks de tests
type Querier interface {
    GetUser(ctx context.Context, id int64) (User, error)
    CreateUser(ctx context.Context, email string) (User, error)
    ListActiveUsers(ctx context.Context) ([]User, error)
}

// Queries implementa Querier
type Queries struct{ db DBTX }
```

### Uso en Producción

```go
// Queries acepta *sql.DB, *sql.Tx, *pgxpool.Pool — cualquier cosa que implemente DBTX
queries := db.New(pool)

user, err := queries.GetUser(ctx, userID)
if err != nil {
    if errors.Is(err, pgx.ErrNoRows) {
        return nil, ErrNotFound
    }
    return nil, fmt.Errorf("get user: %w", err)
}
```

---

## Transacciones Idiomáticas

### El Patrón Canónico

```go
func (s *Store) TransferFunds(ctx context.Context, fromID, toID int64, amount int64) error {
    tx, err := s.db.Begin(ctx)
    if err != nil {
        return fmt.Errorf("begin tx: %w", err)
    }
    // defer Rollback SIEMPRE — es no-op si Commit ya fue llamado
    defer tx.Rollback(ctx)

    q := s.queries.WithTx(tx)

    if err := q.DebitAccount(ctx, db.DebitAccountParams{ID: fromID, Amount: amount}); err != nil {
        return fmt.Errorf("debit: %w", err)
    }
    if err := q.CreditAccount(ctx, db.CreditAccountParams{ID: toID, Amount: amount}); err != nil {
        return fmt.Errorf("credit: %w", err)
    }

    if err := tx.Commit(ctx); err != nil {
        return fmt.Errorf("commit: %w", err)
    }
    return nil
}
```

### Retry para Serialization Failures

En PostgreSQL con `REPEATABLE READ` o `SERIALIZABLE`, las transacciones pueden fallar con `40001` (serialization failure) y deben reintentarse:

```go
func withRetry(ctx context.Context, db *pgxpool.Pool, fn func(pgx.Tx) error) error {
    const maxRetries = 3
    for attempt := range maxRetries {
        tx, err := db.Begin(ctx)
        if err != nil {
            return fmt.Errorf("begin: %w", err)
        }

        err = fn(tx)
        if err == nil {
            return tx.Commit(ctx)
        }
        tx.Rollback(ctx)

        // Serialization failure — solo este error justifica retry
        var pgErr *pgconn.PgError
        if errors.As(err, &pgErr) && pgErr.Code == "40001" {
            backoff := time.Duration(attempt+1) * 50 * time.Millisecond
            select {
            case <-time.After(backoff):
                continue
            case <-ctx.Done():
                return ctx.Err()
            }
        }
        return err // cualquier otro error: no reintentar
    }
    return fmt.Errorf("max retries exceeded")
}
```

---

## Patrón Repository — Testabilidad sin Mocks de Framework

La interface va en el paquete que la usa (el dominio), no en el paquete que la implementa (la persistencia).

```go
// internal/user/store.go
package user

// Store es la interface definida por el dominio user.
// No sabe nada de postgres, pgx, ni sql — solo habla "usuario".
type Store interface {
    GetByID(ctx context.Context, id int64) (*User, error)
    Create(ctx context.Context, u *User) error
    Update(ctx context.Context, u *User) error
    Delete(ctx context.Context, id int64) error
    ListActive(ctx context.Context) ([]*User, error)
}

// PostgresStore es la implementación de producción
type PostgresStore struct {
    q *db.Queries // generado por sqlc
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
    return &PostgresStore{q: db.New(pool)}
}

func (s *PostgresStore) GetByID(ctx context.Context, id int64) (*User, error) {
    row, err := s.q.GetUser(ctx, id)
    if err != nil {
        if errors.Is(err, pgx.ErrNoRows) {
            return nil, ErrNotFound
        }
        return nil, fmt.Errorf("get user %d: %w", id, err)
    }
    return rowToUser(row), nil
}
```

```go
// En tests — mock manual, sin frameworks
type mockStore struct {
    users  map[int64]*User
    nextID int64
}

func newMockStore() *mockStore {
    return &mockStore{users: make(map[int64]*User), nextID: 1}
}

func (m *mockStore) GetByID(_ context.Context, id int64) (*User, error) {
    u, ok := m.users[id]
    if !ok {
        return nil, ErrNotFound
    }
    return u, nil
}

func (m *mockStore) Create(_ context.Context, u *User) error {
    u.ID = m.nextID
    m.nextID++
    m.users[u.ID] = u
    return nil
}
```

---

## Migrations — golang-migrate

```bash
# Instalar
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

# Crear una migration
migrate create -ext sql -dir migrations -seq create_users_table
# Genera: 000001_create_users_table.up.sql
#         000001_create_users_table.down.sql
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

### Embed Migrations en el Binario

```go
import "embed"
import "github.com/golang-migrate/migrate/v4/source/iofs"

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
    if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
        return fmt.Errorf("migrate up: %w", err)
    }
    return nil
}
```

---

## Checklist de Databases

- [ ] Pool configurado con `MaxOpenConns`, `MaxIdleConns`, `ConnMaxLifetime`
- [ ] Siempre `QueryContext` / `ExecContext` — nunca `Query` / `Exec` sin context
- [ ] `defer rows.Close()` inmediatamente después de `Query`
- [ ] `rows.Err()` verificado después del loop de `rows.Next()`
- [ ] `defer tx.Rollback()` inmediatamente después de `BeginTx`
- [ ] Errors de DB traducidos a errores de dominio antes de salir del paquete (`ErrNotFound`, etc.)
- [ ] Interface `Store` definida en el paquete de dominio, no en el de persistencia
- [ ] Migrations embebidas en el binario con `go:embed`
