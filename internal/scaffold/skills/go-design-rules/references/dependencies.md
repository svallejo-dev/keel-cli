# Dependencias — Go Design Rules

Las dependencias de un componente deben ser **visibles en su firma** y **sustituibles
desde fuera**. En Go esto no exige un framework de dependency injection ni un contenedor:
basta pasar valores por parámetro o constructor y declarar interfaces pequeñas en el
consumidor. El grafo queda explícito, testeable y sin nada oculto en el arranque.

**Regla #0 (principio rector):** ante conflicto entre un principio de diseño y un idiom
de Go, gana el idiom de Go. Ninguna regla de aquí justifica escribir Go antinatural
(p. ej. un contenedor DI reflectivo "porque el principio lo pide").

Esta referencia es autocontenida. Las skills `senior-go-idiomatic` (idioms de lenguaje) y
`ddd-go-idiomatic` (modelado táctico) son compañeras opcionales, no un prerequisito.
Snippets verificados con la toolchain 1.24.7 (estándar actual: Go 1.26). Ver también
`references/coupling-behavior.md`, `references/domain-naming.md`,
`references/illegal-states.md` y `references/dry-solid-go.md`.

---

## 1. Dependencias Explícitas

**Regla:** pasa cada dependencia por parámetro o constructor. Nunca la alcances a través
de un global, un singleton o un service locator.

**Por qué:** la firma se vuelve la documentación honesta de lo que el componente necesita.
`NewGreetUser(repo, clock)` declara su grafo completo; un `NewGreetUser()` que dentro lee
`db.Global` y llama `time.Now()` miente sobre sus requisitos y no es sustituible.

```go
// greet_user.go
type GreetUser struct {
	repo  Repo
	clock Clock
}

func NewGreetUser(repo Repo, clock Clock) *GreetUser {
	return &GreetUser{repo: repo, clock: clock}
}

func (uc *GreetUser) Execute(ctx context.Context, id int64) (string, error) {
	u, err := uc.repo.UserByID(ctx, id)
	if err != nil {
		return "", fmt.Errorf("greet user: %w", err)
	}
	return fmt.Sprintf("hi %s at %s", u.Name, uc.clock.Now().Format(time.RFC3339)), nil
}
```

Cada colaborador se reemplaza por un doble en tests sin tocar `GreetUser`. El struct
nombra **un** caso de uso y vive en su archivo (`greet_user.go`); un `Service` con varios
métodos sería un tipo cajón (ver `references/domain-naming.md` §7).

---

## 2. Cero Globals Mutables, Singletons o Service Locator

**Regla:** ningún `var` mutable a nivel de paquete usado como dependencia. Ni singletons,
ni service locator (una función global `Get[T]()` que resuelve colaboradores).

**Por qué (concreto de Go):** un `var` global mutable compartido entre goroutines es un
**data race real** — falla `go test -race` y el detector aborta el proceso. Además rompe
el aislamiento de tests: el estado sobrevive entre casos, así que el resultado depende del
**orden de ejecución** y los tests no se pueden paralelizar.

```go
// BAD — global mutable + funciones que lo usan a escondidas.
var DB *sql.DB // cualquier goroutine lo lee y escribe

func GetUser(id int64) (User, error) {
	var u User
	err := DB.QueryRow("SELECT id, name FROM users WHERE id = $1", id).
		Scan(&u.ID, &u.Name) // depende de que alguien haya inicializado DB antes
	return u, err
}
```

```go
// GOOD — inyecta *sql.DB (o una interfaz) por constructor. Cero estado global.
type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db} // sus métodos (UserByID, ...) usan s.db, nunca un global
}
```

### Por qué el global rompe el test

Los tests que preparan escenario mutando el global quedan acoplados entre sí:

```go
var reqCount int // estado global que los tests deben tocar

// BAD: dos tests mutan el mismo global → el resultado depende del ORDEN.
func TestFirst(t *testing.T) {
	reqCount++
	if reqCount != 1 {
		t.Fatalf("got %d, want 1", reqCount) // falla si TestSecond corrió antes
	}
}

func TestSecond(t *testing.T) {
	reqCount++
	if reqCount != 2 { // acoplado a que TestFirst haya corrido justo antes
		t.Fatalf("got %d, want 2", reqCount)
	}
}
```

Y si dos goroutines tocan `reqCount` sin sincronizar es un data race que `go test -race`
reporta y aborta. Con la dependencia inyectada, cada test construye su propio `Store`:
estado fresco por caso, aislable y paralelizable.

---

## 3. Package-Level State que SÍ es Aceptable

No todo estado a nivel de paquete es un antipatrón. La línea es nítida: **inmutable o de
solo-init, nunca estado mutable compartido de negocio**.

| Aceptable | Por qué es seguro |
|---|---|
| `const` | Inmutable por definición; no hay race posible. |
| Tabla `var x = [...]{...}` no mutada tras la carga | De solo lectura; se comparte entre goroutines sin sincronizar. |
| `sync.Once` para init idempotente | Un solo escritor, una sola vez, con barrera de memoria. |

```go
// OK: constante — inmutable.
const MaxRetries = 3

// OK: tabla inmutable de solo lectura. `[...]` fija el tamaño en compilación.
var httpVerbs = [...]string{"GET", "POST", "PUT", "DELETE"}

// OK: sync.Once para init controlado (no estado mutable de negocio).
var (
	slugOnce sync.Once
	slugRxp  *regexp.Regexp
)

func slugRegexp() *regexp.Regexp {
	slugOnce.Do(func() {
		slugRxp = regexp.MustCompile(`[a-z0-9-]+`)
	})
	return slugRxp
}
```

El matiz honesto: un `var` de configuración "solo se escribe una vez al arrancar" es una
trampa — nada en el tipo lo garantiza y el siguiente commit lo mutará en caliente. Si debe
cambiar en runtime, es una dependencia inyectable, no un global.

---

## 4. Dependency Injection Idiomática sin Framework

**Regla:** en Go, "inyectar" es solo pasar interfaces **pequeñas definidas en el
consumidor**. No necesitas un contenedor DI ni tags de anotación; el compilador conecta
todo por implicit satisfaction.

**Por qué:** la interfaz mínima pertenece al lado que **depende** de la abstracción. El
consumidor declara los uno o dos métodos que usa; el implementador exporta un tipo
concreto y lo satisface sin importar al consumidor.

```go
type Repo interface {
	UserByID(ctx context.Context, id int64) (*User, error)
}
```

`GreetUser` (sección 1) recibe este `Repo`. `*postgres.Store` (sección 2) lo
satisface sin declararlo. En el test, un `stubRepo` con la misma firma sustituye a
Postgres sin base de datos. Eso es toda la "inyección" que Go requiere.

---

## 5. Reloj y Aleatoriedad como Dependencias

**Regla:** inyecta un `Clock` (o un `func() time.Time`) y una fuente de `rand`. No llames
`time.Now()` ni al `rand` global directamente dentro de la lógica.

**Por qué:** `time.Now()` y el rand global son entradas no deterministas incrustadas. Un
test que verifica "expira en 1h" no puede fijar el resultado si la lógica lee el reloj del
sistema. Inyectadas, el test las congela.

```go
type Clock interface {
	Now() time.Time
}

// fixedClock congela el tiempo en tests; en producción se pasa un reloj real.
type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

// TokenIssuer recibe tiempo y aleatoriedad; no los toma del entorno global.
type TokenIssuer struct {
	clock Clock
	rng   *rand.Rand
}

func NewTokenIssuer(clock Clock, rng *rand.Rand) *TokenIssuer {
	return &TokenIssuer{clock: clock, rng: rng}
}

func (ti *TokenIssuer) Issue(ttl time.Duration) (id int64, expires time.Time) {
	return ti.rng.Int63(), ti.clock.Now().Add(ttl)
}
```

El test se vuelve determinista con un reloj fijo y una semilla fija:

```go
func TestIssueDeterministic(t *testing.T) {
	clock := fixedClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	rng := rand.New(rand.NewSource(1)) // semilla fija → secuencia reproducible

	ti := NewTokenIssuer(clock, rng)
	_, exp := ti.Issue(1 * time.Hour)

	if want := time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC); !exp.Equal(want) {
		t.Fatalf("exp = %v, want %v", exp, want)
	}
}
```

Para casos triviales, un `func() time.Time` como campo es más ligero que una interfaz
(`s.now()` en vez de `s.clock.Now()`). Cualquiera de los dos evita el acoplamiento al
reloj del sistema.

---

## 6. Wiring en un Solo Lugar

**Regla:** el ensamblado del grafo vive en la entrada (`main` / `cmd`). El resto del código
no conoce a `main` ni sabe cómo se construye nada; recibe sus colaboradores ya resueltos.

**Por qué:** concentrar el `new` de todo en un punto hace el grafo auditable de un vistazo
y evita que la lógica de negocio dependa del arranque. La dirección de imports es
`main → paquetes`, nunca al revés.

```go
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func main() {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	store := postgres.NewStore(db)
	greetUser := user.NewGreetUser(store, systemClock{})

	msg, err := greetUser.Execute(context.Background(), 1)
	if err != nil {
		log.Fatalf("greeting: %v", err)
	}
	log.Println(msg)
}
```

Cuando el grafo crece sigue siendo composición manual en `main` (o `cmd/app`). Si se
vuelve inmanejable, `google/wire` genera ese mismo código en compilación, sin reflection
ni contenedor en runtime. La regla no cambia: el wiring vive en la entrada.

---

## Checklist

- [ ] Cada dependencia se pasa por parámetro o constructor; la firma no esconde requisitos
- [ ] Cero `var` mutables a nivel de paquete como dependencia; sin singletons ni service locator
- [ ] Ningún global compartido entre goroutines (pasa `go test -race` y no rompe el aislamiento de tests)
- [ ] El package-level state que existe es `const`, tabla inmutable o `sync.Once`, nunca estado mutable de negocio
- [ ] Las interfaces de dependencia son mínimas y se definen en el **consumidor**, no en el implementador
- [ ] `Clock`/`func() time.Time` y la fuente de `rand` están inyectados; la lógica no llama `time.Now()` ni al rand global
- [ ] Todo el wiring del grafo vive en `main`/`cmd`; ningún paquete de dominio importa la entrada
- [ ] La inyección es Go plano (pasar interfaces), sin contenedor DI reflectivo (Regla #0: gana el idiom de Go)
