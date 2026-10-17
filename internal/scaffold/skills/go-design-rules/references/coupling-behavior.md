# Acoplamiento & Comportamiento — Go Design Rules Reference

Tres reglas clásicas de diseño OOP —Ley de Demeter, Tell-Don't-Ask y Command–Query
Separation— reinterpretadas para Go. No se aplican como canon: se aplican con su marco
Go y con la TRAMPA donde el canon literal se desalinea. Estándar hoy Go 1.26; snippets
verificados contra el toolchain 1.24.7.

> **Regla #0 (permea todo lo demás).** Ante conflicto entre un principio de diseño
> (OOP/DDD) y una idiom de Go, **gana el idiom de Go**. Estas reglas son heurísticas de
> acoplamiento, no dogmas: cuando chocan con `gofmt`, con la stdlib componible o con
> "accept interfaces, return concrete types", la que se dobla es la regla de diseño.

Skills compañeras **opcionales**: `senior-go-idiomatic` (estilo e idioms de base) y
`ddd-go-idiomatic` (modelado táctico). Esta referencia es autocontenida; no dependes de
ellas para leerla.

---

## 1. Ley de Demeter — no atravieses objetos ajenos

**Regla.** Un método debería hablar solo con sus vecinos inmediatos: sus propios campos,
sus parámetros y lo que él mismo crea. No con lo que un colaborador te *devuelve*.

**Por qué.** El *train-wreck* `a.B().C().D()` te acopla a **todo el grafo intermedio**.
Si cambia la estructura interna de `B` o de `C`, tu código —que ni siquiera los
menciona— se rompe. Estás dependiendo de la forma de tres tipos para leer un dato.

```go
type Address struct{ zip ZIP }

func (a Address) ZIP() ZIP { return a.zip }

type Customer struct{ addr Address }

func (c Customer) Address() Address { return c.addr }

type Order struct{ customer Customer }
```

```go
// BAD: train-wreck. Dependes de la estructura interna de Order, Customer Y Address.
// Renombrar Address.ZIP a Address.PostalCode rompe este caller lejano.
func shipBad(o Order) ZIP {
	return o.Customer().Address().ZIP()
}

// GOOD: Order expone el dato que el caller necesita; el grafo queda encapsulado.
func (o Order) ShippingZIP() ZIP { return o.customer.addr.zip }

func shipGood(o Order) ZIP {
	return o.ShippingZIP()
}
```

Dos salidas idiomáticas del train-wreck:

- **Tell-Don't-Ask** (§3): que el objeto raíz exponga la operación (`o.ShippingZIP()`),
  no la cadena de acceso.
- **Pasa solo el dato**: si una función solo necesita el ZIP, su firma es `f(zip ZIP)`,
  no `f(o Order)`. La interfaz mínima al punto de uso reduce el acoplamiento sin añadir
  métodos. Ver `references/dependencies.md`.

---

## 2. La trampa — LoD NO es "la regla de un solo punto"

Aquí es donde el canon se desalinea de Go. Demeter se malinterpreta como "no más de un
punto por expresión". Aplicado literal, eso **prohíbe la mitad de la stdlib** y viola la
Regla #0. La regla real es más estrecha:

> No dependas de la **estructura interna** de lo que te devuelven **tus colaboradores**.

Encadenar sobre valores que **posees**, sobre la **stdlib componible**, o sobre un
**builder que tú construyes** no viola nada: no estás atravesando el estado privado de un
colaborador, estás transformando un valor propio.

```go
// Ninguno de estos viola LoD, y los cuatro son idiomáticos:
func normalize(s string) string        { return strings.ToLower(strings.TrimSpace(s)) }
func copyAll(d io.Writer, s io.Reader) (int64, error) { return io.Copy(d, s) }
func isMissing(err error) bool         { return errors.Is(err, ErrNotFound) }

func build(parts []string) string {
	var b strings.Builder // lo POSEES: escribir sobre él es fluent, no train-wreck
	for _, p := range parts {
		b.WriteString(p)
	}
	return b.String()
}
```

Functional options son el caso extremo: encadenas mutaciones sobre el struct que estás
construyendo, no sobre el estado de un tercero.

```go
func NewServer(opts ...Option) *Server {
	s := &Server{addr: ":8080"}
	for _, opt := range opts {
		opt(s) // configuras lo que creas; sigue siendo tuyo
	}
	return s
}
```

### Viola / no viola

| Expresión | ¿Viola LoD? | Por qué |
| --- | --- | --- |
| `order.Customer().Address().ZIP()` | **Sí** | Navegas la estructura de 3 colaboradores |
| `strings.ToLower(strings.TrimSpace(s))` | No | Transformación de un valor propio, stdlib pura |
| `io.Copy(dst, src)` | No | Composición de interfaces pequeñas, sin estado ajeno |
| `errors.Is(err, ErrX)` | No | Consulta semántica; no inspecciona la forma del error |
| `b.WriteString(x)` sobre tu `strings.Builder` | No | El builder es tuyo (fluent interface) |
| `opt(s)` en functional options | No | Configuras el objeto que construyes |
| `req.Header.Get("...")` (dato inmediato) | Límite | OK si `Header` es tu contrato; mal si navegas 3 niveles |

Criterio operativo: cuenta **grafos de propiedad cruzados**, no puntos. Un punto que
cruza hacia el interior de un colaborador es el problema; diez puntos sobre valores
tuyos, no.

---

## 3. Tell, Don't Ask — pide acción, no estado

**Regla.** No extraigas el estado de un objeto para decidir por él afuera. Pídele que
actúe; él conoce y **protege su propia invariante**.

**Por qué.** El patrón *ask* (leer estado → decidir → escribir estado) dispersa la lógica
del objeto entre todos sus callers. Cada uno puede olvidar una comprobación, y la
invariante deja de estar garantizada en un solo lugar. Es la hermana **conductual** de
Demeter: el *ask* casi siempre precede a un train-wreck.

```go
type Account struct{ balance int64 }

func (a *Account) Balance() int64     { return a.balance }
func (a *Account) SetBalance(v int64) { a.balance = v }
```

```go
// BAD: extrae el estado, decide afuera, reescribe. La invariante "balance >= 0"
// vive en el caller, no en Account. Otro caller la olvidará.
func withdrawBad(a *Account, amt int64) {
	if a.Balance() >= amt {
		a.SetBalance(a.Balance() - amt)
	}
}

// GOOD: la cuenta hace el trabajo y protege su invariante; el fallo es un error.
func (a *Account) Withdraw(amt int64) error {
	if amt > a.balance {
		return ErrInsufficientFunds
	}
	a.balance -= amt
	return nil
}
```

El caller pasa de un `if` frágil a `if err := acct.Withdraw(amt); err != nil { ... }`. El
`error` es el canal idiomático para señalar el rechazo de la invariante.

### La trampa (para no desalinear a Go)

Tell-Don't-Ask **no** significa "prohibido tener getters". Go los usa constantemente y de
forma idiomática: serializar (`json.Marshal` lee campos exportados), loggear, tests que
inspeccionan resultado. La regla ataca el patrón **read-decide-write sobre estado que
otro objeto debería custodiar**, no la lectura per se. Un getter de dato inmutable
(`u.Name()`) es lícito; un getter usado para reconstruir afuera una decisión que rompe
una invariante del objeto, no. Modelar el estado para que el objeto no *pueda* quedar
inválido es el paso siguiente: `references/illegal-states.md`.

---

## 4. Command–Query Separation (CQS)

**Regla.** Un método o **muta** (command) o **consulta** (query), nunca ambos calladamente.

- **Command**: cambia estado. En Go, señala el resultado con `error` (o nada). Nombre en
  imperativo: `Load`, `Withdraw`, `Close`.
- **Query**: retorna un valor **sin efectos observables**. Segura de llamar N veces,
  reordenar o cachear. Nombre nominal: `Values`, `Balance`.

**Por qué.** Una query con efecto secundario escondido es intratable: no puedes razonar
sobre ella (llamarla dos veces da resultados distintos) ni testearla en aislamiento.

```go
// BAD: un "getter" que muta perezosamente y devuelve. Query en la firma, command en
// la implementación. Llamarlo bajo el mutex hace side effects invisibles.
type lazyConfig struct {
	mu     sync.Mutex
	loaded bool
	values map[string]string
}

func (c *lazyConfig) Values() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.loaded {
		c.values = loadFromDisk() // command escondido dentro de una query
		c.loaded = true
	}
	return c.values
}
```

```go
// GOOD: command explícito (Load, devuelve error) separado de query pura (Values).
type Config struct {
	values map[string]string
}

func (c *Config) Load() error { c.values = loadFromDisk(); return nil } // command
func (c *Config) Values() map[string]string { return c.values }        // query pura
```

Beneficio directo: `Load()` se testea por su efecto (¿pobló `values`?, ¿retornó error?);
`Values()` se testea por su retorno, sin montar el disco. Separadas, cada una es trivial.

### La excepción idiomática (nómbrala)

Go tiene dos patrones que **combinan legítimamente** consulta y mutación —o consulta y un
segundo valor— y son canónicos:

```go
// comma-ok: query PURA que devuelve dos valores (valor + presencia). No muta nada;
// los dos retornos no violan CQS.
func lookup(m map[string]int, k string) (int, bool) {
	v, ok := m[k]
	return v, ok
}

// pop: consulta el tope Y lo quita. Muta y retorna a propósito, con la firma honesta
// (T, bool). Aceptado porque el efecto ES la semántica de la operación.
func (s *Stack[T]) Pop() (T, bool) {
	var zero T
	if len(s.items) == 0 {
		return zero, false
	}
	top := s.items[len(s.items)-1]
	s.items = s.items[:len(s.items)-1]
	return top, true
}
```

La diferencia con el `BAD` de arriba: en `Pop` el efecto está **en el nombre y la firma**
(`Pop` grita "quito algo"); en `lazyConfig.Values` está **oculto** tras un nombre de
query. CQS castiga el efecto **callado**, no el declarado.

---

## 5. Cómo se relacionan las tres

Dos ejes distintos; no las confundas:

| Regla | Eje | Qué ataca |
| --- | --- | --- |
| Ley de Demeter | Acoplamiento (estructural) | Depender de la **forma** del grafo ajeno |
| Tell, Don't Ask | Acoplamiento (conductual) | Sacar estado para **decidir afuera** |
| CQS | Claridad de contrato | Mezclar **command y query** calladamente |

- **Demeter y Tell-Don't-Ask son primos**: ambas reducen acoplamiento y suelen aparecer
  juntas. Un train-wreck (`a.B().C()`) casi siempre es un *ask* disfrazado —lees estado
  ajeno para decidir— y la cura es la misma: mover el comportamiento al objeto raíz
  (`a.DoX()`). Arréglalo con Tell-Don't-Ask y el train-wreck desaparece de regalo.
- **CQS es ortogonal**: no habla de acoplamiento sino de la honestidad de cada método.
  Un `Withdraw(amt) error` perfectamente encapsulado (respeta Demeter y Tell-Don't-Ask)
  igual puede violar CQS si además devuelve el nuevo balance en silencio y lo cachea.

Las tres, bajo la Regla #0, se subordinan a los idioms de Go: si aplicar una obliga a
una interfaz especulativa, a esconder un tipo concreto tras una fachada, o a pelear con
la stdlib componible, la regla cede. Para el reparto entre dominio y aplicación ver
`references/domain-naming.md`; para no sobre-abstraer en nombre de SOLID,
`references/dry-solid-go.md`.

---

## Checklist

- [ ] Sin train-wrecks `a.B().C().D()` que naveguen la estructura de colaboradores ajenos
- [ ] El objeto raíz expone la operación (`order.ShippingZIP()`), o pasas solo el dato que necesitas
- [ ] Encadenar sobre stdlib componible, un builder propio o functional options NO cuenta como violación de Demeter
- [ ] Cuentas grafos de propiedad cruzados, no puntos: LoD ≠ "un solo punto"
- [ ] El comportamiento vive en el objeto que custodia la invariante (`acct.Withdraw`), no en el caller
- [ ] El rechazo de una invariante se señala con `error`, no con un `if` en el caller
- [ ] Getters de lectura pura (serializar, loggear, tests) siguen siendo lícitos; solo se ataca read-decide-write ajeno
- [ ] Cada método o muta (command, devuelve `error`) o consulta (query pura), nunca ambos calladamente
- [ ] Las queries son seguras de reordenar/repetir/cachear: sin efectos ocultos
- [ ] Las excepciones comma-ok y pop declaran su efecto en el nombre y la firma
- [ ] Ninguna regla se aplica a costa de un idiom de Go (Regla #0)
