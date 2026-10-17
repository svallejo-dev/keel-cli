# Value Objects — DDD Táctico al Estilo Idiomático de Go

> **Regla #0.** Ante cualquier conflicto entre la ortodoxia DDD y el Go idiomático, gana Go. Los value objects son el punto del táctico DDD donde ambos mundos coinciden casi por completo: Go ya tiene semántica de valor de primera clase. No importamos Java-ismos (getters/setters, `equals()`/`hashCode()`, `final` por todas partes); usamos lo que el lenguaje ya te da. La skill compañera `senior-go-idiomatic` cubre el detalle del lenguaje; aquí solo asumimos su vocabulario.

Snippets verificados con toolchain `go1.24.7`; el código es válido en Go 1.26 estándar.

## 1. Definición

Un **value object** (VO) es un tipo pequeño, **inmutable**, **sin identidad**, definido enteramente por sus atributos. Dos VOs con los mismos atributos son intercambiables: no existe "este email" frente a "aquel email", solo existe el valor.

El VO es la **unidad mínima donde viven las validaciones de esa unidad**. La regla de negocio "un email tiene forma `local@domain`" no pertenece al handler, ni al use case, ni a la entity que lo contiene: pertenece al tipo `Email`. Concentrar la validación en el VO es lo que hace que el resto del sistema pueda asumir datos correctos sin re-validar.

Contraste con **entity** (ver `references/entities.md`): la entity tiene identidad (`ID`) y ciclo de vida; dos entities con los mismos atributos siguen siendo distintas. El VO no: es puro valor.

## 2. Realización en Go — dos formas y su trade-off

Hay exactamente dos maneras idiomáticas de materializar un VO en Go, y la elección no es estética.

| | `type Email struct{ v string }` (campo no-exportado) | `type Email string` (alias) |
|---|---|---|
| Allocación | Igual que el string subyacente (un campo) | Cero-alloc, es el string |
| Ergonomía | Requiere `String()`/accessor para leer | Se usa casi como un `string` |
| Barrera de invariantes | **Infranqueable** desde fuera del paquete | **Evadible**: `Email("basura")` compila dentro del paquete |
| Comparable con `==` | Sí (si los campos lo son) | Sí |
| Uso recomendado | VOs con invariantes fuertes | Envoltorios triviales sin invariante real |

El alias es tentador: es cero-alloc y se lee como un string. Pero su invariante es **evadible**: cualquier código en el mismo paquete puede escribir `Email("no-es-un-email")` con una conversión y saltarse el constructor. El struct con campo no-exportado no tiene esa fuga: fuera del paquete es imposible construir un `Email` con un valor arbitrario porque `v` no es accesible.

```go
type Email struct {
	v string
}

type Username string
```

La fuga del alias es concreta: dentro del paquete que lo declara, la conversión de tipo evita el constructor por completo.

```go
// BAD: dentro del paquete, esto compila y salta toda validación.
u := Username("basura") // sin pasar por NewUsername

// GOOD con el struct: NewEmail es la única puerta desde fuera del paquete.
e, err := NewEmail("basura") // err != nil
```

**Recomendación:** struct con campo privado para VOs con invariantes fuertes (email, dinero, coordenadas); alias solo para envoltorios triviales donde el tipo aporta significado semántico pero no hay regla que proteger.

## 3. Constructor con validación

La validación vive en un **constructor**. Esto no es el patrón GoF "Factory Method" (no hay jerarquía, ni polimorfismo, ni subclases): es un constructor idiomático de Go, una función que devuelve `(T, error)`.

```go
func NewEmail(s string) (Email, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Email{}, fmt.Errorf("%w: empty", ErrInvalidEmail)
	}
	at := strings.LastIndexByte(s, '@')
	if at <= 0 || at == len(s)-1 {
		return Email{}, &ValidationError{Field: "email", Reason: "missing local or domain part"}
	}
	return Email{v: s}, nil
}
```

El principio rector es **make invalid states unrepresentable**: si tienes en la mano un `Email`, es válido *por construcción*. No hay un `Email` medio-formado esperando validación posterior. El `zero value` (`Email{}`) es el único estado inválido posible, y no puede confundirse con uno construido porque el constructor nunca lo devuelve junto a un `err == nil`.

Consecuencia práctica: el resto del código deja de defenderse. Una función que recibe `Email` no re-valida — el tipo *es* la garantía. Esto elimina la validación defensiva dispersa que plaga los sistemas donde se pasan `string` crudos (ver `references/antipatterns.md`).

El mismo patrón escala a VOs multi-campo. `Money` valida en construcción y expone operaciones que devuelven un valor nuevo, no que mutan:

```go
type Money struct {
	cents    int64
	currency string
}

func NewMoney(cents int64, currency string) (Money, error) {
	if currency == "" {
		return Money{}, &ValidationError{Field: "currency", Reason: "empty"}
	}
	return Money{cents: cents, currency: currency}, nil
}

// Add falla con ErrCurrencyMismatch si las monedas difieren.
func (m Money) Add(other Money) (Money, error) {
	if m.currency != other.currency {
		return Money{}, fmt.Errorf("%w: %s vs %s", ErrCurrencyMismatch, m.currency, other.currency)
	}
	return Money{cents: m.cents + other.cents, currency: m.currency}, nil
}
```

La operación de negocio (`Add`) puede fallar, así que devuelve `error`: sumar dos monedas distintas es un estado inválido y el tipo lo impide en el punto exacto donde ocurre.

## 4. Inmutabilidad por convención

Go no tiene `readonly` ni `const` para structs. La inmutabilidad es una **convención sostenida por tres decisiones**, no una garantía del compilador:

1. **Campos no-exportados** — nadie fuera del paquete puede reasignarlos.
2. **Value receivers** — el método opera sobre una copia; mutarla no afecta al original.
3. **Métodos que devuelven un VO NUEVO** — cualquier "transformación" produce otro valor, nunca muta el receptor.

```go
// Normalized baja a minúsculas solo el dominio.
func (e Email) Normalized() Email {
	at := strings.LastIndexByte(e.v, '@')
	local, domain := e.v[:at], strings.ToLower(e.v[at+1:])
	return Email{v: local + "@" + domain}
}
```

```go
// BAD: pointer receiver que muta in-place rompe la semántica de valor.
func (e *Email) normalize() { e.v = strings.ToLower(e.v) }

// GOOD: value receiver que devuelve un valor nuevo.
func (e Email) Normalized() Email { /* ... */ }
```

Un VO con pointer receiver mutante es un smell: significa que estás tratando un valor como si tuviera identidad. Si necesitas mutación, probablemente es una entity, no un VO.

## 5. Igualdad

Un VO se define por su valor, así que la igualdad es **igualdad por valor**. Cuando el tipo es `comparable` (sin slices, maps ni funciones dentro), Go te la da gratis con `==`. Nada de escribir `Equal`/`hashCode` a mano como en Java.

```go
a, _ := NewEmail("dev@example.com")
b, _ := NewEmail("dev@example.com")
_ = a == b // true, y compila: Email{ v string } es comparable
```

**FOOTGUN.** Si el VO lleva un slice (o map) interno deja de ser `comparable` y `==` **no compila**. El compilador es tajante:

```go
type Recipients struct {
	to []Email
}

// BAD: no compila.
//   invalid operation: a == b
//   (struct containing []Email cannot be compared)
func recipientsEqualBAD(a, b Recipients) bool {
	return a == b
}
```

La corrección es proveer un método `Equal` explícito. Con Go 1.21+, `slices.Equal` lo resuelve cuando el elemento es comparable (aquí `Email` lo es):

```go
// GOOD: método Equal explícito.
func (r Recipients) Equal(other Recipients) bool {
	return slices.Equal(r.to, other.to)
}
```

Regla operativa: **diseña los VOs para que sean `comparable` siempre que puedas** (usa arrays de tamaño fijo o campos escalares en vez de slices). Un VO comparable, además, sirve como key de map sin ceremonia — algo imposible con un tipo no-comparable:

```go
balances := map[Money]int{}
usd, _ := NewMoney(100, "USD")
balances[usd]++
```

Cuando la colección tiene una cota conocida, un **array** (`[N]T`) preserva la comparabilidad donde un slice la rompería:

```go
type Line3 struct {
	fields [3]Email // con []Email el struct dejaría de ser comparable
}
```

Solo cuando la naturaleza del dato exige una colección de tamaño arbitrario aceptas el coste de un `Equal` manual.

## 6. Errores de validación tipados / sentinela

El constructor falla con errores que las capas superiores puedan **mapear a un 400** sin inspeccionar strings. Dos herramientas complementarias:

**Sentinela** — para un modo de fallo único y estable, comparable con `errors.Is`:

```go
var ErrInvalidEmail = errors.New("identity: invalid email")
```

**`ValidationError` tipado** — cuando quieres transportar el campo y la razón hasta el handler, inspeccionable con `errors.As`:

```go
type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("identity: %s: %s", e.Field, e.Reason)
}
```

El transporte traduce sin acoplarse al texto del mensaje (ver `references/application-layer.md`):

```go
switch {
case errors.Is(err, ErrInvalidEmail):
	http.Error(w, "invalid email", http.StatusBadRequest)
default:
	var ve *identity.ValidationError
	if errors.As(err, &ve) {
		http.Error(w, ve.Reason, http.StatusBadRequest)
		return
	}
	http.Error(w, "internal error", http.StatusInternalServerError)
}
```

El dominio no conoce HTTP; solo emite errores tipados. El mapeo a status code vive en el borde. Envuelve siempre con `%w` para no romper la cadena de `errors.Is`/`errors.As`.

## 7. Stateless / siempre-crea-nuevo

El VO es **stateless en el sentido temporal**: no se actualiza in-place, siempre se reemplaza. Esto encaja de forma natural con la semántica de valor de Go, donde asignar y pasar copia es el comportamiento por defecto.

La entity **compone** VOs y, cuando cambia, reemplaza el VO completo — nunca alcanza dentro de él para mutar un campo:

```go
type User struct {
	id    int64
	email Email
}

func (u *User) ChangeEmail(e Email) {
	u.email = e
}
```

Nótese el reparto de receivers: la entity usa **pointer receiver** (tiene identidad y muta su estado interno); el VO usa **value receiver** (es valor puro). Esa asimetría es la firma en el código de la distinción entity/VO. La entity es dueña de su ciclo de vida; el VO solo existe como el valor de un campo. Cómo se agrupan entities y VOs bajo una raíz transaccional es competencia del aggregate (ver `references/aggregates.md`).

### El borde de persistencia

El campo no-exportado que protege el invariante tiene un coste: el repository no puede reconstruir un VO leído de la DB con un struct literal, porque no puede tocar `v` desde otro paquete. La reconstitución **debe pasar por el constructor**, igual que la entrada de usuario:

```go
// pgstore, al leer una fila
e, err := identity.NewEmail(row.Email)
if err != nil {
	return nil, fmt.Errorf("corrupt email in row %d: %w", row.ID, err)
}
```

Esto es una feature, no una fricción: si un dato persistido viola el invariante (migración defectuosa, escritura fuera de banda), lo detectas al cargar en vez de propagar un VO inválido por el dominio. Cuando la validación en la lectura sea un cuello de botella medido, ofrece un constructor `mustReconstruct` **no-exportado** dentro del bounded context, nunca uno público que reabra la evasión.

## Checklist

- [ ] VO con invariante fuerte → `struct` con campo **no-exportado**, no alias.
- [ ] Alias (`type T string`) solo para envoltorios triviales sin regla que proteger.
- [ ] Toda validación vive en el constructor `NewX(...) (X, error)` — *make invalid states unrepresentable*.
- [ ] Sin `readonly`: inmutabilidad por campos privados + value receivers + métodos que devuelven un VO nuevo.
- [ ] Ningún método de VO usa pointer receiver para mutar in-place.
- [ ] VO diseñado como `comparable` cuando es posible → `==` gratis, sirve como key de map.
- [ ] VO con slice/map interno → método `Equal` explícito (`slices.Equal`); nunca esperar que `==` compile.
- [ ] Errores de construcción tipados/sentinela (`errors.Is`/`errors.As`), envueltos con `%w`, mapeados a 400 en el borde.
- [ ] El dominio no conoce HTTP; el mapeo a status code vive en la capa de aplicación.
- [ ] La entity reemplaza el VO completo al cambiar; nunca muta sus campos internos.
