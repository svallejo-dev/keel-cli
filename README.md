# keel — Infraestructura como Servicio local

Una plataforma CLI que levanta **infraestructura con forma de nube en tu máquina** — un
[Fury](https://web.archive.org/web/2023/https://medium.com/mercadolibre-tech) /
Vercel local para infra. Cualquier repo, en cualquier lenguaje, puede crear los
componentes que necesita (Postgres, Redis, Kafka, S3, SNS/SQS, Secrets Manager,
Vault, Identity Platform) y conectarse a ellos, sin que cada proyecto reinvente cómo
levantar y cablear su infraestructura.

Cada engine refleja un servicio gestionado real:

- **AWS** vía [floci](https://floci.io): Postgres ≡ RDS/Aurora, Redis ≡ ElastiCache,
  Kafka ≡ MSK, más S3 / SNS / SQS / Secrets Manager / DynamoDB.
- **GCP** vía sus emuladores oficiales: **Identity Platform** (IdP de Google).
- **Agnóstico**: **HashiCorp Vault** como almacén de secretos y configuración.

Además, cada proyecto puede guardar **su configuración de aplicación** (flags,
límites, claves de terceros) en **Vault o Secrets Manager**: `keel` la siembra y la
app la lee al arrancar, igual que en QA/prod (ver [Config store](#config-store-vault--secrets-manager)).

> 📓 **Memoria técnica** (visión, decisiones, arquitectura, proceso y hallazgos):
> [`docs/dossier/`](docs/dossier/00-indice.md).

---

## Por qué

- **Un comando para obtener infra.** `keel create postgres --name orders-db`.
- **Instancias aisladas.** Tres microservicios → tres Postgres independientes, cada
  uno con su propio contenedor, puertos y credenciales (base-de-datos-por-servicio).
- **Cableado agnóstico al lenguaje.** `keel inject` genera configuración de
  conexión lista para usar (dotenv/env/json/yaml) implementando el
  [patrón de inyección de conexiones](docs/CONNECTION_INJECTION_PATTERN.md) del
  proyecto.
- **Gestionable y auditable.** Levanta/apaga todo, lista, chequea salud, un dashboard
  TUI integrado y un log de auditoría append-only de cada acción.

## Qué puedes hacer (resumen)

Todo funciona igual desde el **CLI** (con argumentos) y desde el **TUI** (`keel tui`,
guiado). Paridad garantizada: ambos usan la misma capa interna.

**Gestión de infraestructura**
- Ver el **catálogo** de engines (paridad AWS) y **crear** instancias aisladas
  (`create`, con `--provider docker` opcional) o **stacks declarativos** (`apply`).
- **Ciclo de vida**: `up` / `down` / `restart` (una o todas) y `destroy [--purge]`.
- **Editar** params/notas (`update [--restart]`).
- **Observar**: `list`, `describe` (detalle completo), `health`, `doctor`, `logs`.
- **Conectar**: `connect` e `inject` (env/dotenv/json/yaml · host/docker) para cablear cualquier repo.
- **Auditar**: `audit`.

**Operaciones por componente**
- **Kafka**: `topic list/create/delete/describe`, `produce`, `consume`.
- **Postgres**: `db list/create/drop`, `tables`, `sql -c "…"`.
- **Redis**: `keys`, `get/set/del`, `info`, `flush`.
- **S3**: `ls/put/get/rm`.
- **SNS**: `publish`, `subscriptions`.
- **SQS**: `send/receive/purge/attrs`.
- **Secrets**: `get/set/describe`.

**Scaffolding de proyectos**
- **Crear proyectos** desde una plantilla única y consistente (`new go <name>`): un
  servicio REST en Go orientado a Package Oriented Design, listo para correr.

**TUI** (`keel tui`): dashboard con health en vivo → detalle · asistente de creación ·
menú de operaciones por componente (formularios + confirmaciones) · inject con preview · logs.

## Arquitectura de un vistazo

```
keel (CLI + TUI)
   └─ control.Manager ── registry (estado + auditoría + puertos) · ops (operaciones por componente)
        └─ interfaz Provider
             ├─ aws      → floci @ :4566  (RDS≡postgres, ElastiCache≡redis, MSK≡kafka, S3, SNS, SQS+DLQ, Secrets)  ← default
             ├─ dockerx  → contenedores propios (schema-registry, legacy, o cualquier engine con --provider docker)
             └─ mock     → simulador en memoria (no requiere Docker)
```

Por defecto **todo pasa por floci** para máxima paridad AWS: los engines de datos se crean
como RDS/ElastiCache/MSK reales (floci levanta el contenedor por debajo) y los servicios de
API AWS (S3/SNS/SQS/Secrets) responden en `:4566`, así el SDK de AWS de una app funciona sin
cambios. La interfaz `Provider` permite cambiar cualquier blueprint entre drivers
(`--provider docker`) sin tocar a quien lo consume.

## Requisitos previos

- **Go 1.26+** (para compilar).
- **Docker Desktop** — requerido para el motor real. Sin él, keel corre
  automáticamente en modo **mock** (las instancias son simuladas) para que la
  CLI/TUI sigan siendo totalmente usables. Verifícalo cuando quieras con
  `keel doctor`.
- Opcional: [go-task](https://taskfile.dev) para los atajos `task`.

## Inicio rápido

```bash
# instalar (queda en tu PATH vía $GOBIN / $GOPATH/bin)
go install github.com/svallejo-dev/keel-cli/cmd/keel@latest
# o desde un clon: go install ./cmd/keel   ·   go build -o bin/keel ./cmd/keel

# abrir el panel interactivo (launcher): crear infra, nuevo proyecto, doctor…
keel

# ver qué hay disponible
keel catalog

# crear instancias aisladas
keel create postgres --name orders-db  --param dbname=entdb_orders
keel create postgres --name billing-db --param dbname=entdb_billing
keel create redis    --name session-cache
keel create kafka    --name event-bus
keel create secrets  --name gateway-creds

# gestionar
keel list
keel health
keel tui                                 # dashboard integrado

# cablear en una app (inyección de conexiones)
keel inject orders-db session-cache gateway-creds --out .env
keel inject --all --format yaml --mode docker --out infra.yaml

# stacks declarativos
keel apply stacks/example-relational-cache-bus.yaml
```

### Infraestructura real (con Docker) — arranque desde cero

**No tienes que preparar nada.** Con Docker instalado, `keel create` levanta el emulador
floci automáticamente si hace falta: crea la red, descarga la imagen, arranca el contenedor
y espera a que responda.

```bash
keel create postgres --name my-db   # si floci no está arriba, keel lo arranca solo
```

Si prefieres controlarlo a mano:

```bash
keel engine up        # arranca floci desde cero (red + imagen + contenedor)
keel engine status    # ¿Docker ok? ¿contenedor? ¿endpoint alcanzable?
keel engine down      # lo detiene (conserva el volumen de datos)
```

También está en el panel interactivo: **Arrancar motor (floci)**. Y `keel doctor` /
la opción **Doctor** muestran el diagnóstico completo.

Fuerza un motor con `--engine real|mock` o `KEEL_ENGINE`.

## Comandos

| Comando | Propósito |
|---|---|
| `catalog [kind]` | Lista blueprints; con un kind, muestra su ficha y sus `--param` |
| `create <kind> --name <n>` | Crea una instancia aislada |
| `apply <manifest.yaml>` | Provisiona un conjunto declarativo de instancias (idempotente) |
| `init` | Crea el `keel.yaml` del proyecto (qué infra necesita este repo) |
| `sync [--resolve]` | Concilia el proyecto: motor arriba + infra declarada (y reparada) + config store sembrado + `.env` fresco |
| `config list/get/set/unset/import/export/where` | Configuración de la app en su store (Vault o Secrets Manager) |
| `up [nombres…] / --all` · `down` · `restart` | Ciclo de vida |
| `destroy [nombres…] / --all [--purge]` | Elimina instancias (libera puertos; `--purge` borra datos) |
| `list` (`ls`) | Todas las instancias: estado, endpoints, servicio cloud equivalente |
| `describe <name>` (`info`) | Detalle completo: endpoints, credenciales, recursos, operaciones, conexión |
| `update <name> [--param k=v] [--note] [--restart]` | Edita params/notas de una instancia |
| `health [nombres…]` | Sondas de conectividad (SELECT 1, PING, TCP, endpoint AWS) |
| `doctor` | Diagnóstico del entorno (Docker, floci, motor, estado) |
| `engine up / status / down` (`floci`) | Arranca el emulador desde cero, lo consulta o lo detiene |
| `logs <name> [-f]` | Logs de la instancia |
| `connect <name>` | Detalles de conexión listos para usar |
| `inject [nombres…] --format --mode --out` | Genera config de conexión para un repo consumidor |
| `audit` | Log append-only de acciones del ciclo de vida |
| `new go [name]` | Genera un proyecto Go (servicio REST) desde plantilla (ver abajo) |
| `tui` (`dashboard`) | Dashboard interactivo multi-vista (ver abajo) |

### Operaciones por componente (paridad total con el TUI)

Cada engine expone sub-recursos y operaciones de datos. La misma capa (`internal/ops`)
respalda estos comandos y el TUI, así que **todo lo que se hace en el TUI se hace también
por CLI**:

| Grupo | Operaciones |
|---|---|
| `kafka topic list/create/delete/describe` · `kafka produce/consume` | Tópicos y mensajes de MSK |
| `pg db list/create/drop` · `pg tables` · `pg sql -c "…"` | Databases, tablas y SQL de RDS |
| `redis keys/get/set/del/info/flush` | Claves y datos de ElastiCache |
| `s3 ls/put/get/rm` | Objetos de S3 |
| `sns publish/subscriptions` | Mensajes y suscripciones de SNS |
| `sqs send/receive/purge/attrs` | Mensajes y atributos de SQS |
| `secret get/set/describe` | Valor y metadatos de Secrets Manager |
| `vault status` · `vault kv list/get/put/delete` | Estado y secretos KV v2 de Vault |
| `idp user list/create/delete/claims` · `idp token` · `idp reset` | Usuarios, custom claims e ID tokens de Identity Platform |

Ejemplos:
```bash
keel kafka topic create event-bus pagos -p 3
keel kafka produce event-bus pagos -m "cobro.confirmado:u1"
keel pg db create orders-db appdb && keel pg sql orders-db --db appdb -c "select 1"
keel redis set cache user:42 '{"n":"ana"}' --ttl 5m
keel s3 put archive hola.txt --body "hola" && keel s3 ls archive
keel sqs send work-queue -m "job-1" && keel sqs receive work-queue
```

## Catálogo

| Kind | Paridad | Provider por defecto |
|---|---|---|
| `postgres` | Amazon RDS / Aurora PostgreSQL (roles write + read) | **floci** |
| `redis` | Amazon ElastiCache | **floci** |
| `kafka` | Amazon MSK | **floci** |
| `schema-registry` | *(sin equivalente nativo en AWS)* | docker |
| `s3` | Amazon S3 | floci |
| `sns` | Amazon SNS | floci |
| `sqs` | Amazon SQS (+ DLQ) | floci |
| `secrets` | AWS Secrets Manager | floci |
| `dynamodb` | Amazon DynamoDB (NoSQL, on-demand) | floci |
| `vault` | HashiCorp Vault (KV v2) — agnóstico a la nube | docker |
| `gcp-identity` | Google Cloud Identity Platform | **gcp-emulator** |

Por defecto **todo lo AWS pasa por floci** (máxima paridad: RDS/ElastiCache/MSK).
`schema-registry` (que floci no ofrece), `vault` y los legacy usan contenedor
independiente; los servicios **GCP** corren su emulador oficial en su propio contenedor.
Puedes forzar el contenedor independiente en cualquier engine con
`--provider docker` (útil, p. ej., para acceso host completo a Kafka).

### Opciones por servicio (`--param`)

Cada engine declara sus opciones **en el catálogo**, que es la fuente única de verdad: de
ahí se generan el formulario del TUI, la validación del CLI y esta documentación. Consulta
las de cualquier servicio con:

```bash
keel catalog dynamodb     # ficha del engine + tabla de params (default, valores, driver)
```

Una clave inexistente **falla** en vez de ignorarse en silencio:
```
$ keel create dynamodb --name t --param pkk=x
Error: unknown param "pkk" for dynamodb (accepted: pk, pktype, sk, sktype, gsi_name, …)
```

Resumen de lo configurable hoy:

| Kind | Opciones |
|---|---|
| `postgres` | `dbname`, `engine_version`, `instance_class`, `storage`, `image`/`version` (docker) |
| `redis` | `node_type`, `engine_version`, `image`/`version` (docker) |
| `kafka` | `topics` (pre-crea tópicos: `pagos:3,envios:6`), `image`/`version` (docker) |
| `s3` | `versioning` |
| `sns` | `fifo` |
| `sqs` | `visibility_timeout`, `retention`, `dlq`, `max_receive_count`, `fifo` |
| `secrets` | `value`, `description` |
| `dynamodb` | `pk`/`pktype`, `sk`/`sktype`, `gsi_name`/`gsi_pk`/`gsi_sk`/`gsi_projection`, `ttl`, `read_capacity`, `write_capacity` |
| `vault` | `mount` (KV v2, default `secret`), `image`/`version` |
| `gcp-identity` | `project` (default `$KEEL_GCP_PROJECT` o `demo-keel`), `ui` (consola web) |

```bash
keel create dynamodb --name eventos --param pk=eventId --param sk=createdAt --param sktype=N \
  --param gsi_name=byUser --param gsi_pk=userId --param ttl=expiresAt
keel create sqs --name work-queue --param visibility_timeout=45 --param max_receive_count=3
keel create kafka --name event-bus --param topics=pagos:3,envios:6
```

**Todo esto también desde el TUI**: el asistente de creación construye su formulario desde
el catálogo (con defaults y valores válidos como ayuda, y scroll cuando hay muchos campos),
así que cualquier opción configurable por CLI lo es también por el panel.

Diferidos/legacy (solo catálogo): `oracle`, `cassandra`, `rabbitmq` (`catalog --all`).

## Flujo de proyecto (`init` + `sync`)

Parado en tu repo, declara qué infra necesita y deja que keel la mantenga viva y
cableada con **un solo comando**.

```bash
cd mi-proyecto
keel init --kinds dynamodb,s3,sqs   # escribe keel.yaml (te pregunta si omites flags)
keel sync                           # motor arriba + infra creada + .env escrito
```

El `keel.yaml` es el manifiesto del proyecto (**versiónalo**): quien clone el repo
corre `keel sync` y obtiene la misma infraestructura.

```yaml
version: 1
name: admin-eventhub
instances:
  - {name: admin-eventhub-dynamodb, kind: dynamodb}
  - {name: admin-eventhub-s3,       kind: s3}
  - {name: admin-eventhub-sqs,      kind: sqs}
inject:
  out: .env
  format: dotenv
  mode: host        # docker si tu app corre en contenedor
```

**`keel sync` hace cuatro cosas, y es idempotente:**
1. **Arranca el motor** si floci está caído (solo si el proyecto usa algo de AWS).
2. **Concilia la infra**: crea lo que falte, arranca lo detenido y **repara** lo que
   el registro da por vivo pero no lo está (contenedor caído tras reiniciar el PC,
   Vault que volvió sellado).
3. **Reescribe el archivo de conexión** con los valores vigentes.
4. **Config store** (si `keel.yaml` tiene sección `config:`): siembra los valores que
   falten y apunta la app al store (o, con `resolve`, vuelca los valores al `.env`).

Ese tercer punto es el que más ahorra: puertos y URLs de colas los asigna floci en
runtime, así que un `.env` viejo puede apuntar a algo que ya no existe. Córrelo tras
reiniciar el PC, tras un `git pull` o al editar `keel.yaml`:

```bash
keel sync                    # tras cualquier cambio
keel sync --mode docker      # variante para consumidores en contenedor
```

> **Local vs. ambientes**: `keel.yaml` y `.env` son **solo para local**. En QA/producción
> la plataforma inyecta **las mismas variables** (K8s secrets, ECS task definition,
> Terraform…). El contrato compartido es `configs/config.yaml` del proyecto generado.

## Config store (Vault / Secrets Manager)

Las variables que necesita la **lógica de la app** (flags, límites, URLs y claves de
terceros…) no viven en el `.env`: viven en un **config store**, igual que en QA/prod.
Las de infraestructura (DSN, endpoints) siguen llegando por `.env`, y en Kubernetes
las que inyecta la plataforma **siempre ganan** sobre el store.

Elige el backend por proyecto en `keel.yaml` (`keel init` lo propone si eliges
`vault` o `secrets`):

```yaml
instances:
  - {name: orders-vault, kind: vault}     # o {kind: secrets} para AWS Secrets Manager
config:
  store: orders-vault      # instancia vault o secrets declarada arriba
  path: orders             # Vault: ruta KV (default: nombre del proyecto) · Secrets: secret id
  seed: config.dev.env     # valores de desarrollo; sync añade los que falten
  resolve: false           # true: sync también escribe los valores en el .env
```

Dos formas de consumo (elegibles):

| Modo | Qué escribe `sync` en el `.env` | La app… |
|---|---|---|
| **store** (default) | `CONFIG_STORE`, `CONFIG_VAULT_MOUNT`/`CONFIG_VAULT_PATH` o `CONFIG_SECRET_ID` (+ `VAULT_ADDR`/`VAULT_TOKEN` o `AWS_*`) | lee el store al arrancar, el mismo camino que en QA/prod |
| **resolve** (`resolve: true` o `sync --resolve`) | los valores ya resueltos | no necesita cliente de Vault/AWS |

```bash
keel sync                          # siembra config.dev.env → store y escribe .env
keel config set MAX_RETRIES=5      # cambia un valor (reinicia tu app para tomarlo)
keel config list                   # enmascara claves con pinta de secreto (--reveal)
keel config import otros.env       # solo claves nuevas (--overwrite para pisar)
keel config export --format json
keel config where                  # qué store y qué variables necesita la app
```

La siembra **nunca pisa** valores existentes: lo que cambies con `keel config set`
sobrevive a cada `sync`. Las mutaciones quedan en `keel audit` (solo nombres de clave,
nunca valores).

**Vault local**: keel levanta un Vault **persistente** (no `-dev`, que vive en
memoria), lo inicializa, lo desella y habilita KV v2. Vault vuelve **sellado** tras
cada reinicio del contenedor; `keel up`/`keel sync` lo desellan solos (la unseal key y
el root token se guardan en el estado local — válido solo para desarrollo).

## GCP: Identity Platform

`gcp-identity` levanta el **emulador oficial de Firebase Auth**, que sirve la misma
API `identitytoolkit` que **Google Cloud Identity Platform** (registro, login, ID
tokens JWT, custom claims). La imagen se construye localmente la primera vez
(firebase-tools fijado) y los usuarios **persisten** entre reinicios.

```bash
keel create gcp-identity --name auth
keel idp user create auth -e ana@test.com -p secret123
keel idp user claims auth ana@test.com '{"role":"admin"}'
TOKEN=$(keel idp token auth -e ana@test.com -p secret123)   # JWT para tu API
keel describe auth                                           # incluye la URL de la consola web
```

`inject`/`sync` emiten `FIREBASE_AUTH_EMULATOR_HOST`, `GOOGLE_CLOUD_PROJECT` y
`<INSTANCIA>_API_KEY`: los **Admin SDKs** (Go, Node, Java, Python) usan el emulador
cuando esa variable existe y el Identity Platform real cuando no (QA/prod), sin cambiar
código. El proyecto por defecto es `demo-keel`: los ids `demo-*` hacen que el emulador
nunca toque recursos reales de Google. Cámbialo con `--param project=` o
`KEEL_GCP_PROJECT`.

> Los ID tokens del emulador **no van firmados** (`alg: none`); el Admin SDK los acepta
> solo en modo emulador. Nunca configures `FIREBASE_AUTH_EMULATOR_HOST` fuera de local.

## Scaffolding de proyectos (`new`)

Además de levantar infraestructura, keel **crea proyectos** desde una plantilla única
y consistente, para que todo repo nazca con la misma forma. Rama de comandos aparte de la
gestión de infra (`new` genera código fuente, no provisiona infra).

```bash
keel new go payments-api                 # pide el nombre si lo omites
keel new go orders --module github.com/acme/orders --dir ./services/orders
```

`new go` genera un **servicio REST en Go** orientado a **Package Oriented Design** (estilo
Ardan Labs), listo para `go run`:

- **Layout POD**: `cmd/<name>` (binario), `internal/` (paquetes de aplicación),
  `internal/platform/web` (plumbing HTTP fundacional, agnóstico al dominio).
- **Framework**: [Gin](https://github.com/gin-gonic/gin) (el más popular de Go).
- **Health**: `GET /ping` (liveness, no toca dependencias) y `GET /health` (readiness, con
  `Checker`s de dependencias — 503 si alguna falla).
- **Graceful shutdown**: `signal.NotifyContext` (SIGINT/SIGTERM) + `http.Server.Shutdown`
  con timeout, dando margen a las peticiones en vuelo.
- **Idiomático**: logging estructurado (`log/slog`) con `request_id` por request propagado
  vía `context.Context` (cada log lo lleva), config `entorno → configs/config.yaml → struct`
  inyectada por constructor (sin globals; el código no hard-codea env keys), middleware
  (request-id, access-log, recovery), `Dockerfile` multi-stage (distroless nonroot),
  `Makefile`, `.golangci.yml`, `.env.example`.
- **Config store** (`--config-store`, default `vault`): `internal/platform/configstore`
  lee al arrancar los valores de Vault (cliente stdlib, sin dependencias) y los usa para
  resolver las `${VAR}` de `configs/config.yaml` con precedencia
  **entorno > config store > default**. `aws` añade AWS Secrets Manager (trae el SDK de
  AWS); `none` deja solo variables de entorno. Sin `CONFIG_STORE` definido, el servicio
  arranca solo con el entorno.

**Configuración de IA (tool-agnóstica):** opcionalmente scaffoldea espacios para agents,
skills y MCP de **Claude Code**, **GitHub Copilot** y/o **Cursor**, más un `AGENTS.md`
compartido como fuente de verdad. El CLI **pregunta qué herramientas incluir** (o pásalo con
`--ai claude,cursor` · `all` · `none`):

```bash
keel new go api --ai claude,cursor   # .claude/ .cursor/ .mcp.json + AGENTS.md
keel new go api                       # pregunta interactivamente
```

Cada herramienta obtiene su layout nativo (`.claude/agents|skills`, `.github/copilot-instructions.md`,
`.cursor/rules`, y sus archivos MCP), todos apuntando al `AGENTS.md` común.

**Skills embebidas:** keel trae un set de **skills de Go** (idiomas, diseño, DDD y flujo
de git) empaquetadas en el binario. Cuando se elige `claude`, se **clonan verbatim** a
`.claude/skills/` del proyecto nuevo. Controla cuáles con `--skills` (`all` por defecto ·
`none` · lista):

```bash
keel new go api --ai claude                                  # clona todas las skills
keel new go api --ai claude --skills senior-go-idiomatic     # solo una
keel new go api --ai claude --skills none                    # ninguna
```

Skills incluidas: `senior-go-idiomatic`, `go-design-rules`, `ddd-go-idiomatic`, `git-commit`,
`create-pr`, `review-pr`. Viven en `internal/scaffold/skills/` y se editan ahí (una sola fuente,
versionada con la herramienta).

Tras generar corre `go mod tidy` automáticamente (usa `--no-tidy` para omitirlo). Flags:
`--module` (path del módulo), `--dir` (carpeta destino), `--force` (generar en carpeta no
vacía), `--no-tidy`, `--ai` (configs de IA).

Las plantillas viven embebidas en `internal/scaffold/templates/` (`embed` + `text/template`),
así que la plantilla es única y versionada con la herramienta. Añadir otro lenguaje es un
subcomando nuevo bajo `new`.

## Panel interactivo (launcher)

Escribe **`keel`** a secas (sin subcomando, en una terminal) y se abre el **panel de
inicio**: un menú con identidad para llegar a todo sin recordar flags.

```
 _  __ _____ _____ _
| |/ /| ____| ____| |
| ' / |  _| |  _| | |
| . \ | |___| |___| |___
|_|\_\|_____|_____|_____|

 ⚓ keel  Inicio                                        ● mock · 0 instancias

 ▸ 1  ◆ Infraestructura   — crear y gestionar componentes
   2  ◈ Nuevo proyecto    — repo Go (simple · +IA · +IA+skills · todo · personalizado)
   3  ✦ Doctor            — diagnóstico del entorno
   4  ❔ Acerca de Keel
   5  ⏻ Salir
```

- **Infraestructura** entra al dashboard de instancias (crear/gestionar/operar).
- **Nuevo proyecto** es un asistente: eliges un preset (repo simple · repo + config de IA ·
  repo + IA + skills de Go · todo) o **Personalizado** (multiselect para marcar herramientas de
  IA y skills a la carta), te pide el nombre y genera el proyecto.
- Header con estado del motor (● verde = real, ● ámbar = mock) y breadcrumbs.
- Navegación: `↑/↓` mover · `1–5` atajo · `space` alternar (en multiselect) · `enter` elegir ·
  `esc` atrás · `q` salir.

Si no hay terminal interactiva (salida a pipe/CI), `keel` imprime la ayuda. Todos los
subcomandos siguen funcionando igual; el panel es solo una puerta de entrada más.

## Dashboard interactivo (TUI)

`keel tui` (o **Infraestructura** en el panel) abre una app multi-vista con navegación por
pila (esc = atrás). Todo lo del CLI se puede hacer aquí, de forma guiada:

- **Dashboard**: tabla de instancias con estado y health en vivo. Teclas: `enter` detalle ·
  `o` operaciones · `n` crear · `u/s/R` start/stop/restart · `d` eliminar · `i` inject ·
  `l` logs · `r` refrescar · `q` salir.
- **Detalle**: endpoints (host + docker), credenciales, recursos, params, notas.
- **Crear**: asistente por pasos (elegir blueprint → formulario con nombre, provider y params).
- **Operaciones**: menú por componente (topics de Kafka, databases/SQL de Postgres, keys de
  Redis, objetos de S3, publish/send, secret get/set…). Las acciones que necesitan datos abren
  un **formulario**; las destructivas piden **confirmación**; los resultados se muestran en
  tablas o texto navegable.
- **Inject**: previsualiza la config de conexión; `f` cambia formato, `m` alterna host/docker,
  `w` escribe el archivo.

Corre en una terminal real (Windows Terminal / PowerShell). Requiere floci arriba para las
operaciones sobre instancias reales.

## Inyección de conexiones

`inject` es la implementación en la plataforma de
[`docs/CONNECTION_INJECTION_PATTERN.md`](docs/CONNECTION_INJECTION_PATTERN.md):

- **Un conjunto de variables por rol** — Postgres emite `*_DSN_WRITE` y `*_DSN_READ`.
- **Entrega uniforme** — endpoints y secretos llegan ambos como variables de entorno.
- **Con forma de nube** — emite `AWS_ENDPOINT_URL`, `AWS_REGION` y credenciales dummy
  para que el SDK de AWS hable con floci; `FIREBASE_AUTH_EMULATOR_HOST` y
  `GOOGLE_CLOUD_PROJECT` para los SDKs de Google; `VAULT_ADDR`/`VAULT_TOKEN` para Vault.
- **`--mode host`** (localhost:puertoMapeado) vs **`--mode docker`** (hostname de red)
  para consumidores en el host vs en contenedores.

## Configuración

Los ajustes de la plataforma vienen de variables de entorno (ver
[`configs/keel.example.env`](configs/keel.example.env)): `KEEL_ENGINE`,
`KEEL_HOME`, `KEEL_REGION`, `KEEL_FLOCI_ENDPOINT`,
`KEEL_DOCKER_NETWORK`, `KEEL_GCP_PROJECT`. El estado y la auditoría viven en `~/.keel`.

## Riesgos / notas

- **floci es un proyecto joven** (creado tras el cambio a pago de LocalStack en 2026).
  El tag de imagen y los nombres `FLOCI_*` en
  [`docker/floci-compose.yml`](docker/floci-compose.yml) deben verificarse contra la
  documentación de floci en la primera ejecución real; ajústalos si difieren. Si la
  fidelidad de floci no alcanza para un servicio, cambiar ese blueprint al provider
  `docker` es un cambio de una línea.
- Los roles read/write de Postgres apuntan a un **único nodo local** (sin réplica de
  streaming); los dos DSN permiten a las apps cablear clientes por rol desde ya,
  igual que en producción.
- **Engines vía floci (RDS/ElastiCache/MSK):** floci los expone en su propia dirección
  en puertos asignados (RDS 7001+, ElastiCache 6379+), publicados al host por el
  compose. Postgres y Redis funcionan en **host-mode** (`localhost:<port>`) y
  **docker-mode** (`floci:<port>`). **Kafka/MSK** solo es accesible en **docker-mode**
  (el broker anuncia una dirección interna y no se publica al host); para acceso host
  completo a Kafka usa `--provider docker`. Cada instancia obtiene un puerto distinto,
  así que múltiples instancias del mismo engine quedan aisladas también vía floci.

## Desarrollo

```bash
task check      # go fmt + vet + test
task demo       # provisiona el stack de ejemplo en modo mock
```
