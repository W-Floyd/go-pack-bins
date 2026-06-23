#!/usr/bin/env bash
# Runs the algorithm comparison benchmarks (packapi BenchmarkAlgos*) compiled to
# WebAssembly, so the timings reflect the client-side runtime the web demo
# actually uses — not native Go. The packing *results* (bins / fill% / compact%)
# are identical to native; only the ns/op differs (wasm runs roughly 4-7× slower
# and single-threaded — GOMAXPROCS=1, so meta.BestOf can't parallelise).
#
#   ./scripts/bench-wasm.sh                       # all modes, 1 iteration each
#   ./scripts/bench-wasm.sh -bench BenchmarkAlgos3D -benchtime=5x
#
# Any extra args are passed through to `go test`. Output (the benchmark table on
# stdout) is also saved to bench-wasm.out. The README comparison table is NOT
# touched: the auto-writer's file write is a no-op under the wasm fs, so this
# never overwrites the native table in README.md.
#
# Requires node (the js/wasm exec host) — wasmtime/wasip1 would also work if
# installed, but the demo ships js/wasm, so that is what we measure.
set -euo pipefail

cd "$(dirname "$0")/.."

if ! command -v node >/dev/null 2>&1; then
	echo "bench-wasm: node is required to run js/wasm tests (the demo's runtime)" >&2
	exit 1
fi

EXEC="$(go env GOROOT)/lib/wasm/go_js_wasm_exec"
if [[ ! -x "$EXEC" ]]; then
	echo "bench-wasm: missing $EXEC (Go toolchain js/wasm support)" >&2
	exit 1
fi

# Default to the whole suite, one iteration (quality metrics are exact at 1x;
# bump -benchtime for steadier timings). Caller args override/extend.
args=(-run '^$' -benchmem)
if [[ $# -eq 0 ]]; then
	args+=(-bench 'BenchmarkAlgos' -benchtime=1x)
fi

echo "Running packapi benchmarks under js/wasm (node $(node --version))..."
GOOS=js GOARCH=wasm go test ./packapi/ "${args[@]}" "$@" -exec="$EXEC" 2>&1 | tee bench-wasm.out
echo
echo "Saved to bench-wasm.out (README.md left untouched — wasm fs write is a no-op)."
