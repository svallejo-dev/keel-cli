# Domain and Semantic Naming

How Go's type system turns domain names into compiler guarantees. There is no conflict with
Go idioms here: named types and enums *are* the idiomatic form.

## 1. The two layers of naming

Naming in Go has two independent layers:

- **Lexical** (how an identifier is written): MixedCaps, no stutter (`http.Server`, not
  `http.HTTPServer`), no `Get` prefix on getters, short consistent receivers. That is
  general Go style, covered by the `senior-go-idiomatic` skill.
- **Semantic / domain** (what an identifier represents): what a `UserID` is, why `Status` is
  not a `string`, why a method is called `Withdraw` and not `SetBalance`. **This file covers
  this layer.**

The rule that joins them: the compiler should understand your domain as well as the reader
does. A good name resting on a primitive type wastes half the work.

## 2. Named types over primitives

An `int64` that identifies an account and another `int64` that is money are, to the compiler,
the same type: interchangeable without warning. A **named type** separates them.

```go
type (
	UserID    int64
	AccountID int64
	ProductID int64
	Cents     int64
)
```

The signature goes from opaque to self-documenting, and the compiler catches the
*parameter swap* at build time instead of in production.

```go
// BAD: three int64 in a row; nothing stops crossing from/to or passing an ID as an amount.
func Transfer(from, to int64, amount int64) error { /* ... */ }

Transfer(toAcct, fromAcct, balance) // compiles happily with from/to swapped

// GOOD: each argument has its domain type; the wrong order does not compile.
func Transfer(from, to AccountID, amount Cents) error { /* ... */ }
```

Named types with the same underlying type **are not interchangeable**; you convert
explicitly (`AccountID(x)`). That is not friction, it is the feature: the same wall that
stops the bug.

```go
var p ProductID = 42
Lookup(p) // Lookup(c CustomerID): does not compile
// cannot use p (variable of int64 type ProductID) as CustomerID value in argument to Lookup
```

| Primitive signature | With named types | What the compiler gains |
| --- | --- | --- |
| `Transfer(from, to int64, amt int64)` | `Transfer(from, to AccountID, amt Cents)` | Catches argument swaps and ID↔money mixups |
| `Charge(cust int64, amt int64)` | `Charge(cust CustomerID, amt Cents)` | Catches customer↔amount swaps |
| `Send(to string)` | `Send(to Email)`, `Email` a struct VO | Only addresses that passed `NewEmail` reach `Send` |

### Named type vs value object

A defined type names a concept; it does **not** validate it. With `type Email string`:

```go
Send("garbage")    // compiles: an untyped string constant converts implicitly to Email
Send(Email(input)) // compiles in any package: explicit conversion skips any constructor
```

That is fine for `UserID` or `Cents`, where every underlying value is acceptable and the goal
is to stop swaps. When the value has an invariant (a well-formed email, a currency, a
percentage), make it a struct with an unexported field and a constructor
`NewEmail(raw string) (Email, error)`; see [illegal-states.md](illegal-states.md) §2.

Rule: if a primitive crosses a function boundary and represents a domain concept (identity,
money, unit, address), give it a named type, and give it a validating constructor when it has
rules.

## 3. Kill stringly-typed state

**Stringly-typed** code models with `string` (or a raw `int`) something that has a closed set
of values: states, kinds, currencies, units. Comparisons become fragile (literals scattered
around, typos the compiler cannot see) and the valid set lives nowhere in the type.

```go
// BAD: status as a string. "actve" compiles; the valid set is not in the type.
type Order struct{ status string }

func (o *Order) IsOpen() bool {
	return o.status == "pending" || o.status == "paid"
}
```

The cure is a named type with an **`iota` enum**:

```go
// Status is the closed set of order states.
type Status uint8

// Order states; the zero value is deliberately not a real state (see illegal-states.md §3).
const (
	StatusUnknown Status = iota
	StatusPending
	StatusPaid
	StatusShipped
	StatusCancelled
)

func (s Status) String() string {
	switch s {
	case StatusPending:
		return "pending"
	case StatusPaid:
		return "paid"
	case StatusShipped:
		return "shipped"
	case StatusCancelled:
		return "cancelled"
	default:
		return fmt.Sprintf("Status(%d)", uint8(s))
	}
}
```

Now `o.status == StatusPaid` is type-safe, a typo does not compile, and `String()` gives
readable logs. The same applies to `Currency`, `Unit`, `Role`: closed set ⇒ named type, never
a loose `string`.

## 4. Kill boolean blindness

**Boolean blindness** loses information at the call site: `true`/`false` do not say what they
switch on, and two adjacent bools swap silently.

```go
// BAD: what is true? what is false? what if you swap them?
func Charge(amount Cents, recurring, refundable bool) error { /* ... */ }

Charge(500, true, false) // unreadable, and Charge(500, false, true) compiles too
```

Three idiomatic ways out, from lightest to heaviest:

**a) Named bool types**: readable call site and type mismatch on swap.

```go
type (
	Recurring  bool
	Refundable bool
)

func Charge(amount Cents, recurring Recurring, refundable Refundable) error { /* ... */ }

Charge(500, Recurring(true), Refundable(false))
```

**b) An enum** when the flag is really a mode that has, or will have, more than two values
(`BillingMode` instead of `recurring bool`); it avoids the second bool that always arrives.

**c) Functional options** when there are several optional flags; the default is the zero
value and each option has a name:

```go
// ChargeOption tweaks one charge setting; the zero config is a one-off, non-refundable charge.
type ChargeOption func(*chargeConfig)

// WithRecurring bills the amount every period.
func WithRecurring() ChargeOption { return func(c *chargeConfig) { c.recurring = true } }

// WithRefundable allows a later refund.
func WithRefundable() ChargeOption { return func(c *chargeConfig) { c.refundable = true } }

func Charge(amount Cents, opts ...ChargeOption) error { /* ... */ }

Charge(500, WithRecurring()) // reads on its own; refundable keeps its default
```

Rule: a bare `bool` in a public signature is a smell. Name it, make it an enum, or make it an
option.

## 5. Methods named by intent

A method name should reveal the **business operation**, not the mechanical mutation of a
field. `SetStatus(StatusCancelled)` describes how memory changes; `Cancel()` describes what
happened in the domain, and owns the rule.

```go
// BAD: CRUD setters; the caller assembles the transition and can break the rules.
func (a *Account) SetBalance(c Cents) { a.balance = c }
func (a *Account) SetStatus(s Status) { a.status = s }

a.SetBalance(a.Balance() - amount) // who checked the balance? who guards the invariant?
```

```go
// Withdraw fails with ErrInsufficientFunds instead of going negative.
func (a *Account) Withdraw(amount Cents) error {
	if amount > a.balance {
		return ErrInsufficientFunds
	}
	a.balance -= amount
	return nil
}

// Cancel moves the account to its terminal state.
func (a *Account) Cancel() { a.status = StatusCancelled }
```

Typical intent verbs: `Withdraw`, `Deposit`, `Confirm`, `Cancel`, `Promote`, `Approve`,
`Ship`. Each one is a single place where validation lives; `Set*` spreads it across every
call site. Getters still exist (`Balance()`, `Status()`), but mutators are named after the
business operation, not the field. This is Tell-Don't-Ask from the method's side
([coupling-behavior.md](coupling-behavior.md) §3).

## 6. The package name is vocabulary

A package's name prefixes every symbol it exports (`order.Status`, `billing.Charge`): it is
part of the name and therefore part of the domain vocabulary. A package should **name a
concept**, not a miscellany.

| BAD | Why | GOOD |
| --- | --- | --- |
| `util`, `common` | Name nothing; attract unrelated code | `order`, `billing` |
| `helpers`, `shared` | A drawer; symbols gain no context | `pricing`, `auth` |
| `models`, `types` | Group by mechanics, not by concept | `account`, `invoice` |

If a package is called `util`, the right question is not how to rename it but which concept
each symbol in it belongs to. Package and symbol read together: `order.New`, not
`models.NewOrderModel`.

## 7. File names

How to name files and where use cases go inside a bounded context (one use case per file,
no `service.go`) is owned by the `ddd-go-idiomatic` skill.

## 8. Named types as the foundation of legal states

Named types and enums do more than document: they are the first tool for **making invalid
states impossible**. A `Status uint8` with `iota` bounds the representable states; an `Email`
struct built only by a validating constructor guarantees every value passed that gate. Taking
this all the way (validating constructors, sum types, unexported fields) is the topic of
[illegal-states.md](illegal-states.md). Semantic naming is the entry point: without named
types there is nowhere to hang the invariant.

## Checklist

- [ ] Identity, money, units and addresses are named types, not raw `int64`/`string`.
- [ ] Values with rules are struct VOs with a constructor; a plain `type X string` only names, it does not validate.
- [ ] Named types are converted explicitly at the boundary where they are parsed or built.
- [ ] Swapping IDs or ID↔money is a compile error, not a runtime bug.
- [ ] Closed sets (states, kinds, currencies) are `iota` enums with `String()`; no magic strings.
- [ ] No bare `bool` in public signatures: named bool type, enum or functional option.
- [ ] Mutators name the business operation (`Withdraw`, `Cancel`), not the field (`SetBalance`).
- [ ] Each intent method is the single owner of its validation.
- [ ] Packages name a concept (`order`, `billing`); no `util`/`common`/`helpers`/`models`.
