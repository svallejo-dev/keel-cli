# Bounded Contexts in Go

DDD is a set of **heuristics about where behaviour lives**, not a folder structure or a
framework. When a tactical DDD rule clashes with idiomatic Go, Go wins: no top-level
`domain/application/infrastructure` layers, no `AbstractRepositoryFactory`, no central
`interfaces` package. A bounded context in Go is, almost always, **one package inside
`internal/`**.

Tactical details live in [value-objects.md](value-objects.md), [entities.md](entities.md),
[aggregates.md](aggregates.md), [application-layer.md](application-layer.md) and
[antipatterns.md](antipatterns.md). Examples use the module `example.com/shop`.

## 1. What a bounded context is and how it maps to Go

A bounded context is a boundary inside which a model and its language are coherent and
unambiguous. In Go that boundary is **a package** (or a small cluster of packages) under
`internal/`.

- `internal/` is a **privacy boundary the compiler enforces**: only the tree rooted at its parent can import it.
- The **context is the package** (`internal/identity`, `internal/billing`), named after the context.

The root type, its value objects, invariants and persistence port live together: they are
one concept, not three technical packages. The `identity` package of the
[worked example](worked-example.md) is the reference: `User` with unexported fields, built
only through `NewUser(id, email, name, now) (*User, error)`.

## 2. Ubiquitous language = the package's exported vocabulary

A context's ubiquitous language **is its exported API**; the package name is part of it
(`identity.NewUser`, `identity.Email`, `identity.RegisterUser`). The domain reads in the
exported identifiers, not in external documentation.

**internal/identity/email.go**

```go
package identity

import (
	"fmt"
	"net/mail"
	"strings"
)

// Email is a lower-cased address that is valid by construction.
type Email struct{ v string }

// NewEmail trims and lower-cases raw and fails with ErrInvalidEmail if it is not an address.
func NewEmail(raw string) (Email, error) {
	addr := strings.ToLower(strings.TrimSpace(raw))
	if _, err := mail.ParseAddress(addr); err != nil {
		return Email{}, fmt.Errorf("%w: %q", ErrInvalidEmail, raw)
	}
	return Email{v: addr}, nil
}

// String returns the normalized address.
func (e Email) String() string { return e.v }
```

**No grab-bag packages.** `util`, `common`, `models`, `types`, `helpers` have no ubiquitous
language: they represent no domain concept, they only group things. If you cannot name the
package with a business-specific noun, you have not found the context yet.

| Package | Why it fails |
|---|---|
| `models` | ORM thinking; the type is separated from its behaviour |
| `util` / `common` | A drawer without a boundary; couples contexts that should not touch |
| `types` | Anemic data layer; nobody owns the invariant |
| `service` | "Service" is not a concept; it usually becomes a god object |

The same rule applies one level down, to **files**: inside a context each file names a
concept (`user.go`, `email.go`) or a use case (`register_user.go`, `log_in.go`). The file
rules are in [application-layer.md](application-layer.md) §2.

## 3. Folder per CONTEXT, not per layer

The central mistake transplanted from Java/C# is organizing the tree by technical layer. Go
organizes by **concept**.

```
# BAD: top-level technical layers
internal/
├── domain/          ← which domain?
├── application/     ← a layer, not a concept
└── infrastructure/  ← the User model lives far from its logic
```

```
# GOOD: one package per context; inside, one FILE per concept or use case
internal/
├── identity/
│   ├── user.go              ← entity + package doc
│   ├── email.go             ← value object
│   ├── user_repository.go   ← shared persistence port (consumer-defined)
│   ├── register_user.go     ← use case RegisterUser
│   ├── log_in.go            ← use case LogIn
│   ├── change_email.go      ← use case ChangeEmail
│   └── pgstore/             ← infrastructure subpackage
└── billing/
    ├── customer.go
    ├── accounts.go          ← port towards identity (consumer-defined)
    └── charge_customer.go   ← use case ChargeCustomer
```

```
# BAD: grab-bag files inside the context
internal/identity/
├── models.go        ← which models?
├── service.go       ← LogIn(), SignUp(), ChangeEmail()... all together
└── handler.go       ← every endpoint
```

Inside the context, split **by file**, not by subpackage: the package is the unit of design
and files only organize reading. Nesting subpackages does **not** create hierarchy or grant
access to the "parent's" unexported identifiers; visibility in Go is per package, never per
folder.

Extract a subpackage only for a detail with its own name: infrastructure
(`internal/identity/pgstore/`) and transport (`internal/identity/identityhttp/`). The
interface lives in the consumer (`identity`) and the implementation in the subpackage: the
context does **not** import `pgstore`; it is the other way round.

**internal/identity/user_repository.go**

```go
package identity

import "context"

// UserRepository is the persistence port shared by the context's use cases.
type UserRepository interface {
	FindByID(ctx context.Context, id UserID) (*User, error)
	ExistsByEmail(ctx context.Context, email Email) (bool, error)
	Save(ctx context.Context, u *User) error
}
```

## 4. Integration between contexts

**Never import another context's internals.** Two packages compiling together does not mean
they should be coupled. Contexts talk through three channels:

1. **Consumer-defined published interface**: the consumer declares what it needs; wiring satisfies it.
2. **Events**: the producer publishes a domain event; consumers subscribe without importing it.
3. **Anti-Corruption Layer (ACL)**: a small translation package at the border.

### Consumer-defined interface

`billing` declares what it needs from outside, in its own vocabulary, **without importing
`identity`**:

**internal/billing/customer.go**

```go
// Package billing charges customers; it never imports identity.
package billing

// Customer is billing's read-only snapshot of a paying person, not identity.User.
type Customer struct {
	AccountID string
	Email     string
	Plan      string
}
```

**internal/billing/accounts.go**

```go
package billing

import "context"

// Accounts resolves customers that live outside billing; wiring provides the implementation.
type Accounts interface {
	Lookup(ctx context.Context, accountID string) (Customer, error)
}
```

The same human is a `User` in `identity` and a `Customer` in `billing`: each context models
what it needs. `Customer` is a plain snapshot returned by a port, not a billing entity, so
exported fields are fine here.

### Events

A domain event crosses the border with **primitive, serializable types**, never internal
value objects (`identity.Email` must not leak to another context or onto a bus):

**internal/identity/user_registered.go**

```go
package identity

// UserRegistered is published after RegisterUser saves a new user.
type UserRegistered struct {
	UserID string
	Email  string
}
```

### Minimal Anti-Corruption Layer

The ACL is the **only** place that knows both contexts: it translates `identity`'s model
into `billing`'s, satisfies the consumer-defined interface and shields `billing` from changes
in `identity`.

**internal/acl/identityacl/accounts.go**

```go
// Package identityacl translates identity users into billing customers.
package identityacl

import (
	"context"

	"example.com/shop/internal/billing"
	"example.com/shop/internal/identity"
)

// Accounts satisfies billing.Accounts by reading identity users.
type Accounts struct {
	users identity.UserRepository
}

// New adapts an identity repository for billing.
func New(users identity.UserRepository) *Accounts { return &Accounts{users: users} }

var _ billing.Accounts = (*Accounts)(nil)

// Lookup treats the billing account ID as an identity user ID.
func (a *Accounts) Lookup(ctx context.Context, accountID string) (billing.Customer, error) {
	u, err := a.users.FindByID(ctx, identity.UserID(accountID))
	if err != nil {
		return billing.Customer{}, err
	}
	return translate(u), nil
}

func translate(u *identity.User) billing.Customer {
	return billing.Customer{
		AccountID: string(u.ID()),
		Email:     u.Email().String(),
		Plan:      "free", // identity knows nothing about plans; billing's default applies
	}
}
```

The wiring (`cmd/`) is the only other place that imports both contexts: it builds
`identityacl.New(users)` and passes it to the billing use case that needs it
(`billing.NewChargeCustomer(accounts, ...)`). Neither `identity` nor `billing` imports the
other.

## 5. Each context has its own model

`identity.User` **is not** `billing.Customer`. A shared "global" type reintroduces `models`
and couples independent contexts.

- No type crosses the border as-is: it is **translated** (the ACL's `translate`).
- A change in `identity.User` breaks **only** the ACL, not all of `billing`. That is the point.
- IDs cross as strings or UUIDs, not as entities: store `AccountID`, not `*identity.User`.

## 6. Context = package, not automatically a service

A bounded context is a **logical** boundary, not a deployment one. Do not turn it into a
microservice by default: a package is free to move and checked by the compiler; the network
costs latency, versioning and observability.

- **Package first**: `internal/identity` and `internal/billing` in the same binary.
- **Split into a service only once the boundary is proven**: a stable consumer-defined
  interface and ACL, plus a real operational reason (scaling, fault isolation, separate teams).
- With a good ACL, promoting to a service means reimplementing `identityacl.Accounts` over
  HTTP/gRPC **without touching** `billing`: `Accounts` stays, only its implementation changes.

```
Monolith:  billing ──(Accounts)──▶ identityacl.Accounts ──▶ identity.UserRepository (in-process)
Service:   billing ──(Accounts)──▶ identityacl.Accounts ──▶ HTTP client ──▶ identity service
```

## Checklist

- [ ] Each bounded context is **one package** under `internal/` (`internal/identity`, `internal/billing`).
- [ ] The tree is organized by **context**, not by layer (`domain/application/infrastructure`).
- [ ] Inside a context, split by **file**; subpackages only for infrastructure (`pgstore`) and transport (`identityhttp`).
- [ ] Each file names a concept (`user.go`) or a use case (`register_user.go`); no `service.go`, `usecase.go`, `manager.go`, `helpers.go`.
- [ ] The exported API is the **ubiquitous language**; no grab-bag packages (`util`, `models`, `types`).
- [ ] No context imports another's internals; they talk through consumer-defined interfaces, events or an ACL.
- [ ] Interfaces are defined by the **consumer**; an infrastructure subpackage implements them.
- [ ] Each context has its **own model**; types are **translated** at the border, not shared.
- [ ] Events and IDs cross as **primitives**, never as value objects or entity pointers.
- [ ] Context = package by default; **split into a service only when the boundary is proven**.
