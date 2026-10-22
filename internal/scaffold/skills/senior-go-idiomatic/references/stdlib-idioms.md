# Stdlib Idioms — Go Idiomatic Reference

## Contents

- net/http: routing with ServeMux patterns, middleware, server timeouts
- Context patterns
- OS signals and graceful shutdown
- encoding/json: streaming decode, limits, custom marshalers
- log/slog
- time: timers, tickers and `time.After`
- go:embed and build constraints
- Toolchain: building release binaries

---

## net/http — Routing and Middleware

Since Go 1.22, `http.ServeMux` patterns accept a method and path wildcards, which covers most of
what third-party routers were used for:

```go
mux := http.NewServeMux()
mux.HandleFunc("GET /users/{id}", getUser)     // r.PathValue("id")
mux.HandleFunc("POST /users", createUser)
mux.HandleFunc("GET /files/{path...}", serveFile) // {name...} matches the rest of the path
mux.HandleFunc("GET /{$}", home)                  // {$} matches "/" exactly, not every path

func getUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// ...
}
```

A request with the wrong method on a known path gets `405 Method Not Allowed` with an `Allow`
header, and the most specific pattern wins regardless of registration order. Reach for a router
library only when you need something the mux lacks, such as route groups with their own
middleware stacks.

### Middleware

Middleware is a function from `http.Handler` to `http.Handler`; no framework type is needed.

```go
type Middleware func(http.Handler) http.Handler

// Chain applies middlewares so that the first one listed is the outermost.
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

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

func Timeout(d time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

handler := Chain(mux, Logging, Timeout(10*time.Second))
```

### Server timeouts

A bare `http.ListenAndServe` has no timeouts, so a slow client can hold a connection open
forever. Configure the server explicitly:

```go
srv := &http.Server{
	Addr:              ":8080",
	Handler:           handler,
	ReadHeaderTimeout: 5 * time.Second,
	ReadTimeout:       30 * time.Second,
	WriteTimeout:      30 * time.Second,
	IdleTimeout:       2 * time.Minute,
}
```

Request bodies are untrusted input: cap them with `http.MaxBytesReader` before decoding (see
`io-streams.md`, "Size limits on untrusted input").

---

## Context Patterns

### WithValue — unexported key types

Use an unexported key type so no other package can collide with or read your value by accident.
Keep request-scoped data (request ID, auth claims) in the context; pass dependencies and optional
parameters explicitly.

```go
type ctxKey int

const requestIDKey ctxKey = iota

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

func RequestIDFrom(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(requestIDKey).(string)
	return id, ok
}
```

### Propagation

```go
func HandleRequest(ctx context.Context, req *Request) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := validate(ctx, req); err != nil {
		return fmt.Errorf("validate: %w", err)
	}
	result, err := fetchData(ctx, req.ID) // ctx flows to every downstream call
	if err != nil {
		return fmt.Errorf("fetch data: %w", err)
	}
	return persist(ctx, result)
}
```

`context.WithoutCancel` (Go 1.21) detaches work that must outlive the request (an audit write)
while keeping its values; `context.AfterFunc` (Go 1.21) runs cleanup when a context ends;
`context.WithCancelCause` lets you report why something was canceled.

---

## OS Signals

### Graceful shutdown

```go
func main() {
	srv := &http.Server{Addr: ":8080", Handler: newHandler(), ReadHeaderTimeout: 5 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server stopped", "err", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		stop() // a second Ctrl-C now kills the process immediately
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown", "err", err)
	}
}
```

`Shutdown` stops accepting connections and waits for in-flight requests until the deadline.
Compare against `http.ErrServerClosed` with `errors.Is`, as with any sentinel.

### A daemon reacting to several signals

```go
func runDaemon(ctx context.Context) error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGUSR1, syscall.SIGTERM)
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
				toggleDebugLogging()
			case syscall.SIGTERM:
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
```

| Signal | Typical use |
|---|---|
| SIGTERM / SIGINT | Graceful shutdown |
| SIGHUP | Reload configuration without a restart |
| SIGUSR1 / SIGUSR2 | Toggle debug logging, dump state |

The channel passed to `signal.Notify` must be buffered: the runtime does not block when sending
to it, so an unbuffered channel can miss a signal.

---

## encoding/json — Streaming

### Decode from the reader

```go
// Reading the whole body first doubles the memory and adds a copy.
body, _ := io.ReadAll(r.Body)
_ = json.Unmarshal(body, &v)

// Decode straight from the stream, with a size cap.
r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
dec := json.NewDecoder(r.Body)
dec.DisallowUnknownFields() // reject typos in field names instead of ignoring them
if err := dec.Decode(&v); err != nil {
	return fmt.Errorf("decode request: %w", err)
}
```

### Custom marshaler

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
		return fmt.Errorf("duration %q: %w", s, err)
	}
	d.Duration = dur
	return nil
}
```

Use `omitempty` for zero numbers/strings/slices and `omitzero` (Go 1.24) for structs such as
`time.Time`, which `omitempty` never omits.

---

## log/slog — Structured Logging

```go
logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
slog.SetDefault(logger)

slog.Info("user created", "user_id", u.ID, "duration_ms", time.Since(start).Milliseconds())
slog.Error("query failed", "err", err, "user_id", userID)

// Attach fields once, reuse the derived logger.
reqLog := logger.With("request_id", reqID)
reqLog.InfoContext(ctx, "handled")
```

Log key–value pairs, not `fmt.Sprintf` strings: the output stays queryable. Never log secrets;
give secret types a `LogValue` or `String` method that redacts them.

---

## time — Timers and Tickers

### `time.After` in a loop

Since Go 1.23 (for modules whose `go` line is 1.23 or later), timers that nothing references are
garbage-collected even if they were never stopped, and timer channels are unbuffered, so `Reset`
and `Stop` never leave a stale value behind. `time.After` inside a `select` loop therefore no
longer leaks; it never started a goroutine either.

It still allocates a timer per iteration, and it means "time since this iteration started", not
"since the last event". For an idle timeout in a busy loop, a single reusable timer says what you
mean:

```go
idle := time.NewTimer(30 * time.Second)
defer idle.Stop()
for {
	select {
	case msg := <-msgs:
		handle(msg)
		idle.Reset(30 * time.Second) // safe without draining since Go 1.23
	case <-idle.C:
		return errIdle
	case <-ctx.Done():
		return ctx.Err()
	}
}
```

When the deadline belongs to the whole operation, prefer `context.WithTimeout` so it propagates
to every call.

### time.Ticker for periodic work

```go
ticker := time.NewTicker(time.Second)
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

Other traps: compare instants with `t.Equal(u)`, not `==` (the monotonic reading and location
differ); measure elapsed time with `time.Since`, which uses the monotonic clock; and inject a
clock (`now func() time.Time`) into code you need to test deterministically, or test it inside a
`testing/synctest` bubble (see `testing-advanced.md`).

---

## go:embed — Static Files in the Binary

```go
import "embed"

//go:embed config/default.yaml
var defaultConfig []byte

//go:embed templates
var templateFS embed.FS

tmpl, err := template.ParseFS(templateFS, "templates/*.html")
```

Embedding into a `string` or `[]byte` only needs a blank `import _ "embed"`; using `embed.FS`
imports the package normally. Paths are relative to the package directory and cannot contain `..`.

---

## Build Constraints

**store_integration_test.go**
```go
//go:build integration

package pgstore_test
```

The `//go:build` line goes before the package clause, followed by a blank line. Run with
`go test -tags integration ./...`. `GOOS`/`GOARCH` constraints usually come from file suffixes
(`_linux.go`, `_arm64.go`) instead of explicit tags.

---

## Toolchain — Release Binaries

```makefile
VERSION := $(shell git describe --tags --always --dirty)
COMMIT  := $(shell git rev-parse --short HEAD)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/app ./cmd/app

lint:
	golangci-lint run ./...

test:
	go test -race -count=1 ./...

vulncheck:
	govulncheck ./...
```

`-trimpath` is a `go build` flag (it removes local file-system paths from the binary), not a
linker flag. `-s -w` strip the symbol table and DWARF data. The binary already embeds the module
version and VCS revision; read them with `debug.ReadBuildInfo()` or `go version -m bin/app`
before reaching for `-X`.

### Cross-compilation

```bash
GOOS=linux GOARCH=amd64 go build -o bin/app-linux-amd64 ./cmd/app
GOOS=linux GOARCH=arm64 go build -o bin/app-linux-arm64 ./cmd/app
GOOS=windows GOARCH=amd64 go build -o bin/app.exe ./cmd/app
```

Cross-compiling is this simple only with `CGO_ENABLED=0` (the default when cross-compiling);
cgo needs a C cross-toolchain for the target.
