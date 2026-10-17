# Concurrency Patterns — Go Idiomatic Reference

## When to Use Which Pattern

| Situation | Pattern |
|---|---|
| N jobs, bounded parallelism | Worker Pool |
| Sequential transformations | Pipeline |
| Same job → N workers | Fan-Out |
| N results → 1 stream | Fan-In |
| Limit concurrent resource access | Semaphore |
| N goroutines, first error cancels all | Errgroup |
| Detect blocked goroutines | Heartbeat |
| Cancel when any one of N signals fires | Or-Done |
| Slow consumer must throttle the producer | Backpressure |
| Cap events per unit of time | Rate Limiter |
| Collapse duplicate concurrent calls | Singleflight |
| Reuse short-lived allocations | sync.Pool |

---

## 1. Worker Pool (Bounded Concurrency)

```go
func WorkerPool(ctx context.Context, jobs <-chan Job, numWorkers int) <-chan Result {
    results := make(chan Result)
    var wg sync.WaitGroup

    for range numWorkers {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for {
                select {
                case job, ok := <-jobs:
                    if !ok {
                        return
                    }
                    select {
                    case results <- process(job):
                    case <-ctx.Done():
                        return
                    }
                case <-ctx.Done():
                    return
                }
            }
        }()
    }

    go func() {
        wg.Wait()
        close(results)
    }()

    return results
}
```

**Key:** Close `jobs` to signal workers to stop. Workers never close `results` — the WaitGroup goroutine does.

---

## 2. Pipeline (Chained Stages)

```go
// Each stage: takes a read-only channel, returns a read-only channel
func generate(ctx context.Context, nums ...int) <-chan int {
    out := make(chan int)
    go func() {
        defer close(out)
        for _, n := range nums {
            select {
            case out <- n:
            case <-ctx.Done():
                return
            }
        }
    }()
    return out
}

func square(ctx context.Context, in <-chan int) <-chan int {
    out := make(chan int)
    go func() {
        defer close(out)
        for n := range in {
            select {
            case out <- n * n:
            case <-ctx.Done():
                return
            }
        }
    }()
    return out
}

// Usage
ctx, cancel := context.WithCancel(context.Background())
defer cancel()
for v := range square(ctx, generate(ctx, 1, 2, 3, 4)) {
    fmt.Println(v)
}
```

---

## 3. Fan-Out / Fan-In

```go
// Fan-Out: distribute work from one channel to N workers
func fanOut(ctx context.Context, in <-chan Work, n int) []<-chan Result {
    channels := make([]<-chan Result, n)
    for i := range n {
        channels[i] = worker(ctx, in) // all read from the same `in`
    }
    return channels
}

// Fan-In: merge N channels into one
func merge(ctx context.Context, channels ...<-chan Result) <-chan Result {
    out := make(chan Result)
    var wg sync.WaitGroup

    forward := func(ch <-chan Result) {
        defer wg.Done()
        for r := range ch {
            select {
            case out <- r:
            case <-ctx.Done():
                return
            }
        }
    }

    wg.Add(len(channels))
    for _, ch := range channels {
        go forward(ch)
    }

    go func() {
        wg.Wait()
        close(out)
    }()

    return out
}
```

---

## 4. Semaphore (Rate-Limit Concurrency)

```go
// Buffered channel as counting semaphore
sem := make(chan struct{}, maxConcurrent)

for _, item := range items {
    sem <- struct{}{} // acquire
    go func(item Item) {
        defer func() { <-sem }() // release
        process(item)
    }(item)
}

// Drain the semaphore to wait for all goroutines
for range maxConcurrent {
    sem <- struct{}{}
}
```

**Production alternative:** `golang.org/x/sync/semaphore` for weighted semaphores.

---

## 5. Errgroup (Concurrent Errors with Auto-Cancellation)

```go
import "golang.org/x/sync/errgroup"

func fetchAll(ctx context.Context, ids []int64) ([]*User, error) {
    g, ctx := errgroup.WithContext(ctx)
    users := make([]*User, len(ids))

    for i, id := range ids {
        i, id := i, id // shadow for closure
        g.Go(func() error {
            u, err := fetchUser(ctx, id)
            if err != nil {
                return fmt.Errorf("fetchUser %d: %w", id, err)
            }
            users[i] = u
            return nil
        })
    }

    if err := g.Wait(); err != nil {
        return nil, err
    }
    return users, nil
}
```

**Analogy to JS:** `errgroup.Wait()` is like `Promise.all` — fails on first error and cancels the rest. For "collect all errors" behavior, use a custom aggregator.

---

## 6. Or-Done (Cancel When Any Signal Fires)

```go
// Wraps a channel so you can range over it respecting context cancellation
func orDone(ctx context.Context, ch <-chan interface{}) <-chan interface{} {
    out := make(chan interface{})
    go func() {
        defer close(out)
        for {
            select {
            case <-ctx.Done():
                return
            case v, ok := <-ch:
                if !ok {
                    return
                }
                select {
                case out <- v:
                case <-ctx.Done():
                }
            }
        }
    }()
    return out
}

// Usage: range safely without caring whether ctx or ch finishes first
for v := range orDone(ctx, inputCh) {
    process(v)
}
```

---

## 7. Heartbeat (Detect Blocked Goroutines)

```go
func doWork(ctx context.Context, pulse time.Duration) (<-chan Result, <-chan struct{}) {
    results := make(chan Result)
    heartbeat := make(chan struct{}, 1)

    go func() {
        defer close(results)
        ticker := time.NewTicker(pulse)
        defer ticker.Stop()

        for {
            select {
            case <-ticker.C:
                select {
                case heartbeat <- struct{}{}: // non-blocking send
                default:
                }
            case <-ctx.Done():
                return
            default:
                // do actual work
                result := compute()
                select {
                case results <- result:
                case <-ctx.Done():
                    return
                }
            }
        }
    }()

    return results, heartbeat
}
```

---

## 8. Timeout Pattern

```go
// Per-operation timeout using context
func fetchWithTimeout(id int64) (*User, error) {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel() // always call cancel to release resources

    return db.GetUser(ctx, id)
}

// select-based timeout (for channel operations)
select {
case result := <-resultCh:
    return result, nil
case <-time.After(5 * time.Second):
    return nil, ErrTimeout
case <-ctx.Done():
    return nil, ctx.Err()
}
```

**Warning:** `time.After` in a loop creates a goroutine-per-call that leaks until the timer fires. In loops, use `time.NewTimer` and `Reset`:
```go
timer := time.NewTimer(timeout)
defer timer.Stop()
// ... reset with timer.Reset(timeout) between iterations
```

---

## sync.Cond — When Channels Aren't Enough

Use `sync.Cond` when multiple goroutines need to wait for a **condition** to become true (not just for data arrival):

```go
type Queue struct {
    mu    sync.Mutex
    cond  *sync.Cond
    items []Item
}

func NewQueue() *Queue {
    q := &Queue{}
    q.cond = sync.NewCond(&q.mu)
    return q
}

func (q *Queue) Push(item Item) {
    q.mu.Lock()
    q.items = append(q.items, item)
    q.cond.Signal() // wake one waiter
    q.mu.Unlock()
}

func (q *Queue) Pop() Item {
    q.mu.Lock()
    defer q.mu.Unlock()
    for len(q.items) == 0 {
        q.cond.Wait() // releases lock, sleeps, re-acquires on wake
    }
    item := q.items[0]
    q.items = q.items[1:]
    return item
}
```

**Use `Broadcast()`** when the state change matters to all waiters (e.g., shutdown signal). **Use `Signal()`** when only one waiter needs to proceed.

---

## Backpressure — Dejar que el Consumidor Frene al Productor

En un pipeline con channels **unbuffered**, la backpressure es automática: si el consumidor es lento, el `send` del productor se bloquea, y esa lentitud se propaga hacia atrás por todo el pipeline. No hay acumulación ilimitada en memoria — ese es el comportamiento que quieres por defecto.

Un buffer pequeño **desacopla ráfagas** (el productor no se bloquea por micro-variaciones del consumidor) sin perder el freno cuando el consumidor está saturado de verdad. La capacidad es un colchón, no una cura para un consumidor lento.

```go
func boundedStage(ctx context.Context, in <-chan int) <-chan int {
	out := make(chan int, 4) // buffer pequeño: absorbe ráfagas, NO esconde un consumidor lento
	go func() {
		defer close(out)
		for n := range in {
			select {
			case out <- n * 2:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}
```

**Anti-patrón:** poner `make(chan T, 100000)` para que "no se bloquee". Un buffer enorme convierte la backpressure en una fuga de memoria: el productor corre libre, el consumidor no da abasto, y la cola crece hasta el OOM. Si necesitas un buffer grande "para que funcione", el problema real es que el consumidor no escala — arréglalo ahí, no con memoria.

---

## Rate Limiter — Acotar Eventos por Unidad de Tiempo

`golang.org/x/time/rate` implementa un **token bucket**: se reponen `r` tokens por segundo, con capacidad de ráfaga `b`.

```go
import "golang.org/x/time/rate"

// 10 eventos/segundo, ráfagas de hasta 20.
limiter := rate.NewLimiter(rate.Limit(10), 20)

// Wait: bloquea hasta tener token o hasta que el ctx se cancele.
func handle(ctx context.Context, limiter *rate.Limiter, req Request) error {
	if err := limiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limit: %w", err)
	}
	return process(req)
}

// Allow: no bloqueante — descarta si no hay token disponible.
func tryHandle(limiter *rate.Limiter, req Request) error {
	if !limiter.Allow() {
		return fmt.Errorf("rate limited: dropping request %d", req.ID)
	}
	return process(req)
}
```

`Wait` para throttling (clientes salientes que respetan un ritmo). `Allow` para load shedding (descartar carga entrante bajo presión). `Reserve` cuando necesitas saber de antemano cuánto esperarías.

---

## Singleflight — Colapsar Llamadas Duplicadas

Cuando N goroutines piden lo mismo a la vez (un *cache miss stampede*), `golang.org/x/sync/singleflight` ejecuta la función **una sola vez por key** y reparte el resultado a todos los que esperan.

```go
import "golang.org/x/sync/singleflight"

var group singleflight.Group

func GetUser(ctx context.Context, id int64) (*User, error) {
	key := strconv.FormatInt(id, 10)
	// Si 100 goroutines llaman GetUser(7) a la vez, fetchUserFromDB corre UNA sola vez.
	v, err, _ := group.Do(key, func() (any, error) {
		return fetchUserFromDB(ctx, id)
	})
	if err != nil {
		return nil, err
	}
	return v.(*User), nil // el 3er valor (shared bool) indica si el resultado se compartió
}
```

Uso típico: delante de un cache, para que un miss no dispare miles de queries idénticas. Cuidado: si la función falla, **todos** reciben el mismo error; usa `DoChan` + timeout si una llamada lenta no debe arrastrar a las demás.

---

## sync.Pool — Reutilizar Allocaciones de Vida Corta

`sync.Pool` recicla objetos temporales para reducir la presión de GC en hot paths. No es un pool de conexiones ni un cache: el GC puede vaciarlo en cualquier momento.

```go
var bufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

func render(data []byte) string {
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset() // SIEMPRE resetea: el objeto viene "sucio" de un uso anterior
	defer bufPool.Put(buf)
	buf.Write(data)
	return buf.String()
}
```

Reglas: resetea siempre al obtener; no conserves punteros a lo que metiste en el objeto después del `Put`; úsalo solo si un benchmark con `-benchmem` demuestra que las allocaciones importan. Ver `references/memory-pointers.md` para medir presión de GC.

> `rate`, `singleflight` y `errgroup` viven en los sub-repos oficiales `golang.org/x/...` — dependencias "bendecidas" (ver `references/dependencies-and-stdlib.md`), no librerías de terceros arbitrarias.
