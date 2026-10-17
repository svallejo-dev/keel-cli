# Package Oriented Design — Go Project Layout

## The Core Principle

> **A package is not a namespace. A package is a unit of design.**

This is the defining difference between Package Oriented Design and how Java/C# engineers approach Go. In Java, packages are folders that group related things. In Go, **a package defines what it IS** — its purpose, its data, and its behavior are inseparable.

The question is never *"where should I put this file?"*  
The question is *"what single concept does this package represent?"*

---

## What a Package IS (and is NOT)

### A Package IS:
- A single, self-contained concept with a clear boundary
- The owner of its own data — it does not share raw types with other packages
- Named after what it represents, not what it does
- Small enough that its entire API can be understood in one reading

### A Package is NOT:
- A folder to dump related files
- A Java-style namespace (`com.company.utils`)
- A collection of helpers for other packages
- An excuse to avoid thinking about design

### The Naming Test
If you cannot name your package with a **single, specific noun**, the package has no identity yet.

| Bad Name | Why It Fails | What You're Really Designing |
|---|---|---|
| `util` | It's a drawer, not a concept | Break into specific packages |
| `helper` | Helps what? | A package that helps nothing |
| `common` | Common to whom? | Shared coupling disguised as reuse |
| `types` | Types of what? | An anemic data layer |
| `models` | ORM thinking | Domain types belong inside their domain package |
| `base` | Base of what? | Missing abstraction |
| `shared` | Red flag: forced coupling | Rethink the boundaries |
| `manager` | Manages what? | A God object disguised as a package |

---

## Canonical Project Structure

```
myservice/
├── cmd/
│   └── myservice/
│       └── main.go          ← Thin: wires dependencies, calls Run(), exits
├── internal/
│   ├── user/                ← The "user" concept owns its data AND behavior
│   │   ├── user.go          ← Type definition + constructors
│   │   ├── get_user.go      ← Use case GetUser + its DTOs and narrow ports
│   │   ├── register_user.go ← Use case RegisterUser (one use case per file)
│   │   ├── store.go         ← PostgresStore: implements the ports
│   │   ├── user_test.go
│   │   └── userhttp/        ← Transport: one file per endpoint + routes.go
│   │       ├── get_user.go
│   │       ├── register_user.go
│   │       └── routes.go
│   ├── auth/                ← The "auth" concept: tokens, sessions, claims
│   │   ├── auth.go
│   │   ├── token.go
│   │   └── middleware.go
│   ├── platform/            ← Cross-cutting infrastructure (not "util")
│   │   ├── postgres/        ← DB connection + pool setup
│   │   ├── logger/          ← Logger initialization
│   │   └── config/          ← Config loading and validation
│   └── app/                 ← Wires everything together; the application graph
│       └── run.go
├── go.mod
└── go.sum
```

### `cmd/` — Entry Points Only
Each subdirectory of `cmd/` is a `main` package. Its job is **exclusively** to:
1. Parse flags / environment
2. Build the dependency graph
3. Call the top-level `Run(ctx)` function
4. Handle the exit code

If `main.go` is longer than ~50 lines, business logic has leaked into it.

```go
func main() {
    cfg, err := config.Load()
    if err != nil {
        log.Fatalf("config: %v", err)
    }

    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer stop()

    if err := app.Run(ctx, cfg); err != nil {
        log.Fatalf("run: %v", err)
    }
}
```

### `internal/` — The Compiler-Enforced Boundary
`internal/` is **not a convention** — it is enforced by the Go compiler. Code inside `internal/` can only be imported by code rooted at the parent of `internal/`. External packages and other modules cannot import it.

Use `internal/` for:
- All business logic that should not be a public API
- Domain packages (`user`, `order`, `payment`)
- Infrastructure wrappers (`platform/postgres`, `platform/redis`)

Use a public `pkg/` only for code you explicitly want other modules to import. When in doubt, `internal/` first.

---

## Package Oriented Design in Practice

### Each Domain Package Owns Its World

The `user` package owns the `User` type. Not `models.User`. Not `types.User`. The type, its use cases, the ports they need and the persistence all live together because they are all about the same concept. Each file is named after its intent: opening the directory lists what the concept can do.

```
internal/user/
├── user.go          ← type User struct { ... }  +  constructors
├── get_user.go      ← GetUser use case + GetUserOutput + userFinder port
├── store.go         ← PostgresStore: satisfies the ports
├── user_test.go
└── userhttp/
    ├── get_user.go  ← GET /users/{id}
    └── routes.go    ← mounts the endpoints
```

```go
// internal/user/user.go
package user

type User struct {
    ID        int64
    Email     string
    CreatedAt time.Time
}

// New is the only way to obtain a valid User.
func New(email string) (*User, error) {
    if !isValidEmail(email) {
        return nil, fmt.Errorf("user: invalid email %q", email)
    }
    return &User{Email: email, CreatedAt: time.Now()}, nil
}
```

```go
// internal/user/get_user.go
package user

type userFinder interface {
    ByID(ctx context.Context, id int64) (*User, error)
}

type GetUserOutput struct {
    ID    int64
    Email string
}

type GetUser struct{ users userFinder }

func NewGetUser(users userFinder) *GetUser { return &GetUser{users: users} }

func (uc *GetUser) Execute(ctx context.Context, id int64) (GetUserOutput, error) {
    u, err := uc.users.ByID(ctx, id)
    if err != nil {
        return GetUserOutput{}, fmt.Errorf("get user: %w", err)
    }
    return GetUserOutput{ID: u.ID, Email: u.Email}, nil
}
```

The port is declared in the file that consumes it, with only the method this use case needs — not a 10-method `Store` interface in a separate "interfaces" package.

```go
// internal/user/store.go
package user

type PostgresStore struct{ db *pgxpool.Pool }

func (s *PostgresStore) ByID(ctx context.Context, id int64) (*User, error) { ... }
```

```go
// internal/user/userhttp/get_user.go
package userhttp

func getUser(uc *user.GetUser) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
        out, err := uc.Execute(r.Context(), id)
        if errors.Is(err, user.ErrNotFound) {
            http.Error(w, "not found", http.StatusNotFound)
            return
        }
        // ...
    }
}
```

### Platform Packages — Not "util"

Infrastructure code that multiple domain packages need is NOT `util/`. It is a **specific named concept**:

```
internal/platform/
├── postgres/    ← package postgres: opens pool, runs health check, returns *pgxpool.Pool
├── logger/      ← package logger: builds *slog.Logger from config
├── config/      ← package config: loads and validates Config struct
└── httpserver/  ← package httpserver: builds *http.Server with timeouts, graceful shutdown
```

```go
// internal/platform/postgres/postgres.go
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
    cfg.MaxConnIdleTime = 1 * time.Minute

    pool, err := pgxpool.NewWithConfig(ctx, cfg)
    if err != nil {
        return nil, fmt.Errorf("postgres: open: %w", err)
    }
    if err := pool.Ping(ctx); err != nil {
        return nil, fmt.Errorf("postgres: ping: %w", err)
    }
    return pool, nil
}
```

### `app/` — The Dependency Graph

`app.Run` is the only place in the codebase that knows about all packages simultaneously. Its job is to wire the graph and nothing else.

```go
// internal/app/run.go
package app

func Run(ctx context.Context, cfg *config.Config) error {
    pool, err := postgres.Open(ctx, cfg.DatabaseURL)
    if err != nil {
        return fmt.Errorf("app: %w", err)
    }
    defer pool.Close()

    log := logger.New(cfg.LogLevel)

    userStore := &user.PostgresStore{Pool: pool}
    authMW := auth.NewMiddleware(&auth.PostgresStore{Pool: pool})

    mux := http.NewServeMux()
    userhttp.Routes(mux, authMW, userhttp.Deps{
        GetUser: user.NewGetUser(userStore),
    })

    return httpserver.New(cfg.Addr, mux).Run(ctx)
}
```

---

## Anti-Patterns — Spotted in the Wild

### The God Package
```
// BAD: one package that knows everything
internal/service/
├── user_service.go
├── auth_service.go
├── payment_service.go
└── email_service.go  // ← not a "service", it's everything
```

### The Grab-Bag File
```
// BAD: one file, several use cases — the name says nothing about any of them
internal/identity/
├── service.go   // type Service; func (s *Service) Login(...); func (s *Service) Signup(...)
└── handler.go   // every endpoint of the context

// GOOD: one file per use case, named by what it does
internal/identity/
├── login.go
├── signup.go
└── identityhttp/
    ├── login.go
    ├── signup.go
    └── routes.go
```
See "Nombres de Archivo por Intención" below.

### The Anemic Type Layer
```
// BAD: types separated from their behavior — Java DTO thinking
internal/models/
└── models.go  // type User struct { ... }  type Order struct { ... }

internal/repositories/
└── user_repo.go  // func GetUser(id int64) (*models.User, error)

internal/services/
└── user_service.go // func CreateUser(u *models.User) error
```
The `User` type is owned by nobody. Three packages must coordinate to do anything with a user.

### The Circular Import Smell
If packages A and B need to import each other, one of three things is true:
1. They are actually one concept and should be merged
2. A third package C needs to be extracted that both A and B depend on
3. An interface should replace the direct dependency

---

## Evitar Nested Packages

Go no tiene jerarquía de paquetes. Anidar un paquete dentro de otro **no** crea relación padre-hijo, **no** da acceso privilegiado a los identificadores no-exportados del "padre", y **no** comunica encapsulación. `internal/user/profile` no es "hijo" de `internal/user`: son dos paquetes independientes que resultan estar en carpetas anidadas. La visibilidad en Go es **por paquete**, nunca por carpeta.

Consecuencias de anidar sin motivo:
- Ruido en los import paths sin ganar nada de diseño.
- La suposición (falsa) de que el sub-paquete ve los no-exportados del de arriba. No los ve.
- Estructuras profundas (`internal/domain/user/service/impl/...`) son diseño Java/C# trasplantado.

Regla: **estructura plana por defecto**. Un nivel de paquetes de dominio bajo `internal/`. Anida SOLO cuando el sub-paquete es un concepto genuinamente distinto y subordinado que nadie más va a importar.

```
// BAD — capas técnicas disfrazadas de paquetes (models/repo/service = Java)
internal/
└── user/
    ├── service/      ← "service" no es un concepto
    ├── repository/   ← capa técnica, no dominio
    └── model/        ← el tipo User separado de su comportamiento

// GOOD — plano; cada carpeta es un concepto con nombre propio
internal/
├── user/            ← un paquete: tipo + casos de uso + store juntos
│   ├── user.go
│   ├── get_user.go
│   └── store.go
└── billing/         ← otro concepto, al mismo nivel
    ├── billing.go
    └── store.go
```

### `internal/` es la única frontera real
La encapsulación entre carpetas la impone el compilador SOLO con `internal/`: el código bajo `internal/X` solo puede importarlo el árbol enraizado en el padre de ese `internal/`. Anidar `internal/` a distintos niveles crea fronteras finas cuando de verdad las necesitas:

```
myservice/
└── internal/            ← privado a myservice
    └── billing/
        └── internal/    ← privado a billing: ni user ni auth pueden importar esto
            └── ledger/
```

Usa esto para ocultar detalles de un sub-dominio del resto de sub-dominios — nunca para simular capas.

---

## Nombres de Archivo por Intención

El paquete nombra el concepto; el **archivo** nombra la intención. Dentro de un bounded
context, cada caso de uso vive en su propio archivo, en `snake_case`, nombrado por lo
que hace (`login.go`, `signup.go`, `search_users.go`). Un archivo **nunca** contiene dos
casos de uso: un `service.go` con `Login()` y `Signup()` es un cajón que obliga a abrirlo
para saber qué hay.

| Prohibido dentro de un contexto | Correcto |
|---|---|
| `service.go`, `services.go` | un archivo por caso de uso: `login.go`, `signup.go` |
| `usecase.go`, `usecases.go`, `use_cases.go` | `register_user.go`, `search_users.go` |
| `application.go`, `app.go` | idem: la capa no es una intención |
| `manager.go`, `helpers.go`, `utils.go`, `common.go` | el archivo del concepto al que pertenece el código |
| `handler.go` / `handlers.go` con varios endpoints | `<ctx>http/login.go` por endpoint + `routes.go` |

Permitidos: conceptos (`user.go`, `email.go`), `errors.go`, un puerto compartido con su
nombre (`user_repository.go`), `store.go` para la implementación de persistencia,
`routes.go`. Fuera de los contextos, un composition root (`internal/handlers/handlers.go`
que solo monta rutas, `internal/app/run.go`) es aceptable: tiene una sola intención.

El tipo sigue al archivo: un struct por caso de uso con nombre de acción, constructor
`NewX(deps...)` y un único método exportado `Execute(ctx, in XInput) (XOutput, error)`;
DTOs y puertos estrechos en el mismo archivo. Nada de tipos cajón `Service`, `UseCase` o
`Manager`.

```go
// internal/identity/signup.go
package identity

type SignupInput struct {
    Email    string
    Password string
}

type SignupOutput struct{ UserID string }

type accountCreator interface {
    Create(ctx context.Context, a Account) error
}

type Signup struct{ accounts accountCreator }

func NewSignup(accounts accountCreator) *Signup { return &Signup{accounts: accounts} }

func (uc *Signup) Execute(ctx context.Context, in SignupInput) (SignupOutput, error) {
    // ...
}
```

---

## Dirección de Dependencias (Grafo Acíclico)

Las dependencias entre paquetes deben formar un **DAG** (grafo dirigido acíclico) que apunta hacia adentro: `wiring → aplicación → dominio`. El dominio (`user`, `billing`) **no** importa infraestructura (`platform/postgres`); la infraestructura implementa las interfaces que el dominio define.

```
cmd/ ──▶ internal/app ──▶ internal/user ──▶ (define la interfaz Store)
                    │                              ▲
                    └──▶ internal/platform/postgres ┘  (implementa Store)
```

- Las flechas nunca vuelven: `postgres` puede depender de `user` (para implementar su `Store`), pero `user` **jamás** importa `postgres`.
- Un ciclo lo rechaza el compilador. Es una feature: te obliga a arreglar el diseño (ver "The Circular Import Smell").
- `go mod graph` (a nivel de módulo) y herramientas como `go-cleanarch` (a nivel de paquete) verifican la dirección.

---

## Rules Summary

1. **Name packages after what they represent, not what they do**
2. **Each package owns its types** — no shared `models/` or `types/` package
3. **Define interfaces at the point of use** — inside the package that depends on them, not inside the package that implements them
4. **`internal/` first** — expose a public API only when another module needs it
5. **`cmd/` packages are wiring only** — 50 lines max, no business logic
6. **Platform packages are named concepts** — `postgres`, `redis`, `logger`, never `util`
7. **If you can't name it, you haven't designed it yet** — applies to files too: one use case per file, named by intent; no `service.go`/`usecase.go`/`utils.go`
8. **Estructura plana** — anidar paquetes no crea jerarquía ni encapsulación; solo `internal/` es una frontera real
9. **Dependencias en DAG** — el dominio define interfaces, la infraestructura las implementa; las flechas nunca vuelven
