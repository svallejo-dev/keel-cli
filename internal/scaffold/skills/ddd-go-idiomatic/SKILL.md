---
name: ddd-go-idiomatic
description: >
  Domain-Driven Design (tactical) applied the idiomatic Go way — not copied from
  Java/C#. Use this skill when designing or reviewing a Go domain model: value
  objects, entities, aggregates, bounded contexts, repositories, use cases /
  application services, or the domain-vs-application split. Trigger on "value
  object", "entity", "aggregate", "aggregate root", "bounded context", "use case",
  "repository pattern", "domain layer in Go", "DDD in Go", "hexagonal / clean
  architecture in Go", "where should this logic live", "is this an entity or a
  value object", or when translating DDD/Java layering into idiomatic Go. Also
  trigger when creating, naming or reviewing files and types inside a bounded
  context (service.go, usecase.go, handler.go, "where does this use case go",
  "how do I name this file"), and when generated Go code is over-commented.
  Rule #0: when DDD and Go idioms conflict, Go idioms win.
---

# DDD Go-Idiomatic Skill

Diseñas y revisas modelos de dominio en Go con las heurísticas de DDD, pero **traducidas al idioma de Go**, no copiadas de Java/C#.

## Regla #0 — go-idiomatic manda

Ante cualquier conflicto entre un principio de DDD y una convención idiomática de Go, **gana go-idiomatic**. DDD es un conjunto de heurísticas sobre *dónde vive el comportamiento*, no una estructura de carpetas ni un framework. Escribir Go no idiomático no genera solo bugs funcionales: genera **bugs arquitectónicos** —ciclos de import, interface pollution, indirección y allocaciones innecesarias— que impiden que la app escale. Si una regla de DDD te empuja a un Java-ismo, es la regla la que cede.

> Companion opcional: la skill `senior-go-idiomatic` cubre el detalle del idioma (receivers, interfaces, project layout, errores). Esta skill es **autocontenida** y no depende de ella.

## Los building blocks (y su forma en Go)

| Bloque | Qué es | Forma idiomática en Go | Receiver |
|---|---|---|---|
| Value Object | Valor sin identidad, inmutable; valida su unidad | `struct{ v ... }` con campo no-exportado + `NewX() (X, error)` | value |
| Entity | Identidad (ID), mutable; compone VOs | struct con campos privados + ID + métodos con intención | pointer |
| Aggregate Root | Puerta única a un límite de consistencia | entity cuyo interior no-exportado impone el límite | pointer |
| Bounded Context | Frontera de lenguaje | un paquete dentro de `internal/` | — |

Reglas duras:

- **Value Object**: value receiver, inmutable, "nunca actualiza, crea uno nuevo". Igualdad por valor (`==`; `Equal` si lleva slice). → `references/value-objects.md`
- **Entity**: pointer receiver, igualdad por **ID** (nunca `==`), sin getters/setters, comportamiento con intención. El constructor valida la **integración** de VOs, no lo que cada VO ya valida. → `references/entities.md`
- **Aggregate**: campos no-exportados ⇒ la privacidad de paquete impone el límite; 1 aggregate por transacción; referencia a otros aggregates **por ID, no por puntero**. → `references/aggregates.md`
- **Bounded Context**: contexto = paquete; **carpeta por contexto, NO por capa**. → `references/bounded-contexts.md`

## Negocio vs Aplicación

- **Lógica de negocio**: vive en entidades y value objects. **Pura** — sin imports de `database/sql`, `net/http` ni libs externas. El bloque `import` del paquete de dominio es tu test de arquitectura.
- **Lógica de aplicación** (use cases): orquesta dominio + repositorios. Dos columnas:
  - **Integración** (use case): `Input → orquesta repos/entidades → Output`. Conoce el repo; el dominio no.
  - **Transformación** (DTO): los structs `Input`/`Output` del use case; sin capa `mapper/`.
- La **interfaz de repositorio la define el consumidor** (aplicación); la implementa la infra, que traduce errores del driver a errores de dominio en la frontera. → `references/application-layer.md`

Flujo: `Input → Use Case (orquesta repositories + entities/aggregates) → Output`.

## Archivos y casos de uso: un archivo = una intención

Dentro de un bounded context, el nombre de cada archivo dice qué hace. Abrir el árbol del paquete debe bastar para saber qué sabe hacer el contexto.

- **Un caso de uso = un tipo + un archivo**, con el nombre de la acción en snake_case: `register_user.go` → `RegisterUser`, `login.go` → `Login`, `search_users.go` → `SearchUsers`. Un archivo **nunca** contiene dos casos de uso (`Login()` y `SignUp()` juntos es una violación, aunque sean cortos).
- **Forma fija**: `NewRegisterUser(deps...) *RegisterUser` + un único método exportado `Execute(ctx context.Context, in RegisterUserInput) (RegisterUserOutput, error)`, con `RegisterUserInput`/`RegisterUserOutput` en el mismo archivo. Sin `Command`/`Handler`/`UseCase[I, O]`.
- **Conceptos del dominio** por su nombre: `user.go`, `email.go`, `order.go`; `errors.go` para los sentinela del contexto.
- **Puertos** en el consumidor: en el archivo del caso de uso si solo él lo usa; si varios lo comparten, en su propio archivo (`user_repository.go` → `UserRepository`).
- **Transporte** (`<ctx>http/`): un archivo por endpoint con el nombre del caso de uso (`register_user.go`) + `routes.go` que los monta.
- **Prohibidos** dentro de un contexto: `service.go`, `services.go`, `usecase.go`, `usecases.go`, `use_cases.go`, `application.go`, `app.go`, `manager.go`, `helpers.go`, `utils.go`, `common.go`, y `handler.go`/`handlers.go` con varios endpoints. Tampoco tipos `Service`, `UseCase` o `Manager` que agrupen operaciones.

→ forma completa en `references/application-layer.md`; ejemplo compilable en `references/worked-example.md`.

## Comentarios: el código no se narra

- Package doc de **una línea**, en un solo archivo del paquete.
- Exportados: doc comment de **una línea**, y solo si dice algo que el nombre no dice (`// NewUser exige nombre y apellido cuando el email es corporativo.`). `// RegisterUser registra un usuario.` sobra.
- No exportados: sin comentario, salvo un **por qué** no obvio.
- Prohibido: comentarios que narran el código (`// guardamos el usuario`), banners `// ----`, comentarios que repiten el nombre, bloques que explican la arquitectura. Eso va en el README o en el ADR, no en el `.go`.
- Si un bloque generado tiene más líneas de comentario que de código, está mal: recórtalo antes de entregarlo.

## Anti-patrones (Java-ismo → Go idiomático)

| Java-ismo | Go idiomático |
|---|---|
| Carpetas por capa (`domain/`, `application/`, `infrastructure/`) | Paquete por contexto, archivo por rol |
| Interfaz por clase | Interfaz en el consumidor, solo con 2ª impl o seam de test |
| Getters/Setters (`GetX`/`SetX`) | Métodos con intención; sin `Get` |
| Modelo anémico (datos + service gordo) | Rich model: la lógica vive con los datos |
| Capa `mapper/` + auto-mapper (reflection) | DTO junto al use case; traducción a mano |
| Referencia entre aggregates por puntero | Por ID |
| Repo que filtra `pgconn.PgError` / `sql.ErrNoRows` | Traduce a error de dominio en la frontera |
| Value object mutable / con setters | Inmutable; crea uno nuevo |
| `service.go`/`usecase.go` con `type Service` y varios casos de uso | Un archivo y un tipo por caso de uso (`login.go` → `Login`) |

→ catálogo completo (13) en `references/antipatterns.md`.

## Checklist de decisión

Antes de escribir un tipo de dominio:

- [ ] ¿Tiene identidad que persiste en el tiempo? → **Entity**. ¿Se define solo por sus atributos? → **Value Object**.
- [ ] Si es VO: ¿campo no-exportado + `NewX() (X, error)` + value receiver + inmutable?
- [ ] Si es Entity: ¿pointer receiver + igualdad por ID + métodos con intención (sin `Set`)?
- [ ] ¿Hay una invariante transaccional que abarque varias entidades? → **Aggregate root**. Si no, no inventes uno.
- [ ] ¿Esta lógica toca repos/http/libs? → va en el **use case**, no en el dominio.
- [ ] ¿El paquete de dominio importa `database/sql` o `net/http`? → se filtró la infraestructura.
- [ ] ¿Estoy creando `domain/`, `application/`, `infrastructure/`, `models/`, `dto/`, `util/`? → reorganiza por contexto.
- [ ] ¿El archivo se llama `service.go`, `usecase.go`, `app.go`, `manager.go`, `helpers.go` o `handler.go`? → renómbralo por lo que hace o pártelo.
- [ ] ¿Este archivo contiene más de un caso de uso? → un archivo y un tipo por caso de uso (`RegisterUser` en `register_user.go`).
- [ ] ¿Cada caso de uso tiene `NewX`, un único `Execute(ctx, XInput) (XOutput, error)` y sus DTO al lado?
- [ ] ¿Hay comentarios que narran el código o repiten el nombre? → bórralos; deja solo el *por qué* no obvio.

## Reference Files

Lee el archivo relevante cuando la pregunta sea de ese tema. No cargues todos a la vez.

- `references/bounded-contexts.md` — contexto = paquete en `internal/`, ubiquitous language, carpeta por contexto no por capa, integración por interfaz publicada / eventos / ACL
- `references/value-objects.md` — inmutabilidad por convención, alias vs struct, constructor validador, igualdad `==`/`Equal`, errores tipados
- `references/entities.md` — identidad + pointer receivers, igualdad por ID, constructor que integra VOs, sin getters/setters, rich vs anémico
- `references/aggregates.md` — root, límite de consistencia, la privacidad de paquete como frontera, 1 aggregate/tx, referencia por ID
- `references/application-layer.md` — un caso de uso = un tipo + un archivo, nombres prohibidos, repositorio consumer-defined, DTO sin `mapper/`, mapeo error→status, test de pureza del import
- `references/antipatterns.md` — catálogo Java-ismo → Go idiomático (13 anti-patrones, con BAD/GOOD)
- `references/worked-example.md` — contexto `identity` completo y **compilable**: VO → entity → puertos → dos casos de uso en dos archivos → infra → un endpoint por archivo → wiring → tests
