# 6. Hallazgos sobre Vault y GCP (empíricos)

Probado en real con Docker 29.8, `hashicorp/vault:2.1` (2.1.2) y firebase-tools 15.33.0
sobre `node:22-alpine`.

## Vault

- **Modo `-dev` descartado**: guarda todo en memoria; un `docker restart` borraría la
  configuración del proyecto. keel usa modo servidor con `storage "file"` en el volumen
  `/vault/file` que declara la imagen.
- Configuración vía `VAULT_LOCAL_CONFIG` (JSON) + `SKIP_SETCAP=true` y
  `disable_mlock: true` (los contenedores no suelen tener `IPC_LOCK`). Comando: `server`.
- `GET /v1/sys/health` codifica el estado en el **código HTTP**: `200` activo, `501` sin
  inicializar, `503` sellado (el cuerpo JSON viene siempre).
- Bootstrap: `PUT /v1/sys/init` (`secret_shares=1`, `threshold=1`) → `PUT /v1/sys/unseal`
  → `POST /v1/sys/mounts/secret` `{"type":"kv","options":{"version":"2"}}`. Arranca en ~2 s.
- **Tras cada reinicio del contenedor vuelve sellado.** keel guarda la unseal key en el
  estado local y desella en `up`/`restart`/`sync` (`Ensure`). Válido solo para desarrollo.
- KV v2: datos en `/v1/<mount>/data/<path>`, listado con `LIST /v1/<mount>/metadata/`,
  borrado definitivo con `DELETE /v1/<mount>/metadata/<path>`. Un path inexistente da 404.

## Identity Platform (Firebase Auth emulator)

- No hay imagen oficial; keel construye `keel/gcp-identity-emulator:<versión>` desde un
  Dockerfile embebido (`npm i -g firebase-tools@<versión>` + `firebase setup:emulators:ui`
  para que la UI funcione sin red). La versión sale del tag de la imagen en el catálogo.
- `GET /` responde `{"authEmulator":{"ready":true}}`: sirve de sonda de salud.
- API cliente: `/identitytoolkit.googleapis.com/v1/accounts:signUp|signInWithPassword?key=<cualquiera>`.
  API admin: `/identitytoolkit.googleapis.com/v1/projects/<p>/accounts…` con
  `Authorization: Bearer owner`. Borrado total: `DELETE /emulator/v1/projects/<p>/accounts`.
- Los ID tokens van **sin firma** (`alg: none`), con `iss=https://securetoken.google.com/<p>`
  y `aud=<p>`; los custom claims (`customAttributes`) aparecen en el siguiente token.
- **Persistencia**: `--import` + `--export-on-exit` en SIGTERM. Exportar directamente al
  volumen (`/data`) falla con `EBUSY` (el emulador intenta borrar el directorio destino,
  que es el punto de montaje): se exporta a `/data/export`. `docker stop -t 30` da margen
  al export; `--import` solo se pasa si existe `firebase-export-metadata.json`.
- **UI**: entrega al navegador las direcciones que el emulador cree tener. Por eso el
  contenedor escucha en **los mismos números de puerto** que keel publica en el host
  (`firebase.json` se genera al arrancar desde `AUTH_PORT`/`UI_PORT`).
- Los ids de proyecto `demo-*` hacen que el emulador no intente tocar recursos reales.

## Riesgos

- firebase-tools cambia a menudo; está fijado por tag de imagen. Al subir versión,
  repetir los *spikes* de arriba (persistencia y UI sobre todo).
- Vault es BSL 1.1: válido para uso interno en desarrollo. Si hiciera falta una
  alternativa con licencia abierta, OpenBao expone la misma API (su imagen usa
  variables `BAO_*`, no sería un cambio de solo `--param image=`).
