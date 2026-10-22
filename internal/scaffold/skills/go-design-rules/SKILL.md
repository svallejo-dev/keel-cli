---
name: go-design-rules
description: >
  Cross-cutting design principles applied the idiomatic Go way: Law of Demeter,
  Tell-Don't-Ask, Command–Query Separation, named types over primitives, making illegal
  states unrepresentable (parse, don't validate), explicit dependencies instead of
  globals, and DRY/SOLID read through a Go lens. Use when designing or reviewing Go code
  for coupling, type-safety or dependency smells. Triggers: "law of demeter", "train
  wreck", "tell don't ask", "CQS", "stringly-typed", "boolean blindness", "named types",
  "illegal states unrepresentable", "parse don't validate", "dependency injection in Go",
  "global variables", "singleton", "DRY", "SOLID in Go", "design smell", "acoplamiento",
  "ley de Demeter", "estados ilegales", "inyección de dependencias".
---

# Go Design Rules

Design and coupling rules aligned with Go. They are not about syntax or about DDD: they are
about the coupling, naming, typing and dependency decisions that let code keep scaling. Each
rule comes with its Go framing and its **trap**: where the OOP canon misleads you if applied
literally.

**Rule #0: when a design principle conflicts with a Go idiom, the Go idiom wins.** Quote the
canon with an asterisk: importing it whole (inheritance hierarchies, abstract factories,
interface per class, getters/setters) pushes towards Java-isms that break in Go (import
cycles, interface pollution, indirection, allocations). Take what fits, reinterpret the rest,
drop what needs inheritance.

Scope: these principles only. Where a use case, port or file goes inside a bounded context
(one use case per file, `service.go`, file naming) is owned by `ddd-go-idiomatic`; comment
policy and general idioms by `senior-go-idiomatic`.

## Principles (Go rule → smell it kills)

| Principle | Rule in Go | Smell it kills |
|---|---|---|
| Law of Demeter | Do not reach through objects you do not own | *train wreck* `a.B().C().D()` over someone else's graph |
| Tell, Don't Ask | Ask the owner to act; it guards its invariant | read state → decide outside → write state |
| Command–Query Separation | A command mutates and returns `error`; a query is pure | a "getter" that mutates on the fly |
| Named types | Domain types over primitives; enums with `iota` | *stringly-typed* code, *boolean blindness*, swapped arguments |
| Intent-revealing methods | Business verbs, not CRUD | `SetStatus` / `SetBalance` that break invariants |
| Illegal states unrepresentable | Parse, don't validate; constructors `(T, error)`; unexported fields | the same value validated in ten places |
| Explicit dependencies | Constructor injection; wiring in `main` | mutable package `var`, singleton, service locator |
| DRY with nuance | Do not duplicate **knowledge** (not code that looks alike) | `common/`/`shared/` coupling unrelated domains |
| Selective SOLID | ISP and DIP are native; OCP by composition | simulated inheritance, interface per class |

The most important trap, so as not to betray Rule #0: the Law of Demeter is **not** "one dot
per line". Chaining over values you own or over composable stdlib
(`strings.ToLower(strings.TrimSpace(s))`, `io.Copy`, `errors.Is`, functional options) is
idiomatic and does not violate it. The real rule: do not depend on the **structure** of what
your collaborators return.

A second one: a defined type such as `type Email string` names a concept and stops argument
swaps, but it does **not** guarantee validity (an untyped constant like `"garbage"` converts
implicitly, and any package can write `Email(s)`). When the value has an invariant, use a
struct with an unexported field and a validating constructor.

## Smell checklist

Review a change against this:

- [ ] A train wreck `x.A().B().C()` over objects you do not own? → Tell-Don't-Ask, or pass only the data.
- [ ] A method that mutates **and** silently returns data? → split command from query.
- [ ] Raw `int64`/`string`/`bool` parameters where there is a concept? → named type, enum or functional option.
- [ ] A `SetX` that can leave the object invalid? → an intent-revealing method that keeps the invariant.
- [ ] A value with an invariant modeled as `type X string`? → struct with unexported field + `NewX(...) (X, error)`.
- [ ] The same value validated in several places? → parse it into a valid type **once**, at the boundary.
- [ ] A mutable package `var`, a singleton or a service locator? → inject the dependency through the constructor.
- [ ] `time.Now()` or global `rand` inside logic? → inject a clock / `*rand.Rand`.
- [ ] A `common/`/`shared/`/`util/` package deduplicating unrelated domains? → split them; they couple by convenience.
- [ ] Simulated inheritance (`BaseX`), an abstract factory or one interface per struct? → composition + small consumer-defined interfaces.

## Reference files

Read the one that matches the question; do not load them all.

- [coupling-behavior.md](references/coupling-behavior.md): train wrecks and what does *not* count as one, Tell-Don't-Ask, Command–Query Separation and its idiomatic exceptions (comma-ok, `Pop`).
- [domain-naming.md](references/domain-naming.md): named types over primitives, stringly-typed state, boolean blindness, intent-revealing method names, package names as vocabulary.
- [illegal-states.md](references/illegal-states.md): parse-don't-validate, `(T, error)` constructors, `iota` enums and the invalid zero value, unit types, functional options, sealed interfaces.
- [dependencies.md](references/dependencies.md): constructor injection, no globals/singletons/service locator, acceptable package-level state, clock and `math/rand/v2` as dependencies, wiring in `main`.
- [dry-solid-go.md](references/dry-solid-go.md): when deduplication couples, and which SOLID letters apply natively, get reinterpreted or drop out in Go.
