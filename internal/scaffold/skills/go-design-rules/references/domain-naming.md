# Domain & Semantic Naming — Go Design Rules

Cómo el modelo de tipos de Go convierte los nombres del dominio en garantías del
compilador. Estándar hoy Go 1.26; snippets verificados contra el toolchain 1.24.7.

> **Regla #0.** Ante conflicto entre un principio de diseño y un idiom de Go, gana
> el idiom de Go. Aquí no hay conflicto: los named types y los enums *son* la forma
> idiomática. Las skills compañeras `senior-go-idiomatic` y `ddd-go-idiomatic`
> profundizan en el estilo léxico y en el modelado táctico de dominio.

---

## 1. Las dos capas del naming

El naming en Go tiene dos capas independientes:

- **Léxica** (cómo se escribe el identificador): MixedCaps, sin stutter
  (`http.Server`, no `http.HTTPServer`), sin prefijo `Get` en getters, receivers
  cortos y consistentes. Es territorio de las convenciones canónicas de Go y vive
  en la skill compañera de idioms; no se repite aquí.
- **Semántica / de dominio** (qué representa el identificador): qué es un `UserID`,
  por qué `Status` no es un `string`, por qué un método se llama `Withdraw` y no
  `SetBalance`. **Esta capa es la que cubre este archivo.**

La regla que une ambas: el compilador debe entender tu dominio tanto como el lector.
Un nombre bueno que descansa sobre un tipo primitivo desperdicia la mitad del trabajo.

---

## 2. Named types sobre primitivos

Un `int64` que es un identificador de cuenta y otro `int64` que es dinero son, para
el compilador, el mismo tipo: intercambiables sin aviso. Un **named type** los separa.

```go
type UserID int64
type AccountID int64
type ProductID int64
type Cents int64
type Email string
```

La firma pasa de opaca a **self-documenting**, y el compilador caza el *parameter-swap*
en tiempo de compilación en vez de en producción.

```go
// BAD: cuatro int64 en fila; nada impide cruzar from/to ni pasar un ID como monto.
func Transfer(from, to int64, amount int64) error { /* ... */ }

Transfer(toAcct, fromAcct, balance) // compila feliz aunque from/to estén cruzados
```

```go
// GOOD: cada argumento tiene su tipo de dominio; el orden equivocado no compila.
func Transfer(from, to AccountID, amount Cents) error { /* ... */ }
```

**Los named types NO son intercambiables aunque compartan el tipo base**: hay que
convertir explícito (`AccountID(x)`). Eso no es fricción, es la feature — el mismo muro
que impide el bug. Pasar un `ProductID` donde va un `CustomerID` es un error de compilación:

```go
type CustomerID int64
type ProductID int64

func Lookup(c CustomerID) { /* ... */ }

var p ProductID = 42
Lookup(p) // no compila
```

```text
./badswap.go:10:9: cannot use p (variable of int64 type ProductID)
    as CustomerID value in argument to Lookup
```

| Firma primitiva                          | Firma con named types                       | Qué gana el compilador           |
| ---------------------------------------- | ------------------------------------------- | -------------------------------- |
| `Transfer(from, to int64, amt int64)`    | `Transfer(from, to AccountID, amt Cents)`   | Caza swap de args y ID↔dinero    |
| `Charge(cust int64, amt int64)`          | `Charge(cust CustomerID, amt Cents)`        | Caza swap cust↔monto             |
| `Send(to string)`                        | `Send(to Email)`                            | `Email` no acepta cualquier str  |

Regla: si un primitivo cruza una frontera de función y representa un concepto del
dominio (identidad, dinero, unidad, dirección), dale un named type. La conversión
explícita es el punto exacto donde validas o construyes el valor.

---

## 3. Muerte al stringly-typed

**Stringly-typed** es modelar con `string` (o `int` crudo) algo que tiene un conjunto
cerrado de valores: estados, tipos, monedas, unidades. Las comparaciones se vuelven
frágiles —literales dispersos por el código, typos que el compilador no ve— y el
conjunto válido no está en ninguna parte del tipo.

```go
// BAD: status como string. "actve" compila; el set válido no existe en el tipo.
type Order struct{ status string }

func (o *Order) IsOpen() bool {
	return o.status == "pending" || o.status == "paid" // literales frágiles
}
```

La cura es un named type con **enum vía `iota`** (ver también la referencia de estilo
para el patrón `iota` + `Stringer`):

```go
// GOOD: Status es un tipo cerrado; el compilador y el reader ven el set completo.
type Status uint8

const (
	StatusPending Status = iota
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

Ahora `o.status == StatusPaid` es type-safe, un typo no compila, y `String()` da
logging legible sin exponer el `uint8` interno. El mismo criterio aplica a `Currency`,
`Unit`, `Role`: conjunto cerrado ⇒ named type, nunca `string` suelto.

---

## 4. Muerte al boolean blindness

**Boolean blindness** es perder la información en el call site: `true`/`false` no dicen
qué activan, y dos bools contiguos se cruzan sin que el compilador avise —el mismo
*parameter-swap* del §2, pero peor porque `bool` no admite named-type-mismatch si los
declaras `bool` a secas.

```go
// BAD: ¿qué es true? ¿qué es false? ¿y si los inviertes?
func Charge(amount Cents, recurring, refundable bool) error { /* ... */ }

Charge(500, true, false) // ilegible, y Charge(500, false, true) compila igual
```

Tres salidas idiomáticas, de menor a mayor peso:

**a) Named bool types** — legibilidad en el call site y mismatch de tipos:

```go
type Recurring bool
type Refundable bool

func Charge(amount Cents, recurring Recurring, refundable Refundable) error { /* ... */ }

Charge(500, Recurring(true), Refundable(false)) // el nombre viaja al call site
```

**b) Enum** cuando el flag es en realidad un modo con más de dos valores hoy o mañana
(`BillingMode` en vez de `recurring bool`) — evita el segundo bool que siempre llega.

**c) Functional options** cuando hay varios flags opcionales; el default es el zero value
y cada opción se nombra:

```go
// GOOD: cada bit de configuración es una opción con nombre; el default no requiere flags.
type ChargeOption func(*chargeConfig)

func WithRecurring() ChargeOption  { return func(c *chargeConfig) { c.recurring = true } }
func WithRefundable() ChargeOption { return func(c *chargeConfig) { c.refundable = true } }

ChargeOpts(500, WithRecurring()) // se lee solo; refundable queda en su default
```

Regla: un `bool` desnudo en una firma pública es una señal de humo. Nómbralo, vuélvelo
enum, o hazlo option.

---

## 5. Métodos con verbo de intención

El nombre de un método debe revelar la **operación de negocio**, no la mutación mecánica
de un campo. `SetStatus(StatusCancelled)` describe cómo cambia la memoria; `Cancel()`
describe qué pasó en el dominio —y encapsula la regla de negocio y los invariantes.

```go
// BAD: setters CRUD; el llamador arma la transición a mano y puede violar reglas.
func (a *Account) SetBalance(c Cents)  { a.balance = c }
func (a *Account) SetStatus(s Status)  { a.status = s }

a.SetBalance(a.Balance() - amount) // ¿validó saldo? ¿quién garantiza el invariante?
```

```go
// GOOD: el método nombra la intención y es dueño de la regla.
func (a *Account) Withdraw(amount Cents) error {
	if amount > a.balance {
		return errInsufficient
	}
	a.balance -= amount
	return nil
}

func (a *Account) Cancel() { a.status = StatusCancelled }
```

Verbos de intención típicos: `Withdraw`, `Deposit`, `Confirm`, `Cancel`, `Promote`,
`Approve`, `Ship`. Cada uno es un punto único donde vive la validación; los `Set*`
la reparten entre todos los call sites. Los getters sí existen (`Balance()`, `Status()`),
pero los mutadores se nombran por la operación de negocio, no por el campo.
Esto entronca con la regla de acoplar comportamiento al estado que lo gobierna
(ver `references/coupling-behavior.md`).

---

## 6. El nombre del paquete es vocabulario

El identificador de un paquete se antepone a cada símbolo que exporta (`order.Status`,
`billing.Charge`): es parte del nombre y, por tanto, parte del vocabulario del dominio.
Un paquete debe **nombrar un concepto**, no una miscelánea.

| BAD                 | Por qué                                        | GOOD                 |
| ------------------- | ---------------------------------------------- | -------------------- |
| `util`, `common`    | No nombran nada; atraen basura sin cohesión    | `order`, `billing`   |
| `helpers`, `shared` | Cajón de sastre; el símbolo no gana contexto   | `pricing`, `auth`    |
| `models`, `types`   | Agrupan por mecánica, no por concepto          | `account`, `invoice` |

Si un paquete se llama `util`, la pregunta correcta no es cómo renombrarlo sino a qué
concepto pertenece cada símbolo que contiene. El nombre del paquete y el del símbolo
se leen juntos: `order.New`, no `models.NewOrderModel`.

---

## 7. El nombre del archivo es intención

Dentro de un paquete, el nombre del **archivo** cumple el mismo papel que el del
paquete: dice qué hay dentro antes de abrirlo. En un bounded context, **cada caso de
uso vive en su propio archivo, nombrado por lo que hace** (snake_case, verbo + objeto),
y cada concepto de dominio en el archivo del concepto. Un archivo nunca agrupa dos
casos de uso.

| BAD (archivo cajón)                                   | Por qué                                         | GOOD                                       |
| ----------------------------------------------------- | ----------------------------------------------- | ------------------------------------------ |
| `service.go` con `Login()` y `Signup()`               | Dos intenciones; el nombre no dice ninguna      | `login.go`, `signup.go`                    |
| `usecase.go` / `usecases.go` / `use_cases.go`         | Nombra la capa, no la acción                    | `search_users.go`, `register_user.go`      |
| `application.go` / `app.go`                           | Capa técnica disfrazada de archivo              | un archivo por caso de uso                 |
| `manager.go`, `helpers.go`, `utils.go`, `common.go`   | Miscelánea; atrae código sin cohesión           | el archivo del concepto al que pertenece   |
| `handler.go` / `handlers.go` con varios endpoints     | Mezcla transporte de varios casos de uso        | `register_user.go` por endpoint + `routes.go` |

Permitidos porque nombran algo concreto: `user.go`, `email.go` (conceptos),
`errors.go` (errores del dominio), `user_repository.go` (un puerto compartido),
`routes.go` (montaje del transporte). Fuera de los contextos, un composition root como
`internal/handlers/handlers.go` es aceptable: su única intención es montar rutas.

El tipo sigue al archivo: un struct por caso de uso con nombre de acción y un único
método exportado. Prohibidos los tipos cajón `Service`, `UseCase`, `Manager`.

```go
// BAD: internal/identity/service.go — dos casos de uso en un tipo y un archivo.
type Service struct{ users Users; tokens Tokens }

func (s *Service) Login(ctx context.Context, in LoginInput) (LoginOutput, error)   { /* ... */ }
func (s *Service) Signup(ctx context.Context, in SignupInput) (SignupOutput, error) { /* ... */ }
```

```go
// GOOD: internal/identity/login.go
type Login struct {
	users  userFinder
	tokens tokenIssuer
}

func NewLogin(users userFinder, tokens tokenIssuer) *Login {
	return &Login{users: users, tokens: tokens}
}

func (uc *Login) Execute(ctx context.Context, in LoginInput) (LoginOutput, error) {
	/* ... */
}
```

`Signup` vive en `signup.go` con su propio struct, sus DTOs (`SignupInput`,
`SignupOutput`) y los puertos estrechos que usa. El listado del directorio pasa a ser el
índice de lo que el contexto sabe hacer: `ls internal/identity` responde "¿qué casos de
uso hay?" sin abrir nada.

---

## 8. Named types como fundamento de estados legales

Named types y enums no solo documentan: son la primera herramienta para **volver
imposibles los estados inválidos**. Un `Status uint8` con `iota` acota el conjunto de
estados representables; un `Email` construido por una función de validación garantiza
que todo valor del tipo ya pasó por esa puerta. Llevar esto hasta hacer los estados
ilegales *no representables* (constructores que validan, tipos suma, campos privados)
es el tema de `references/illegal-states.md`. El naming semántico es el punto de entrada:
sin named types no hay dónde colgar el invariante.

El principio se apoya además en `references/dependencies.md` (dónde inyectar la
construcción validada) y en `references/dry-solid-go.md` (un named type es también el
lugar natural para no repetir la lógica de validación).

---

## Checklist

- [ ] Identidad, dinero, unidades y direcciones son named types, no `int64`/`string` crudos
- [ ] Named types se convierten explícito en la frontera donde se validan/construyen
- [ ] El *parameter-swap* de IDs o de ID↔dinero es error de compilación, no bug de runtime
- [ ] Estados/tipos/monedas cerrados son enums con `iota` + `String()`, cero strings mágicos
- [ ] Cero `bool` desnudos en firmas públicas: named bool type, enum o functional option
- [ ] Mutadores nombran la operación de negocio (`Withdraw`, `Cancel`), no el campo (`SetBalance`)
- [ ] Cada método de intención es el dueño único de su validación/invariante
- [ ] Paquetes nombran un concepto (`order`, `billing`); cero `util`/`common`/`helpers`/`models`
- [ ] Un archivo por caso de uso, nombrado por su intención (`login.go`, `search_users.go`); cero `service.go`/`usecase.go`/`manager.go`/`utils.go`
- [ ] Ningún archivo ni tipo agrupa dos casos de uso; cero tipos cajón `Service`/`UseCase`/`Manager`
- [ ] El invariante cuelga del named type; el estado ilegal tiende a no ser representable
- [ ] Regla #0: si un principio choca con un idiom de Go, gana el idiom
