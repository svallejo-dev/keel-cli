# Application Layer — orquestación en DDD idiomático

La capa de aplicación es la costura entre el mundo de afuera (HTTP, colas, cron) y el dominio puro. No contiene reglas de negocio: las **orquesta**. Aquí viven los casos de uso, las interfaces de repository que necesitan, y la traducción entre formatos externos y tipos de dominio.

> **Regla #0 — go-idiomatic gana.** Ante un conflicto entre un patrón DDD y el Go idiomático, gana Go. Nada de Java-ismos: sin capa `mapper/`, sin una interfaz por cada struct, sin `AbstractServiceFactory`. Un caso de uso es un tipo concreto en su propio archivo; un DTO es un struct; una interfaz se define donde se consume. Si un patrón "canónico" de DDD te empuja a escribir Go que un senior rechazaría en review, el patrón está mal traducido.

Este archivo no redefine los building blocks tácticos: `Email` es un value object (ver [value-objects.md](value-objects.md)), `User` es una entity (ver [entities.md](entities.md)), y una raíz transaccional es un aggregate (ver [aggregates.md](aggregates.md)). Aquí se muestra cómo la aplicación los **usa**. Los ejemplos viven en el contexto `identity`: dominio y casos de uso comparten paquete, la infra (`pgstore`) y el transporte (`identityhttp`) son sub-paquetes.

---

## 1. Negocio vs Aplicación: qué es puro y qué orquesta

La lógica de **negocio** (entities + value objects) es pura: valida invariantes, calcula, decide. No sabe de `context.Context`, de SQL ni de HTTP. La lógica de **aplicación** orquesta: recibe input externo, construye tipos de dominio, consulta repositories, coordina un aggregate y devuelve output.

| Rol | Dónde vive | Responsabilidad | Prohibido importar |
|---|---|---|---|
| Dominio | `identity/user.go`, `email.go`… | invariantes, cálculo, VOs/entities | `database/sql`, `net/http`, drivers |
| Caso de uso | `identity/register_user.go`… | orquestar dominio + puertos | `net/http`, drivers concretos |
| Infra / transporte | `identity/pgstore`, `identity/identityhttp` | implementar puertos, traducir | — |

Dominio y casos de uso comparten paquete: la separación es **por archivo**, no por carpeta `domain/` + `app/` (ver [bounded-contexts.md](bounded-contexts.md)).

### El test de pureza es una verificación de arquitectura real

El `import` del paquete del contexto **no** debe contener `database/sql`, `net/http` ni libs externas de infra. Si aparecen, la infraestructura se filtró hacia el núcleo. Esto no es una guía de estilo: es un check ejecutable que puedes poner en CI.

```bash
go list -deps ./internal/identity | grep -E 'database/sql|net/http|jackc|redis' \
  && echo "FUGA: infraestructura en el dominio" && exit 1 || echo "dominio puro"
```

`go list -deps` expande el cierre transitivo, así que también atrapa fugas indirectas (un VO que importa un helper que importa `pgx`). Fíjate en que se lista el paquete del contexto, no `./internal/identity/...`: los sub-paquetes de infra sí importan drivers, y está bien.

---

## 2. Un caso de uso = un tipo + un archivo

En Go un caso de uso es **un tipo concreto con el nombre de la acción**, en un archivo con ese mismo nombre: `RegisterUser` en `register_user.go`, `Login` en `login.go`, `SearchUsers` en `search_users.go`. No es un método más de un `Service` que agrupa todo el contexto, ni un árbol `UseCase → AbstractUseCase → Command`.

La razón es semántica: el nombre de un archivo debe decir qué hace. `service.go` no dice nada, y un `Service` con `Login()` y `Signup()` mezcla dos intenciones, dos sets de dependencias y dos razones para cambiar en el mismo lugar. Con un tipo por caso de uso:

- El árbol del paquete es el índice de lo que el contexto sabe hacer.
- Cada caso de uso declara **solo** las dependencias que usa.
- Añadir un caso de uso es añadir un archivo; nunca editar uno ajeno.

```go
// internal/identity/register_user.go
package identity

type RegisterUserInput struct {
	Email string
}

type RegisterUserOutput struct {
	ID    string
	Email string
}

// RegisterUser da de alta un usuario con un email que nadie más use.
type RegisterUser struct {
	users UserRepository
	ids   IDGenerator
}

func NewRegisterUser(users UserRepository, ids IDGenerator) *RegisterUser {
	return &RegisterUser{users: users, ids: ids}
}

func (uc *RegisterUser) Execute(ctx context.Context, in RegisterUserInput) (RegisterUserOutput, error) {
	email, err := NewEmail(in.Email)
	if err != nil {
		return RegisterUserOutput{}, err
	}

	exists, err := uc.users.ExistsByEmail(ctx, email)
	if err != nil {
		return RegisterUserOutput{}, fmt.Errorf("register user: check email: %w", err)
	}
	if exists {
		return RegisterUserOutput{}, ErrEmailTaken
	}

	u := NewUser(uc.ids.NewUserID(), email)
	if err := uc.users.Save(ctx, u); err != nil {
		return RegisterUserOutput{}, fmt.Errorf("register user: save: %w", err)
	}
	return RegisterUserOutput{ID: string(u.ID()), Email: u.Email().String()}, nil
}
```

La forma canónica es siempre la misma:

| Pieza | Forma |
|---|---|
| Archivo | `<accion>.go` en snake_case: `register_user.go` |
| Tipo | `type RegisterUser struct{ ...dependencias }` |
| Constructor | `func NewRegisterUser(deps...) *RegisterUser` |
| Método | un único exportado: `Execute(ctx context.Context, in RegisterUserInput) (RegisterUserOutput, error)` |
| DTO | `RegisterUserInput` / `RegisterUserOutput`, en el mismo archivo |

El caso de uso **conoce** el repository; el dominio **no**. La construcción de invariantes (`NewEmail`) vive en el dominio; el caso de uso solo la invoca y decide el flujo.

Nombres prohibidos dentro de un contexto, porque no dicen qué hace el archivo o porque agrupan varias acciones:

| Prohibido | Por qué | En su lugar |
|---|---|---|
| `service.go`, `services.go` + `type Service` | cajón de casos de uso | `register_user.go` + `type RegisterUser` |
| `usecase.go`, `usecases.go`, `use_cases.go` + `type UseCase` | nombra la capa, no la acción | un archivo por caso de uso |
| `application.go`, `app.go` | nombra la capa | el nombre de la acción |
| `manager.go` + `type Manager` | "gestiona" no es una intención | el nombre de la acción |
| `helpers.go`, `utils.go`, `common.go` | cajón sin concepto | el concepto que contiene |
| `handler.go`/`handlers.go` con varios endpoints | mezcla casos de uso en el transporte | un archivo por endpoint + `routes.go` |

Lo que esta forma **no** es: CQRS ceremonial. No hay `RegisterUserCommand` + `RegisterUserHandler` + bus + interfaz `UseCase[I, O]` genérica. Un caso de uso es un struct concreto con un método; el wiring lo construye y el transporte lo llama directamente.

---

## 3. La interfaz de repository es consumer-defined

**La interfaz de repository se define en quien la usa (el caso de uso), no en quien la implementa (infra).** Esto es exactamente el "accept interfaces, return structs" de Go y, a la vez, la inversión de dependencias de Clean Architecture: son la misma idea. El contexto declara qué necesita; `pgstore` lo cumple.

Dónde se declara depende de cuántos casos de uso la consumen:

- **Solo uno** → en el archivo de ese caso de uso, con el método justo que necesita. `IDGenerator` vive en `register_user.go`.
- **Varios** → en su propio archivo, con el nombre del puerto: `user_repository.go` → `type UserRepository interface`.

```go
// internal/identity/user_repository.go
package identity

// UserRepository lo comparten RegisterUser y ChangeEmail.
type UserRepository interface {
	FindByID(ctx context.Context, id UserID) (*User, error)
	ExistsByEmail(ctx context.Context, email Email) (bool, error)
	Save(ctx context.Context, u *User) error
}
```

La interfaz es **estrecha**: solo los métodos que sus consumidores usan. No un `Repository` gigante con veinte métodos que ninguna implementación necesita entera. Si un caso de uso solo lee, puede declarar su propia interfaz de un método en su archivo aunque exista `UserRepository`.

### La implementación traduce errores de driver en la frontera

La impl de Postgres vive en un sub-paquete (`pgstore`) y **nunca deja salir un `*pgconn.PgError`**. En la frontera traduce el error del driver a un error de dominio: un `23505` (unique violation) se convierte en `identity.ErrEmailTaken`; un `pgx.ErrNoRows` se convierte en `identity.ErrUserNotFound`.

```go
// internal/identity/pgstore/pgstore.go
var _ identity.UserRepository = (*Store)(nil)

const uniqueViolation = "23505"

func (s *Store) Save(ctx context.Context, u *identity.User) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO users (id, email) VALUES ($1, $2)`,
		u.ID(), u.Email().String())

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return identity.ErrEmailTaken
	}
	if err != nil {
		return fmt.Errorf("pgstore: save user: %w", err)
	}
	return nil
}

func (s *Store) FindByID(ctx context.Context, id identity.UserID) (*identity.User, error) {
	var raw string
	err := s.pool.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, id).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, identity.ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("pgstore: find user %s: %w", id, err)
	}
	email, err := identity.NewEmail(raw)
	if err != nil {
		return nil, fmt.Errorf("pgstore: corrupt email for user %s: %w", id, err)
	}
	return identity.NewUser(id, email), nil
}
```

```go
// BAD: el error del driver se escapa; el caso de uso queda acoplado a pgconn.
func (s *Store) Save(ctx context.Context, u *identity.User) error {
	_, err := s.pool.Exec(ctx, `INSERT ...`, u.ID(), u.Email().String())
	return err
}

// GOOD: la frontera traduce; el caso de uso solo ve vocabulario de dominio.
if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
	return identity.ErrEmailTaken
}
```

El compilador verifica la dirección con `var _ identity.UserRepository = (*Store)(nil)`: `pgstore` depende de `identity`, nunca al revés. No hay una interfaz `IUserRepository` duplicada junto a la impl: eso es interfaz-por-clase, un Java-ismo (ver [antipatterns.md](antipatterns.md)).

---

## 4. La columna de transformación: DTO sin ceremonia

Los structs `XInput`/`XOutput` del caso de uso **son** los DTO. Se definen en el mismo archivo que el caso de uso, no en un paquete `dto/` ni `mapper/`. Llevan el nombre del caso de uso como prefijo porque comparten paquete con los demás: `RegisterUserInput`, `ChangeEmailInput`.

La traducción `HTTP request → Input` y `Output → HTTP response` es una función **delgada** en el endpoint. Sin auto-mappers (reflection + una dependencia + errores en runtime), sin una capa `mapper/` que solo copia campos.

```go
// BAD: capa mapper + auto-mapper. Ceremonia, reflection, y un panic esperando.
out := mapper.Map[RegisterUserOutput](entity)

// GOOD: copia explícita de dos campos. Legible, sin dependencias, sin reflection.
resp := registerUserResponse{ID: out.ID, Email: out.Email}
```

Si el mapeo es trivial (copiar campos), escríbelo a mano. Si es no-trivial (aplanar un aggregate, formatear fechas), sigue siendo una función normal con un nombre — no un framework. Reservar `mapper/` para "por si crece" es diseño especulativo.

---

## 5. Errores de dominio → status HTTP en la frontera

El mapeo de error de dominio a status HTTP ocurre en la **frontera** (el transporte), con `errors.Is`/`errors.As`. El dominio expone errores centinela (`ErrInvalidEmail`, `ErrUserNotFound`, `ErrEmailTaken`); el transporte los traduce a 400/404/409. Lo desconocido cae a 500 **sin filtrar detalles internos**.

| Error de dominio | Status | Semántica |
|---|---|---|
| `ErrInvalidEmail` | 400 Bad Request | input inválido |
| `ErrUserNotFound` | 404 Not Found | recurso ausente |
| `ErrEmailTaken` | 409 Conflict | violación de invariante de unicidad |
| (cualquier otro) | 500 Internal | fallo no clasificado, mensaje genérico |

El transporte sigue la misma regla de nombres que el contexto: un archivo por endpoint con el nombre del caso de uso que expone, un `routes.go` que los monta, y el mapeo de errores en un solo lugar.

```
internal/identity/identityhttp/
├── routes.go          # Mount(mux, UseCases)
├── register_user.go   # POST /users
├── change_email.go    # PUT /users/{id}/email
└── responses.go       # writeJSON + writeError
```

```go
// internal/identity/identityhttp/register_user.go
func registerUser(uc *identity.RegisterUser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req registerUserRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "cuerpo inválido", http.StatusBadRequest)
			return
		}
		out, err := uc.Execute(r.Context(), identity.RegisterUserInput{Email: req.Email})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, registerUserResponse{ID: out.ID, Email: out.Email})
	}
}
```

```go
// internal/identity/identityhttp/responses.go

// writeError nunca devuelve el texto de un error desconocido: filtraría SQL, hosts o stack.
func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, identity.ErrInvalidEmail):
		http.Error(w, "email inválido", http.StatusBadRequest)
	case errors.Is(err, identity.ErrUserNotFound):
		http.Error(w, "usuario no encontrado", http.StatusNotFound)
	case errors.Is(err, identity.ErrEmailTaken):
		http.Error(w, "email ya registrado", http.StatusConflict)
	default:
		http.Error(w, "error interno", http.StatusInternalServerError)
	}
}
```

Usa `errors.Is` para centinelas y `errors.As` cuando necesitas datos del error (p. ej. un `*ValidationError` con el campo que falló). El `default` que devuelve el mensaje del error interno es una fuga de información: siempre un texto genérico y el detalle al log.

---

## 6. El flujo completo y quién conoce a quién

```
   HTTP request
        │  decode + validación de forma (identityhttp/register_user.go)
        ▼
   RegisterUserInput ──────────► RegisterUser.Execute (identity/register_user.go)
                                    │  orquesta:
                                    │   1. NewEmail(in.Email)           ← dominio (VO)
                                    │   2. users.ExistsByEmail(...)     ← UserRepository
                                    │   3. NewUser(id, email)           ← dominio (entity)
                                    │   4. users.Save(ctx, u)           ← UserRepository
                                    ▼
                                RegisterUserOutput
        │  Output → response                   │  error → writeError → 4xx/5xx
        ▼                                       ▼
   HTTP response
```

Dirección de dependencias, siempre hacia adentro:

```
cmd/  ──►  identityhttp ──►  identity   (define UserRepository, IDGenerator)
                                 ▲
           pgstore ──────────────┘      (implementa UserRepository)
```

- **Casos de uso → dominio**: viven en el mismo paquete; usan VOs/entities y devuelven sus errores.
- **Infra y transporte → contexto**: `pgstore` implementa sus puertos e `identityhttp` llama a sus casos de uso.
- **El contexto no conoce a nadie de afuera**: `identity` no importa `pgstore`, `identityhttp` ni `net/http`. Las flechas nunca vuelven; un ciclo lo rechaza el compilador, y eso es una feature.

El wiring vive en `cmd/` y es lo único que conoce todos los paquetes a la vez. Construye cada caso de uso por separado:

```go
store := pgstore.New(pool)
identityhttp.Mount(mux, identityhttp.UseCases{
	RegisterUser: identity.NewRegisterUser(store, uuidgen.New()),
	ChangeEmail:  identity.NewChangeEmail(store),
})
```

Los bounded contexts (ver [bounded-contexts.md](bounded-contexts.md)) se ensamblan aquí: cada uno expone sus casos de uso, y `cmd/` los cablea sin que se importen entre sí. El ejemplo completo y compilable está en [worked-example.md](worked-example.md).

---

## Checklist

- [ ] El `import` del paquete del contexto no contiene `database/sql`, `net/http` ni drivers — verificado con `go list -deps` en CI.
- [ ] Cada caso de uso es un tipo propio (`RegisterUser`) en un archivo con su nombre (`register_user.go`), con `NewRegisterUser` y un único método `Execute(ctx, RegisterUserInput) (RegisterUserOutput, error)`.
- [ ] Ningún archivo contiene dos casos de uso; no hay `service.go`, `usecase.go`, `app.go`, `manager.go` ni tipos `Service`/`UseCase`/`Manager` que agrupen operaciones.
- [ ] Sin jerarquías Command/Handler/`UseCase[I, O]`: un struct concreto con un método.
- [ ] Los puertos se declaran en el consumidor: en el archivo del caso de uso si solo él los usa, en `<puerto>.go` si son compartidos. La impl vive en un sub-paquete de infra.
- [ ] La implementación traduce errores de driver (`pgconn.PgError`, `pgx.ErrNoRows`) a errores de dominio en la frontera; nunca los deja salir.
- [ ] Los DTO son `XInput`/`XOutput` junto al caso de uso: sin capa `mapper/`, sin auto-mappers por reflection.
- [ ] El transporte tiene un archivo por endpoint + `routes.go`; el mapeo error → status está centralizado; lo desconocido → 500 con mensaje genérico.
- [ ] `var _ identity.UserRepository = (*Store)(nil)` verifica en compilación que la infra satisface el puerto.
- [ ] Las dependencias forman un DAG hacia adentro; el contexto no importa a nadie; el wiring vive solo en `cmd/`.
