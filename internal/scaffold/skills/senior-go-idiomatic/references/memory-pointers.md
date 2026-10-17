# Memory & Pointers — Go Idiomatic Reference

## El Modelo Mental Correcto

La memoria de un programa Go vive en dos sitios: el **stack** (una pila por goroutine, barata) y el **heap** (compartido, gestionado por el garbage collector, caro). La pregunta que importa no es "¿uso un puntero?" sino "¿dónde termina viviendo este dato y cuánto le cuesta al GC?". Decisión clave: **el programador no elige stack o heap.** Lo decide el compilador mediante *escape analysis*; tú solo controlas las condiciones — retornar direcciones, guardar valores en interfaces, capturar en closures — y el compilador deduce las consecuencias.

Este archivo cubre **dónde vive la memoria y por qué en términos de performance**. Para *qué se copia* al pasar o asignar (semántica de copia, aliasing de slice/map), ver `references/value-vs-reference.md`. El layout y padding de structs ya está en `SKILL.md`; aquí no se desarrolla.

> Toolchain de las salidas transcritas: Go 1.24.7. Estándar hoy: Go 1.26.

---

## 1. Stack vs Heap

El **stack** crece y decrece con el *frame* de la función: entrar empuja un frame, retornar lo libera. Asignar es incrementar un puntero; liberar, decrementarlo. Sin coste de gestión y con los datos calientes en cache. El **heap** almacena valores cuya vida excede el frame que los creó: nadie los libera al retornar; los reclama el garbage collector, que debe rastrear qué sigue vivo. Cada valor en el heap es trabajo futuro para el GC.

| | Stack | Heap |
|---|---|---|
| Gestión | Automática con el frame (push/pop) | Garbage collector (mark-sweep) |
| Coste de alloc | ~0 (mover un puntero) | Allocador + presión de GC |
| Cache locality | Alta (contiguo, caliente) | Depende — a menudo disperso |
| Vida | Termina con la función | Hasta que el GC lo reclama |
| Quién decide | El compilador (escape analysis) | El compilador (escape analysis) |

Regla operativa: **lo que no escapa es gratis.** El objetivo en el hot path no es evitar punteros por dogma, sino evitar que los valores escapen al heap sin necesidad.

---

## 2. Escape Analysis

El compilador determina en tiempo de compilación si un valor puede vivir en el stack o debe "escapar" al heap. Se inspecciona con `-gcflags="-m"`:

```bash
go build -gcflags="-m" ./...
```

Un valor escapa cuando su vida no se puede acotar al frame. Los disparadores canónicos:

```go
// stays: x vive en el stack; muere con el frame.
func stays() int {
	x := 42
	return x // se copia el valor al caller; x no escapa
}

// escapesPtr: retornar &local mueve x al heap.
func escapesPtr() *int {
	x := 42
	return &x // el puntero sobrevive a la función → x al heap
}

type Stringer interface{ String() string }
type box struct{ n int }

func (b box) String() string { return "box" }

// escapesIface: meter un valor en una interfaz lo mueve al heap.
func escapesIface() Stringer {
	b := box{n: 1}
	return b // la interfaz oculta el tipo concreto → box al heap
}

// escapesClosure: el closure sobrevive a la función; captura x.
func escapesClosure() func() int {
	x := 42
	return func() int { return x } // x vive mientras viva el closure
}

// unknownSize: make con longitud variable → backing array al heap.
func unknownSize(n int) []byte {
	return make([]byte, n) // tamaño no conocido en compilación
}

// knownSize: tamaño constante y pequeño → puede quedarse en el stack.
func knownSize() byte {
	buf := make([]byte, 64)
	return buf[0]
}
```

Salida **real** de `go build -gcflags="-m" ./demo/` (Go 1.24.7), recortada a las líneas de escape:

```
demo.go:11:2:  moved to heap: x                       // escapesPtr
demo.go:23:9:  b escapes to heap                      // escapesIface
demo.go:29:9:  func literal escapes to heap           // escapesClosure
demo.go:34:13: make([]byte, n) escapes to heap        // unknownSize (n dinámico)
demo.go:39:13: make([]byte, 64) does not escape       // knownSize (constante)
```

Vocabulario del compilador: `moved to heap: x` (variable local reubicada al heap), `escapes to heap` (el valor de una expresión se aloja en el heap), `does not escape` (se queda en el stack — lo que quieres ver en el hot path).

Disparadores adicionales: **enviar un puntero por un channel** (`ch <- &x` mueve `x` al heap, porque otra goroutine puede leerlo después) y guardar un puntero en un campo de un struct que escapa. Regla general: si algo con vida más larga que el frame puede alcanzar el valor, escapa.

---

## 3. Punteros

`&v` toma la dirección de `v`; `*p` deref­erencia el puntero `p`. El zero value de cualquier puntero es `nil`.

```go
x := 42
p := &x  // p es *int
*p = 100 // muta x a través del puntero
_ = x    // x == 100
```

### `new(T)` vs `&T{}`

```go
a := new(int)       // *int → int con zero value (0); útil para tipos básicos
c := &Counter{n: 1} // *Counter → construir y tomar dirección en un paso
```

Ambos alojan e igual retornan `*T`. Idiomático: usa `&T{...}` para structs (permite inicializar campos); `new(T)` queda para tipos básicos o cuando solo quieres el zero value. `&int{}` **no compila** — los literales compuestos son solo para structs, arrays, slices y maps.

### nil deref → panic

```go
var p *Counter // nil
p.Inc()        // panic: runtime error: invalid memory address or nil pointer dereference
```

Un método con pointer receiver sobre un puntero `nil` panica al tocar un campo; valida antes de deref­erenciar datos externos.

### Punteros a interfaces y a slices/maps

- **`*interface{}` — casi nunca.** Una interfaz ya es una referencia (par tipo+puntero). `*io.Reader` es casi siempre un error de diseño; pasa la interfaz por valor.
- **`*[]T` / `*map[K]V` — rara vez.** Slices y maps ya son *reference types* baratos de copiar (ver `references/value-vs-reference.md`). Solo necesitas `*[]T` cuando el método debe **reasignar** el slice del caller (p. ej. `append` que cambia el header). Para maps, nunca — el header ya comparte el mismo backing.

---

## 4. Framework: value vs pointer receiver

Esta es la decisión de memoria que más veces tomarás. La respuesta no es estética: determina si los métodos pueden mutar, si el tipo es seguro de copiar y si el valor escapa.

### Usa POINTER receiver `(t *T)` si:

- **El método muta el receiver.** Un value receiver opera sobre una copia; la mutación se pierde al retornar.
- **El struct es grande.** Copiar el receiver en cada llamada cuesta; un puntero copia 8 bytes.
- **Contiene un campo no-copiable** — `sync.Mutex`, `sync.WaitGroup`, `atomic.*`, o cualquier struct que a su vez los tenga. Copiar un `Mutex` rompe la exclusión mutua.
- **Consistencia.** Si *algún* método del tipo necesita pointer receiver, usa pointer receiver en **todos** para presentar un *method set* uniforme.

### Usa VALUE receiver `(t T)` si:

- **El tipo es pequeño e inmutable** — `time.Time`, `Point{X, Y float64}`, IDs.
- **Es un tipo básico o numérico** — `type Celsius float64`.
- **Quieres la seguridad de operar sobre una copia** — el método no puede alterar el original por accidente.

### Ejemplos

```go
// POINTER: muta el receiver.
type Counter struct{ n int }

func (c *Counter) Inc()     { c.n++ } // sobre un value receiver, el ++ se perdería
func (c *Counter) Get() int { return c.n }

// POINTER: contiene sync.Mutex (no-copiable) → obligatorio.
type Registry struct {
	mu    sync.Mutex
	items map[string]int
}

func (r *Registry) Add(k string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[k]++
}

// VALUE: pequeño e inmutable; cada método retorna un nuevo Point.
type Point struct{ X, Y float64 }

func (p Point) Add(q Point) Point { return Point{p.X + q.X, p.Y + q.Y} }
```

`go vet` detecta la copia accidental de un `Mutex` — salida real:

```
badCopy passes lock by value: x.Registry contains sync.Mutex
```

### Regla de oro: NO mezcles receivers en el mismo tipo

Un tipo con métodos value **y** pointer tiene un *method set* inconsistente: solo `*T` satisface interfaces que incluyan los métodos con pointer receiver, y copiar un valor cuyos métodos mutan produce bugs silenciosos. Elige uno por tipo.

### Tabla de decisión

| Condición | Receiver |
|---|---|
| El método muta el receiver | `*T` |
| Contiene `sync.Mutex` / `WaitGroup` / `atomic` | `*T` |
| Struct grande (varias palabras) | `*T` |
| Algún otro método del tipo ya usa `*T` | `*T` (consistencia) |
| Tipo pequeño, inmutable, básico | `T` |
| No estás seguro | `*T` (default seguro para structs con estado) |

---

## 5. Garbage Collector

Go usa un **mark-sweep concurrente tricolor**: marca en concurrencia con el programa los objetos alcanzables (blanco→gris→negro) y barre los blancos. El *stop-the-world* se reduce a pausas de sub-milisegundo, pero el trabajo de marcado compite por CPU con tu código. Dos perillas:

- **`GOGC`** (default `100`): dispara un ciclo de GC cuando el heap crece un 100% respecto al heap vivo tras el último ciclo. Subirlo (`GOGC=200`) reduce la frecuencia de GC a cambio de más memoria residente.
- **`GOMEMLIMIT`** (Go 1.19+): límite *soft* de memoria total. El GC se vuelve más agresivo al acercarse al límite. Es la herramienta correcta en contenedores con límite de memoria — evita el OOM kill sin fijar `GOGC` a ciegas.

La cadena causal que importa en producción:

```
más allocaciones al heap → más objetos que marcar → ciclos de GC
más frecuentes → más CPU robada al hot path → mayor latencia p99
```

**Objetivo:** minimizar allocaciones en el hot path. Menos escapes al heap = menos presión de GC = colas de latencia más planas. Un allocador rápido no arregla un hot path que genera basura; el coste real es el marcado posterior.

---

## 6. Localidad de caché

Cómo dispones los datos en memoria pesa tanto como el algoritmo cuando iteras sobre muchos elementos.

```go
type Particle struct {
	x, y, z float64
	vx, vy  float64
}

// []Particle: structs contiguos en un solo backing array.
func sumContiguous(ps []Particle) float64 {
	var total float64
	for i := range ps {
		total += ps[i].x // el prefetcher de la CPU acierta: siguiente struct ya en cache
	}
	return total
}

// []*Particle: punteros contiguos, structs DISPERSOS por el heap.
func sumScattered(ps []*Particle) float64 {
	var total float64
	for _, p := range ps {
		total += p.x // cada deref salta a una dirección impredecible → cache miss
	}
	return total
}
```

`[]T` guarda los structs uno tras otro: recorrerlos es un barrido lineal que el hardware pre-carga. `[]*T` guarda solo punteros; los structs viven fragmentados donde el allocador los dejó. Cada iteración es un salto que el prefetcher no anticipa, y un cache miss cuesta cientos de ciclos.

**Cuándo importa:** en hot loops sobre colecciones grandes (miles+ de elementos) recorridas densamente. Ahí `[]T` puede ser varias veces más rápido que `[]*T` con el mismo big-O. **Cuándo no:** colecciones pequeñas, acceso esporádico, o cuando los elementos son grandes y necesitas identidad compartida (mutación in-place vista por varios dueños). No reestructures por cache locality sin un benchmark que lo justifique.

---

## 7. Cuándo NO usar punteros

Un puntero no es gratis: añade una indirección (posible cache miss) y, si el valor escapa, presión de GC. Para valores pequeños, **copiar suele ser más barato que apuntar**.

```go
type RGBA struct{ R, G, B, A uint8 } // 4 bytes

// Value receiver + copia: cero indirección, cero allocaciones, no escapa.
func invert(c RGBA) RGBA {
	return RGBA{255 - c.R, 255 - c.G, 255 - c.B, c.A}
}
```

Pasar `RGBA` por valor copia 4 bytes en un registro. Por `*RGBA` copia 8 bytes (el puntero) **y** arriesga mover el valor al heap si escapa, sumando trabajo de GC. La copia gana.

Heurística: para structs de hasta ~2-3 palabras (16-24 bytes) sin campos no-copiables y sin necesidad de mutar al caller, prefiere value semantics. Por encima, o si hay mutación o estado compartido, usa puntero. Verifica con `-gcflags="-m"` y `-benchmem` en lugar de suponer.

---

## 8. Profiling

No optimices por intuición; mide. Herramientas, de más rápida a más profunda:

### `-gcflags="-m"` — diagnóstico estático

Antes de medir, comprueba qué escapa (Sección 2): barato y sin ejecutar nada.

### `go test -bench` con `-benchmem`

```bash
go test -bench=. -benchmem ./...
```

```go
func BenchmarkInvert(b *testing.B) {
	b.ReportAllocs() // fuerza el reporte de allocs aunque falte -benchmem
	c := RGBA{10, 20, 30, 255}
	for range b.N { // Go 1.22+: range sobre un entero
		c = invert(c)
	}
	_ = c
}
```

Salida **real** (Go 1.24.7):

```
BenchmarkInvert-2   1000000000   0.4303 ns/op   0 B/op   0 allocs/op
```

Cómo leerla: `ns/op` es latencia por operación; `B/op` son bytes alojados al heap por operación (`0` confirma que `invert` no escapa); `allocs/op` es el número de allocaciones al heap — **la métrica que persigues en el hot path: bajarla a 0.** Una allocación aquí sería regresión aunque `ns/op` no se mueva: cada alloc es deuda que pagará el GC bajo carga real.

### `-memprofile` + `go tool pprof`

```bash
go test -bench=BenchmarkInvert -benchmem -memprofile mem.out ./...
go tool pprof mem.out
#   top      → funciones que más memoria alojan
#   list Fn  → líneas exactas con allocaciones dentro de Fn
```

`pprof` te dice **dónde** se aloja; `-benchmem` te dice **cuánto**. Flujo idiomático: `-m` (qué escapa) → `-benchmem` (allocs/op) → `pprof list` (línea culpable) → refactor (reusar buffers, evitar interfaces en el hot path, `sync.Pool` si aplica) → re-benchmark.

---

## Checklist

- [ ] El valor no escapa al heap en el hot path — verificado con `go build -gcflags="-m"`; `moved to heap` / `escapes to heap` inesperados investigados
- [ ] Pointer receiver cuando el método muta, el struct es grande, o contiene `sync.Mutex`; value receiver para tipos pequeños, inmutables o básicos
- [ ] Un solo estilo de receiver por tipo — nunca value y pointer mezclados
- [ ] `go vet` limpio de `passes lock by value` (ningún `Mutex` copiado)
- [ ] Sin `*interface{}`; `*[]T` solo si el método reasigna el slice del caller
- [ ] Datos externos validados antes de deref­erenciar (evitar nil deref → panic)
- [ ] `[]T` en vez de `[]*T` para hot loops sobre colecciones grandes y densas
- [ ] `GOMEMLIMIT` fijado en contenedores con límite de memoria (Go 1.19+)
- [ ] Benchmarks con `b.ReportAllocs()` / `-benchmem`; objetivo `0 allocs/op` en el hot path
- [ ] Regresiones de memoria localizadas con `-memprofile` + `go tool pprof list`
