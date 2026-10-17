# 5. Hallazgos sobre floci (empíricos)

Lo aprendido probando floci en real (Docker 29.6.1). Útil para mantener/depurar y para decidir
cuándo usar `--provider docker`.

## General

- Imagen `floci/floci:latest` (probada **v1.5.32**, edición “floci-always-free”). Endpoint
  `http://localhost:4566`, cuenta `000000000000`, región `us-east-1`.
- Habilita ~70 servicios AWS (s3, sns, sqs, secretsmanager, msk, rds, elasticache, kinesis,
  kms, dynamodb, …). Health en `/_floci/health` (responde 200 JSON); el contenedor reporta
  `healthy`.
- **Almacenamiento en memoria**: al recrear el contenedor floci se pierden los recursos
  (RDS/redis/kafka). Aceptable para dev local.

## Cómo expone los engines con estado

floci **proxya** los engines en su propia dirección a puertos asignados de forma incremental:

| Engine | API correcta | Puerto reportado | Backing container |
|---|---|---|---|
| **RDS (postgres)** | `CreateDBInstance` (engine `postgres`) | `7001`, `7002`, … (en la IP de floci) | `postgres:16.3-alpine` |
| **ElastiCache (redis)** | `CreateReplicationGroup` ⚠️ (NO `CreateCacheCluster`, que es solo memcached) | `6379`, `6380`, … | `valkey/valkey:8` |
| **MSK (kafka)** | `CreateClusterV2` (serverless, requiere `VpcConfigs`) | broker en contenedor aparte `:9092` | `redpandadata/redpanda` |

### Acceso host vs docker

- **RDS y ElastiCache**: floci los proxya en **su propio hostname** (`floci:7001`, `floci:6379`).
  Publicando esos rangos en el compose (`7000-7060`, `6379-6400`), también son alcanzables desde
  el **host** en `localhost:<puerto>`. → Funcionan en **ambos modos**.
- **MSK/Kafka**: el `BootstrapBrokerString` apunta al **contenedor del broker** (`<ip>:9092`),
  no a un puerto proxied de floci. Es alcanzable desde la **red de contenedores** (docker-mode),
  pero **no desde el host** (el broker anuncia una dirección interna y el puerto no se publica).
  → Kafka por floci es **docker-mode**; para acceso host completo, `--provider docker`
  (redpanda independiente).

### Aislamiento multi-instancia

Verificado: instancias distintas obtienen **puertos distintos** (RDS `7001`/`7002`, redis
`6379`/`6380`). El requisito de “3 Postgres separados” se cumple **también** vía floci.

## Red de Docker

- El driver `dockerx` crea la red `keel-net`. Como la crea **sin etiquetas de compose**,
  el `floci-compose.yml` debe declararla `external: true` **y** debe existir antes de
  `docker compose up`. Por eso `task floci-up` (o el flujo manual) hace primero
  `docker network create keel-net`.

## Operaciones de Kafka (`rpk`)

- Se ejecutan lanzando `rpk` en un **contenedor efímero** (`redpandadata/redpanda`) sobre
  `keel-net`, apuntando `--brokers` al endpoint docker-mode del broker. Funciona igual
  para floci-MSK y para redpanda `--provider docker`.
- Detalle: algunos errores de `rpk` (p. ej. tópico ya existe) se reportan por **stdout**, no
  stderr; la herramienta los captura para dar un mensaje claro.

## Comandos de referencia usados en los spikes

```bash
# RDS
aws --endpoint-url http://floci:4566 rds create-db-instance \
  --db-instance-identifier pg1 --engine postgres --db-instance-class db.t3.micro \
  --allocated-storage 20 --master-username infra --master-user-password pw --db-name demo
aws --endpoint-url http://floci:4566 rds describe-db-instances --db-instance-identifier pg1

# ElastiCache (redis) — ¡ReplicationGroup!
aws --endpoint-url http://floci:4566 elasticache create-replication-group \
  --replication-group-id r1 --replication-group-description r1 --engine redis \
  --cache-node-type cache.t3.micro

# MSK serverless
aws --endpoint-url http://floci:4566 kafka create-cluster-v2 --cluster-name k1 \
  --serverless '{"VpcConfigs":[{"SubnetIds":["subnet-1"],"SecurityGroupIds":["sg-1"]}]}'
aws --endpoint-url http://floci:4566 kafka get-bootstrap-brokers --cluster-arn <arn>
```

## Riesgo y mitigación

floci es un proyecto joven; sus números y comportamientos pueden cambiar. La abstracción
`Provider` permite, si la fidelidad de floci no alcanza para un engine, **cambiar ese blueprint
a `dockerx`** en una línea. Conviene verificar el tag de imagen y los nombres `FLOCI_*` del
compose contra la documentación de floci en cada actualización.
