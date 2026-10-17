# Testing Avanzado — Go Idiomatic Reference

## TestMain — Setup y Teardown Global

`TestMain` es el punto de entrada de la suite completa. Se usa cuando los tests de un paquete necesitan infraestructura compartida: una conexión a base de datos, un servidor de prueba, archivos temporales, o un logger configurado.

```go
// El archivo se llama main_test.go por convención
package user_test

var testDB *pgxpool.Pool

func TestMain(m *testing.M) {
    // Setup: todo lo que debe existir ANTES de cualquier test
    pool, cleanup, err := setupTestDatabase()
    if err != nil {
        log.Fatalf("TestMain: setup db: %v", err)
    }
    testDB = pool

    // m.Run() ejecuta todos los Test*, Benchmark*, Fuzz* del paquete
    // El código de salida refleja si los tests pasaron o fallaron
    exitCode := m.Run()

    // Teardown: se ejecuta SIEMPRE, incluso si los tests fallan
    cleanup()

    os.Exit(exitCode) // obligatorio — sin esto el proceso no termina limpiamente
}
```

### Cuándo usar TestMain (y cuándo no)

| Situación | ¿TestMain? |
|---|---|
| Conexión a DB compartida entre tests | Sí |
| Servidor HTTP de prueba compartido | Sí |
| Variables de entorno globales | Sí |
| Setup específico de un solo test | No — usar `t.Cleanup()` en ese test |
| Setup de subtests | No — usar `t.Run()` con setup local |

### Pattern: Test Fixtures con t.Cleanup

```go
// helpers_test.go
func newTestUser(t *testing.T, db *pgxpool.Pool) *user.User {
    t.Helper() // marca esta función como helper; los errores apuntan al llamador

    u, err := user.New("test@example.com")
    if err != nil {
        t.Fatalf("newTestUser: %v", err)
    }
    if err := db.Create(context.Background(), u); err != nil {
        t.Fatalf("newTestUser create: %v", err)
    }

    // El cleanup se registra en el test que llama, no aquí
    t.Cleanup(func() {
        db.Delete(context.Background(), u.ID)
    })

    return u
}
```

---

## Build Tags — Separar Unit de Integration Tests

La convención estándar es que los integration tests requieran un tag explícito para no correr en cada `go test ./...`:

```go
// Al inicio de cualquier archivo de integration test
//go:build integration

package user_test
```

```bash
# Solo unit tests (el default en CI rápido)
go test ./...

# Unit + integration (en CI completo o localmente)
go test -tags integration ./...
```

---

## testcontainers-go — Integration Tests con Infraestructura Real

`testcontainers-go` levanta contenedores Docker reales durante los tests. Sin mocks frágiles — tu código corre contra PostgreSQL real, Redis real, Kafka real.

### Setup de PostgreSQL para Tests

```go
//go:build integration

package user_test

import (
    "context"
    "testing"

    "github.com/testcontainers/testcontainers-go"
    "github.com/testcontainers/testcontainers-go/modules/postgres"
    "github.com/testcontainers/testcontainers-go/wait"
)

func TestMain(m *testing.M) {
    ctx := context.Background()

    // Levanta PostgreSQL en Docker
    pgContainer, err := postgres.RunContainer(ctx,
        testcontainers.WithImage("postgres:16-alpine"),
        postgres.WithDatabase("testdb"),
        postgres.WithUsername("test"),
        postgres.WithPassword("test"),
        testcontainers.WithWaitStrategy(
            wait.ForLog("database system is ready to accept connections").
                WithOccurrence(2), // postgres imprime esto dos veces al arrancar
        ),
    )
    if err != nil {
        log.Fatalf("start postgres: %v", err)
    }

    // Obtén el DSN dinámico — el puerto cambia en cada run
    dsn, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
    if err != nil {
        log.Fatalf("connection string: %v", err)
    }

    // Abre el pool y corre migraciones
    pool, err := pgxpool.New(ctx, dsn)
    if err != nil {
        log.Fatalf("open pool: %v", err)
    }
    if err := runMigrations(dsn); err != nil {
        log.Fatalf("migrations: %v", err)
    }

    testDB = pool
    exitCode := m.Run()

    pool.Close()
    pgContainer.Terminate(ctx)
    os.Exit(exitCode)
}
```

### Test de Integración Completo

```go
//go:build integration

func TestUserStore_CreateAndGet(t *testing.T) {
    t.Parallel()

    store := &user.PostgresStore{Pool: testDB}
    ctx := context.Background()

    // Create
    u, err := user.New("integration@example.com")
    if err != nil {
        t.Fatalf("new user: %v", err)
    }
    if err := store.Create(ctx, u); err != nil {
        t.Fatalf("create: %v", err)
    }
    t.Cleanup(func() { store.Delete(ctx, u.ID) })

    // Get
    got, err := store.GetByID(ctx, u.ID)
    if err != nil {
        t.Fatalf("get: %v", err)
    }
    if got.Email != u.Email {
        t.Errorf("email: got %q, want %q", got.Email, u.Email)
    }
}
```

---

## Fuzzing — Encontrar Bugs que los Tests Manuales No Encuentran

El fuzzing genera inputs automáticamente, mutando un corpus inicial para encontrar panics, errores no manejados, o comportamientos inesperados.

### Estructura de un Fuzz Test

```go
// El nombre DEBE empezar con Fuzz
func FuzzParseConfig(f *testing.F) {
    // Corpus inicial: casos que el fuzzer usa como punto de partida para mutar
    f.Add([]byte(`{"host":"localhost","port":5432}`))
    f.Add([]byte(`{}`))
    f.Add([]byte(``))

    // f.Fuzz es la función que el runtime llama con inputs mutados
    f.Fuzz(func(t *testing.T, data []byte) {
        // El contrato de un fuzz test: nunca debe panic
        // Si parseConfig hace panic, el fuzzer lo reporta como fallo
        cfg, err := parseConfig(data)
        if err != nil {
            return // error está bien — panic no está bien
        }

        // Invariante: si parseó, debe poder volver a serializar
        if _, err := json.Marshal(cfg); err != nil {
            t.Errorf("round-trip failed: %v", err)
        }
    })
}
```

### Cuándo es útil el Fuzzing

- Parsers de cualquier formato (JSON, CSV, binario, protobuf)
- Funciones que trabajan con input de usuario o de red
- Serialización/deserialización (round-trip invariants)
- Funciones con muchos edge cases numéricos (divide, bit operations)

### Cómo Correrlo

```bash
# Corre el fuzz test por 30 segundos acumulando corpus
go test -fuzz=FuzzParseConfig -fuzztime=30s

# Corre solo con el corpus existente (como test normal, rápido, para CI)
go test -run=FuzzParseConfig

# El corpus encontrado se guarda en testdata/fuzz/FuzzParseConfig/
# Commitearlo: el CI lo usará como regression test permanente
```

### Patrón: Invariant Testing con Fuzzing

```go
func FuzzCompress(f *testing.F) {
    f.Add([]byte("hello world"))
    f.Add([]byte(""))
    f.Add(make([]byte, 1000)) // slice de ceros

    f.Fuzz(func(t *testing.T, original []byte) {
        // Invariante: compress → decompress debe producir el input original
        compressed, err := compress(original)
        if err != nil {
            return
        }

        recovered, err := decompress(compressed)
        if err != nil {
            t.Fatalf("decompress failed on valid compressed data: %v", err)
        }

        if !bytes.Equal(original, recovered) {
            t.Fatalf("round-trip failed:\noriginal:  %q\nrecovered: %q", original, recovered)
        }
    })
}
```

---

## Golden Files — Snapshot Testing

Los golden files guardan el output esperado en archivos. Útil para JSON responses, HTML generado, mensajes formateados — cualquier output complejo donde escribir el expected value inline sería tedioso.

```go
func TestGenerateReport(t *testing.T) {
    got := GenerateReport(testData)

    // El path por convención: testdata/golden/<testname>.golden
    goldenPath := filepath.Join("testdata", "golden", "report.golden")

    // -update regenera los golden files en lugar de comparar
    // Uso: go test -run TestGenerateReport -update
    if *update {
        if err := os.WriteFile(goldenPath, []byte(got), 0644); err != nil {
            t.Fatalf("write golden: %v", err)
        }
        return
    }

    want, err := os.ReadFile(goldenPath)
    if err != nil {
        t.Fatalf("read golden (run with -update to create): %v", err)
    }

    if got != string(want) {
        t.Errorf("output mismatch:\ngot:\n%s\nwant:\n%s", got, want)
    }
}

// Flag global — declarar en main_test.go o en el mismo archivo
var update = flag.Bool("update", false, "regenerate golden files")
```

---

## Estrategia de Testing — Qué Tipo de Test Para Qué

| Situación | Tipo de Test | Herramienta |
|---|---|---|
| Lógica pura sin dependencias | Unit + table-driven | stdlib testing |
| Código con dependencias externas | Unit + mock por interface | interface manual |
| Parser o función con mucho input space | Fuzz | testing.F |
| Performance crítica | Benchmark | testing.B + ReportAllocs |
| Output complejo o que cambia poco | Golden file | testdata/golden/ |
| Flujo completo contra infra real | Integration | testcontainers-go |
| Concurrencia | Unit + `-race` + goleak | testing + goleak |

**Regla de pirámide:** Muchos unit tests rápidos → Pocos integration tests lentos. Los integration tests con containers son lentos pero irremplazables para verificar SQL real, transacciones, y comportamiento de red.
