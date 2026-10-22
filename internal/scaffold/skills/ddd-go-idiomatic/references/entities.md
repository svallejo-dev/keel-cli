# Entities

An entity in Go is not a JavaBean: no `Get*` getters, no setters, no anemic model. Identity
and mutable state are modeled with the language's own tools (structs, pointer receivers,
dedicated ID types). When a DDD habit and a Go idiom disagree, the Go idiom wins.

This file covers only the entity. Neighbouring topics live elsewhere:

- Building, normalizing and comparing a VO → [value-objects.md](value-objects.md) (here VOs are only composed).
- The root that groups entities under a transactional boundary → [aggregates.md](aggregates.md).
- Orchestration, transactions and persistence → [application-layer.md](application-layer.md).
- The anemic model and its refactor → [antipatterns.md](antipatterns.md).

## 1. What an entity is

An **entity** is defined by its **identity**, not its attributes:

- It has a stable **ID** that identifies it uniquely and persistently.
- It is **mutable over time**: attributes change, identity does not.
- It is **composed of value objects**: every attribute with its own rules is a VO, not a bare primitive.

A `User` is still the same `User` after changing email, name and everything else, because
its `id` did not change.

## 2. Shape in Go

A struct with **unexported fields** (no other package can build an invalid state) and
**pointer-receiver methods** (they mutate state and carry identity).

```go
// UserID identifies a User; the empty string means "not assigned".
type UserID string

// User is a registered person; its identity is id.
type User struct {
	id        UserID
	email     Email
	name      Name
	createdAt time.Time
}
```

The ID is a **dedicated type** (`type UserID string`, or a UUID value object), not a loose
`string` or `int64`: passing an `OrderID` where a `UserID` is expected does not compile.

### Pointer receiver (entity) vs value receiver (VO)

The receiver kind encodes domain semantics, not taste.

| Aspect | Entity | Value object |
|---|---|---|
| Receiver | `func (u *User)`, pointer | `func (e Email)`, value |
| Mutates state | Yes (`u.email = ...`) | Never; returns a new value |
| Identity | Yes, by ID | No; pure value |
| Equality | By ID (`SameAs`) | By value (`==`) |
| Copying | Shared by pointer | Copied freely |

If a type has identity or any method mutates it, **all** its methods use pointer receivers;
mixing value and pointer receivers on one type makes its method set inconsistent.

### Zero value and why the constructor is mandatory

The zero value of an entity is **invalid**: no ID, empty VOs, no `createdAt`. Unexported
fields are what stop other packages from building that state; the only door is `NewUser`.
Exporting the fields opens the door and the entity protects nothing.

```go
// BAD: exported fields; anyone builds an invalid User without NewUser.
type User struct {
	ID    UserID
	Email Email
}

u := User{} // no ID, empty email: a state the domain should never see

// GOOD: unexported fields + constructor.
u, err := NewUser(id, email, name, now)
```

## 3. Equality by identity, not by value

Two entities are the **same** when they share an ID, even if every attribute differs, and
**different** when IDs differ, even if everything else matches. That is the opposite of a VO.

So never use `==` on entities. On values it compares field by field (same ID, different
`createdAt` → "different"); on pointers it compares addresses, so two loads of the same row
are "different". Compare identity explicitly:

```go
// SameAs compares identity, never field values.
func (u *User) SameAs(other *User) bool {
	return u != nil && other != nil && u.id == other.id
}
```

`==` on the **ID** (`u.id == other.id`) is right: the ID is a value and value equality is
exactly what identity means.

The same rule governs indexing and deduplication: the key is always the ID.

```go
byID := make(map[UserID]*User, len(users))
for _, u := range users {
	byID[u.ID()] = u // two loads of the same entity collapse into one
}
```

Using the entity itself as a key (`map[User]...`) ties the key to mutable values instead of
identity.

## 4. The constructor validates how the VOs fit together

An entity constructor **does not re-validate VOs**: an `Email` or a `Name` that reaches it is
already valid by construction. The entity checks rules that **span** several VOs, which no
single VO can know.

```go
// ErrIncompleteName means a corporate email needs a first and last name.
var ErrIncompleteName = errors.New("identity: corporate email requires first and last name")

// NewUser requires first and last name when the email is corporate.
func NewUser(id UserID, email Email, name Name, now time.Time) (*User, error) {
	if id == "" {
		return nil, errors.New("identity: user requires an id")
	}
	if email.IsCorporate() && len(strings.Fields(name.String())) < 2 {
		return nil, ErrIncompleteName
	}
	return &User{id: id, email: email, name: name, createdAt: now}, nil
}
```

(`Email.IsCorporate` is a VO method that checks the domain part.)

The signature takes **VOs, not strings**, and the current time **as a parameter**: the
domain never calls `time.Now()`. The use case owns the clock (`now func() time.Time`,
injected), which keeps `NewUser` deterministic and testable. Stores call the same
constructor with the stored `createdAt` when they rebuild a `User`.

Responsibilities do not overlap:

| Rule | Where it lives | Example |
|---|---|---|
| Format/range of one field | VO constructor | `NewEmail` requires an address |
| Normalization of one field | VO constructor | `NewEmail` lower-cases |
| Combination of several VOs | Entity constructor | corporate email ⇒ first and last name |
| Valid state transition | Entity method | `ChangeEmail` re-checks the combination |
| Coordination across entities/aggregates | Use case | see [application-layer.md](application-layer.md) |

Return `(*User, error)`: the entity is shared by reference, and the error forces the caller
to handle the invalid case before holding a `User`. If an entity grows many cross-cutting
invariants or coordinates sub-objects, it is probably an aggregate root
([aggregates.md](aggregates.md)).

## 5. Behaviour with intent, not setters

State changes through methods that **name the domain operation** and **re-check
invariants**, not through mechanical setters.

```go
// ChangeEmail re-applies the corporate-email rule before switching.
func (u *User) ChangeEmail(newEmail Email) error {
	if newEmail.IsCorporate() && len(strings.Fields(u.name.String())) < 2 {
		return ErrIncompleteName
	}
	u.email = newEmail
	return nil
}
```

```go
// BAD: anemic setter; no intent, and it can break the invariant.
func (u *User) SetEmail(e Email) { u.email = e }
```

(In the worked example there is no cross-VO rule, so its `ChangeEmail` cannot fail and
returns nothing.)

Accessors: Go has no `Get` prefix (`Email()`, not `GetEmail()`), and an accessor exists only
when another package really needs to read the attribute. It returns the **VO**, not its
internal representation, so the guarantees travel with it.

```go
// Email is the current normalized address.
func (u *User) Email() Email { return u.email }
```

Method names are ubiquitous language: `Deactivate`, `Promote`, `Rename`, `ChangeEmail`. A
generic `Update(fields map[string]any)` erases intent and reopens invalid states.

## 6. Rich vs anemic

Logic lives **with** the data, in the same package as the type. A rich entity's methods are
the domain behaviour: `u.ChangeEmail(...)`, `u.Deactivate()`.

The **anemic model** is the reverse: the entity is a bag of exported fields and the logic
lives in a fat `UserService` that edits it from outside. Nobody owns the invariants.

```go
// BAD: data without behaviour, in one package...
type User struct {
	ID    string
	Email string
}

// ...and a service in another that edits it with no protection.
func (UserService) ChangeEmail(u *models.User, e string) { u.Email = e }
```

The fix is §5: the method on `User`, in the context package. Legitimate orchestration
(transactions, coordinating aggregates) does live outside the entity, but as one use case
per file (`change_email.go` → `ChangeEmail`, which loads the `User`, calls
`u.ChangeEmail(...)` and saves it), never a `UserService`. See
[application-layer.md](application-layer.md) and [antipatterns.md](antipatterns.md) §4.

## 7. The entity does not touch infrastructure

An entity works only with VOs and domain types. Its import list is the fastest diagnosis
of a healthy design, the **import purity test**:

```go
// GOOD: a healthy entity imports pure stdlib.
import (
	"errors"
	"strings"
	"time"
)

// BAD: infrastructure leaked into the domain.
import (
	"database/sql"
	"net/http"
)
```

If the entity's package imports `database/sql`, `net/http`, a driver or an HTTP client, the
domain is contaminated. Persistence is a port (`UserRepository`) declared by the consuming
use cases and implemented in an infrastructure subpackage; transport lives in its own
subpackage. `time` passes the test because `time.Time` is a domain value; reading the clock
does not, which is why `now` is a parameter.

## Checklist

- [ ] The entity is a struct with **unexported** fields and a **dedicated ID type** (`type UserID string` or a UUID VO).
- [ ] All methods use **pointer receivers**; value receivers are for VOs.
- [ ] Equality uses `SameAs` (by ID), never `==` on the entity.
- [ ] The constructor takes **VOs and `now time.Time`**, returns `(*User, error)` and checks only cross-VO rules.
- [ ] The domain never calls `time.Now()`; the use case injects the clock.
- [ ] State changes through **domain-named methods** that re-check invariants; no setters, no `Get` prefix.
- [ ] Accessors exist only when needed and return the **VO**, not its raw representation.
- [ ] Logic lives with the data; no `models/` + `services/` split.
- [ ] Orchestration that leaves the entity is a named use case (`change_email.go`), never a `UserService`.
- [ ] Imports pass the purity test: no `database/sql`, `net/http` or infrastructure libraries.
