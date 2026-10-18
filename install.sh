#!/bin/sh
# Installs keel from GitHub Releases. Env: KEEL_VERSION (default latest), KEEL_BIN_DIR.
set -eu

REPO="svallejo-dev/keel-cli"
VERSION="${KEEL_VERSION:-latest}"
BIN_DIR="${KEEL_BIN_DIR:-$HOME/.local/bin}"
BASE_URL="${KEEL_BASE_URL:-}"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  darwin|linux) ;;
  *) echo "keel: sistema no soportado: $os (en Windows descarga el .zip del release)" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "keel: arquitectura no soportada: $(uname -m)" >&2; exit 1 ;;
esac

if [ -n "$BASE_URL" ]; then
  base="$BASE_URL"
elif [ "$VERSION" = latest ]; then
  base="https://github.com/$REPO/releases/latest/download"
else
  base="https://github.com/$REPO/releases/download/$VERSION"
fi
asset="keel_${os}_${arch}.tar.gz"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "keel: descargando $asset ($VERSION)"
curl -fsSL "$base/$asset" -o "$tmp/$asset"
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt"

expected=$(grep " $asset\$" "$tmp/checksums.txt" | cut -d' ' -f1)
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$tmp/$asset" | cut -d' ' -f1)
else
  actual=$(shasum -a 256 "$tmp/$asset" | cut -d' ' -f1)
fi
if [ -z "$expected" ] || [ "$expected" != "$actual" ]; then
  echo "keel: checksum inválido para $asset, no se instaló nada" >&2
  exit 1
fi

tar -xzf "$tmp/$asset" -C "$tmp" keel
mkdir -p "$BIN_DIR"
install -m 0755 "$tmp/keel" "$BIN_DIR/keel"

echo "keel: instalado en $BIN_DIR/keel ($("$BIN_DIR/keel" --version))"
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) echo "keel: añade a tu shell: export PATH=\"$BIN_DIR:\$PATH\"" ;;
esac
