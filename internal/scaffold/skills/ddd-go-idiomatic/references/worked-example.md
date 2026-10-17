# Worked Example — Un Bounded Context Completo (`identity`)

> **Regla #0:** ante conflicto entre DDD y go-idiomatic, gana go-idiomatic. Este ejemplo aplica los building blocks tácticos SIN Java-ismos: cero carpetas por capa, cero `mapper/`, cero interfaz-por-clase, cero `service.go`.

Un contexto `identity` de punta a punta: value objects → entity/aggregate → puertos → casos de uso → infraestructura → transporte → wiring. **Todo el código de esta página compila, pasa `go vet` y tiene tests que pasan** (`go build ./... && go vet ./... && go test ./...`), y el paquete de dominio pasa el *test de pureza*: `go list -deps ./internal/identity` no importa `database/sql` ni `net/http`.

Casos de uso: **registrar un usuario** y **cambiar su email**. Son dos casos de uso, así que son dos tipos en dos archivos: `register_user.go` y `change_email.go`.

## Layout de archivos

El contexto es UN paquete (`internal/identity`). Cada archivo dice qué contiene: un concepto del dominio o un caso de uso. Infraestructura y transporte son sub-paquetes que dependen hacia adentro.

```
identityexample/
├── cmd/identityd/main.go              # wiring (único que conoce todo)
└── internal/
    └── identity/                      # el bounded context: dominio + casos de uso
        ├── errors.go                  # errores de dominio (sentinela)
        ├── email.go / name.go         # value objects
        ├── user.go                    # entity / aggregate root
        ├── user_repository.go         # puerto compartido por los dos casos de uso
        ├── register_user.go           # caso de uso + IDGenerator + DTOs
        ├── change_email.go            # caso de uso + DTOs
        ├── register_user_test.go
        ├── change_email_test.go
        ├── fake_user_repository_test.go
        ├── memstore/memstore.go       # infra: impl en memoria de los puertos
        └── identityhttp/              # transporte: un archivo por endpoint
            ├── routes.go
            ├── register_user.go
            ├── change_email.go
            └── responses.go           # JSON y error de dominio → status
```

Lo que **no** aparece: `service.go`, `usecase.go`, `handler.go`. Abrir el árbol ya cuenta qué hace el contexto.

## Dirección de dependencias (todo apunta hacia adentro)

```
cmd/identityd ─▶ identityhttp ─▶ identity ◀─ memstore
                                    ▲  (define UserRepository, IDGenerator)
                                    └─ el dominio no importa a nadie de afuera
```

`identity` (dominio + casos de uso) no conoce a `memstore` (infra) ni a `identityhttp` (transporte): ellos lo consumen/implementan a él. Esa es la inversión de dependencias de Clean Architecture, expresada con interfaces *consumer-defined* de Go.

## 1. Value Objects — `errors.go`, `email.go`, `name.go`

Cada VO es inmutable (campo no-exportado), se valida en su constructor `NewX() (X, error)` y expone su valor con `String()`. Los errores de validación son sentinela, para que la frontera los mapee a `400`; salen del contexto como identidad de error, nunca como error del driver.

```go
// internal/identity/errors.go
// Package identity es el bounded context de usuarios: dominio y casos de uso.
package identity

import "errors"

var (
	ErrInvalidEmail = errors.New("identity: invalid email")
	ErrInvalidName  = errors.New("identity: invalid name")
	ErrEmailTaken   = errors.New("identity: email already registered")
	ErrUserNotFound = errors.New("identity: user not found")
)
```

```go
// internal/identity/email.go
package identity

import (
	"fmt"
	"net/mail"
	"strings"
)

// Email es una dirección normalizada a minúsculas y válida por construcción.
type Email struct{ v string }

// NewEmail normaliza raw y falla con ErrInvalidEmail si no es una dirección.
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

func (e Email) String() string { return e.v }
```

```go
// internal/identity/name.go
package identity

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Name es el nombre visible de una persona, entre 2 y 60 caracteres.
type Name struct{ v string }

// NewName recorta raw y falla con ErrInvalidName si queda fuera de rango.
func NewName(raw string) (Name, error) {
	n := strings.TrimSpace(raw)
	if c := utf8.RuneCountInString(n); c < 2 || c > 60 {
		return Name{}, fmt.Errorf("%w: length %d out of [2,60]", ErrInvalidName, c)
	}
	return Name{v: n}, nil
}

func (n Name) String() string { return n.v }
```

## 2. Entity / Aggregate Root — `user.go`

`User` tiene identidad (`UserID`), se compone de VOs y solo se muta por métodos con intención (`ChangeEmail`), nunca por setters. `NewUser` impone las invariantes de **integración** del aggregate, no la validación propia de cada VO. Aquí `User` **es** el aggregate root del contexto (no hay sub-entidades): no inventamos un root de más. Igualdad por ID (`SameAs`), nunca por valor.

```go
// internal/identity/user.go
package identity

import (
	"errors"
	"time"
)

// UserID es la identidad de un User; "" significa sin asignar.
type UserID string

// User es el aggregate root del contexto.
type User struct {
	id        UserID
	email     Email
	name      Name
	createdAt time.Time
}

// NewUser integra value objects ya válidos; solo exige que haya identidad.
func NewUser(id UserID, email Email, name Name, now time.Time) (*User, error) {
	if id == "" {
		return nil, errors.New("identity: user requires an id")
	}
	return &User{id: id, email: email, name: name, createdAt: now}, nil
}

func (u *User) ID() UserID   { return u.id }
func (u *User) Email() Email { return u.email }
func (u *User) Name() Name   { return u.name }

// ChangeEmail es la única forma de cambiar el email de un User.
func (u *User) ChangeEmail(newEmail Email) {
	u.email = newEmail
}

// SameAs compara por identidad, nunca por valor.
func (u *User) SameAs(other *User) bool {
	return u != nil && other != nil && u.id == other.id
}
```

## 3. Puerto compartido — `user_repository.go`

Las interfaces las define el **consumidor**, no la infraestructura. `UserRepository` vive en su propio archivo porque lo usan **los dos** casos de uso. `IDGenerator` solo lo necesita `RegisterUser`, así que se declara en `register_user.go`, junto a quien lo consume.

```go
// internal/identity/user_repository.go
package identity

import "context"

// UserRepository es el puerto de persistencia que comparten RegisterUser y ChangeEmail.
type UserRepository interface {
	FindByID(ctx context.Context, id UserID) (*User, error)
	ExistsByEmail(ctx context.Context, email Email) (bool, error)
	Save(ctx context.Context, u *User) error
}
```

## 4. Casos de uso — `register_user.go`, `change_email.go`

Un caso de uso = un tipo con el nombre de la acción + un archivo con ese nombre. Cada uno tiene constructor `NewX`, un único método `Execute(ctx, XInput) (XOutput, error)` y sus DTO al lado (la columna de **transformación**, sin capa `mapper/`). `Execute` es la columna de **integración**: orquesta VOs, entity y repositorio. Los DTO hablan primitivos amigables al transporte, no value objects.

```go
// internal/identity/register_user.go
package identity

import (
	"context"
	"fmt"
	"time"
)

// IDGenerator produce identidades nuevas; solo RegisterUser lo necesita.
type IDGenerator interface {
	NewUserID() UserID
}

type RegisterUserInput struct {
	Email string
	Name  string
}

type RegisterUserOutput struct {
	ID    string
	Email string
}

// RegisterUser da de alta un usuario con un email que nadie más use.
type RegisterUser struct {
	users UserRepository
	ids   IDGenerator
	now   func() time.Time
}

// NewRegisterUser usa time.Now si now es nil.
func NewRegisterUser(users UserRepository, ids IDGenerator, now func() time.Time) *RegisterUser {
	if now == nil {
		now = time.Now
	}
	return &RegisterUser{users: users, ids: ids, now: now}
}

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

```go
// internal/identity/change_email.go
package identity

import (
	"context"
	"fmt"
)

type ChangeEmailInput struct {
	UserID string
	Email  string
}

type ChangeEmailOutput struct {
	ID    string
	Email string
}

// ChangeEmail cambia el email de un usuario existente si nadie más lo usa.
type ChangeEmail struct {
	users UserRepository
}

func NewChangeEmail(users UserRepository) *ChangeEmail {
	return &ChangeEmail{users: users}
}

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

`Execute` no lleva doc comment: el tipo ya dice qué hace y el método es el único que tiene. Si mañana aparece "desactivar usuario", es un archivo nuevo (`deactivate_user.go`) con su tipo `DeactivateUser`; no un método más en un struct compartido.

## 5. Infraestructura — `memstore/memstore.go`

Implementación en memoria de ambos puertos. Los `var _ identity.UserRepository = (*Store)(nil)` son chequeos en compilación: si `Store` deja de satisfacer el puerto, no compila. En producción aquí iría un `pgstore` traduciendo errores del driver a errores de dominio en la frontera.

```go
// internal/identity/memstore/memstore.go
// Package memstore implementa en memoria los puertos de identity.
package memstore

import (
	"context"
	"strconv"
	"sync"

	"identityexample/internal/identity"
)

// Store es seguro para uso concurrente.
type Store struct {
	mu     sync.Mutex
	byID   map[identity.UserID]*identity.User
	emails map[string]identity.UserID
	seq    int64
}

func New() *Store {
	return &Store{
		byID:   make(map[identity.UserID]*identity.User),
		emails: make(map[string]identity.UserID),
	}
}

var (
	_ identity.UserRepository = (*Store)(nil)
	_ identity.IDGenerator    = (*Store)(nil)
)

func (s *Store) NewUserID() identity.UserID {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	return identity.UserID("u" + strconv.FormatInt(s.seq, 10))
}

func (s *Store) FindByID(_ context.Context, id identity.UserID) (*identity.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.byID[id]
	if !ok {
		return nil, identity.ErrUserNotFound
	}
	return u, nil
}

func (s *Store) ExistsByEmail(_ context.Context, email identity.Email) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.emails[email.String()]
	return ok, nil
}

func (s *Store) Save(_ context.Context, u *identity.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for email, id := range s.emails {
		if id == u.ID() {
			delete(s.emails, email)
		}
	}
	s.byID[u.ID()] = u
	s.emails[u.Email().String()] = u.ID()
	return nil
}
```

## 6. Transporte — `identityhttp/`

Un archivo por endpoint, con el nombre del caso de uso que expone. Cada uno traduce HTTP ↔ `Input`/`Output` con structs request/response propios; no hay `mapper/`. `routes.go` monta los endpoints y `responses.go` concentra el mapeo error de dominio → status.

```go
// internal/identity/identityhttp/routes.go
// Package identityhttp expone los casos de uso de identity por HTTP.
package identityhttp

import (
	"net/http"

	"identityexample/internal/identity"
)

type UseCases struct {
	RegisterUser *identity.RegisterUser
	ChangeEmail  *identity.ChangeEmail
}

func Mount(mux *http.ServeMux, uc UseCases) {
	mux.HandleFunc("POST /users", registerUser(uc.RegisterUser))
	mux.HandleFunc("PUT /users/{id}/email", changeEmail(uc.ChangeEmail))
}
```

```go
// internal/identity/identityhttp/register_user.go
package identityhttp

import (
	"encoding/json"
	"net/http"

	"identityexample/internal/identity"
)

type registerUserRequest struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

type registerUserResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

func registerUser(uc *identity.RegisterUser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
	}
}
```

```go
// internal/identity/identityhttp/change_email.go
package identityhttp

import (
	"encoding/json"
	"net/http"

	"identityexample/internal/identity"
)

type changeEmailRequest struct {
	Email string `json:"email"`
}

type changeEmailResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

func changeEmail(uc *identity.ChangeEmail) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req changeEmailRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		out, err := uc.Execute(r.Context(), identity.ChangeEmailInput{UserID: r.PathValue("id"), Email: req.Email})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, changeEmailResponse{ID: out.ID, Email: out.Email})
	}
}
```

```go
// internal/identity/identityhttp/responses.go
package identityhttp

import (
	"encoding/json"
	"errors"
	"net/http"

	"identityexample/internal/identity"
)

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeError nunca devuelve el texto de un error desconocido: podría filtrar internals.
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

Único lugar que conoce todos los paquetes a la vez: construye la infra, la inyecta en cada caso de uso y monta el transporte.

```go
// cmd/identityd/main.go
package main

import (
	"log"
	"net/http"

	"identityexample/internal/identity"
	"identityexample/internal/identity/identityhttp"
	"identityexample/internal/identity/memstore"
)

func main() {
	store := memstore.New()

	mux := http.NewServeMux()
	identityhttp.Mount(mux, identityhttp.UseCases{
		RegisterUser: identity.NewRegisterUser(store, store, nil),
		ChangeEmail:  identity.NewChangeEmail(store),
	})

	log.Fatal(http.ListenAndServe(":8080", mux))
}
```

## 8. Tests sin frameworks

Los tests siguen el mismo criterio de nombres: `register_user_test.go` prueba `RegisterUser`, `change_email_test.go` prueba `ChangeEmail`. El fake del puerto compartido vive en `fake_user_repository_test.go` y se escribe a mano —sin mocks generados— porque la interfaz es pequeña y la define el consumidor.

```go
// internal/identity/fake_user_repository_test.go
package identity_test

import (
	"context"

	"identityexample/internal/identity"
)

type fakeUserRepository struct {
	byID  map[identity.UserID]*identity.User
	taken map[string]bool
	saved *identity.User
}

func newFakeUserRepository(users ...*identity.User) *fakeUserRepository {
	f := &fakeUserRepository{byID: map[identity.UserID]*identity.User{}, taken: map[string]bool{}}
	for _, u := range users {
		f.byID[u.ID()] = u
		f.taken[u.Email().String()] = true
	}
	return f
}

func (f *fakeUserRepository) FindByID(_ context.Context, id identity.UserID) (*identity.User, error) {
	u, ok := f.byID[id]
	if !ok {
		return nil, identity.ErrUserNotFound
	}
	return u, nil
}

func (f *fakeUserRepository) ExistsByEmail(_ context.Context, e identity.Email) (bool, error) {
	return f.taken[e.String()], nil
}

func (f *fakeUserRepository) Save(_ context.Context, u *identity.User) error {
	f.saved = u
	return nil
}
```

```go
// internal/identity/register_user_test.go
package identity_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"identityexample/internal/identity"
)

type fixedID struct{}

func (fixedID) NewUserID() identity.UserID { return "u1" }

func TestRegisterUser(t *testing.T) {
	repo := newFakeUserRepository()
	uc := identity.NewRegisterUser(repo, fixedID{}, func() time.Time { return time.Unix(0, 0) })

	out, err := uc.Execute(context.Background(), identity.RegisterUserInput{Email: "Ada@Example.com", Name: "Ada Lovelace"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if out.Email != "ada@example.com" {
		t.Errorf("email should be normalized, got %q", out.Email)
	}
	if repo.saved == nil || repo.saved.ID() != "u1" {
		t.Errorf("user not saved with expected id")
	}
}

func TestRegisterUser_InvalidEmail(t *testing.T) {
	uc := identity.NewRegisterUser(newFakeUserRepository(), fixedID{}, nil)
	_, err := uc.Execute(context.Background(), identity.RegisterUserInput{Email: "nope", Name: "Ada"})
	if !errors.Is(err, identity.ErrInvalidEmail) {
		t.Fatalf("want ErrInvalidEmail, got %v", err)
	}
}

func TestRegisterUser_EmailTaken(t *testing.T) {
	repo := newFakeUserRepository()
	repo.taken["ada@example.com"] = true
	uc := identity.NewRegisterUser(repo, fixedID{}, nil)
	_, err := uc.Execute(context.Background(), identity.RegisterUserInput{Email: "ada@example.com", Name: "Ada"})
	if !errors.Is(err, identity.ErrEmailTaken) {
		t.Fatalf("want ErrEmailTaken, got %v", err)
	}
}
```

```go
// internal/identity/change_email_test.go
package identity_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"identityexample/internal/identity"
)

func existingUser(t *testing.T, id identity.UserID, email string) *identity.User {
	t.Helper()
	e, err := identity.NewEmail(email)
	if err != nil {
		t.Fatal(err)
	}
	n, err := identity.NewName("Ada Lovelace")
	if err != nil {
		t.Fatal(err)
	}
	u, err := identity.NewUser(id, e, n, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestChangeEmail(t *testing.T) {
	repo := newFakeUserRepository(existingUser(t, "u1", "ada@example.com"))
	uc := identity.NewChangeEmail(repo)

	out, err := uc.Execute(context.Background(), identity.ChangeEmailInput{UserID: "u1", Email: "ADA@new.com"})
	if err != nil {
		t.Fatalf("change email: %v", err)
	}
	if out.Email != "ada@new.com" || repo.saved == nil || repo.saved.Email().String() != "ada@new.com" {
		t.Errorf("email not changed: out=%+v saved=%v", out, repo.saved)
	}
}

func TestChangeEmail_Taken(t *testing.T) {
	repo := newFakeUserRepository(
		existingUser(t, "u1", "ada@example.com"),
		existingUser(t, "u2", "grace@example.com"),
	)
	_, err := identity.NewChangeEmail(repo).Execute(context.Background(), identity.ChangeEmailInput{UserID: "u1", Email: "grace@example.com"})
	if !errors.Is(err, identity.ErrEmailTaken) {
		t.Fatalf("want ErrEmailTaken, got %v", err)
	}
}

func TestChangeEmail_UserNotFound(t *testing.T) {
	_, err := identity.NewChangeEmail(newFakeUserRepository()).Execute(context.Background(), identity.ChangeEmailInput{UserID: "nope", Email: "a@b.com"})
	if !errors.Is(err, identity.ErrUserNotFound) {
		t.Fatalf("want ErrUserNotFound, got %v", err)
	}
}
```

## Qué NO tiene, a propósito

- **Sin `service.go` ni `usecase.go`**: cada caso de uso es su propio tipo en su propio archivo.
- **Sin `handler.go` con todos los endpoints**: un archivo por endpoint + `routes.go`.
- **Sin carpetas `domain/`, `application/`, `infrastructure/`**: un paquete por contexto.
- **Sin capa `mapper/`**: los DTO viven junto al caso de uso; la traducción está en cada endpoint.
- **Sin interfaz-por-clase**: solo `UserRepository` e `IDGenerator`, definidas donde se consumen.
- **Sin aggregate de más**: `User` es a la vez entity y root; un aggregate se justifica por una invariante transaccional real, no por ceremonia.
- **Sin getters/setters ni comentarios que narran**: métodos con intención y doc comments de una línea solo donde el nombre no basta.

## Cómo correrlo

```bash
go test ./...            # los tests de los casos de uso pasan sin infraestructura
go build ./...           # compila el binario
go vet ./...             # sin observaciones
go run ./cmd/identityd   # levanta el servidor en :8080
# curl -X POST localhost:8080/users -d '{"email":"ada@example.com","name":"Ada Lovelace"}'
# curl -X PUT localhost:8080/users/u1/email -d '{"email":"ada@new.com"}'
```

## Checklist

- [ ] El paquete de dominio no importa `database/sql`, `net/http` ni libs externas (test de pureza)
- [ ] Cada VO se valida en su constructor y es inmutable
- [ ] La entity valida la INTEGRACIÓN de VOs; no re-valida lo que cada VO ya valida
- [ ] El aggregate root muta su interior solo por métodos propios (campos no-exportados)
- [ ] Cada caso de uso es un tipo propio en un archivo con su nombre (`register_user.go` → `RegisterUser`)
- [ ] Ningún archivo contiene dos casos de uso; no existen `service.go`, `usecase.go` ni `handler.go`
- [ ] Los puertos se definen en el consumidor: en el archivo del caso de uso, o en su propio archivo si son compartidos
- [ ] La infra traduce errores del driver a errores de dominio en la frontera
- [ ] Los DTO `XInput`/`XOutput` viven junto al caso de uso; no hay capa `mapper/`
- [ ] Los errores de dominio se mapean a status HTTP solo en el transporte
- [ ] El wiring vive en `cmd/`; el resto no conoce `main`
