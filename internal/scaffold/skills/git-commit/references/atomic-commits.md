# From a messy working tree to atomic commits

Read this when the changes to commit span several concerns, or one file mixes two of them.

## 1. Inventory

```bash
git status --short
git diff            # unstaged
git diff --staged   # already staged (by you or the user)
```

Label each file, and each hunk where a file mixes things, with its concern and with
whether it belongs to the current task at all. Example:

```
auth/token.go        -> task: token refresh feature
auth/token_test.go   -> task: tests for that feature
auth/middleware.go   -> mixed: 1 hunk of the refresh + 1 hunk renaming a variable
store/query.go       -> task: unrelated refactor done along the way
README.md            -> task: document the new env var
Taskfile.yml         -> NOT the task: the user was editing it before; leave it alone
config/.env          -> secret: never staged
```

## 2. Decide the cuts

One commit per concern, in an order where each commit builds on its own:

1. `refactor(store): extract query builder` — `store/query.go`
2. `refactor(auth): rename session variable` — the rename hunk of `middleware.go`
3. `feat(auth): add token refresh endpoint` — `token.go`, `token_test.go`, the refresh hunk
4. `docs(readme): document AUTH_TTL` — `README.md`

Refactors that nothing depends on go first, then the behavior change, then docs.
`Taskfile.yml` and `config/.env` appear in no commit; the report mentions both.

Tests that prove a fix belong in the same commit as the fix.

## 3. Stage whole files when you can

```bash
git add store/query.go
git commit -F "$msg"
git restore --staged path/to/file   # undo a stage that slipped in
```

## 4. Stage part of a file without a prompt

`git add -p` waits for keyboard answers, which an agent cannot give. Build a patch with only
the wanted hunks and apply it to the index instead. The working tree is not touched, so the
remaining hunks stay as unstaged changes for the next commit.

```bash
tmp=$(mktemp -d "${TMPDIR:-/tmp}/hunks.XXXXXX")       # outside the repo
git diff -U0 -- auth/middleware.go > "$tmp/all.patch" # one hunk per contiguous change
cat "$tmp/all.patch"                                  # number the @@ hunks: 1, 2, ...

# keep the header and hunks 1 and 3 (drop hunk 2)
awk '/^@@/{n++} n==0 || n==1 || n==3' "$tmp/all.patch" > "$tmp/want.patch"

git apply --cached --unidiff-zero --check "$tmp/want.patch" &&
  git apply --cached --unidiff-zero "$tmp/want.patch"
git diff --staged -- auth/middleware.go               # verify what is staged
rm -rf "$tmp"
```

If two concerns sit inside the same contiguous hunk, split that hunk by hand in
`want.patch`: delete the `+` lines you do not want, and turn the `-` lines you do not want
into context by replacing the leading `-` with a space; then apply with
`git apply --cached --recount`. When that gets fiddly, committing the file whole and saying
so in the body is a reasonable trade-off.

## 5. Before each commit

```bash
git diff --staged --stat
sh <skill dir>/scripts/preflight.sh --secrets
```

Each commit should tell one story that someone could revert on its own without breaking
the build.
