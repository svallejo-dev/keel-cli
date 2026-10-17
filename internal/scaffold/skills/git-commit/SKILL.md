---
name: git-commit
description: >
  Stage changes and create clean, atomic Conventional Commits automatically. Use this skill
  whenever the user wants to commit work — triggers include "commit", "commit these changes",
  "haz commit", "crea un commit", "conventional commit", "stage and commit", "split into
  atomic commits", or "write a commit message". Runs git for real (status, diff, add, commit,
  log), groups unrelated changes into separate commits, writes the message in Conventional
  Commits format, and executes without pausing to ask — subject to non-negotiable safety
  guardrails (no secrets, no committing on protected branches, no force-push).
---

# Git Commit Skill

Eres un ingeniero que hace commits como en un repo serio: **atómicos**, con mensajes en
formato **Conventional Commits**, ejecutados de forma **totalmente automática**. La skill
corre `git` de verdad (no describe: hace). No pausa para pedir confirmación paso a paso;
sí respeta guardrails de seguridad innegociables (ver `## Guardrails`). Para la referencia
completa de tipos, reglas de subject/body/footer y el playbook de "working tree desordenado
→ commits atómicos", lee `references/conventional-commits.md`.

## Principio rector

**Un commit = un concern.** Antes de teclear `git commit`, pregúntate: ¿este commit cuenta
una sola historia coherente que alguien podría revertir por sí sola? Si no, divídelo.

---

## Flujo paso a paso

### 1. Entender el cambio

Nunca commitees a ciegas. Primero mira qué hay:

```bash
git status                 # archivos modificados, staged y sin trackear
git diff                   # cambios en el working tree (aún sin stage)
git diff --staged          # lo que ya está en el índice (--cached es alias)
git branch --show-current  # en qué rama estás (importa para el guardrail de rama)
```

Lee el diff de verdad. Identifica los archivos tocados y agrúpalos mentalmente por
**concern** (área/propósito): ¿es una feature nueva? ¿un fix? ¿un refactor? ¿formateo?
¿docs? ¿configuración de build/CI? Un mismo working tree suele mezclar varios.

### 2. Agrupar en commits ATÓMICOS

Reglas de corte (una por concern):

- **Un concern por commit.** No mezcles una feature con un fix no relacionado.
- **Refactor aparte de feature.** Mover/renombrar código va en su propio commit, separado
  del cambio de comportamiento.
- **Formateo/estilo aparte de lógica.** Un reformateo masivo ahoga el diff real; sepáralo.
- **Cambios no relacionados → commits distintos**, aunque estén en el mismo archivo.

Staging selectivo para lograrlo:

```bash
git add path/to/file.go path/to/other.go   # solo los archivos de este concern
git add -p                                  # trocea un archivo: elige qué hunks van
git restore --staged path/to/file          # sacar algo del índice si se coló
```

`git add -p` es la herramienta clave cuando un mismo archivo contiene dos concerns: te
deja elegir hunk por hunk (`y`/`n`/`s` para partir, `q` para salir).

### 3. Chequeos rápidos antes de commitear

Si el repo tiene checks **baratos y disponibles**, córrelos antes de sellar el commit.
No commitees algo que rompe el build.

```bash
# Go (ejemplo típico):
gofmt -l .        # lista archivos mal formateados; vacío = OK
go build ./...    # debe compilar
go vet ./...      # análisis estático básico
```

Otros ecosistemas: el linter/formateador/`build` o `test` que el repo ya use (p. ej.
`npm run lint`, `ruff`, `cargo build`). Si un chequeo **barato** falla y el fix es
formateo, arréglalo y re-stagea. **Aborta el commit que rompe el build** y reporta el
error en vez de commitear código roto. No inventes ni instales toolchains que el repo
no tenga; si no hay checks baratos, sigue.

### 4. Redactar el mensaje Conventional Commit

Formato: `type(scope): subject`, cuerpo opcional tras línea en blanco, footers al final.

- **type**: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`,
  `chore`, `revert` (tabla completa en la referencia).
- **scope** (opcional): el paquete/área afectada, entre paréntesis: `feat(auth): …`.
- **subject**: imperativo, minúscula inicial, **sin punto final**, ≤50 columnas.
- **body** (tras línea en blanco): el **PORQUÉ** y el contexto, ~72 columnas por línea.
  Explica la motivación, no repitas el diff.
- **footers**: `BREAKING CHANGE: …`, `Refs #N`, `Closes #N`, `Co-Authored-By: …`.
- **Breaking change**: marca con `!` (`feat(api)!: …`) y/o footer `BREAKING CHANGE:`.

Con cuerpo y/o footers, usa un archivo o `-m` repetido (cada `-m` es un párrafo):

```bash
# Opción A — archivo de mensaje (mejor para cuerpos largos y footers):
cat > /tmp/commitmsg.txt <<'EOF'
feat(auth): add token refresh endpoint

Sessions expired silently after 1h, forcing a full re-login. This adds
a refresh endpoint so the client can renew tokens transparently.

Closes #142
EOF
git commit -F /tmp/commitmsg.txt

# Opción B — solo subject:
git commit -m "fix(parser): handle empty input without panicking"

# Opción C — subject + body con -m repetido:
git commit -m "refactor(store): extract query builder" \
           -m "No behavior change; splits a 200-line method for testability."
```

### 5. Ejecutar los commits (automático)

Repite stage-selectivo + `git commit` una vez por concern, en orden lógico (p. ej. el
refactor antes de la feature que lo usa). No pares a pedir confirmación entre commits.

### 6. Reportar

Muestra el resultado para que el usuario vea qué quedó registrado:

```bash
git log --oneline -n 5     # ajusta -n al nº de commits que hiciste
git status                 # confirma que el working tree quedó limpio
```

---

## Guardrails

Innegociables. No son "preguntar antes": son "no hacer estupideces".

1. **Nunca commitees secretos.** Si el diff contiene `.env`, claves privadas, tokens,
   credenciales, `id_rsa`/`*.pem`, cadenas tipo `AKIA…`/`sk-…`, o binarios enormes:
   **ABORTA** ese commit, saca el archivo del índice (`git restore --staged <path>`) y
   avisa al usuario. Sugiere `.gitignore` en vez de commitear.
2. **Rama protegida → crea feature branch primero.** Si `git branch --show-current`
   devuelve `main`/`master` (o una rama protegida), crea una rama **antes** de commitear:
   ```bash
   git switch -c feat/descripcion-corta
   ```
   Nunca commitees directo sobre la rama protegida.
3. **Nunca `git push --force`.** Como mucho `git push --force-with-lease` sobre **tu
   propia** rama y solo si es imprescindible (p. ej. tras un rebase de tu feature). Jamás
   force-push a ramas compartidas/protegidas.
4. **Respeta hooks y plantillas del repo.** Si existen `pre-commit`, `commit-msg`,
   `commitlint`, `.gitmessage`, etc., déjalos actuar: no uses `--no-verify` para saltarte
   un hook que falla; corrige la causa. Si el hook rechaza el mensaje, ajústalo al estándar.
5. **No reescribas historia publicada.** `commit --amend`/`rebase` solo sobre commits
   locales aún no empujados.

## Checklist

Antes de dar por cerrado el trabajo, verifica:

- [ ] Leíste `git diff` / `git diff --staged`; entiendes cada cambio que vas a commitear.
- [ ] Cada commit es **un solo concern**; refactor/formateo separados de la lógica.
- [ ] Ningún secreto, credencial, `.env` ni binario enorme entró al índice.
- [ ] No estás commiteando sobre `main`/`master`; si lo estabas, creaste feature branch.
- [ ] Los checks baratos del repo pasan; ningún commit rompe el build.
- [ ] Cada mensaje es Conventional Commit válido: `type(scope): subject` imperativo ≤50,
      cuerpo con el porqué a ~72, footers correctos (`Closes #N`, `BREAKING CHANGE:`…).
- [ ] Hooks/plantillas del repo respetados; no se usó `--no-verify` ni force-push.
- [ ] Reportaste con `git log --oneline` y el working tree quedó limpio.

---

## Reference Files

- `references/conventional-commits.md` — tabla completa de types y cuándo usar cada uno,
  reglas de subject/body/footer con GOOD/BAD, breaking changes (`!` y `BREAKING CHANGE:`),
  el playbook "working tree desordenado → commits atómicos" (cómo decidir los cortes,
  staging selectivo, separar refactor de feature) y ejemplos completos de mensajes reales.
