# Idioms & Style — Go Idiomatic Reference

## Contents

1. Idiomatic naming
2. Useful zero value
3. Control flow — early return
4. Enums with iota + stringer
5. Comments: few, short, about "why" (the full comment policy)
6. defer subtleties
7. Modern slices and maps
8. Tooling

Naming, control flow and style that a Go code review takes for granted. Error handling in depth lives in `references/errors-advanced.md`; here only the style of **error strings** is fixed (section 1).

---

## 1. Idiomatic naming

Go identifiers don't use `snake_case`: everything is **MixedCaps** (`UserID`) or **mixedCaps** (`parseURL`). The first letter decides visibility: **upper case = exported**, lower case = package-private.

### Getters, setters and receivers

```go
// User is an account holder identified by a numeric ID.
type User struct {
	name string
	id   int64
}

func (u *User) Name() string     { return u.name } // getter: no Get prefix
func (u *User) SetName(n string) { u.name = n }    // setter: Set prefix
func (u *User) ID() int64        { return u.id }   // acronym in caps: ID
```

- Getters have **no** `Get` prefix: `user.Name()`, not `user.GetName()`. Setters do use `Set`.
- Receiver names are short (1–2 letters) and **the same** across all methods of the type (never `self`/`this`), so every method reads alike.

### Stutter and acronyms

**Stutter** is repeating the package name in the identifier; callers already write the package name, so the repetition is noise.

| BAD (stutter)      | GOOD           | Why                          |
| ------------------ | -------------- | ---------------------------- |
| `http.HTTPServer`  | `http.Server`  | The package already says `http` |
| `user.UserStore`   | `user.Store`   | The package already says `user` |

Acronyms keep a **consistent case**: `userID` (not `userId`), `ServeHTTP`, `parseURL` — all upper case, or all lower case when the identifier starts lower.

### Packages, files and error strings

Package names are short, lower case, one word, **no** `under_scores`, no plurals: `user`, `bytes` — not `user_service`, `utils`, `models` (`bytes.Buffer`, not `bytesutil.BytesBuffer`).

File names are `snake_case` and named by what they contain; no `utils.go`/`helpers.go`/`common.go` grab-bags. Inside a bounded context, one use case per file — see the ddd-go-idiomatic skill.

Error strings get concatenated into longer messages (`"load config: open file: permission denied"`), so they are lower case, have **no** trailing punctuation and **no** `"failed to"` (every error is a failure).

```go
errors.New("Failed to open config file.")   // BAD
errors.New("open config: file not found")    // GOOD
```

---

## 2. Useful zero value

Design types that work without a constructor: the **zero value** should be valid. `sync.Mutex`, `bytes.Buffer` and a `nil` slice (appendable) all work as soon as they are declared.

```go
// Counter is safe for concurrent use; its zero value is ready to use.
type Counter struct {
	mu    sync.Mutex
	total int64
}

func (c *Counter) Add(n int64) {
	c.mu.Lock()
	c.total += n
	c.mu.Unlock()
}

var c Counter    // ready without a constructor; the sync.Mutex is already valid
c.Add(3)

var s []int      // nil slice, but appendable
s = append(s, 1) // no make() needed first
```

If the zero value already works, don't write `NewCounter()`; a constructor that only does `return &Counter{}` is noise. Write a constructor when it establishes an invariant (validation, required dependencies, initialized maps).

---

## 3. Control flow — early return

Keep the **happy path** at the left margin, unindented. Handle error cases first with **early returns**, avoiding `else` and nesting, so a reader can scan straight down the success path.

```go
// BAD: happy path indented, needless else, deep nesting.
func processBad(r *Record) error {
	if r != nil {
		if r.Valid {
			return nil
		} else {
			return errInvalid
		}
	} else {
		return errNil
	}
}

// GOOD: early returns, happy path on the left.
func processGood(r *Record) error {
	if r == nil {
		return errNil
	}
	if !r.Valid {
		return errInvalid
	}
	return nil
}
```

---

## 4. Enums with iota + stringer

Go has no `enum` type: use a named type + `iota`, plus a `String()` method for logs and errors. Avoid magic strings (`"running"`): they are not type-safe and the compiler cannot catch typos.

```go
// State is the lifecycle state of a worker.
type State int

//go:generate go tool stringer -type=State

const (
	StateIdle State = iota
	StateRunning
	StatePaused
	StateStopped
)
```

`go generate` runs `stringer`, which writes `state_string.go` with the `String()` method. Since Go 1.24 the tool is pinned in `go.mod` with `go get -tool golang.org/x/tools/cmd/stringer` and run with `go tool stringer`, so every contributor and CI use the same version without a separate install step. With `iota`, inserting a value in the middle renumbers the rest automatically; regenerate after changing the constants.

What `stringer` generates is equivalent to this hand-written version (only write it by hand when you can't run the generator):

```go
func (s State) String() string {
	switch s {
	case StateIdle:
		return "StateIdle"
	case StateRunning:
		return "StateRunning"
	case StatePaused:
		return "StatePaused"
	case StateStopped:
		return "StateStopped"
	default:
		return fmt.Sprintf("State(%d)", int(s))
	}
}
```

The `default` branch covers out-of-range values, so a corrupted state prints `State(7)` instead of an empty string.

---

## 5. Comments: few, short, about "why"

This section is the full comment policy. Well-named code already says **what** it does; a comment earns its place only when it says something the code cannot. If a file has more comment lines than code lines, either the code (names) or the comments (noise) are the problem. Rough target: comment lines ≤ 10–15% of the file.

**Write:**
- A **one-line** package doc, in a single file of the package (usually the file named after the package): `// Package user models accounts and their lifecycle.` Longer package documentation goes in `doc.go`, and only for packages that are a public API.
- A **one-line** doc comment on every exported identifier. `revive`'s `exported` rule requires it, and it must be a sentence starting with the identifier's name. Make it add what the name cannot say: units, invariants, the errors it returns, concurrency safety, ownership of arguments. `// Timeout is the per-request deadline in seconds.` earns its line; `// NewUser creates a new User.` does not.
- Inline comments only for the non-obvious **why**: an external constraint, a workaround with its issue link, a measured performance decision.
- Directives: `//go:generate`, `//go:embed`, `//nolint:name // reason`.

Unexported identifiers usually need no doc comment; add one only when it records a why the name can't carry.

**Don't write:**
- Narration of the code (`// increment i`, `// call the repo`, `// return the error`).
- Doc comments that restate the name (`// NewUser creates a new User.`).
- Banners and separators (`// -------`, `// ==== Handlers ====`): the file is already the section.
- Comments longer than the code they sit on. Design rationale goes in the README or an ADR, not above every function.
- Commented-out code (version control remembers it) and `TODO` without an owner or issue.

```go
// BAD: the comments outweigh the code and say nothing the code doesn't.

// -----------------------------------------------------------------------------
// NewUser creates a new User. It receives the email and the name, validates
// them and returns the User or an error if validation fails.
func NewUser(email Email, name string) (User, error) {
	// check that the name is not empty
	if name == "" {
		return User{}, ErrEmptyName // return the error
	}
	// build and return the user
	return User{email: email, name: name}, nil
}
```

**internal/user/user.go**
```go
// Package user models accounts and their lifecycle.
package user

// NewUser returns ErrEmptyName when name is blank; email is already validated.
func NewUser(email Email, name string) (User, error) {
	if name == "" {
		return User{}, ErrEmptyName
	}
	return User{email: email, name: name}, nil
}

func canonicalAddress(addr string) string {
	local, domain, ok := strings.Cut(addr, "@")
	if !ok {
		return addr
	}
	// Only the domain is case-insensitive; RFC 5321 lets servers treat the local part as case-sensitive.
	return local + "@" + strings.ToLower(domain)
}
```

### Example tests

**Example tests** are executable documentation: they appear on pkg.go.dev, and `go test` checks that their stdout matches the `// Output:` comment. If the output drifts, the test fails, so the documentation can't go stale.

```go
func ExampleAbs() {
	fmt.Println(Abs(-42))
	fmt.Println(Abs(7))
	// Output:
	// 42
	// 7
}
```

---

## 6. defer subtleties

### Arguments are evaluated when the defer is declared

```go
// BAD: n is evaluated when the defer is declared; the later assignment is not seen.
func snapshotBad(n int) {
	defer fmt.Println("captured:", n) // prints the original n
	n = 99
}

// GOOD: the closure reads n when the function returns.
func snapshotGood(n int) {
	defer func() { fmt.Println("captured:", n) }() // prints 99
	n = 99
}
```

The same trap bites with time: `defer log.Println(time.Since(start))` evaluates `time.Since` **now** (≈ 0), not at return. `go vet` reports it.

### defer inside a loop piles up until the function returns

```go
// BAD: every defer f.Close() waits until processAllBad returns.
func processAllBad(paths []string) error {
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close() // every file stays open for the whole loop
	}
	return nil
}

// GOOD: move the body into a function; each defer runs per iteration.
func processAllGood(paths []string) error {
	for _, p := range paths {
		if err := processOne(p); err != nil {
			return err
		}
	}
	return nil
}

func processOne(p string) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close() // released when processOne returns
	return nil
}
```

### defer + named result: don't lose the Close error

When writing a file, `Close` can fail (the OS may report a deferred write error there), so `defer f.Close()` alone silently drops it. A deferred closure with a named result keeps the first error:

```go
func writeConfig(path string, data []byte) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	_, err = f.Write(data)
	return err
}
```

With a `bufio.Writer`, flush explicitly as the last step of the success path and return its error; don't `defer bw.Flush()`, which discards the error and leaves the outcome to defer ordering. `bufio.Writer` errors are sticky — after a failed write every later write and `Flush` return the same error — so checking `Flush` also covers the writes before it:

```go
func writeLines(path string, lines []string) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	bw := bufio.NewWriter(f)
	for _, l := range lines {
		fmt.Fprintln(bw, l)
	}
	return bw.Flush()
}
```

---

## 7. Modern slices and maps

Since Go 1.21 the `slices`/`maps` packages and the `min`/`max`/`clear` builtins replace hand-written loops. Since Go 1.23, `maps.Keys`/`maps.Values` return **iterators** (`iter.Seq`), which `slices.Sorted` and `slices.Collect` turn into slices.

```go
nums := []int{3, 1, 2}
slices.Sort(nums)                      // [1 2 3]
_ = slices.Contains(nums, 2)           // true
_ = slices.Equal(nums, []int{1, 2, 3}) // true

i := slices.IndexFunc(nums, func(n int) bool { return n > 1 }) // 1
nums = slices.Insert(nums, 0, 0)                                // [0 1 2 3]
nums = slices.Delete(nums, 1, 2)                                // [0 2 3]

x := min(3, 7) // 3
y := max(3, 7) // 7

m := map[string]int{"c": 3, "a": 1, "b": 2}
keys := slices.Sorted(maps.Keys(m))  // [a b c]   (Go 1.23+)
vals := slices.Collect(maps.Values(m)) // unordered (Go 1.23+)
clear(m)                               // empties the map in place
```

Before writing a `for` loop to sort, search, insert, delete or compare, check whether `slices`/`maps` already does it: the library versions handle the edge cases (`slices.Delete` zeroes the vacated tail so the garbage collector can reclaim it). Always reassign the result of `Insert`/`Delete`, as with `append`. `clear` on a slice sets every element to its zero value without changing the length.

---

## 8. Tooling

In Go formatting is **not debated**: `gofmt` defines a single canonical format, so code review never spends time on tabs vs spaces or brace placement.

| Tool                  | Role                                                     |
| --------------------- | -------------------------------------------------------- |
| `gofmt` / `goimports` | Canonical format + import grouping; runs in the editor   |
| `go vet`              | Finds bugs that compile (bad defer, Printf args, copied locks) |
| `staticcheck`         | Deeper static analysis (unused code, simplifications)    |
| `golangci-lint`       | Meta-linter that runs the above (and `revive`) in CI     |

`goimports` is `gofmt` plus import management. Run `go vet` and a linter (`golangci-lint` or `staticcheck`) in CI, not only in editors: they catch real bugs at no review cost, and a CI gate keeps the whole team on the same rules. A PR that fails `gofmt -l` (empty output means clean) is not worth reviewing yet.

---

## Checklist

- [ ] Identifiers in MixedCaps; no `snake_case` (file names do use it)
- [ ] Exported ⇔ upper-case initial; no `Get` on getters, `Set` on setters
- [ ] Short receiver name (1–2 letters), the same across all methods of the type
- [ ] No stutter (`http.Server`, not `http.HTTPServer`)
- [ ] Acronyms in consistent case (`userID`, `ServeHTTP`, `parseURL`)
- [ ] Short, lower-case, singular package names; no under_scores, no `util`/`models`
- [ ] Error strings lower case, no trailing period, no `"failed to"`
- [ ] Usable zero value; no constructor unless it establishes an invariant
- [ ] Early return; happy path unindented; no `else` after `return`
- [ ] Enums with `iota` + generated `String()` (`go tool stringer`); no magic strings
- [ ] One-line package doc in one file; one-line doc comment on exported identifiers that starts with the name and adds information; Example tests with `// Output:`
- [ ] No narrating comments, banners or comments longer than the code; inline comments only for the non-obvious why
- [ ] File names `snake_case`, named by what they contain; no `utils.go`/`helpers.go`/`common.go`
- [ ] `defer`: watch arguments evaluated at declaration, loops, and the error from `Close`/`Flush`
- [ ] `slices`/`maps`/`min`/`max`/`clear` instead of hand-written loops
- [ ] `gofmt -l` empty, `go vet` clean and linter green before pushing
