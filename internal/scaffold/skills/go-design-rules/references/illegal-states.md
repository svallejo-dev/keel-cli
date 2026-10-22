# Illegal States: Model With Types So Invalid Values Do Not Compile

## Contents

1. Make illegal states unrepresentable + parse, don't validate
2. Constructors that return `(T, error)`
3. `iota` enums and the invalid zero value
4. Distinct types for units
5. Functional options instead of bool flags and contradictory config
6. Mutually exclusive states: sum types in Go
7. Goal: push every invariant as early as possible

Techniques to make the set of values a type admits match the set the domain allows, moving
errors from runtime to compile time. A fortress type that fights `gofmt`, value semantics or
a useful zero value is not safer, it is friction; when they clash, the Go idiom wins.

## 1. Make illegal states unrepresentable + parse, don't validate

**Rule.** Model the domain with types such that an invalid value cannot be built.
**Why.** Every raw `string`/`int` in circulation is a pending re-validation: nobody knows
whether it was checked. **Parse, don't validate**: at the boundary (HTTP, CLI, DB) parse the
raw input into a domain type once. `validate(raw) error` leaves `raw` alive;
`parse(raw) (T, error)` returns a value whose existence *is* the proof of validity, and
unexported fields keep it that way.

```go
// RegistrationRequest is raw transport input; nothing about it is trusted.
type RegistrationRequest struct {
	Email string
	Age   int
}

// Registration is a parsed request: a well-formed address and an adult age.
type Registration struct {
	email string
	age   int
}

// ParseRegistration is the only way to get a Registration, so holding one proves it is valid.
func ParseRegistration(req RegistrationRequest) (Registration, error) {
	addr, err := mail.ParseAddress(req.Email)
	if err != nil {
		return Registration{}, fmt.Errorf("parse registration: email: %w", err)
	}
	if req.Age < 18 {
		return Registration{}, fmt.Errorf("parse registration: age %d below minimum", req.Age)
	}
	return Registration{email: addr.Address, age: req.Age}, nil
}

// Email is the parsed address, without display name.
func (r Registration) Email() string { return r.email }
```

Had `Registration` exported its fields, any package could write
`Registration{Email: "x", Age: 3}` and the parse would prove nothing. Where the boundary sits
inside a bounded context (the use case parses its `XInput` into value objects) is described
by the `ddd-go-idiomatic` skill.

## 2. Constructors that return `(T, error)`

**Rule.** A type's validation lives ONCE, in its constructor. **Why.** If the only path to a
`Percentage` goes through `NewPercentage`, holding one already implies a valid range; no
consumer re-checks `0..100`.

```go
// Percentage is a value in [0, 100].
type Percentage struct {
	v float64
}

// NewPercentage rejects values outside [0, 100].
func NewPercentage(v float64) (Percentage, error) {
	if v < 0 || v > 100 {
		return Percentage{}, fmt.Errorf("percentage: %v out of range 0..100", v)
	}
	return Percentage{v: v}, nil
}

// Apply returns the percentage of amount.
func (p Percentage) Apply(amount float64) float64 {
	return amount * p.v / 100
}
```

The unexported `v` makes `Percentage{v: 500}` impossible outside the package. The only value
nobody validated is the zero value (`Percentage{}` ≡ 0%), and here 0 is legal; when it is
not, see §3. Note that a defined type (`type Percentage float64`) would **not** give this
guarantee: `Percentage(500)` compiles in any package, and so does passing the untyped
constant `500` where a `Percentage` is expected. If the zero value is valid and there is no
invariant to protect, a constructor that just returns `T{}` is noise: leave it out.

## 3. `iota` enums and the invalid zero value

**Rule.** The zero value of an `iota` enum is its first constant; design it on purpose.
**Why.** An uninitialized `Status` is `0`; if `0` is `StatusPending`, a half-built struct
"looks" pending and the bug is silent. Two combinable patterns: **(a)** zero is an explicit
`Unknown`/`Invalid` sentinel no flow produces; **(b)** a `Parse*` function at the boundary is
the only bridge from strings.

```go
// Status is the shipping state of an order.
type Status int

//go:generate stringer -type=Status

// Shipping states; zero is deliberately invalid so a forgotten field is detectable.
const (
	StatusUnknown Status = iota
	StatusPending
	StatusShipped
	StatusDelivered
)

// ParseStatus is the only conversion from external strings.
func ParseStatus(raw string) (Status, error) {
	switch raw {
	case "pending":
		return StatusPending, nil
	case "shipped":
		return StatusShipped, nil
	case "delivered":
		return StatusDelivered, nil
	default:
		return StatusUnknown, fmt.Errorf("invalid status %q", raw)
	}
}
```

`go generate` with `stringer` produces `String()`. Never use magic strings (`"shipped"`) as
internal state: they are not type-safe and the compiler cannot catch a typo.

## 4. Distinct types for units

**Rule.** Give each unit its own named type; do not share `float64`/`int`. **Why.** `Meters`
and `Feet` are both `float64`, but adding them is a bug; with distinct types the compiler
stops you. Same for `Cents` vs a `float64` of dollars, where you also lose precision.

```go
type (
	Meters float64
	Feet   float64
)

// ToFeet is the only bridge between the two units.
func (m Meters) ToFeet() Feet { return Feet(m * 3.28084) }

func runwayMargin(length Meters, needed Feet) Feet {
	// return length - needed  // does not compile: mismatched types Meters and Feet
	return length.ToFeet() - needed
}
```

Zero runtime cost (they are the underlying type), and the explicit conversion marks where
you change units, which is exactly where factor errors sneak in. Every `float64` is a valid
length, so a defined type is enough here: there is no invariant for a constructor to protect.

## 5. Functional options instead of bool flags and contradictory config

**Rule.** For constructors with many optional parameters, use **functional options** rather
than positional flags or a config struct that admits invalid combinations. **Why.**
`NewServer(addr, true, false, 30)` is unreadable (what is `false`?) and grows badly; a
`Config{TLS: false, Cert: "x.pem"}` makes "no TLS but with a certificate" representable. An
`Option` is a function that mutates an unexported `config`.

```go
type config struct {
	tls     bool
	cert    string
	timeout time.Duration
}

// Option configures a Server.
type Option func(*config)

// WithTLS enables TLS with the given certificate; it is the only way to set a certificate.
func WithTLS(cert string) Option {
	return func(c *config) {
		c.tls = true
		c.cert = cert
	}
}

// WithTimeout overrides the 30s default.
func WithTimeout(d time.Duration) Option {
	return func(c *config) { c.timeout = d }
}

// NewServer applies opts over safe defaults; addr is required, so it is positional.
func NewServer(addr string, opts ...Option) *Server {
	cfg := config{timeout: 30 * time.Second}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Server{addr: addr, cfg: cfg}
}
```

```go
s := NewServer("localhost:8443",
	WithTLS("server.pem"),
	WithTimeout(5*time.Second),
)
```

`WithTLS` binds `tls` and `cert` in one operation: there is no way to ask for a certificate
without enabling TLS. Here the pattern removes a contradictory state, not just noise.

## 6. Mutually exclusive states: sum types in Go

**Rule.** When a value is exactly one of N closed variants, express it with a **sealed
interface**. **Why.** Go has no unions or enums with payload. The idiom is an interface with
an **unexported** method: only types in the same package can implement it, so the set is
closed and no outside package adds a variant.

```go
// PaymentMethod is sealed by its unexported method: only this package adds variants.
type PaymentMethod interface {
	isPaymentMethod()
}

// Card pays with a card; only cards carry digits.
type Card struct {
	Last4 string
}

func (Card) isPaymentMethod() {}

// Cash has no payload.
type Cash struct{}

func (Cash) isPaymentMethod() {}

// Describe renders a method for receipts.
func Describe(m PaymentMethod) string {
	switch v := m.(type) {
	case Card:
		return fmt.Sprintf("card ending %s", v.Last4)
	case Cash:
		return "cash"
	default:
		// Unreachable while the set is closed; a panic reveals a programming error.
		panic(fmt.Sprintf("payment: unhandled method %T", m))
	}
}
```

Each variant carries only its own fields (`Card` has `Last4`, `Cash` nothing), which makes
"cash with a card number" unrepresentable. The variants are plain values, so exported fields
are fine; the closed set is what the interface protects.

### Alternative: a `kind` field plus validation

A sealed interface costs interface dispatch, an allocation per variant and verbosity. When
variants share almost all fields, a `kind` discriminator is simpler:

```go
// EventKind discriminates Event; zero is invalid.
type EventKind int

// Event kinds.
const (
	EventUnknown EventKind = iota
	EventClick
	EventScroll
)

// Event is a UI event; X and Y only mean something for EventClick.
type Event struct {
	Kind EventKind
	X, Y int
}
```

A `kind` does not prevent an incoherent `Event{Kind: EventScroll, X: 5}`: the struct exposes
fields that do not apply to every variant, so coherence rests on constructors such as
`NewClick(x, y)` and a `Validate()` check. A sealed interface makes that state
unrepresentable; a `kind` only makes it detectable.

## 7. Goal: if a state must not exist, it should not compile

All of these techniques shrink the set of buildable values towards the set of valid ones,
as early as possible on the compile-time → runtime scale.

| Level | Tool | When the invalid state fails |
| --- | --- | --- |
| Compile time (best) | distinct types, sealed interface | while writing the code |
| Construction | `(T, error)` constructor with unexported fields, options | when building the value |
| Boundary | parse-don't-validate, `Parse*` | when raw input enters |
| Runtime (last resort) | `kind` + `Validate()` | when checked explicitly |

Push every invariant as high as the language allows without fighting it: `Meters`/`Feet`
cost nothing and catch the bug at compile time; a `Validate()` is the last resort. Naming
these types is covered in [domain-naming.md](domain-naming.md); avoiding duplicated
validation in [dry-solid-go.md](dry-solid-go.md).

## Checklist

- [ ] Raw input is **parsed** into a domain type at the boundary, not validated and left circulating.
- [ ] Parsed types keep their fields unexported, so holding one proves it passed the parser.
- [ ] Each invariant lives ONCE, in `NewX(...) (X, error)`; a defined type (`type X float64`) only where every value is valid.
- [ ] `iota` enum: first constant is an explicit `Unknown`/`Invalid`, or a `Parse*` guards the boundary; `String()` via `stringer`.
- [ ] No magic strings as internal state.
- [ ] Distinct units and magnitudes → distinct types; explicit conversion as the only bridge.
- [ ] Many optional settings → functional options; a `WithX` binds co-dependent fields so the invalid combination cannot exist.
- [ ] Mutually exclusive variants → sealed interface (unexported method) + type switch; each variant has only its fields.
- [ ] `kind` + `Validate()` only when a sealed interface is overkill.
