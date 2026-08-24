#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"
mkdir -p dist
cp ../frontend/index.html ../frontend/pico.min.css dist/
cp web-adapter.js worker.js dist/
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" dist/
rm -f dist/database.json dist/affixes.json
ASSET_DIR="assets"
if [[ -e "$ASSET_DIR" ]]; then
  echo "$ASSET_DIR already exists; refusing to overwrite it" >&2
  exit 1
fi
mkdir "$ASSET_DIR"
trap 'rm -rf "$ASSET_DIR"' EXIT
cp ../affixes.json "$ASSET_DIR/affixes.json"
python3 export_database.py ../../database/db_mistfalldb.sqlite "$ASSET_DIR/database.json"
BUILD_CACHE="${MISTFALL_HUNTER_GO_CACHE:-${TMPDIR:-/tmp}/mistfall-hunter-go-cache}"
mkdir -p "$BUILD_CACHE"
if command -v garble >/dev/null 2>&1; then
  GOCACHE="$BUILD_CACHE" GOOS=js GOARCH=wasm garble -tiny build -trimpath -ldflags="-s -w" -o dist/mistfall.wasm .
else
  echo "garble not found; using stripped Go WASM build" >&2
  GOCACHE="$BUILD_CACHE" GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" -o dist/mistfall.wasm .
fi
BUILD_TIMESTAMP="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
sed -i "s#__BUILD_TIMESTAMP__#${BUILD_TIMESTAMP}#; s#</head>#<script defer src=\"https://cloud.umami.is/script.js\" data-website-id=\"8cab2508-a215-4c59-85f9-90004ad58d6e\"></script><script src=\"web-adapter.js\"></script></head>#" dist/index.html
