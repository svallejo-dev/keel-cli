---
name: create-pr
description: >
  Open a GitHub pull request end-to-end, fully automatically. Use this skill whenever the
  user wants to ship a branch for review — triggers include "create a PR", "open a pull
  request", "abre un PR", "haz un PR", "PR for this branch", "raise a pull request", or
  "sube esto a review". Runs git and the gh CLI for real (commit if needed, push, detect
  base branch, craft a Conventional-Commits title, fill the body from commits + diff, and
  run gh pr create). Executes without pausing to ask — subject to non-negotiable safety
  guardrails (no force-push, no auto-merge, no secrets, protected-branch protection).
---

# Create PR Skill

Eres un ingeniero que abre Pull Requests como en un repo serio: rama limpia empujada,
**título en Conventional Commits**, cuerpo autocompletado desde los commits y el diff,
issues vinculados, y todo ejecutado de forma **totalmente automática**. La skill corre
`git` y `gh` de verdad (no describe: hace). No pausa para pedir confirmación paso a paso;
sí respeta guardrails de seguridad innegociables (ver `## Guardrails`). Plataforma: GitHub
vía `gh` CLI. Para la plantilla del cuerpo y cómo rellenar cada sección, lee
`references/pr-template.md`.

## Principio rector

**El PR se explica solo.** Un revisor debería entender qué cambió, por qué, cómo probarlo
y qué riesgo tiene sin leer el código todavía. El título dice *qué* en una línea; el cuerpo
dice *por qué* y *cómo*.

---

## Flujo paso a paso

### 1. Preparar: rama y commits

Nunca abras un PR sobre trabajo a medias. Primero mira el estado:

```bash
git status --porcelain      # ¿hay cambios sin commitear? (vacío = limpio)
git branch --show-current   # ¿en qué rama estás? (importa para el guardrail)
```

**Guardrail de rama (innegociable).** Si estás en `main`/`master` (o una rama protegida),
mueve el trabajo a una feature branch **antes** de seguir. Dos casos:

- Cambios aún **sin commitear** → crea la rama primero; el trabajo nace en ella:
  ```bash
  git switch -c feat/descripcion-corta
  ```
- Commits que ya cayeron sobre `main` local (adelantados respecto al remoto) → móvelos a
  una rama nueva y devuelve `main` a su sitio:
  ```bash
  git switch -c feat/descripcion-corta    # la rama nueva se lleva tus commits
  git branch --force main origin/main     # main local vuelve al remoto
  ```
  (si no hay upstream aún, usa el SHA base en vez de `origin/main`).

**Commitear lo suelto.** Si quedan cambios sin commitear, compromételos en **Conventional
Commits** — `type(scope): subject` imperativo, minúscula, sin punto, ≤50 columnas; cuerpo =
el porqué; footers `BREAKING CHANGE:`, `Closes #N`, `Refs #N`. Para el playbook completo de
commits atómicos usa la skill `git-commit`. En corto:

```bash
git add path/al/archivo ...              # stage selectivo por concern
git commit -m "feat(scope): subject imperativo"
```

No dejes un working tree sucio: el PR debe reflejar exactamente lo commiteado.

### 2. Push de la rama

```bash
git push -u origin "$(git branch --show-current)"
```

`-u` fija el upstream para futuros `push`/`pull`. Si la rama ya existía y divergió tras un
rebase **tuyo**, actualízala con `git push --force-with-lease` (nunca `--force`; ver
Guardrails).

### 3. Determinar la rama base

La base es la rama por defecto del repo, salvo que el trabajo apunte explícitamente a otra
(p. ej. `develop` o una release):

```bash
BASE=$(gh repo view --json defaultBranchRef -q .defaultBranchRef.name)
echo "base=$BASE"
```

### 4. Título del PR (Conventional Commits)

El **título sigue la misma convención que los commits**: `type(scope): subject`.

```bash
BRANCH=$(git branch --show-current)
git log "origin/$BASE..$BRANCH" --pretty=format:'%s'   # revisa los subjects de la rama
```

- **1 commit** → reusa su subject tal cual como título.
- **N commits** → sintetiza un `type(scope): resumen` que capture el conjunto (el `type` es
  el del cambio dominante; el `scope`, el área común). No listes todos los subjects en el
  título; eso va en el cuerpo.

Ejemplos de título válido: `feat(auth): add token refresh endpoint`,
`fix(parser): handle empty input without panicking`, `refactor(store): extract query builder`.

### 5. Cuerpo del PR

Elige la plantilla en este orden:

1. **`.github/PULL_REQUEST_TEMPLATE.md` del repo si existe** — esa manda (respétala al pie):
   ```bash
   ls .github/PULL_REQUEST_TEMPLATE.md .github/pull_request_template.md 2>/dev/null
   ```
2. Si no existe, usa la plantilla de `references/pr-template.md`.

Autocompleta **cada** sección desde los commits y el diff de la rama contra la base:

```bash
git log  "origin/$BASE..$BRANCH" --pretty=format:'- %s'   # → "Qué cambió" (bullets)
git diff "origin/$BASE...$BRANCH" --stat                  # → alcance / blast radius
git diff "origin/$BASE...$BRANCH"                         # → detalle para "Cómo probar" y riesgo
```

Ojo con los rangos: `..` (dos puntos) para el **log** de commits de la rama; `...` (tres
puntos) para el **diff** contra el punto de divergencia (merge-base) con la base. Vincula
issues con `Closes #N` (cierra al mergear) o `Refs #N` (solo referencia) cuando aplique.
Escribe el cuerpo en un archivo:

```bash
cat > /tmp/pr-body.md <<'EOF'
## Resumen
...
EOF
```

### 6. Crear el PR

```bash
gh pr create \
  --base "$BASE" \
  --head "$BRANCH" \
  --title "feat(scope): subject imperativo" \
  --body-file /tmp/pr-body.md
```

- **Ready por defecto.** Añade `--draft` **solo** si los checks de CI están claramente en
  rojo o el trabajo aún está incompleto (WIP).
- Si el repo usa `.github/PULL_REQUEST_TEMPLATE.md`, ya lo incorporaste en el body-file; no
  hace falta `--template`.
- **Nunca** `gh pr merge` aquí: la skill abre el PR, no lo mergea (ver Guardrails).

### 7. Reportar

`gh pr create` imprime la URL del PR en stdout al terminar. Repórtala al usuario. Si la
necesitas después:

```bash
gh pr view --json url -q .url
```

---

## Guardrails

Innegociables. No son "preguntar antes": son "no hacer estupideces".

1. **Rama protegida → feature branch primero.** Si estás en `main`/`master` (o rama
   protegida), mueve el trabajo a una rama nueva **antes** de push/PR (paso 1). Nunca abras
   un PR desde la rama protegida ni empujes commits directos a ella.
2. **Nunca `git push --force`.** Como mucho `git push --force-with-lease` sobre **tu propia**
   rama y solo si es imprescindible (p. ej. tras un rebase de tu feature). Jamás force-push
   a ramas compartidas/protegidas.
3. **Nunca auto-merge.** No ejecutes `gh pr merge` (ni `--auto`). La skill deja el PR abierto
   para revisión humana; mergear es decisión de otra persona.
4. **Nunca secretos en el cuerpo del PR.** No pegues `.env`, claves privadas, tokens,
   credenciales ni cadenas tipo `AKIA…`/`sk-…` en el título ni en el body. Si un diff los
   expone, avisa al usuario en vez de publicarlos.
5. **Respeta la plantilla y los CODEOWNERS del repo.** Si existe
   `.github/PULL_REQUEST_TEMPLATE.md`, esa plantilla manda. Si existe `CODEOWNERS`, deja que
   GitHub asigne los revisores automáticamente; no fuerces reviewers que contradigan esa
   configuración.
6. **No mientas en el estado.** No marques Ready un PR con CI claramente roja ni Draft uno
   terminado. `--draft` refleja realidad, no comodidad.

## Checklist

Antes de dar por cerrado el trabajo, verifica:

- [ ] El working tree quedó limpio; todo lo que va al PR está commiteado.
- [ ] No estás en `main`/`master`; si lo estabas, moviste el trabajo a una feature branch.
- [ ] La rama está empujada con upstream (`git push -u origin <branch>`); sin `--force`.
- [ ] La base es la correcta (`defaultBranchRef` u otra deliberada).
- [ ] El **título** es Conventional Commit válido: `type(scope): subject` imperativo ≤50.
- [ ] El cuerpo usa la plantilla del repo si existe; si no, la de `references/pr-template.md`.
- [ ] Cada sección del cuerpo está rellenada desde `git log`/`git diff`; issues vinculados
      con `Closes #N`/`Refs #N` donde aplique.
- [ ] Ningún secreto en título ni cuerpo.
- [ ] Ready por defecto; `--draft` solo si CI en rojo o WIP. No se ejecutó `gh pr merge`.
- [ ] Reportaste la URL del PR.

---

## Reference Files

- `references/pr-template.md` — la plantilla del cuerpo del PR (Resumen, Motivación &
  contexto, Qué cambió, Cómo probar, Riesgo & rollout, Screenshots/demos, Issues vinculados,
  Checklist), un ejemplo completo ya rellenado, y cómo autocompletar cada sección desde los
  commits y el diff. Nota: si el repo trae su propia `.github/PULL_REQUEST_TEMPLATE.md`, esa
  manda sobre esta.
