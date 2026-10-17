# Memoria técnica — keel (Local Infrastructure-as-a-Service)

Este dossier documenta **qué construimos, por qué, qué problema resuelve y cómo**. Es el
registro completo del proyecto `poc-local-infra` / `keel`.

## Índice

1. [Visión y problema](01-vision-y-problema.md) — qué dolor resolvemos y para quién.
2. [Decisiones clave](02-decisiones.md) — floci vs ministack, floci por defecto, Secrets
   Manager, CLI+TUI, capa de operaciones… con su justificación.
3. [Arquitectura](03-arquitectura.md) — capas, drivers, cómo floci expone los engines,
   endpoints host/docker, inyección de conexiones.
4. [Proceso (cronología)](04-proceso.md) — cómo llegamos aquí: hitos, verificación real,
   bugs encontrados y corregidos.
5. [Hallazgos sobre floci](05-hallazgos-floci.md) — lo aprendido empíricamente del emulador
   (endpoints, puertos, aislamiento, límites).
6. [Hallazgos sobre Vault y GCP](06-hallazgos-vault-gcp.md) — Vault persistente
   (init/unseal) y el emulador de Identity Platform (puertos, persistencia, tokens).

## Resumen en una frase

**keel** es una CLI (con TUI interactivo) que levanta infraestructura **con forma de
nube** en tu máquina vía Docker — AWS con [floci](https://floci.io) (Postgres≡RDS,
Redis≡ElastiCache, Kafka≡MSK, S3, SNS, SQS, Secrets Manager, DynamoDB), GCP con sus
emuladores oficiales (Identity Platform) y Vault — para que cualquier repo, en cualquier
lenguaje, cree los componentes que necesita, se conecte a ellos y guarde su configuración
de aplicación en Vault o Secrets Manager, de forma gestionable, auditable y agnóstica al
microservicio.

## Documentos relacionados

- [`README.md`](../../README.md) — guía de uso y referencia de comandos.
- [`docs/CONNECTION_INJECTION_PATTERN.md`](../CONNECTION_INJECTION_PATTERN.md) — el patrón
  de inyección de conexiones que la herramienta implementa.
