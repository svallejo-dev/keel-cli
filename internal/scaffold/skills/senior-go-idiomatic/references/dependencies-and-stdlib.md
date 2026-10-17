# Dependencies & Stdlib — Go Idiomatic Reference

## Filosofía stdlib-first

La stdlib no es un fallback — es la primera respuesta. Go se diseñó "batteries included": `net/http` es un servidor de producción, `encoding/json` parsea sin dependencias, `crypto/*` cubre TLS, AEAD y hashing, `log/slog` (Go 1.21+) hace logging estructurado. Antes de `go get`, la pregunta es siempre: **¿ya lo resuelve la stdlib o el sub-repo oficial `golang.org/x/...`?**

Dos propiedades hacen a la stdlib estructuralmente superior a cualquier dep:

- **Go 1 compatibility promise** — el código que compila hoy compilará en cada release de Go 1.x. La stdlib no rompe tu build; una dep de terceros no te debe nada.
- **Cero costo de supply-chain** — no añade nada al grafo de módulos, no tiene CVEs que no sean los del propio toolchain, no depende de que un maintainer siga vivo.

Regla mental: **cada dependencia es un pasivo, no un activo.** Un activo produce valor; un pasivo hay que mantenerlo, auditarlo y parchearlo para siempre. La dep se justifica solo cuando el valor que aporta supera ese pasivo permanente.

---

## El costo real de una dependencia

Una línea en `require` no es "una" dependencia. Es todo lo que arrastra:

| Costo | Qué significa | Cómo se paga |
|---|---|---|
| Transitive deps | Cada dep trae las suyas — un grafo, no un nodo | `go mod graph` explota; builds más lentos |
| Superficie de CVE | Más código de terceros = más vulnerabilidades que te afectan | `govulncheck` reporta lo que tú ni escribiste |
| Bugs ajenos | Sus bugs son tus bugs en producción | Debugging en código que no controlas |
| Mantenimiento | ¿El repo sigue vivo? ¿Un solo maintainer? | Fork de emergencia cuando lo abandonan |
| Tiempo de build | Cada módulo se descarga, verifica y compila | CI más lento, feedback loop más largo |
| Breaking changes | Un `v2` ajeno rompe tu API sin avisar | Migraciones no planificadas |

El caso canónico: añades una lib de "utils" de 200 líneas y `go mod graph` revela que trajo 40 módulos transitivos, uno de ellos sin commits en tres años. Ese módulo abandonado es ahora **tu** responsabilidad de seguridad.

---

## Cómo evaluar una dep antes de añadirla

Checklist de decisión — si alguna respuesta es "no" o "no sé", para y reconsidera:

- [ ] **¿Lo resuelve la stdlib o `golang.org/x/...`?** Si sí, no hay debate.
- [ ] **¿Cuántas transitive deps arrastra?** Cuéntalas con `go mod graph` en un módulo de prueba antes de comprometerte.
- [ ] **¿Está viva?** Última release y último commit recientes; issues abiertos con respuesta del maintainer.
- [ ] **¿Bus factor > 1?** Un solo maintainer es un riesgo de abandono y de supply-chain (cuenta comprometida = tu build comprometido).
- [ ] **¿Licencia compatible?** MIT / BSD / Apache-2.0 son seguras; GPL/AGPL contaminan según tu distribución.
- [ ] **¿API estable v1+?** Un módulo en `v0.x` declara explícitamente que puede romper compat en cualquier minor.

```bash
# Sondear una dep en un módulo desechable ANTES de añadirla al proyecto real
mkdir -p /tmp/probe && cd /tmp/probe && go mod init probe
go get github.com/some/candidate@latest   # descárgala aislada
go mod graph | wc -l                       # ¿cuántas aristas transitivas?
go mod graph | awk '{print $2}' | sort -u  # lista de módulos que entran a tu build
```

---

## Herramientas de auditoría

Comandos reales, todos verificados. Ninguno modifica el proyecto salvo `tidy`, `vendor` y `go get`.

```bash
go mod why golang.org/x/sync/errgroup   # POR QUÉ está en el grafo: cadena de import que la trae
go mod why -m github.com/foo/bar        # -m razona a nivel módulo, no paquete
go mod graph                            # grafo completo de dependencias (origen -> destino)
go mod tidy                             # añade lo usado, elimina lo no usado; sincroniza go.sum
go mod verify                           # confirma que los módulos en caché no fueron alterados
go list -m all                          # lista plana de todos los módulos del build
go list -m -u all                       # igual, marcando upgrades disponibles [available]
go mod download                         # baja el grafo a la caché sin construir
```

Escaneo de vulnerabilidades — `govulncheck` cruza tu código contra la Go Vulnerability Database y, crucialmente, solo reporta CVEs que tu código **realmente llama** (analiza call graph, no solo el `require`):

```bash
go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck ./...
# Salida típica:
#   Your code is affected by 0 vulnerabilities.
#   This scan also found 3 vulnerabilities in packages you import ... but your code
#   doesn't appear to call these  ← ruido filtrado por análisis de reachability
```

Blindaje de CI — que el build **falle** si `go.mod`/`go.sum` no están sincronizados, en vez de mutarlos silenciosamente:

```bash
GOFLAGS=-mod=readonly go build ./...   # prohíbe modificaciones implícitas de go.mod en el build
```

### Minimal Version Selection (MVS)

MVS (módulos, Go 1.11+): Go elige para cada dependencia la **versión mínima** que satisface todos los `require` del grafo — nunca "la más nueva disponible". Consecuencia directa: **el build es una función determinista de `go.mod`** — el mismo `go.mod` produce el mismo grafo en cualquier máquina y en cualquier momento, sin resolución "sorpresa" en cada CI ni rangos que floten. Reproducibilidad por construcción, no por lockfile de rangos.

---

## "A little copying is better than a little dependency"

Proverbio de Go (Rob Pike). Traer un módulo entero — con sus transitivas, su CVE surface y su maintainer — para usar 30 líneas es un mal negocio. A veces copiar esas 30 líneas, testeadas y bajo tu control, es estrictamente superior.

```go
// BAD: go get github.com/foo/sliceutil  → trae 40 transitivas para esto
keys := sliceutil.MapKeys(m)

// GOOD: 6 líneas copiadas, cero deps, cero superficie de ataque, tuyas para siempre
func keys[K comparable, V any](m map[K]V) []K {
	out := make([]K, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
```

Criterio para copiar en vez de depender:

- El código es **pequeño y estable** (algoritmo cerrado, no un dominio que evoluciona).
- Puedes **testearlo tú** por completo — al copiar, copias también el test.
- La dep aportaría **más transitivas que líneas** te ahorra.
- **No** es crypto, no es un parser de un estándar, no es correctness-critical de un dominio que no dominas (ver abajo).

Atribuye la fuente y su licencia en un comentario. Copiar no es plagiar; ocultar el origen sí.

---

## Evitar frameworks: library, not framework

Go prefiere librerías pequeñas y componibles sobre frameworks que **toman el control del flujo** (inversión de control, magia por reflection, un runtime que te llama a ti). Una librería la llamas tú; un framework te llama a él y dicta tu arquitectura.

```go
// GOOD: net/http (stdlib) + un router pequeño y explícito (chi). Tú controlas el flujo.
r := chi.NewRouter()
r.Use(middleware.Logger)                 // middleware es solo func(http.Handler) http.Handler
r.Get("/users/{id}", getUser)            // el handler es un http.HandlerFunc normal
http.ListenAndServe(":8080", r)          // el server sigue siendo el de la stdlib
```

Contraste con el mundo de ORM y mega-frameworks:

| Necesidad | GOOD (library) | BAD (framework) |
|---|---|---|
| Routing HTTP | `net/http` + `chi` (router fino, `http.Handler` estándar) | Mega-framework que envuelve request/response en tipos propios |
| Acceso a datos | `database/sql` / `pgx` + `sqlc` (SQL real, código generado) | ORM pesado con reflection en runtime y queries opacas |
| Config | `flag` / `env` + structs explícitas | Framework de config con lifecycle y DI container |

El ORM cobra su factura en runtime: reflection en cada query, N+1 invisibles, y SQL que no ves hasta que el `EXPLAIN` te sorprende en producción. `sqlc` mueve todo eso a compile-time — escribes SQL, obtienes Go type-safe, cero reflection. El router fino deja tus handlers como `http.HandlerFunc` puros, testeables con `httptest` sin arrancar nada.

---

## Cuándo SÍ tomar la dep

Copiar y evitar frameworks no es dogma anti-dependencias. Hay categorías donde tomar la dep es la decisión de ingeniería correcta:

- **Drivers de base de datos** — `pgx` habla el wire protocol de Postgres. No lo reimplementas.
- **Primitivas de crypto que no debes escribir tú** — argon2, bcrypt, JWT firmado, X.509. Un bug tuyo aquí es un CVE. Usa `golang.org/x/crypto` o libs auditadas.
- **Parsers de estándares complejos** — `protobuf`, `yaml`, `toml`. Reimplementar el estándar es reintroducir sus edge cases.
- **Utilidades correct-by-design del sub-repo oficial** — `golang.org/x/*` lo mantiene el equipo de Go con el mismo rigor que la stdlib, solo fuera del compatibility promise estricto: `x/sync/errgroup`, `x/time/rate` (token bucket), `x/sync/singleflight` (colapsa llamadas duplicadas concurrentes).

```go
import (
	"golang.org/x/sync/errgroup"      // goroutines con propagación de error + cancelación
	"golang.org/x/sync/singleflight" // dedup de llamadas concurrentes idénticas
	"golang.org/x/time/rate"         // rate limiting con token bucket
)
```

### Deps "bendecidas" que esta skill ya usa

Aprobadas: probadas, mantenidas, con grafo transitivo controlado. Añádelas sin ceremonia.

| Módulo | Rol | Por qué se bendice |
|---|---|---|
| `github.com/jackc/pgx/v5` | Driver Postgres de producción | Pool nativo, Copy protocol, tipos nativos |
| `sqlc` (herramienta) | SQL → Go type-safe | Genera en compile-time; cero reflection en runtime |
| `golang.org/x/sync/errgroup` | Goroutines con error + context | Sub-repo oficial; correct-by-design |
| `github.com/golang-migrate/migrate/v4` | Migrations versionadas | Estándar de facto; embebible con `go:embed` |
| `github.com/testcontainers/testcontainers-go` | Postgres real en tests de integración | Fidelidad total vs. mock; ver `references/testing-advanced.md` |
| `go.uber.org/goleak` | Detección de goroutine leaks en tests | Un assert en teardown atrapa toda una clase de bugs |

---

## Higiene de módulos

### go.sum es el lockfile de integridad

`go.mod` declara qué versiones; `go.sum` fija el **hash criptográfico** de cada módulo y de cada `go.mod` transitivo. Si el contenido descargado no coincide con el hash registrado, el build **falla** — así detectas un módulo alterado en tránsito o en un proxy comprometido. `go.sum` **siempre** se commitea.

```bash
go mod verify   # ¿la caché coincide con go.sum? -> "all modules verified"
go mod tidy     # única forma correcta de editar require/go.sum: nunca a mano
```

### go.mod anotado

```go.mod
module github.com/acme/billing

go 1.26                      // versión mínima del lenguaje que el código exige
toolchain go1.26.0           // toolchain con la que buildear (go la descarga si falta)

require (
	github.com/jackc/pgx/v5 v5.7.6          // versión exacta, fijada por MVS
	golang.org/x/sync v0.22.0
)

require (
	github.com/jackc/puddle/v2 v2.2.2 // indirect  ← transitiva, no la importas tú directamente
)
```

`// indirect` marca una transitiva que no aparece en tus `import`. `go mod tidy` mantiene esta sección; no la edites a mano.

### Vendoring — cuándo sí

`go mod vendor` copia el grafo entero a `./vendor/`. Úsalo cuando el build debe ser **hermético** — CI sin acceso a red, auditorías que exigen el árbol de deps commiteado, o para no depender de la disponibilidad de un proxy. Coste: un `vendor/` grande en el repo y `go mod vendor` tras cada cambio de deps.

```bash
go mod vendor            # materializa deps en ./vendor
go build -mod=vendor ./... # construye solo desde vendor, ignora la caché y la red
```

### Fijar y renovar con cuidado

```bash
go get github.com/jackc/pgx/v5@v5.7.6   # fija una versión exacta (reproducibilidad)
go list -m -u all                       # audita qué upgrades hay disponibles
go get -u ./...                          # sube minors/patches — SIEMPRE seguido de:
go test -race ./... && govulncheck ./... # nada se mergea sin pasar esto
```

Renovar es una acción deliberada con tests detrás, no un `-u` a ciegas antes de un release.

### GOPROXY / GOSUMDB en una línea

`GOPROXY` (default `https://proxy.golang.org,direct`) sirve los módulos; `GOSUMDB` (default `sum.golang.org`) es el checksum database global que verifica que nadie reescribió una versión ya publicada — juntos dan descargas cacheadas y a prueba de manipulación.

---

## Checklist

- [ ] ¿La stdlib o `golang.org/x/...` ya lo resuelve antes de `go get`?
- [ ] Antes de añadir una dep: transitivas contadas (`go mod graph`), maintainer vivo, bus factor > 1, licencia compatible, API estable v1+
- [ ] `go mod tidy` corrido; `go.mod` y `go.sum` commiteados y sincronizados
- [ ] CI corre con `GOFLAGS=-mod=readonly` (el build falla si go.mod está sucio)
- [ ] `govulncheck ./...` en el pipeline; falla el build ante vulnerabilidad alcanzable
- [ ] 30 líneas copiadas y testeadas preferidas sobre una dep con 40 transitivas
- [ ] Libraries componibles, no frameworks: `net/http`+router fino, `sqlc` sobre ORM
- [ ] Deps solo en categorías justificadas: drivers, crypto, parsers de estándares, sub-repo `x/*`
- [ ] Upgrades deliberados: `go get -u` seguido de `go test -race` + `govulncheck`, nunca a ciegas
- [ ] `go.sum` versionado; `go mod vendor` solo si el build debe ser hermético
