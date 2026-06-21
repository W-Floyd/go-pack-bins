#!/usr/bin/env bash
# Builds the self-contained static bundle: the packing UI plus the Go solver
# compiled to WebAssembly, with no server required. Output goes to dist/:
#
#   dist/index.html     the UI, with the wasm bootstrap injected before </body>
#   dist/wasm_exec.js   Go's JS support shim (copied from GOROOT)
#   dist/app.wasm       the solver (cmd/wasm)
#
# Serve dist/ from any static host (it needs an HTTP origin — wasm cannot be
# instantiated from a file:// URL). When the page is instead served by the Go
# server (cmd/webdemo), the bootstrap is absent, goPackReady is never set, and
# the UI falls back to the /api/* HTTP endpoints automatically.
set -euo pipefail

cd "$(dirname "$0")/.."
DIST=dist
SRC=cmd/webdemo/static/index.html

rm -rf "$DIST"
mkdir -p "$DIST"

echo "→ building app.wasm"
GOOS=js GOARCH=wasm go build -o "$DIST/app.wasm" ./cmd/wasm

echo "→ copying wasm_exec.js + pack-worker.js"
# wasm_exec.js lives in lib/wasm/ as of Go 1.24 (the version CI and dev use).
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" "$DIST/wasm_exec.js"
cp cmd/webdemo/static/pack-worker.js "$DIST/pack-worker.js"

echo "→ generating index.html with wasm flag"
# The page itself does NOT load the wasm — the worker (pack-worker.js) does. We
# only set window.PACK_WASM so the app routes solves to the worker instead of the
# server. It must be set BEFORE the main inline script reads it, so inject it at
# the top of <head> (the app script lives later in the body).
SRC="$SRC" OUT="$DIST/index.html" python3 - <<'PY'
import os
src = open(os.environ["SRC"]).read()
flag = "<head>\n<script>window.PACK_WASM = true;</script>"
if "<head>" not in src:
    raise SystemExit("no <head> in source index.html")
out = src.replace("<head>", flag, 1)
open(os.environ["OUT"], "w").write(out)
PY

echo "✓ bundle ready in $DIST/ ($(du -h "$DIST/app.wasm" | cut -f1) wasm)"
echo "  serve it, e.g.:  (cd $DIST && python3 -m http.server 8083)"
