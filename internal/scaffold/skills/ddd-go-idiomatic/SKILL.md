---
name: ddd-go-idiomatic
description: >
  Tactical Domain-Driven Design written as idiomatic Go, not translated Java: value
  objects, entities, aggregates, bounded contexts, repositories/ports, use cases and the
  file layout of a context. Use when designing, generating or reviewing Go domain code or
  deciding where logic, a use case or a file goes. Triggers: "value object", "entity",
  "aggregate", "bounded context", "use case", "repository pattern", "DDD in Go",
  "hexagonal/clean architecture in Go", "where should this logic live", service.go,
  usecase.go, handler.go, "file naming", "how do I name this file", "one use case per
  file", "caso de uso", "objeto de valor", "agregado", "contexto acotado", "dónde va
  esta lógica", "cómo nombro este archivo".
---

# DDD, the Go Way

DDD is a set of heuristics about **where behaviour lives**, not a folder structure or a
framework. This skill applies them with Go's own tools: packages, unexported fields,
consumer-defined interfaces and plain constructors.

**Rule #0: when DDD and Go idioms conflict, Go idioms win.** Non-idiomatic Go does not
only cause functional bugs; it causes architectural ones (import cycles, interface
pollution, needless indirection and allocations) that stop the codebase from scaling. If a
DDD rule pushes towards a Java-ism, the rule gives way.

Related skills: `go-design-rules` owns the general design principles (Law of Demeter,
Tell-Don't-Ask, CQS, named types, illegal states, explicit dependencies, DRY/SOLID);
`senior-go-idiomatic` owns comment policy, error handling, concurrency, testing and project
layout outside a bounded context.

## Building blocks and their Go shape

| Block | What it is | Idiomatic Go | Receiver |
|---|---|---|---|
| Value object | Value without identity, immutable, validates its unit | struct with unexported field + `NewX(raw) (X, error)` | value |
| Entity | Identity (ID), mutable, composed of VOs | struct with unexported fields + ID type + intent-revealing methods | pointer |
| Aggregate root | Single entry to a consistency boundary | entity whose unexported interior enforces the boundary | pointer |
| Bounded context | Language boundary | one package under `internal/` | — |

- **Value object**: immutable, "never update, create a new one"; equality with `==` (an `Equal` method if it holds a slice). A defined type like `type Email string` is not a safe VO, because untyped constants and conversions from any package bypass validation. → [value-objects.md](references/value-objects.md)
- **Entity**: equality by ID (`SameAs`), never `==`; no getters/setters; `NewUser(id, email, name, now) (*User, error)` checks how the VOs fit together, not what each VO already checks, and receives time instead of calling `time.Now()`. → [entities.md](references/entities.md)
- **Aggregate**: unexported fields make package privacy the boundary; one aggregate per transaction; other aggregates referenced **by ID, not pointer**. → [aggregates.md](references/aggregates.md)
- **Bounded context**: context = package; **folder per context, not per layer**. → [bounded-contexts.md](references/bounded-contexts.md)

## Business vs application

- **Business logic** lives in entities and VOs and is **pure**: the domain package imports no `database/sql`, `pgx`, `net/http` or `encoding/json`. Its import block is your architecture test (`go list -deps`).
- **Application logic** (use cases) orchestrates domain + ports: `Input → parse VOs → call ports / entities → Output`. The use case knows the repository; the domain does not.
- **Ports are declared by the consumer** (the context package) and implemented by infrastructure subpackages (`identity/pgstore`, `identity/memstore`), which translate driver errors into domain errors at the border.
- **Domain errors become HTTP status at the edge** (`identity/identityhttp`), never inside the domain or the use case.

→ [application-layer.md](references/application-layer.md)

## Files and use cases: one file = one intent

Inside a bounded context, each file name says what the file does, so the package listing
is the index of what the context can do. These rules are owned here; other skills only
link to them.

- **One use case = one type + one file**, named after the action in snake_case: `register_user.go` → `RegisterUser`, `sign_up.go` → `SignUp`, `log_in.go` → `LogIn`, `change_email.go` → `ChangeEmail`. A file never holds two use cases, however short: each one has its own dependencies and its own reason to change.
- **Fixed shape**: `NewRegisterUser(deps...) *RegisterUser` and a single exported `Execute(ctx context.Context, in RegisterUserInput) (RegisterUserOutput, error)`. Input is always a struct, even for a single ID, so adding a field never breaks callers. DTOs and ports used only by that use case live in the same file. No `Command`/`Handler`/`UseCase[I, O]` ceremony.
- **Domain concepts** by name: `user.go`, `email.go`, `order.go`; `errors.go` for the context's sentinels.
- **Shared ports** in their own file: `user_repository.go` → `type UserRepository interface { FindByID; ExistsByEmail; Save }`, small, defined in the context package.
- **Infrastructure** in a subpackage of the context (`identity/memstore`, `identity/pgstore`), never in the domain package.
- **Transport** in `identity/identityhttp/`: one file per endpoint named after its use case plus `routes.go`, using `net/http` method patterns (`mux.Handle("POST /users", ...)`, Go 1.22+).
- **Forbidden inside a context**, because they name a layer or a drawer instead of an intent: `service.go`, `services.go`, `usecase.go`, `usecases.go`, `use_cases.go`, `application.go`, `app.go`, `manager.go`, `helpers.go`, `utils.go`, `common.go`, a `handler.go`/`handlers.go` with several endpoints, and grouping types `Service`, `UseCase`, `Manager`. The composition root (`internal/handlers/handlers.go`, `cmd/<app>/main.go`) is the exception: wiring is its single intent.

Comments in generated code follow the `senior-go-idiomatic` comment policy (one-line package
doc in one file per package, one-line doc comments that add information, no narration).

→ full rules and forbidden-name table in [application-layer.md](references/application-layer.md) §2; compiled example in [worked-example.md](references/worked-example.md).

## Anti-patterns (Java-ism → idiomatic Go)

| Java-ism | Idiomatic Go |
|---|---|
| Folders per layer (`domain/`, `application/`, `infrastructure/`) | Package per context, file per concept or use case |
| Interface per class | Interface at the consumer, only with a 2nd implementation or a test seam |
| `GetX`/`SetX` | Intent-revealing methods; no `Get` prefix |
| Anemic model + fat service | Rich model: logic lives with the data |
| `mapper/` layer + reflection auto-mapper | DTOs next to the use case; hand translation |
| Pointer between aggregates | Reference by ID |
| Repository leaking `pgconn.PgError` / `sql.ErrNoRows` | Translated to a domain error at the border |
| Mutable VO with setters | Immutable; create a new one |
| `service.go` with `type Service` and several use cases | One file and one type per use case (`log_in.go` → `LogIn`) |

→ full catalogue (13, with BAD/GOOD) in [antipatterns.md](references/antipatterns.md).

## Decision checklist

Before writing a domain type or a use case:

- [ ] Does it have an identity that persists over time? → **Entity**. Defined only by its attributes? → **Value object**.
- [ ] VO: unexported field + `NewX(...) (X, error)` + value receiver + immutable?
- [ ] Entity: pointer receiver + equality by ID + intent-revealing methods (no `Set`) + `now` passed in?
- [ ] Is there a transactional invariant spanning several entities? → **Aggregate root**. If not, do not invent one.
- [ ] Does this logic touch repositories, HTTP or libraries? → it belongs in the **use case**, not the domain.
- [ ] Does the domain package import `database/sql`, `net/http` or `encoding/json`? → infrastructure leaked.
- [ ] Am I creating `domain/`, `application/`, `infrastructure/`, `models/`, `dto/`, `util/`? → reorganize by context.
- [ ] Is the file called `service.go`, `usecase.go`, `app.go`, `manager.go`, `helpers.go` or `handler.go`? → rename it after what it does, or split it.
- [ ] Does each use case have `NewX`, one `Execute(ctx, XInput) (XOutput, error)` and its DTOs next to it, in its own file?

## Reference files

Read the one that matches the question; do not load them all.

- [bounded-contexts.md](references/bounded-contexts.md): drawing context boundaries, package vs folder layout, integration via consumer-defined interfaces, events and an ACL, when to split into a service.
- [value-objects.md](references/value-objects.md): designing a VO, struct vs defined type, validating constructors, equality, typed/sentinel errors, reconstitution from storage.
- [entities.md](references/entities.md): identity, pointer receivers, equality by ID, constructors that take `now`, accessors vs setters, rich vs anemic.
- [aggregates.md](references/aggregates.md): choosing a root, consistency boundary, one aggregate per transaction, references by ID, authorization in the use case, not exposing internals.
- [application-layer.md](references/application-layer.md): use-case shape and file rules, forbidden names, port placement, infrastructure error translation, DTOs, error → status mapping, wiring.
- [antipatterns.md](references/antipatterns.md): reviewing code that looks like Java/C# DDD; 13 BAD/GOOD pairs.
- [worked-example.md](references/worked-example.md): when you need a complete, compiled context (`identity`) to copy from: VOs → entity → port → two use cases → memstore → HTTP → wiring → race-tested tests.
