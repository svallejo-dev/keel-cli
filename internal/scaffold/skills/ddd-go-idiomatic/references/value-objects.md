# Value Objects

Value objects are where DDD and Go agree almost completely: Go already has first-class value
semantics. There is nothing to import from Java (getters/setters, `equals`/`hashCode`,
`final` everywhere); use what the language gives you. When a DDD habit and a Go idiom
disagree, the Go idiom wins.

## 1. Definition

A **value object** (VO) is a small, **immutable** type **without identity**, defined entirely
by its attributes. Two VOs with the same attributes are interchangeable: there is no "this
email" versus "that email", only the value.

The VO is **the smallest unit where that unit's rules live**. "An email has the shape
`local@domain`" does not belong to the handler, the use case or the entity that holds it; it
belongs to `Email`. Concentrating validation in the VO is what lets the rest of the system
assume correct data without re-validating.

Contrast with an entity (see [entities.md](entities.md)): an entity has an ID and a life
cycle, and two entities with equal attributes are still different. A VO is pure value.

## 2. Two shapes in Go, and why the struct wins

| | `type Email struct{ v string }` | `type Email string` (defined type) |
|---|---|---|
| Cost | One field, same as the string | The string itself |
| Reading the value | Needs `String()` or an accessor | Used almost like a `string` |
| Can code skip the constructor? | **No**, outside the package only the zero value `Email{}` can be built | **Yes**, from any package |
| Comparable with `==` | Yes, if every field is | Yes |
| Use for | VOs with an invariant | Named types with no rule to protect |

`type Email string` is a **defined type**, not an alias (an alias is `type Email = string`
and creates no new type at all). A defined type is still not a safe value object, because
two things bypass any `NewEmail` constructor:

```go
type Email string

func Send(to Email) error { /* ... */ }

Send("garbage")                 // compiles: an untyped string constant is assignable to Email
e := identity.Email(userInput)  // compiles in any package: explicit conversion
```

With the struct, `v` is unexported, so no other package can put an arbitrary string inside
an `Email`; `NewEmail` is the only way in, and the zero value `Email{}` is the only other
value anyone can produce.

**Rule:** a VO with an invariant (email, money, coordinates) is a struct with unexported
fields plus a validating constructor. Keep defined types (`type Meters float64`,
`type UserID string`) for named types where every underlying value is acceptable; their job
is to stop parameter swaps, not to guarantee validity.

## 3. Validating constructor

Validation lives in a constructor returning `(T, error)`. This is not the GoF Factory Method
(no hierarchy, no polymorphism); it is an ordinary Go constructor.

```go
// NewEmail trims and lower-cases raw and fails with ErrInvalidEmail if it is not an address.
func NewEmail(raw string) (Email, error) {
	addr := strings.ToLower(strings.TrimSpace(raw))
	if addr == "" {
		return Email{}, fmt.Errorf("%w: empty", ErrInvalidEmail)
	}
	if _, err := mail.ParseAddress(addr); err != nil {
		return Email{}, fmt.Errorf("%w: %q", ErrInvalidEmail, raw)
	}
	return Email{v: addr}, nil
}
```

The governing idea is **make invalid states unrepresentable**: if you hold an `Email`, it is
valid by construction. The zero value `Email{}` is the only invalid value, and the
constructor never returns it together with a `nil` error. Code that receives an `Email`
stops defending itself; the type is the guarantee.

The pattern scales to multi-field VOs. Operations that can fail return an error; operations
never mutate the receiver.

```go
// Money is an amount in the smallest unit of a currency.
type Money struct {
	cents    int64
	currency string
}

// NewMoney rejects an empty currency; negative amounts are allowed (refunds).
func NewMoney(cents int64, currency string) (Money, error) {
	if currency == "" {
		return Money{}, &ValidationError{Field: "currency", Reason: "empty"}
	}
	return Money{cents: cents, currency: currency}, nil
}

// Add fails with ErrCurrencyMismatch when the currencies differ.
func (m Money) Add(other Money) (Money, error) {
	if m.currency != other.currency {
		return Money{}, fmt.Errorf("%w: %s vs %s", ErrCurrencyMismatch, m.currency, other.currency)
	}
	return Money{cents: m.cents + other.cents, currency: m.currency}, nil
}
```

Adding two currencies is an invalid state, so the type rejects it exactly where it would
happen.

## 4. Immutability by convention

Go has no `readonly` for structs. Immutability rests on three decisions, not on the compiler:

1. **Unexported fields**: nobody outside the package can reassign them.
2. **Value receivers**: the method works on a copy.
3. **Methods return a new VO**: every "transformation" produces another value.

```go
// BAD: a pointer receiver that mutates in place breaks value semantics.
func (m *Money) Scale(n int64) { m.cents *= n }

// GOOD: value receiver, new value.
func (m Money) Times(n int64) Money { return Money{cents: m.cents * n, currency: m.currency} }
```

A VO with a mutating pointer receiver is a smell: you are treating a value as if it had
identity. If it really needs to change over time, it is probably an entity.

## 5. Equality

A VO is equal to another when their values are equal. When the type is `comparable` (no
slices, maps or funcs inside), `==` does it for free; no hand-written `Equal`/`hashCode`.

```go
a, _ := NewEmail("dev@example.com")
b, _ := NewEmail("DEV@example.com")
_ = a == b // true: both normalize to the same value
```

A slice or map field makes the struct non-comparable and `==` stops compiling
(`invalid operation: a == b (struct containing []Email cannot be compared)`). Provide an
explicit `Equal` instead:

```go
// Recipients is an ordered list of addresses.
type Recipients struct {
	to []Email
}

// Equal compares element by element, since == does not compile for slices.
func (r Recipients) Equal(other Recipients) bool {
	return slices.Equal(r.to, other.to)
}
```

Design VOs to be `comparable` when you can (scalar fields, fixed-size arrays such as
`[3]Email` instead of slices). A comparable VO also works as a map key without ceremony:

```go
balances := map[Money]int{}
usd, _ := NewMoney(100, "USD")
balances[usd]++
```

Accept a manual `Equal` only when the data really is a collection of arbitrary size.

## 6. Typed and sentinel validation errors

Constructors fail with errors the edge can map to `400` without inspecting strings.

**Sentinel**, for a single stable failure mode, checked with `errors.Is`:

```go
var ErrInvalidEmail = errors.New("identity: invalid email")
```

**Typed `ValidationError`**, when the field and reason should reach the client, checked with
`errors.As`:

```go
// ValidationError names the field that failed and why.
type ValidationError struct {
	Field  string
	Reason string
}

// Error reads "identity: <field>: <reason>".
func (e *ValidationError) Error() string {
	return fmt.Sprintf("identity: %s: %s", e.Field, e.Reason)
}
```

The domain only emits errors; the **transport** (the edge) maps them to status codes, as
described in [application-layer.md](application-layer.md) §5:

```go
var ve *identity.ValidationError
switch {
case errors.Is(err, identity.ErrInvalidEmail):
	http.Error(w, "invalid email", http.StatusBadRequest)
case errors.As(err, &ve):
	http.Error(w, ve.Field+": "+ve.Reason, http.StatusBadRequest)
default:
	http.Error(w, "internal error", http.StatusInternalServerError)
}
```

Always wrap with `%w` so `errors.Is`/`errors.As` keep working up the chain.

## 7. Replace, never update

A VO is never updated in place; it is replaced. The entity composes VOs and, when it
changes, swaps the whole VO instead of reaching inside it:

```go
type User struct {
	id    UserID
	email Email
}

// ChangeEmail swaps the whole value object.
func (u *User) ChangeEmail(e Email) { u.email = e }
```

Note the receiver asymmetry: the entity uses a **pointer receiver** (it has identity and
mutates), the VO a **value receiver**. That asymmetry is the entity/VO distinction written in
code. How entities and VOs group under a transactional root is the aggregate's job
([aggregates.md](aggregates.md)).

### The persistence edge

The unexported field has a cost: a repository in another package cannot rebuild a VO with a
struct literal. Reconstitution goes through the constructor, exactly like user input:

```go
// in pgstore, when reading a row
email, err := identity.NewEmail(r.Email)
if err != nil {
	return nil, fmt.Errorf("pgstore: corrupt email for user %s: %w", r.ID, err)
}
```

That is a feature: a stored value that violates the invariant (bad migration, out-of-band
write) is caught on load instead of spreading through the domain. If validation on read is a
measured bottleneck, add an unexported reconstruction helper inside the context, never an
exported one that reopens the bypass.

## Checklist

- [ ] A VO with an invariant is a struct with **unexported** fields, not a defined type like `type Email string`.
- [ ] Defined types (`type T string`) only where every underlying value is valid; they stop swaps, not bad values.
- [ ] All validation lives in `NewX(...) (X, error)`; holding an `X` means it is valid.
- [ ] Immutability: unexported fields + value receivers + methods that return a new VO.
- [ ] No VO method uses a pointer receiver to mutate in place.
- [ ] VOs are `comparable` when possible (`==` for free, usable as map keys).
- [ ] A VO with a slice/map field has an explicit `Equal` (`slices.Equal`).
- [ ] Construction errors are sentinels or typed, wrapped with `%w`, mapped to 400 at the transport edge.
- [ ] The domain knows nothing about HTTP; status mapping lives in the transport.
- [ ] Entities replace whole VOs; stores rebuild VOs through their constructors.
