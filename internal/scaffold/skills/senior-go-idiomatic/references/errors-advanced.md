# Error Handling Avanzado — Go Idiomatic Reference

El modelo base vive en `SKILL.md`, sección **Error Handling (M6)**: wrapping con `%w`, inspección con `errors.Is` / `errors.As`, sentinel errors, y propagación con `errgroup`. Eso ya está — no se repite aquí. Este archivo cubre lo avanzado: sentinel vs typed vs opaque, interfaces de comportamiento, agregación con `errors.Join`, y el uso correcto de `panic` / `recover` (incluyendo fronteras de goroutine).

---

## Sentinel vs Typed vs Opaque

Tres formas de exponer un error, en orden creciente de acoplamiento con el llamador. La decisión no es de estilo: define cuánto puede el llamador depender de tu implementación interna.

| Forma | Qué expone | El llamador… | Úsalo cuando |
|---|---|---|---|
| **Opaque** (default) | Nada — solo el mensaje | Solo puede loguear o propagar | El llamador no necesita distinguir este error de otro |
| **Sentinel** | La identidad (`var ErrX`) | Compara con `errors.Is` | Existe una condición discreta que el llamador ramifica (`ErrNotFound`) |
| **Typed** | Un tipo con datos | Extrae campos con `errors.As` | El llamador necesita **datos** del error (campo inválido, código, retry-after) |

**Regla:** empieza opaco. Promueve a sentinel o typed solo cuando un llamador real necesita ramificar sobre el error. Cada identidad y cada campo que expones se vuelve parte de tu API pública y ata a tus consumidores a tu implementación.

### Opaque — el default

```go
// El llamador no puede inspeccionar la causa; solo propagarla con más contexto.
// Cambiar la implementación interna nunca rompe a nadie.
func loadConfig(id int64) error {
    if id == 0 {
        return fmt.Errorf("load config id=%d: %w", id, ErrNotFound)
    }
    return nil
}
```

### Sentinel — condición que el llamador compara

```go
// Definido a nivel de paquete, exportado. El llamador hace errors.Is(err, ErrNotFound).
var ErrNotFound = errors.New("user: not found")
```

Ver `SKILL.md` M6 para la mecánica de `errors.Is` sobre cadenas envueltas.

### Typed — cuando el llamador necesita datos

```go
type ValidationError struct {
    Field string
    Msg   string
}

func (e *ValidationError) Error() string {
    return fmt.Sprintf("validation: field %q: %s", e.Field, e.Msg)
}

// El llamador extrae el dato, no solo la identidad:
var vErr *ValidationError
if errors.As(err, &vErr) {
    respondBadRequest(w, vErr.Field) // necesita el campo concreto
}
```

---

## Interfaces de Comportamiento (behavior, no type)

Exponer un tipo concreto (`errors.As` a `*MyError`) ata al llamador a tu struct. Exponer **comportamiento** no: el llamador hace `errors.As` a una **interfaz** que él mismo define, y tú implementas ese método sin exportar el tipo. Es el patrón de `net.Error` (`Timeout() bool`, `Temporary() bool`).

**Regla:** para condiciones transversales (es temporal, es un timeout, se puede reintentar), expón un método, no un tipo. Desacopla al productor del consumidor: cualquier error de cualquier paquete que implemente el método clasifica.

```go
// El paquete que INSPECCIONA define la interfaz mínima que le importa.
type temporary interface {
    Temporary() bool
}

func IsTemporary(err error) bool {
    var t temporary
    return errors.As(err, &t) && t.Temporary()
}
```

```go
// El paquete que PRODUCE implementa el comportamiento sin exportar el tipo.
// Unwrap mantiene la cadena viva para errors.Is / errors.As.
type transientError struct {
    err error
}

func (e *transientError) Error() string   { return e.err.Error() }
func (e *transientError) Unwrap() error   { return e.err }
func (e *transientError) Temporary() bool { return true }
```

`errors.As(err, &t)` con `t` de tipo interfaz recorre la cadena y hace match con el primer error que satisface el método. Ni el productor ni el consumidor conocen el tipo concreto del otro.

---

## `errors.Join` — Agregar Múltiples Errores  `// Go 1.20+`

`errors.Join` combina N errores en uno solo. Devuelve `nil` si todos los argumentos son `nil`, así que es seguro pasarle un slice que puede estar vacío. El error resultante implementa `Unwrap() []error`, de modo que `errors.Is` / `errors.As` inspeccionan **todos** los errores unidos, no solo el primero.

### Validar N campos — acumular en vez de fallar al primero

```go
func validateUser(u User) error {
    var errs []error
    if u.Name == "" {
        errs = append(errs, errors.New("name is required"))
    }
    if u.Age < 0 {
        errs = append(errs, errors.New("age must be non-negative"))
    }
    return errors.Join(errs...) // nil si errs está vacío
}
```

### Cerrar N recursos — acumular errores de cleanup

```go
// No cortocircuites al primer Close que falle: intenta cerrarlos todos.
func closeAll(closers []io.Closer) error {
    var err error
    for _, c := range closers {
        err = errors.Join(err, c.Close()) // Join ignora los nil
    }
    return err
}
```

### Recorrerlo — `errors.Is` inspecciona todos los unidos

```go
err := errors.Join(ErrNotFound, io.EOF)
errors.Is(err, ErrNotFound) // true
errors.Is(err, io.EOF)      // true — ambos, no solo el primero
```

`errors.Join` no reemplaza a `%w`: usa `%w` para envolver **una** causa, y `Join` para agregar **varios** errores hermanos.

---

## panic / recover — El Uso Correcto

**Regla:** en una API de librería, `panic` **nunca** es para errores esperados. Un error esperado (input inválido, recurso ausente, red caída) se retorna como `error`. `panic` se reserva para:

1. **Bugs de programación** — un invariante roto que indica que el código está mal (índice fuera de rango lógico, estado imposible, un `default:` en un switch exhaustivo).
2. **Config imposible en el arranque** — el proceso no puede funcionar y debe morir de inmediato (`regexp.MustCompile`, `template.Must`).

```go
// panic aceptable: patrón inválido = bug del programador, detectado al iniciar el proceso.
var reEmail = regexp.MustCompile(`^\S+@\S+$`)
```

### BAD — panic como control de flujo

```go
// BAD: input inválido es una condición esperada, no un bug. Esto tumba al llamador.
func parsePort(s string) int {
    n, err := strconv.Atoi(s)
    if err != nil {
        panic("invalid port")
    }
    return n
}
```

### GOOD — error retornado

```go
// GOOD: el llamador decide qué hacer con el error.
func parsePort(s string) (int, error) {
    n, err := strconv.Atoi(s)
    if err != nil {
        return 0, fmt.Errorf("parse port %q: %w", s, err)
    }
    return n, nil
}
```

### `recover` solo en fronteras

`recover` va **únicamente** en un boundary donde un panic no debe tumbar el proceso: un handler HTTP, un supervisor de goroutine, la raíz de un worker. Nunca lo uses para "manejar" errores esperados en medio de la lógica de negocio.

```go
// Middleware que convierte un panic en un 500 + log, sin tumbar el servidor.
func Recoverer(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        defer func() {
            if v := recover(); v != nil {
                log.Printf("panic recovered: %v\n%s", v, debug.Stack())
                http.Error(w, "internal server error", http.StatusInternalServerError)
            }
        }()
        next.ServeHTTP(w, r)
    })
}
```

---

## Errores Cruzando Goroutines

Un `recover` solo atrapa panics de **su propia** goroutine. Un panic que escapa de una goroutine sin `recover` tumba **todo el proceso** — el `recover` del middleware o del llamador no lo alcanza, porque está en otra pila.

**Regla:** toda goroutine que ejecuta trabajo arbitrario necesita, o bien su propio `recover` en la frontera, o bien un mecanismo que convierta panic en error antes de cruzar el boundary.

```go
// BAD: si process() hace panic, adiós proceso. Ningún recover externo lo captura.
go func() {
    process(job)
}()

// GOOD: recover dentro de la goroutine; el panic se convierte en error y se propaga
// por un canal con buffer 1 (nunca bloquea al emisor, no hay leak).
func safeGo(fn func() error) <-chan error {
    errc := make(chan error, 1)
    go func() {
        defer func() {
            if v := recover(); v != nil {
                errc <- fmt.Errorf("panic in goroutine: %v", v)
            }
        }()
        errc <- fn()
    }()
    return errc
}
```

Para propagar **errores** (no panics) desde múltiples goroutines con cancelación automática al primer fallo, usa `errgroup` — ver `references/concurrency-patterns.md`, sección Errgroup. `errgroup` no recupera panics: si tu `g.Go(...)` puede entrar en pánico, envuélvelo con un `recover` como el de arriba.

---

## Checklist

- [ ] Errores opacos por default; sentinel o typed solo cuando un llamador real ramifica
- [ ] Sentinel (`var ErrX`) para condiciones comparadas con `errors.Is`; typed (`errors.As`) solo cuando el llamador necesita **datos**
- [ ] Condiciones transversales (temporal, timeout, retryable) expuestas como comportamiento, no como tipo concreto
- [ ] Tipos de error con causa envuelta implementan `Unwrap()` para preservar la cadena
- [ ] `errors.Join` para validación multi-campo y cleanup de N recursos; nunca reemplaza a `%w`
- [ ] `panic` solo para bugs de programación o config imposible en el arranque — jamás como control de flujo
- [ ] `recover` únicamente en fronteras (handler HTTP, supervisor de goroutine)
- [ ] Toda goroutine con trabajo arbitrario tiene `recover` propio o convierte panic en error; errores entre goroutines se propagan con `errgroup` (ver `concurrency-patterns.md`)
