# CLAUDE.md

Guidance for working in this repo. See [README.md](README.md) for the public
overview and [ATTRIBUTION.md](ATTRIBUTION.md) for algorithm provenance.

## What this is

A pure-Go bin-packing library (1-D/2-D/3-D) with online/offline algorithms, exact
solvers, metaheuristics, constraints, preferences, a container catalog, a
Generalized-BPP objective, and a single-page web demo that also compiles to
WebAssembly. **No external dependencies** in the main module (keep it that way).

## Commands

```
go build ./...
go test ./...                      # add -race before committing solver changes
go vet ./...
GOOS=js GOARCH=wasm go build ./cmd/wasm     # wasm build check
./scripts/build-wasm.sh            # produces dist/ (static WASM bundle)
cd cmd/webdemo && go run .         # server at http://localhost:8082
cd bench && go run .               # benchmark vs boxpacker3/bp3d (SEPARATE module)
go test ./packapi/ -bench BenchmarkAlgos -run '^$' -benchmem   # algo-vs-algo: speed + bins/fill%
```

CI runs build/vet/`test -race` on Go `stable`. Toolchain is Go ≥1.24 (wasm_exec.js
lives in `$(go env GOROOT)/lib/wasm/`).

## Architecture

- **`pack`** — core interfaces: `Item`, `Bin`, `Placement`, `BinFactory`,
  `BinSelector`, `OnlinePacker`/`OfflinePacker`/`CtxOfflinePacker`, plus
  `Constraint`/`Preference` and scalar/metric helpers. Items carry named float
  **scalars** (weight, profit, category, …) via `pack.ScalarsOf`.
- **`online`** — one `Packer` loop + pluggable selectors (FF/NF/BF/WF/AWF/NkF/RFF/
  Harmonic/SumOfSquares/PreferenceFit).
- **`offline`** — sort-then-delegate wrappers (FFD/BFD/NFD/WFD/MFFD/shelf) + bespoke
  KK, BinCompletion (exact 1-D), BalancedFit, BruteForce, BeamSearch, RuinRecreate,
  GRASP.
- **`meta`** — `BestOf` (fewest bins) and `LexBestOf` (lexicographic metric order).
- **`d1`/`d2`/`d3`** — geometry. 2-D: MaxRects/Guillotine/Skyline/Shelf. 3-D:
  extreme-point (`ContactSpec`: Bottom support, NoFloating, SideX/Y anti-slosh),
  BLF, LAFF. **`joint`** = 3-D one-pass bin-select+place. **`catalog`** = best
  single container type, sequential cascade, or (for GBPP) cheapest profitable mix.
  **`gbpp`** = optional-items + profit + bin-cost objective; `Pack` packs *all*
  items together (FFD) then prunes only the optional items in all-optional bins
  whose profit can't cover the bin cost (do not revert to compulsory-then-optional
  — it fragments and wastes bins).
- **`packapi`** — transport-independent solve API (`PackCtx`/`StreamPack`/
  `PackNested`); the shared core for both front-ends. **No net/http or json here.**
- **`cmd/webdemo`** — thin HTTP server. **`cmd/wasm`** (`//go:build js && wasm`) —
  exposes `goPack`/`goPackNested`/`goPackStream` to JS.

## Conventions & gotchas

- **Cancellation:** long solves take `context.Context`. Offline packers implement
  `PackAllCtx` (and `pack.CtxOfflinePacker`); exact solvers have `...Ctx` variants;
  HTTP handlers pass `r.Context()`. Sample `ctx.Err()` in hot loops, don't check
  every iteration.
- **Adding an algorithm:** implement it (usually in `offline`/`d3`), wire a case in
  `packapi.pack1D/pack2D/pack3D` (early-return block in 3-D, switch case in 1-D/2-D),
  add it to the `ALGOS` arrays in `cmd/webdemo/static/index.html`, add it to
  `isStreamable` only if it commits placements incrementally, and add a `packapi`
  test. Attribute any literature/repo source in ATTRIBUTION.md and the file's doc
  comment.
- **The frontend is one file** (`cmd/webdemo/static/index.html`, ~2.8k lines, inline
  JS + three.js from CDN). It is **dual-mode**: served by the Go server it calls
  `/api/*`; the `dist/` bundle sets `window.PACK_WASM` and routes through the Web
  Worker (`pack-worker.js`). Keep both paths working; the build script injects the
  flag — don't hardcode it in the source file.
- **Streaming protocol:** `/api/pack/stream` emits NDJSON `StreamFrame`s
  (meta → batch → done). The same `onFrame` handler consumes server NDJSON and
  worker messages. Non-incremental algorithms (auto, catalog, gbpp, lex, balancing)
  solve fully then emit one batched result.
- **bench/ is a separate Go module** (its own go.mod, `replace` → parent) so
  boxpacker3 never becomes a dependency of the main module. `go ... ./...` from the
  root does not include it.
- **3-D rendering:** the camera far-plane scales with scene size in `ensureBins`
  (many bins were clipped at the old hardcoded 1000). Catalog mixes render every
  bin at the largest size in the mix (`bin_dims`).
- **Nested mode** (`/api/pack/nested`, two levels: cartons → pallets) supports the
  new features *per level*: each `NestedLevelSpec` carries `Containers` (catalog),
  `BinCost`, and `LexObjectives`. A level-0 catalog that mixes carton sizes feeds
  level 1 via each carton's *actual* chosen dimensions; results carry per-bin dims.
  The UI mirrors the outer (pallet) and inner (carton) controls separately.
- Run `gofmt -w` on Go files you edit. Prefer the Edit/Write tools over `sed`/`perl`.
- UI changes are verified by the user, who drives the running demo themselves; JS
  errors and blank renders don't show up in `go test`, so flag UI-affecting changes
  for them to check rather than asserting the frontend works from Go tests alone.

## Git

Default branch `main`. Commit only when asked, and commit **directly to `main`**
— do not create a feature branch. `bench/bench` is a build artifact (gitignored);
never commit binaries. End commit messages with the Co-Authored-By trailer.
