# 2. Decisiones clave (con justificación)

## D1 — Emulador: **floci** (sobre ministack)

**Decisión:** usar [floci](https://floci.io) como sustrato de emulación AWS.

**Por qué:** el factor decisivo fue **Kafka**. Nuestro catálogo es fuertemente de mensajería
(un bus de eventos central con varios tópicos). El MSK de **ministack es solo un *stub* de control-plane** (hay que
traer tu propio broker), mientras que **floci respalda MSK con un broker Redpanda real**
(protocolo Kafka funcional). floci además:

- Tiene mayor catálogo con engines reales (RDS/Aurora, ElastiCache, MSK, S3, SNS, SQS,
  Secrets Manager, KMS…).
- Es **MIT**, se levanta con Docker Compose, y trae CLI/UI + módulos Testcontainers.

**Riesgo asumido:** floci (y ministack) son proyectos **jóvenes** (~2026, tras el cambio a
pago de LocalStack). Se mitigó con un *spike* de fidelidad en Docker antes de comprometernos,
y con una abstracción `Provider` que permite cambiar de driver por blueprint.

## D2 — Por defecto **todo pasa por floci** (paridad AWS máxima)

**Decisión:** `postgres`, `redis`, `kafka` se crean por defecto como **RDS / ElastiCache /
MSK** en floci; solo `schema-registry` (que floci no ofrece) y los legacy usan contenedor
independiente. Cualquier engine puede forzarse a contenedor propio con `--provider docker`.

**Por qué:** el criterio *no negociable* era la paridad AWS. floci levanta el mismo engine
por debajo (postgres, valkey/redis, redpanda) pero expuesto **como recurso AWS**, así una app
usa el SDK/protocolo igual que en producción. El escape `--provider docker` cubre casos donde
se necesita acceso host completo (p. ej. Kafka, ver [hallazgos](05-hallazgos-floci.md)).

> Evolución: inicialmente los engines de datos iban por `dockerx` (más determinista mientras
> no teníamos Docker). Al verificar floci en real, se cambió el default a floci por decisión
> explícita del usuario para cumplir la paridad.

## D3 — Secretos: **AWS Secrets Manager** (no Vault)

**Decisión:** los secretos se proveen vía **Secrets Manager** (en floci).

**Por qué:** aunque los assessments mencionan Vault, el criterio de “priorizar AWS ante todo”
manda. Una app lee secretos con el SDK de AWS, igual que en producción.

## D4 — Interfaz: **CLI (Cobra) + TUI (Bubble Tea)** + Taskfile

**Decisión:** CLI robusto con Cobra, un TUI interactivo con Bubble Tea, y un `Taskfile.yml`
como envoltura de conveniencia.

**Por qué:** el encargo pedía una interfaz amigable y con visión integral. El CLI da
scripting/automatización; el TUI da exploración guiada y visual.

## D5 — **Paridad CLI ↔ TUI por construcción** (capa `ops`)

**Decisión:** todas las operaciones por componente viven en una **capa única
(`internal/ops`)** consumida por igual por los comandos CLI y por el TUI.

**Por qué:** el requisito era que “lo que se puede hacer en el TUI se pueda hacer también en
el CLI”. En vez de mantener dos implementaciones sincronizadas a mano, ambas superficies
llaman a las mismas funciones → la paridad es **estructural**, no un esfuerzo manual.

## D6 — Estado local: **registro JSON + auditoría + asignador de puertos**

**Decisión:** una única fuente de verdad en `~/.keel` (state.json + audit.log), con un
asignador de puertos que evita colisiones.

**Por qué:** “gestionable y auditable”. Cada acción del ciclo de vida queda registrada
(append-only); el estado es inspeccionable y estable entre ejecuciones.

## D7 — Modo **mock** cuando no hay Docker

**Decisión:** si Docker no está disponible, el motor cae automáticamente a un simulador
en memoria (`mock`).

**Por qué:** permite usar y demostrar la CLI/TUI completa sin Docker (útil en el arranque del
proyecto, en tests y en CI). Las operaciones de datos se deshabilitan con un mensaje claro.

## D8 — **Config store de la app: Vault o AWS Secrets Manager** (elegible por proyecto)

**Decisión:** la configuración que necesita la lógica de una app (flags, límites, claves
de terceros) vive en un **config store** —Vault KV v2 o un secreto JSON en Secrets
Manager—, declarado en la sección `config:` de `keel.yaml`. `keel sync` lo siembra desde
un dotenv (`seed`, sin pisar nunca valores existentes) y la app lo lee al arrancar
(`CONFIG_STORE` + variables de arranque). Opcionalmente (`resolve`) los valores se
vuelcan al `.env`.

**Por qué:** en QA/prod esa configuración vive en Vault/Secrets, no en el `.env`; leerla
del store también en local ejercita el mismo camino. Las variables que inyecta
Kubernetes **ganan** sobre el store (precedencia: entorno > store > default), así la
plataforma siempre puede sobreescribir. `resolve` queda para apps que no pueden leer el
store. Vault corre en **modo servidor persistente** (no `-dev`, que vive en memoria y
perdería la configuración en cada reinicio); keel lo inicializa y desella.

## D9 — **GCP vía emuladores oficiales; Identity Platform = Firebase Auth emulator**

**Decisión:** los servicios GCP se ofrecen con un provider propio (`gcp-emulator`) que
corre el **emulador oficial** de cada servicio en su contenedor. El primero,
**Identity Platform**, usa el Firebase Auth emulator (firebase-tools fijado, imagen
construida localmente desde un Dockerfile embebido). El proyecto por defecto es
`demo-keel`.

**Por qué:** GCP no tiene un "floci" que agrupe servicios; cada emulador es oficial y
separado. El Auth emulator sirve la misma API `identitytoolkit` que Identity Platform y
los Admin SDKs lo detectan con `FIREBASE_AUTH_EMULATOR_HOST`, así que el código de la app
no cambia entre local y prod. No hay imagen oficial del emulador: construirla localmente
con versión fijada evita depender de imágenes de terceros. Los ids `demo-*` garantizan
que el emulador no toque recursos reales.

## D10 — **`sync` repara, no solo crea** (`provider.Ensurer`)

**Decisión:** los drivers que pueden devolver una instancia a su estado declarado
implementan `Ensure` (idempotente). `sync` lo llama sobre las instancias existentes.

**Por qué:** el registro podía decir "running" sobre un contenedor muerto tras reiniciar
el PC, o un Vault que volvió sellado; `sync` saltaba esas instancias y dejaba un `.env`
apuntando a la nada. Ahora `docker start` + pasos post-arranque (desellar Vault, esperar
al emulador) las reparan.

## Stack técnico resultante

Go 1.26 · Cobra (CLI) · Bubble Tea + Bubbles + Lipgloss (TUI) · aws-sdk-go-v2 (rds,
elasticache, kafka, s3, sns, sqs, secretsmanager, dynamodb) · pgx/v5 · go-redis/v9 · rpk
(vía Docker, para Kafka) · gopkg.in/yaml.v3 · Docker + Docker Compose + floci ·
HashiCorp Vault 2.1 (API HTTP, sin SDK) · Firebase Auth emulator (firebase-tools 15.33.0,
para Identity Platform).
