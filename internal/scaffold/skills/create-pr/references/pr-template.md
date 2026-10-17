# Plantilla del cuerpo del PR

Cuerpo por defecto cuando el repo **no** trae su propia plantilla. Si existe
`.github/PULL_REQUEST_TEMPLATE.md`, **esa manda**: úsala tal cual y rellénala; ignora esta.

El **título** del PR no va aquí: es Conventional Commits (`type(scope): subject`) y se pasa
con `--title` (ver `SKILL.md`, paso 4). Esta plantilla es solo el `--body-file`.

---

## Plantilla (copiar y rellenar)

```markdown
## Resumen

<!-- 1-2 líneas: qué hace este PR, en cristiano. -->

## Motivación & contexto

<!-- El PORQUÉ. Qué problema resuelve, qué issue lo pide, qué decisión de diseño hubo. -->

## Qué cambió

<!-- Bullets, uno por cambio relevante. Salen de los subjects de los commits. -->
-

## Cómo probar

<!-- Pasos o comandos exactos para que el revisor lo verifique localmente. -->
```bash

```

## Riesgo & rollout

<!-- Blast radius (qué se rompe si esto falla), feature flags, migraciones de datos/BD,
     y el plan de reversa (cómo se revierte). -->
- **Blast radius:**
- **Feature flags:**
- **Migraciones:**
- **Plan de reversa:**

## Screenshots / demos

<!-- Solo si toca UI: antes/después, GIF o captura. Si no aplica: "N/A". -->

## Issues vinculados

<!-- `Closes #N` cierra el issue al mergear; `Refs #N` solo lo referencia. -->
Closes #

## Checklist

- [ ] Tests añadidos/actualizados y en verde
- [ ] Docs/README actualizados si hacía falta
- [ ] Self-review hecho (leí mi propio diff)
- [ ] CI en verde
- [ ] Sin secretos ni credenciales en el diff
```

---

## Cómo autocompletar cada sección

Define primero el rango contra la base (ver `SKILL.md`, pasos 3-5):

```bash
BASE=$(gh repo view --json defaultBranchRef -q .defaultBranchRef.name)
BRANCH=$(git branch --show-current)
```

| Sección | De dónde sale | Comando |
|---|---|---|
| **Resumen** | Síntesis en 1-2 líneas del conjunto de commits | `git log "origin/$BASE..$BRANCH" --pretty=format:'%s'` |
| **Motivación & contexto** | El cuerpo de los commits (el porqué) + el issue vinculado | `git log "origin/$BASE..$BRANCH" --pretty=format:'%b'` |
| **Qué cambió** | Un bullet por subject de commit | `git log "origin/$BASE..$BRANCH" --pretty=format:'- %s'` |
| **Cómo probar** | Comandos reales del repo (tests, build, run) según lo tocado | inferir del diff: `git diff "origin/$BASE...$BRANCH" --stat` |
| **Riesgo & rollout** | Archivos tocados (blast radius), migraciones, flags | `git diff "origin/$BASE...$BRANCH" --stat` y buscar migraciones/flags en el diff |
| **Screenshots / demos** | Solo si el diff toca UI (`.tsx`, `.vue`, `.css`, plantillas) | `git diff "origin/$BASE...$BRANCH" --stat` → si no hay UI, "N/A" |
| **Issues vinculados** | Footers `Closes #N`/`Refs #N` de los commits | `git log "origin/$BASE..$BRANCH" --pretty=format:'%b' \| grep -iE 'closes\|refs #'` |

Recuerda: **`..`** (dos puntos) para el **log** de commits; **`...`** (tres puntos) para el
**diff** contra el merge-base con la base. Reglas del contenido:

- **Resumen** conciso y en presente ("Añade…", "Corrige…"), no un volcado de commits.
- **Cómo probar** con comandos que el revisor pueda copiar-pegar; nada de "probar la app".
- **Riesgo & rollout**: si no hay migraciones/flags, dilo explícito ("Sin migraciones",
  "Sin flags"); el plan de reversa suele ser "revert del merge" salvo que haya migración de
  datos irreversible (entonces detállalo).
- **Nunca** metas secretos, tokens ni `.env` en ninguna sección.

---

## Ejemplo completo (ya rellenado)

Cambio de ejemplo: se añade un endpoint de refresh de tokens porque las sesiones expiraban
en silencio. Dos commits en la rama `feat/auth-refresh`:
`feat(auth): add token refresh endpoint` y `test(auth): cover refresh happy path`.

**Título del PR:** `feat(auth): add token refresh endpoint`

**Cuerpo (`--body-file`):**

```markdown
## Resumen

Añade un endpoint `POST /auth/refresh` para renovar tokens de acceso sin forzar un
re-login completo.

## Motivación & contexto

Las sesiones expiraban en silencio tras 1h y el cliente no tenía forma de renovar el token:
el usuario era expulsado a la pantalla de login a mitad de flujo. Este PR introduce un
refresh token de vida corta y el endpoint que lo canjea por un nuevo access token. Diseño
acordado en el issue #142.

## Qué cambió

- feat(auth): add token refresh endpoint
- test(auth): cover refresh happy path

## Cómo probar

```bash
go test ./internal/auth/...
# Manual, con el server levantado:
curl -sS -X POST localhost:8080/auth/refresh \
  -H 'Authorization: Bearer <refresh-token>' | jq .access_token
```

## Riesgo & rollout

- **Blast radius:** aislado a `internal/auth`; el resto de endpoints no cambia. Si el
  refresh falla, el cliente cae al login actual (comportamiento previo).
- **Feature flags:** ninguno; el endpoint es aditivo y opt-in desde el cliente.
- **Migraciones:** ninguna (los refresh tokens se guardan en la tabla `sessions` existente,
  sin cambio de esquema).
- **Plan de reversa:** revert del merge commit; no hay estado persistido incompatible.

## Screenshots / demos

N/A (cambio de backend, sin UI).

## Issues vinculados

Closes #142

## Checklist

- [x] Tests añadidos/actualizados y en verde
- [x] Docs/README actualizados si hacía falta
- [x] Self-review hecho (leí mi propio diff)
- [x] CI en verde
- [x] Sin secretos ni credenciales en el diff
```
