# DRY and SOLID in Go: When the OOP Canon Applies and When It Gets in the Way

DRY and SOLID were born alongside inheritance (Smalltalk, Java, C#). Go has none: it uses
implicit interface satisfaction and composition. Transplanting the whole canon without
translation produces Java-isms that **break** in Go: import cycles, interface pollution,
indirection that does not pay for itself. This file separates what applies as-is, what gets
reinterpreted and what gets dropped.

When a principle (DRY, any SOLID letter, getters/setters) clashes with Go's idiom, Go wins.
DRY and SOLID are quoted **with an asterisk**: they are worth it when they reduce real
coupling and get in the way when they invent it.

## 1. DRY, subordinate to decoupling

**Rule.** DRY is not "do not duplicate code that looks alike"; it is **do not duplicate
knowledge**. Two fragments identical today that encode **different decisions** are not
duplication, they are a coincidence. Deduplicating them ties them to change together.

**Why in Go.** The proverb: *"a little copying is better than a little dependency"*.
Extracting what "looks alike" in two unrelated domains into `common/`, `shared/` or `base/`
creates the coupling DRY wanted to avoid: both now depend on a third package, and a change
for one drags the other.

> Would the two copies change for the **same reason**?
> **Yes** → it is the same knowledge; extract it. **No** → keep them apart.

That is SRP (§2) applied to duplication: if the reasons to change differ, unifying is a
mistake even if the code is identical to the character.

```go
// BAD: one shared Validate couples user and payment.
package common

func Validate(name string, amount int) error {
	if name == "" {
		return errors.New("name required")
	}
	if amount < 0 {
		return errors.New("amount must be >= 0")
	}
	return nil
}
```

```go
// GOOD: each domain owns its rule, as a type that cannot hold an invalid value.
package user

// Name is a non-empty display name.
type Name struct{ v string }

// NewName rejects blank names.
func NewName(raw string) (Name, error) {
	if strings.TrimSpace(raw) == "" {
		return Name{}, errors.New("user: name required")
	}
	return Name{v: raw}, nil
}
```

`payment.NewAmount` does the same for amounts in its own package: it shares no line with
`user`, and nothing forces them to change together. Each domain names its rule in its own
vocabulary ([domain-naming.md](domain-naming.md)), and parsing into a type makes later
re-validation unnecessary ([illegal-states.md](illegal-states.md)). `common.Validate`
prevents both.

## 2. Selective SOLID

The five letters do not weigh the same: two are native, two get reinterpreted, one almost
disappears.

| Principle | Applies in Go? | How it shows up |
|---|---|---|
| **S**: SRP | Yes, as-is | A package or type with **one** reason to change |
| **O**: OCP | Reinterpreted | Extension by **composition and interfaces**, never inheritance |
| **L**: LSP | Barely | Without inheritance it reduces to **honouring the interface contract** |
| **I**: ISP | Yes, **native** | Small, **consumer-defined** interfaces; the most Go of all |
| **D**: DIP | Yes | Depend on **consumer-defined abstractions**: accept interfaces + injection |

### S: Single Responsibility

**Rule.** A package or type has **one** reason to change. In Go the natural unit of
responsibility is the **package**, not the class.

```go
// Config is the parsed server configuration.
type Config struct {
	Addr string
}

// ParseConfig reads key=value lines; unknown keys are ignored.
func ParseConfig(r io.Reader) (*Config, error) {
	cfg := &Config{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if ok && strings.TrimSpace(k) == "addr" {
			cfg.Addr = strings.TrimSpace(v)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}
```

If `ParseConfig` started logging, opening connections or applying business rules, it would
have several reasons to change: split them into separate packages.

### O: Open/Closed, reinterpreted

**Rule.** Open to extension, closed to modification, but in Go extension is by
**composition and interfaces**, never by subclasses (there are none).

```go
// Notifier delivers a message on one channel.
type Notifier interface {
	Notify(ctx context.Context, msg string) error
}

// Dispatch stops at the first failing notifier.
func Dispatch(ctx context.Context, ns []Notifier, msg string) error {
	for _, n := range ns {
		if err := n.Notify(ctx, msg); err != nil {
			return fmt.Errorf("dispatch: %w", err)
		}
	}
	return nil
}
```

Adding a channel (`EmailNotifier`, `SlackNotifier`) is a new type with one method;
`Dispatch` does not change. The OOP version would be an `AbstractNotifier` hierarchy.

### L: Liskov Substitution

**Rule.** Without inheritance, LSP has little to apply to. Its Go version: **honour the
interface's contract**. A type satisfying `io.Reader`, `error` or `sort.Interface` must
fulfil its documented contract, not just the method signatures.

```go
// BAD: when exhausted it returns (0, nil) instead of io.EOF.
func (r *badReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, nil // should be io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// GOOD: delivers bytes, then io.EOF.
func (r *goodReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}
```

With `badReader`, `io.Copy` and `bufio.Scanner` spin forever: the signature fits but it does
not behave as an `io.Reader`. The compiler checks the signature; you (and your tests) check
the contract.

### I: Interface Segregation

**Rule.** Small, **consumer-defined** interfaces. It is the most Go of the principles:
`io.Reader` has **one** method by design, and interfaces live where they are **used**, not
where they are implemented.

```go
// WriteReport accepts any io.Writer: a file, a buffer, a socket or a test fake.
func WriteReport(w io.Writer, lines []string) error {
	for _, l := range lines {
		if _, err := fmt.Fprintln(w, l); err != nil {
			return fmt.Errorf("write report: %w", err)
		}
	}
	return nil
}
```

A five-method interface "for completeness" is the opposite: nobody uses all of them.

### D: Dependency Inversion

**Rule.** Depend on abstractions, not concretions, and in Go the abstraction is **defined by
the consumer** and **injected**. It is "accept interfaces" raised to architecture policy.

```go
// userFinder is all ShowProfile needs from storage.
type userFinder interface {
	FindByID(ctx context.Context, id UserID) (*User, error)
}

// ShowProfile renders a user's public profile.
type ShowProfile struct {
	users userFinder
}

// NewShowProfile accepts any store with FindByID; tests pass a stub.
func NewShowProfile(users userFinder) *ShowProfile {
	return &ShowProfile{users: users}
}
```

`ShowProfile` depends on `userFinder`, an abstraction **it** defines, not on a concrete
Postgres store. The concrete `*postgres.Store` satisfies it without importing the consumer:
no cycles, and the test injects a stub. Wiring is covered in
[dependencies.md](dependencies.md).

## 3. The general principle: do not import the whole canon

Importing SOLID/DRY **wholesale**, with their inheritance baggage, pushes towards Java-isms
that are expensive in Go:

| Imported Java-ism | What it breaks in Go |
|---|---|
| Inheritance hierarchies | They do not exist; emulated with embedding they come out fragile |
| Interface per class | Interface pollution: indirection without a second implementation |
| Abstract factories / DI containers | Indirection and reflection where a constructor was enough |
| Getters/setters by default | Noise; use intent-revealing methods |
| `common/` / `base/` utilities | Coupling between unrelated modules (§1) |

The right policy is **selective**:

- **Take as-is** what is already native: ISP and DIP (small, consumer-defined, injected interfaces) and SRP at package level.
- **Reinterpret** OCP (composition, not subclasses) and LSP (interface contract, not nominal subtyping).
- **Drop** whatever needs inheritance or invents coupling: hierarchies, speculative interface per class, deduplicating unrelated knowledge.

Go's default bias is **less abstraction, more explicit code**: a layer of indirection earns
its place when there is a real second implementation or a concrete test double, not before.

## Checklist

- [ ] Before deduplicating: do both copies change for the **same reason**? If not, keep them apart.
- [ ] No `common/`/`shared/`/`base/` packages joining unrelated domains.
- [ ] SRP per package: one reason to change; parsing, logging and business rules separated.
- [ ] OCP via interfaces + composition; adding a behaviour does not modify the dispatcher.
- [ ] LSP = interface contract honoured (`io.Reader` returns `io.EOF`, not `(0, nil)`).
- [ ] ISP: minimal interfaces, defined by the consumer, no interface per class.
- [ ] DIP: the consumer defines and receives the interface; the implementer does not import it.
- [ ] No Java-isms: no inheritance hierarchies, abstract factories or ceremonial getters/setters.
- [ ] Abstraction is added for a second implementation or a real test double, not "just in case".
