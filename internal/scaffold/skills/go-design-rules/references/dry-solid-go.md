# DRY & SOLID en Go — cuándo el canon OOP aplica y cuándo estorba

DRY y SOLID nacieron con la herencia (Smalltalk, Java, C#). Go no la tiene: usa
satisfacción implícita de interfaces y composición. Trasplantar el canon entero sin
traducir produce Java-ismos que **rompen** en Go — ciclos de import, interface pollution,
indirección que no paga su costo. Esta referencia separa lo que aplica tal cual, lo que
se reinterpreta y lo que se descarta. Compañeras opcionales (no enlazadas; este doc es
autocontenido): `senior-go-idiomatic` y `ddd-go-idiomatic`. Verificado con toolchain
1.24.7 (estándar: Go 1.26).

---

## Regla #0 — Ante conflicto principio ↔ Go, gana Go

Es el corazón de este archivo. Cuando un principio OOP (DRY, cualquier letra de SOLID,
getters/setters) choca con el idioma de Go, **gana Go, siempre**. El canon es heurística
de otra familia de lenguajes; Go optimiza lectura, compilación explícita y composición.

Corolario: en Go, DRY y SOLID se citan **con asterisco** — valen cuando reducen
acoplamiento real, estorban cuando lo inventan. El resto es la lista de asteriscos.

---

## 1. DRY subordinado al desacople

**Regla.** DRY no es "no dupliques código que se parece", es **no dupliques
conocimiento**. Dos fragmentos idénticos hoy que representan **decisiones distintas** no
son una duplicación: son una coincidencia. Deduplicarlos los ata a cambiar juntos.

**Por qué en Go.** El proverbio canónico (Rob Pike): *"a little copying is better than a
little dependency"*. Extraer lo que "se parece" de dos dominios sin relación a un paquete
`common/`, `shared/` o `base/` crea el acoplamiento que DRY quería evitar: ambos pasan a
depender de un tercero y un cambio en uno arrastra al otro. Ver
`references/coupling-behavior.md` (acoplamiento) y `references/dependencies.md` (copiar vs. depender).

### Criterio de decisión

> ¿Las dos copias cambiarían por la **misma razón**?
> **Sí** → es el mismo conocimiento; extráelo. **No** → déjalas separadas.

Es SRP disfrazado (§2, S) aplicado a la duplicación: si las razones de cambio difieren,
unificar es un error aunque el código sea idéntico al carácter.

```go
// BAD: un Validate compartido acopla user y payment.
package common

func Validate(name string, amount int) error {
	if name == "" {
		return errors.New("name required")
	}
	if amount < 0 {
		return errors.New("amount must be >= 0")
	}
	return nil
}
```

```go
// GOOD: cada dominio valida solo lo suyo.
package user

func (u User) Validate() error {
	if u.Name == "" {
		return errors.New("user: name required")
	}
	return nil
}
```

`payment.Validate` hace lo propio con `Amount` en su paquete: no comparte una línea con
`user` y nada las obliga a cambiar juntas. Cada dominio nombra su regla con su vocabulario
(`references/domain-naming.md`); mejor aún, el tipo hace el estado ilegal irrepresentable
y la validación sobra (`references/illegal-states.md`). `common.Validate` impide ambas.

---

## 2. SOLID selectivo

No las cinco letras pesan igual: dos son nativas, dos se reinterpretan, una casi desaparece.

| Principio | ¿Aplica en Go? | Cómo se expresa |
|---|---|---|
| **S** — SRP | Sí, tal cual | Un paquete/tipo con **una** razón para cambiar |
| **O** — OCP | Reinterpretado | Extensión por **composición e interfaces**, nunca por herencia |
| **L** — LSP | Casi irrelevante | Sin herencia se reduce a **respetar el contrato** de la interfaz |
| **I** — ISP | Sí, y es **nativo** | Interfaces pequeñas y **consumer-defined**; el más Go de todos |
| **D** — DIP | Sí | Depender de **abstracciones definidas por el consumidor**: accept interfaces + inyección |

### S — Single Responsibility

**Regla.** Un paquete o tipo tiene **una** razón para cambiar. En Go la unidad natural
de responsabilidad es el **paquete**, no la clase.

```go
type Config struct {
	Addr string
	TLS  bool
}

func ParseConfig(r io.Reader) (*Config, error) {
	cfg := &Config{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(k) == "addr" {
			cfg.Addr = strings.TrimSpace(v)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}
```

Si `ParseConfig` empezara a loguear, abrir conexiones o aplicar reglas de negocio,
tendría varias razones para cambiar: sepáralas en paquetes distintos.

### O — Open/Closed, reinterpretado

**Regla.** Abierto a extensión, cerrado a modificación — pero en Go la extensión es por
**composición e interfaces**, jamás por subclases (no existen). "Abierto vía interfaces,
no vía herencia."

```go
type Notifier interface {
	Notify(ctx context.Context, msg string) error
}

func Dispatch(ctx context.Context, ns []Notifier, msg string) error {
	for _, n := range ns {
		if err := n.Notify(ctx, msg); err != nil {
			return fmt.Errorf("dispatch: %w", err)
		}
	}
	return nil
}

type EmailNotifier struct{ From string }

func (e EmailNotifier) Notify(_ context.Context, msg string) error { return nil }

type SlackNotifier struct{ Channel string }

func (s SlackNotifier) Notify(_ context.Context, msg string) error { return nil }
```

Añadir un canal de notificación no toca `Dispatch`. La versión OOP de esto sería una
jerarquía `AbstractNotifier`; en Go es una interfaz de un método más tipos concretos.

### L — Liskov Substitution

**Regla.** Sin herencia, LSP casi no tiene dónde aplicarse. Su versión Go es: **respeta
el contrato de la interfaz**. Un tipo que satisface `io.Reader`, `error` o `sort.Interface`
debe cumplir su contrato documentado, no solo la firma de los métodos.

```go
// BAD: al agotarse devuelve (0, nil) en vez de io.EOF.
func (r *badReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, nil // debia ser io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}
```

```go
// GOOD: entrega bytes y luego io.EOF.
func (r *goodReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}
```

Con `badReader`, `io.Copy` y `bufio.Scanner` entran en busy-loop: la firma calza pero no se comporta como un `io.Reader`. El compilador verifica la firma; el contrato lo verificas tú (y los tests). La firma
que calca sin respetar el contrato es el LSP que Go sí puede violar.

### I — Interface Segregation

**Regla.** Interfaces pequeñas y **definidas por el consumidor**. Es el principio más Go
de todos: `io.Reader` tiene **un** método por diseño, y las interfaces viven donde se
**usan**, no donde se implementan.

```go
func WriteReport(w io.Writer, lines []string) error {
	for _, l := range lines {
		if _, err := fmt.Fprintln(w, l); err != nil {
			return fmt.Errorf("write report: %w", err)
		}
	}
	return nil
}
```

`WriteReport` acepta `*os.File`, `*bytes.Buffer`, un socket o un mock porque pide el
mínimo. Una interfaz de cinco métodos "por completitud" es el opuesto: nadie los usa todos.

### D — Dependency Inversion

**Regla.** Depende de abstracciones, no de concreciones — y en Go la abstracción la
**define el consumidor** y se **inyecta**. Es "accept interfaces" elevado a política de
arquitectura.

```go
// show_profile.go
type userRepo interface {
	ByID(ctx context.Context, id int64) (User, error)
}

type ShowProfile struct {
	users userRepo
}

func NewShowProfile(users userRepo) *ShowProfile {
	return &ShowProfile{users: users}
}
```

`ShowProfile` depende de `userRepo`, una abstracción que **él mismo** define, no del Postgres concreto. La interfaz vive en el archivo que la consume: el `*postgres.Store` concreto
la satisface sin importar al consumidor —cero ciclos, y el test inyecta un stub—. Por qué
el sentido de la dependencia importa: `references/coupling-behavior.md`.

---

## 3. El principio general: no importes el canon entero

Importar SOLID/DRY **completos**, con su bagaje de herencia (jerarquías de clases,
abstract factories, interfaz-por-clase, getters/setters ceremoniales), empuja a
Java-ismos que en Go se pagan caro:

| Java-ismo importado | Qué rompe en Go |
|---|---|
| Jerarquías de herencia | No existen; se emulan con embedding y salen frágiles |
| Interfaz-por-clase | Interface pollution: indirección sin un segundo implementador |
| Abstract factories / DI containers | Indirección y reflection donde bastaba un constructor |
| Getters/setters por defecto | Ruido; en Go el campo exportado o un método con nombre de dominio |
| `common/` / `base/` de utilidades | Acoplamiento entre módulos sin relación (§1) |

La política correcta es **selectiva**, no de bloque:

- **Toma tal cual** lo que ya es nativo: ISP y DIP (interfaces pequeñas, consumer-defined,
  inyectadas) y SRP a nivel de paquete.
- **Reinterpreta** OCP (composición, no subclases) y LSP (contrato de interfaz, no
  subtipado nominal).
- **Descarta** todo lo que exige herencia o inventa acoplamiento: jerarquías,
  interfaz-por-clase especulativa, dedup de conocimiento no relacionado.

El sesgo por defecto en Go es **menos abstracción, más código explícito**: una capa de
indirección se gana su lugar cuando hay un segundo implementador real o un test double
concreto, no antes. DRY y SOLID sirven al desacople, no al revés — y cuando chocan con
el idioma, Regla #0.

---

## Checklist

- [ ] Regla #0 interiorizada: ante conflicto principio ↔ Go, gana Go; DRY/SOLID se citan con asterisco
- [ ] Antes de deduplicar: ¿las dos copias cambian por la **misma razón**? Si no, quedan separadas
- [ ] Cero paquetes `common/`/`shared/`/`base/` que unan dominios sin relación
- [ ] SRP por paquete: una única razón para cambiar; parseo, logging y negocio separados
- [ ] OCP por interfaces + composición; añadir un comportamiento no modifica el dispatcher
- [ ] LSP = contrato de interfaz respetado (`io.Reader` devuelve `io.EOF`, no `(0, nil)`)
- [ ] ISP nativo: interfaces mínimas, definidas por el consumidor, sin interfaz-por-clase
- [ ] DIP: la interfaz la define y la inyecta el consumidor; el implementador no lo importa
- [ ] Sin Java-ismos: nada de jerarquías de herencia, abstract factories ni getters/setters ceremoniales
- [ ] La abstracción se añade cuando hay segundo implementador o test double real, no "por si acaso"
