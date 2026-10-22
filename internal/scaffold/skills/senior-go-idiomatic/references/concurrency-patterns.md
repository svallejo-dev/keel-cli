# Concurrency Patterns — Go Idiomatic Reference

## Contents

- When to use which pattern
- 1. Worker Pool · 2. Pipeline · 3. Fan-Out / Fan-In · 4. Semaphore · 5. Errgroup
- 6. Or-Done · 7. Heartbeat · 8. Timeout
- sync.Cond · Backpressure · Rate Limiter · Singleflight · sync.Pool

---

## When to Use Which Pattern

| Situation | Pattern |
|---|---|
| N jobs, bounded parallelism | Worker Pool |
| Sequential transformations | Pipeline |
| Same job → N workers | Fan-Out |
| N results → 1 stream | Fan-In |
| Limit concurrent resource access | Semaphore |
| N goroutines, first error cancels all | Errgroup |
| Range over a channel while honoring cancellation | Or-Done |
| Detect blocked goroutines | Heartbeat |
| Slow consumer must throttle the producer | Backpressure |
| Cap events per unit of time | Rate Limiter |
| Collapse duplicate concurrent calls | Singleflight |
| Reuse short-lived allocations | sync.Pool |

---

## 1. Worker Pool (Bounded Concurrency)

`sync.WaitGroup.Go` (Go 1.25) does the `Add(1)` / `go` / `defer Done()` dance in one call, so the counter cannot drift from the goroutines it tracks.

```go
func WorkerPool(ctx context.Context, jobs <-chan Job, numWorkers int) <-chan Result {
	results := make(chan Result)
	var wg sync.WaitGroup

	for range numWorkers {
		wg.Go(func() {
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
		})
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	return results
}
```

The producer closes `jobs` to tell workers to stop. Workers never close `results`: the goroutine waiting on the WaitGroup does, because only it knows when every sender is done.

---

## 2. Pipeline (Chained Stages)

Each stage takes a receive-only channel and returns a receive-only channel that it owns and closes.

```go
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
// Fan-out: N workers read from the same input channel.
func fanOut(ctx context.Context, in <-chan Work, n int) []<-chan Result {
	channels := make([]<-chan Result, n)
	for i := range n {
		channels[i] = worker(ctx, in)
	}
	return channels
}

// Fan-in: merge N channels into one.
func merge(ctx context.Context, channels ...<-chan Result) <-chan Result {
	out := make(chan Result)
	var wg sync.WaitGroup

	for _, ch := range channels {
		wg.Go(func() {
			for r := range ch {
				select {
				case out <- r:
				case <-ctx.Done():
					return
				}
			}
		})
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}
```

---

## 4. Semaphore (Limit Concurrency)

A buffered channel works as a counting semaphore. Since Go 1.22 each iteration has its own `item`, so the closure captures it directly.

```go
sem := make(chan struct{}, maxConcurrent)

for _, item := range items {
	sem <- struct{}{} // acquire
	go func() {
		defer func() { <-sem }() // release
		process(item)
	}()
}

// Fill the semaphore to wait until every goroutine has released its slot.
for range maxConcurrent {
	sem <- struct{}{}
}
```

For weighted acquisition or context-aware waiting, use `golang.org/x/sync/semaphore`. If you also need error propagation, `errgroup.Group.SetLimit(n)` gives the same bound with less code.

---

## 5. Errgroup (Concurrent Errors with Auto-Cancellation)

```go
import "golang.org/x/sync/errgroup"

func fetchAll(ctx context.Context, ids []int64) ([]*User, error) {
	g, ctx := errgroup.WithContext(ctx)
	users := make([]*User, len(ids))

	for i, id := range ids {
		g.Go(func() error {
			u, err := fetchUser(ctx, id)
			if err != nil {
				return fmt.Errorf("fetch user %d: %w", id, err)
			}
			users[i] = u // each goroutine writes its own index: no race
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}
	return users, nil
}
```

`g.Wait()` returns the first non-nil error, and the derived `ctx` is canceled as soon as any goroutine fails, so the others can stop early. Call `g.SetLimit(n)` before the loop to bound concurrency. To collect every error instead of the first, use a plain `sync.WaitGroup` and aggregate with `errors.Join` under a mutex. Errgroup does not recover panics; see `references/errors-advanced.md`.

---

## 6. Or-Done (Range While Honoring Cancellation)

Wraps a channel so the caller can `range` over it and still stop when the context is canceled, whichever comes first.

```go
func orDone[T any](ctx context.Context, ch <-chan T) <-chan T {
	out := make(chan T)
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
					return
				}
			}
		}
	}()
	return out
}

// Usage
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
				case heartbeat <- struct{}{}: // non-blocking: a missed beat is fine
				default:
				}
			case <-ctx.Done():
				return
			default:
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

A supervisor that stops receiving beats knows the worker is stuck without knowing anything about the work itself.

---

## 8. Timeout Pattern

```go
// Derive from the caller's context, not from Background,
// so an upstream cancellation still wins.
func fetchWithTimeout(ctx context.Context, id int64) (*User, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel() // releases the context's timer as soon as we return

	return db.GetUser(ctx, id)
}

// select-based timeout for a channel operation
select {
case result := <-resultCh:
	return result, nil
case <-time.After(5 * time.Second):
	return nil, ErrTimeout
case <-ctx.Done():
	return nil, ctx.Err()
}
```

`time.After` inside a loop is no longer a leak. Since Go 1.23 (for modules whose `go` line is 1.23 or later), timers that nothing references are garbage-collected even if never stopped, and timer channels are unbuffered, so `Reset` and `Stop` never leave a stale value behind. (`time.After` never started a goroutine.) A reusable timer is still the clearer choice when the loop is hot, because `time.After` allocates a timer per iteration, or when the timeout should restart only on specific events:

```go
timer := time.NewTimer(timeout)
defer timer.Stop()
for {
	select {
	case v := <-in:
		handle(v)
		timer.Reset(timeout) // idle timeout: restart after each message
	case <-timer.C:
		return ErrTimeout
	case <-ctx.Done():
		return ctx.Err()
	}
}
```

For one deadline over the whole loop, `context.WithTimeout` is simpler than any timer.

---

## sync.Cond — When Channels Aren't Enough

Use `sync.Cond` when several goroutines wait for a **condition** on shared state to become true, not just for a value to arrive:

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
		q.cond.Wait() // releases the lock, sleeps, re-acquires on wake
	}
	item := q.items[0]
	q.items = q.items[1:]
	return item
}
```

Wait in a `for` loop, not an `if`: another goroutine may have taken the item between the wake-up and re-acquiring the lock. Use `Broadcast()` when the change matters to every waiter (shutdown); `Signal()` when one waiter can proceed. `sync.Cond` cannot take part in a `select` or honor a context; if waiters need cancellation, a channel-based design is usually easier.

---

## Backpressure — Let the Consumer Slow the Producer

In a pipeline built on **unbuffered** channels, backpressure is automatic: when the consumer is slow, the producer's send blocks, and that slowness propagates upstream through every stage. Nothing piles up in memory, which is the behavior you want by default.

A small buffer **absorbs bursts** (the producer does not stall on small variations in consumer speed) while keeping the brake when the consumer is genuinely saturated. Capacity is a cushion, not a cure for a slow consumer.

```go
func boundedStage(ctx context.Context, in <-chan int) <-chan int {
	out := make(chan int, 4) // small buffer: absorbs bursts, does not hide a slow consumer
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

**Anti-pattern:** `make(chan T, 100000)` "so it doesn't block". A huge buffer turns backpressure into a memory leak: the producer runs free, the consumer can't keep up, and the queue grows until OOM. If a large buffer is what makes it "work", the real problem is a consumer that doesn't scale; fix it there.

---

## Rate Limiter — Cap Events per Unit of Time

`golang.org/x/time/rate` implements a **token bucket**: `r` tokens are added per second, with burst capacity `b`.

```go
import "golang.org/x/time/rate"

// 10 events per second, bursts of up to 20.
limiter := rate.NewLimiter(rate.Limit(10), 20)

// Wait blocks until a token is available or ctx is done.
func handle(ctx context.Context, limiter *rate.Limiter, req Request) error {
	if err := limiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limit: %w", err)
	}
	return process(req)
}

// Allow never blocks: it drops the request when no token is available.
func tryHandle(limiter *rate.Limiter, req Request) error {
	if !limiter.Allow() {
		return fmt.Errorf("rate limited: dropping request %d", req.ID)
	}
	return process(req)
}
```

`Wait` is for throttling (outbound clients that must respect a pace). `Allow` is for load shedding (rejecting inbound work under pressure). `Reserve` tells you up front how long you would have to wait.

---

## Singleflight — Collapse Duplicate Calls

When N goroutines ask for the same thing at once (a cache-miss stampede), `golang.org/x/sync/singleflight` runs the function **once per key** and hands the result to every waiter.

```go
import "golang.org/x/sync/singleflight"

var group singleflight.Group

func GetUser(ctx context.Context, id int64) (*User, error) {
	key := strconv.FormatInt(id, 10)
	// 100 concurrent GetUser(7) calls run fetchUserFromDB once.
	v, err, _ := group.Do(key, func() (any, error) {
		return fetchUserFromDB(ctx, id)
	})
	if err != nil {
		return nil, err
	}
	return v.(*User), nil // the third result reports whether the value was shared
}
```

Typical use: in front of a cache, so one miss doesn't fire thousands of identical queries. Two caveats: if the call fails, **every** waiter gets the same error, and the function runs with the context of whichever caller arrived first, so that caller's cancellation affects all of them. Use `DoChan` with a `select` on your own context when one slow or canceled call must not drag the others down.

---

## sync.Pool — Reuse Short-Lived Allocations

`sync.Pool` recycles temporary objects to reduce GC pressure on hot paths. It is neither a connection pool nor a cache: the GC may empty it at any time.

```go
var bufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

func render(data []byte) string {
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset() // the object may still hold data from its previous user
	defer bufPool.Put(buf)
	buf.Write(data)
	return buf.String()
}
```

Reset on every `Get`; keep no references into the object after `Put`; and reach for it only when a `-benchmem` benchmark shows allocations matter. See `references/memory-pointers.md` for measuring GC pressure.

> `rate`, `singleflight`, `semaphore` and `errgroup` live in the official `golang.org/x/...` sub-repositories, which this skill treats as blessed dependencies (see `references/dependencies-and-stdlib.md`), not arbitrary third-party code.
