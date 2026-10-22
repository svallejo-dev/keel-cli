# Aggregates

## Contents

1. Definition
2. In Go, package privacy is the boundary (`Order` with `AddLine`, `RemoveLine`, `Total`)
3. One aggregate per transaction; reference others by ID
4. Invariants (domain) vs authorization (use case `AddOrderLine`)
5. Do not expose mutable internals
6. Avoid premature aggregates

An aggregate is not a class hierarchy with getters/setters or an abstract
`AggregateRoot<T>`. In Go it is a struct whose boundary **the compiler enforces**. When a DDD
habit and a Go idiom disagree, the Go idiom wins.

This file covers the **root** and the **consistency boundary**. Entity mechanics (identity,
equality) live in [entities.md](entities.md), VOs in [value-objects.md](value-objects.md),
transactions and repositories in [application-layer.md](application-layer.md).

## 1. Definition

An **aggregate** is a cluster of entities and value objects treated as one unit for data
changes. Two properties define it:

- A **consistency boundary**: the data that must be consistent **synchronously** after every operation.
- An **aggregate root**: the entity that is the **only valid entry point** to the cluster.
  Every create/update/delete of the internals goes through it.

You never reference, load or mutate an internal entity directly. To change an order line,
ask the order, not the line; the root guarantees no mutation leaves the cluster violating an
invariant.

## 2. In Go, package privacy is the boundary

In most OO languages the boundary depends on discipline: nothing stops a service from calling
`order.getLines().add(...)`. In Go it is **structural**: internals live in **unexported**
fields of the root, and package visibility stops any outside code from building or mutating
them. "Please don't touch this" becomes "the compiler won't let you".

`Money` is the value object from [value-objects.md](value-objects.md) (`NewMoney`, `Add`,
`Times`), plus `Cents()` and `Currency()` accessors.

**internal/order/order.go**

```go
// Package order is the ordering bounded context.
package order

import (
	"errors"
	"fmt"
	"slices"
)

// Errors returned when an operation would break an Order invariant.
var (
	ErrIncompleteOrder = errors.New("order: id, customer and currency are required")
	ErrDuplicateLine   = errors.New("order: SKU already in the order")
	ErrLineNotFound    = errors.New("order: SKU not in the order")
	ErrNonPositive     = errors.New("order: quantity must be > 0")
)

// OrderID identifies an Order.
type OrderID string

// CustomerID points at a customer aggregate by identity, never by pointer.
type CustomerID string

// SKU identifies a product.
type SKU string

// Quantity counts units of one SKU.
type Quantity int

type orderLine struct {
	sku      SKU
	quantity Quantity
	unit     Money
}

func (l orderLine) subtotal() Money { return l.unit.Times(int64(l.quantity)) }

// Order is the aggregate root for an order and its lines; every line shares its currency.
type Order struct {
	id         OrderID
	customerID CustomerID
	currency   string
	lines      []orderLine
}

// NewOrder starts an empty order priced in currency.
func NewOrder(id OrderID, customer CustomerID, currency string) (*Order, error) {
	if id == "" || customer == "" || currency == "" {
		return nil, ErrIncompleteOrder
	}
	return &Order{id: id, customerID: customer, currency: currency}, nil
}

// AddLine rejects non-positive quantities, duplicate SKUs and prices in another currency.
func (o *Order) AddLine(sku SKU, qty Quantity, unit Money) error {
	if qty <= 0 {
		return ErrNonPositive
	}
	if unit.Currency() != o.currency {
		return fmt.Errorf("%w: %s vs %s", ErrCurrencyMismatch, unit.Currency(), o.currency)
	}
	if slices.ContainsFunc(o.lines, func(l orderLine) bool { return l.sku == sku }) {
		return fmt.Errorf("%w: %s", ErrDuplicateLine, sku)
	}
	o.lines = append(o.lines, orderLine{sku: sku, quantity: qty, unit: unit})
	return nil
}

// RemoveLine fails with ErrLineNotFound when sku is not in the order.
func (o *Order) RemoveLine(sku SKU) error {
	i := slices.IndexFunc(o.lines, func(l orderLine) bool { return l.sku == sku })
	if i < 0 {
		return fmt.Errorf("%w: %s", ErrLineNotFound, sku)
	}
	o.lines = slices.Delete(o.lines, i, i+1)
	return nil
}

// Total is derived from the lines on every call, so it can never drift from them.
func (o *Order) Total() (Money, error) {
	total, err := NewMoney(0, o.currency)
	if err != nil {
		return Money{}, err
	}
	for _, l := range o.lines {
		if total, err = total.Add(l.subtotal()); err != nil {
			return Money{}, err
		}
	}
	return total, nil
}
```

`orderLine` is unexported, so outside the package it cannot be built; `lines` is
unexported, so the package is the boundary. There is no `AggregateRoot` base type or
marker interface: `Order` is an ordinary struct that owns its internals and exposes them
only through rules.

`Total` still returns the error from `Money.Add`. `AddLine` makes a currency mismatch
unreachable today, but the compiler cannot know that, and returning the error costs less
than a silent wrong total if a future change breaks the invariant.

## 3. One aggregate per transaction; reference others by ID

The consistency boundary is the **transactional boundary**: strong, synchronous consistency
inside; eventual consistency outside. Hence **one transaction modifies exactly one
aggregate**. If an operation must change two aggregates atomically, the boundary is usually
drawn wrong, or the second change should be a later event.

Corollary: reference **other** aggregates **by ID**, never by pointer.

```go
// BAD: a pointer drags the other aggregate's graph in and blurs the boundary.
type Order struct {
	customer *Customer
}

// GOOD: only the identifier; Order's boundary stays sharp.
type Order struct {
	customerID CustomerID
}
```

Why it matters:

- A pointer invites `order.customer.ChangeAddress(...)`, mutating `Customer` inside an `Order` transaction.
- Loading `Order` would load `Customer`, then its orders, then the whole graph.
- The ID makes it explicit that `Customer` is **another** consistency unit, loaded by its own root in its own operation.

| Inside the boundary | Outside the boundary |
|---|---|
| Strong, synchronous consistency | Eventual consistency |
| Direct reference (internal field) | Reference by ID |
| One transaction | Coordination via events or another use case |

## 4. Invariants (domain) vs authorization (use case)

The aggregate enforces **business invariants** ("no two lines with the same SKU"). It does
**not** decide **who** may run the operation. Authorization is an application concern and
lives in the **use case**: the aggregate knows nothing of sessions, roles or tenants, and
putting them inside couples it to infrastructure and makes it hard to test in isolation.

The use case is its own type in `add_order_line.go`. It decides the *who* through a narrow
port declared in the same file (only this use case needs it), delegates the *what* to the
root, and saves a single aggregate.

**internal/order/add_order_line.go**

```go
package order

import (
	"context"
	"errors"
	"fmt"
)

// ErrForbidden means the actor may not edit the order.
var ErrForbidden = errors.New("order: forbidden")

// Permissions answers authorization questions; only AddOrderLine needs it.
type Permissions interface {
	CanEditOrder(ctx context.Context, actorID string, id OrderID) (bool, error)
}

// AddOrderLineInput carries raw values from the transport.
type AddOrderLineInput struct {
	ActorID   string
	OrderID   string
	SKU       string
	Quantity  int
	UnitCents int64
	Currency  string
}

// AddOrderLineOutput reports the new total in the smallest currency unit.
type AddOrderLineOutput struct {
	TotalCents int64
	Currency   string
}

// AddOrderLine adds a line to an order the actor is allowed to edit.
type AddOrderLine struct {
	orders OrderRepository
	perms  Permissions
}

// NewAddOrderLine wires the shared repository and the authorization port.
func NewAddOrderLine(orders OrderRepository, perms Permissions) *AddOrderLine {
	return &AddOrderLine{orders: orders, perms: perms}
}

// Execute fails with ErrForbidden before touching the order when the actor lacks permission.
func (uc *AddOrderLine) Execute(ctx context.Context, in AddOrderLineInput) (AddOrderLineOutput, error) {
	id := OrderID(in.OrderID)
	ok, err := uc.perms.CanEditOrder(ctx, in.ActorID, id)
	if err != nil {
		return AddOrderLineOutput{}, fmt.Errorf("add order line: check permission: %w", err)
	}
	if !ok {
		return AddOrderLineOutput{}, ErrForbidden
	}

	unit, err := NewMoney(in.UnitCents, in.Currency)
	if err != nil {
		return AddOrderLineOutput{}, err
	}
	o, err := uc.orders.FindByID(ctx, id)
	if err != nil {
		return AddOrderLineOutput{}, err
	}
	if err := o.AddLine(SKU(in.SKU), Quantity(in.Quantity), unit); err != nil {
		return AddOrderLineOutput{}, err
	}
	if err := uc.orders.Save(ctx, o); err != nil {
		return AddOrderLineOutput{}, fmt.Errorf("add order line: save: %w", err)
	}

	total, err := o.Total()
	if err != nil {
		return AddOrderLineOutput{}, err
	}
	return AddOrderLineOutput{TotalCents: total.Cents(), Currency: total.Currency()}, nil
}
```

`Order.AddLine` takes no actor and checks no permission. If you find yourself passing a
user ID into a domain method to decide permissions, the logic is in the wrong layer.

## 5. Do not expose mutable internals

Returning the internal slice lets the caller alias the backing array and mutate the
aggregate **around** the root's invariants:

```go
// BAD: the caller gets the internal slice and can mutate it without AddLine.
func (o *Order) Lines() []orderLine { return o.lines }
```

`o.Lines()[0].quantity = -5` would then corrupt the total without a single error (inside the
package; outside it, an exported method returning an unexported type is useless anyway and
linters flag it). Return a **copy** or a **read-only view**:

```go
// LineView is a read-only projection of one order line.
type LineView struct {
	SKU      SKU
	Quantity Quantity
	Unit     Money
}

// Lines returns copies; changing them does not affect the order.
func (o *Order) Lines() []LineView {
	out := make([]LineView, len(o.lines))
	for i, l := range o.lines {
		out[i] = LineView{SKU: l.sku, Quantity: l.quantity, Unit: l.unit}
	}
	return out
}
```

Copying a small slice of VOs is cheap; do not hand out the interior to save it. For
sequential read-only use, an `iter.Seq[LineView]` (Go 1.23+) is an idiomatic alternative that
also keeps the backing array private.

## 6. Avoid premature aggregates

In small services most "aggregates" are **a single entity** with no sub-entities: there is no
cluster to protect, so no root to invent. A ceremonial root adds indirection (`AddX`/`RemoveX`,
views, copies) without protecting anything.

You **do** need an aggregate with a root when:

- A rule must hold across **several** entities at once ("lines total ≤ credit limit", "no duplicate SKU").
- Those entities **only** make sense inside the parent and are created/deleted with it.
- You want to load and save them as **one** transactional unit.

Otherwise model a flat entity. Promoting it later is mechanical in Go (make the fields
unexported, move mutations into methods). Related anti-patterns are in
[antipatterns.md](antipatterns.md).

## Checklist

- [ ] The root is the **only** entry point: sub-entities live in unexported fields.
- [ ] Every mutation of the interior goes through a root method that keeps the invariant.
- [ ] Derived values (`Total()`) are **computed**, not stored in a mutable field.
- [ ] Other aggregates are referenced **by ID** (`customerID CustomerID`), never by pointer.
- [ ] One transaction modifies **one** aggregate; the rest is eventual consistency.
- [ ] **Authorization** lives in the use case (`add_order_line.go` → `AddOrderLine`); the domain enforces business invariants only.
- [ ] Read methods return a **copy or view**, never the internal slice/map.
- [ ] No root without a real transactional invariant to justify it.
