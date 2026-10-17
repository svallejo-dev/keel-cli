# 1. Visión y problema

## El problema

En una organización con muchos microservicios (en varios lenguajes), cada equipo reinventa
cómo **levantar y cablear su infraestructura local**: una base de datos aquí, un Redis allá,
un Kafka con su compose particular, credenciales a mano, puertos que chocan, y cada repo con
su propia manera de conectarse. Esto genera:

- Fricción para arrancar un proyecto nuevo (“¿qué necesito levantar y cómo me conecto?”).
- Divergencia entre el entorno local y producción (imágenes/engines distintos a los de la nube).
- Configuración de conexión dispersa y no auditable.

## Qué pretendemos resolver

Construir una **plataforma / framework basado en CLI** que, desde local, permita levantar
diferentes servicios de infraestructura para que **cualquier proyecto, repo o lenguaje** se
conecte según sus necesidades. En esencia: un **Infrastructure-as-a-Service local**, al
estilo de Fury (MercadoLibre) o Vercel, pero corriendo íntegramente en la máquina del
desarrollador.

### Objetivos duros (del encargo)

1. **Gestionable**: levantar todos los servicios, levantar uno, **crear instancias**
   independientes (p. ej. 3 Postgres para 3 microservicios), listar, y revisar la **salud**
   de cada componente. Más cualquier característica adicional auditable y coherente.
2. **Basado en Docker** para levantar los servicios.
3. **Paridad AWS** (nube de referencia, criterio *no negociable*): los componentes deben
   nacer/parecerse lo más posible a AWS. Si usamos Kafka, que sea **MSK** (no el Confluent
   nativo); si Redis, **ElastiCache**; si relacional, **RDS/Aurora**; etc.
4. **Emulador de infra**: apoyarse en un servicio tipo [ministack](https://ministack.org) o
   [floci](https://floci.io) para obtener los componentes, eligiendo uno con buen catálogo,
   fácil de gestionar y con soporte de comunidad.
5. **CLI amigable**: fácil de usar, con visión integral de cada componente.
6. **Agnóstico al microservicio**: la plataforma NO construye ni corre los microservicios;
   solo ofrece la infraestructura y el mecanismo de conexión.

## De dónde salió el catálogo inicial

Los servicios que ofrecemos primero se extrajeron de tres *assessments* de arquitectura
de un proyecto real (no incluidos en el repo), leídos **solo para decidir qué infra
ofrecer** (no para implementarlos):

| Dominio | Aporta al catálogo |
|---|---|
| **Autenticación y usuarios** (microservicios Go) | Postgres (write+read), Redis (sesiones/rate-limit), Secrets, event bus (topics) |
| **Monetización y acceso** (varios servicios, pagos) | Postgres database-per-service, Redis, event bus, Secrets, S3 |
| **Bus de eventos central** | Kafka/MSK (varios tópicos), Schema Registry, Redis (dedup), SNS+SQS+DLQ, Postgres, KMS/Secrets |

De ahí el **catálogo inicial**: `postgres`, `redis`, `kafka`, `schema-registry`, `s3`,
`sns`, `sqs`, `secrets` (y engines legacy diferidos: `oracle`, `cassandra`, `rabbitmq`).

Los documentos **no** especifican puertos, nombres de variables ni versiones — esas
convenciones las define la plataforma.

## Cómo lo resolvemos (idea central)

- Un **CLI en Go** (con TUI interactivo) que orquesta, vía Docker, los componentes de infra.
- Los engines nacen **con forma de AWS**: se crean como recursos AWS (RDS, ElastiCache, MSK,
  S3, SNS, SQS, Secrets Manager) a través del emulador **floci**; una app conecta usando el
  SDK de AWS o el protocolo nativo del engine, sin cambios respecto a producción.
- Un **registro** local (estado + auditoría + asignación de puertos) como única fuente de
  verdad, y una capa de **inyección de conexiones** que entrega a cada repo la configuración
  lista para consumir, implementando el
  [patrón de inyección](../CONNECTION_INJECTION_PATTERN.md).
