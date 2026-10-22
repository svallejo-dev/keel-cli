---
name: create-pr
description: >
  Draft or open a GitHub pull request for the current branch, following the repo's PR
  template, title style and language (falls back to a Conventional Commits title and a
  built-in template). Drafting only, nothing pushed: "write a PR description", "draft
  the PR", "redacta la descripción del PR". Pushes the branch and runs gh pr create
  only when asked to open one: "open a PR", "create a pull request", "abre un PR", "haz
  un PR", "sube esto a review". Commits only the task's files, handles forks, never
  merges. Without a shell or gh it drafts the text plus the commands.
---

# Create PR

A pull request should explain itself: a reviewer understands what changed, why, how to
verify it and what could break before reading any code. The title says *what* in one line;
the body says *why* and *how*, in the shape and language this repo already uses.

## 1. Pick the mode

- **Draft** — "write/draft the PR description", "redacta el PR", a pasted diff, or no
  shell/`gh`. Gather context read-only (`git log`, `git diff`) and return the title and
  body, plus the commands that would open it. Nothing is committed, pushed or created,
  because the user asked for text.
- **Open** — "open/create a PR", "abre un PR", "sube esto a review". Follow the steps
  below. Pause and ask only when something is genuinely unclear: which uncommitted changes
  belong to the PR, which base to target, or which remote to push to.

## 2. Preflight

```bash
sh <this skill's directory>/scripts/preflight.sh
```

It is read-only and prints `key=value` lines. The ones that matter here:

| Key | Use |
|---|---|
| `gh_installed=no`, `gh_authenticated=no` | Switch to draft mode and give the `gh auth login` / `gh pr create` commands. |
| `remote`, `push_remote`, `is_fork`, `fork_parent`, `parent_remote`, `base_repo` | Where to push and where to open the PR (step 5). The remote is not always `origin`. |
| `gh_default_branch` / `base_default_branch` | Default base. |
| `on_default_branch`, `ahead`, `staged`, `unstaged`, `untracked` | Branch and commit state (step 3). |
| `open_pr_for_branch` | A PR already exists: update it instead of opening a duplicate. |
| `pr_template`, `contributing`, `commit_convention_config`, `history_style` | Title and body conventions (step 6). |
| `operation_in_progress`, `conflicted` | Stop and tell the user. |

## 3. Get the commits in place

**Base.** The user's explicit target wins; otherwise the default branch of `base_repo`.
Fetch it before comparing, so the log and diff reflect the real remote state (a fetch only
updates remote-tracking refs):

```bash
git fetch "$BASE_REMOTE" "$BASE"     # BASE_REMOTE: parent_remote for forks, else remote
BASE_REF="$BASE_REMOTE/$BASE"
```

For a fork with no remote pointing at the parent, fetch by URL and use the fetched commit:
`git fetch https://github.com/<fork_parent>.git "$BASE"` then `BASE_REF=FETCH_HEAD`.

**Branch.** A PR needs a head branch different from the base. If you are on the default
branch, create one with `git switch -c <name>`; your local commits move with it. Name it
like the repo's existing branches (`git branch -r`), else `type/short-topic`. Leave the
local default branch where it is and mention that it is ahead of the remote; resetting it
is the user's decision, since a reset there can discard work.

**Uncommitted changes.** Commit only the files that belong to this task, using the
`git-commit` skill when available (same rules: stage by path, repo message convention,
secret scan, no `--no-verify`). Leave the user's unrelated changes untouched and list them
in the report. If you cannot tell which changes belong, ask.

## 4. Push

```bash
git push -u "$PUSH_REMOTE" "$BRANCH"
```

`PUSH_REMOTE` is `push_remote` from preflight (the user's fork, for fork workflows). If
the branch already exists remotely and diverged after **your own** rebase, use
`git push --force-with-lease`; never plain `--force`.

## 5. Open (or update) the PR

If `open_pr_for_branch` shows a URL, the push already updated it; report the URL and edit
title/body (`gh pr edit`) only if the user asked.

Otherwise:

```bash
body=$(mktemp "${TMPDIR:-/tmp}/pr-body.XXXXXX")
# write the body (step 6) into "$body"
gh pr create --repo "$BASE_REPO" --base "$BASE" --head "$HEAD" \
  --title "$TITLE" --body-file "$body"
rm -f "$body"
```

- `HEAD` is `<branch>` for a same-repo PR and `<fork-owner>:<branch>` for a fork.
- Add `--draft` when the user said WIP/draft or you know the work is incomplete or a check
  is failing. Otherwise open it ready for review.
- Add reviewers, labels or assignees only when the user asks; CODEOWNERS assigns reviewers
  on its own.

## 6. Title and body

Repo conventions win:

1. **Template**: `pr_template` from preflight (if it is a directory, pick the template that
   fits or ask). Keep its headings and checkboxes and fill every section.
2. **Recent PRs** show title style, structure and language:
   `gh pr list --repo "$BASE_REPO" --state merged --limit 10 --json title,body`.
   If the default branch history ends subjects with `(#123)`, PRs are squash-merged and the
   title becomes the commit subject, so it must follow the commit convention
   (commitlint, CONTRIBUTING, `history_style`).
3. **Fallback**: a Conventional Commits title and the template in
   [references/pr-template.md](references/pr-template.md), which also explains how to fill
   each section from `git log "$BASE_REF"..HEAD` and `git diff "$BASE_REF"...HEAD`.

Write in the language the repo's PRs and commits use. Title: one commit → reuse its
subject; several → one line summarizing the dominant change. Link issues with `Closes #N`
or `Refs #N`. If the environment asks for an attribution line at the end of PR
descriptions, include it as given. Never paste secrets, tokens or `.env` contents into the
title or body.

## 7. Report

The PR URL, base ← head, draft or ready, and anything left out on purpose (uncommitted
files, the local default branch now ahead of its remote).

## Guardrails

Hard limits, because their effects reach other people:

- **Never merge** — no `gh pr merge`, no `--auto`. Merging is a human decision.
- **Never force-push a shared branch**; `--force-with-lease` only on your own branch.
- **Never commit or publish secrets**, in commits or in the PR text.
- **Never skip hooks or signing** (`--no-verify`, `--no-gpg-sign`).
