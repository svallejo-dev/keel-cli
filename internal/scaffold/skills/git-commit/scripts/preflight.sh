#!/bin/sh
# Read-only snapshot of the git/GitHub context before committing or opening a PR.
# Prints key=value lines. It never changes the repo, never prompts, and reports a
# missing tool or failed lookup as a value instead of exiting non-zero.
#
# Usage: sh preflight.sh [--offline] [--secrets]
#   --offline  skip every gh/network call
#   --secrets  only run the secret scan of staged changes (use it right after staging)

offline=no
secrets_only=no
for arg in "$@"; do
  case $arg in
    --offline) offline=yes ;;
    --secrets) secrets_only=yes ;;
  esac
done

# gh and git must never wait for input: the caller cannot answer a prompt.
GH_PROMPT_DISABLED=1 GH_NO_UPDATE_NOTIFIER=1 GIT_TERMINAL_PROMPT=0 GIT_PAGER=cat PAGER=cat
export GH_PROMPT_DISABLED GH_NO_UPDATE_NOTIFIER GIT_TERMINAL_PROMPT GIT_PAGER PAGER

kv() { printf '%s=%s\n' "$1" "$2"; }
have() { command -v "$1" >/dev/null 2>&1; }
join() { tr '\n' ',' | sed 's/,$//'; }
first_existing() { # prints the first argument that exists, or "none"
  for p in "$@"; do
    if [ -e "$p" ]; then printf '%s\n' "$p"; return; fi
  done
  echo none
}

secret_scan() {
  staged=$(git diff --cached --name-only --diff-filter=ACMR 2>/dev/null)
  if [ -z "$staged" ]; then
    kv secrets_scan "skipped (nothing staged)"
    return
  fi
  if have gitleaks; then
    # gitleaks exits 1 when it finds leaks; --redact keeps values out of the output.
    if gitleaks git --pre-commit --staged --redact --no-banner >/dev/null 2>&1 ||
       gitleaks protect --staged --redact --no-banner >/dev/null 2>&1; then
      kv secrets_scan "gitleaks: clean"
    else
      kv secrets_scan "gitleaks: findings (run: gitleaks git --pre-commit --staged --redact -v)"
    fi
  else
    kv secrets_scan "grep (gitleaks not installed)"
  fi
  # The grep pass always runs: it is cheap and also flags risky file names.
  hits=0
  printf '%s\n' "$staged" | {
    while IFS= read -r f; do
      [ -n "$f" ] || continue
      kind=""
      case $f in
        *.env.example | *.env.sample | *.env.template | *.env.dist) ;;
        .env | .env.* | */.env | */.env.* | *.pem | *.p12 | *.pfx | *.key | *id_rsa | *id_dsa | \
        *id_ecdsa | *id_ed25519 | *credentials.json | *.npmrc | *.pypirc | *.netrc)
          kind="sensitive-filename" ;;
      esac
      added=$(git diff --cached -U0 --no-color -- "$f" 2>/dev/null | sed -n 's/^+//p' | grep -v '^++ ')
      if [ -n "$added" ]; then
        printf '%s\n' "$added" | grep -Eq 'AKIA[0-9A-Z]{16}|ASIA[0-9A-Z]{16}' && kind="$kind aws-key"
        printf '%s\n' "$added" | grep -Eq -- '-----BEGIN [A-Z ]*PRIVATE KEY' && kind="$kind private-key"
        printf '%s\n' "$added" | grep -Eq 'gh[pousr]_[A-Za-z0-9]{36}|github_pat_[A-Za-z0-9_]{20,}' && kind="$kind github-token"
        printf '%s\n' "$added" | grep -Eq 'glpat-[A-Za-z0-9_-]{20}' && kind="$kind gitlab-token"
        printf '%s\n' "$added" | grep -Eq 'xox[abprs]-[A-Za-z0-9-]{10,}' && kind="$kind slack-token"
        printf '%s\n' "$added" | grep -Eq '(sk|rk)_live_[A-Za-z0-9]{20,}' && kind="$kind stripe-key"
        printf '%s\n' "$added" | grep -Eq 'sk-[A-Za-z0-9_-]{20,}' && kind="$kind api-key(sk-)"
        printf '%s\n' "$added" | grep -Eq 'AIza[0-9A-Za-z_-]{35}' && kind="$kind google-api-key"
        printf '%s\n' "$added" | grep -Eiq "(password|passwd|secret|api[_-]?key|access[_-]?token|auth[_-]?token|client[_-]?secret)[\"']?[[:space:]]*[:=][[:space:]]*[\"'][^\"'[:space:]]{8,}[\"']" &&
          kind="$kind hardcoded-credential"
      fi
      if [ -n "$kind" ]; then
        hits=$((hits + 1))
        # File and kind only: the matched value is never printed.
        # shellcheck disable=SC2086 # unquoted on purpose to trim the leading space
        kv secret_hit "$f:$(echo $kind | tr ' ' ',')"
      fi
      size=$(git cat-file -s ":$f" 2>/dev/null || echo 0)
      [ "${size:-0}" -gt 1048576 ] && kv large_staged_file "$f ($size bytes)"
    done
    kv secrets_grep_hits "$hits"
  }
}

if ! have git; then
  kv git_installed no
  kv git_repo unknown
elif ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  kv git_installed yes
  kv git_repo no
else
  kv git_repo yes
  if [ "$secrets_only" = yes ]; then
    secret_scan
    exit 0
  fi
  root=$(git rev-parse --show-toplevel)
  gitdir=$(git rev-parse --git-dir)
  kv repo_root "$root"

  branch=$(git symbolic-ref --quiet --short HEAD 2>/dev/null)
  if [ -n "$branch" ]; then kv branch "$branch"; else kv branch "detached@$(git rev-parse --short HEAD 2>/dev/null)"; fi
  if git rev-parse --verify --quiet HEAD >/dev/null; then kv head "$(git rev-parse --short HEAD)"; else kv head "none (no commits yet)"; fi

  op=none
  [ -d "$gitdir/rebase-merge" ] || [ -d "$gitdir/rebase-apply" ] && op=rebase
  [ -f "$gitdir/MERGE_HEAD" ] && op=merge
  [ -f "$gitdir/CHERRY_PICK_HEAD" ] && op=cherry-pick
  [ -f "$gitdir/REVERT_HEAD" ] && op=revert
  kv operation_in_progress "$op"

  remotes=$(git remote 2>/dev/null)
  kv remotes "$(printf "%s\n" "${remotes:-none}" | join)"
  remote=""
  [ -n "$branch" ] && remote=$(git config "branch.$branch.remote" 2>/dev/null)
  [ "$remote" = "." ] && remote=""
  if [ -z "$remote" ]; then
    if printf '%s\n' "$remotes" | grep -qx origin; then remote=origin; else remote=$(printf '%s\n' "$remotes" | head -n 1); fi
  fi
  kv remote "${remote:-none}"
  push_remote=""
  [ -n "$branch" ] && push_remote=$(git config "branch.$branch.pushRemote" 2>/dev/null)
  [ -z "$push_remote" ] && push_remote=$(git config remote.pushDefault 2>/dev/null)
  kv push_remote "${push_remote:-${remote:-none}}"

  gh_repo=""
  if [ -n "$remote" ]; then
    url=$(git remote get-url "$remote" 2>/dev/null)
    host=$(printf '%s\n' "$url" | sed -E 's#^[a-z+]+://##; s#^[^@/]*@##; s#[:/].*##')
    gh_repo=$(printf '%s\n' "$url" | sed -E 's#^[a-z+]+://[^/]+/##; s#^[^@/]*@[^:]+:##; s#\.git$##; s#/$##')
    kv remote_host "${host:-unknown}"
    kv remote_repo "${gh_repo:-unknown}"
  fi

  default_branch=""
  [ -n "$remote" ] && default_branch=$(git symbolic-ref --quiet --short "refs/remotes/$remote/HEAD" 2>/dev/null | sed "s#^$remote/##")
  if [ -z "$default_branch" ]; then
    for b in main master trunk develop; do
      if git show-ref --verify --quiet "refs/remotes/$remote/$b" || git show-ref --verify --quiet "refs/heads/$b"; then
        default_branch="$b (guessed)"; break
      fi
    done
  fi
  kv default_branch "${default_branch:-unknown}"
  db=${default_branch% (guessed)}
  if [ -n "$branch" ] && [ "$branch" = "$db" ]; then kv on_default_branch yes; else kv on_default_branch no; fi

  upstream=$(git rev-parse --abbrev-ref --symbolic-full-name '@{u}' 2>/dev/null)
  if [ -n "$upstream" ]; then
    kv upstream "$upstream"
    counts=$(git rev-list --left-right --count '@{u}...HEAD' 2>/dev/null)
    kv behind "$(echo "$counts" | awk '{print $1}')"
    kv ahead "$(echo "$counts" | awk '{print $2}')"
  else
    kv upstream none
    if [ -n "$db" ] && git show-ref --verify --quiet "refs/remotes/$remote/$db"; then
      kv ahead_of_default "$(git rev-list --count "$remote/$db..HEAD" 2>/dev/null)"
    fi
  fi
  if [ ! -f "$gitdir/FETCH_HEAD" ]; then kv last_fetch never
  elif [ -n "$(find "$gitdir/FETCH_HEAD" -mmin +60 2>/dev/null)" ]; then kv last_fetch "over 1h ago (counts may be stale)"
  else kv last_fetch "within 1h"
  fi

  porcelain=$(git status --porcelain 2>/dev/null)
  kv staged "$(printf '%s\n' "$porcelain" | grep -c '^[MADRCT]')"
  kv unstaged "$(printf '%s\n' "$porcelain" | grep -c '^.[MDT]')"
  kv untracked "$(printf '%s\n' "$porcelain" | grep -c '^??')"
  kv conflicted "$(printf '%s\n' "$porcelain" | grep -cE '^(UU|AA|DD|AU|UA|DU|UD)')"

  # Style hints: does recent history follow Conventional Commits, and does the default
  # branch receive work through PRs (merge or "(#123)" squash subjects) or direct commits?
  subjects=$(git log --no-merges -n 10 --format=%s 2>/dev/null)
  total=$(printf '%s\n' "$subjects" | grep -c .)
  conv=$(printf '%s\n' "$subjects" | grep -cE '^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\([^)]*\))?!?: ')
  kv history_conventional "$conv/$total"
  if [ "$total" -eq 0 ]; then style=unknown
  elif [ $((conv * 10)) -ge $((total * 8)) ]; then style=conventional
  elif [ "$conv" -eq 0 ]; then style=free-form
  else style=mixed
  fi
  kv history_style "$style"
  ref=""
  if [ -n "$db" ] && git show-ref --verify --quiet "refs/remotes/$remote/$db"; then ref="$remote/$db"
  elif [ -n "$db" ] && git show-ref --verify --quiet "refs/heads/$db"; then ref="$db"
  fi
  if [ -n "$ref" ]; then
    fp=$(git log --first-parent -n 20 --format='%P|%s' "$ref" 2>/dev/null)
    fp_total=$(printf '%s\n' "$fp" | grep -c .)
    via_pr=$(printf '%s\n' "$fp" | grep -cE '^[0-9a-f]+ [0-9a-f]+|\(#[0-9]+\)$')
    if [ "$fp_total" -eq 0 ]; then flow=unknown
    elif [ $((via_pr * 10)) -ge $((fp_total * 7)) ]; then flow=pull-requests
    elif [ "$via_pr" -eq 0 ]; then flow=direct-commits
    else flow=mixed
    fi
    kv default_branch_flow "$flow ($via_pr/$fp_total via PR)"
  fi

  cd "$root" || exit 0
  kv pr_template "$(first_existing .github/pull_request_template.md .github/PULL_REQUEST_TEMPLATE.md \
    pull_request_template.md PULL_REQUEST_TEMPLATE.md docs/pull_request_template.md \
    docs/PULL_REQUEST_TEMPLATE.md .github/PULL_REQUEST_TEMPLATE)"
  lint=$(first_existing commitlint.config.js commitlint.config.cjs commitlint.config.mjs \
    commitlint.config.ts commitlint.config.cts .commitlintrc .commitlintrc.json .commitlintrc.yaml \
    .commitlintrc.yml .commitlintrc.js .commitlintrc.cjs .czrc .cz.toml cz.json .cz.json)
  if [ "$lint" = none ] && [ -f package.json ] && grep -Eq '"(commitlint|commitizen)"' package.json; then lint="package.json"; fi
  kv commit_convention_config "$lint"
  kv commit_template "$(git config commit.template 2>/dev/null || echo none)"
  kv contributing "$(first_existing CONTRIBUTING.md .github/CONTRIBUTING.md docs/CONTRIBUTING.md \
    contributing.md CONTRIBUTING.rst CONTRIBUTING.txt CONTRIBUTING)"
  agents=""
  for f in AGENTS.md CLAUDE.md .claude/CLAUDE.md; do [ -f "$f" ] && agents="$agents,$f"; done
  kv agent_instructions "${agents:+${agents#,}}${agents:-none}"
  hooks=""
  hp=$(git config core.hooksPath 2>/dev/null) && hooks="$hooks,core.hooksPath=$hp"
  [ -d .husky ] && hooks="$hooks,husky"
  [ -f .pre-commit-config.yaml ] && hooks="$hooks,pre-commit"
  for f in lefthook.yml lefthook.yaml .lefthook.yml; do [ -f "$f" ] && hooks="$hooks,lefthook"; done
  for h in pre-commit commit-msg prepare-commit-msg pre-push; do
    [ -x "$gitdir/hooks/$h" ] && hooks="$hooks,.git/hooks/$h"
  done
  kv hooks "${hooks:+${hooks#,}}${hooks:-none}"
  kv gpg_sign "$(git config commit.gpgsign 2>/dev/null || echo unset)"

  secret_scan
fi

# GitHub CLI: installed, authenticated, fork status and real branch protection.
if ! have gh; then
  kv gh_installed no
  exit 0
fi
kv gh_installed yes
if [ "$offline" = yes ]; then
  kv gh_checks "skipped (--offline)"
  exit 0
fi
ghhost=${host:-github.com}
case $ghhost in *github*) ;; *) kv gh_checks "skipped (remote host $ghhost is not GitHub)"; exit 0 ;; esac
GH_HOST=$ghhost
export GH_HOST
if gh auth status --hostname "$ghhost" >/dev/null 2>&1; then
  kv gh_authenticated yes
  kv gh_user "$(gh api --hostname "$ghhost" user --jq .login 2>/dev/null || echo unknown)"
else
  kv gh_authenticated "no (run: gh auth login --hostname $ghhost)"
  exit 0
fi
[ -n "$gh_repo" ] || exit 0

info=$(gh repo view "$gh_repo" --json isFork,parent,defaultBranchRef,viewerPermission \
  --jq '[.isFork, (.parent.owner.login // "") + "/" + (.parent.name // ""), .defaultBranchRef.name, .viewerPermission] | @tsv' 2>/dev/null)
if [ -z "$info" ]; then
  kv gh_repo "unreachable ($gh_repo)"
  exit 0
fi
is_fork=$(printf '%s\n' "$info" | cut -f1)
parent=$(printf '%s\n' "$info" | cut -f2)
kv gh_default_branch "$(printf '%s\n' "$info" | cut -f3)"
kv viewer_permission "$(printf '%s\n' "$info" | cut -f4)"
base_repo=$gh_repo
if [ "$is_fork" = true ]; then
  kv is_fork yes
  kv fork_parent "$parent"
  base_repo=$parent
  parent_remote=""
  for r in $remotes; do
    git remote get-url "$r" 2>/dev/null | grep -q "$parent\(\.git\)\{0,1\}$" && parent_remote=$r
  done
  kv parent_remote "${parent_remote:-none}"
  pb=$(gh repo view "$parent" --json defaultBranchRef --jq .defaultBranchRef.name 2>/dev/null)
  kv base_default_branch "${pb:-unknown}"
else
  kv is_fork no
fi
kv base_repo "$base_repo"

# A branch counts as protected when classic protection is on or a ruleset applies to it.
# The branches endpoint is readable with plain read access (the /protection endpoint
# needs admin and answers 404 "Branch not protected" otherwise).
protection() {
  p=$(gh api "repos/$1/branches/$2" --jq .protected 2>/dev/null) || { echo "unknown (branch not on GitHub or no access)"; return; }
  rules=$(gh api "repos/$1/rules/branches/$2" --jq '[.[].type] | unique | join(",")' 2>/dev/null)
  if [ "$p" = true ] || [ -n "$rules" ]; then echo "yes${rules:+ (rules: $rules)}"; else echo no; fi
}
bdb=${pb:-$(printf '%s\n' "$info" | cut -f3)}
[ -n "$bdb" ] && kv default_branch_protected "$(protection "$base_repo" "$bdb")"
if [ -n "$branch" ] && [ "$branch" != "$bdb" ]; then
  kv current_branch_protected "$(protection "$gh_repo" "$branch")"
  owner=${gh_repo%%/*}
  pr=$(gh pr list --repo "$base_repo" --head "$branch" --state open --json url,headRepositoryOwner \
    --jq "[.[] | select(.headRepositoryOwner.login == \"$owner\")][0].url // \"none\"" 2>/dev/null)
  kv open_pr_for_branch "${pr:-unknown}"
fi
exit 0
