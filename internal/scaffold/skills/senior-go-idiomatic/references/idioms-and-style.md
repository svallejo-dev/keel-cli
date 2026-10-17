# Idioms & Style — Go Idiomatic Reference

Naming, control flow y estilo que un code review en Cloudflare, HashiCorp o Uber da
por sentados. Estándar hoy Go 1.26; snippets verificados contra el toolchain 1.24.7.

> Frontera: el manejo de errores en profundidad vive en otra referencia; aquí solo se fija el estilo de los **error strings** (punto 1).

---

## 1. Naming Idiomático

Go no usa `snake_case`: todo es **MixedCaps** (`userID`) o **mixedCaps** (`parseURL`).
La visibilidad la decide la inicial: **mayúscula = exportado**, minúscula = privado.

### Getters, setters y receivers

```go
// User modela un usuario. Receiver consistente `u` en TODOS los métodos.
type User struct {
	name string
	id   int64
}

func (u *User) Name() string     { return u.name } // getter: sin prefijo Get
func (u *User) SetName(n string) { u.name = n }    // setter: con Set
func (u *User) ID() int64        { return u.id }   // acrónimo en caps: ID
```

- Getters **sin** prefijo `Get`: `user.Name()`, nunca `user.GetName()`. Setters **sí** llevan `Set`.
- Receiver corto (1-2 letras) y **consistente** en todos los métodos del tipo (nunca `self`/`this`).

### Stutter y acrónimos

**Stutter** es repetir el nombre del paquete en el identificador; duplicarlo es ruido.

| BAD (stutter)      | GOOD           | Regla                      |
| ------------------ | -------------- | -------------------------- |
| `http.HTTPServer`  | `http.Server`  | El paquete ya dice `http`  |
| `user.UserStore`   | `user.Store`   | El paquete ya dice `user`  |

Los acrónimos van en **case consistente**: `userID` (no `userId`), `ServeHTTP`, `parseURL`
— todo mayúsculas, o todo minúsculas si arranca abajo.

### Paquetes y error strings

Nombres de paquete cortos, minúscula, una palabra, **sin** `under_scores` ni plural:
`user`, `bytes` — no `user_service`, `utils`, `models` (`bytes.Buffer`, no `bytesutil.BytesBuffer`).

Los **archivos** sí usan `snake_case` y nombran su intención: dentro de un bounded
context, un archivo por caso de uso nombrado por lo que hace (`login.go`,
`search_users.go`), nunca `service.go`, `usecase.go`, `manager.go`, `helpers.go`,
`utils.go` o `common.go`. Detalle en `references/project-layout.md` ("Nombres de Archivo por Intención").

Los error strings se concatenan: minúscula, **sin** puntuación final y **sin** `"failed to"`.

```go
errors.New("Failed to open config file.")   // BAD
errors.New("open config: file not found")    // GOOD
```

---

## 2. Zero Value Útil

Diseña tipos usables sin constructor: el **zero value** debe ser válido. `sync.Mutex`,
`bytes.Buffer` y un slice `nil` (appendable) funcionan recién declarados.

```go
// Counter es safe para uso concurrente y usable como su zero value.
type Counter struct {
	mu    sync.Mutex
	total int64
}

func (c *Counter) Add(n int64) {
	c.mu.Lock()
	c.total += n
	c.mu.Unlock()
}

var c Counter    // listo sin constructor; el sync.Mutex ya es válido
c.Add(3)

var s []int      // slice nil, pero appendable
s = append(s, 1) // no hace falta make() previo
```

Regla: **si el zero value ya sirve, no escribas `NewCounter()`**; un constructor que
solo hace `return &Counter{}` es ruido.

---

## 3. Control Flow — Early Return

El **happy path** va alineado a la izquierda, sin indentar. Los casos de error se
descartan primero con **early return**, evitando `else` y anidamiento.

```go
// BAD: happy path indentado, else innecesario, anidamiento profundo
func processBad(r *Record) error {
	if r != nil {
		if r.Valid {
			return nil
		} else {
			return errInvalid
		}
	} else {
		return errNil
	}
}

// GOOD: early return, happy path alineado a la izquierda
func processGood(r *Record) error {
	if r == nil {
		return errNil
	}
	if !r.Valid {
		return errInvalid
	}
	return nil // happy path, sin indentar
}
```

---

## 4. Enums con iota + Stringer

Go no tiene tipo `enum`: usa un tipo con nombre + `iota` y un `String()` para logging.
Nunca strings mágicos (`"running"`) — no son type-safe y el compilador no detecta typos.

```go
// State is the lifecycle state of a worker.
type State int

//go:generate stringer -type=State

const (
	StateIdle State = iota // 0
	StateRunning           // 1
	StatePaused            // 2
	StateStopped           // 3
)

// String implementa fmt.Stringer. En producción lo genera `stringer`;
// escrito a mano aquí para que compile sin dependencias.
func (s State) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateRunning:
		return "running"
	case StatePaused:
		return "paused"
	case StateStopped:
		return "stopped"
	default:
		return fmt.Sprintf("State(%d)", int(s))
	}
}
```

`//go:generate stringer -type=State` genera el `String()` con `go generate`. Con `iota`,
insertar un valor en medio renumera el resto solo; el `default` cubre valores fuera de rango.

---

## 5. Comentarios: pocos, cortos y con "por qué"

El código bien nombrado ya dice **qué** hace; un comentario solo se gana el sitio
cuando dice algo que el código no puede decir. Si un archivo tiene más líneas de
comentario que de código, el problema es el código (nombres) o el comentario (ruido).
Objetivo orientativo: ≤ 10–15 % de líneas de comentario.

**Sí:**
- Package doc de **una línea** sobre `package` (o en `doc.go` si necesita más):
  `// Package user models accounts and their lifecycle.`
- Doc comment de **una línea** en identificadores exportados, solo con lo que el nombre
  no dice (unidades, invariante, error devuelto, concurrencia). Empieza con el nombre y
  termina en punto; `revive` lo exige en exportados.
- El **por qué** no obvio: una restricción externa, un workaround con su issue, una
  decisión de rendimiento medida.
- Directivas: `//go:generate`, `//go:embed`, `//nolint:x // razón`.

**No:**
- Narrar el código (`// incrementa i`, `// llama al repo`, `// devuelve el error`).
- Repetir el nombre (`// NewUser crea un nuevo User.`): si no aporta, sobra; en no
  exportados, se omite.
- Banners y separadores (`// -------`, `// ==== Handlers ====`): el archivo ya es la sección.
- Comentarios más largos que el código que acompañan; las explicaciones de diseño van
  al README o al ADR, no encima de cada función.
- Código comentado y `TODO` sin dueño ni issue.

```go
// BAD: el comentario pesa más que el código y no dice nada que el código no diga.

// -----------------------------------------------------------------------------
// NewUser creates a new User. It receives the email and the name, validates
// them and returns the User or an error if validation fails.
func NewUser(email Email, name string) (User, error) {
	// check that the name is not empty
	if name == "" {
		return User{}, ErrEmptyName // return the error
	}
	// build and return the user
	return User{email: email, name: name}, nil
}
```

```go
// Package user models accounts and their lifecycle.
package user

// NewUser returns ErrEmptyName when name is blank; email is already validated.
func NewUser(email Email, name string) (User, error) {
	if name == "" {
		return User{}, ErrEmptyName
	}
	return User{email: email, name: name}, nil
}

func normalize(s string) string {
	// NFC first: Apple clients send decomposed accents and equality would fail.
	return strings.TrimSpace(norm.NFC.String(s))
}
```

### Example tests

Los **Example tests** son documentación ejecutable: aparecen en pkg.go.dev y `go test`
verifica que su stdout coincida con `// Output:`. Si diverge, el test falla.

```go
func ExampleAbs() {
	fmt.Println(Abs(-42))
	fmt.Println(Abs(7))
	// Output:
	// 42
	// 7
}
```

---

## 6. defer — Sutilezas

### Los argumentos se evalúan al DECLARAR el defer

```go
// BAD: n se evalúa al DECLARAR el defer; el reasignado luego no se ve.
func snapshotBad(n int) {
	defer fmt.Println("captured:", n) // imprime el n ORIGINAL
	n = 99                            // este cambio no llega al defer
}

// GOOD: el closure lee n en el momento del return.
func snapshotGood(n int) {
	defer func() { fmt.Println("captured:", n) }() // imprime 99
	n = 99
}
```

El mismo footgun muerde con `time.Now()`: `defer log(time.Since(start))` evalúa
`time.Since` **ahora** (≈ 0), no al retornar. `go vet` lo detecta.

### defer dentro de un loop acumula hasta el return de la función

```go
// BAD: cada defer f.Close() se acumula hasta el return de processAll
func processAllBad(paths []string) error {
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close() // fds abiertos durante TODO el loop
	}
	return nil
}

// GOOD: extraer el cuerpo a una función; el defer se libera por iteración
func processAllGood(paths []string) error {
	for _, p := range paths {
		if err := processOne(p); err != nil {
			return err
		}
	}
	return nil
}

func processOne(p string) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close() // se libera al retornar de processOne
	return nil
}
```

### defer + named return value

Un `defer` con named return puede **modificar el error de salida** — la forma limpia de
no perder el error de `Close()` al flushear:

```go
func writeConfig(path string) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr // Close puede fallar al flushear; no lo pierdas
		}
	}()
	_, err = f.WriteString("data")
	return err
}
```

---

## 7. Slices y Maps Idiomáticos Modernos

Desde Go 1.21 los paquetes `slices`/`maps` y los builtins `min`/`max`/`clear` reemplazan bucles a mano.
En 1.23 `maps.Keys`/`Values` devuelven **iterators** (`iter.Seq`), materializables con `slices.Sorted`.

```go
nums := []int{3, 1, 2}
slices.Sort(nums)                      // [1 2 3]              // Go 1.21+
_ = slices.Contains(nums, 2)           // true                // Go 1.21+
_ = slices.Equal(nums, []int{1, 2, 3}) // true                // Go 1.21+

x := min(3, 7) // 3                                            // Go 1.21+ builtin
y := max(3, 7) // 7                                            // Go 1.21+ builtin

m := map[string]int{"c": 3, "a": 1, "b": 2}
keys := slices.Sorted(maps.Keys(m)) // iterator → slice ordenado // Go 1.23+
clear(m)                            // vacía el map in-place      // Go 1.21+ builtin
```

Regla: antes de escribir un `for` para ordenar, buscar o comparar slices, revisa si
`slices`/`maps` ya lo tiene. `clear` sobre un slice pone cada elemento a su zero value.

---

## 8. Herramientas NO Negociables

En Go el formato **no se discute**: `gofmt` define un único formato canónico, así que no
hay debates de estilo (tabs vs spaces, dónde va la llave) en el code review.

| Herramienta           | Rol                                                     |
| --------------------- | ------------------------------------------------------- |
| `gofmt` / `goimports` | Formato canónico + orden de imports; corre en el editor |
| `go vet`              | Detecta bugs que compilan (defer mal, Printf, locks)    |
| `staticcheck`         | Análisis estático profundo (unused, simplificaciones)   |
| `golangci-lint`       | Meta-linter que orquesta los anteriores en CI           |

`goimports` es `gofmt` + gestión de imports. `go vet` y un linter (`golangci-lint` o `staticcheck`)
son **obligatorios en CI**: un PR que no pasa `gofmt -l` (salida vacía = limpio) ni se revisa.

---

## Checklist

- [ ] Identificadores en MixedCaps; cero `snake_case` (los nombres de archivo sí lo usan)
- [ ] Exportado ⇔ inicial mayúscula; nada de `Get` en getters, `Set` sí en setters
- [ ] Receiver corto (1-2 letras) y consistente en todos los métodos del tipo
- [ ] Sin stutter (`http.Server`, no `http.HTTPServer`)
- [ ] Acrónimos en case consistente (`userID`, `ServeHTTP`, `parseURL`)
- [ ] Paquetes cortos, minúscula, singular, sin under_scores ni `util`/`models`
- [ ] Error strings en minúscula, sin punto final, sin `"failed to"`
- [ ] Zero value usable; sin constructor si no aporta invariantes
- [ ] Early return; happy path sin indentar; sin `else` tras `return`
- [ ] Enums con `iota` + `String()`; cero strings mágicos
- [ ] Doc comments de una línea, empiezan con el identificador y aportan algo que el nombre no dice; Example tests con `// Output:`
- [ ] Cero comentarios narrativos, banners `// ----` o comentarios más largos que el código; solo el "por qué" no obvio
- [ ] Archivos en `snake_case` nombrados por intención; cero `service.go`/`usecase.go`/`utils.go`
- [ ] `defer`: ojo con args evaluados al declarar, loops y named return
- [ ] `slices`/`maps`/`min`/`max`/`clear` en vez de bucles a mano (Go 1.21+)
- [ ] `gofmt -l` vacío, `go vet` limpio y linter en verde antes de push
