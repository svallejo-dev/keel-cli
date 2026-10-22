# Coupling and Behaviour: Demeter, Tell-Don't-Ask, CQS

Three classic OOP design rules (Law of Demeter, Tell-Don't-Ask, Command–Query Separation)
reinterpreted for Go. They are coupling heuristics, not dogma: when one clashes with `gofmt`,
composable stdlib or "accept interfaces, return concrete types", the design rule bends.

## 1. Law of Demeter: do not reach through other objects

**Rule.** A method talks only to its immediate neighbours: its own fields, its parameters and
what it creates. Not to what a collaborator *returns*.

**Why.** The train wreck `a.B().C().D()` couples you to **the whole intermediate graph**. If
the internal structure of `B` or `C` changes, your code, which does not even mention them,
breaks. You depend on the shape of three types to read one piece of data.

```go
// ZIP is a postal code.
type ZIP string

// Address is where an order ships.
type Address struct{ zip ZIP }

// ZIP returns the postal code.
func (a Address) ZIP() ZIP { return a.zip }

// Customer owns an address.
type Customer struct{ addr Address }

// Address returns the customer's shipping address.
func (c Customer) Address() Address { return c.addr }

// Order belongs to one customer.
type Order struct{ customer Customer }

// Customer exposes the whole customer, which invites callers to navigate it.
func (o Order) Customer() Customer { return o.customer }
```

```go
// BAD: train wreck. Depends on the internals of Order, Customer AND Address.
// Renaming Address.ZIP to Address.PostalCode breaks this distant caller.
func shipBad(o Order) ZIP {
	return o.Customer().Address().ZIP()
}

// ShippingZIP is what callers need; how Order stores it stays private.
func (o Order) ShippingZIP() ZIP { return o.customer.addr.zip }

// GOOD: the caller asks the root for the data it needs.
func shipGood(o Order) ZIP {
	return o.ShippingZIP()
}
```

Two idiomatic ways out of a train wreck:

- **Tell-Don't-Ask** (§3): the root exposes the operation (`o.ShippingZIP()`), not the access path.
- **Pass only the data**: if a function only needs the ZIP, its signature is `f(zip ZIP)`,
  not `f(o Order)`. A minimal parameter reduces coupling without adding methods.

## 2. The trap: LoD is NOT "one dot per line"

Here the canon misleads in Go. Demeter is often misread as "never more than one dot per
expression". Applied literally, that **forbids half the stdlib**. The real rule is narrower:

> Do not depend on the **internal structure** of what **your collaborators** return.

Chaining over values you **own**, over **composable stdlib**, or over a **builder you are
building** violates nothing: you are transforming your own value, not walking a
collaborator's private state.

```go
// None of these violate LoD, and all four are idiomatic.
func normalize(s string) string                       { return strings.ToLower(strings.TrimSpace(s)) }
func copyAll(d io.Writer, s io.Reader) (int64, error) { return io.Copy(d, s) }
func isMissing(err error) bool                        { return errors.Is(err, ErrNotFound) }

func build(parts []string) string {
	var b strings.Builder // you OWN it: writing to it is fluent, not a train wreck
	for _, p := range parts {
		b.WriteString(p)
	}
	return b.String()
}
```

Functional options are the extreme case: you chain mutations on the struct you are
constructing, not on a third party's state.

| Expression | Violates LoD? | Why |
| --- | --- | --- |
| `order.Customer().Address().ZIP()` | **Yes** | Walks the structure of 3 collaborators |
| `strings.ToLower(strings.TrimSpace(s))` | No | Transforms your own value, pure stdlib |
| `io.Copy(dst, src)` | No | Composes small interfaces, no foreign state |
| `errors.Is(err, ErrX)` | No | Semantic question; does not inspect the error's shape |
| `b.WriteString(x)` on your `strings.Builder` | No | The builder is yours |
| `opt(s)` in functional options | No | Configures the object you are building |
| `req.Header.Get("...")` | Borderline | Fine if `Header` is your contract; bad if you walk 3 levels |

Count **crossed ownership graphs**, not dots. One dot that reaches into a collaborator's
interior is the problem; ten dots over your own values are not.

## 3. Tell, Don't Ask: request action, not state

**Rule.** Do not pull an object's state out to decide for it outside. Ask it to act; it knows
and **guards its own invariant**.

**Why.** The *ask* pattern (read state → decide → write state) scatters the object's logic
across all its callers. Each one can forget a check, and the invariant stops being guaranteed
in one place. It is Demeter's **behavioural** sibling: an *ask* usually precedes a train wreck.

```go
// BAD: pull the state out, decide outside, write it back. The invariant
// "balance >= 0" lives in the caller, not in Account; another caller will forget it.
func withdrawBad(a *Account, amt int64) {
	if a.Balance() >= amt {
		a.SetBalance(a.Balance() - amt)
	}
}

// Withdraw fails with ErrInsufficientFunds rather than going negative.
func (a *Account) Withdraw(amt int64) error {
	if amt > a.balance {
		return ErrInsufficientFunds
	}
	a.balance -= amt
	return nil
}
```

The caller goes from a fragile `if` to `if err := acct.Withdraw(amt); err != nil { ... }`.
`error` is the idiomatic channel to signal that the invariant refused the operation.

### The trap

Tell-Don't-Ask does **not** mean "no getters". Go uses them constantly and idiomatically:
serialization, logging, tests that inspect a result. The rule targets **read-decide-write
over state another object should guard**, not reading as such. A getter for immutable data
(`u.Name()`) is fine; a getter used to rebuild, outside, a decision that breaks the object's
invariant is not. Modeling state so the object *cannot* become invalid is the next step:
[illegal-states.md](illegal-states.md).

## 4. Command–Query Separation (CQS)

**Rule.** A method either **mutates** (command) or **queries** (query), never both silently.

- **Command**: changes state and signals the outcome with `error` (or nothing). Imperative name: `Load`, `Withdraw`, `Close`.
- **Query**: returns a value **with no observable effects**. Safe to call N times, reorder or cache. Noun name: `Values`, `Balance`.

**Why.** A query with a hidden side effect cannot be reasoned about (calling it twice gives
different results) or tested in isolation.

```go
// BAD: a "getter" that lazily mutates and returns. A query in the signature,
// a command in the implementation.
type lazyConfig struct {
	mu     sync.Mutex
	loaded bool
	values map[string]string
}

func (c *lazyConfig) Values() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.loaded {
		c.values = loadFromDisk() // a command hidden inside a query
		c.loaded = true
	}
	return c.values
}
```

```go
// Config separates loading (a command) from reading (a query).
type Config struct {
	values map[string]string
}

// Load reads the file; call it once at startup.
func (c *Config) Load() error {
	v, err := readConfigFile()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	c.values = v
	return nil
}

// Values has no side effects; it returns whatever Load stored.
func (c *Config) Values() map[string]string { return c.values }
```

`Load()` is tested by its effect (did it populate `values`? did it return an error?);
`Values()` by its return value, without touching the disk.

### The idiomatic exceptions (name them)

Two Go patterns legitimately combine query and mutation, or query and a second value:

```go
// comma-ok: a PURE query that returns two values (value + presence). Nothing mutates.
func lookup(m map[string]int, k string) (int, bool) {
	v, ok := m[k]
	return v, ok
}

// Pop returns the top and removes it; the effect is the operation's meaning, and the name says so.
func (s *Stack[T]) Pop() (T, bool) {
	var zero T
	if len(s.items) == 0 {
		return zero, false
	}
	top := s.items[len(s.items)-1]
	s.items = s.items[:len(s.items)-1]
	return top, true
}
```

The difference from the `BAD` above: in `Pop` the effect is **in the name and the
signature**; in `lazyConfig.Values` it is **hidden** behind a query name. CQS punishes the
**silent** effect, not the declared one.

## 5. How the three relate

| Rule | Axis | What it attacks |
| --- | --- | --- |
| Law of Demeter | Coupling (structural) | Depending on the **shape** of someone else's graph |
| Tell, Don't Ask | Coupling (behavioural) | Pulling state out to **decide outside** |
| CQS | Contract clarity | Mixing **command and query** silently |

- **Demeter and Tell-Don't-Ask are cousins**: a train wreck (`a.B().C()`) is almost always a disguised *ask*, and the cure is the same: move the behaviour to the root (`a.DoX()`). Fix it with Tell-Don't-Ask and the train wreck disappears for free.
- **CQS is orthogonal**: it is about each method's honesty. A perfectly encapsulated `Withdraw(amt) error` can still violate CQS if it also silently returns and caches the new balance.

All three bow to Go idioms (Rule #0): if applying one forces a speculative interface, hides a
concrete type behind a facade or fights composable stdlib, the rule gives way. To avoid
over-abstracting in SOLID's name, see [dry-solid-go.md](dry-solid-go.md).

## Checklist

- [ ] No train wrecks `a.B().C().D()` walking the structure of collaborators you do not own.
- [ ] The root exposes the operation (`order.ShippingZIP()`), or you pass only the data you need.
- [ ] Chaining over composable stdlib, your own builder or functional options does NOT count as a violation.
- [ ] You count crossed ownership graphs, not dots.
- [ ] Behaviour lives in the object that guards the invariant (`acct.Withdraw`), not in the caller.
- [ ] A refused invariant is signalled with `error`, not with an `if` in the caller.
- [ ] Pure-read getters (serialization, logging, tests) stay legitimate; only foreign read-decide-write is attacked.
- [ ] Each method either mutates (command, returns `error`) or queries (pure), never both silently.
- [ ] Queries are safe to reorder, repeat and cache.
- [ ] The comma-ok and pop exceptions declare their effect in the name and signature.
