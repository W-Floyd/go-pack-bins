# go-pack-bins

A bin-packing library for Go covering 1-D, 2-D, and 3-D problems, with online and
offline algorithms, exact solvers, metaheuristics, hard scalar **constraints**,
soft **preferences** (balancing, colocation, stability), physical stacking rules,
a heterogeneous **container catalog**, a **generalized** (optional-items + profit
+ bin-cost) objective, and a browser demo — which also compiles to **WebAssembly**
so the whole thing runs client-side with no server.

```
go get github.com/W-Floyd/go-pack-bins
```

The library has **no external dependencies**. Solves are cancellable via
`context.Context`.

## Architecture

The core is a single decision interface and one shared engine; every algorithm is
a small variation on it.

- **`pack`** — the vocabulary. `Item`, `Bin`, `Placement`, `BinFactory`, the
  `BinSelector` decision (`Select(bins, item) → placement, idx, err`), and the
  `OnlinePacker` / `OfflinePacker` / `CtxOfflinePacker` interfaces. Also
  `Constraint`, `Preference`, and the scalar/metric helpers.
- **`online`** — one shared `Packer` loop plus pluggable `BinSelector`s:
  First/Next/Best/Worst-Fit, Almost-Worst, Next-K-Fit, Refined-First-Fit,
  Harmonic-k, Sum-of-Squares, and `PreferenceFit`. Bins open lazily.
- **`offline`** — packers that see all items first. Most are *sort-then-delegate*
  wrappers around an online selector (`FFD = decreasing volume + First-Fit`, etc.).
  Bespoke ones: Karmarkar-Karp, **Bin-Completion** (exact, 1-D), Modified-FFD,
  shelf NFDH/FFDH/BFDH, `BalancedFit`, **BruteForce** (exhaustive order search for
  small instances), **BeamSearch**, and the **RuinRecreate** / **GRASP**
  metaheuristics.
- **`meta`** — composition: `BestOf(...)` runs several packers and keeps the
  fewest-bins result (winner via `Winner()`); `LexBestOf(metrics, ...)` chooses by
  a lexicographic ordering of objectives.
- **`d1` / `d2` / `d3`** — dimensioned bins, items, and placement geometry.
  2-D: MaxRects, Guillotine, Skyline, Shelf. 3-D: extreme-point (with support /
  anti-slosh), Bottom-Left-Fill, **EMS** (empty-maximal-space), **Heightmap**,
  **LAFF** (largest-area-fit-first layers), and **LayerStack** (flat, sequential
  layers that stream their progress).
- **`joint`** — a 3-D packer that decides bin selection *and* placement position
  together under one multi-objective score (balance + anti-slosh, single pass).
- **`catalog`** — picks the best container type for an order from a catalog of
  candidate sizes (honouring a per-type max count), or cascades an order across
  sizes when one type's count is exhausted.
- **`gbpp`** — the Generalized Bin Packing objective: optional items carry a
  profit, bins carry a cost, and the solve minimises net cost (with rejection).
- **`geometry`** — vector/matrix helpers for 3-D solids.
- **`packapi`** — transport-independent solve API (plain structs in/out) shared by
  the server and the WASM build.
- **`cmd/webdemo`** — HTTP server + single-page visualiser.
- **`cmd/wasm`** — the same `packapi` compiled to `js/wasm`; see [WebAssembly](#webassembly).
- **`bench/`** — a separate module benchmarking against
  [bavix/boxpacker3](https://github.com/bavix/boxpacker3) and
  [gedex/bp3d](https://github.com/gedex/bp3d).

Algorithm provenance for methods adapted from other projects/papers is recorded in
[ATTRIBUTION.md](ATTRIBUTION.md).

## Constraints (hard) vs preferences (soft)

**Constraints** gate placement and are enforced by `ConstrainedBin` regardless of
which selector you use, so they compose with every algorithm:

```go
factory := pack.NewConstrainedFactory(d1.NewFactory(10),
    pack.MaxAggregate("weight", 8),            // no bin may exceed total weight 8
    pack.AllSame("zone"),                      // one zone per bin
    pack.Incompatible("hazmat", [2]float64{1, 2})) // categories 1 and 2 never share a bin
```

Also available: `MinAggregate`. `Incompatible` is the "manifest" rule (e.g. never
co-pack lighters and dynamite).

**Per-face contact** (`d3.ContactSpec`) unifies physical stacking and
anti-slosh as one primitive — the contacted fraction of a box's faces:

- `Bottom` is a hard **support gate**: ≥ this fraction of the −z face must rest
  on the floor or boxes below. `NoFloating` requires every box to rest on the
  floor or another box.
- `SideX` / `SideY` are soft **anti-slosh targets** on the lateral faces: a
  positive value makes placement prefer wall/neighbour contact, and a compaction
  pass (`d3.Compact` / `d2.Compact`) then slides items together to close lateral
  gaps that let a load shift in transit.

**Preferences** score *which* bin a candidate goes in. They are consumed by
`PreferenceFit` (and the two-pass `BalancedFit`):

| Preference | Effect |
|---|---|
| `ColocateHigh(s)` / `ColocateLow(s)` | concentrate / spread a scalar |
| `BalanceCount()` / `ConcentrateCount()` | even / tight item counts |
| `FillHigh()` / `FillLow()` | Best-Fit / Worst-Fit, as preferences |
| `MinimizeHeight()` | keep 3-D stacks low |
| `MinimizeCG(mass)` | keep the centre of gravity low (stable loads) |
| `Weighted(p, w)` | scale any preference's pull |

```go
// Pack tightly, but lean toward even weight across bins:
p := online.PreferenceFit(factory, pack.FillHigh(), pack.Weighted(pack.ColocateLow("weight"), 2))
```

## BalancedFit — balance without wasting bins

Online preference selection opens bins lazily, so a balancing preference can only
even out the bins already open and the tail may spill into an extra bin.
`offline.BalancedFit` fixes this in two passes: it first learns the minimum bin
count *K* from a best-of decreasing-fit probe, then pre-opens *K* bins and
distributes items (largest first) under the preferences — balancing *within* the
fewest bins. `NewBalancedFitW` takes positional weights and min-max normalizes
each preference across the candidate bins, so weights are comparable even when
preferences live on wildly different scales.

## Metaheuristics & search

Beyond the constructive heuristics, several searches improve packing quality and
all honour `context.Context` as a deadline:

- **`offline.BruteForce`** — exhaustive search over item order for small orders
  (with duplicate-permutation pruning; falls back to FFD above a cap).
- **`offline.BeamSearch`** — width-limited tree search over placement order; the
  middle ground between greedy and brute force.
- **`offline.RuinRecreate`** — ruin-and-recreate local search: repeatedly remove a
  subset of items and repack, keeping improvements.
- **`offline.GRASP`** — greedy-randomized multistart + local search.

## Container catalog & the Generalized BPP

`catalog.Best(ctx, items, candidates)` packs an order into each candidate
container type and returns the type that packs best (most placed → fewest
containers → least waste), honouring a per-type max count.

`gbpp.Pack` implements the Generalized Bin Packing objective: items may be
*compulsory* or *optional* (optional ones carry a `profit` scalar), bins carry a
cost, and the solve minimises **net cost = bins×cost − included profit**, rejecting
optional items that aren't worth a bin. It packs *all* items together first (so
optional and compulsory items consolidate tightly) and then drops only the optional
items in an all-optional bin whose profit can't cover the bin cost — an optional
item riding along in a bin a compulsory item already paid for is always kept free.
`gbpp.PackCatalog` extends this to a heterogeneous catalog, choosing the most
profitable *mix* of bin types rather than exhausting one type first.

## Quick start

```go
factory := d1.NewFactory(10)
packer := offline.FirstFitDecreasing(factory)
result, err := packer.PackAll([]pack.Item{
    d1.NewItem("a", 6), d1.NewItem("b", 4), d1.NewItem("c", 5),
})
fmt.Println(result.BinsUsed())
```

With cancellation (any `CtxOfflinePacker`, the exact solvers, and `packapi`):

```go
ctx, cancel := context.WithTimeout(context.Background(), time.Second)
defer cancel()
result, err := packer.PackAllCtx(ctx, items)
```

## Web demo

```
cd cmd/webdemo && go run .   # then open http://localhost:8082
```

Pick a dimension and algorithm, add items (with scalars), set constraints (incl.
incompatibilities), enable a container catalog or nested (cartons → pallets), and —
for the ⚖ balanceable algorithms — add balance objectives. Nested mode exposes the
new features at *each* level independently: the inner (carton) and outer (pallet)
stages each take their own catalog, bin cost, GBPP, and lexicographic objectives.
The pack streams in progressively; the right-hand panel reports per-metric sum /
average / σ across bins plus a per-bin breakdown. Setups can be saved and reloaded
as JSON.

## WebAssembly

The solver compiles to `js/wasm` so the demo can run entirely in the browser with
no server:

```
./scripts/build-wasm.sh    # → dist/ (index.html + wasm_exec.js + app.wasm + worker)
cd dist && python3 -m http.server 8083
```

The bundle runs the solver in a Web Worker, so packs stream progressively without
blocking the UI. The same `cmd/webdemo` page also works server-served (it falls
back to the `/api/*` endpoints when the WASM bridge is absent).

## Benchmarks

The tables below compare the algorithms head-to-head on identical instances —
speed plus solution quality (bins used, fill rate). Regenerate them in place with:

```
go test ./packapi/ -bench BenchmarkAlgos -run '^$'
```

That run rewrites everything between the markers below (the comparison vs the
external `boxpacker3`/`bp3d` libraries lives in the separate [`bench/`](bench/) module).

<!-- BENCH:START -->

_Arrows mark the better direction (↓ lower-is-better, ↑ higher-is-better); the best value in each column is **bold** (all ties, unless every row matches). `fill%` = packed volume ÷ (bins × bin volume); higher is tighter. `compact%` = packed volume ÷ the items' bounding-box volume, averaged over bins — how void-free the occupied envelope is, *independent* of how full the bin is, so it isn't flattered by underfill. Time is per solve; absolute numbers vary by machine._

### 3D · carton SKUs — 400 boxes from a 10-SKU palette (sizes divide the bin) into 12×12×12

| Algorithm | Bins ↓ | Fill % ↑ | Compact % ↑ | Unfit ↓ | Time/op ↓ |
|-----------|-------:|---------:|------------:|--------:|----------:|
| ff | 26 | 89.3 | 89.7 | 0 | 4.8ms |
| ffd | **24** | **96.7** | 97.0 | 0 | 2.552ms |
| bfd | **24** | **96.7** | 97.0 | 0 | 2.534ms |
| nfd | 26 | 89.3 | 93.8 | 0 | **677µs** |
| blf | 26 | 89.3 | 91.4 | 0 | 6.017ms |
| ems | 26 | 89.3 | 90.2 | 0 | 2.165ms |
| heightmap | 27 | 86.0 | 87.3 | 0 | 17.834ms |
| laff | 27 | 86.0 | 89.3 | 0 | 4.264ms |
| layer | 27 | 86.0 | 87.4 | 0 | 1.527ms |
| blocks | **24** | **96.7** | 97.6 | 0 | 6.349ms |
| assemble | 32 | 72.6 | **98.8** | 0 | 834µs |
| auto | **24** | **96.7** | 97.0 | 0 | 6.5ms |

<!-- BENCH:END -->

## Development

```
go build ./...
go test ./...           # add -race for the concurrency-sensitive paths
go vet ./...
GOOS=js GOARCH=wasm go build ./cmd/wasm   # wasm build check

cd bench && go run .     # benchmark vs boxpacker3 (separate module)
```
