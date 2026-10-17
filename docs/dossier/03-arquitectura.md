# 3. Arquitectura

## Vista de capas

```
cmd/keel                      punto de entrada (Cobra)
  └─ internal/cli                 comandos: create/up/down/.../describe/update + grupos por componente + tui
  └─ internal/tui                 app Bubble Tea multi-vista (pila de navegación)
        │
        ▼
  internal/control (Manager)      orquestación: create/lifecycle/destroy/update/health + Ops()
        ├─ internal/registry      estado JSON (única fuente de verdad) + audit log + asignador de puertos
        ├─ internal/catalog       blueprints (postgres, redis, kafka, s3, sns, sqs, secrets, schema-registry, legacy)
        ├─ internal/health        sondas de conectividad (pgx SELECT 1, redis PING, TCP, endpoint AWS)
        ├─ internal/inject        emisión de config de conexión (env/dotenv/json/yaml · host/docker)
        ├─ internal/ops           operaciones por componente (Kafka topics, PG SQL, Redis keys, S3, SNS, SQS, Secrets, Vault KV, usuarios IdP)
        ├─ internal/configstore   config de la app en Vault KV v2 o Secrets Manager (read/write/seed + variables de arranque)
        └─ internal/provider      interfaz Provider (+ Ensurer opcional) + drivers
              ├─ aws     (floci)  RDS, ElastiCache, MSK, S3, SNS, SQS+DLQ, Secrets, DynamoDB  ← default AWS
              ├─ dockerx (docker) vault, schema-registry, legacy, y cualquier engine con --provider docker
              ├─ gcp  (emulador)  Identity Platform (Firebase Auth emulator)              ← default GCP
              └─ mock             simulador en memoria (sin Docker)

  internal/domain                 tipos puros (sin SDKs): Blueprint, Instance, Spec, Endpoint, Credential…
  internal/awsx                   builder compartido de clientes AWS apuntando a floci
  internal/vaultx                 cliente HTTP mínimo de Vault (init, unseal, KV v2)
  internal/dockercli              wrapper del CLI de docker compartido por dockerx y gcp
  internal/config                 configuración de la plataforma (capas: defaults → env)
```

Principio: **`domain` no importa ningún SDK**; toda dependencia de infraestructura vive en las
capas externas. El CLI y el TUI **no** acceden a los drivers directamente: pasan por
`control.Manager` + `ops`.

## El modelo de `Provider`

Una interfaz única materializa cada blueprint:

```go
Provision(ctx, spec, blueprint, portAllocator) (*Instance, error)
Start / Stop / Destroy / Logs
```

Tres drivers la implementan (`aws-floci`, `docker`, `mock`). El blueprint declara su driver
por defecto; `--provider` lo puede sobreescribir por instancia. Cambiar un engine de floci a
contenedor propio (o viceversa) es un cambio de una línea, sin tocar a los consumidores.

## Cómo floci expone los engines (clave para entender endpoints)

floci corre como **un contenedor** que expone la API de AWS en `:4566`. Al crear un recurso
con estado, floci **levanta el contenedor del engine real por debajo** y lo **proxya en su
propia dirección** a un puerto asignado:

| Engine | Recurso AWS | Endpoint que reporta floci | Acceso host | Acceso docker |
|---|---|---|---|---|
| postgres | RDS `CreateDBInstance` | `<floci>:7001`, `7002`… | `localhost:<port>` (rango publicado) | `floci:<port>` |
| redis | ElastiCache `CreateReplicationGroup` | `<floci>:6379`, `6380`… | `localhost:<port>` | `floci:<port>` |
| kafka | MSK `CreateClusterV2` (serverless) | broker en contenedor aparte `:9092` | ⚠️ limitado | `<broker>:9092` |
| s3/sns/sqs/secrets | API en `:4566` | `localhost:4566` | `localhost:4566` | `floci:4566` |

- Cada instancia obtiene un **puerto distinto** → múltiples instancias del mismo engine quedan
  **aisladas** también vía floci (cumple “3 Postgres separados”).
- El `docker/floci-compose.yml` **publica los rangos** (`7000-7060`, `6379-6400`) para que
  procesos en el host alcancen los engines. El broker de Kafka/MSK **no** se publica (anuncia
  una dirección interna) → Kafka es cómodo en **docker-mode**; para acceso host completo se usa
  `--provider docker`.

Detalles empíricos completos en [Hallazgos sobre floci](05-hallazgos-floci.md).

## Inyección de conexiones (`internal/inject`)

Implementa el [patrón de inyección](../CONNECTION_INJECTION_PATTERN.md). Es la “superficie de
entrega” a los procesos:

- Entrega **uniforme** por variables de entorno (mínimo común denominador multi-lenguaje).
- **Un set de variables por rol** (Postgres emite `*_DSN_WRITE` y `*_DSN_READ`).
- **Con forma de AWS**: emite `AWS_ENDPOINT_URL`, `AWS_REGION` y credenciales dummy para que el
  SDK de AWS de una app hable con floci de forma transparente.
- Modo **host** (`localhost:<puerto>`) vs **docker** (hostname de red) según dónde corra el
  consumidor.
- Formatos: dotenv, export env, JSON, YAML.

## Capa de operaciones (`internal/ops`) — el pilar de la paridad

Interfaz tipada por engine, respaldada por:
- **Postgres**: `pgx` contra el endpoint host.
- **Redis**: `go-redis`.
- **Kafka**: `rpk` ejecutado en un **contenedor efímero** sobre la red compartida, usando el
  endpoint docker-mode del broker (funciona para floci-MSK y para redpanda `--provider docker`).
- **S3/SNS/SQS/Secrets**: clientes AWS (`internal/awsx`) apuntando a floci.

El CLI (`kafka`, `pg`, `redis`, `s3`, `sns`, `sqs`, `secret`) y el TUI llaman a estas mismas
funciones. Instancias `mock` devuelven un error claro (“no disponible en modo mock”).

## Config store de la app (`internal/configstore`)

Separa dos tipos de variables: las de **conexión** (las emite `inject` a partir de las
instancias) y las de **aplicación** (flags, límites, terceros), que viven en un store.

```
keel.yaml config: ─► configstore.Open(instancia vault|secrets, path)
                        ├─ vault:   KV v2  <mount>/<path>   (vaultx, HTTP)
                        └─ secrets: secreto JSON {"KEY":"valor"} (aws-sdk)
sync:  seed (dotenv, solo claves nuevas) ─► store
       store.BootstrapEnv() ─► .env   (CONFIG_STORE, CONFIG_VAULT_PATH | CONFIG_SECRET_ID)
       o, con resolve: store.Read() ─► .env (los valores; las de conexión ganan si chocan)
app:   configstore.Load() al arrancar ─► config.Load(path, store)   entorno > store > default
```

El servicio que genera `keel new go` trae ese loader (`internal/platform/configstore`):
Vault con stdlib y, con `--config-store aws`, Secrets Manager con el SDK de AWS.

## El TUI (`internal/tui`) — app multi-vista

- **Shell raíz** con una **pila de navegación** (`esc` = atrás) y estado compartido
  (instancias, health, toast, spinner). El engine se resuelve **una sola vez** al inicio (para
  no ejecutar `docker info` por frame) y el spinner solo anima cuando hay trabajo en curso.
- **Pantallas**: dashboard (tabla + health en vivo) · detalle · asistente de creación · menú de
  operaciones por componente · formularios dinámicos (con multilínea) · confirmación para
  destructivas · salida (tabla/texto) · inject (preview) · logs.
- **Acciones genéricas**: el registro `actions.go` define cada operación como una acción con
  campos + una función `run` que llama a `Manager`/`ops` — las mismas que usa el CLI.
