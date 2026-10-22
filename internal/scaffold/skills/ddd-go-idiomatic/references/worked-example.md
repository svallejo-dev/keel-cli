# Worked Example — A Complete Bounded Context (`identity`)

## Contents

- Layout and dependency direction
- 1. Value objects — `errors.go`, `email.go`, `name.go`
- 2. Entity / aggregate root — `user.go`
- 3. Shared port — `user_repository.go`
- 4. Use cases — `register_user.go`, `change_email.go`
- 5. Infrastructure — `memstore/memstore.go`
- 6. Transport — `identityhttp/`
- 7. Wiring — `cmd/identityd/main.go`
- 8. Tests — `register_user_test.go`, `change_email_test.go`
- What it leaves out on purpose, and how to run it

The `identity` context end to end: value objects → entity → port → use cases →
infrastructure → transport → wiring → tests. Two use cases, **register a user** and
**change their email**, so two types in two files. Every Go block below is extracted by
`scripts/check-worked-example.sh` (repo root) into a module and must pass `gofmt -l`,
`go vet` and `go test -race`; the bold line above each block is the file path.

## Layout and dependency direction

```
shop/                                  # module example.com/shop
├── cmd/identityd/main.go              # wiring: the only place that knows every package
└── internal/identity/                 # the bounded context: domain + use cases
    ├── errors.go                      # sentinel errors
    ├── email.go, name.go              # value objects
    ├── user.go                        # entity / aggregate root + package doc
    ├── user_repository.go             # port shared by both use cases
    ├── register_user.go               # use case + IDGenerator + DTOs
    ├── change_email.go                # use case + DTOs
    ├── register_user_test.go
    ├── change_email_test.go
    ├── memstore/memstore.go           # infrastructure: in-memory port implementation
    └── identityhttp/                  # transport: one file per endpoint
        ├── routes.go
        ├── register_user.go
        ├── change_email.go
        └── responses.go               # JSON + domain error → status
```

```
cmd/identityd ─▶ identityhttp ─▶ identity ◀─ memstore
                                    (defines UserRepository, IDGenerator; imports nothing outward)
```

`identity` does not know `memstore` or `identityhttp`; they consume or implement it. That
is Clean Architecture's dependency inversion expressed with consumer-defined interfaces.

## 1. Value objects — `errors.go`, `email.go`, `name.go`

Each value object is a struct with an unexported field, validated once in `NewX`, so holding
an `Email` proves it is valid. Validation errors are sentinels so the transport can map them
to `400` without parsing strings.

**internal/identity/errors.go**

```go
package identity

import "errors"

// Sentinel errors of the identity context; identityhttp maps each one to a status code.
var (
	ErrInvalidEmail = errors.New("identity: invalid email")
	ErrInvalidName  = errors.New("identity: invalid name")
	ErrEmailTaken   = errors.New("identity: email already registered")
	ErrUserNotFound = errors.New("identity: user not found")
)
```

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
	if addr == "" {
		return Email{}, fmt.Errorf("%w: empty", ErrInvalidEmail)
	}
	if _, err := mail.ParseAddress(addr); err != nil {
		return Email{}, fmt.Errorf("%w: %q", ErrInvalidEmail, raw)
	}
	return Email{v: addr}, nil
}

// String returns the normalized address.
func (e Email) String() string { return e.v }
```

**internal/identity/name.go**

```go
package identity

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Name is a person's display name, 2 to 60 characters after trimming.
type Name struct{ v string }

// NewName trims raw and fails with ErrInvalidName when the length is out of range.
func NewName(raw string) (Name, error) {
	n := strings.TrimSpace(raw)
	if c := utf8.RuneCountInString(n); c < 2 || c > 60 {
		return Name{}, fmt.Errorf("%w: length %d out of [2,60]", ErrInvalidName, c)
	}
	return Name{v: n}, nil
}

// String returns the trimmed name.
func (n Name) String() string { return n.v }
```

## 2. Entity / aggregate root — `user.go`

`User` has identity, is composed of value objects and changes only through intent-revealing
methods. `NewUser` checks what no single value object can (here, only that an ID exists);
time comes in as a parameter so the domain never reads the clock. `User` is the root because
it has no sub-entities; inventing a separate root would be ceremony. This file carries the
package doc, once for the whole package.

**internal/identity/user.go**

```go
// Package identity is the bounded context for user accounts: domain model and use cases.
package identity

import (
	"errors"
	"time"
)

// UserID identifies a User; the empty string means "not assigned".
type UserID string

// User is the aggregate root of the identity context.
type User struct {
	id        UserID
	email     Email
	name      Name
	createdAt time.Time
}

// NewUser assembles already-valid value objects; it is also how stores rebuild a User.
func NewUser(id UserID, email Email, name Name, now time.Time) (*User, error) {
	if id == "" {
		return nil, errors.New("identity: user requires an id")
	}
	return &User{id: id, email: email, name: name, createdAt: now}, nil
}

// ID never changes during the life of the user.
func (u *User) ID() UserID { return u.id }

// Email is the current normalized address.
func (u *User) Email() Email { return u.email }

// Name is the display name given at registration.
func (u *User) Name() Name { return u.name }

// CreatedAt is the registration time taken from the use case's clock.
func (u *User) CreatedAt() time.Time { return u.createdAt }

// ChangeEmail replaces the address; uniqueness is enforced by the use case and the repository.
func (u *User) ChangeEmail(newEmail Email) { u.email = newEmail }
```

## 3. Shared port — `user_repository.go`

The consumer defines the interface. `UserRepository` gets its own file because both use
cases need it; `IDGenerator` is used only by `RegisterUser`, so it lives in that file.

**internal/identity/user_repository.go**

```go
package identity

import "context"

// UserRepository is shared by RegisterUser and ChangeEmail; Save reports ErrEmailTaken like a unique index.
type UserRepository interface {
	FindByID(ctx context.Context, id UserID) (*User, error)
	ExistsByEmail(ctx context.Context, email Email) (bool, error)
	Save(ctx context.Context, u *User) error
}
```

## 4. Use cases — `register_user.go`, `change_email.go`

One type named after the action, in a file with the same name: constructor `NewX`, a single
`Execute(ctx, XInput) (XOutput, error)`, DTOs next to it. DTOs carry transport-friendly
primitives, not value objects, so transports never depend on domain types.

**internal/identity/register_user.go**

```go
package identity

import (
	"context"
	"fmt"
	"time"
)

// IDGenerator is declared here because only RegisterUser needs it.
type IDGenerator interface {
	NewUserID() UserID
}

// RegisterUserInput carries raw transport values; Execute parses them into value objects.
type RegisterUserInput struct {
	Email string
	Name  string
}

// RegisterUserOutput uses primitives so transports never depend on value objects.
type RegisterUserOutput struct {
	ID    string
	Email string
}

// RegisterUser signs up a user whose email nobody else owns.
type RegisterUser struct {
	users UserRepository
	ids   IDGenerator
	now   func() time.Time
}

// NewRegisterUser takes the clock as a dependency so tests can freeze time; nil means time.Now.
func NewRegisterUser(users UserRepository, ids IDGenerator, now func() time.Time) *RegisterUser {
	if now == nil {
		now = time.Now
	}
	return &RegisterUser{users: users, ids: ids, now: now}
}

// Execute fails with ErrInvalidEmail, ErrInvalidName or ErrEmailTaken when the input is rejected.
func (uc *RegisterUser) Execute(ctx context.Context, in RegisterUserInput) (RegisterUserOutput, error) {
	email, err := NewEmail(in.Email)
	if err != nil {
		return RegisterUserOutput{}, err
	}
	name, err := NewName(in.Name)
	if err != nil {
		return RegisterUserOutput{}, err
	}

	taken, err := uc.users.ExistsByEmail(ctx, email)
	if err != nil {
		return RegisterUserOutput{}, fmt.Errorf("register user: check email: %w", err)
	}
	if taken {
		return RegisterUserOutput{}, ErrEmailTaken
	}

	u, err := NewUser(uc.ids.NewUserID(), email, name, uc.now())
	if err != nil {
		return RegisterUserOutput{}, err
	}
	if err := uc.users.Save(ctx, u); err != nil {
		return RegisterUserOutput{}, fmt.Errorf("register user: save: %w", err)
	}
	return RegisterUserOutput{ID: string(u.ID()), Email: u.Email().String()}, nil
}
```

**internal/identity/change_email.go**

```go
package identity

import (
	"context"
	"fmt"
)

// ChangeEmailInput carries the target user and the raw new address.
type ChangeEmailInput struct {
	UserID string
	Email  string
}

// ChangeEmailOutput echoes the stored, normalized address.
type ChangeEmailOutput struct {
	ID    string
	Email string
}

// ChangeEmail moves an existing user to an address nobody else owns.
type ChangeEmail struct {
	users UserRepository
}

// NewChangeEmail needs only the shared repository.
func NewChangeEmail(users UserRepository) *ChangeEmail {
	return &ChangeEmail{users: users}
}

// Execute fails with ErrUserNotFound for an unknown id and ErrEmailTaken if another user owns the address.
func (uc *ChangeEmail) Execute(ctx context.Context, in ChangeEmailInput) (ChangeEmailOutput, error) {
	email, err := NewEmail(in.Email)
	if err != nil {
		return ChangeEmailOutput{}, err
	}

	u, err := uc.users.FindByID(ctx, UserID(in.UserID))
	if err != nil {
		return ChangeEmailOutput{}, err
	}
	if u.Email() == email {
		return ChangeEmailOutput{ID: string(u.ID()), Email: email.String()}, nil
	}

	taken, err := uc.users.ExistsByEmail(ctx, email)
	if err != nil {
		return ChangeEmailOutput{}, fmt.Errorf("change email: check email: %w", err)
	}
	if taken {
		return ChangeEmailOutput{}, ErrEmailTaken
	}

	u.ChangeEmail(email)
	if err := uc.users.Save(ctx, u); err != nil {
		return ChangeEmailOutput{}, fmt.Errorf("change email: save: %w", err)
	}
	return ChangeEmailOutput{ID: string(u.ID()), Email: email.String()}, nil
}
```

A new action such as "deactivate user" is a new file (`deactivate_user.go`) with its own
`DeactivateUser` type, never one more method on a shared struct.

## 5. Infrastructure — `memstore/memstore.go`

An in-memory implementation of both ports, used by the tests and for local runs. It stores
rows and rebuilds a fresh `*User` on every read: handing out the stored pointer would let
`ChangeEmail` mutate shared state outside the lock (a data race), and copying is what a real
database does anyway. `Save` enforces email uniqueness like a unique index, which closes the
gap between `ExistsByEmail` and `Save` when two requests race. A `pgstore` would sit next to
it and translate driver errors (`23505`, `pgx.ErrNoRows`) into the same sentinels.

**internal/identity/memstore/memstore.go**

```go
// Package memstore keeps identity users in memory, for tests and local runs.
package memstore

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"example.com/shop/internal/identity"
)

type row struct {
	email     string
	name      string
	createdAt time.Time
}

// Store is safe for concurrent use; it never shares a *identity.User with callers.
type Store struct {
	mu     sync.Mutex
	rows   map[identity.UserID]row
	owners map[string]identity.UserID
	seq    int64
}

// New returns an empty Store.
func New() *Store {
	return &Store{
		rows:   make(map[identity.UserID]row),
		owners: make(map[string]identity.UserID),
	}
}

var (
	_ identity.UserRepository = (*Store)(nil)
	_ identity.IDGenerator    = (*Store)(nil)
)

// NewUserID hands out sequential ids: u1, u2, ...
func (s *Store) NewUserID() identity.UserID {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	return identity.UserID("u" + strconv.FormatInt(s.seq, 10))
}

// FindByID returns a new *User rebuilt from the stored row, or identity.ErrUserNotFound.
func (s *Store) FindByID(_ context.Context, id identity.UserID) (*identity.User, error) {
	s.mu.Lock()
	r, ok := s.rows[id]
	s.mu.Unlock()
	if !ok {
		return nil, identity.ErrUserNotFound
	}
	email, err := identity.NewEmail(r.email)
	if err != nil {
		return nil, fmt.Errorf("memstore: corrupt row %s: %w", id, err)
	}
	name, err := identity.NewName(r.name)
	if err != nil {
		return nil, fmt.Errorf("memstore: corrupt row %s: %w", id, err)
	}
	return identity.NewUser(id, email, name, r.createdAt)
}

// ExistsByEmail reports whether any user owns email.
func (s *Store) ExistsByEmail(_ context.Context, email identity.Email) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.owners[email.String()]
	return ok, nil
}

// Save copies u's state and fails with identity.ErrEmailTaken if another user owns the email.
func (s *Store) Save(_ context.Context, u *identity.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	email := u.Email().String()
	if owner, ok := s.owners[email]; ok && owner != u.ID() {
		return identity.ErrEmailTaken
	}
	if old, ok := s.rows[u.ID()]; ok {
		delete(s.owners, old.email)
	}
	s.rows[u.ID()] = row{email: email, name: u.Name().String(), createdAt: u.CreatedAt()}
	s.owners[email] = u.ID()
	return nil
}
```

## 6. Transport — `identityhttp/`

One file per endpoint, named after the use case it exposes. Each one translates HTTP ↔
`Input`/`Output` with its own request/response structs; there is no `mapper/`. `routes.go`
mounts them with `net/http` method patterns (Go 1.22+), and `responses.go` holds the single
domain error → status mapping.

**internal/identity/identityhttp/routes.go**

```go
// Package identityhttp exposes the identity use cases over HTTP.
package identityhttp

import (
	"net/http"

	"example.com/shop/internal/identity"
)

// UseCases lists what this transport exposes; main builds each one with its own dependencies.
type UseCases struct {
	RegisterUser *identity.RegisterUser
	ChangeEmail  *identity.ChangeEmail
}

// Mount registers one route per use case; ServeMux answers 405 for other methods.
func Mount(mux *http.ServeMux, uc UseCases) {
	mux.Handle("POST /users", registerUser(uc.RegisterUser))
	mux.Handle("PUT /users/{id}/email", changeEmail(uc.ChangeEmail))
}
```

**internal/identity/identityhttp/register_user.go**

```go
package identityhttp

import (
	"encoding/json"
	"net/http"

	"example.com/shop/internal/identity"
)

type registerUserRequest struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

type registerUserResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

func registerUser(uc *identity.RegisterUser) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req registerUserRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		out, err := uc.Execute(r.Context(), identity.RegisterUserInput{Email: req.Email, Name: req.Name})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, registerUserResponse{ID: out.ID, Email: out.Email})
	})
}
```

**internal/identity/identityhttp/change_email.go**

```go
package identityhttp

import (
	"encoding/json"
	"net/http"

	"example.com/shop/internal/identity"
)

type changeEmailRequest struct {
	Email string `json:"email"`
}

type changeEmailResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

func changeEmail(uc *identity.ChangeEmail) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req changeEmailRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		in := identity.ChangeEmailInput{UserID: r.PathValue("id"), Email: req.Email}
		out, err := uc.Execute(r.Context(), in)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, changeEmailResponse{ID: out.ID, Email: out.Email})
	})
}
```

**internal/identity/identityhttp/responses.go**

```go
package identityhttp

import (
	"encoding/json"
	"errors"
	"net/http"

	"example.com/shop/internal/identity"
)

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeError never echoes an unknown error: it could leak SQL, hosts or stack details.
func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, identity.ErrInvalidEmail), errors.Is(err, identity.ErrInvalidName):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, identity.ErrEmailTaken):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, identity.ErrUserNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	default:
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}
```

## 7. Wiring — `cmd/identityd/main.go`

The only place that knows every package: it builds the infrastructure, injects it into each
use case separately and mounts the transport.

**cmd/identityd/main.go**

```go
// Command identityd serves the identity context over HTTP backed by the in-memory store.
package main

import (
	"log"
	"net/http"
	"time"

	"example.com/shop/internal/identity"
	"example.com/shop/internal/identity/identityhttp"
	"example.com/shop/internal/identity/memstore"
)

func main() {
	store := memstore.New()

	mux := http.NewServeMux()
	identityhttp.Mount(mux, identityhttp.UseCases{
		RegisterUser: identity.NewRegisterUser(store, store, nil),
		ChangeEmail:  identity.NewChangeEmail(store),
	})

	srv := &http.Server{Addr: ":8080", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
```

## 8. Tests — `register_user_test.go`, `change_email_test.go`

Test files follow the same naming: `register_user_test.go` tests `RegisterUser`. They use
`memstore` as the fake, since it is small, already satisfies the consumer-defined ports and
needs no mocking library. The concurrent tests are there for `go test -race`: they fail if the
store ever shares a `*User` or checks uniqueness outside its lock.

**internal/identity/register_user_test.go**

```go
package identity_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"example.com/shop/internal/identity"
	"example.com/shop/internal/identity/memstore"
)

func TestRegisterUser(t *testing.T) {
	store := memstore.New()
	frozen := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	uc := identity.NewRegisterUser(store, store, func() time.Time { return frozen })

	out, err := uc.Execute(context.Background(), identity.RegisterUserInput{Email: " Ada@Example.com ", Name: "Ada Lovelace"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Email != "ada@example.com" {
		t.Errorf("Email = %q, want normalized ada@example.com", out.Email)
	}
	u, err := store.FindByID(context.Background(), identity.UserID(out.ID))
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if !u.CreatedAt().Equal(frozen) {
		t.Errorf("CreatedAt = %v, want %v", u.CreatedAt(), frozen)
	}
}

func TestRegisterUser_Rejects(t *testing.T) {
	tests := []struct {
		name string
		in   identity.RegisterUserInput
		want error
	}{
		{"invalid email", identity.RegisterUserInput{Email: "nope", Name: "Ada"}, identity.ErrInvalidEmail},
		{"short name", identity.RegisterUserInput{Email: "ada@example.com", Name: "A"}, identity.ErrInvalidName},
		{"email taken", identity.RegisterUserInput{Email: "taken@example.com", Name: "Ada"}, identity.ErrEmailTaken},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := memstore.New()
			uc := identity.NewRegisterUser(store, store, nil)
			if _, err := uc.Execute(context.Background(), identity.RegisterUserInput{Email: "taken@example.com", Name: "Grace"}); err != nil {
				t.Fatalf("seed: %v", err)
			}
			if _, err := uc.Execute(context.Background(), tt.in); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestRegisterUser_ConcurrentSameEmail(t *testing.T) {
	store := memstore.New()
	uc := identity.NewRegisterUser(store, store, nil)

	const n = 20
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			_, err := uc.Execute(context.Background(), identity.RegisterUserInput{Email: "ada@example.com", Name: "Ada"})
			errs <- err
		})
	}
	wg.Wait()
	close(errs)

	wins := 0
	for err := range errs {
		switch {
		case err == nil:
			wins++
		case !errors.Is(err, identity.ErrEmailTaken):
			t.Errorf("unexpected error: %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("%d registrations succeeded, want exactly 1", wins)
	}
}
```

**internal/identity/change_email_test.go**

```go
package identity_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"example.com/shop/internal/identity"
	"example.com/shop/internal/identity/memstore"
)

func register(t *testing.T, store *memstore.Store, email string) string {
	t.Helper()
	out, err := identity.NewRegisterUser(store, store, nil).Execute(context.Background(), identity.RegisterUserInput{Email: email, Name: "Ada Lovelace"})
	if err != nil {
		t.Fatalf("register %s: %v", email, err)
	}
	return out.ID
}

func TestChangeEmail(t *testing.T) {
	store := memstore.New()
	id := register(t, store, "ada@example.com")

	out, err := identity.NewChangeEmail(store).Execute(context.Background(), identity.ChangeEmailInput{UserID: id, Email: "ADA@new.com"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Email != "ada@new.com" {
		t.Errorf("Email = %q, want ada@new.com", out.Email)
	}
	old, _ := identity.NewEmail("ada@example.com")
	if taken, _ := store.ExistsByEmail(context.Background(), old); taken {
		t.Error("old address is still reserved")
	}
}

func TestChangeEmail_Rejects(t *testing.T) {
	store := memstore.New()
	id := register(t, store, "ada@example.com")
	register(t, store, "grace@example.com")
	uc := identity.NewChangeEmail(store)

	if _, err := uc.Execute(context.Background(), identity.ChangeEmailInput{UserID: id, Email: "grace@example.com"}); !errors.Is(err, identity.ErrEmailTaken) {
		t.Errorf("taken: err = %v, want ErrEmailTaken", err)
	}
	if _, err := uc.Execute(context.Background(), identity.ChangeEmailInput{UserID: "nope", Email: "a@b.com"}); !errors.Is(err, identity.ErrUserNotFound) {
		t.Errorf("unknown user: err = %v, want ErrUserNotFound", err)
	}
}

func TestChangeEmail_Concurrent(t *testing.T) {
	store := memstore.New()
	id := register(t, store, "ada@example.com")
	uc := identity.NewChangeEmail(store)

	const n = 20
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			in := identity.ChangeEmailInput{UserID: id, Email: fmt.Sprintf("ada+%d@example.com", i)}
			if _, err := uc.Execute(context.Background(), in); err != nil {
				t.Errorf("Execute: %v", err)
			}
		})
	}
	wg.Wait()

	u, err := store.FindByID(context.Background(), identity.UserID(id))
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	for i := range n {
		e, _ := identity.NewEmail(fmt.Sprintf("ada+%d@example.com", i))
		taken, _ := store.ExistsByEmail(context.Background(), e)
		if taken != (e == u.Email()) {
			t.Errorf("%s reserved = %v, but the user's email is %s", e, taken, u.Email())
		}
	}
}
```

## What it leaves out on purpose

- No `service.go` or `usecase.go`: each use case is its own type in its own file.
- No `handler.go` with every endpoint: one file per endpoint plus `routes.go`.
- No `domain/`, `application/`, `infrastructure/` folders: one package per context.
- No `mapper/`: DTOs live next to the use case; each endpoint translates by hand.
- No interface per struct: only `UserRepository` and `IDGenerator`, declared by their consumer.
- No extra aggregate: `User` is entity and root at once; a root needs a real transactional invariant.

## How to run it

```bash
sh scripts/check-worked-example.sh   # extracts this page, then gofmt -l, go vet, go test -race
go run ./cmd/identityd               # inside the extracted module; serves :8080
curl -X POST localhost:8080/users -d '{"email":"ada@example.com","name":"Ada Lovelace"}'
curl -X PUT localhost:8080/users/u1/email -d '{"email":"ada@new.com"}'
```

The domain purity test also runs in the script: `go list -deps ./internal/identity` must not
list `database/sql` or `net/http`.
