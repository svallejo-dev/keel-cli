# Capa de review Go/DDD

Se aplica **solo a los archivos `.go`** del diff, además de la rúbrica universal. Es el estándar
de `senior-go-idiomatic`, `ddd-go-idiomatic` y —si está instalada— `go-design-rules`.

## Cómo/cuándo invocar las compañeras vs el checklist inline

- **Si las skills están disponibles, invócalas** para mayor profundidad:
  - `senior-go-idiomatic` → idioma: errores, concurrencia, interfaces, layout, footguns.
  - `ddd-go-idiomatic` → modelo de dominio: value objects, entities, aggregates, repos, use cases.
  - `go-design-rules` → **OPCIONAL, puede no existir aún**. Si está, úsala para smells de diseño
    (Ley de Demeter, stringly-typed, globals, acoplamiento).
- **Si no están instaladas**, esta capa es **autocontenida**: usa los checklists inline de abajo.
  No dependas de que las compañeras existan. Cuando existan, profundizan; el checklist es el piso.

Mapea cada hallazgo Go a una severidad de la rúbrica (data race o secreto = blocker; error
tragado, límite de aggregate roto, archivo/tipo cajón o dos casos de uso en un archivo = major;
naming/interface pollution o exceso de comentarios narrativos = minor/nit).

## Archivos con intención y comentarios (detección rápida)

Corre esto antes del checklist; cada match es un hallazgo candidato que confirmas leyendo el archivo.

```bash
# 1. Archivos cajón dentro de un contexto (major). Excluye composition roots como
#    internal/handlers/handlers.go si solo monta rutas.
gh pr diff <n> --name-only | grep -E '/(service|services|usecase|usecases|use_cases|application|app|manager|helpers|utils|common)\.go$'
gh pr diff <n> --name-only | grep -E '/handlers?\.go$'      # ¿sirve varios endpoints?

# 2. Tipos cajón (major).
gh pr diff <n> | grep -E '^\+type (Service|UseCase|Manager|[A-Z][A-Za-z]*(Service|UseCase|Manager)) struct'

# 3. Dos casos de uso en un archivo (major): más de un método exportado con ctx sobre el
#    mismo receiver. Ignora adaptadores (store/repositorio) que implementan varios puertos.
for f in $(gh pr diff <n> --name-only | grep '\.go$' | grep -v _test.go); do
  c=$(grep -cE '^func \([a-z]+ \*?[A-Z][A-Za-z0-9_]*\) [A-Z][A-Za-z0-9_]*\(ctx context\.Context' "$f" 2>/dev/null)
  [ "${c:-0}" -gt 1 ] && echo "$f: $c métodos exportados con ctx"
done

# 4. Densidad de comentarios (minor si > ~15 %, o si hay banners/narración).
for f in $(gh pr diff <n> --name-only | grep '\.go$'); do
  awk -v f="$f" '/^[[:space:]]*\/\//{c++} NF{t++} END{if(t&&c*100/t>15) printf "%s: %d%% comentarios\n", f, c*100/t}' "$f"
done
gh pr diff <n> | grep -nE '^\+[[:space:]]*// ?[-=]{4,}'                 # banners
```

- [ ] **Un archivo por caso de uso, nombrado por su intención** (`login.go`, `search_users.go`).
      `service.go`/`usecase.go`/`manager.go`/`helpers.go`/`utils.go`/`common.go` dentro de un
      contexto, o un archivo con `Login()` y `Signup()`, es **major**: fix = un archivo y un struct
      por caso de uso (`type Login struct`, `NewLogin(...)`, `Execute(ctx, in LoginInput)
      (LoginOutput, error)`), con sus DTOs y puertos estrechos al lado.
- [ ] **Sin tipos cajón** `Service`/`UseCase`/`Manager` que agrupen operaciones (**major**).
- [ ] **Transporte por endpoint**: un `handler.go` con todos los endpoints del contexto es
      **major**, igual que un archivo cajón (fix: `<ctx>http/<caso>.go` + `routes.go`). Un composition root que solo monta
      rutas (`internal/handlers/handlers.go`) está bien.
- [ ] **Comentarios mínimos** (**minor**; **nit** si es aislado): narrar el código, banners
      `// ----`, doc comments que repiten el nombre, comentarios más largos que el código,
      código comentado, `TODO` sin dueño. Se quedan: package doc de 1 línea, doc de 1 línea en
      exportados que aporte algo, y el "por qué" no obvio.

## Checklist Go idiomático (derivado de `senior-go-idiomatic`)

- [ ] **Errores envueltos con `%w`**, no tragados: `return fmt.Errorf("op ctx=%v: %w", x, err)`.
      Nada de `if err != nil { return err }` que pierda contexto, ni `_ = err`, ni `log + continue`
      silencioso. Inspección con `errors.Is`/`errors.As`, nunca `==`.
- [ ] **`context.Context` primero** en toda función que hace I/O o concurrencia; se propaga a las
      llamadas downstream (no se guarda en structs, no se pasa `context.Background()` a media cadena).
- [ ] **Goroutines con fin claro** — cada `go func()` termina vía `context`, `done` channel o
      `WaitGroup`. Sin leaks (`time.After` en loop, canal sin cerrar, receiver eterno).
- [ ] **Interfaces pequeñas y consumer-defined** — definidas en el paquete que las usa, no junto a
      la implementación; "accept interfaces, return concrete types". Sin interface pollution
      (una interfaz por struct sin 2ª impl ni seam de test).
- [ ] **Sin paquetes-cajón**: `util`, `helper`, `common`, `shared`, `models`, `types`, `base`,
      `manager`. Cada tipo vive en el paquete del concepto que sirve.
- [ ] **Receivers consistentes** dentro de un tipo (todos value o todos pointer); nunca copiar un
      `sync.Mutex`/`sync.WaitGroup` por valor (usa pointer receiver).
- [ ] **Sin data races** — estado compartido protegido con `sync.Mutex`/canales; el código
      concurrente pasaría `go test -race`. Sin `concurrent map writes`.
- [ ] **Footguns**:
  - **Nil interface**: devolver un `*T` nil como `error` da una interfaz **no-nil**. Devuelve
    `nil` sin tipo.
  - **`append` aliasing**: reslicing + `append` puede pisar el backing array de otro slice; usa
    `s[a:b:c]` (three-index) o copia defensiva.
  - **Map concurrente**: escribir un `map` desde varias goroutines es panic fatal; sincroniza.
  - `range` copia el elemento (`for _, v := range xs { v.X = … }` no muta); indexa `xs[i]`.
  - `len(s)` cuenta bytes, no runas, en strings no-ASCII.

## Checklist DDD (derivado de `ddd-go-idiomatic`)

- [ ] **Value objects inmutables y validados en el constructor** — campo no-exportado +
      `NewX(...) (X, error)`, value receiver, "nunca actualiza: crea uno nuevo". Igualdad por valor
      (`==`, o `Equal` si contiene slice). Sin setters.
- [ ] **Entities con identidad, sin getters/setters** — pointer receiver, igualdad por **ID**
      (nunca `==` sobre el struct), métodos con **intención** (`user.Deactivate()`, no
      `user.SetStatus(...)`). El constructor valida la **integración** de VOs, no lo que cada VO ya
      valida. Nada de modelo anémico (datos + service gordo).
- [ ] **Límites de aggregate respetados** — se referencia a otros aggregates **por ID, no por
      puntero**; 1 aggregate por transacción; los campos no-exportados del root imponen el límite de
      consistencia (nadie muta el interior saltándose el root).
- [ ] **Dominio puro** — el paquete de dominio **no importa** `database/sql`, `net/http`, ni libs
      externas. El bloque `import` es el test de arquitectura: si la infra se filtró, es un hallazgo.
- [ ] **Interfaz de repositorio consumer-defined** — la define la capa de aplicación (use case), la
      implementa la infra, que **traduce errores del driver** (`sql.ErrNoRows`, `pgconn.PgError`) a
      errores de dominio en la frontera. El use case nunca ve tipos del driver.
- [ ] **DTO sin capa `mapper/`** — los `<Caso>Input`/`<Caso>Output` viven en el archivo del use case; la traducción se
      hace a mano, sin auto-mapper por reflection ni carpetas `dto/`/`mapper/` genéricas.
- [ ] **Carpeta por contexto, no por capa** — nada de `domain/`, `application/`, `infrastructure/`
      como layout; un paquete por bounded context dentro de `internal/`.

## Smells de diseño (derivado de `go-design-rules`, OPCIONAL)

Usa esta sección aunque `go-design-rules` no esté instalada; si lo está, invócala para el detalle.

- [ ] **Ley de Demeter** — train-wrecks / reach-through: `a.B().C().D().do()` o alcanzar campos
      internos de un objeto lejano acopla a su estructura. Pide un método en el objeto directo que
      encapsule la cadena ("tell, don't ask").
- [ ] **Stringly-typed / boolean-blindness** — pasar dominios como `string` o `bool` desnudos
      (`newUser(true, false, true)`) pierde tipos y significado. Sugiere un tipo con nombre
      (VO, enum con `iota`+`Stringer`) o structs de opciones en vez de flags posicionales.
- [ ] **Globals / singletons** — estado mutable a nivel de paquete, `init()` con efectos, singletons
      ocultos: rompen tests y concurrencia. Inyecta la dependencia.
- [ ] **`common/` de acoplamiento** — un paquete `common`/`shared` al que todos importan se
      convierte en un nudo de dependencias. Reparte cada tipo al paquete del concepto que sirve.

## Regla #0

Ante conflicto entre una regla DDD y una convención idiomática de Go, **gana Go idiomático**.
Un Java-ismo (interfaz por clase, `mapper/`, getters/setters, carpetas por capa) es un hallazgo
aunque "cumpla DDD".
