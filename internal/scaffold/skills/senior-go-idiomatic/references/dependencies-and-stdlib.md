# Dependencies & Stdlib — Go Idiomatic Reference

## Contents

- Stdlib-first and the real cost of a dependency
- Evaluating a dependency; audit tools (`go mod why/graph`, `govulncheck`), MVS
- "A little copying is better than a little dependency"
- Libraries, not frameworks (ServeMux patterns, sqlc over ORMs)
- When to take the dependency; pre-approved dependencies
- Module hygiene: `go.sum`, annotated `go.mod`, the `tool` directive, vendoring, upgrades

---

## Stdlib-First

The standard library is the first answer, not the fallback. `net/http` is a production
server and, since Go 1.22, a router with method and wildcard patterns; `encoding/json`
parses without dependencies; `crypto/*` covers TLS, AEAD and hashing; `log/slog` (Go 1.21)
does structured logging; `slices`, `maps` and `iter` replace most "collection utils"
libraries. Before `go get`, ask: **does the stdlib or the official `golang.org/x/...`
sub-repositories already cover this?**

Two properties make the stdlib structurally cheaper than any dependency:

- **The Go 1 compatibility promise** — code that compiles today keeps compiling on every
  Go 1.x release. A third-party module promises nothing of the kind.
- **No supply-chain cost** — it adds nothing to the module graph, has no CVEs beyond the
  toolchain's own, and does not depend on a maintainer staying active.

Treat every dependency as a liability, not an asset: something you maintain, audit and
patch for as long as you ship. It is justified only when its value exceeds that ongoing
cost.

---

## The Real Cost of a Dependency

One `require` line is not one dependency. It is everything it pulls in:

| Cost | What it means | How you pay |
|---|---|---|
| Transitive deps | Each dep brings its own — a graph, not a node | `go mod graph` grows; slower builds |
| CVE surface | More third-party code, more vulnerabilities that reach you | `govulncheck` reports code you never wrote |
| Someone else's bugs | Their bugs are your production incidents | Debugging code you do not control |
| Maintenance | Is it alive? A single maintainer? | An emergency fork when it is abandoned |
| Build time | Every module is downloaded, verified and compiled | Slower CI, longer feedback loop |
| Breaking changes | Their `v2` breaks your code on their schedule | Unplanned migrations |

The typical story: a 200-line "utils" library turns out, in `go mod graph`, to bring 40
transitive modules, one without a commit in three years. That abandoned module is now
**your** security responsibility.

---

## Evaluating a Dependency Before Adding It

If any answer is "no" or "I don't know", stop and reconsider:

- [ ] **Does the stdlib or `golang.org/x/...` solve it?** If yes, there is no debate.
- [ ] **How many transitive deps does it bring?** Count them with `go mod graph` in a scratch module first.
- [ ] **Is it alive?** Recent releases and commits; issues answered by a maintainer.
- [ ] **Bus factor above one?** A single maintainer is an abandonment risk and a supply-chain risk (one compromised account compromises your build).
- [ ] **Compatible license?** MIT / BSD / Apache-2.0 are safe; GPL/AGPL obligations depend on how you distribute.
- [ ] **Stable API (v1+)?** A `v0.x` module explicitly reserves the right to break compatibility in any release.

```bash
# Probe a candidate in a throwaway module before adding it to the real project.
mkdir -p /tmp/probe && cd /tmp/probe && go mod init probe
go get github.com/some/candidate@latest
go mod graph | wc -l                       # how many transitive edges?
go mod graph | awk '{print $2}' | sort -u  # every module that would enter your build
```

---

## Audit Tools

Only `tidy`, `vendor` and `go get` modify the project; the rest just report.

```bash
go mod why golang.org/x/sync/errgroup   # WHY it is in the graph: the import chain
go mod why -m github.com/foo/bar        # -m reasons at module level, not package level
go mod graph                            # full dependency graph (from -> to)
go mod tidy                             # add what is used, drop what is not; sync go.sum
go mod verify                           # check cached modules were not altered
go list -m all                          # flat list of every module in the build
go list -m -u all                       # same, flagging available upgrades
go mod download                         # fetch the graph into the cache without building
```

`govulncheck` checks your code against the Go vulnerability database and, crucially,
reports only vulnerabilities your code **actually calls** — it analyzes the call graph,
not just the `require` lines:

```bash
go run golang.org/x/vuln/cmd/govulncheck@latest ./...   # or pin it as a tool (see go.mod below)
#   Your code is affected by 0 vulnerabilities.
#   This scan also found 3 vulnerabilities in packages you import ... but your code
#   doesn't appear to call these  ← noise filtered out by reachability analysis
```

Make CI fail when `go.mod`/`go.sum` are out of sync instead of letting the build edit
them silently:

```bash
GOFLAGS=-mod=readonly go build ./...
```

### Minimal Version Selection (MVS)

For each dependency, Go picks the **minimum** version that satisfies every `require` in
the graph — never "the newest available". So the build is a deterministic function of
`go.mod`: the same `go.mod` yields the same graph on any machine at any time, with no
floating ranges re-resolved on each CI run. Reproducibility comes from the algorithm, not
from a lockfile of ranges.

---

## "A little copying is better than a little dependency"

A Go proverb. Pulling in a whole module — transitive deps, CVE surface, maintainer — to
use 30 lines of it is a bad trade. And many of the 30-line helpers people used to import
are now in the stdlib:

```go
// BAD: go get github.com/foo/sliceutil — 40 transitive modules for this
keys := sliceutil.MapKeys(m)

// GOOD: the stdlib already does it (Go 1.23: maps.Keys returns an iterator)
keys := slices.Sorted(maps.Keys(m))     // sorted []K
all := slices.Collect(maps.Keys(m))     // []K in map order
```

When the stdlib does not cover it, copying beats depending if:

- The code is **small and stable** (a closed algorithm, not an evolving domain).
- You can **test it fully** yourself — copy the tests along with the code.
- The dependency would bring **more transitive modules than lines** it saves you.
- It is **not** cryptography, a parser for a standard, or correctness-critical code in a
  domain you do not master (see below).

Credit the source and its license in a comment. Copying is not plagiarism; hiding the
origin is.

---

## Libraries, Not Frameworks

Go favors small, composable libraries over frameworks that **take over control flow**
(inversion of control, reflection magic, a runtime that calls you). You call a library; a
framework calls you and dictates your architecture.

```go
// GOOD: net/http alone. Since Go 1.22 ServeMux matches methods and wildcards.
mux := http.NewServeMux()
mux.HandleFunc("GET /users/{id}", getUser) // POST /users/42 gets a 405 automatically
handler := logging(mux)                    // middleware is just func(http.Handler) http.Handler
http.ListenAndServe(":8080", handler)

func getUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// ...
}
```

A thin router such as `chi` is still a reasonable choice when you need what ServeMux
lacks — route groups with their own middleware stacks, sub-router mounting — because it
keeps handlers as plain `http.Handler`s. Reach for it when that need is real, not by
default.

| Need | GOOD (library) | BAD (framework) |
|---|---|---|
| HTTP routing | `net/http` ServeMux; a thin router only for groups/mounting | A mega-framework that wraps request/response in its own types |
| Data access | `database/sql` / `pgx` + `sqlc` (real SQL, generated code) | A heavy ORM with runtime reflection and opaque queries |
| Configuration | `flag` / environment + explicit structs | A config framework with its own lifecycle and DI container |

An ORM bills you at runtime: reflection on every query, invisible N+1s, and SQL you only
see when `EXPLAIN` surprises you in production. `sqlc` moves all of that to build time —
you write SQL and get type-safe Go with no reflection. Handlers that stay plain
`http.HandlerFunc`s are testable with `httptest` without starting anything.

---

## When to Take the Dependency

Copying and avoiding frameworks is not an anti-dependency dogma. In these categories the
dependency is the right engineering call:

- **Database drivers** — `pgx` speaks the Postgres wire protocol. You do not rewrite it.
- **Crypto you must not write yourself** — argon2, bcrypt, signed JWTs, X.509. A bug of
  yours here is a CVE. Use `golang.org/x/crypto` or audited libraries.
- **Parsers for complex standards** — protobuf, YAML, TOML. Reimplementing the standard
  reintroduces its edge cases.
- **The official sub-repositories** — `golang.org/x/*` is maintained by the Go team with
  stdlib-level rigor, just outside the strict compatibility promise:
  `x/sync/errgroup`, `x/sync/singleflight`, `x/time/rate`.

```go
import (
	"golang.org/x/sync/errgroup"     // goroutines with error propagation + cancellation
	"golang.org/x/sync/singleflight" // collapse identical concurrent calls
	"golang.org/x/time/rate"         // token-bucket rate limiting
)
```

### Pre-approved dependencies used in this skill

Proven, maintained, with a controlled transitive graph. Add them without ceremony.

| Module | Role | Why it is approved |
|---|---|---|
| `github.com/jackc/pgx/v5` | Production Postgres driver | Native pool, COPY protocol, native types |
| `sqlc` (tool) | SQL → type-safe Go | Generates at build time; no runtime reflection |
| `golang.org/x/sync/errgroup` | Goroutines with errors + context | Official sub-repository |
| `github.com/golang-migrate/migrate/v4` | Versioned migrations | De facto standard; embeddable with `go:embed` |
| `github.com/testcontainers/testcontainers-go` | Real Postgres in integration tests | Full fidelity vs. a fake; see `references/testing-advanced.md` |
| `go.uber.org/goleak` | Goroutine leak detection in tests | One assertion in teardown catches a whole class of bugs |

---

## Module Hygiene

### go.sum is the integrity lockfile

`go.mod` declares which versions; `go.sum` pins the **cryptographic hash** of every module
and every transitive `go.mod`. If downloaded content does not match the recorded hash, the
build fails — that is how you catch a module altered in transit or on a compromised
proxy. Commit `go.sum`.

```bash
go mod verify   # does the cache match go.sum? -> "all modules verified"
go mod tidy     # the way to edit require/go.sum; do not hand-edit them
```

### Annotated go.mod

```go.mod
module example.com/billing

go 1.26                // minimum language version the code requires
toolchain go1.26.0     // preferred toolchain; the go command downloads it if missing

tool (
	github.com/sqlc-dev/sqlc/cmd/sqlc
	golang.org/x/tools/cmd/stringer
)

require (
	github.com/jackc/pgx/v5 v5.7.6   // exact version, chosen by MVS
	golang.org/x/sync v0.22.0
)

require (
	github.com/jackc/puddle/v2 v2.2.2 // indirect ← transitive; you do not import it directly
)
```

Versions above are illustrative. `// indirect` marks a module that none of your own
packages import; `go mod tidy` maintains it, so do not edit it by hand.

### Developer tools: the `tool` directive

Since Go 1.24, code generators and linters are tracked in `go.mod` with the `tool`
directive, replacing the old `tools.go` file with blank imports. Their versions are pinned
by the same `require` / `go.sum` machinery as everything else, so every developer and CI
run the same binary.

```bash
go get -tool golang.org/x/tools/cmd/stringer     # adds a tool line + its requirements
go get -tool github.com/sqlc-dev/sqlc/cmd/sqlc
go tool stringer -type=State                     # runs the pinned version
go tool sqlc generate
```

In source, `//go:generate go tool stringer -type=State` uses the pinned version instead of
whatever happens to be on `PATH`. Remove a tool with `go get -tool <pkg>@none`.

### Vendoring — when it pays off

`go mod vendor` copies the whole graph into `./vendor/`. Use it when the build must be
**hermetic**: CI without network access, audits that require the dependency tree in the
repository, or independence from proxy availability. The cost is a large `vendor/` in the
repo and re-running `go mod vendor` after every dependency change.

```bash
go mod vendor                # materialize dependencies in ./vendor
go build -mod=vendor ./...   # build only from vendor, ignoring the cache and the network
```

### Pin and upgrade deliberately

```bash
go get github.com/jackc/pgx/v5@v5.7.6   # pin an exact version
go list -m -u all                       # see which upgrades exist
go get -u ./...                         # minor/patch upgrades, always followed by:
go test -race ./... && govulncheck ./... # nothing merges without this passing
```

Upgrading is a deliberate change with tests behind it, not a blind `-u` before a release.

### GOPROXY / GOSUMDB in one line

`GOPROXY` (default `https://proxy.golang.org,direct`) serves modules; `GOSUMDB` (default
`sum.golang.org`) is the global checksum database that proves no one rewrote an already
published version — together they give cached, tamper-evident downloads.

---

## Checklist

- [ ] The stdlib or `golang.org/x/...` was checked before `go get` (`slices`, `maps`, ServeMux patterns, `slog`)
- [ ] Before adding a dep: transitive count (`go mod graph`), active maintainer, bus factor > 1, compatible license, stable v1+ API
- [ ] `go mod tidy` run; `go.mod` and `go.sum` committed and in sync
- [ ] CI builds with `GOFLAGS=-mod=readonly`
- [ ] `govulncheck ./...` in the pipeline, failing on a reachable vulnerability
- [ ] A few copied and tested lines preferred over a dep with dozens of transitive modules
- [ ] Composable libraries, not frameworks: `net/http` ServeMux, `sqlc` over an ORM
- [ ] Dependencies only in justified categories: drivers, crypto, standard parsers, `x/*`
- [ ] Code generators and linters pinned with the `tool` directive and run via `go tool`
- [ ] Upgrades are deliberate: `go get -u` followed by `go test -race` and `govulncheck`
- [ ] `go.sum` committed; `go mod vendor` only when the build must be hermetic
