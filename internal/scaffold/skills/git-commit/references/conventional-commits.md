# Conventional Commits — referencia

Formato base:

```
type(scope): subject

cuerpo opcional: el PORQUÉ y el contexto, envuelto a ~72 columnas

Footer(s): BREAKING CHANGE: … / Closes #N / Refs #N / Co-Authored-By: …
```

`scope` es opcional; `!` antes de los dos puntos marca un breaking change.

---

## Tabla de types

| type | Cuándo usarlo | Ejemplo de subject |
|---|---|---|
| `feat` | Nueva funcionalidad visible para el usuario/API. | `feat(auth): add token refresh endpoint` |
| `fix` | Corrección de un bug de comportamiento. | `fix(parser): handle empty input without panicking` |
| `docs` | Solo documentación (README, comentarios, godoc, guías). | `docs(readme): document env vars` |
| `style` | Formato/estética sin cambio de lógica (espacios, comas, `gofmt`, imports ordenados). | `style: run gofmt on the pkg` |
| `refactor` | Reestructura código sin cambiar comportamiento ni añadir features (extraer, renombrar, mover). | `refactor(store): extract query builder` |
| `perf` | Cambio cuyo objetivo es mejorar rendimiento. | `perf(cache): avoid re-allocating buffer per call` |
| `test` | Añadir o corregir tests; nada de código de producción. | `test(auth): cover expired-token path` |
| `build` | Sistema de build o dependencias (go.mod, Makefile, Dockerfile, package.json deps). | `build(deps): bump go to 1.24` |
| `ci` | Configuración de CI/CD (workflows de GitHub Actions, pipelines). | `ci: cache go modules in test job` |
| `chore` | Tareas de mantenimiento que no encajan arriba y no tocan src/tests (config repo, .gitignore, versionado). | `chore: add .env to gitignore` |
| `revert` | Revertir un commit anterior. El body cita el hash revertido. | `revert: feat(auth): add token refresh endpoint` |

Reglas de desempate frecuentes:

- ¿Cambia lo que el usuario/API puede hacer? → `feat` o `fix`. Si no cambia comportamiento
  → `refactor`, `style`, `docs`, `test`, `build`, `ci` o `chore`.
- `perf` gana a `refactor` cuando la **intención** es rendimiento (aunque también reestructure).
- Tocar tests **de un fix** puede ir dentro del mismo `fix`; tests aislados → `test`.
- Actualizar dependencias es `build(deps)`, no `chore`, cuando afecta al build.

---

## Reglas de subject

- **Imperativo, tiempo presente**: "add", "fix", "remove" — no "added"/"adds"/"adding".
- **Minúscula inicial** tras los dos puntos.
- **Sin punto final.**
- **≤50 columnas** (guía dura; el cuerpo va aparte).
- Describe **qué hace el commit**, no cómo lo implementa.

```
GOOD: feat(user): validate email on signup
BAD:  feat(user): Added email validation.     # pasado, mayúscula, punto final
BAD:  fix: stuff                               # vago, no dice qué
BAD:  feat: implement the new user registration flow with validation  # >50 col
```

## Reglas de body

- Va **tras una línea en blanco** que lo separa del subject (obligatoria si hay body).
- Explica el **PORQUÉ** y el contexto: qué problema resolvía, qué alternativa se descartó,
  qué efecto tiene. No parafrasees el diff línea por línea.
- Envuelve a **~72 columnas**. Puede tener varios párrafos y listas con `-`.

```
GOOD:
  fix(session): expire idle tokens after 30m

  Tokens lived until server restart, so a stolen token stayed valid
  indefinitely. We now stamp an expiry and reject expired tokens at
  the middleware boundary.

BAD:
  fix(session): expire idle tokens

  Changed token.go and middleware.go and added a field and an if.   # repite el diff
```

## Reglas de footer

- Van al final, tras una línea en blanco, uno por línea, con formato `Token: valor`.
- Cierre de issues de GitHub: `Closes #142`, `Fixes #142` (cierran al mergear a la rama
  por defecto). Referencia sin cerrar: `Refs #142`.
- Coautoría: `Co-Authored-By: Nombre <email@dominio>`.
- Breaking: `BREAKING CHANGE: <descripción del cambio incompatible>`.

```
GOOD:
  Closes #142
  Co-Authored-By: Ana Ruiz <ana@example.com>

BAD:
  closes: #142 and also refs 143     # token mal formado, varios en una línea
```

---

## Breaking changes

Dos formas, combinables:

1. **`!`** antes de los dos puntos, para señalarlo en el subject:
   ```
   feat(api)!: remove deprecated /v1/login endpoint
   ```
2. **Footer `BREAKING CHANGE:`** con la explicación (recomendado para dar contexto y para
   herramientas de versionado que lo detectan):
   ```
   feat(api)!: remove deprecated /v1/login endpoint

   The v1 auth flow is gone. Clients must migrate to /v2/session, which
   returns a refresh token instead of a long-lived session cookie.

   BREAKING CHANGE: /v1/login no longer exists; use /v2/session.
   ```

Un breaking change dispara un bump de versión **major** en SemVer, aunque el type sea `fix`.

---

## Playbook: de un working tree desordenado a commits atómicos

Situación típica: tocaste varias cosas a la vez y todo está mezclado. Objetivo: una serie
de commits donde cada uno cuenta una sola historia.

### 1. Inventario

```bash
git status
git diff            # sin stage
git diff --staged   # ya en el índice
```

Anota cada archivo/hunk y etiquétalo con su **concern**. Ejemplo real de un working tree:

```
auth/token.go        -> feature: refresh de token
auth/token_test.go   -> tests de esa feature
auth/middleware.go   -> mezcla: 1 hunk del refresh + 1 hunk que renombra una var (refactor)
store/query.go       -> refactor no relacionado: extraer queryBuilder
README.md            -> docs: documentar la env var nueva
.gitignore           -> chore: ignorar .env
config/.env          -> ¡SECRETO! no se commitea
```

### 2. Decide los cortes (por concern)

Del inventario salen commits independientes:

1. `refactor(store): extract query builder` — `store/query.go`.
2. `feat(auth): add token refresh endpoint` — `auth/token.go`, `auth/token_test.go` y el
   **hunk** del refresh en `middleware.go`.
3. `refactor(auth): rename session var for clarity` — el **otro hunk** de `middleware.go`.
4. `docs(readme): document AUTH_TTL env var` — `README.md`.
5. `chore: ignore local .env files` — `.gitignore`.

`config/.env` **no entra en ningún commit** (guardrail de secretos): va al `.gitignore`.

Orden lógico: primero los refactors que no dependen de nada, luego la feature que se apoya
en ellos, luego docs/chore.

### 3. Staging selectivo, commit por commit

```bash
# Commit 1 — refactor aislado
git add store/query.go
git commit -m "refactor(store): extract query builder" \
           -m "Splits a 200-line method so the builder can be unit-tested; no behavior change."

# Commit 2 — feature (archivos completos + un hunk concreto de middleware.go)
git add auth/token.go auth/token_test.go
git add -p auth/middleware.go        # marca 'y' solo en el hunk del refresh, 'n' en el rename
git commit -F /tmp/commitmsg.txt     # mensaje con body + Closes #N

# Commit 3 — el refactor que quedó pendiente en middleware.go
git add auth/middleware.go
git commit -m "refactor(auth): rename session var for clarity"

# Commit 4 y 5 — docs y chore
git add README.md
git commit -m "docs(readme): document AUTH_TTL env var"
git add .gitignore
git commit -m "chore: ignore local .env files"
```

Herramientas clave para separar concerns dentro de un archivo:

```bash
git add -p <archivo>            # elige hunk por hunk (y/n; s parte un hunk grande; q sale)
git restore --staged <archivo> # deshace un stage que se coló
git diff --staged              # revisa el índice antes de cada commit
```

### 4. Separar refactor de feature (regla de oro)

Si el mismo archivo tiene un cambio de comportamiento **y** un movimiento/renombrado, van
en commits distintos: usa `git add -p` para stagear solo los hunks de la feature, commitea,
luego stagea el resto para el commit de refactor. Un revisor debería poder leer el commit
de feature sin que el ruido del refactor lo tape.

---

## Ejemplos completos

**feat con scope y body (cierra issue):**

```
feat(auth): add token refresh endpoint

Sessions expired silently after one hour, forcing users through a full
re-login. This adds POST /session/refresh so the client can renew an
access token using its refresh token, transparently to the user.

Closes #142
```

**fix con Closes:**

```
fix(parser): handle empty input without panicking

An empty request body reached the tokenizer and indexed into a
zero-length slice. We now short-circuit on empty input and return a
typed ErrEmptyInput instead of panicking.

Closes #207
```

**refactor (sin cambio de comportamiento):**

```
refactor(store): extract query builder

Splits Repository.Find, a 200-line method that inlined SQL assembly,
into a dedicated queryBuilder. Pure move + rename; behavior and the
generated SQL are unchanged, but the builder is now unit-testable.
```

**breaking change (`!` + footer):**

```
feat(api)!: return refresh token from /session instead of cookie

The endpoint no longer sets a long-lived session cookie. It returns a
short-lived access token plus a refresh token in the JSON body, so the
client owns token storage and rotation.

BREAKING CHANGE: /session stops setting the `sid` cookie. Clients must
read `access_token`/`refresh_token` from the response body and send the
access token in the Authorization header.
Refs #310
```

**chore:**

```
chore: ignore local .env files

Adds .env and .env.* to .gitignore so local secrets stop showing up in
git status and can never be staged by accident.
```

**revert:**

```
revert: feat(auth): add token refresh endpoint

This reverts commit 9f2c1ab. The refresh flow leaked tokens into server
logs; reverting until the redaction fix in #221 lands.
```
