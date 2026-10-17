# 4. Proceso (cronología)

Cómo llegamos al resultado, incluyendo lo que se descubrió y corrigió en el camino.

## Fase de investigación

- Se leyó el patrón [`CONNECTION_INJECTION_PATTERN.md`](../CONNECTION_INJECTION_PATTERN.md) y,
  con **sub-agentes en paralelo**, se extrajo el catálogo de infra de los tres assessments.
- Un cuarto sub-agente investigó **floci vs ministack** → recomendación floci (MSK real).
- Decisiones aprobadas por el usuario: floci · Secrets Manager · CLI+TUI · agnóstico a
  microservicios.

## Construcción base (plataforma)

- **M0 — Scaffolding**: módulo Go, layout, esqueleto Cobra, Taskfile, compose de floci.
  Hallazgo temprano: **Docker no estaba instalado** en la máquina → se añadió el **motor mock**
  para poder construir y verificar todo sin Docker.
- **M1 — Núcleo**: `domain`, `Provider` (aws/dockerx/mock), `registry` (estado + auditoría +
  puertos), `catalog`, `health`, `control`, comandos CLI (create/up/down/destroy/list/health/…).
- **M2 — Inyección**: `connect` e `inject` (env/dotenv/json/yaml, host/docker, roles).
- **M3 — TUI**: primer dashboard (lista + health + ciclo de vida).
- **M4 — Robustez**: `apply` (manifiestos), `doctor`, `audit`, confirmaciones.
- **Verificación en mock**: create/list/health/inject/apply/audit/destroy end-to-end, sin Docker.

## Verificación real (tras instalar Docker)

Con Docker 29.6.1 + Compose:
- Se levantó **floci 1.5.32** y se verificó la ruta AWS-API: S3, SNS, SQS+DLQ, Secrets, con un
  **round-trip real de SQS** vía `amazon/aws-cli`.
- Se hicieron *spikes* con `aws-cli` para entender cómo floci expone **RDS, ElastiCache y MSK**
  (ver [hallazgos](05-hallazgos-floci.md)).
- Se comprobó **aislamiento multi-instancia** (puertos distintos por instancia) y conectividad
  **host** y **docker** para Postgres/Redis, y **docker-mode** para Kafka (round-trip con `rpk`).

## Giro de arquitectura: floci por defecto

A petición del usuario (paridad AWS no negociable), se cambió el default de `postgres/redis/
kafka` de `dockerx` a **floci** (RDS/ElastiCache/MSK), añadiendo el override `--provider docker`
y publicando los rangos de puertos en el compose.

## Expansión: operaciones por componente + TUI multi-vista

- **Capa `ops`** + `internal/awsx` (builder de clientes compartido).
- `Manager.Ops()` + `Manager.Update()` + `domain.Instance.Notes`.
- **Comandos CLI** por componente: `kafka`, `pg`, `redis`, `s3`, `sns`, `sqs`, `secret`, más
  `describe` y `update`.
- **Reescritura del TUI** a multi-vista (dashboard/detalle/creación/operaciones/formularios/
  confirmación/salida/inject/logs) con `bubbles`, sobre la misma capa `ops` (paridad).

## Bugs encontrados y corregidos

| Síntoma | Causa | Corrección |
|---|---|---|
| `destroy` de un recurso ya inexistente dejaba **estado fantasma** en el registro | el error NotFound abortaba antes de borrar del registro | `ignoreNotFound` en el driver aws (destruir algo ya ausente = éxito) |
| Errores de validación de argumentos **no se mostraban** | `SilenceErrors=true` en el root de Cobra | `SilenceErrors=false` (Cobra imprime una vez) |
| Mensaje de error de Kafka poco claro (“exit status 1”) | `rpk` reporta algunos errores por **stdout** | incluir stdout en el mensaje de error |
| **TUI lento** al mover de fila (cientos de ms) | el header ejecutaba `docker info` **por cada frame** (motor `auto`) + spinner animando siempre | resolver el motor **una vez** al inicio; spinner solo cuando hay trabajo; preservar cursor en el refresco |

## Expansión: config store (Vault / Secrets Manager) + GCP

Decisiones del usuario: soportar **Vault y AWS Secrets Manager** (elegible por
proyecto); consumo **elegible** (la app lee el store, o `resolve` al `.env`); el "IdP de
Google" es **Identity Platform**; el scaffold Go trae el loader.

- *Spikes* reales antes de codificar: Vault 2.1 en modo servidor (init → unseal → KV v2 →
  reinicio → vuelve sellado) y Firebase Auth emulator (signUp/signIn, API admin
  `Bearer owner`, UI, persistencia con import/export).
- Nuevos: `vault` (dockerx), provider `gcp-emulator` con `gcp-identity`, `configstore`,
  `vaultx`, `dockercli`, sección `config:` en manifiestos, comandos `config`, `vault`,
  `idp`, acciones TUI, `Ensurer`, campo `Cloud` y `CloudService` (antes `AWSService`, con
  migración del estado).
- Verificado end-to-end: `init`+`sync` (Vault + IdP), config set/list, usuarios, claims
  en el JWT, Vault sellado y contenedor caído reparados por `sync`, servicios generados
  leyendo **Vault** y **Secrets Manager** (vía floci) con la precedencia correcta.

| Síntoma | Causa | Corrección |
|---|---|---|
| Usuarios del IdP perdidos tras reiniciar | `--export-on-exit=/data` intenta borrar el punto de montaje (`EBUSY`) | exportar a `/data/export` |
| La UI del emulador no conectaba con el auth | la UI entrega al navegador los puertos internos | el contenedor escucha en los mismos puertos que publica keel |
| `sync` arrancaba floci en proyectos solo Vault/GCP | `EnsureUp` incondicional | solo si alguna instancia es AWS |
| `keel tui` podía escribir progreso encima de la pantalla | usaba el manager no silencioso | manager silencioso; `sync`/`config` muestran progreso sin banner |

## Estado final

- Compila, `go vet` limpio, tests en verde (incluye test headless del TUI sobre 10 pantallas).
- Verificado end-to-end en floci real: gestión + todas las operaciones por componente.
- Binario instalado globalmente (`go install`), invocable como `keel` desde cualquier carpeta.
