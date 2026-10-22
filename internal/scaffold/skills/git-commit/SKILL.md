---
name: git-commit
description: >
  Write commit messages and create atomic git commits that follow the repo's own
  conventions (commitlint, CONTRIBUTING, git log style), falling back to Conventional
  Commits. Drafting only, nothing executed: "write a commit message", "suggest a commit
  message", "redacta el mensaje de commit", or a pasted diff. Runs git commit only when
  asked to commit: "commit this", "haz commit", "commitea", "split into atomic commits".
  Stages only the current task's files, leaves other changes alone, never pushes unless
  asked. Without a shell it drafts the text plus the commands.
---

# Git Commit

Turn finished work into commits a reviewer can read and revert one by one, written the
way this particular repo writes them.

## 1. Pick the mode

- **Draft** — the user asked for a message ("write/suggest a commit message", "redacta el
  mensaje"), pasted a diff, or there is no shell. Read what you can (`git diff --staged`,
  `git diff`, `git log`) but change nothing: return the message and, if useful, the exact
  `git add`/`git commit` commands to run. Drafting never stages, commits or branches,
  because the user asked for text, not for a change to their repo.
- **Commit** — the user asked to commit ("commit this", "haz commit", "commitea los
  cambios"). Follow the steps below.

When the request is ambiguous, draft and offer to commit.

## 2. Preflight

Run the bundled read-only script; it prints `key=value` lines and never fails hard:

```bash
sh <this skill's directory>/scripts/preflight.sh --offline
```

`--offline` is all draft mode needs. In commit mode, run it again without `--offline` only
if step 5 needs the branch-protection check.

| Key | Use |
|---|---|
| `git_repo=no` | Switch to draft mode. |
| `operation_in_progress` ≠ `none`, `conflicted` > 0 | Stop and tell the user; committing mid-rebase or mid-merge is their call. |
| `staged` / `unstaged` / `untracked` | Scope, step 3. |
| `history_style`, `commit_convention_config`, `contributing`, `agent_instructions`, `commit_template` | Message convention, step 4. |
| `on_default_branch`, `default_branch_flow` | Branch decision, step 5. |
| `hooks`, `gpg_sign` | Hooks and signing will run; let them. |

## 3. Decide what belongs in the commit

Commit only the changes that belong to the task at hand: the files you edited in this
conversation, or the ones the user named. A working tree often also holds the user's own
unrelated edits, and sweeping them into your commit would publish half-finished work under
the wrong message. So:

- Stage by explicit path (`git add path/a.go path/b.go`). Use `git add -A` or `git add .`
  only when the user asked to commit everything.
- Anything already staged before you started is the user's selection; keep it, and if it
  looks unrelated to the task, say so before committing.
- Leave everything else untouched and list it in the report as "not committed".
- If you cannot tell whether a change is part of the task, ask instead of guessing.

Then split into **atomic commits**, one concern each: refactor apart from behavior change,
mass formatting apart from logic, unrelated fixes apart from each other. Prefer splitting
by whole files. When one file mixes two concerns and the split matters, stage only some of
its hunks without a prompt (`git add -p` and `git add -i` wait for keyboard input you
cannot give); the recipe is in [references/atomic-commits.md](references/atomic-commits.md).

After staging, run the secret scan and read `git diff --staged` once more:

```bash
sh <this skill's directory>/scripts/preflight.sh --secrets
```

A `secret_hit` or `large_staged_file` keeps that file out: unstage it
(`git restore --staged <path>`), tell the user what kind of secret it looked like (never
the value) and suggest `.gitignore` or a secret store.

If the repo has cheap checks that fit its layout and what you touched (formatter, `go build ./...`,
`go vet ./...`, the lint script in `package.json`), run them; a commit that does not build
breaks bisect for everyone. Do not install toolchains the repo does not already use.

## 4. Write the message

The repo's convention wins over this skill's defaults, in this order:

1. Explicit config: commitlint/commitizen config, `commit.template`, CONTRIBUTING,
   AGENTS.md / CLAUDE.md.
2. Recent history: `git log --no-merges -n 15 --format=%s` shows type prefixes, scopes,
   ticket keys (`ABC-123: …`), capitalization, tense and **language**. Write in the
   language the recent commits use; with no history to look at (a pasted diff, a new
   repo), use the language the user is writing in.
3. Fallback when the repo shows no pattern: Conventional Commits, described in
   [references/conventional-commits.md](references/conventional-commits.md).

Whatever the format: a short imperative subject (about 50 columns), a blank line, then a
body explaining *why* when the subject does not make it obvious, wrapped near 72 columns.
Footers go last: issue refs (`Closes #12`), `BREAKING CHANGE:`, and any trailer the
environment asks you to add (for example a `Co-Authored-By:` attribution line from the
harness), kept exactly as given.

Write multi-line messages to a temp file outside the repo, so it can never be staged and
parallel sessions do not overwrite each other:

```bash
msg=$(mktemp "${TMPDIR:-/tmp}/commitmsg.XXXXXX")
cat > "$msg" <<'EOF'
fix(parser): handle empty input without panicking

An empty body reached the tokenizer and indexed a zero-length slice.
EOF
git commit -F "$msg"; rm -f "$msg"
```

## 5. Branch

Commit on the current branch unless there is a reason not to. Being on `main` is not by
itself that reason: many repos commit straight to trunk.

- `on_default_branch=no` → commit where you are.
- On the default branch, run preflight without `--offline` to get
  `default_branch_protected`:
  - `yes` → a push would be rejected anyway. Create a branch with `git switch -c <name>`
    (follow the naming in `git branch -r`, else `type/short-topic`) and tell the user.
  - `no` and `default_branch_flow=direct-commits` → commit on the default branch.
  - `remotes=none` → commit on the current branch; with no remote there is no protection
    or review flow to respect.
  - Anything else (`pull-requests`, `mixed`, `unknown`, no gh, no
    `default_branch_protected` line) → ask whether to commit here or on a new branch, and
    say what you saw.

## 6. Report

Show `git log --oneline -n <commits made>` and list what was left uncommitted on purpose
(other files, skipped hunks, flagged secrets). Push only if the user asked; then push the
current branch to its upstream (`git push`, or `git push -u <remote> <branch>` the first
time). Opening a pull request is the `create-pr` skill's job.

## Guardrails

Hard limits, because their damage is hard to undo:

- **Never commit secrets.** A flagged file stays out until the user confirms it is a false
  positive.
- **Never skip hooks or signing** (`--no-verify`, `--no-gpg-sign`,
  `-c commit.gpgsign=false`). When a hook fails, fix the cause and commit again; the failed
  commit never happened, so do not `--amend` the previous one.
- **Never force-push a shared branch.** On your own feature branch after your own rebase,
  `git push --force-with-lease` is the most that is allowed.
- **Do not rewrite pushed history** (`--amend`, rebase, reset) unless the user asks.
