# Illegal States — Modelar con Tipos para que lo Inválido no Compile

Técnicas para que el conjunto de valores que un tipo admite coincida con el que el dominio
permite, moviendo errores de runtime a compile time. Estándar hoy Go 1.26; snippets
verificados contra el toolchain 1.24.7.

> **Regla #0.** Ante conflicto entre un principio de diseño y el Go idiomático, gana Go. Un
> tipo-fortaleza que pelea con `gofmt`, con la semántica de valor o con el zero value útil no
> es más seguro: es fricción. Las skills compañeras `senior-go-idiomatic` (lenguaje) y
> `ddd-go-idiomatic` (dominio) son opcionales; esta referencia es autocontenida.

## 1. Make illegal states unrepresentable + parse, don't validate

**Regla.** Modela el dominio con tipos tales que un valor inválido no se pueda construir.
**Por qué.** Cada `string`/`int` crudo en circulación es una re-validación pendiente: nadie
sabe si ya fue validada. **Parse, don't validate**: en la frontera (HTTP, CLI, DB) parsea el
input crudo a un tipo de dominio de una vez; `validate(raw) error` deja el `raw` vivo, `parse(raw) (T, error)`
devuelve un valor cuya existencia *es* la prueba de validez.

```go
type User struct {
	Email string
	Age   int
}

type signupRequest struct {
	Email string
	Age   int
}

// ParseSignup es el único punto que valida; el resto confía en User.
func ParseSignup(req signupRequest) (User, error) {
	addr, err := mail.ParseAddress(req.Email)
	if err != nil {
		return User{}, fmt.Errorf("parse signup: email: %w", err)
	}
	if req.Age < 18 {
		return User{}, fmt.Errorf("parse signup: age %d below minimum", req.Age)
	}
	return User{Email: addr.Address, Age: req.Age}, nil
}
```

Dónde vive cada validación respecto a las capas es competencia de `references/dependencies.md`.

## 2. Constructores que devuelven `(T, error)`

**Regla.** La validación de un tipo vive UNA vez, en su constructor. **Por qué.** Si el único
camino a un `Percentage` pasa por `NewPercentage`, tenerlo en la mano ya implica rango válido;
ningún consumidor re-chequea `0..100`.

```go
type Percentage struct {
	v float64
}

func NewPercentage(v float64) (Percentage, error) {
	if v < 0 || v > 100 {
		return Percentage{}, fmt.Errorf("percentage: out of range 0..100: %v", v)
	}
	return Percentage{v: v}, nil
}

func (p Percentage) Value() float64 { return p.v }

func (p Percentage) Apply(amount float64) float64 {
	return amount * p.v / 100
}
```

El campo `v` no-exportado hace imposible fabricar `Percentage{v: 500}` fuera del paquete. El
único estado inválido posible es el zero value (`Percentage{}` ≡ 0%), y ahí 0 sí es legal;
cuando NO lo sea, ese es el problema de la sección 3. Si el zero value ya es válido y no hay
invariante que proteger, un constructor `return T{}` es ruido: déjalo fuera.

## 3. Enums con iota y el problema del "invalid zero"

**Regla.** El zero value de un enum con `iota` es su primera constante; diséñala a propósito.
**Por qué.** Un `Status` sin inicializar vale `0`; si `0` es `StatusPending`, un struct a
medio construir "parece" pending y el bug es silencioso. Dos patrones combinables: **(a)** el
cero es `Unknown`/`Invalid` explícito, un centinela que ningún flujo produce; **(b)** validar
en la frontera con un `Parse*`, único puente desde strings.

```go
type Status int

//go:generate stringer -type=Status

const (
	StatusUnknown   Status = iota // 0: zero value = inválido explícito
	StatusPending                 // 1
	StatusShipped                 // 2
	StatusDelivered               // 3
)

// A mano para que compile sin `stringer`.
func (s Status) String() string {
	switch s {
	case StatusPending:
		return "pending"
	case StatusShipped:
		return "shipped"
	case StatusDelivered:
		return "delivered"
	default:
		return "unknown"
	}
}

func ParseStatus(raw string) (Status, error) {
	switch raw {
	case "pending":
		return StatusPending, nil
	case "shipped":
		return StatusShipped, nil
	case "delivered":
		return StatusDelivered, nil
	default:
		return StatusUnknown, fmt.Errorf("enums: invalid status %q", raw)
	}
}
```

`//go:generate stringer -type=Status` genera el `String()` con `go generate` (a mano aquí para
que compile). Nunca uses strings mágicos (`"shipped"`) como estado interno: no son type-safe
y el compilador no detecta un typo.

## 4. Tipos distintos para unidades

**Regla.** Da a cada unidad su propio tipo con nombre; no compartas `float64`/`int`. **Por qué.**
`Meters` y `Feet` son ambos `float64`, pero sumarlos es un bug; con tipos distintos el
compilador te detiene. Igual con `Cents` vs un `float` de dólares, donde además pierdes precisión.

```go
type (
	Meters float64
	Feet   float64
)

func (m Meters) ToFeet() Feet { return Feet(m * 3.28084) }

type Cents int64

func (c Cents) Add(other Cents) Cents { return c + other }

func demo() {
	var d Meters = 10
	var runway Feet = 100
	// BAD: no compila — invalid operation: mismatched types Meters and Feet.
	// _ = d + runway
	_ = d.ToFeet() + runway // GOOD: la conversión es el único puente entre unidades.
}
```

Coste cero en runtime (son el tipo subyacente) y la conversión explícita marca dónde cambias
de unidad: el punto donde se cuelan los errores de factor.

## 5. Functional options sobre bool flags y config contradictorio

**Regla.** Para constructores con muchos parámetros opcionales, usa **functional options** en
vez de flags posicionales o un config struct que admite combinaciones inválidas. **Por qué.**
`NewServer(addr, true, false, 30)` es ilegible (¿qué es `false`?) y crece mal; un
`Config{TLS: false, Cert: "x.pem"}` deja representable "sin TLS pero con certificado". El
patrón: una `Option` es una función que muta una `config` no-exportada.

```go
type config struct {
	tls     bool
	cert    string
	timeout time.Duration
}

type Option func(*config)

func WithTLS(cert string) Option {
	return func(c *config) {
		c.tls = true
		c.cert = cert
	}
}

func WithTimeout(d time.Duration) Option {
	return func(c *config) { c.timeout = d }
}

func NewServer(addr string, opts ...Option) *Server {
	cfg := config{timeout: 30 * time.Second} // defaults sanos
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Server{addr: addr, cfg: cfg}
}
```

```go
// GOOD: cada option se autodescribe en el call site (vs NewServer(addr, true, false, 30)).
s := NewServer("localhost:8443",
	WithTLS("server.pem"),
	WithTimeout(5*time.Second),
)
```

`WithTLS` liga `tls` y `cert` en una operación: no hay forma de pedir certificado sin activar
TLS — ahí el patrón elimina el estado contradictorio, no solo el ruido. Los parámetros
obligatorios van posicionales (`addr`), no como `WithAddr`.

## 6. Estados mutuamente excluyentes: sum types en Go

**Regla.** Cuando un valor es exactamente una de N variantes cerradas, exprésalo con una
**sealed interface**. **Por qué.** Go no tiene unions ni `enum` con payload. El idiom es una
interfaz con un método **no-exportado**: solo los tipos del propio paquete pueden
implementarla, así el conjunto queda cerrado y ningún paquete externo añade una variante.

```go
// El método no exportado sella el conjunto: otros paquetes no pueden implementarla.
type PaymentMethod interface {
	isPaymentMethod()
}

type Card struct {
	Last4 string
}

func (Card) isPaymentMethod() {}

type Cash struct{}

func (Cash) isPaymentMethod() {}

func Describe(m PaymentMethod) string {
	switch v := m.(type) {
	case Card:
		return fmt.Sprintf("card ending %s", v.Last4)
	case Cash:
		return "cash"
	default:
		// Inalcanzable: el set es cerrado. Panic revela un error de programa.
		panic(fmt.Sprintf("payment: unhandled method %T", m))
	}
}
```

Cada variante lleva solo sus campos (`Card` tiene `Last4`, `Cash` ninguno): eso hace
irrepresentable "efectivo con número de tarjeta". El type switch es la forma idiomática de
consumirlas; el `default` con `panic` documenta que el caso es inalcanzable.

### Alternativa: un `kind` + validación

La sealed interface cuesta interface dispatch, allocación por variante y verbosidad. Cuando
las variantes comparten casi todos los campos, un discriminador `kind` con validación es más simple.

```go
type EventKind int

const (
	EventUnknown EventKind = iota
	EventClick
	EventScroll
)

type Event struct {
	Kind EventKind
	X, Y int // solo válidos para EventClick
}

func NewClick(x, y int) Event {
	return Event{Kind: EventClick, X: x, Y: y}
}

func (e Event) Validate() error {
	if e.Kind == EventUnknown {
		return fmt.Errorf("kindvariant: unknown event kind")
	}
	return nil
}
```

El `kind` no impide un `Event{Kind: EventScroll, X: 5}` incoherente —el struct expone campos
que no aplican a toda variante—, así que la coherencia recae en `NewClick` y `Validate`. La
sealed interface hace ese estado irrepresentable; el `kind` solo lo hace detectable. Agrupar
comportamiento por variante es tema de `references/coupling-behavior.md`.

## 7. Objetivo: si un estado no debe existir, que no compile

Las siete técnicas persiguen lo mismo: reducir el conjunto de valores construibles al de
valores válidos, lo más arriba posible en la escala compile-time → runtime.

| Nivel                    | Herramienta                        | Falla el estado inválido    |
| ------------------------ | ---------------------------------- | --------------------------- |
| Compile time (mejor)     | tipos distintos, sealed interface  | al escribir el código       |
| Construcción             | constructor `(T, error)`, options  | al construir el valor       |
| Frontera                 | parse-don't-validate, `Parse*`     | al entrar el input crudo    |
| Runtime (último recurso) | `kind` + `Validate()`              | al chequear explícitamente  |

Sube cada invariante tan alto como el lenguaje te deje sin pelear con él (Regla #0): un
`Meters`/`Feet` cuesta cero y atrapa el bug al compilar; un `Validate()` es el último recurso.
Nombrar estos tipos se trata en `references/domain-naming.md`; evitar validación duplicada, en
`references/dry-solid-go.md`.

## Checklist

- [ ] El input crudo se **parsea** a un tipo de dominio en la frontera, no se valida y se deja circular.
- [ ] Cada invariante vive UNA vez, en el constructor `NewX(...) (X, error)`; campos con invariante van no-exportados.
- [ ] Enum con `iota`: primera constante = `Unknown`/`Invalid` explícito, o `Parse*` en la frontera; `String()` vía `stringer`.
- [ ] Cero strings mágicos como estado interno.
- [ ] Unidades y magnitudes distintas → tipos distintos; conversión explícita como único puente.
- [ ] Muchos opcionales → functional options; `WithX` liga campos co-dependientes para que no exista la combinación inválida.
- [ ] Variantes mutuamente excluyentes → sealed interface (método no-exportado) + type switch; cada variante solo sus campos.
- [ ] `kind` + `Validate()` solo cuando la sealed interface es demasiado; la coherencia recae en el constructor.
- [ ] Cada invariante empujado tan alto como se pueda: compile time > construcción > frontera > runtime.
- [ ] `gofmt -l` vacío, `go build ./...` y `go vet ./...` limpios.
