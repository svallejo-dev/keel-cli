# Go/DDD review layer

Applies only to the `.go` files of the PR, on top of the universal rubric.

## Who owns which rule

Each rule has one owner skill; this layer only points at them. When the owner is available,
use it for the full rule and its rationale.

| Area | Owner |
|---|---|
| Bounded-context layout, one use case per file, use-case shape, ports, infra placement | `ddd-go-idiomatic` |
| Design principles: Law of Demeter, Tell-Don't-Ask, CQS, named types, illegal states, explicit dependencies | `go-design-rules` |
| Comment policy, idioms, errors, concurrency, I/O, testing, tooling | `senior-go-idiomatic` |

If a principle from DDD or OOP conflicts with Go idioms, Go idioms win: an interface per
struct, a `mapper/` package, getters/setters or layer folders are findings even when they
"follow DDD".

## Step 1: does the repo adopt the house rules?

The layout and comment rules are one team's conventions, not universal Go. Check the PR
head (not the local checkout) for adoption, after fetching `pull/<n>/head` as in SKILL.md:

```bash
SHA=<headRefOid>
git cat-file -e "$SHA:.claude/skills/ddd-go-idiomatic/SKILL.md" 2>/dev/null && echo adopted
for f in AGENTS.md CLAUDE.md .claude/CLAUDE.md; do
  git show "$SHA:$f" 2>/dev/null | grep -Eiq 'ddd-go-idiomatic|one use case per file|un caso de uso por archivo' && echo "adopted via $f"
done
```

(Without a local clone, read the same paths with
`gh api "repos/<owner>/<repo>/contents/<path>?ref=$SHA"`; a 404 means absent.)

- **Adopted** → layout violations are **major** and comment-policy violations **minor**,
  citing the owner skill.
- **Not adopted** → report them as **minor** or **nit** "house-style suggestions", grouped
  together, and never let them drive a request-changes verdict. Follow the patterns the
  repo already uses instead of imposing new ones.

## Step 2: quick detection

File names come from the PR, contents from the PR head:

```bash
files=$(gh pr diff <n> --name-only | grep '\.go$')
# grab-bag file names
printf '%s\n' "$files" | grep -E '(^|/)(service|services|usecase|usecases|use_cases|application|app|manager|helpers|utils|common)\.go$'
# grab-bag types added
gh pr diff <n> | grep -E '^\+type ([A-Za-z]*(Service|UseCase|Manager)) struct'
# several exported ctx methods on one receiver (more than one use case per file?)
for f in $(printf '%s\n' "$files" | grep -v '_test\.go$'); do
  c=$(git show "$SHA:$f" 2>/dev/null | grep -cE '^func \([a-z]+ \*?[A-Z][A-Za-z0-9_]*\) [A-Z][A-Za-z0-9_]*\(ctx context\.Context')
  [ "${c:-0}" -gt 1 ] && echo "$f: $c exported ctx methods"
done
# comment banners added
gh pr diff <n> | grep -nE '^\+[[:space:]]*// ?[-=]{4,}'
```

Each hit is a candidate: confirm by reading the file. Adapters (stores, repositories) legitimately
implement several port methods; a composition root (`cmd/<app>/main.go`,
`internal/handlers/handlers.go`) legitimately wires many routes.

## Step 3: Go correctness (every repo)

These are general Go bugs, rated by impact like any rubric finding (owner:
`senior-go-idiomatic`):

- Errors swallowed, compared with `==` instead of `errors.Is/As`, or returned without
  context where the caller cannot tell what failed.
- `context.Context` not first or not propagated; `context.Background()` mid-chain.
- Goroutines without a clear end (no ctx, done channel or WaitGroup); `time.After` in loops.
- Data races and concurrent map writes; a `sync.Mutex` copied by value.
- Typed nil returned as `error`; `append` aliasing a shared backing array; mutating the
  `range` copy instead of `xs[i]`.
- Interfaces defined next to the implementation with a single implementation and no seam.
- Package-level mutable state, `init()` with side effects, hidden singletons.

## Step 4: domain modelling (when the PR touches a domain package)

Owner: `ddd-go-idiomatic`. Look for value objects that can be built without validation,
entities with setters instead of intention-revealing methods, aggregates referenced by
pointer instead of ID, domain packages importing `database/sql`, `net/http` or
`encoding/json`, and driver errors (`sql.ErrNoRows`) leaking past the repository. Rate
them by impact; a leaked infrastructure import in a repo that adopted the house rules is
major.
