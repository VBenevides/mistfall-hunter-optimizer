#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"
cp ../database/db_mistfalldb.sqlite ../database/affixes.json .
mkdir -p dist

BUILD_TIMESTAMP="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
FRONTEND_BACKUP="$(mktemp)"
cp frontend/index.html "$FRONTEND_BACKUP"
trap 'cp "$FRONTEND_BACKUP" frontend/index.html; rm -f "$FRONTEND_BACKUP"' EXIT
sed -i "s#__BUILD_TIMESTAMP__#${BUILD_TIMESTAMP}#" frontend/index.html

BUILD_CACHE="${MISTFALL_HUNTER_GO_CACHE:-${TMPDIR:-/tmp}/mistfall-hunter-go-cache}"
mkdir -p "$BUILD_CACHE"

GOCACHE="$BUILD_CACHE" GOOS=linux GOARCH=amd64 CGO_ENABLED=1 go build -tags production -ldflags="-w -s" \
  -o dist/mistfall-hunter-equipment-optimizer-linux-amd64 .

GOCACHE="$BUILD_CACHE" GOOS=windows GOARCH=amd64 CGO_ENABLED=1 go build \
  -tags production -ldflags="-w -s -H=windowsgui" \
  -o dist/mistfall-hunter-equipment-optimizer-windows-amd64.exe .

echo "Built dist/mistfall-hunter-equipment-optimizer-linux-amd64"
echo "Built dist/mistfall-hunter-equipment-optimizer-windows-amd64.exe"
