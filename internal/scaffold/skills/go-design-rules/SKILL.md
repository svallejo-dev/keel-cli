---
name: go-design-rules
description: >
  Design and coupling rules applied the idiomatic Go way — Law of Demeter,
  Tell-Don't-Ask, Command–Query Separation, domain naming (named types over
  primitives), make-illegal-states-unrepresentable / parse-don't-validate,
  explicit dependencies (no globals/singletons), and DRY/SOLID read through a Go
  lens. Use when designing or reviewing Go code for coupling, naming, type-safety,
  or dependency smells. Trigger on "law of demeter", "tell don't ask", "command
  query separation", "CQS", "stringly-typed", "boolean blindness", "named types",
  "make illegal states unrepresentable", "parse don't validate", "dependency
  injection in Go", "no globals", "DRY", "SOLID in Go", "is this good design",
  "design smell", "how should this be structured", "service.go", "usecase.go",
  "file naming", "one use case per file", or "too many comments". Rule #0: when a design
  principle conflicts with Go idioms, Go idioms win.
---

# Go Design Rules

Reglas de diseño y acoplamiento **alineadas a Go**. No son sobre sintaxis ni sobre DDD: son sobre las decisiones de acoplamiento, nombres, tipos y dependencias que hacen que el código escale. Cada regla trae su marco Go y su **trampa** (dónde el canon OOP se desalinea si lo aplicas literal).

## Regla #0 — go-idiomatic manda

Ante cualquier conflicto entre un principio de diseño (OOP/SOLID/DRY) y una convención idiomática de Go, **gana go-idiomatic**. El canon se cita **con asterisco**: importarlo entero —jerarquías de herencia, abstract factories, interfaz-por-clase, getters/setters— te empuja a Java-ismos que rompen en Go (ciclos de import, interface pollution, indirección y allocaciones). Toma lo que encaja, reinterpreta lo demás, descarta lo que exige herencia.

> Compañeras opcionales: `senior-go-idiomatic` (el idioma) y `ddd-go-idiomatic` (modelado de dominio). Esta skill es **autocontenida** y no depende de ellas.

## Principios (regla en Go → smell que mata)

| Principio | Regla en Go | Smell que mata |
|---|---|---|
| Ley de Demeter | No atravieses objetos ajenos; Tell-Don't-Ask | *train-wreck* `a.B().C().D()` sobre grafos que no posees |
| Command–Query Separation | Command muta y devuelve `error`; query es pura | getter que muta al vuelo y devuelve |
| Nombres de dominio | Named types sobre primitivos | *stringly-typed*, *boolean blindness* |
| Métodos con intención | Verbo de negocio, no CRUD | `SetStatus` / `SetBalance` que rompe invariantes |
| Archivos con intención | Un caso de uso por archivo, nombrado por lo que hace | `service.go`/`usecase.go` con `Login()` + `Signup()` |
| Illegal states unrepresentable | Parse-don't-validate; constructores `(T, error)` | validar el mismo dato en 10 sitios |
| Dependencias explícitas | Inyección por constructor | `var` global mutable, singleton, service locator |
| DRY con matiz | No dupliques **conocimiento** (no código que se parece) | `common/`/`shared/` que acopla dominios distintos |
| SOLID selectivo | ISP y DIP son nativos; OCP por composición | herencia simulada, interfaz-por-clase |

La **trampa** más importante, para no traicionar la Regla #0: la Ley de Demeter **no** es "la regla del punto único" — encadenar sobre valores que *posees* o sobre la stdlib componible (`strings.ToLower(strings.TrimSpace(s))`, `io.Copy`, `errors.Is`, functional options) es idiomático y no la viola. La regla real es: no dependas de la **estructura** de lo que te devuelven tus colaboradores.

## Guardián: archivos y tipos con intención

En un bounded context, **el nombre del archivo dice qué hace**. Cada caso de uso vive en
su archivo (`login.go`, `signup.go`, `search_users.go`) con **un** struct de nombre de
acción (`Login`), constructor `NewLogin(deps...)` y un único método exportado
`Execute(ctx, in LoginInput) (LoginOutput, error)`; sus DTOs y puertos estrechos van en
el mismo archivo (un puerto compartido, en su propio archivo: `user_repository.go`).

- **Prohibidos** dentro de un contexto: `service.go`, `services.go`, `usecase.go`,
  `usecases.go`, `use_cases.go`, `application.go`, `app.go`, `manager.go`, `helpers.go`,
  `utils.go`, `common.go`, y `handler.go`/`handlers.go` con varios endpoints (transporte:
  un archivo por endpoint + `routes.go`).
- **Prohibidos** los tipos cajón `Service`, `UseCase`, `Manager`.
- **Permitidos**: conceptos (`user.go`, `email.go`), `errors.go`, puertos con nombre
  (`user_repository.go`), `routes.go`, y composition roots fuera de los contextos
  (`internal/handlers/handlers.go` solo monta rutas).

Detalle, tabla BAD → GOOD y el caso Login/Signup: `references/domain-naming.md` §7.

Comentarios: el nombre ya documenta. Doc comment de una línea en exportados solo si dice
algo que el nombre no dice; nada de narrar el código ni banners `// ----` (política
completa en `senior-go-idiomatic`, `references/idioms-and-style.md` §5).

## Checklist de smells

Revisa un cambio contra esto:

- [ ] ¿Hay un *train-wreck* `x.A().B().C()` sobre objetos que no posees? → Tell-Don't-Ask, o pasa solo el dato.
- [ ] ¿Un método muta **y** devuelve dato calladamente? → separa command de query.
- [ ] ¿Parámetros `int64`/`string`/`bool` crudos donde hay un concepto? → named type / enum / functional option.
- [ ] ¿Un `SetX` que puede dejar el objeto en un estado inválido? → método con verbo de intención que preserva la invariante.
- [ ] ¿Se valida el mismo valor en varios sitios? → parsea a un tipo válido **una** vez en la frontera.
- [ ] ¿Un `var` global mutable, un singleton o un service locator? → inyecta la dependencia por constructor.
- [ ] ¿Un paquete `common/`/`shared/`/`util/` deduplicando dominios distintos? → sepáralos (acoplan por conveniencia).
- [ ] ¿Herencia simulada (`BaseX`), abstract factory o una interfaz por cada struct? → composición + interfaces pequeñas consumer-defined.
- [ ] ¿Un `service.go`/`usecase.go`/`manager.go`/`utils.go`, o un archivo con dos casos de uso (`Login()` + `Signup()`)? → un archivo y un struct por caso de uso, nombrado por su intención.
- [ ] ¿Un tipo `Service`/`UseCase`/`Manager` que agrupa operaciones? → un struct por acción con `Execute`.
- [ ] ¿Comentarios que narran el código o pesan más que él? → bórralos; deja solo el "por qué" no obvio.

## Reference Files

Lee el archivo relevante cuando la pregunta sea de ese tema. No cargues todos a la vez.

- `references/coupling-behavior.md` — Ley de Demeter (y su trampa), Tell-Don't-Ask, Command–Query Separation
- `references/domain-naming.md` — named types sobre primitivos, muerte al stringly-typed y al boolean blindness, métodos con verbo de intención, archivos nombrados por intención (un caso de uso por archivo)
- `references/illegal-states.md` — make illegal states unrepresentable / parse-don't-validate, constructores, enums con iota, functional options, sealed interfaces
- `references/dependencies.md` — inyección por constructor, cero globals/singletons/service locator, clock/rand como dependencias, wiring en `main`
- `references/dry-solid-go.md` — DRY subordinado al desacople, SOLID selectivo (ISP/DIP nativos, OCP por composición, LSP como contrato de interfaz)
