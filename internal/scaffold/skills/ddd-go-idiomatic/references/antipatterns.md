# Anti-patterns: Tactical DDD That Breaks in Go

## Contents

1. Folders per layer → package per context
2. One interface per class → interface at the consumer
3. Getters/setters → intent-revealing methods
4. Anemic model → rich model
5. `mapper/` + reflection → hand translation
6. GoF Builder / Abstract Factory → idiomatic constructor
7. Repository leaking driver errors → domain error at the border
8. Pointer between aggregates → reference by ID
9. Generic `Repository[T]` → concrete repositories
10. Grab-bag packages → concept names
11. `BaseEntity` inheritance → purposeful composition
12. Mutable value object → immutable, create-new
13. Grab-bag file/type (`service.go`) → one file and one type per use case
- Summary table and checklist

Most "tactical patterns" that circulate as canonical are Java/Spring or C#/.NET conventions
transplanted. In Go they compile but fight the language: import cycles, interface pollution,
needless allocations, orphan types. DDD supplies the concepts (aggregate, bounded context,
invariant); Go dictates the implementation. When they disagree, Go wins. Each `BAD` below is
valid Go whose defect is design, unless stated otherwise.

## 1. Folders per layer → package per context

Top-level `domain/` + `application/` + `infrastructure/`, with `models/`, `repositories/`,
`services/` inside. In Go a package is a unit of design, not a namespace: splitting one
concept across layers produces the anemic model (`models.Order` belongs to nobody) and, as
soon as the domain needs a type from infrastructure, an **import cycle** the compiler
rejects. Fix: one package per bounded context ([bounded-contexts.md](bounded-contexts.md));
inside, one file per concept or use case.

```
# BAD: technical layers; Order is an orphan and domain ↔ infra drifts into a cycle
internal/domain/models/order.go
internal/domain/repositories/order_repo.go
internal/application/services/order_service.go
internal/infrastructure/order_pg.go

# GOOD: the concept, its port, its use cases and its infrastructure together
internal/order/order.go              Order + invariants
internal/order/order_repository.go   OrderRepository (port, declared by the consumer)
internal/order/place_order.go        PlaceOrder use case
internal/order/pgstore/              OrderRepository on PostgreSQL
```

## 2. One interface per class → interface at the consumer

A `port` plus `adapter` for every collaborator, even with a single implementation. Go
interfaces are satisfied implicitly and declared where they are used; an interface with one
implementation declared next to it is **interface pollution**: indirection (blocks
inlining), an allocation and a file jump for nothing. Fix: the implementation is an exported
concrete struct; the consumer declares the minimal interface it needs
([application-layer.md](application-layer.md) §3).

```go
// BAD: package smtp declares a port next to its ONLY implementation.
type EmailSender interface {
	Send(to, subject, body string) error
}

// GOOD: welcome/send_welcome.go — the use case declares what it calls.
// Mailer is the one method SendWelcome needs; *smtp.Client satisfies it implicitly.
type Mailer interface {
	Send(to, subject, body string) error
}

// SendWelcome mails the welcome message to a newly registered user.
type SendWelcome struct{ mail Mailer }

// NewSendWelcome takes any Mailer; tests pass a fake.
func NewSendWelcome(m Mailer) *SendWelcome { return &SendWelcome{mail: m} }
```

## 3. Getters/setters → intent-revealing methods

JavaBean `GetX()`/`SetX(v)`. Go style drops `Get` (the accessor for `email` is `Email()`),
and a generic setter leaves invariants unprotected (`SetActive(true)` admits impossible
states). Fix: accessors without `Get`; each setter becomes a method that names the transition
([entities.md](entities.md) §5).

```go
// BAD: JavaBean; the setter admits invalid states.
func (u *User) GetEmail() string  { return u.email }
func (u *User) SetEmail(e string) { u.email = e }
func (u *User) SetActive(b bool)  { u.active = b }

// GOOD: no Get; the parameter is a valid Email; transitions have names.
func (u *User) Email() Email           { return u.email }
func (u *User) ChangeEmail(e Email)    { u.email = e }
func (u *User) Deactivate(t time.Time) { u.active = false; u.deactivatedAt = t }
```

## 4. Anemic model → rich model

Data structs with exported fields and the logic in a fat `XxxService` that edits them from
outside. With exported fields nobody guarantees invariants (`o.Total = -1`) and the service
accumulates rules that belong to the type. Encapsulation in Go is per package, so unexported
fields stay coherent only if the behaviour lives with them.

```go
// BAD: public data + a service that edits someone else's insides.
type Order struct {
	Lines []string
	Total int
}

func (OrderService) AddLine(o *Order, sku string, price int) {
	o.Lines = append(o.Lines, sku)
	o.Total += price
}
```

GOOD: `Order` with unexported `lines` and an `AddLine(sku, qty, unit) error` method that
enforces the invariants; the total is derived, not stored ([aggregates.md](aggregates.md) §2).

## 5. `mapper/` + reflection → hand translation

A `mapper/` package or an AutoMapper-style library that copies structs by reflection. The
compiler cannot see reflection: renaming a field breaks the mapping at runtime, not in the
build, and every request pays for reflection. Fix: `XInput`/response structs next to the use
case or endpoint and explicit translation the compiler checks.

```go
// BAD: reflection-based automapper; the build does not catch a rename.
func Map(src, dst any) {
	sv, dv := reflect.ValueOf(src).Elem(), reflect.ValueOf(dst).Elem()
	for i := 0; i < sv.NumField(); i++ {
		if f := dv.FieldByName(sv.Type().Field(i).Name); f.IsValid() && f.CanSet() {
			f.Set(sv.Field(i))
		}
	}
}

// GOOD: the endpoint's response struct + explicit translation.
resp := registerUserResponse{ID: out.ID, Email: out.Email}
```

## 6. GoF Builder / Abstract Factory → idiomatic constructor

`OrderBuilder{}.WithID(..).WithCustomer(..).Build()` or abstract factories. The fluent
Builder exists where a language lacks multiple returns; Go has them, so `NewX(...) (X, error)`
validates and returns the error on the spot, while the Builder defers validation to `Build()`
and admits half-built objects. Fix: a constructor, or functional options for many optional
settings; never a mutable Builder.

```go
// BAD: GoF Builder; validation is deferred or lost.
func (b *OrderBuilder) WithID(id OrderID) *OrderBuilder { b.o.id = id; return b }
func (b *OrderBuilder) Build() *Order                   { return b.o }

// GOOD: a constructor that validates and returns (value, error).
func NewOrder(id OrderID, customer CustomerID, currency string) (*Order, error) {
	if id == "" || customer == "" || currency == "" {
		return nil, ErrIncompleteOrder
	}
	return &Order{id: id, customerID: customer, currency: currency}, nil
}
```

## 7. Repository leaking driver errors → domain error at the border

The repository returns `sql.ErrNoRows` or `*pgconn.PgError` as-is. That couples every caller
to the driver: the application layer ends up importing `database/sql`/`pgx` just to call
`errors.Is`. The repository border is exactly where an infrastructure error becomes a domain
error ([application-layer.md](application-layer.md) §3).

```go
// BAD: the driver error escapes and couples callers to database/sql.
func (s *Store) FindByID(ctx context.Context, id order.OrderID) (*order.Order, error) {
	var r orderRow
	err := s.db.QueryRowContext(ctx, query, id).Scan(&r.customerID, &r.currency)
	return r.toOrder(id), err // sql.ErrNoRows leaks out
}

// GOOD: translate at the border into the context's sentinel.
func (s *Store) FindByID(ctx context.Context, id order.OrderID) (*order.Order, error) {
	var r orderRow
	switch err := s.db.QueryRowContext(ctx, query, id).Scan(&r.customerID, &r.currency); {
	case errors.Is(err, sql.ErrNoRows):
		return nil, order.ErrOrderNotFound
	case err != nil:
		return nil, fmt.Errorf("pgstore: find order %s: %w", id, err)
	}
	return order.NewOrder(id, order.CustomerID(r.customerID), r.currency)
}
```

## 8. Pointer between aggregates → reference by ID

`Order` holds `customer *Customer`. The pointer crosses the transactional boundary: loading an
`Order` drags in the whole `Customer` graph, invites mutating another aggregate in the same
transaction and creates shared aliasing. Fix: store `CustomerID`; when you need the
customer's data, load it through its own repository ([aggregates.md](aggregates.md) §3).

```go
// BAD: a pointer to another aggregate blurs the boundary and drags the graph.
type Order struct {
	id       OrderID
	customer *Customer
}

// GOOD: reference by identity; each aggregate is its own boundary.
type Order struct {
	id         OrderID
	customerID CustomerID
}
```

## 9. Generic `Repository[T]` → concrete repositories

A generic `Repository[T]` with `Get`/`Save` "to avoid repetition". Aggregates do not share
operations: one is found by `FindOpenByCustomer`, another by `ExistsByEmail`.
`Repository[T].Get(id)` forces all of them down to the lowest common denominator and hides the
real queries. Generics pay off when logic is truly type-agnostic (containers, algorithms); a
repository is not. Fix: one concrete interface per aggregate.

```go
// BAD: an abstraction that hides the real queries.
type Repository[T any] interface {
	Get(ctx context.Context, id string) (T, error)
	Save(ctx context.Context, entity T) error
}

// GOOD: a concrete port that names the aggregate's operations.
type OrderRepository interface {
	FindByID(ctx context.Context, id OrderID) (*Order, error)
	FindOpenByCustomer(ctx context.Context, customer CustomerID) ([]*Order, error)
	Save(ctx context.Context, o *Order) error
}
```

## 10. Grab-bag packages (`domain`, `vo`, `dto`, `util`, `common`) → concept names

Generic packages cannot be described in a sentence that starts with their name: they are
drawers, not concepts. They force coupling (a `common` everybody imports) and ruin import
paths: `vo.Money` says nothing, `billing.Money` does. In Go the package name is part of
every identifier. Fix: name by bounded context or concept ([bounded-contexts.md](bounded-contexts.md) §2).

```
# BAD: drawers without identity      # GOOD: named concepts
internal/vo/money.go                  internal/billing/money.go
internal/common/util.go               internal/pricing/discount.go
```

## 11. `BaseEntity` inheritance → purposeful composition

A `BaseEntity` with `ID`/`CreatedAt`/`UpdatedAt`/`Touch()` embedded in every type. Go has no
inheritance: embedding is composition, with no override or dynamic dispatch. A universal
`BaseEntity` injects state and methods into types that do not need them (an immutable `Tag`
has no `UpdatedAt`), inflates their public API and, with exported fields, lets anyone rewrite
them. Fix: embed a small, unexported piece only in the types that use it, and pass time in.

```go
// BAD (the anti-pattern): a universal BaseEntity with exported, writable fields.
type BaseEntity struct {
	ID                   string
	CreatedAt, UpdatedAt time.Time
}

func (b *BaseEntity) Touch() { b.UpdatedAt = time.Now() }

type Tag struct {
	BaseEntity // a Tag needs neither UpdatedAt nor Touch
	Name       string
}

// GOOD: only audited types embed audit; the domain receives time instead of reading the clock.
type audit struct {
	createdAt, updatedAt time.Time
}

func (a *audit) touch(now time.Time) { a.updatedAt = now }

// Order is audited; a Tag simply would not embed audit.
type Order struct {
	id     OrderID
	status Status
	audit
}

// Cancel records when the order was cancelled.
func (o *Order) Cancel(now time.Time) {
	o.status = StatusCancelled
	o.touch(now)
}
```

## 12. Mutable value object → immutable, create-new

A `Money` with exported fields and `SetAmount`/`Add` that mutate the receiver through a
pointer. A value object is defined by its value: two `Money{10, "EUR"}` are interchangeable.
If it is mutable and shared by pointer, `Add` changes the amount under everyone's feet
(aliasing). Fix: unexported fields, no setters, value receivers, every operation returns a
new `Money` ([value-objects.md](value-objects.md) §3–4).

```go
// BAD: mutable VO with a setter; the shared Money changes for everyone.
func (m *Money) SetAmount(a int64) { m.Amount = a }
func (m *Money) Add(other Money)   { m.Amount += other.Amount }

// GOOD: immutable; value receiver; each operation creates a new value.
func (m Money) Add(other Money) (Money, error) {
	if m.currency != other.currency {
		return Money{}, fmt.Errorf("%w: %s vs %s", ErrCurrencyMismatch, m.currency, other.currency)
	}
	return Money{cents: m.cents + other.cents, currency: m.currency}, nil
}
```

## 13. Grab-bag file/type (`service.go`, `usecase.go`) → one file and one type per use case

A `service.go` (or `usecase.go`, `usecases.go`, `app.go`, `manager.go`) with a
`type Service struct` that accumulates `LogIn()`, `SignUp()`, `ResetPassword()`... It is the
Go version of Spring's `UserService`: the file name does not say what it does, the struct
sums the dependencies of every operation (each test builds five collaborators to exercise
one), and every new use case edits the same file, so every change collides with every other.
Having one package per context does not fix it; the drawer just moved one level down. Fix:
one use case = one type named after the action + one file with that name, with its
constructor, a single `Execute` and its DTOs ([application-layer.md](application-layer.md) §2).
The same applies to transport: one file per endpoint + `routes.go`, not a `handler.go` with
everything.

```
# BAD: internal/identity/              # GOOD: internal/identity/
service.go   LogIn, SignUp, Reset...    log_in.go           LogIn
handler.go   every endpoint             sign_up.go          SignUp
                                        reset_password.go   ResetPassword
                                        identityhttp/routes.go, log_in.go, sign_up.go...
```

```go
// BAD: service.go — two intents, the union of their dependencies.
type Service struct {
	users  UserRepository
	hasher Hasher
	tokens TokenIssuer
	mail   Mailer
}

func (s *Service) LogIn(ctx context.Context, in LogInInput) (LogInOutput, error)    { /* ... */ }
func (s *Service) SignUp(ctx context.Context, in SignUpInput) (SignUpOutput, error) { /* ... */ }

// GOOD: log_in.go — the file, the type and its dependencies say the same thing.
type LogIn struct {
	users  UserRepository
	hasher Hasher
	tokens TokenIssuer
}

func NewLogIn(users UserRepository, hasher Hasher, tokens TokenIssuer) *LogIn {
	return &LogIn{users: users, hasher: hasher, tokens: tokens}
}

func (uc *LogIn) Execute(ctx context.Context, in LogInInput) (LogInOutput, error) { /* ... */ }
```

`SignUp` lives in `sign_up.go` with its own dependencies (`Mailer` included); `LogIn` no
longer carries it.

## Summary: Java-ism → idiomatic Go

| Java/C#-ism | Why it breaks in Go | Idiomatic Go |
|---|---|---|
| `domain/` + `application/` + `infrastructure/` | Import cycles, orphan types | Package per context, file per concept/use case |
| A port + adapter per collaborator | Interface pollution, indirection | Interface at the consumer, with a 2nd implementation or a test seam |
| `GetX()` / `SetX()` | Not Go style; setters break invariants | `X()` without `Get`; intent-revealing methods |
| Anemic model + fat service | Invariants without an owner | Rich model: logic with the data |
| `mapper/` + reflection | No compile-time check; runtime cost | I/O structs + hand translation |
| Abstract Factory / GoF Builder | Defers validation; half-built objects | `NewX(...) (X, error)` / functional options |
| Repository leaks `sql.ErrNoRows`/`PgError` | Couples callers to the driver | Translate to a domain sentinel at the border |
| `Order.customer *Customer` | Crosses the transactional boundary | Reference by `CustomerID` |
| `Repository[T any]` | Hides the real queries; false DRY | Concrete repository per aggregate |
| `util`/`common`/`vo`/`dto` | Drawer without identity | Concept or context name |
| `BaseEntity` embedded everywhere | Simulated inheritance; inflated API | Small, unexported, purposeful composition |
| Mutable VO with setters | Aliasing; no longer "a value" | Immutable; value receiver; create-new |
| `service.go` / `usecase.go` + `type Service` | File without intent; summed deps; every change collides | One file and one type per use case (`log_in.go` → `LogIn`) |

## Checklist

- [ ] No top-level layer folders or grab-bag packages (`util`, `common`, `vo`, `dto`, `models`, `types`).
- [ ] Every interface has a second implementation or is a test seam, and the consumer declares it.
- [ ] Behaviour and accessors live on the type that owns the data (no `Get`, no blind setters, no external `Service`); VOs are immutable.
- [ ] No reflection-based mapping: I/O structs and hand translation next to the use case or endpoint.
- [ ] Aggregates are built with `NewX(...) (*X, error)`, not Builders or abstract factories.
- [ ] No driver error (`sql.ErrNoRows`, `pgconn.PgError`) leaves the repository; aggregates reference each other by ID.
- [ ] No `Repository[T any]`: one concrete interface per aggregate.
- [ ] No `BaseEntity`: only the types that need a piece of composition embed it.
- [ ] No `service.go`, `usecase.go`, `app.go`, `manager.go` or multi-endpoint `handler.go`: each use case is a type in a file with its name.
