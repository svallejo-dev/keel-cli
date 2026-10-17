# Anti-patrones: DDD táctico que rompe en Go

DDD es un vocabulario de diseño, no un dialecto de Java. La mayoría de los "patrones tácticos" que circulan como canónicos son convenciones de Java/Spring o C#/.NET trasplantadas: en Go compilan, pero pelean contra el lenguaje (ciclos de import, interface pollution, allocaciones, tipos huérfanos). Este archivo cataloga esos Java/C#-ismos y su forma idiomática. Toolchain objetivo: Go 1.26 estándar (pin 1.24.7). Los `// BAD` son Go válido cuyo defecto es de diseño, salvo donde se marque lo contrario.

## Regla #0 — Ante conflicto DDD ↔ Go idiomático, gana Go

Es el corazón de este documento. Cuando la ortodoxia DDD ("una interfaz por repositorio", "capas domain/application/infrastructure", "value objects con getters") choca con lo que escribiría un principal engineer de Go, se sigue Go. Cada anti-patrón de abajo es un caso concreto de esa regla: el "patrón" viene de un lenguaje con herencia, reflection ubicua y namespaces jerárquicos, y Go no tiene nada de eso. DDD aporta los conceptos (aggregate, bounded context, invariante); Go dicta la implementación. Si necesitas citar a Evans en vez de `go doc` para justificar tu layout, ya te desviaste.

---

## 1. Carpetas por capa → paquete por contexto, archivo por rol

Carpetas top-level `domain/`+`application/`+`infrastructure/` con `models/`+`repositories/`+`services/` dentro. En Go un paquete es unidad de diseño, no namespace: partir un concepto en capas crea el modelo anémico (`models.Order` no lo posee nadie) y, en cuanto el dominio necesita una firma de la infra, un **ciclo de import** que el compilador rechaza. Fix: un paquete por bounded context (ver `references/bounded-contexts.md`); dentro, un archivo por rol, con el tipo, sus invariantes y su persistencia en el mismo paquete.

```
// BAD — capas técnicas; Order huérfano y domain↔infra tiende al ciclo
internal/domain/models/order.go        internal/domain/repositories/order_repo.go
internal/application/services/order_service.go   internal/infrastructure/order_pg.go
```

```go
// GOOD — internal/order/order.go: tipo + invariante + store en un paquete
package order

type Order struct {
	id    ID
	lines []Line
}

func New(id ID, lines []Line) (*Order, error) {
	if len(lines) == 0 {
		return nil, ErrEmpty // sentinela del paquete
	}
	return &Order{id: id, lines: lines}, nil
}

type Store interface{ Save(o *Order) error } // la persistencia, en el mismo paquete
```

## 2. Una interfaz por clase → interfaz en el consumidor

Un `port` + su `adapter` por cada colaborador, aunque haya una sola impl. Las interfaces en Go se satisfacen implícitamente y se declaran en el punto de uso; una interfaz con una única impl es **interface pollution**: mete indirección (bloquea inlining), una allocación y un salto de archivo para nada. Fix: la impl es un struct concreto exportado; el consumidor declara la interfaz mínima que necesita (ver `references/application-layer.md`).

```go
// BAD — package smtp: port declarado junto a su ÚNICA impl
type EmailSender interface {
	Send(to, subject, body string) error
}

func (c *Client) Send(to, subject, body string) error { return nil }

// GOOD — welcome/send_welcome.go: el caso de uso define la interfaz mínima que usa
type sender interface {
	Send(to, subject, body string) error
}

type SendWelcome struct{ mail sender }

func NewSendWelcome(m sender) *SendWelcome { return &SendWelcome{mail: m} }
```

## 3. Getters/Setters → métodos con intención

`GetX()`/`SetX(v)` estilo JavaBean. El estilo Go no usa `Get` (el accessor de `email` es `Email()`), y un setter genérico deja a la entidad sin proteger invariantes (`SetActive(true)` admite estados imposibles). Fix: accessor sin `Get`; cada setter se reemplaza por un método que nombra la transición y valida (ver `references/entities.md`).

```go
// BAD — JavaBean; el setter admite estados inválidos
func (u *User) GetEmail() string  { return u.email }
func (u *User) SetEmail(e string) { u.email = e }
func (u *User) SetActive(b bool)  { u.active = b }

// GOOD — sin Get; la mutación tiene nombre y preserva la invariante
func (u *User) Email() string { return u.email }

func (u *User) ChangeEmail(e string) error {
	if !strings.Contains(e, "@") {
		return ErrBadEmail
	}
	u.email = e
	return nil
}

func (u *User) Deactivate() { u.active = false } // no SetActive(false)
```

## 4. Modelo anémico → rich model

Structs de datos con campos públicos y la lógica en un `XxxService` gordo que los edita desde fuera. Con campos exportados nadie garantiza las invariantes (`o.Total = -1`) y el service acumula reglas que pertenecen al tipo. En Go el encapsulamiento es por paquete: los campos privados solo se mantienen coherentes si el comportamiento vive con ellos. Fix: la lógica vive con los datos.

```go
// BAD — datos públicos + service que edita las tripas ajenas
type Order struct {
	Lines []string
	Total int
}

func (OrderService) AddLine(o *Order, sku string, price int) {
	o.Lines = append(o.Lines, sku)
	o.Total += price
}
```

```go
// GOOD — el comportamiento (y los campos privados) viven con el tipo
func (o *Order) AddLine(sku string, price int) error {
	if price < 0 {
		return ErrNegative
	}
	o.lines = append(o.lines, sku)
	o.total += price
	return nil
}
```

## 5. `mapper/` + auto-mapping por reflection → traducción a mano

Un paquete `mapper/` o una librería tipo AutoMapper que copia structs por reflection. El compilador no ve reflection: renombrar un campo rompe el mapeo en runtime, no en el build, y pagas coste de reflection por request. Fix: structs `XInput`/`response` junto al caso de uso y traducción explícita que revisa el compilador (ver `references/application-layer.md`).

```go
// BAD — automapper por reflection; el build no detecta un rename
func Map(src, dst any) {
	sv, dv := reflect.ValueOf(src).Elem(), reflect.ValueOf(dst).Elem()
	for i := 0; i < sv.NumField(); i++ {
		if f := dv.FieldByName(sv.Type().Field(i).Name); f.IsValid() && f.CanSet() {
			f.Set(sv.Field(i))
		}
	}
}

// GOOD — struct de salida junto al endpoint + traducción explícita
type response struct {
	ID    string `json:"id"`
	Total int    `json:"total"`
}

func toResponse(o *Order) response {
	return response{ID: o.ID(), Total: o.Total()}
}
```

## 6. Abstract Factory / Builder GoF → constructor idiomático

`OrderBuilder{}.WithID(..).WithTotal(..).Build()` o factorías abstractas. El Builder fluido de GoF existe porque Java no tiene retorno múltiple; Go sí, así que `New() (X, error)` valida y devuelve el error en el sitio, mientras el Builder difiere la validación hasta `Build()` y admite objetos a medio construir. Fix: `NewX(...) (X, error)`, o functional options si hay muchos opcionales; nunca un Builder mutable.

```go
// BAD — Builder GoF; la validación se difiere o se pierde
func (b *OrderBuilder) WithID(id string) *OrderBuilder { b.o.id = id; return b }
func (b *OrderBuilder) Build() Order                   { return b.o }

// GOOD — constructor que valida y devuelve (valor, error)
func New(id string, total int) (Order, error) {
	if id == "" {
		return Order{}, ErrNoID
	}
	return Order{id: id, total: total}, nil
}
```

## 7. Repository que filtra errores del driver → error de dominio en la frontera

El repositorio devuelve `sql.ErrNoRows` o `*pgconn.PgError` tal cual. Filtrarlo acopla a cada caller al driver: la capa de aplicación acaba importando `database/sql`/`pgx` para hacer `errors.Is`. La frontera del repo es justo donde se traduce el error de infraestructura a uno de dominio. Fix: traducir en el método y exponer sentinelas de dominio (ver `references/aggregates.md`).

```go
// BAD — el error del driver escapa el paquete y acopla a los callers
func (r *Repo) FindByID(id string) (*Customer, error) {
	var c Customer
	err := r.db.QueryRow(query, id).Scan(&c.ID)
	return &c, err // sql.ErrNoRows sale tal cual
}
```

```go
// GOOD — traducción en la frontera a un error de dominio
var ErrNotFound = errors.New("customer: no encontrado")

func (r *Repo) FindByID(id string) (*Customer, error) {
	var c Customer
	switch err := r.db.QueryRow(query, id).Scan(&c.ID); {
	case errors.Is(err, sql.ErrNoRows):
		return nil, ErrNotFound
	case err != nil:
		return nil, fmt.Errorf("customer: find id=%s: %w", id, err)
	}
	return &c, nil
}
```

## 8. Referencia entre aggregates por puntero → por ID

`Order` contiene `Customer *Customer`. El puntero cruza la frontera transaccional: cargar un `Order` arrastra el grafo entero de `Customer`, invita a mutar otro aggregate en la misma transacción y crea aliasing compartido. Cada aggregate es su propia frontera de consistencia. Fix: guarda `CustomerID`; si necesitas los datos, cárgalos por su propio repositorio (ver `references/aggregates.md`).

```go
// BAD — puntero a otro aggregate: difumina la frontera y arrastra el grafo
type Order struct {
	ID       string
	Customer *Customer
}
```

```go
// GOOD — referencia por identidad; cada aggregate es su propia frontera
type CustomerID string

type Order struct {
	ID         string
	CustomerID CustomerID
}
```

## 9. `Repository[T any]` genérico → repos concretos

Una interfaz genérica `Repository[T]` con `Get/Save` "para no repetir" _(generics: Go 1.18+)_. Los aggregates no comparten operaciones: uno se busca por `FindOpen(customerID)`, otro por `FindByEmail`. `Repository[T].Get(id)` fuerza a todos al mínimo común denominador y esconde las consultas reales. Generics solo cuando la lógica es type-agnostic de verdad (contenedores, algoritmos); un repo no lo es. Fix: interfaz concreta por aggregate.

```go
// BAD — abstracción que no aporta y oculta las consultas reales
type Repository[T any] interface {
	Get(ctx context.Context, id string) (T, error)
	Save(ctx context.Context, entity T) error
}

// GOOD — repo concreto que nombra las operaciones del aggregate
type OrderRepository interface {
	FindOpen(ctx context.Context, customerID string) ([]*Order, error)
	Save(ctx context.Context, o *Order) error
}
```

## 10. Paquetes-cajón (`domain`, `vo`, `dto`, `util`, `common`) → nombres de concepto

Paquetes genéricos `domain/`, `entity/`, `vo/`, `dto/`, `util/`, `common/`. No se pueden describir en una frase que empiece por su nombre: son cajones, no conceptos. Fuerzan acoplamiento (`common` que todos importan) y arruinan los import paths: `vo.Money` no dice nada, `billing.Money` sí. En Go el nombre del paquete es parte del identificador. Fix: nombra por el bounded context o el concepto (ver `references/bounded-contexts.md`).

```
// BAD — cajones sin identidad          // GOOD — conceptos con nombre
internal/vo/money.go                     internal/billing/money.go
internal/common/util.go                  internal/pricing/discount.go
```

## 11. Herencia simulada con `BaseEntity` → composición con propósito

Un `BaseEntity` con `ID`/`CreatedAt`/`UpdatedAt`/`Touch()` embebido en cada tipo. Go no tiene herencia: el embedding es composición, no jerarquía, y no hay override ni dispatch dinámico. Un `BaseEntity` universal inyecta estado y métodos (`Touch`) en tipos que no los necesitan (un `Tag` inmutable no tiene `UpdatedAt`) e infla su API pública. Fix: embebe una pieza acotada (`Timestamps`) solo en los tipos que la usan (ver `references/entities.md`).

```go
// BAD — BaseEntity universal: mete Touch/UpdatedAt donde no toca
type BaseEntity struct {
	ID                   string
	CreatedAt, UpdatedAt time.Time
}

func (b *BaseEntity) Touch() { b.UpdatedAt = time.Now() }

type Tag struct {
	BaseEntity // un Tag no necesita UpdatedAt ni Touch
	Name       string
}

// GOOD — composición con propósito: solo los tipos auditables embeben Timestamps
type Timestamps struct {
	CreatedAt, UpdatedAt time.Time
}

func (t *Timestamps) Touch() { t.UpdatedAt = time.Now() }

type Order struct { // Order audita y la embebe; un Tag simplemente no lo haría
	ID string
	Timestamps
}
```

## 12. Value Object mutable / con setters → inmutable, crea-nuevo

Un `Money` con campos públicos y `SetAmount`/`Add` que mutan el receptor por puntero. Un value object se define por su valor: dos `Money(10,"EUR")` son intercambiables. Si es mutable y se comparte por puntero, `Add` cambia el importe bajo los pies de todos (aliasing). Fix: campos privados, sin setters, receptor por valor, cada operación devuelve un `Money` nuevo (ver `references/value-objects.md`).

```go
// BAD — VO mutable con setter; el Money compartido cambia para todos
func (m *Money) SetAmount(a int64) { m.Amount = a }
func (m *Money) Add(other Money)   { m.Amount += other.Amount }
```

```go
// GOOD — inmutable: receptor por valor, cada operación crea un valor nuevo
func (m Money) Add(other Money) (Money, error) {
	if m.currency != other.currency {
		return Money{}, ErrCurrency
	}
	return Money{amount: m.amount + other.amount, currency: m.currency}, nil
}

func (m Money) Amount() int64 { return m.amount }
```

## 13. Archivo/tipo cajón (`service.go`, `usecase.go`) → un archivo y un tipo por caso de uso

Un `service.go` (o `usecase.go`, `usecases.go`, `app.go`, `manager.go`) con un `type Service struct` que acumula `Login()`, `Signup()`, `ResetPassword()`… Es la versión Go del `UserService` de Spring: el nombre del archivo no dice qué hace, el struct suma las dependencias de todas las operaciones (cada test construye cinco colaboradores para probar una) y cada caso de uso nuevo edita el mismo archivo, así que todo cambio choca con todo. Que el código esté en un paquete por contexto no lo arregla: el cajón solo se movió un nivel abajo. Fix: un caso de uso = un tipo con el nombre de la acción + un archivo con ese nombre, con su constructor, un único método `Execute` y sus DTO (ver `references/application-layer.md`). Lo mismo aplica al transporte: un archivo por endpoint + `routes.go`, no un `handler.go` con todo.

```
// BAD — internal/identity/                // GOOD — internal/identity/
service.go     Login, Signup, Reset…         login.go            Login
handler.go     todos los endpoints           sign_up.go          SignUp
                                             reset_password.go   ResetPassword
                                             identityhttp/routes.go, login.go, sign_up.go…
```

```go
// BAD — service.go: dos intenciones, la unión de sus dependencias
type Service struct {
	users  UserRepository
	hasher Hasher
	tokens TokenIssuer
	mail   Mailer
}

func (s *Service) Login(ctx context.Context, in LoginInput) (LoginOutput, error)   { /* ... */ }
func (s *Service) SignUp(ctx context.Context, in SignUpInput) (SignUpOutput, error) { /* ... */ }
```

```go
// GOOD — login.go: el archivo, el tipo y sus dependencias dicen lo mismo
type Login struct {
	users  UserRepository
	hasher Hasher
	tokens TokenIssuer
}

func NewLogin(users UserRepository, hasher Hasher, tokens TokenIssuer) *Login {
	return &Login{users: users, hasher: hasher, tokens: tokens}
}

func (uc *Login) Execute(ctx context.Context, in LoginInput) (LoginOutput, error) { /* ... */ }
```

`SignUp` vive en `sign_up.go` con sus propias dependencias (`Mailer` incluido); `Login` ya no carga con ella.

---

## Tabla resumen: Java-ismo → Go idiomático

| Java/C#-ismo | Por qué rompe en Go | Go idiomático |
|---|---|---|
| `domain/`+`application/`+`infrastructure/` | Ciclos de import, tipos huérfanos | Paquete por contexto, archivo por rol |
| Un port+adapter por colaborador | Interface pollution, indirección, allocación | Interfaz en el consumidor, con 2ª impl o test |
| `GetX()` / `SetX()` | No es el estilo Go; el setter rompe invariantes | `X()` sin `Get`; método con intención |
| Modelo anémico + service gordo | Invariantes sin dueño | Rich model: lógica con los datos |
| `mapper/` + reflection | Sin verificación en compilación; coste runtime | Structs I/O + traducción a mano |
| Abstract Factory / Builder GoF | Difiere/omite validación; estados a medias | `New() (X, error)` / functional options |
| Repo filtra `sql.ErrNoRows`/`PgError` | Acopla callers al driver | Traducir a `ErrNotFound` en la frontera |
| `Order.Customer *Customer` | Cruza frontera transaccional; aliasing | Referencia por `CustomerID` |
| `Repository[T any]` | Oculta consultas reales; falso DRY | Repo concreto por aggregate |
| `util`/`common`/`vo`/`dto` | Cajón sin identidad; acoplamiento | Nombre de concepto / contexto |
| `BaseEntity` embebida en todo | Herencia simulada; API inflada | Composición acotada, con propósito |
| VO mutable con setter | Aliasing; deja de ser "un valor" | Inmutable; receptor valor, crea-nuevo |
| `service.go` / `usecase.go` + `type Service` | Archivo sin intención; dependencias sumadas; todo cambio choca | Un archivo y un tipo por caso de uso (`login.go` → `Login`) |

## Checklist

- [ ] Cero carpetas top-level de capa ni paquetes-cajón (`util`, `common`, `vo`, `dto`, `models`, `types`): paquete por contexto/concepto.
- [ ] Toda interfaz tiene ≥2 impls o es un seam de test, y la declara el consumidor, no el implementador.
- [ ] Comportamiento y accessors en métodos del tipo que posee los datos (sin `Get`, sin setters ciegos, sin `Service` externo); value objects inmutables (crea-nuevo).
- [ ] Nada de auto-mapping por reflection: structs I/O y traducción a mano junto al use case.
- [ ] Los agregados se construyen con `New() (X, error)`, no con Builders ni factorías abstractas.
- [ ] Ningún error de driver (`sql.ErrNoRows`, `pgconn.PgError`) sale del repositorio; los aggregates se referencian por ID, nunca por puntero.
- [ ] Sin `Repository[T any]`: interfaz concreta por aggregate (generics solo si es type-agnostic real).
- [ ] Sin `BaseEntity`: la composición solo la embeben los tipos que la usan.
- [ ] Sin `service.go`, `usecase.go`, `app.go`, `manager.go` ni `handler.go` con varios endpoints: cada caso de uso es un tipo en un archivo con su nombre.
- [ ] Ante cualquier duda DDD ↔ Go: gana Go (Regla #0).
