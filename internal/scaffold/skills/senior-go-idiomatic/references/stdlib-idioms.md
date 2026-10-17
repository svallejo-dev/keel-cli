# Stdlib Idioms — Go Idiomatic Reference

## net/http — Middleware Chain

```go
// Middleware type alias for clarity
type Middleware func(http.Handler) http.Handler

// Chain applies middlewares right-to-left (outermost first)
func Chain(h http.Handler, middlewares ...Middleware) http.Handler {
    for i := len(middlewares) - 1; i >= 0; i-- {
        h = middlewares[i](h)
    }
    return h
}

// Example middleware: logging
func Logging(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()
        next.ServeHTTP(w, r)
        slog.Info("request",
            "method", r.Method,
            "path", r.URL.Path,
            "duration", time.Since(start),
        )
    })
}

// Example middleware: timeout per request
func Timeout(d time.Duration) Middleware {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            ctx, cancel := context.WithTimeout(r.Context(), d)
            defer cancel()
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}

// Usage
mux := http.NewServeMux()
mux.HandleFunc("/users", usersHandler)
handler := Chain(mux, Logging, Timeout(10*time.Second), Auth)
```

---

## Context Patterns

### WithValue — Type-Safe Keys
```go
// ALWAYS use unexported type for context keys to avoid collisions
type contextKey string
const requestIDKey contextKey = "requestID"

func WithRequestID(ctx context.Context, id string) context.Context {
    return context.WithValue(ctx, requestIDKey, id)
}

func RequestIDFrom(ctx context.Context) (string, bool) {
    id, ok := ctx.Value(requestIDKey).(string)
    return id, ok
}
```

### Propagation Chain — The Full Pattern
```go
func HandleRequest(ctx context.Context, req *Request) error {
    ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
    defer cancel()

    if err := validate(ctx, req); err != nil {
        return fmt.Errorf("validate: %w", err)
    }

    result, err := fetchData(ctx, req.ID) // ctx flows downstream
    if err != nil {
        return fmt.Errorf("fetchData: %w", err)
    }

    return persist(ctx, result)
}
```

---

## OS Signals — Reactive Patterns

### Graceful Shutdown (The Standard)
```go
func main() {
    srv := &http.Server{Addr: ":8080"}

    // Go 1.16+: signal.NotifyContext integrates signals into context tree
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer stop()

    go func() {
        if err := srv.ListenAndServe(); err != http.ErrServerClosed {
            log.Fatalf("server error: %v", err)
        }
    }()

    <-ctx.Done() // blocks until SIGTERM or SIGINT
    stop()       // release signal resources

    shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
    defer cancel()
    if err := srv.Shutdown(shutdownCtx); err != nil {
        log.Printf("shutdown error: %v", err)
    }
}
```

### Multi-Signal Reactive Daemon
```go
func runDaemon(ctx context.Context) error {
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGUSR1, syscall.SIGUSR2, syscall.SIGTERM)
    defer signal.Stop(sigCh)

    for {
        select {
        case sig := <-sigCh:
            switch sig {
            case syscall.SIGHUP:
                if err := reloadConfig(); err != nil {
                    slog.Error("config reload failed", "err", err)
                }
            case syscall.SIGUSR1:
                activateProfiling()
            case syscall.SIGUSR2:
                toggleDebugLogging()
            case syscall.SIGTERM:
                return nil // clean exit
            }
        case <-ctx.Done():
            return ctx.Err()
        }
    }
}
```

### Signal Map — Production Use Cases
| Signal | Use Case |
|---|---|
| SIGTERM / SIGINT | Graceful shutdown |
| SIGHUP | Config hot-reload without restart |
| SIGUSR1 | Activate profiling, rotate logs, dump stats |
| SIGUSR2 | Toggle feature flags, change log level at runtime |
| SIGPIPE | Handle broken TCP connections in servers |

---

## encoding/json — Streaming Patterns

### Decode From Reader (Not Bytes)
```go
// BAD: loads entire body into memory
body, _ := io.ReadAll(r.Body)
json.Unmarshal(body, &v)

// GOOD: stream decode directly
if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
    return fmt.Errorf("decode request: %w", err)
}
```

### Custom Marshaler
```go
type Duration struct{ time.Duration }

func (d Duration) MarshalJSON() ([]byte, error) {
    return json.Marshal(d.String())
}

func (d *Duration) UnmarshalJSON(b []byte) error {
    var s string
    if err := json.Unmarshal(b, &s); err != nil {
        return err
    }
    dur, err := time.ParseDuration(s)
    if err != nil {
        return err
    }
    d.Duration = dur
    return nil
}
```

---

## log/slog — Structured Logging (Go 1.21+)

```go
// Initialize once at startup
logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelInfo,
}))
slog.SetDefault(logger)

// Usage: always structured, never fmt.Sprintf
slog.Info("user created",
    "user_id", user.ID,
    "email", user.Email,
    "duration_ms", time.Since(start).Milliseconds(),
)

slog.Error("database error",
    "err", err,
    "query", query,
    "user_id", userID,
)
```

---

## time Package — Footguns

### time.After in Loops — Memory Leak
```go
// BAD: creates a new timer goroutine every iteration, leaks until it fires
for {
    select {
    case <-time.After(5 * time.Second): // new goroutine per select
        doWork()
    }
}

// GOOD: one timer, reset between uses
timer := time.NewTimer(5 * time.Second)
defer timer.Stop()
for {
    select {
    case <-timer.C:
        doWork()
        timer.Reset(5 * time.Second)
    }
}
```

### time.Ticker — For Periodic Work
```go
// Ticker is the right tool for recurring work
ticker := time.NewTicker(1 * time.Second)
defer ticker.Stop()

for {
    select {
    case t := <-ticker.C:
        processAt(t)
    case <-ctx.Done():
        return
    }
}
```

---

## go:embed — Static Files in Binary

```go
import _ "embed"
import "embed"

// Single file
//go:embed config/default.yaml
var defaultConfig []byte

// Entire directory tree
//go:embed templates
var templateFS embed.FS

// Usage
tmpl, err := template.ParseFS(templateFS, "templates/*.html")
```

---

## Build Tags — Conditional Compilation

```go
// File only compiled for Linux and macOS
//go:build linux || darwin

// File only compiled when 'integration' tag is passed
//go:build integration

// Run: go test -tags integration ./...
```

---

## Toolchain — Production Binary

### Makefile with Version Injection
```makefile
VERSION   := $(shell git describe --tags --always --dirty)
COMMIT    := $(shell git rev-parse --short HEAD)
DATE      := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS   := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE) -trimpath"

build:
	go build $(LDFLAGS) -o bin/app ./cmd/app

lint:
	golangci-lint run ./...

test:
	go test -race -count=1 ./...

vulncheck:
	govulncheck ./...
```

### Cross-Compilation
```bash
# Linux AMD64 from any OS
GOOS=linux GOARCH=amd64 go build -o bin/app-linux ./cmd/app

# ARM64 for Raspberry Pi
GOOS=linux GOARCH=arm64 go build -o bin/app-arm64 ./cmd/app

# Windows
GOOS=windows GOARCH=amd64 go build -o bin/app.exe ./cmd/app
```
