# Value vs Reference Semantics — Go Idiomatic Reference

## Go SIEMPRE Pasa por Valor

En Go no existe pass-by-reference. **Toda asignación y todo paso de parámetro COPIA el valor.** No hay excepciones y no hay una sintaxis para pedir "pásame el original" como el `&T` de C++.

```go
func mutate(n int) { n = 99 } // opera sobre una copia

x := 1
mutate(x)
fmt.Println(x) // 1 — el original nunca cambió
```

El contraste con otros lenguajes es la fuente de casi todos los malentendidos:

| Lenguaje | Semántica |
|---|---|
| C++ | pass-by-value por defecto; `T&` da pass-by-reference real |
| Java | pass-by-value siempre; las referencias a objetos **también** se pasan por valor (copias del handle) |
| Go | pass-by-value siempre; para mutar el original pasas un `*T` explícito — y ese puntero también se copia |

El punto clave: cuando "pasas un puntero", Go copia el puntero (8 bytes en 64-bit), no lo que apunta. La mutación funciona porque la copia del puntero apunta al mismo dato. Sigue siendo pass-by-value; lo que compartes es el pointee.

---

## Tipos Valor vs Tipos "Referencia"

La categoría "tipo referencia" es informal: el valor se copia por valor como todo lo demás, pero contiene un puntero interno a datos compartidos. Copiar el header es barato; los datos apuntados no se duplican.

| Categoría | Tipos | Qué se copia al pasar |
|---|---|---|
| Valor | `bool`, numéricos, `string`, array, `struct` | Todo el contenido (el struct/array entero) |
| "Referencia" (informal) | `slice`, `map`, `chan`, `func`, pointer | Solo el header/handle; los datos apuntados se comparten |

`string` es un caso especial: es un header `{ptr, len}` pero **inmutable**, así que compartir el backing es seguro y no hay aliasing observable. Los demás tipos referencia sí exponen aliasing: mutar a través de una copia se ve en el original.

---

## Slice Header — {ptr, len, cap}

Un slice es un header de tres words. En 64-bit ocupa **24 bytes**: puntero al backing array, `len` y `cap`.

```go
type sliceHeader struct {
	ptr *T  // apunta al backing array
	len int // elementos accesibles
	cap int // capacidad hasta el final del backing array
}
```

Pasar un slice copia esos 24 bytes, pero el `ptr` copiado apunta al mismo backing array. Por eso mutar `s[i]` dentro de una función **sí** afecta al llamador.

```go
// BAD: mutación accidental — el llamador no espera que le pisen los datos
func normalize(s []float64) {
	for i := range s {
		s[i] /= maxOf(s) // escribe en el backing array del llamador
	}
}

// GOOD: si NO debes mutar al llamador, copia primero y retorna
func normalized(s []float64) []float64 {
	out := make([]float64, len(s))
	copy(out, s) // copia elementos a un backing array propio
	m := maxOf(out)
	for i := range out {
		out[i] /= m
	}
	return out
}
```

Regla: mutar los elementos de un slice recibido es un efecto secundario sobre el llamador. Hazlo solo si el contrato de la función lo documenta.

---

## append — El Detalle que Rompe Código

`append` comparte el backing array cuando hay `cap` suficiente, pero **realloca** (copia a un array nuevo) cuando se excede `cap`. El efecto sobre el llamador es NO determinista: depende del `cap` en runtime.

```go
// cap suficiente → escribe in-place en el backing compartido
a := make([]int, 3, 10)
b := append(a, 99) // b comparte backing con a; b[0:3] alias a

// cap excedido → realloca: b apunta a OTRO array, a queda intacto
c := make([]int, 3, 3)
d := append(c, 99) // d es un array nuevo, c no cambió
```

Como no puedes predecir cuál rama se ejecuta, **siempre reasigna el resultado**:

```go
s = append(s, x) // el único uso correcto — nunca ignores el retorno
```

### Aliasing entre sub-slices

Un sub-slice hereda el `cap` del padre hasta el final del backing array. Un `append` sobre el sub-slice puede pisar elementos del padre.

```go
// BAD: sub hereda cap=5 → append pisa base[2]
base := []int{1, 2, 3, 4, 5}
sub := base[0:2]      // len=2, cap=5
sub = append(sub, 99) // escribe en base[2]
// base == {1, 2, 99, 4, 5}  ← corrupción silenciosa

// GOOD: three-index slice s[a:b:c] recorta cap=c-a → append realloca
sub := base[0:2:2]    // len=2, cap=2
sub = append(sub, 99) // cap agotado → array nuevo
// base == {1, 2, 3, 4, 5}  ← intacto
```

El three-index slice `s[low:high:max]` produce `len = high-low`, `cap = max-low`. Úsalo para aislar el `cap` cuando entregues un sub-slice que el receptor podría hacer crecer.

---

## Map — Puntero a hmap

Un `map` es internamente un puntero a una estructura `hmap`. Pasar un map copia ese puntero, así que add/delete dentro de una función **se ven fuera**.

```go
func addKey(m map[string]int) {
	m["new"] = 1 // visible en el llamador — mutamos el mismo hmap
	delete(m, "old")
}
```

### nil map — Lectura OK, Escritura Panic

```go
var m map[string]int // nil, no inicializado
_, ok := m["x"]      // OK: lectura de nil map retorna el zero value, ok=false
m["x"] = 1           // panic: assignment to entry in nil map
```

Inicializa siempre antes de escribir: `m := make(map[string]int)` o `m := map[string]int{}`.

### Copiar un Map

No existe una copia implícita: asignar un map copia el puntero, no las entradas. Para una copia real, itera o usa `maps.Clone`.

```go
// Go 1.21+ — shallow copy: las claves y valores se copian por valor,
// pero si el valor es a su vez un slice/map/pointer, sigue compartido
clone := maps.Clone(m)

// Equivalente manual
clone := make(map[string]int, len(m))
for k, v := range m {
	clone[k] = v
}
```

---

## Array vs Slice — Por Qué en Go se Pasan Slices

Un array tiene semántica de valor pura: pasar un `[N]T` **copia los N elementos enteros**. Un slice comparte el backing. Esta es la razón por la que las APIs de Go aceptan slices, no arrays.

```go
// array: la copia es del array completo → el original no cambia
func mutArr(a [3]int) { a[0] = 99 }

// slice: comparte backing → el original sí cambia
func mutSlice(s []int) { s[0] = 99 }

arr := [3]int{1, 2, 3}
mutArr(arr)
fmt.Println(arr) // [1 2 3] — se copiaron los 24 bytes del array

sl := []int{1, 2, 3}
mutSlice(sl)
fmt.Println(sl) // [99 2 3] — se compartió el backing
```

Pasar un `[1024]byte` por valor copia 1 KB en cada llamada. Pasa `sl[:]` (un slice) o `*[1024]byte` cuando el array es grande.

---

## Structs — Copia Shallow

Copiar un struct copia todos sus campos, pero la copia es **shallow**: los campos que son punteros, slices o maps siguen apuntando a los mismos datos que el original.

```go
type User struct {
	Name  string
	Tags  []string // slice header copiado; backing COMPARTIDO
	Prefs *Prefs   // puntero copiado; el Prefs apuntado COMPARTIDO
}

u1 := User{Name: "a", Tags: []string{"x"}}
u2 := u1        // copia shallow
u2.Name = "b"   // independiente (string es valor)
u2.Tags[0] = "z" // ¡muta u1.Tags[0] también! backing compartido
```

### go vet copylocks — Copiar un Mutex es un Bug

Un `sync.Mutex` (o cualquier tipo con `sync.Locker`) no debe copiarse: la copia tiene su propio estado de lock y no protege nada. `go vet` lo detecta.

```go
type Counter struct {
	mu    sync.Mutex
	count int
}

// BAD: value receiver copia el struct → copia el Mutex.
// go vet: "Inc passes lock by value: Counter contains sync.Mutex"
func (c Counter) Inc() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.count++ // además, muta una copia — inútil
}

// GOOD: pointer receiver opera sobre el Mutex original
func (c *Counter) Inc() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.count++
}
```

Corolario: cualquier struct que embeba un `sync.Mutex`, `sync.WaitGroup`, `sync.Once`, etc., debe pasarse y recibirse por puntero, nunca por valor.

### range Copia el Elemento

`for _, v := range xs` asigna a `v` una **copia** de cada elemento. Escribir en `v` no toca el slice.

```go
type Point struct{ X, Y int }
xs := []Point{{1, 1}, {2, 2}}

// BAD: v es una copia; la mutación se descarta
for _, v := range xs {
	v.X = 100 // no muta nada
}

// GOOD: indexa el slice original
for i := range xs {
	xs[i].X = 100 // muta el elemento real
}
```

(Nota: en Go 1.22+ la variable de loop es per-iteración, lo que arregla el capture en closures, pero `v` sigue siendo una copia del elemento — esto no cambia.)

---

## Receiver Value vs Pointer (Semántica de Copia)

Un **value receiver** opera sobre una copia del receiver: mutaciones al receiver se pierden. Un **pointer receiver** opera sobre el original: las mutaciones persisten.

```go
type Meter struct{ n int }

func (m Meter) BumpByValue() { m.n++ } // opera sobre una copia; no persiste
func (m *Meter) BumpByPtr()  { m.n++ } // opera sobre el original; persiste
```

Aquí solo cubrimos la semántica de copia. La decisión completa value vs pointer receiver — coste de copia según el tamaño del struct, escape analysis, consistencia del method set, y el criterio de performance — vive en `references/memory-pointers.md`.

---

## Copia Defensiva

Cuando guardas un slice/map recibido en un campo, o retornas un slice/map interno, el llamador conserva un header que apunta a **tu** backing. Puede mutar tu estado interno sin pasar por tus métodos. La copia defensiva corta ese aliasing en la frontera.

```go
type Config struct {
	tags []string
}

// BAD: guarda el slice del llamador — aliasing del backing array
func (c *Config) SetTags(tags []string) {
	c.tags = tags // el caller puede mutar c.tags después
}

// GOOD: copia al entrar → estado interno aislado del llamador
func (c *Config) SetTags(tags []string) {
	c.tags = slices.Clone(tags) // Go 1.21+
}

// BAD: expone el slice interno — el caller puede pisar tu estado
func (c *Config) Tags() []string { return c.tags }

// GOOD: retorna una copia → el caller muta la suya, no la tuya
func (c *Config) Tags() []string { return slices.Clone(c.tags) }
```

Cuándo copiar defensivamente:

- Al **almacenar** un slice/map/pointer que el llamador aún conserva y podría mutar.
- Al **retornar** un slice/map interno que representa estado protegido (especialmente si hay un mutex de por medio).
- **No** copies cuando el dato es efímero, inmutable por contrato, o el coste de la copia domina en un hot path — mide antes.

`slices.Clone` y `maps.Clone` (Go 1.21+) hacen shallow copy: bastan cuando los elementos son valores. Si los elementos contienen punteros y necesitas aislamiento total, la copia debe ser deep — clona cada elemento a mano.

---

## Checklist

- [ ] Recuerda que todo paso de parámetro copia; pasa `*T` explícito para mutar el original
- [ ] No mutes los elementos de un slice recibido salvo que el contrato lo documente
- [ ] Siempre `s = append(s, ...)` — nunca ignores el retorno de `append`
- [ ] Usa el three-index slice `s[a:b:c]` para aislar el `cap` de un sub-slice que el receptor podría hacer crecer
- [ ] Inicializa los maps con `make` antes de escribir — escribir en un nil map es panic
- [ ] `maps.Clone` / `slices.Clone` para copiar (shallow); deep copy manual si los elementos tienen punteros
- [ ] Pasa arrays grandes como slice (`arr[:]`) o `*[N]T`, no por valor
- [ ] Recuerda que copiar un struct es shallow: slices/maps/punteros internos siguen compartidos
- [ ] Nunca copies un struct con `sync.Mutex` embebido — pointer receiver siempre; corre `go vet`
- [ ] En `range`, indexa `xs[i]` para mutar; `v` es una copia
- [ ] Copia defensiva al guardar o retornar slices/maps que representan estado interno protegido
- [ ] Para el framework completo value vs pointer receiver desde performance, ver `references/memory-pointers.md`
