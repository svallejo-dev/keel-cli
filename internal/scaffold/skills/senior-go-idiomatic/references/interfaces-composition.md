# Interfaces & Composition — Go Idiomatic Reference

## Contents

1. Implicit satisfaction
2. The consumer defines the interface
3. Accept interfaces, return concrete types
4. Interface pollution
5. Small interfaces and segregation
6. Method sets — value vs pointer receiver
7. The nil-interface footgun
8. Compile-time interface assertion
9. Struct embedding — composition over inheritance

---

## The Mental Model

In Go an interface is not a contract a type signs; it is one the type **fulfils without knowing**. There is no `implements` and no `extends`: a type satisfies an interface simply by having its methods. This reverses the design direction compared with Java/C# — interfaces don't live where they are implemented, they live where they are consumed, and they are as small as the consumer needs. Everything else here follows from that idea: **implicit satisfaction**.

---

## 1. Implicit satisfaction

A type satisfies an interface by **having its methods** (the right method set), not by declaring it. The compiler checks at the point where the value is assigned or passed to the interface, never at the type's definition.

```go
// Java/C#:  class Temperature implements Stringer { ... }   ← explicit coupling
// Go:       there is no "implements". If you have String(), you are a fmt.Stringer.

type Temperature float64

func (t Temperature) String() string { // this method is enough
	return fmt.Sprintf("%.1f°C", float64(t))
}

var s fmt.Stringer = Temperature(21.5) // satisfies without declaring it
```

The key advantage: you can satisfy interfaces from packages you **don't control**, and **retrofit** a type to a new interface without touching it. In a nominal language that means editing the class; in Go you define the method (or wrap the foreign type).

```go
type IDer interface{ ID() string }

// wire.Message is third-party; wrap it so it speaks IDer.
type OrderMessage struct{ wire.Message }

func (m OrderMessage) ID() string { return "order-" + m.Ref }
```

Type and interface evolve in different packages with no compile-time dependency between them — that decoupling is the basis of idiomatic design.

---

## 2. The consumer defines the interface

The interface lives in the package that **uses** it, not in the one that implements it. The implementer exports a **concrete type**; the consumer declares the minimal interface it consumes. This is the general form of "interfaces at the point of use"; inside a bounded context the same rule produces small ports owned by the context (see the ddd-go-idiomatic skill).

BAD — the implementing package exports a large interface "for completeness", so every
implementation (and every fake) must carry five methods hardly anyone uses:

```go
package pgstore

type UserStore interface {
	GetByID(ctx context.Context, id int64) (*user.User, error)
	Create(ctx context.Context, u *user.User) error
	Delete(ctx context.Context, id int64) error
	Migrate(ctx context.Context) error
	Stats() string
}
```

GOOD — the infrastructure subpackage exports the concrete type and nothing more:

**internal/user/pgstore/store.go**
```go
package pgstore

type Store struct{ pool *pgxpool.Pool }

func (s *Store) GetByID(ctx context.Context, id int64) (*user.User, error) { /* ... */ }

// ...Create, Delete, Migrate, Stats
```

GOOD — the consumer declares the minimal interface it needs, one method:

**internal/greet/greeter.go**
```go
package greet

type userReader interface { // unexported; only what is consumed
	GetByID(ctx context.Context, id int64) (*user.User, error)
}

type Greeter struct{ users userReader }

// *pgstore.Store satisfies userReader without knowing it exists.
```

The interface belongs to the side that **depends** on the abstraction: the implementer never imports the consumer, and a test fake implements one method instead of five.

---

## 3. Accept interfaces, return concrete types

```
Accept interfaces → Return concrete types
```

- **Input = interface** → flexibility: accept any source that meets the contract (file, network, string, buffer, fake).
- **Output = concrete type** → clarity and the full feature set: the caller gets every field and method. Returning an interface hides capabilities and forces type assertions.

```go
// Accepts io.Reader (any source); returns a concrete *Report (full API).
func Analyze(src io.Reader) (*Report, error) {
	rep := &Report{}
	sc := bufio.NewScanner(src)
	for sc.Scan() {
		rep.Lines++
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("analyze: %w", err)
	}
	return rep, nil // the caller sees rep.Summary(), rep.Lines, etc.
}

rep, _ := Analyze(strings.NewReader("a\nb\nc\n")) // string, file or body: all the same
```

Legitimate exception: return an interface when the set of implementations is closed and the caller must treat them uniformly (`error` is the canonical case).

---

## 4. Interface pollution

> *"Don't design with interfaces, discover them."* — Rob Pike

Don't create an interface for every struct "just in case". An interface with **one implementation**, no real test double and no concrete need to swap is noise: it duplicates the API, adds an indirection and hides the real type.

```go
// BAD — speculative interface: one implementation, no reason.
type Cacher interface {
	Get(k string) ([]byte, bool)
	Set(k string, v []byte)
}
type Cache struct{ /* the only implementation there will ever be */ }
```

```go
// GOOD — expose the concrete struct. Discover the interface when a real consumer
// needs it (a test double or a second backend).
type Cache struct{ m map[string][]byte }

func NewCache() *Cache                       { return &Cache{m: make(map[string][]byte)} }
func (c *Cache) Get(k string) ([]byte, bool) { v, ok := c.m[k]; return v, ok }
func (c *Cache) Set(k string, v []byte)      { c.m[k] = v }
```

Adding the interface costs three lines the day it is needed; removing a premature one means refactoring every caller.

---

## 5. Small interfaces and segregation

The smaller the interface, the more types satisfy it and the easier it composes. `io.Reader` has **one** method by design. Larger interfaces are **composed** from small ones via **interface embedding**:

```go
// The actual stdlib definitions (package io):
type Reader interface {
	Read(p []byte) (n int, err error)
}
type Closer interface {
	Close() error
}

// ReadCloser redeclares nothing: it embeds Reader and Closer.
type ReadCloser interface {
	Reader
	Closer
}
```

A consumer that only closes should accept `io.Closer`, not `io.ReadCloser`: ask for the **minimal** contract and you accept more types.

```go
func closeQuietly(c io.Closer) { _ = c.Close() } // accepts any Closer
```

---

## 6. Method sets — value vs pointer receiver

| Type of the value | Method set includes | Satisfies interfaces whose methods are… |
|---|---|---|
| `T`  | **value-receiver** methods | …value-receiver only |
| `*T` | value **and** pointer-receiver methods | …value and pointer receiver |

Practical consequence: if any method uses a pointer receiver, you must assign `*T` (not `T`) to the interface, or it doesn't compile.

```go
type Counter struct{ n int }

func (c *Counter) Inc()          { c.n++ }                         // pointer receiver
func (c Counter) String() string { return fmt.Sprintf("%d", c.n) } // value receiver

var s fmt.Stringer = Counter{}        // OK: String has a value receiver
var w interface{ Inc() } = &Counter{} // OK: *Counter has both

// Does not compile: Counter (value) has no Inc in its method set.
//   var bad interface{ Inc() } = Counter{}
//   → "Counter does not implement ... (method Inc has pointer receiver)"
```

(The example mixes receiver kinds only to show the method-set rule; in real code keep one kind per type.) Value vs pointer receiver also affects copying and performance (mutation, struct size, escape analysis); the reasoning lives in `references/memory-pointers.md` §4 and `references/value-vs-reference.md`.

---

## 7. The nil-interface footgun

An interface value is a **(type, value)** pair. It is `nil` **only if both** are nil. A typed nil pointer — `(*T)(nil)` — stored in an interface carries the type `*T`, so the interface is **not nil** even though the value inside is.

The classic bug: returning a nil `*MyError` declared as `error`.

```go
type MyError struct{ Code int }

func (e *MyError) Error() string { return fmt.Sprintf("code=%d", e.Code) }

// BAD: returns a nil *MyError as error. The interface carries type=*MyError,
// value=nil → it is not the nil interface, and every `if err != nil` fires.
func doWorkBad(fail bool) error {
	var err *MyError // nil pointer
	if fail {
		err = &MyError{Code: 500}
	}
	return err // always a non-nil interface (the type is never nil)
}

// GOOD: return an untyped nil on the success path.
func doWorkGood(fail bool) error {
	if fail {
		return &MyError{Code: 500}
	}
	return nil // a truly nil interface: (type=nil, value=nil)
}
```

Runtime demonstration (it compiles and runs; the bug is in the logic):

```go
func main() {
	err := doWorkBad(false)         // nothing failed
	fmt.Println(err == nil)         // prints: false  ← the footgun
	fmt.Printf("%T %v\n", err, err) // *main.MyError <nil>
}
```

Rules: don't declare a local `*T` and return it as `error` (return `nil` on success, and the `*T` only on failure); be suspicious of `if err != nil` when the value prints `<nil>`. `go vet` does not report this; the `nilness` analyzer and the `nilerr` linter catch common forms.

---

## 8. Compile-time interface assertion

A blank-identifier assignment anchors satisfaction at compile time. It costs nothing at run time (`(*T)(nil)` is a typed nil pointer) and fails the build if the type stops meeting the contract.

```go
// Fails to compile if *Server stops satisfying http.Handler.
var _ http.Handler = (*Server)(nil)
```

Use it where a type is meant to satisfy an interface that nothing in its own package otherwise references — an adapter that satisfies a consumer's port, a type that must be an `http.Handler` or `io.Reader`. Without the assertion, a signature drift surfaces far away (in the wiring code, or in another module); with it, the failure points at the type. Where the package already assigns the type to the interface (a constructor returning the interface, a call that passes it), the assertion adds nothing and can be left out.

---

## 9. Struct embedding — composition over inheritance

Go has no inheritance. It has **embedding**: a type placed inside a struct without a field name. The outer type **promotes** the embedded type's fields and methods, but there is no subtyping and no dynamic polymorphism — it is composition, not a hierarchy.

### Field and method promotion

```go
type Base struct{ ID int64 }

func (b Base) Describe() string { return fmt.Sprintf("id=%d", b.ID) }

type User struct {
	Base // embedded (no field name)
	Name string
}

u := User{Base: Base{ID: 7}, Name: "ada"}
_ = u.ID         // promoted field (sugar for u.Base.ID)
_ = u.Describe() // promoted method
_ = u.Base.ID    // the explicit path still works
```

### Embedding an interface in a struct — decorator/wrapper

Embedding an **interface** (not a struct) promotes its methods to the outer type. This is how you wrap, say, an `io.Reader` and **override** one method while delegating the rest of the contract to the embedded value.

```go
// countingReader is an io.Reader (Read is promoted) that counts bytes.
type countingReader struct {
	io.Reader // embedded interface: promotes Read and any other method
	N         int64
}

// Overrides Read; the embedded io.Reader covers the rest of the contract.
func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.Reader.Read(p) // delegate to the wrapped reader
	c.N += int64(n)
	return n, err
}

var _ io.Reader = (*countingReader)(nil)
```

You wrap a large interface and intercept **one** method without reimplementing the others: the compiler promotes the rest from the embedded value.

### Override by shadowing

```go
type Loud struct{ Base }

// Loud.Describe shadows the one promoted from Base.
func (l Loud) Describe() string { return "LOUD:" + l.Base.Describe() }
```

### Ambiguity with multiple embedding

If two embedded types contribute a method (or field) with the **same name at the same depth**, the promoted selector is **ambiguous** and using it doesn't compile. Resolve it by qualifying the path or defining the method on the outer type.

```go
type A struct{}
type B struct{}

func (A) Ping() string { return "a" }
func (B) Ping() string { return "b" }

type C struct {
	A
	B
}

// c.Ping() would not compile: "ambiguous selector c.Ping".
// Disambiguate by defining the method on C and choosing the path:
func (c C) Ping() string { return c.A.Ping() }
```

A method at a shallower depth **wins** over a deeper one; ambiguity only arises at the same depth.

---

## Checklist

- [ ] No type declares `implements`; satisfaction is implicit and checked at the point of use
- [ ] Interfaces are defined in the package that **consumes** them, not in the one that implements them
- [ ] Functions **accept interfaces** and **return concrete types** (except closed sets like `error`)
- [ ] No single-implementation interfaces without a real test double or swap (no interface pollution)
- [ ] Interfaces are minimal; larger ones are composed by **interface embedding**
- [ ] If any method has a pointer receiver, `*T` is assigned to the interface and the type uses pointer receivers throughout
- [ ] No nil local `*T` returned as `error`; the success path returns an untyped `nil`
- [ ] `var _ Iface = (*T)(nil)` where a type must satisfy an interface its own package doesn't otherwise reference (adapters, handlers)
- [ ] Embedding is used for composition (promotion, decorators), never to imitate inheritance
- [ ] Ambiguity from multiple embedding is resolved with an explicit method on the outer type
