#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUTPUT="${1:-$ROOT_DIR/bin/mita}"
MIERU_REPOSITORY="${MIERU_REPOSITORY:-https://github.com/enfein/mieru.git}"
MIERU_COMMIT="${MIERU_COMMIT:-8b42e23979d14d5afe078d21f9e7d4a6407389b2}"
WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/novapanel-mieru.XXXXXX")"
trap 'rm -rf "$WORK_DIR"' EXIT

mkdir -p "$(dirname "$OUTPUT")"
OUTPUT="$(cd "$(dirname "$OUTPUT")" && pwd)/$(basename "$OUTPUT")"

git -C "$WORK_DIR" init -q
git -C "$WORK_DIR" remote add origin "$MIERU_REPOSITORY"
git -C "$WORK_DIR" fetch -q --depth=1 origin "$MIERU_COMMIT"
git -C "$WORK_DIR" checkout -q --detach FETCH_HEAD
git -C "$WORK_DIR" apply "$ROOT_DIR/patches/mieru-novapanel-bridge-auth.patch"

(
  cd "$WORK_DIR"
  CGO_ENABLED=0 GOOS="$(go env GOHOSTOS)" GOARCH="$(go env GOHOSTARCH)" go test ./pkg/socks5
  CGO_ENABLED="${CGO_ENABLED:-0}" \
    GOOS="${GOOS:-$(go env GOOS)}" \
    GOARCH="${GOARCH:-$(go env GOARCH)}" \
    go build -trimpath -ldflags="-s -w" -o "$OUTPUT" ./cmd/mita
)
