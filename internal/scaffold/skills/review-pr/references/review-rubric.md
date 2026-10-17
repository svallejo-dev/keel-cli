# Rúbrica universal de review

Dimensiones que se aplican a **cualquier** PR, en cualquier lenguaje. Para cada archivo del
diff, recórrelas y anota lo que encuentres como hallazgo (ver taxonomía de severidad abajo).

## Dimensiones — qué buscar

### 1. Corrección y edge cases
- ¿El código hace lo que la intención del PR dice que hace?
- Casos límite: entrada vacía, `nil`/`null`, cero, negativos, listas de un elemento, overflow,
  off-by-one, orden de operaciones, zonas horarias, unicode.
- Rutas de fallo: ¿qué pasa si la llamada de red/DB falla a mitad? ¿Estado consistente?
- Idempotencia y reintentos donde el flujo lo exija.

### 2. Seguridad
- **Injection**: SQL/NoSQL/command/LDAP — ¿input concatenado en vez de parametrizado?
- **Authz/authn**: ¿se verifica permiso en el punto correcto? ¿IDOR (acceso a recursos de otro
  usuario por ID)? ¿se confía en datos del cliente para decidir acceso?
- **Secrets**: claves, tokens, contraseñas hardcodeados o logueados. Márcalo **blocker** y
  **nunca reproduzcas el valor** en el comentario.
- **Validación de input**: todo dato externo (HTTP, colas, archivos) validado y acotado antes
  de usarse; límites de tamaño en lecturas; deserialización segura.
- Crypto: aleatoriedad segura para tokens, comparación en tiempo constante, no algoritmos rotos.

### 3. Tests
- ¿El PR **añade o actualiza** tests para el cambio?
- ¿Cubren los **bordes**, no solo el happy path? ¿El caso de fallo tiene su test?
- ¿Los tests son deterministas (sin sleeps ni dependencias de reloj/red no controladas)?
- ¿Un test que "pasa" realmente ejercita el código nuevo, o es un placeholder?

### 4. Manejo de errores
- Errores **propagados con contexto**, no tragados ni convertidos en `log + continue` silencioso.
- No se pierde el error original; se distingue "esperado" (not found) de "inesperado".
- Sin `panic`/exceptions no capturadas en flujos normales; recursos liberados en el camino de error.

### 5. API y compatibilidad hacia atrás
- ¿Cambia una firma pública, un endpoint, un schema, un formato serializado, un env var?
- **Breaking changes**: campos renombrados/eliminados, tipos cambiados, defaults alterados,
  errores/estatus HTTP distintos. ¿Hay versionado, deprecación o migración?
- Contratos con consumidores: ¿este cambio rompe a quien ya depende del comportamiento actual?

### 6. Performance y concurrencia
- Consultas N+1, trabajo O(n²) evitable, allocaciones en el hot path, ausencia de paginación.
- **Concurrencia**: data races, acceso compartido sin sincronizar, deadlocks, goroutines/hilos
  sin fin claro (leaks), locks retenidos durante I/O.
- ¿Se ejecutó (o debería) el detector de races del lenguaje?

### 7. Tamaño y revisabilidad
- ¿El PR es lo bastante pequeño y enfocado para revisarse con rigor?
- ¿Mezcla concerns (refactor + feature + fix + formato masivo)?
- Nombres claros, funciones cohesivas, sin código muerto ni comentado.
- **Archivos con intención**: cada archivo dice qué hace. Un archivo cajón (`service.go`,
  `usecase.go`, `manager.go`, `helpers.go`, `utils.go`, `common.go`) o un archivo con dos casos
  de uso (`Login()` + `Signup()`) es **major**: obliga a abrirlo para saber qué contiene y
  crece sin cohesión. Fix: un archivo por caso de uso nombrado por su acción (`login.go`).
- **Comentarios mínimos**: si un archivo tiene casi tantas líneas de comentario como de código,
  o comentarios que narran lo obvio, repiten el nombre o son banners (`// ----`), es **minor**
  (ruido que encarece la revisión). Se conservan los que explican un "por qué" no obvio.

### 8. Docs y migraciones
- Cambios de comportamiento reflejados en README/docs/CHANGELOG/comentarios de API pública.
- Migraciones de DB: ¿reversibles? ¿bloquean tablas grandes? ¿compatibles con la versión aún
  desplegada durante el rollout (expand/contract)?
- Feature flags y config nueva documentadas.

### 9. Observabilidad
- Logs con nivel y contexto adecuados (sin secretos, sin spam en hot paths).
- Métricas/trazas para el camino nuevo cuando importa operacionalmente.
- Mensajes de error accionables para quien opera el sistema.

## Taxonomía de severidad

Etiqueta **cada** hallazgo con una y solo una:

- **blocker** — No puede mergear así. Rompe correctness, seguridad, o un contrato público;
  pierde datos; introduce una race. Requiere fix antes de aprobar.
- **major** — Debe arreglarse. Bug real en un caso plausible, test faltante para lógica nueva,
  manejo de errores deficiente, deuda que costará caro pronto (p. ej. archivo/tipo cajón o dos
  casos de uso en un archivo). Justifica `request-changes`.
- **minor** — Debería arreglarse. Mejora de claridad/robustez sin riesgo inmediato; edge case
  poco probable; naming confuso; exceso de comentarios narrativos.
- **nit** — Opcional/estilo. Preferencia, micro-optimización, formato. Nunca bloquea; márcalo
  como tal para que el autor sepa que puede ignorarlo.

Regla del veredicto: ≥1 blocker o major ⇒ `request-changes`. Solo minors/nits ⇒ `comment`.
Todo limpio ⇒ recomiéndalo, pero la aprobación la da un humano.

## Regla de tamaño

Si el PR es **demasiado grande** para revisarse con rigor, o **mezcla concerns**, el hallazgo
de mayor valor es **pedir un split**. Ponlo primero, explica qué cortes propones (p. ej.
"separa el refactor de renombrado del cambio de lógica") y ofrece revisar cada parte por
separado. Un review superficial de un PR gigante es peor que pedir dividirlo.

## Formato recomendado del comentario de review

```markdown
## Review — <título del PR>

**Veredicto:** request-changes | comment  ·  **Intención:** <1 línea>
**Resumen:** <2-3 líneas: qué hace bien, qué falta, blast radius>

### Blockers
- `path/file.ext:LÍNEA` — <qué está mal y por qué>. **Fix:** <cambio concreto>

### Major
- `path/file.ext:LÍNEA` — … **Fix:** …

### Minor
- `path/file.ext:LÍNEA` — …

### Nits
- `path/file.ext:LÍNEA` — …

### Bien
- <1-2 cosas que el PR hace bien — el review también reconoce lo bueno>
```

Si no hay hallazgos en una categoría, omite el encabezado. Mantén el tono constructivo:
critica el código, propone el arreglo, reconoce lo que está bien hecho.
