# Memory & Pointers — Go Idiomatic Reference

## Contents

1. Stack vs Heap
2. Escape Analysis
3. Pointers
4. Value vs pointer receiver
5. Garbage Collector
6. Cache locality and struct layout
7. When not to use pointers
8. Profiling

---

## The Mental Model

A Go program's memory lives in two places: the **stack** (one per goroutine, cheap) and the **heap** (shared, managed by the garbage collector, expensive). The question that matters is not "should I use a pointer?" but "where does this value end up living, and what does it cost the GC?". The programmer does not choose stack or heap: the compiler decides through *escape analysis*. You only control the conditions — returning addresses, storing values in interfaces, capturing in closures — and the compiler derives the consequences.

This file covers **where memory lives and why it matters for performance**. For *what gets copied* when passing or assigning (copy semantics, slice/map aliasing), see `references/value-vs-reference.md`.

---

## 1. Stack vs Heap

The **stack** grows and shrinks with the function *frame*: entering pushes a frame, returning releases it. Allocating is bumping a pointer; freeing is bumping it back. No bookkeeping, and the data is hot in cache. The **heap** holds values whose lifetime outlives the frame that created them: nobody frees them on return; the garbage collector reclaims them and must track what is still alive. Every heap value is future work for the GC.

| | Stack | Heap |
|---|---|---|
| Management | Automatic with the frame (push/pop) | Garbage collector (mark-sweep) |
| Allocation cost | ~0 (move a pointer) | Allocator + GC pressure |
| Cache locality | High (contiguous, hot) | Varies — often scattered |
| Lifetime | Ends with the function | Until the GC reclaims it |
| Who decides | The compiler (escape analysis) | The compiler (escape analysis) |

Working rule: **what does not escape is free.** The goal in a hot path is not to avoid pointers on principle, but to keep values from escaping to the heap without need.

---

## 2. Escape Analysis

The compiler decides at compile time whether a value can live on the stack or must "escape" to the heap. Inspect it with `-gcflags="-m"`:

```bash
go build -gcflags="-m" ./...
```

A value escapes when its lifetime cannot be bounded by the frame. The canonical triggers:

```go
// stays: x lives on the stack and dies with the frame.
func stays() int {
	x := 42
	return x // the value is copied to the caller; x does not escape
}

// escapesPtr: returning &local moves x to the heap.
func escapesPtr() *int {
	x := 42
	return &x // the pointer outlives the function → x on the heap
}

type Stringer interface{ String() string }
type box struct{ n int }

func (b box) String() string { return "box" }

// escapesIface: storing a value in an interface moves it to the heap.
func escapesIface() Stringer {
	b := box{n: 1}
	return b // the interface hides the concrete type → box on the heap
}

// escapesClosure: the closure outlives the function and captures x.
func escapesClosure() func() int {
	x := 42
	return func() int { return x } // x lives as long as the closure
}

// unknownSize: make with a variable length → backing array on the heap.
func unknownSize(n int) []byte {
	return make([]byte, n) // size unknown at compile time
}

// knownSize: small constant size → can stay on the stack.
func knownSize() byte {
	buf := make([]byte, 64)
	return buf[0]
}
```

Output of `go build -gcflags="-m"` on that file (trimmed to the escape lines; exact wording and positions vary between releases):

```
demo.go:11:2:  moved to heap: x                       // escapesPtr
demo.go:23:9:  b escapes to heap                      // escapesIface
demo.go:29:9:  func literal escapes to heap           // escapesClosure
demo.go:34:13: make([]byte, n) escapes to heap        // unknownSize (dynamic n)
demo.go:39:13: make([]byte, 64) does not escape       // knownSize (constant)
```

Compiler vocabulary: `moved to heap: x` (a local variable relocated to the heap), `escapes to heap` (the value of an expression is allocated on the heap), `does not escape` (stays on the stack — what you want to see in a hot path).

More triggers: **sending a pointer on a channel** (`ch <- &x` moves `x` to the heap, because another goroutine may read it later) and storing a pointer in a field of a struct that escapes. General rule: if anything that lives longer than the frame can reach the value, it escapes.

---

## 3. Pointers

`&v` takes the address of `v`; `*p` dereferences the pointer `p`. The zero value of any pointer is `nil`.

```go
x := 42
p := &x  // p is *int
*p = 100 // mutates x through the pointer
_ = x    // x == 100
```

### `new(T)` vs `&T{}`

```go
a := new(int)       // *int → an int with its zero value (0)
c := &Counter{n: 1} // *Counter → build and take the address in one step
```

Both allocate and return `*T`. Idiomatic use: `&T{...}` for structs (it lets you initialize fields); `new(T)` for basic types or when you only want the zero value. `&int{}` does **not** compile — composite literals exist only for structs, arrays, slices and maps.

### nil dereference → panic

```go
var p *Counter // nil
p.Inc()        // panic: runtime error: invalid memory address or nil pointer dereference
```

A pointer-receiver method called on a `nil` pointer panics as soon as it touches a field; validate external data before dereferencing it.

### Pointers to interfaces and to slices/maps

- **`*interface{}` — almost never.** An interface is already a (type, pointer) pair. `*io.Reader` is nearly always a design mistake; pass the interface by value.
- **`*[]T` / `*map[K]V` — rarely.** Slices and maps are already cheap-to-copy headers (see `references/value-vs-reference.md`). You need `*[]T` only when the function must **reassign** the caller's slice (e.g. an `append` that changes the header). For maps, never — the copied header already shares the same table.

---

## 4. Value vs pointer receiver

This is the memory decision you will make most often. It is not cosmetic: it decides whether methods can mutate, whether the type is safe to copy, and whether the value escapes.

### Use a pointer receiver `(t *T)` when:

- **The method mutates the receiver.** A value receiver works on a copy; the mutation is lost on return.
- **The struct is large.** Copying the receiver on every call costs; a pointer copies 8 bytes.
- **It contains a non-copyable field** — `sync.Mutex`, `sync.WaitGroup`, `atomic.*`, or a struct that holds one. Copying a `Mutex` breaks mutual exclusion.
- **Consistency.** If *any* method of the type needs a pointer receiver, use pointer receivers on all of them so the method set is uniform.

### Use a value receiver `(t T)` when:

- **The type is small and immutable** — `time.Time`, `Point{X, Y float64}`, IDs.
- **It is a basic or numeric type** — `type Celsius float64`.
- **You want the safety of working on a copy** — the method cannot alter the original by accident.

### Examples

```go
// Pointer: mutates the receiver.
type Counter struct{ n int }

func (c *Counter) Inc()     { c.n++ } // with a value receiver the ++ would be lost
func (c *Counter) Get() int { return c.n }

// Pointer: contains a sync.Mutex, which must not be copied.
type Registry struct {
	mu    sync.Mutex
	items map[string]int
}

func (r *Registry) Add(k string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[k]++
}

// Value: small and immutable; each method returns a new Point.
type Point struct{ X, Y float64 }

func (p Point) Add(q Point) Point { return Point{p.X + q.X, p.Y + q.Y} }
```

`go vet` (copylocks) catches an accidental `Mutex` copy:

```
badCopy passes lock by value: x.Registry contains sync.Mutex
```

### Don't mix receiver kinds on one type

A type with both value and pointer methods has a confusing method set: only `*T` satisfies interfaces that include the pointer methods, and copying a value whose other methods mutate leads to silent bugs. Pick one kind per type.

### Decision table

| Condition | Receiver |
|---|---|
| The method mutates the receiver | `*T` |
| Contains `sync.Mutex` / `WaitGroup` / `atomic` | `*T` |
| Large struct (several words) | `*T` |
| Another method of the type already uses `*T` | `*T` (consistency) |
| Small, immutable or basic type | `T` |
| Unsure | `*T` (safe default for stateful structs) |

---

## 5. Garbage Collector

Go uses a **concurrent tri-color mark-sweep** collector: it marks reachable objects (white → grey → black) concurrently with your program and sweeps the white ones. Stop-the-world pauses are typically sub-millisecond, but marking competes with your code for CPU. Two knobs:

- **`GOGC`** (default `100`): triggers a GC cycle when the heap has grown 100% over the live heap left by the previous cycle. Raising it (`GOGC=200`) makes GC less frequent at the cost of more resident memory.
- **`GOMEMLIMIT`** (Go 1.19+): a *soft* limit on total memory. The GC becomes more aggressive as usage approaches it. It is the right tool in containers with a memory limit — it avoids the OOM kill without blindly tuning `GOGC`.

The causal chain that matters in production:

```
more heap allocations → more objects to mark → more frequent GC cycles
→ more CPU taken from the hot path → higher p99 latency
```

**Goal:** minimize allocations in the hot path. Fewer escapes = less GC pressure = flatter latency tails. A fast allocator does not fix a hot path that produces garbage; the real cost is the marking afterwards.

---

## 6. Cache locality and struct layout

How you lay data out in memory matters as much as the algorithm when you iterate over many elements.

```go
type Particle struct {
	x, y, z float64
	vx, vy  float64
}

// []Particle: structs stored contiguously in one backing array.
func sumContiguous(ps []Particle) float64 {
	var total float64
	for i := range ps {
		total += ps[i].x // the prefetcher guesses right: the next struct is already in cache
	}
	return total
}

// []*Particle: contiguous pointers, structs scattered across the heap.
func sumScattered(ps []*Particle) float64 {
	var total float64
	for _, p := range ps {
		total += p.x // each dereference jumps to an unpredictable address → cache miss
	}
	return total
}
```

`[]T` stores the structs back to back: walking them is a linear sweep the hardware prefetches. `[]*T` stores only pointers; the structs live wherever the allocator left them. Each iteration is a jump the prefetcher cannot anticipate, and a cache miss costs hundreds of cycles.

**When it matters:** hot loops over large collections (thousands of elements and up) walked densely. There `[]T` can be several times faster than `[]*T` with the same big-O. **When it doesn't:** small collections, sporadic access, or large elements that need shared identity (in-place mutation seen by several owners). Don't restructure for cache locality without a benchmark that justifies it.

### Struct field order and padding

Each field is aligned to its own size, so the compiler inserts padding between fields of different widths. Field order changes the struct's size:

```go
// 24 bytes on 64-bit: 1 + 7 padding + 8 + 1 + 7 padding.
type Loose struct {
	a bool
	b int64
	c bool
}

// 16 bytes: 8 + 1 + 1 + 6 padding.
type Tight struct {
	b int64
	a bool
	c bool
}
```

Reorder fields only for structs that are hot and numerous — millions of instances in a slice, a cache, a wire buffer — and only when a benchmark or the `fieldalignment` analyzer (`golang.org/x/tools/go/analysis/passes/fieldalignment`) shows the saving. Everywhere else, order fields for readability (group related fields, put the mutex next to what it guards); eight bytes per instance is noise on a struct you create a few hundred times.

---

## 7. When not to use pointers

A pointer is not free: it adds an indirection (a possible cache miss) and, if the value escapes, GC pressure. For small values, **copying is usually cheaper than pointing**.

```go
type RGBA struct{ R, G, B, A uint8 } // 4 bytes

// Value receiver + copy: no indirection, no allocation, does not escape.
func invert(c RGBA) RGBA {
	return RGBA{255 - c.R, 255 - c.G, 255 - c.B, c.A}
}
```

Passing `RGBA` by value copies 4 bytes in a register. Passing `*RGBA` copies 8 bytes (the pointer) **and** risks moving the value to the heap if it escapes, adding GC work. The copy wins.

Heuristic: for structs up to ~2–3 words (16–24 bytes) without non-copyable fields and without a need to mutate the caller's value, prefer value semantics. Above that, or when there is mutation or shared state, use a pointer. Check with `-gcflags="-m"` and `-benchmem` instead of guessing.

---

## 8. Profiling

Don't optimize by intuition; measure. Tools, from quickest to deepest:

### `-gcflags="-m"` — static diagnosis

Before measuring, check what escapes (section 2): cheap and nothing runs.

### `go test -bench` with `-benchmem`

```bash
go test -bench=. -benchmem ./...
```

```go
func BenchmarkInvert(b *testing.B) {
	b.ReportAllocs() // report allocations even without -benchmem
	c := RGBA{10, 20, 30, 255}
	for b.Loop() {
		c = invert(c)
	}
}
```

Since Go 1.24, `for b.Loop()` is the benchmark loop to use: the timer starts on the first call, so setup before the loop is not measured and `b.ResetTimer()` is unnecessary; arguments and results of calls in the loop body are kept alive, so the compiler cannot optimize the work away (no `_ = c` sink needed).

Illustrative output:

```
BenchmarkInvert-8   1000000000   0.43 ns/op   0 B/op   0 allocs/op
```

How to read it: `ns/op` is latency per operation; `B/op` is bytes allocated on the heap per operation (`0` confirms `invert` does not escape); `allocs/op` is the number of heap allocations — **the metric to chase in a hot path, ideally down to 0.** One allocation here is a regression even if `ns/op` doesn't move: every allocation is debt the GC collects under real load.

### `-memprofile` + `go tool pprof`

```bash
go test -bench=BenchmarkInvert -benchmem -memprofile mem.out ./...
go tool pprof mem.out
#   top      → functions that allocate the most
#   list Fn  → exact lines that allocate inside Fn
```

`pprof` tells you **where** memory is allocated; `-benchmem` tells you **how much**. The usual loop: `-m` (what escapes) → `-benchmem` (allocs/op) → `pprof list` (the culprit line) → refactor (reuse buffers, avoid interfaces in the hot path, `sync.Pool` if it applies) → benchmark again.

---

## Checklist

- [ ] Hot-path values don't escape to the heap — checked with `go build -gcflags="-m"`; unexpected `moved to heap` / `escapes to heap` investigated
- [ ] Pointer receiver when the method mutates, the struct is large, or it contains a `sync.Mutex`; value receiver for small, immutable or basic types
- [ ] One receiver kind per type — value and pointer not mixed
- [ ] `go vet` clean of `passes lock by value` (no `Mutex` copied)
- [ ] No `*interface{}`; `*[]T` only when the function reassigns the caller's slice
- [ ] External data validated before dereferencing (avoid nil-deref panics)
- [ ] `[]T` instead of `[]*T` for hot loops over large, dense collections
- [ ] Field reordering for padding only on hot, numerous structs, backed by a benchmark or `fieldalignment`
- [ ] `GOMEMLIMIT` set in containers with a memory limit
- [ ] Benchmarks use `for b.Loop()` and `b.ReportAllocs()` / `-benchmem`; target `0 allocs/op` in the hot path
- [ ] Memory regressions located with `-memprofile` + `go tool pprof list`
