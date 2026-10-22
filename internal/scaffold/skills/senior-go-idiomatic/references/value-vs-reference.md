# Value vs Reference Semantics — Go Idiomatic Reference

## Contents

- Go always passes by value
- Value types vs "reference" types
- Slice header and `append` aliasing
- Maps (nil map, copying)
- Arrays vs slices
- Structs: shallow copy, copylocks, `range` copies
- Receiver semantics
- Defensive copies
- Checklist

---

## Go Always Passes by Value

There is no pass-by-reference in Go. **Every assignment and every argument copies the value.** There are no exceptions and no syntax to ask for "the original", like C++'s `T&`.

```go
func mutate(n int) { n = 99 } // works on a copy

x := 1
mutate(x)
fmt.Println(x) // 1 — the original never changed
```

The contrast with other languages is the source of most misunderstandings:

| Language | Semantics |
|---|---|
| C++ | pass-by-value by default; `T&` gives real pass-by-reference |
| Java | always pass-by-value; object references are **also** passed by value (copies of the handle) |
| Go | always pass-by-value; to mutate the original you pass an explicit `*T`, and that pointer is copied too |

When you "pass a pointer", Go copies the pointer (8 bytes on 64-bit), not what it points to. Mutation works because the copy points at the same data. It is still pass-by-value; what you share is the pointee.

---

## Value Types vs "Reference" Types

"Reference type" is an informal category: the value is copied like everything else, but it contains an internal pointer to shared data. Copying the header is cheap; the data it points to is not duplicated.

| Category | Types | What is copied when passed |
|---|---|---|
| Value | `bool`, numerics, `string`, arrays, `struct` | The whole content (the entire struct/array) |
| "Reference" (informal) | slice, map, `chan`, `func`, pointer | Only the header/handle; the pointed-to data is shared |

`string` is a special case: it is a `{ptr, len}` header but **immutable**, so sharing the backing bytes is safe and there is no observable aliasing. The other reference-like types do expose aliasing: mutating through a copy shows up in the original.

---

## Slice Header — {ptr, len, cap}

A slice is a three-word header. On 64-bit it takes **24 bytes**: pointer to the backing array, `len` and `cap`.

```go
type sliceHeader struct {
	ptr *T  // points into the backing array
	len int // accessible elements
	cap int // capacity up to the end of the backing array
}
```

Passing a slice copies those 24 bytes, but the copied `ptr` points to the same backing array. That is why assigning `s[i]` inside a function **does** affect the caller.

```go
// BAD: surprise mutation — the caller doesn't expect its data overwritten
func normalize(s []float64) {
	m := maxOf(s)
	for i := range s {
		s[i] /= m // writes into the caller's backing array
	}
}

// GOOD: when you must not mutate the caller's data, copy first and return
func normalized(s []float64) []float64 {
	out := slices.Clone(s) // own backing array
	m := maxOf(out)
	for i := range out {
		out[i] /= m
	}
	return out
}
```

Mutating the elements of a received slice is a side effect on the caller. Do it only when the function's contract says so (`slices.Sort` does, by name).

---

## append — The Detail That Breaks Code

`append` shares the backing array when there is enough `cap`, but **reallocates** (copies into a new array) when `cap` is exceeded. Which one happens depends on the runtime `cap`, so the effect on other holders of the slice is not visible from the call site.

```go
// enough cap → writes in place into the shared backing array
a := make([]int, 3, 10)
b := append(a, 99) // b shares a's backing array; b[0:3] aliases a

// cap exceeded → reallocates: d points to a NEW array, c is untouched
c := make([]int, 3, 3)
d := append(c, 99)
```

Because you can't tell which branch ran, assign the result back to the same slice:

```go
s = append(s, x) // t := append(s, x) followed by more use of s is the classic aliasing bug
```

### Aliasing between sub-slices

A sub-slice inherits the parent's `cap` up to the end of the backing array. An `append` on the sub-slice can overwrite elements of the parent.

```go
// BAD: sub inherits cap=5 → append overwrites base[2]
base := []int{1, 2, 3, 4, 5}
sub := base[0:2]      // len=2, cap=5
sub = append(sub, 99) // writes into base[2]
// base == {1, 2, 99, 4, 5}  ← silent corruption

// GOOD: the three-index slice s[a:b:c] caps it at c-a → append reallocates
base = []int{1, 2, 3, 4, 5}
sub = base[0:2:2]     // len=2, cap=2
sub = append(sub, 99) // cap exhausted → new array
// base == {1, 2, 3, 4, 5}  ← untouched
```

The three-index slice `s[low:high:max]` yields `len = high-low`, `cap = max-low`. Use it to isolate `cap` when you hand out a sub-slice the receiver might grow.

---

## Maps — A Pointer to Runtime State

A map value is a pointer to a runtime-managed structure. Passing a map copies that pointer, so inserts and deletes inside a function **are visible outside**.

```go
func addKey(m map[string]int) {
	m["new"] = 1 // visible to the caller — same underlying map
	delete(m, "old")
}
```

### nil map — reads OK, writes panic

```go
var m map[string]int // nil, not initialized
_, ok := m["x"]      // fine: reading a nil map returns the zero value, ok=false
m["x"] = 1           // panic: assignment to entry in nil map
```

Initialize before writing: `m := make(map[string]int)` or `m := map[string]int{}`.

### Copying a map

There is no implicit copy: assigning a map copies the pointer, not the entries. For a real copy use `maps.Clone` (Go 1.21+) or iterate.

```go
// Shallow copy: keys and values are copied by value, but a value that is
// itself a slice, map or pointer is still shared.
clone := maps.Clone(m)

// Manual equivalent
clone := make(map[string]int, len(m))
for k, v := range m {
	clone[k] = v
}
```

---

## Array vs Slice — Why Go APIs Take Slices

An array has pure value semantics: passing a `[N]T` **copies all N elements**. A slice shares its backing array. That is why Go APIs accept slices, not arrays.

```go
// array: the whole array is copied → the original doesn't change
func mutArr(a [3]int) { a[0] = 99 }

// slice: shares the backing array → the original changes
func mutSlice(s []int) { s[0] = 99 }

arr := [3]int{1, 2, 3}
mutArr(arr)
fmt.Println(arr) // [1 2 3] — the function got its own 24-byte copy

sl := []int{1, 2, 3}
mutSlice(sl)
fmt.Println(sl) // [99 2 3] — the backing array was shared
```

Passing a `[1024]byte` by value copies 1 KB per call. Pass `arr[:]` (a slice) or `*[1024]byte` when the array is large.

---

## Structs — Shallow Copy

Copying a struct copies all its fields, but the copy is **shallow**: fields that are pointers, slices or maps still point to the same data as the original.

```go
type User struct {
	Name  string
	Tags  []string // header copied; backing array SHARED
	Prefs *Prefs   // pointer copied; the Prefs it points to SHARED
}

u1 := User{Name: "a", Tags: []string{"x"}}
u2 := u1         // shallow copy
u2.Name = "b"    // independent (string is a value)
u2.Tags[0] = "z" // also changes u1.Tags[0]: shared backing array
```

### go vet copylocks — copying a mutex is a bug

A `sync.Mutex` must not be copied: the copy has its own lock state and protects nothing. The same holds for anything containing one, and for `sync.WaitGroup`, `sync.Once`, `sync.Cond` and the `sync/atomic` types. `go vet` (the `copylocks` analyzer) reports it.

```go
type Counter struct {
	mu    sync.Mutex
	count int
}

// BAD: a value receiver copies the struct, and with it the mutex.
// go vet: "Inc passes lock by value: Counter contains sync.Mutex"
func (c Counter) Inc() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.count++ // also mutates a copy — useless
}

// GOOD: the pointer receiver works on the original mutex
func (c *Counter) Inc() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.count++
}
```

Corollary: a struct that contains a `sync.Mutex`, `sync.WaitGroup`, `sync.Once`, etc. is passed and received by pointer.

### range copies the element

`for _, v := range xs` assigns a **copy** of each element to `v`. Writing to `v` does not touch the slice.

```go
type Point struct{ X, Y int }
xs := []Point{{1, 1}, {2, 2}}

// BAD: v is a copy; the write is discarded
for _, v := range xs {
	v.X = 100
}

// GOOD: index into the original slice
for i := range xs {
	xs[i].X = 100
}
```

Go 1.22 made loop variables per-iteration, which fixed closure capture, but `v` is still a copy of the element; that did not change.

---

## Receiver: Value vs Pointer (Copy Semantics)

A **value receiver** works on a copy of the receiver: changes to it are lost. A **pointer receiver** works on the original: changes persist.

```go
type Meter struct{ n int }

func (m Meter) BumpByValue() { m.n++ } // works on a copy; lost on return
func (m *Meter) BumpByPtr()  { m.n++ } // works on the original; persists
```

This file only covers the copy semantics. The full value-vs-pointer receiver decision (copy cost by struct size, escape analysis, method-set consistency, performance) lives in `references/memory-pointers.md`.

---

## Defensive Copies

When you store a received slice or map in a field, or return an internal slice or map, the caller keeps a header pointing at **your** backing data. It can change your internal state without going through your methods. A defensive copy cuts that aliasing at the boundary.

```go
type Config struct {
	tags []string
}

// BAD: stores the caller's slice — aliases its backing array
func (c *Config) SetTags(tags []string) {
	c.tags = tags // the caller can mutate c.tags later
}

// GOOD: copy on the way in → internal state isolated from the caller
func (c *Config) SetTags(tags []string) {
	c.tags = slices.Clone(tags)
}

// BAD: exposes the internal slice — the caller can overwrite your state
func (c *Config) Tags() []string { return c.tags }

// GOOD: return a copy → the caller mutates theirs, not yours
func (c *Config) Tags() []string { return slices.Clone(c.tags) }
```

When to copy defensively:

- When **storing** a slice, map or pointer the caller still holds and might mutate.
- When **returning** an internal slice or map that represents protected state (especially behind a mutex).
- **Not** when the data is ephemeral, immutable by contract, or the copy dominates a hot path; measure first.

`slices.Clone` and `maps.Clone` (Go 1.21+) make shallow copies, enough when the elements are values. If elements contain pointers and you need full isolation, the copy must be deep: clone each element yourself.

---

## Checklist

- [ ] Every argument is a copy; pass an explicit `*T` to mutate the original
- [ ] Don't mutate the elements of a received slice unless the contract says so
- [ ] `s = append(s, ...)` — never discard `append`'s result
- [ ] Three-index slice `s[a:b:c]` to cap a sub-slice the receiver might grow
- [ ] Maps initialized with `make` (or a literal) before writing — writing to a nil map panics
- [ ] `maps.Clone` / `slices.Clone` for shallow copies; deep copy by hand if elements hold pointers
- [ ] Large arrays passed as a slice (`arr[:]`) or `*[N]T`, not by value
- [ ] Struct copies are shallow: inner slices, maps and pointers stay shared
- [ ] No struct containing a `sync.Mutex` is copied — pointer receivers, and `go vet` clean
- [ ] In `range`, index `xs[i]` to mutate; `v` is a copy
- [ ] Defensive copy when storing or returning slices/maps that are protected internal state
- [ ] For the full value-vs-pointer receiver decision from a performance angle, see `references/memory-pointers.md`
