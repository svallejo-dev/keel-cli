# Channels & Concurrency Fundamentals — Go Idiomatic Reference

## Contents

- Concurrency vs parallelism
- Channel axioms (nil / open / closed) and ownership
- Unbuffered vs buffered; when a buffer is right
- select
- Memory model / happens-before
- Data races
- "Share memory by communicating" and its limits
- Checklist

---

## Concurrency vs Parallelism

They are different axes. **Concurrency** is structure: decomposing a program into independent tasks that make progress interleaved. **Parallelism** is execution: running several of those tasks literally at the same time on several cores. A concurrent program runs fine on one core; parallelism only appears if the hardware is there.

A goroutine is a lightweight thread managed by the runtime, not the OS; it starts with a small stack (a few KB) that grows on demand. The scheduler is **G-P-M**: `G` goroutines are multiplexed onto `M` OS threads through `P` logical processors, each `P` with its own local run queue and work-stealing between them.

`GOMAXPROCS` sets how many `P`s are active, i.e. the ceiling on real parallelism. It defaults to the number of visible cores (or, since Go 1.25 on Linux, the cgroup CPU limit when that is lower). Read it without changing it with `runtime.GOMAXPROCS(0)`.

Choose concurrency from the structure of the problem (how many independent tasks exist), not from how many cores you have. The scheduler does the distribution.

---

## Channel Axioms

A channel has three states (nil, open, closed), and each operation behaves differently in each. Knowing this table removes most concurrency deadlocks and panics.

| Channel state | send (`ch <- v`) | receive (`<-ch`) | `close(ch)` |
|---|---|---|---|
| `nil` | blocks **forever** | blocks **forever** | **panic** |
| open | blocks until there is room or a receiver | blocks until there is a value | ok |
| closed | **panic** | zero value + `ok=false`, immediately | **panic** (double close) |

Two consequences used daily:

- Blocking forever on a `nil` channel is **useful** inside a `select`: setting a channel variable to `nil` disables that case dynamically (see below).
- A closed channel can be received from indefinitely: it first drains whatever is buffered, then yields zero values with `ok=false`. That is why `range ch` ends cleanly on close.

```go
ch := make(chan int, 1)
ch <- 42
close(ch)
v, ok := <-ch // v=42, ok=true  — drains the buffer first
v, ok = <-ch  // v=0,  ok=false — closed and empty
```

### Ownership: who closes

The producer that owns the channel closes it; the receiver does not. `close` means "no more values", and only the sender knows that. Closing from the receiver side invites a send on a closed channel (panic) in the producer.

```go
func producer(n int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out) // the owner closes when it is done producing
		for i := range n {
			out <- i
		}
	}()
	return out // returning <-chan int: the receiver cannot close it
}

func consume() {
	for v := range producer(3) { // range ends only when the owner closes
		fmt.Println(v)
	}
}
```

Returning `<-chan T` (receive-only) makes the compiler reject a `close` by the receiver. It is the idiomatic way to encode ownership in the signature.

---

## Unbuffered vs Buffered

An **unbuffered** channel (`make(chan T)`, capacity 0) is a **synchronous rendezvous**: send and receive meet at the same instant and the handoff is guaranteed; the sender does not proceed until someone receives. A **buffered** channel (`make(chan T, N)`) is an **asynchronous queue** of capacity `N`: a send blocks only when the buffer is full; a receive blocks only when it is empty.

| | Unbuffered (`cap==0`) | Buffered (`cap==N`) |
|---|---|---|
| Semantics | synchronous rendezvous | asynchronous FIFO queue |
| Send blocks | until a receiver takes it | only when the buffer is full |
| Receive blocks | until a sender sends | only when the buffer is empty |
| Guarantee after send | the receiver **has** it | only queued, maybe not read yet |
| Typical use | signaling, handoff, synchronization | decoupling producer and consumer |

`len()` and `cap()` work on channels: `cap(ch)` is the buffer size (constant); `len(ch)` is how many elements are queued right now. Both are 0 on an unbuffered channel.

```go
ch := make(chan int, 2)
ch <- 1
ch <- 2                       // does not block: there is room
fmt.Println(len(ch), cap(ch)) // 2 2
<-ch
fmt.Println(len(ch), cap(ch)) // 1 2
```

`len(ch)` is an instantaneous snapshot that can change between reading it and acting on it. Use it for metrics, not for control flow.

---

## When a Buffer Is Right — and the Anti-Pattern

A buffer is justified by a concrete reason:

- **`N` known in advance.** One result per worker with a fixed `N`: sends never block and the receiver collects at its own pace.
- **Decoupling producer from consumer** when their throughput is uneven and short bursts should not stall the producer.
- **Avoiding a goroutine leak when the receiver may leave.** If the receiver gives up (timeout, cancellation) before reading, a send on an unbuffered channel blocks the sender forever. A buffer large enough to hold every send lets the goroutine finish.

```go
func fanOut(ids []int) <-chan int {
	results := make(chan int, len(ids)) // exact N: sends never block
	for _, id := range ids {
		go func() {
			results <- id * id // no leak even if nobody reads yet
		}()
	}
	return results
}
```

### Anti-pattern: a buffer to "fix" a bug

```go
// BAD: grow the buffer until the deadlock or race "goes away"
ch := make(chan int, 1000) // magic number hiding the problem

// GOOD: design termination and synchronization explicitly
ch := make(chan int) // capacity comes from a concrete reason, not convenience
```

A buffer does **not** fix a deadlock or a race; it only shifts timing enough for the symptom to show up less often. If you pick the buffer size by trial and error until "it stops failing", the bug is still there, latent and load-dependent. Derive channel capacity from a property of the problem (number of workers, batch size), never tune it to hide a synchronization fault.

---

## select

`select` waits on several channel operations at once and proceeds with the first one ready.

- If **several** cases are ready, it picks one **at random** (avoids starvation; do not assume an order).
- A `default` case makes the `select` **non-blocking**: if no channel is ready, `default` runs immediately.
- Setting a channel variable to `nil` **disables** that case: since operations on `nil` block forever, the case is never selected again.

```go
// Non-blocking send: try without waiting.
func trySend(ch chan int, v int) bool {
	select {
	case ch <- v:
		return true
	default:
		return false // channel full: do not block
	}
}
```

```go
// nil-channel trick: disable a case dynamically.
func drain(in <-chan int, tick <-chan time.Time) {
	for in != nil || tick != nil {
		select {
		case v, ok := <-in:
			if !ok {
				in = nil // input exhausted → disable this case
				continue
			}
			fmt.Println(v)
		case <-tick:
			tick = nil // disable the ticker case
		}
	}
}
```

The `for`-`select` loop with `ctx.Done()` is the canonical shape of a goroutine that can be stopped: there is always a guaranteed way out.

```go
func worker(ctx context.Context, in <-chan int, out chan<- int) {
	for {
		select {
		case <-ctx.Done():
			return
		case v, ok := <-in:
			if !ok {
				return
			}
			select { // the send honors cancellation too
			case out <- v * 2:
			case <-ctx.Done():
				return
			}
		}
	}
}
```

---

## Memory Model / Happens-Before

A channel does not only **communicate** values; it **synchronizes** memory. The Go memory model guarantees two key happens-before relations:

- A send on a channel **happens before** the corresponding receive completes.
- A `close` **happens before** a receive that observes the close (the one that gets `ok=false`).

So everything a goroutine wrote **before** sending (or closing) is visible to the goroutine that receives. Without that guarantee, a write on one core might not be visible on another.

```go
func happensBefore() {
	var data int
	done := make(chan struct{})
	go func() {
		data = 42   // (1) write
		close(done) // (2) close happens before the receive that observes it
	}()
	<-done            // (3) observes the close
	fmt.Println(data) // 42 guaranteed: (1) is visible here
}
```

For a plain **counter**, a channel is too heavy. `sync/atomic` gives the same visibility guarantee with an atomic read-modify-write and no blocking:

```go
var n atomic.Int64 // typed atomics, Go 1.19+
n.Add(1)
n.Load()
```

Channels to transfer data and coordinate; `atomic` for simple counters and flags; a mutex for invariants spanning several variables.

---

## Data Races

A **data race** happens when two goroutines access the same variable concurrently, at least one access is a write, and no happens-before relation orders them. The result is undefined behavior: corrupted values, not merely "last write wins".

The race detector instruments memory accesses at run time and reports each race with both stacks:

```bash
go run -race .
go test -race ./...
```

```go
// BAD: counter++ is a non-atomic read-modify-write → data race
func racyCounter() int {
	var counter int
	var wg sync.WaitGroup
	for range 1000 {
		wg.Go(func() {
			counter++ // -race flags this; the result is nondeterministic
		})
	}
	wg.Wait()
	return counter
}
```

```go
// GOOD (mutex): protect the shared state
func mutexCounter() int {
	var (
		mu      sync.Mutex
		counter int
		wg      sync.WaitGroup
	)
	for range 1000 {
		wg.Go(func() {
			mu.Lock()
			counter++
			mu.Unlock()
		})
	}
	wg.Wait()
	return counter // always 1000
}
```

```go
// GOOD (channel): one owner touches the state; everyone else sends it work
func channelCounter() int {
	inc := make(chan struct{})
	result := make(chan int)
	go func() {
		counter := 0
		for range inc {
			counter++ // no other goroutine can see counter
		}
		result <- counter
	}()

	var wg sync.WaitGroup
	for range 1000 {
		wg.Go(func() { inc <- struct{}{} })
	}
	wg.Wait()
	close(inc)
	return <-result // always 1000
}
```

Run `-race` in CI over the concurrent tests. It cannot see races on paths the tests don't exercise, but it has no false positives: whatever it reports is real.

---

## "Don't communicate by sharing memory; share memory by communicating"

The Go proverb: instead of several goroutines reading and writing the same variable under locks, pass ownership of the data through a channel. At any moment only one goroutine owns it, and handing it over is a send. That removes the whole class of "forgot to take the lock" bugs.

It has a **limit**, though, and treating it as dogma produces worse code:

| Use **channels** when… | Use **`sync.Mutex`** when… |
|---|---|
| you transfer ownership of data | you protect long-lived shared state |
| you coordinate goroutine lifecycles | the state is a struct with invariants across fields |
| you build a pipeline or fan-out/in | access is a simple get/set under contention |
| the natural unit is "a message" | moving the data through a channel would cost more than a lock |

A concurrent cache, a connection counter, a metrics registry: all long-lived shared state that a `sync.Mutex` (or `sync.RWMutex`, or `atomic`) protects more simply and faster than an owner goroutine orchestrated with channels. Forcing channels there adds a goroutine, latency and complexity for no gain. Pick the tool from the shape of the problem, not from the slogan.

---

## Checklist

- [ ] Channel capacity comes from a concrete reason (N workers, batch size), not trial and error to hide a bug.
- [ ] The producer/owner closes the channel, the receiver does not; encoded with `<-chan T` in the signature.
- [ ] No send on a possibly closed channel, no double `close`, no `close` of a `nil` channel.
- [ ] Every goroutine has a guaranteed exit: `for`-`select` with `case <-ctx.Done()`.
- [ ] Unbuffered for signaling/handoff; buffered only to decouple or to keep a sender from leaking.
- [ ] `len(ch)`/`cap(ch)` only for metrics, not for synchronization.
- [ ] Simple counters and flags use `sync/atomic`; state with invariants uses `sync.Mutex`; ownership transfer uses channels.
- [ ] `go test -race` runs in CI over all concurrent code and passes.
