# Patrón: Configuración Externa Resuelta + Inyección de Conexiones en el Arranque

> Agnóstico de lenguaje, cloud provider y tipo de servicio de infraestructura (DB relacional, NoSQL, cache, cola de mensajería, motor de búsqueda, HTTP externo, etc.). Extraído de la convención compartida por múltiples microservicios de una misma organización; documentado aquí como patrón reutilizable para nuevos proyectos.

## 1. Intención

Que **ningún componente de negocio decida por sí mismo** a qué host, puerto o credencial conectarse, ni cuándo abrir esa conexión. Toda la resolución de "con qué me conecto y cómo" ocurre en una única fase de arranque, antes de que el servicio empiece a atender tráfico. El resto del código solo recibe conexiones ya abiertas y verificadas, como una dependencia más.

Esto separa dos preocupaciones que suelen mezclarse por comodidad:
- **Qué credenciales/host usar** (varía por ambiente, es secreto, cambia con el tiempo).
- **Qué hace el negocio con esa conexión** (no debería cambiar nunca por el ambiente).

## 2. Cuándo aplica

- Servicios de larga duración (procesos que corren de forma continua, no funciones serverless de vida cortísima).
- Más de un servicio de infraestructura externo (DB, cache, cola, buscador, APIs internas), especialmente si conviven varios roles del mismo motor (ej. réplica de lectura vs. escritura).
- Múltiples ambientes de despliegue (local, QA, staging, producción) que comparten el mismo binario/imagen pero difieren en endpoints y credenciales.
- Orquestación con inyección de configuración externa disponible (variables de entorno, montajes de archivos, metadata del entorno de ejecución) — no requiere un orquestador específico, solo *algún* mecanismo de inyección al proceso.

No aplica igual de bien a: funciones muy efímeras donde el costo de "resolver todo antes de servir tráfico" no se amortiza, o sistemas de un solo servicio de infraestructura sin variación entre ambientes.

## 3. Componentes del patrón

```
┌────────────────────┐     ┌───────────────────────┐     ┌─────────────────────────┐
│ Gestor de secretos  │────▶│ Mecanismo de entrega   │────▶│ Superficie de entrada    │
│ (credenciales)       │     │ al proceso             │     │ al proceso               │
└────────────────────┘     │ (sync/mount periódico) │     │ (env vars / archivos /   │
┌────────────────────┐     └───────────────────────┘     │  metadata del entorno)   │
│ Config no sensible   │────────────────▶───────────────▶│                          │
│ (flags, timeouts,    │                                  └────────────┬─────────────┘
│  hosts lógicos)      │                                               │
└────────────────────┘                                               ▼
                                                      ┌──────────────────────────────┐
                                                      │  Resolución de configuración  │
                                                      │  (capas con prioridad fija)   │
                                                      └───────────────┬───────────────┘
                                                                      ▼
                                                      ┌──────────────────────────────┐
                                                      │   Objeto de config único,     │
                                                      │   inmutable durante la vida   │
                                                      │   de cada request             │
                                                      └───────────────┬───────────────┘
                                                                      ▼
                                                      ┌──────────────────────────────┐
                                                      │  Fase de arranque (bootstrap) │
                                                      │  construye cada conexión UNA  │
                                                      │  vez, la valida (ping/health) │
                                                      └───────────────┬───────────────┘
                                                                      ▼
                                                      ┌──────────────────────────────┐
                                                      │  Inyección por constructor a  │
                                                      │  cada componente de negocio   │
                                                      └──────────────────────────────┘
```

### 3.1 Gestor de secretos
Cualquier almacén de secretos gestionado (vault, secret manager, KMS-backed store, etc.). Regla dura: **el código de la aplicación nunca lo llama directamente**. Un proceso externo al servicio sincroniza esos secretos hacia el mecanismo de entrega.

### 3.2 Config no sensible
Flags de features, timeouts, tamaños de pool, nombres lógicos de host — cosas que no son secretas pero sí varían por ambiente. Vive en un artefacto de configuración versionable (archivo de config, config map, parameter store no cifrado).

### 3.3 Mecanismo de entrega al proceso
El puente entre "dónde vive el dato" y "cómo lo ve el proceso". Debe entregar tanto secretos como config no sensible **de la misma forma** (mismo formato de acceso) para que la capa de resolución no tenga que distinguir el origen.

### 3.4 Resolución de configuración (capas con prioridad fija)
Se definen 2–3 capas, cargadas en orden, donde cada capa puede sobreescribir a la anterior:

1. **Defaults embebidos** — valores seguros de fallback, versionados junto al código.
2. **Config de despliegue** — override por ambiente, gestionado fuera del código de negocio.
3. **Entrega en tiempo de arranque** (env vars u otro mecanismo de entrega) — **siempre gana**, es la única capa que puede contener secretos reales.

Regla dura: la capa 3 siempre se aplica al final, sin excepciones, y sin importar si las capas 1–2 ya definieron un valor.

### 3.5 Objeto de configuración único
El resultado de la resolución es **una sola estructura de datos**, construida una vez, que contiene todo lo necesario para todas las conexiones del servicio. No se relee configuración desperdigada en cada punto de uso.

Opcional: si el orquestador soporta actualizar config sin reiniciar el proceso, se puede añadir recarga en caliente **solo para la config no sensible** (flags, timeouts). Las conexiones ya abiertas no se recrean automáticamente por este mecanismo — recrearlas es una decisión aparte, explícita.

### 3.6 Fase de arranque (bootstrap)
Un único punto de entrada del proceso construye, en orden, cada cliente de infraestructura (uno por rol: lectura, escritura, auth, etc.), usando el objeto de configuración ya resuelto. Cada construcción:

- Recibe únicamente los parámetros que necesita del objeto de config (no el objeto completo si se puede evitar).
- Verifica la conexión antes de darla por buena (ping/health-check).
- Decide explícitamente si un fallo aquí es fatal (aborta el arranque) o degradado (loguea y continúa sin esa dependencia).

### 3.7 Inyección por constructor
Cada cliente ya construido y verificado se **pasa como argumento** a los componentes que lo necesitan (repositorios, servicios). Ningún componente de negocio:
- lee variables de entorno directamente,
- construye su propia conexión,
- ni pide una conexión a un registro global (anti-patrón: *service locator*).

## 4. Criterios de diseño (checklist)

- [ ] **Ningún secreto en código ni en config versionada** — solo placeholders/ejemplos en lo que se versiona.
- [ ] **Una sola fuente de verdad por proceso**: el objeto de configuración resuelto, no variables leídas ad-hoc en distintos módulos.
- [ ] **Prioridad de capas fija y documentada**: quien lea el código debe poder predecir qué gana sin ejecutar nada.
- [ ] **Construcción de conexiones centralizada en un único punto de arranque**, nunca dispersa.
- [ ] **Un cliente por rol, no por servicio**: si el mismo motor tiene lectura/escritura/u otro propósito con distinto endpoint, son instancias de cliente separadas, construidas con la misma función genérica parametrizada.
- [ ] **Decisión explícita de fatal vs. degradado** por cada dependencia al arrancar (no todas deben tratarse igual).
- [ ] **Sin reconexión ad-hoc en medio de la lógica de negocio** — si una operación necesita una conexión, usa la que ya existe; abrir una nueva por request es la excepción, no la regla, y debe justificarse (p. ej. conexiones de corta vida por diseño del protocolo).
- [ ] **Sin descubrimiento dinámico de servicios en el código de negocio** — la topología de a qué conectarse es un dato de configuración, no una decisión en runtime del componente que la usa.
- [ ] **Componentes de negocio libres de I/O de arranque**: reciben clientes ya listos por constructor; no conocen el mecanismo de entrega de configuración ni el gestor de secretos.

## 5. Instrucciones de implementación (paso a paso, agnóstico de stack)

1. **Definir el esquema de configuración** como una estructura de datos plana o anidada por dominio (una sección por servicio de infraestructura: cache, base de datos, cola, buscador, etc.). Este esquema vive en la capa de dominio/negocio, sin importar ni depender de ningún SDK de infraestructura.
2. **Escribir defaults seguros** para ese esquema, embebidos junto al código (nunca con secretos reales, solo valores funcionales para desarrollo local sin credenciales).
3. **Elegir un mecanismo de entrega** compatible con el entorno de ejecución (variables de entorno es el mínimo común denominador multi-cloud y multi-orquestador). Todo el config no sensible y todos los secretos deben poder llegar por ese mismo mecanismo.
4. **Implementar la función de resolución** que aplica las capas en orden fijo (defaults → config de despliegue → entrega en arranque), documentando explícitamente que la última capa nunca se salta.
5. **Aislar la resolución en un único módulo/función de arranque**, invocada una sola vez al iniciar el proceso, que entrega el objeto de configuración ya completo a quien lo necesite.
6. **Escribir una función constructora por cada cliente de infraestructura**, parametrizada (recibe host/puerto/credencial/timeouts como argumentos simples, no el objeto de configuración completo), que:
   - construye el cliente nativo del servicio,
   - lo valida (ping/health-check),
   - retorna el cliente listo o un error explícito.
7. **Invocar esas funciones constructoras una sola vez**, en el punto de entrada del proceso, en el orden que tenga sentido de dependencia (ej. lo que otros necesiten primero).
8. **Decidir, por cada una, si un fallo bloquea el arranque o solo se loguea** — y dejarlo explícito en el código (no un manejo de errores genérico compartido).
9. **Pasar cada cliente ya construido como argumento** a los constructores de los repositorios/servicios de negocio que lo consumen. No exponer un registro global de clientes ni un mecanismo de "pedir la conexión X desde cualquier parte".
10. **Prohibir, por revisión de código, cualquier acceso a variables de entorno o a SDKs de secretos fuera de la fase de arranque.** Si un componente de negocio necesita un nuevo dato de configuración, se agrega al esquema (paso 1) y se inyecta, no se lee inline.
11. **(Opcional) Agregar recarga en caliente** solo para la config no sensible, si el mecanismo de entrega lo permite (ej. archivos montados que cambian). Las conexiones activas no se recrean automáticamente; si se necesita reconexión dinámica, es una decisión de diseño aparte y explícita.

## 6. Anti-patrones que este patrón evita

| Anti-patrón | Por qué es un problema | Cómo lo evita este patrón |
|---|---|---|
| Leer `getenv(...)` disperso en cualquier archivo | Nadie puede saber qué variables usa el sistema sin grepear todo el código; imposible auditar | Un único módulo de resolución concentra toda lectura de entorno |
| Conexión perezosa creada dentro del handler de una request | Latencia variable, fugas de conexión, imposible saber si la infraestructura está sana antes de servir tráfico | Conexiones se abren y validan en el arranque, antes de aceptar tráfico |
| Service locator / registro global de conexiones | Acopla todo el código a un singleton oculto, dificulta testing y razonar sobre dependencias | Inyección explícita por constructor, dependencias visibles en la firma |
| Secretos embebidos en archivos de configuración versionados | Fuga de credenciales por control de versiones | Secretos solo entran por el mecanismo de entrega en tiempo de arranque, nunca versionados |
| Un solo cliente compartido para roles distintos (ej. lectura y escritura) | Imposible aplicar políticas distintas (timeouts, réplicas, permisos) por rol | Un cliente por rol, construido con la misma función genérica parametrizada |
| Tratar todas las dependencias igual ante un fallo de arranque | Una cache caída no debería tumbar el servicio igual que una base de datos primaria caída | Decisión explícita fatal-vs-degradado por dependencia |

## 7. Resultado esperado al aplicar el patrón

- Se puede saber, leyendo un solo módulo, **toda** la superficie de configuración externa del servicio.
- Cambiar de ambiente (local → staging → producción) no requiere tocar código, solo lo que el mecanismo de entrega inyecta.
- Agregar un nuevo servicio de infraestructura implica: extender el esquema, escribir su función constructora, invocarla en el arranque, inyectarla donde se use — sin tocar el resto del sistema.
- Testear componentes de negocio no requiere levantar infraestructura real: basta con inyectar un cliente falso/mock en el constructor, porque la dependencia siempre llega por parámetro.

## 8. Ejemplos (few-shots) en Go

Cada ejemplo mapea a una sección de la checklist (§4) y del paso a paso (§5). El servicio de infraestructura usado (una cola genérica) es intercambiable — la forma es lo que importa, no el producto concreto.

### 8.1 Esquema de configuración (paso 1) — vive en el dominio, sin imports de infraestructura

```go
// domain/config/types.go
package config

// AppConfig es el ÚNICO objeto que agrupa toda la config externa del servicio.
// No importa ningún SDK de infraestructura: solo tipos primitivos.
type AppConfig struct {
    Environment string

    Cache CacheConfig
    Queue QueueConfig
    DB    DBConfig
}

type CacheConfig struct {
    HostRead  string
    HostWrite string
    Port      string
    Password  string
    DB        int
    PoolSize  int
}

type QueueConfig struct {
    Host         string
    Port         int
    User         string
    Password     string
    ExchangeName string
}

type DBConfig struct {
    DSNRead         string
    DSNWrite        string
    MaxOpenConns    int
    MaxIdleConns    int
}

// DefaultConfig entrega valores de fallback seguros para desarrollo local.
// NUNCA contiene secretos reales.
func DefaultConfig() *AppConfig {
    return &AppConfig{
        Environment: "local",
        Cache: CacheConfig{
            HostRead: "localhost", HostWrite: "localhost",
            Port: "6379", PoolSize: 20,
        },
        DB: DBConfig{MaxOpenConns: 10, MaxIdleConns: 5},
    }
}
```

❌ **Forma incorrecta** (mezcla dominio con infraestructura, esconde qué variables usa el sistema):

```go
// MAL: cada paquete lee sus propias env vars donde las necesita
func NewCacheClient() *redis.Client {
    return redis.NewClient(&redis.Options{
        Addr:     os.Getenv("CACHE_HOST") + ":" + os.Getenv("CACHE_PORT"), // disperso, no auditable
        Password: os.Getenv("CACHE_PASSWORD"),
    })
}
```

### 8.2 Resolución en capas (paso 4) — un único punto que aplica prioridad fija

```go
// infrastructure/config/resolve.go
package config

import (
    "os"
    "strconv"

    domain "myservice/domain/config"
)

// Resolve aplica: defaults -> archivo de despliegue -> variables de entorno (máxima prioridad).
// Es la ÚNICA función del sistema que lee os.Getenv.
func Resolve(deploymentFilePath string) (*domain.AppConfig, error) {
    cfg := domain.DefaultConfig()

    if err := loadYAMLInto(deploymentFilePath, cfg); err != nil {
        // Config de despliegue ausente no es fatal: seguimos con defaults + env vars.
        log.Warn("no se pudo cargar config de despliegue, se usan defaults", "error", err)
    }

    applyEnvOverrides(cfg) // SIEMPRE al final, sin excepciones
    return cfg, nil
}

func applyEnvOverrides(c *domain.AppConfig) {
    if v := os.Getenv("CACHE_HOST_READ"); v != "" {
        c.Cache.HostRead = v
    }
    if v := os.Getenv("CACHE_HOST_WRITE"); v != "" {
        c.Cache.HostWrite = v
    }
    if v := os.Getenv("CACHE_PASSWORD"); v != "" {
        c.Cache.Password = v
    }
    if v := os.Getenv("QUEUE_HOST"); v != "" {
        c.Queue.Host = v
    }
    if v := os.Getenv("DB_DSN_WRITE"); v != "" {
        c.DB.DSNWrite = v
    }
    if v := os.Getenv("DB_POOL_MAX_OPEN_CONNS"); v != "" {
        if n, err := strconv.Atoi(v); err == nil {
            c.DB.MaxOpenConns = n
        }
    }
    // ... resto de campos, mismo patrón: "si vino algo, pisa el valor anterior"
}
```

### 8.3 Funciones constructoras por rol (paso 6) — parametrizadas, no reciben el config completo

```go
// infrastructure/cache/client.go
package cache

import (
    "context"
    "fmt"
    "time"

    "github.com/redis/go-redis/v9"
)

// newClient es la función genérica reutilizada por cada rol (read/write/auth).
// Recibe parámetros simples, no el AppConfig completo: no necesita saber
// que existen otras secciones de configuración.
func newClient(host, port, password string, db, poolSize int) (*redis.Client, error) {
    client := redis.NewClient(&redis.Options{
        Addr:     fmt.Sprintf("%s:%s", host, port),
        Password: password,
        DB:       db,
        PoolSize: poolSize,
    })

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    if _, err := client.Ping(ctx).Result(); err != nil {
        return nil, fmt.Errorf("cache no disponible en %s:%s: %w", host, port, err)
    }
    return client, nil
}

// NewReadClient y NewWriteClient son instancias de rol distintas del mismo motor.
func NewReadClient(cfg domain.CacheConfig) (*redis.Client, error) {
    return newClient(cfg.HostRead, cfg.Port, cfg.Password, cfg.DB, cfg.PoolSize)
}

func NewWriteClient(cfg domain.CacheConfig) (*redis.Client, error) {
    return newClient(cfg.HostWrite, cfg.Port, cfg.Password, cfg.DB, cfg.PoolSize)
}
```

### 8.4 Bootstrap (pasos 7–8) — construcción centralizada, con decisión explícita fatal-vs-degradado

```go
// cmd/server/main.go
package main

func main() {
    cfg, err := config.Resolve("configs/app-config.yaml")
    if err != nil {
        log.Fatal("no se pudo resolver configuración", "error", err)
    }

    // Cache: fallo NO es fatal — el servicio sigue en modo degradado sin cache.
    cacheRead, err := cache.NewReadClient(cfg.Cache)
    if err != nil {
        log.Warn("cache read no disponible, arrancando sin cache", "error", err)
        cacheRead = nil // los repositorios deben tolerar un cliente nil
    }
    cacheWrite, err := cache.NewWriteClient(cfg.Cache)
    if err != nil {
        log.Warn("cache write no disponible, arrancando sin cache", "error", err)
    }

    // DB: fallo SÍ es fatal — sin base de datos el servicio no tiene sentido operar.
    dbWrite, err := db.NewWriteConnection(cfg.DB)
    if err != nil {
        log.Fatal("no se pudo conectar a la base de datos de escritura", "error", err)
    }

    queuePublisher, err := queue.NewPublisher(cfg.Queue)
    if err != nil {
        log.Warn("cola no disponible, notificaciones async deshabilitadas", "error", err)
    }

    // Inyección por constructor: cada dependencia ya resuelta se entrega explícitamente.
    deviceRepo := repository.NewDeviceRepository(dbWrite, cacheRead, cacheWrite)
    notifier := service.NewNotifierService(queuePublisher)
    handler := api.NewDeviceHandler(deviceRepo, notifier)

    server.Start(handler)
}
```

### 8.5 Inyección por constructor en el componente de negocio (paso 9)

```go
// application/repository/device_repository.go
package repository

// DeviceRepository SOLO conoce las interfaces que necesita, nunca cómo se construyeron.
type DeviceRepository struct {
    db         DBConn
    cacheRead  CacheClient
    cacheWrite CacheClient
}

// NewDeviceRepository recibe las conexiones YA construidas y validadas.
// No lee variables de entorno, no llama a ningún SDK de infraestructura directamente.
func NewDeviceRepository(db DBConn, cacheRead, cacheWrite CacheClient) *DeviceRepository {
    return &DeviceRepository{db: db, cacheRead: cacheRead, cacheWrite: cacheWrite}
}

func (r *DeviceRepository) FindByUser(ctx context.Context, userID string) ([]Device, error) {
    if r.cacheRead != nil {
        if devices, ok := r.cacheRead.Get(ctx, userID); ok {
            return devices, nil // camino feliz: cache disponible
        }
    }
    return r.db.QueryDevices(ctx, userID) // cache nil o miss: sigue funcionando igual
}
```

❌ **Forma incorrecta** (service locator / lectura de entorno dentro del componente de negocio):

```go
// MAL: el repositorio arma su propia conexión y lee el entorno directamente
type DeviceRepository struct{}

func (r *DeviceRepository) FindByUser(ctx context.Context, userID string) ([]Device, error) {
    client := redis.NewClient(&redis.Options{ // conexión nueva en cada llamada
        Addr: os.Getenv("CACHE_HOST_READ") + ":" + os.Getenv("CACHE_PORT"),
    })
    defer client.Close()
    // ... imposible de testear sin un Redis real, imposible de auditar
}
```

### 8.6 Beneficio en tests — mock inyectado por constructor, sin infraestructura real

```go
// application/repository/device_repository_test.go
func TestFindByUser_CacheMiss_FallsBackToDB(t *testing.T) {
    fakeCache := &mockCacheClient{shouldMiss: true}
    fakeDB := &mockDBConn{devices: []Device{{ID: "d1"}}}

    repo := repository.NewDeviceRepository(fakeDB, fakeCache, fakeCache)

    devices, err := repo.FindByUser(context.Background(), "user-1")

    require.NoError(t, err)
    require.Len(t, devices, 1) // vino de la DB, no hizo falta cache real
}
```
