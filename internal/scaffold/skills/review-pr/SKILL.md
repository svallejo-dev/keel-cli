---
name: review-pr
description: >
  Fetch a GitHub pull request with the gh CLI, review it against a universal rubric
  (plus a Go/DDD layer for .go files), and post an actionable, constructive review —
  fully automatic, but never auto-approving. Use when the user says "review this PR",
  "revisa este PR", "code review PR", "review pull request", "review PR #123", or
  "revisión de PR". Triggers on a PR number, URL, or branch handed to you for review.
---

# Review PR (flujo GitHub)

Revisas un pull request de GitHub de punta a punta con `gh`: lo traes, entiendes su
intención y alcance, lo pasas por una rúbrica universal (y una capa Go/DDD para archivos
`.go`), produces hallazgos accionables con severidad, y **posteas** el review. Es una skill
de **acción**: corre `gh` sin pausar. El único freno absoluto es que **nunca auto-aprueba**
—postea `--comment` o `--request-changes`, jamás `--approve`— y nunca filtra secretos.

## Flujo

### 1. Traer el PR y entenderlo

`<n>` puede ser un número, una URL o una rama; sin argumento, `gh` usa el PR de la rama actual.

```bash
# Contexto: intención, archivos tocados, tamaño, ramas, autor
gh pr view <n> --json title,body,files,additions,deletions,headRefName,baseRefName,author

# El diff completo (léelo entero); en PRs grandes, primero solo los nombres
gh pr diff <n>
gh pr diff <n> --name-only

# Estado de CI/checks
gh pr checks <n>
```

Antes de revisar el código, fija tres cosas:
- **Intención**: qué problema resuelve (título + cuerpo + issue vinculado). Un cambio correcto
  que no cumple la intención sigue estando mal.
- **Alcance y blast radius**: qué módulos/consumidores toca; ¿cambia un contrato público?
- **Tamaño y revisabilidad**: mira `additions`/`deletions` y el nº de archivos. Si el PR es
  **demasiado grande** para revisarlo bien o **mezcla concerns** (refactor + feature + fix),
  díselo y **pide un split** — es el feedback de mayor valor y va primero.

### 2. Rúbrica universal

Aplica las dimensiones de `references/review-rubric.md` a todo el diff:
corrección y edge cases · seguridad (injection, authz, secrets, validación de input) ·
tests (¿cubren el cambio y sus bordes?) · manejo de errores · compatibilidad de API/contratos
(breaking changes) · performance y concurrencia (races) · tamaño/revisabilidad (incluye
archivos nombrados por intención y comentarios mínimos) · docs/migraciones · observabilidad.

### 3. Capa Go/DDD (solo archivos `.go`)

Para el subconjunto `.go` del diff, aplica como estándar `senior-go-idiomatic`,
`ddd-go-idiomatic` y —si está instalada— `go-design-rules`.

- Si esas skills están disponibles, **invócalas** para mayor profundidad.
- Si no lo están (p. ej. `go-design-rules` puede no existir aún), usa el **checklist inline
  autocontenido** de `references/go-review-layer.md`. La capa funciona sin las compañeras.
- Corre siempre la **detección rápida** de `references/go-review-layer.md`: archivos cajón
  (`service.go`, `usecase.go`, `manager.go`, `utils.go`…) o dos casos de uso en un archivo =
  **major**; exceso de comentarios narrativos o banners = **minor**.

### 4. Hallazgos

Cada hallazgo lleva: **SEVERIDAD** (blocker / major / minor / nit), `archivo:línea`,
**por qué** es un problema, y un **fix concreto** (no "mejóralo": el cambio exacto).
Ordénalos de más grave a menos. Sé específico, accionable y amable (kind).

### 5. Veredicto

Sintetiza honestamente: `approve` / `comment` / `request-changes`.
- Hay ≥1 **blocker** o **major** → `request-changes`.
- Solo minors/nits o dudas → `comment`.
- Todo correcto → recomiéndalo, pero **la aprobación la da un humano** (paso 6).

### 6. Postear el review

Escribe el resumen (formato en `references/review-rubric.md`) a un archivo y postéalo.

```bash
# Comentario general — NO aprueba
gh pr review <n> --comment --body-file review.md

# Solicitar cambios cuando hay blocker/major
gh pr review <n> --request-changes --body-file review.md
```

Comentarios **inline** por archivo/línea (donde aporten más que en el resumen) vía API REST,
con un payload JSON `{ body, event, comments:[{path,line,side,body}] }`; `{owner}/{repo}` los
resuelve `gh` desde el repo actual:

```bash
cat > review.json <<'JSON'
{
  "body": "Resumen del review. Ver comentarios inline.",
  "event": "COMMENT",
  "comments": [
    { "path": "internal/user/register_user.go", "line": 42, "side": "RIGHT",
      "body": "major: error tragado; envuélvelo con %w: `return fmt.Errorf(\"getUser id=%d: %w\", id, err)`" }
  ]
}
JSON
gh api --method POST repos/{owner}/{repo}/pulls/<n>/reviews --input review.json
```

`event` debe ser `COMMENT` o `REQUEST_CHANGES`. **NUNCA** postees `--approve` ni
`event=APPROVE` de forma automática: la aprobación es una decisión humana.

### 7. Reportar

Devuelve al usuario: veredicto, conteo de hallazgos por severidad, el link del review posteado
y los blockers que faltan por resolver.

## Guardrails

- **Nunca auto-aprueba.** Solo `--comment` o `--request-changes` (API: `event=COMMENT` /
  `REQUEST_CHANGES`). Jamás `--approve`/`event=APPROVE` automático — eso lo decide un humano.
- **Nunca filtres secretos ni credenciales** en los comentarios; si detectas un secreto en el
  diff, márcalo como **blocker** describiendo el patrón, sin reproducir el valor.
- **Específico, accionable y amable.** Cada hallazgo con `archivo:línea`, el porqué y un fix
  concreto. Critica el código, no a la persona.
- **Sé honesto con el veredicto.** No pidas cambios por nits; no dejes pasar un blocker.
- **PR demasiado grande o que mezcla concerns** → el mejor feedback es pedir un split.
- Corre `gh` sin pausar, pero **no hagas `push`, `merge`, `close` ni edites el PR**: la skill
  solo revisa y comenta.
