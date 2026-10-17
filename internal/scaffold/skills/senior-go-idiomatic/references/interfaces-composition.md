# Interfaces & Composition — Go Idiomatic Reference

## El Modelo Mental Correcto

En Go una interfaz no es un contrato que un tipo firma, sino uno que **cumple sin
saberlo**. No hay `implements` ni `extends`: un tipo satisface una interfaz por el
simple hecho de tener sus métodos. Esto invierte la dirección del diseño respecto a
Java/C# — las interfaces no viven donde se implementan, viven donde se consumen, y son
tan pequeñas como el consumidor las necesite. Todo lo demás aquí se deriva de esa idea:
**implicit satisfaction**.

---

## 1. Satisfacción Implícita

Un tipo satisface una interfaz por **tener sus métodos** (el method set correcto), no
por declararlo. El compilador lo comprueba en el punto de asignación o paso a la
interfaz, nunca en la definición del tipo.

```go
// Java/C#:  class Temperature implements Stringer { ... }   ← acoplamiento explícito
// Go:       no existe "implements". Si tienes String(), ERES un fmt.Stringer.

type Temperature float64

func (t Temperature) String() string { // este método basta
	return fmt.Sprintf("%.1f°C", float64(t))
}

var s fmt.Stringer = Temperature(21.5) // satisface sin declararlo
```

La ventaja clave: puedes satisfacer interfaces de paquetes que **no controlas**, y
hacer **retrofit** de un tipo a una interfaz nueva sin tocar el tipo original. En un
lenguaje nominal eso exige editar la clase; en Go basta con definir el método (o
envolver el tipo ajeno).

```go
type IDer interface{ ID() string }

// wire.Message es de terceros; lo envolvemos para que hable IDer.
type OrderMessage struct{ wire.Message }

func (m OrderMessage) ID() string { return "order-" + m.Ref }
```

Tipo e interfaz evolucionan en paquetes distintos sin dependencia de compilación entre
ellos — ese desacoplamiento es la base del diseño idiomático.

---

## 2. El Consumidor Define la Interfaz

La interfaz vive en el paquete que la **usa**, no en el que la implementa. El
implementador exporta un **tipo concreto**; el consumidor declara la interfaz mínima
que consume. (Extiende "interfaces al punto de uso" de `project-layout.md`.)

```go
// BAD — el paquete que implementa exporta una interfaz gigante "por completitud".
// Obliga a cualquier implementador a tener 5 métodos que casi nadie usa.
package store

type UserStore interface {
	GetByID(ctx context.Context, id int64) (*User, error)
	Create(ctx context.Context, u *User) error
	Delete(ctx context.Context, id int64) error
	Migrate(ctx context.Context) error
	Stats() string
}
```

```go
// GOOD — el implementador exporta el tipo concreto y nada más.
package store

type PostgresStore struct{ /* ... */ }

func (s *PostgresStore) GetByID(ctx context.Context, id int64) (*User, error) { /* ... */ }
// ...Create, Delete, Migrate, Stats

// GOOD — el CONSUMIDOR declara la interfaz mínima que necesita: un método.
package greet

type userReader interface { // sin exportar; solo lo que se consume
	GetByID(ctx context.Context, id int64) (*User, error)
}

type Greeter struct{ users userReader }
// *store.PostgresStore satisface userReader automáticamente (implicit satisfaction)
```

La interfaz pertenece al lado que **depende** de la abstracción: el implementador nunca
importa al consumidor.

---

## 3. Accept Interfaces, Return Concrete Types (regla de oro)

```
Accept interfaces → Return concrete types
```

- **Entrada = interfaz** → flexibilidad: acepta cualquier fuente que cumpla el
  contrato (archivo, red, string, buffer, mock).
- **Salida = tipo concreto** → claridad y features completas: el caller recibe todos
  los campos y métodos. Devolver una interfaz esconde capacidades y fuerza type-asserts.

```go
// Acepta io.Reader (cualquier fuente); retorna *Report concreto (API completa).
func Analyze(src io.Reader) (*Report, error) {
	rep := &Report{}
	sc := bufio.NewScanner(src)
	for sc.Scan() {
		rep.Lines++
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("analyze: %w", err)
	}
	return rep, nil // el caller ve rep.Summary(), rep.Lines, etc.
}

rep, _ := Analyze(strings.NewReader("a\nb\nc\n")) // string, archivo o body: da igual
```

Excepción legítima: retornar una interfaz cuando el conjunto de implementaciones es
cerrado y el caller **debe** tratarlas de forma uniforme (`error` es el caso canónico).

---

## 4. Interface Pollution (anti-patrón)

> *"Don't design with interfaces, discover them."* — Rob Pike

No crees una interfaz por cada struct "por si acaso". Una interfaz de **un solo
implementador**, sin test-double real ni necesidad concreta de swap, es puro ruido:
duplica la API, añade una indirección y esconde el tipo real.

```go
// BAD — interfaz especulativa: un método, un implementador, cero razones.
type Cacher interface {
	Get(k string) ([]byte, bool)
	Set(k string, v []byte)
}
type Cache struct{ /* la única implementación que jamás existirá */ }
```

```go
// GOOD — expón el struct concreto. Descubre la interfaz cuando un consumidor real
// la necesite (p. ej. un test double o un segundo backend).
type Cache struct{ m map[string][]byte }

func NewCache() *Cache                       { return &Cache{m: make(map[string][]byte)} }
func (c *Cache) Get(k string) ([]byte, bool) { v, ok := c.m[k]; return v, ok }
func (c *Cache) Set(k string, v []byte)      { c.m[k] = v }
```

YAGNI: añadir la interfaz cuesta 3 líneas el día que haga falta; quitarla obliga a
refactorizar a todos los callers.

---

## 5. Interfaces Pequeñas y Segregación

Cuanto más pequeña la interfaz, más tipos la satisfacen y más fácil es componerla.
`io.Reader` tiene **un** método por diseño. Las grandes se **componen** de pequeñas vía
**interface embedding**:

```go
// Definiciones reales de la stdlib (io):
type Reader interface {
	Read(p []byte) (n int, err error)
}
type Closer interface {
	Close() error
}

// ReadCloser NO redeclara métodos: embebe Reader y Closer.
type ReadCloser interface {
	Reader
	Closer
}
```

Un consumidor que solo cierra debe aceptar `io.Closer`, no `io.ReadCloser`: pide el
contrato **mínimo** y aceptarás más tipos.

```go
func closeQuietly(c io.Closer) { _ = c.Close() } // acepta cualquier Closer
```

---

## 6. Method Sets — Value vs Pointer Receiver

| Tipo del valor | Method set incluye | Satisface interfaces con métodos... |
|---|---|---|
| `T`  | métodos con **value receiver** | ...solo de value receiver |
| `*T` | value **y** pointer receiver | ...de value Y pointer receiver |

Consecuencia práctica: si algún método usa pointer receiver, **debes** asignar `*T`
(no `T`) a la interfaz, o no compila.

```go
type Counter struct{ n int }

func (c *Counter) Inc()          { c.n++ }                         // pointer receiver
func (c Counter) String() string { return fmt.Sprintf("%d", c.n) } // value receiver

var s fmt.Stringer = Counter{}        // OK: String tiene value receiver
var w interface{ Inc() } = &Counter{} // OK: *Counter incluye ambos

// NO COMPILA: Counter (value) no tiene Inc en su method set.
//   var bad interface{ Inc() } = Counter{}
//   → "Counter does not implement ... (method Inc has pointer receiver)"
```

El value-vs-pointer del receiver también implica copia y performance (mutación, tamaño
del struct, escape analysis); el porqué está en `references/value-vs-reference.md`.
Consistencia: si **un** método necesita pointer receiver, úsalo en **todos**.

---

## 7. El nil Interface Footgun (crítico)

Una interfaz es un par **(type, value)**. Es `nil` **solo si AMBOS** son nil. Un
puntero nil de tipo concreto —`(*T)(nil)`— guardado en una interfaz lleva el type `*T`,
así que la interfaz **NO es nil**, aunque el value interno sí lo sea.

El bug clásico: retornar un `*MyError` nil declarado como `error`.

```go
type MyError struct{ Code int }

func (e *MyError) Error() string { return fmt.Sprintf("code=%d", e.Code) }

// BAD: devuelve un *MyError nil como error. La interfaz lleva type=*MyError,
// value=nil → NO es la interfaz nil. Todo caller con `if err != nil` se dispara.
func doWorkBad(fail bool) error {
	var err *MyError // nil pointer
	if fail {
		err = &MyError{Code: 500}
	}
	return err // SIEMPRE interfaz no-nil (el type nunca es nil)
}

// GOOD: retorna nil sin tipo en el camino de éxito.
func doWorkGood(fail bool) error {
	if fail {
		return &MyError{Code: 500}
	}
	return nil // interfaz nil de verdad: (type=nil, value=nil)
}
```

Demostración en runtime (compila y corre; el bug es de lógica, no de compilación):

```go
func main() {
	err := doWorkBad(false)         // no hubo fallo
	fmt.Println(err == nil)         // imprime: false  ← el footgun
	fmt.Printf("%T %v\n", err, err) // *main.MyError <nil>
}
```

Reglas: nunca declares un `*T` local y lo retornes como `error` (retorna `nil` en el
éxito, o el `*T` solo en el fallo); desconfía de `if err != nil` cuando el valor imprime
`<nil>`; `go vet` no lo detecta, los linters `nilness`/`nilerr` sí.

---

## 8. Compile-Time Interface Check

Ancla la satisfacción en tiempo de compilación con un blank identifier. Coste cero en
runtime (`(*T)(nil)` es un puntero tipado nil) y falla el build si el tipo deja de
cumplir el contrato.

```go
// Falla al compilar si *Server deja de satisfacer http.Handler.
var _ http.Handler = (*Server)(nil)
```

Ponlo junto a la definición del tipo exportado: convierte un error de runtime lejano en
un fallo de compilación local.

---

## 9. Struct Embedding — Composición sobre Herencia

Go no tiene herencia. Tiene **embedding**: incrustar un tipo dentro de un struct sin
nombre de campo. El outer type **promociona** los campos y métodos del embebido
(field/method promotion), pero no hay subtipado ni polimorfismo dinámico — es
composición, no jerarquía.

### Promoción de campos y métodos

```go
type Base struct{ ID int64 }

func (b Base) Describe() string { return fmt.Sprintf("id=%d", b.ID) }

type User struct {
	Base // embebido (sin nombre de campo)
	Name string
}

u := User{Base: Base{ID: 7}, Name: "ada"}
_ = u.ID         // campo promovido (azúcar de u.Base.ID)
_ = u.Describe() // método promovido
_ = u.Base.ID    // la ruta explícita sigue disponible
```

### Embeber una INTERFAZ en un struct — patrón decorador/wrapper

Embeber una **interfaz** (no un struct) promociona sus métodos hacia el outer type. Es
el patrón para envolver, p. ej., un `io.Reader` y hacer **override** de un solo método
delegando el resto del contrato al valor embebido.

```go
// countingReader ES un io.Reader (hereda Read por promoción) y cuenta bytes.
type countingReader struct {
	io.Reader // interfaz embebida: promociona Read y cualquier otro método
	N         int64
}

// Override de Read; el resto del contrato lo cubre el io.Reader embebido.
func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.Reader.Read(p) // delega en el wrapped
	c.N += int64(n)
	return n, err
}

var _ io.Reader = (*countingReader)(nil)
```

Envuelves una interfaz enorme e interceptas **un** método sin reimplementar los demás:
el compilador promueve el resto del embebido.

### Override por shadowing

```go
type Loud struct{ Base }

// Loud.Describe hace shadow del promovido de Base.
func (l Loud) Describe() string { return "LOUD:" + l.Base.Describe() }
```

### Ambigüedad con embedding múltiple

Si dos embebidos aportan un método (o campo) con el **mismo nombre al mismo nivel de
profundidad**, el selector promovido es **ambiguo** y no compila. Se resuelve
calificando la ruta o definiendo el método en el outer type.

```go
type A struct{}
type B struct{}

func (A) Ping() string { return "a" }
func (B) Ping() string { return "b" }

type C struct {
	A
	B
}

// c.Ping() NO compila: "ambiguous selector c.Ping".
// Se desambigua definiendo el método en C y eligiendo la ruta:
func (c C) Ping() string { return c.A.Ping() }
```

Un método a menor profundidad **gana** sobre uno más profundo; la ambigüedad solo surge
al mismo nivel.

---

## Checklist

- [ ] Ningún tipo declara `implements`; la satisfacción es implícita y se comprueba en el punto de uso
- [ ] Las interfaces se definen en el paquete que las **consume**, no en el que las implementa
- [ ] Las funciones **aceptan interfaces** y **retornan tipos concretos** (salvo el caso cerrado tipo `error`)
- [ ] No hay interfaces de un solo implementador sin test-double o swap real (sin interface pollution)
- [ ] Las interfaces son mínimas; las grandes se componen por **interface embedding**
- [ ] Si algún método usa pointer receiver, se asigna `*T` a la interfaz y **todos** los métodos usan pointer receiver
- [ ] Ningún `*T` nil local se retorna como `error`; el camino de éxito retorna `nil` sin tipo
- [ ] `var _ Iface = (*T)(nil)` presente para tipos exportados que deben cumplir una interfaz de otro paquete
- [ ] El embedding se usa para composición (promoción/decorador), nunca imitando herencia
- [ ] La ambigüedad por embedding múltiple se resuelve con un método explícito en el outer type
