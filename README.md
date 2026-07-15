# go-pack-bins

A bin-packing library for Go covering 1-D, 2-D, and 3-D problems, with online and
offline algorithms, exact solvers, metaheuristics, hard scalar **constraints**,
soft **preferences** (balancing, colocation, stability), physical stacking rules,
a heterogeneous **container catalog**, a **generalized** (optional-items + profit +
bin-cost) objective, and a browser demo — which also compiles to **WebAssembly**
so the whole thing runs client-side with no server.

```
go get github.com/W-Floyd/go-pack-bins
```

The library has a **single optional dependency** — `github.com/crillab/gophersat`,
a pure-Go SAT solver used only by the SAT-based exact solver; every other package
is dependency-free (and the `js/wasm` build works without it). Solves are
cancellable via `context.Context`.

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
  small instances), **BeamSearch**, and the **RuinRecreate** / **AdaptiveRuinRecreate**
  / **GRASP** metaheuristics. The 3-D order-search metaheuristics decode candidate
  orderings through a strong constructive strategy (EMS by default, configurable).
- **`meta`** — composition: `BestOf(...)` runs several packers and keeps the
  fewest-bins result (winner via `Winner()`); `LexBestOf(metrics, ...)` chooses by
  a lexicographic ordering of objectives.
- **`d1` / `d2` / `d3`** — dimensioned bins, items, and placement geometry.
  2-D: MaxRects, Guillotine, Skyline, Shelf. 3-D: extreme-point (with support /
  anti-slosh), Bottom-Left-Fill, **EMS** (empty-maximal-space), **Fit**
  (maximal-contact best-fit, grounded so it never floats against walls/ceiling),
  **Heightmap**, **LAFF** (largest-area-fit-first layers), **LayerStack**
  (flat, sequential layers that stream their progress), **BlockPacker** /
  **ColumnPacker** (waste-free rectangular blocks / columns tiled with MaxRects),
  and **Assembler** (fuse perfectly-nesting items into solid blocks, then EMS).
  `d3.LowerBound` gives a combinatorial bin-count bound (so a matching packing is
  *provably optimal* and the gap is real), and `d3.RefineVoids` is a void-guided
  post-pass that pulls items into the deepest gaps to tighten a finished packing.
- **`joint`** — a 3-D packer that decides bin selection *and* placement position
  together under one multi-objective score (balance + anti-slosh, single pass).
- **`catalog`** — picks the best container type for an order from a catalog of
  candidate sizes (honouring a per-type max count), or cascades an order across
  sizes when one type's count is exhausted.
- **`gbpp`** — the Generalized Bin Packing objective: optional items carry a
  profit, bins carry a cost, and the solve minimises net cost (with rejection).
- **`sat`** — an exact **2-D** packer built on a SAT encoding (the sole gophersat
  user). It *certifies* optimality: it squeezes the bin count k until k is
  satisfiable and k−1 is not (or k hits the area lower bound). Needs
  integer-scalable dimensions; errors rather than rounding.
- **`strip`** — strip packing: fix the container's base and minimise the open
  dimension (2-D roll/fabric/PCB height; 3-D printer-bed/pallet/truck length),
  reusing the d2/d3 placement strategies and keeping the smallest extent found.
- **`knapsack`** — single-container value maximisation: given one bin too small
  for everything, choose the highest-value subset that fits (defaults to volume,
  i.e. best utilisation). Dimension-agnostic; greedy by value density.
- **`geometry`** — vector/matrix helpers for 3-D solids.
- **`packapi`** — transport-independent solve API (plain structs in/out) shared by
  the server and the WASM build.
- **`algoreg`** — the self-describing algorithm registry: one source of truth for
  which algorithms exist and what each supports. The front-ends build their UI
  from it, so adding an algorithm needs no `index.html` edit.
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
- **`offline.AdaptiveRuinRecreate`** — stronger R&R that walks a current solution
  with record-to-record-travel acceptance and an adaptive ruin size (grows on
  stall to escape local optima), reaching tighter packings in the same budget.
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
as JSON, and a solved result can be exported as **placement JSON** (per-item
position/size/orientation + bin dimensions) or as a **binary STL** mesh (one box
per placement, bins laid out side by side).

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
The benchmark instances are defined once in [`packapi/benchmarks.json`](packapi/benchmarks.json);
`cmd/render` draws those same instances per algorithm — see the rendered sheets in
[`docs/renders/`](docs/renders/).

<!-- BENCH:START -->

_Arrows mark the better direction (↓ lower-is-better, ↑ higher-is-better); the best value in each column is **bold** (all ties, unless every row matches). `fill%` = packed volume ÷ (bins × bin volume); higher is tighter. `compact%` = packed volume ÷ the items' bounding-box volume, averaged over bins — how void-free the occupied envelope is, *independent* of how full the bin is, so it isn't flattered by underfill. Each solve is timeboxed to 4s (an interactive-request budget; raise PACK_BENCH_TIMEOUT for an offline-planning table); **DNF** = did not finish in time. A `≤`-prefixed time marks an anytime improvement search (rr/arr/grasp/beam) that ran to the budget and reports its best-so-far. Time is per solve; absolute numbers vary by machine._

<!-- BENCH:3d-mixed:START -->
### 3D — 500 mixed boxes (sides 1–6) into a 20×20×20 bin

| Algorithm | Bins ↓ | Fill % ↑ | Compact % ↑ | Unfit ↓ | Time/op ↓ |
|-----------|-------:|---------:|------------:|--------:|----------:|
| ff | **3** | **76.7** | 86.1 | 0 | 23.135ms |
| ffd | **3** | **76.7** | 88.1 | 0 | 18.325ms |
| bfd | **3** | **76.7** | 88.1 | 0 | 17.943ms |
| nfd | **3** | **76.7** | 82.2 | 0 | 15.186ms |
| blf | **3** | **76.7** | 86.6 | 0 | 64.538ms |
| ems | **3** | **76.7** | 86.8 | 0 | 4.804ms |
| fit | **3** | **76.7** | 76.7 | 0 | 25.986ms |
| heightmap | **3** | **76.7** | 83.2 | 0 | 495.046ms |
| laff | 4 | 57.5 | 68.7 | 0 | 2.151ms |
| layer | **3** | **76.7** | 82.1 | 0 | **573µs** |
| blocks | **3** | **76.7** | **97.0** | 0 | 5.901ms |
| columns | **3** | **76.7** | 91.9 | 0 | 5.017ms |
| assemble | **3** | **76.7** | 91.2 | 0 | 4.62ms |
| rr | **3** | **76.7** | 90.0 | 0 | 5.217ms |
| arr | **3** | **76.7** | 90.0 | 0 | 5.199ms |
<!-- BENCH:3d-mixed:END -->

<!-- BENCH:3d-slosh:START -->
### 3D · anti-slosh — same 500 boxes with 60% bottom support + 50% side anti-slosh (X & Y)

| Algorithm | Bins ↓ | Fill % ↑ | Compact % ↑ | Unfit ↓ | Time/op ↓ |
|-----------|-------:|---------:|------------:|--------:|----------:|
| ff | 3 | 76.7 | 85.7 | 0 | 60.399ms |
| ffd | 3 | 76.7 | 89.4 | 0 | 60.637ms |
| bfd | 3 | 76.7 | 89.4 | 0 | 61.689ms |
| nfd | 3 | 76.7 | 80.6 | 0 | 153.411ms |
| blf | 3 | 76.7 | 86.6 | 0 | 65.921ms |
| ems | 3 | 76.7 | 85.2 | 0 | 10.324ms |
| heightmap | 3 | 76.7 | 86.5 | 0 | 493.431ms |
| layer | 3 | 76.7 | 82.1 | 0 | **1.199ms** |
| blocks | 3 | 76.7 | **97.0** | 0 | 5.814ms |
| assemble | 3 | 76.7 | 91.2 | 0 | 4.589ms |
| rr | 3 | 76.7 | 90.2 | 0 | 8.459ms |
| arr | 3 | 76.7 | 90.2 | 0 | 8.324ms |
<!-- BENCH:3d-slosh:END -->

<!-- BENCH:3d-cartons:START -->
### 3D · carton SKUs — 400 boxes from a 10-SKU palette (sizes divide the bin) into 12×12×12

| Algorithm | Bins ↓ | Fill % ↑ | Compact % ↑ | Unfit ↓ | Time/op ↓ |
|-----------|-------:|---------:|------------:|--------:|----------:|
| ff | 26 | 89.3 | 89.7 | 0 | 3.506ms |
| ffd | **24** | **96.7** | 97.0 | 0 | 1.967ms |
| bfd | **24** | **96.7** | 97.0 | 0 | 1.984ms |
| nfd | 26 | 89.3 | 93.8 | 0 | 676µs |
| blf | 26 | 89.3 | 91.4 | 0 | 5.015ms |
| ems | 26 | 89.3 | 90.2 | 0 | 480µs |
| fit | 26 | 89.3 | 93.2 | 0 | 588µs |
| heightmap | 27 | 86.0 | 87.3 | 0 | 16.251ms |
| laff | 27 | 86.0 | 89.3 | 0 | 4.57ms |
| layer | 27 | 86.0 | 87.4 | 0 | 382µs |
| blocks | **24** | **96.7** | **98.6** | 0 | 5.742ms |
| columns | **24** | **96.7** | 97.9 | 0 | 1.531ms |
| assemble | **24** | **96.7** | 97.5 | 0 | 640µs |
| rr | **24** | **96.7** | 98.0 | 0 | 385µs |
| arr | **24** | **96.7** | 98.0 | 0 | **380µs** |
<!-- BENCH:3d-cartons:END -->

<!-- BENCH:3d-mega:START -->
### 3D · mega-stress — 10 000 mixed boxes (sides 1–6) into a 75×75×75 bin

| Algorithm | Bins ↓ | Fill % ↑ | Compact % ↑ | Unfit ↓ | Time/op ↓ |
|-----------|-------:|---------:|------------:|--------:|----------:|
| ff | — | — | — | — | **DNF** |
| ffd | — | — | — | — | **DNF** |
| bfd | — | — | — | — | **DNF** |
| nfd | — | — | — | — | **DNF** |
| blf | — | — | — | — | **DNF** |
| ems | **1** | **87.2** | 93.5 | 0 | 1.914281s |
| fit | — | — | — | — | **DNF** |
| heightmap | — | — | — | — | **DNF** |
| laff | 2 | 43.6 | 71.6 | 0 | **84.048ms** |
| layer | **1** | **87.2** | 90.9 | 0 | 192.138ms |
| blocks | **1** | **87.2** | **97.7** | 0 | 639.905ms |
| columns | **1** | **87.2** | 87.2 | 0 | 2.449158s |
| assemble | — | — | — | — | **DNF** |
| rr | **1** | **87.2** | 94.8 | 0 | ≤4s |
| arr | **1** | **87.2** | 94.8 | 0 | ≤4s |
<!-- BENCH:3d-mega:END -->

<!-- BENCH:3d-chunky:START -->
### 3D · combinatorial — 24 sizable boxes (sides 5–7) into a 12×12×12 bin — how items combine into a bin decides the count, so the order-search metaheuristics (rr/arr) can save a bin the greedy/constructive packers strand

| Algorithm | Bins ↓ | Fill % ↑ | Compact % ↑ | Unfit ↓ | Time/op ↓ |
|-----------|-------:|---------:|------------:|--------:|----------:|
| ff | 5 | 56.5 | 89.7 | 0 | 26µs |
| ffd | 4 | 70.7 | 93.5 | 0 | 21µs |
| bfd | 4 | 70.7 | 91.3 | 0 | 26µs |
| blf | 5 | 56.5 | 89.7 | 0 | 35µs |
| ems | 5 | 56.5 | 89.7 | 0 | **18µs** |
| fit | 4 | 70.7 | 92.0 | 0 | **18µs** |
| columns | 4 | 70.7 | 93.5 | 0 | 32µs |
| blocks | 5 | 56.5 | 88.3 | 0 | 214µs |
| assemble | 4 | 70.7 | 93.5 | 0 | 47µs |
| rr | **3** | **94.2** | **94.2** | 0 | 7.397ms |
| arr | **3** | **94.2** | **94.2** | 0 | 402µs |
<!-- BENCH:3d-chunky:END -->

<!-- BENCH:2d:START -->
### 2D — 400 mixed rectangles (10–50) into a 300×300 bin

| Algorithm | Bins ↓ | Fill % ↑ | Compact % ↑ | Unfit ↓ | Time/op ↓ |
|-----------|-------:|---------:|------------:|--------:|----------:|
| ff | 4 | 71.1 | 93.0 | 0 | 1.872ms |
| ffd | **3** | **94.8** | **95.0** | 0 | 2.384ms |
| bfd | **3** | **94.8** | **95.0** | 0 | 2.4ms |
| nfd | 4 | 71.1 | 86.9 | 0 | 1.765ms |
| skyline | 4 | 71.1 | 83.8 | 0 | **319µs** |
<!-- BENCH:2d:END -->

<!-- BENCH:1d:START -->
### 1D — 1000 mixed items (1–8) into capacity-10 bins

| Algorithm | Bins ↓ | Fill % ↑ | Compact % ↑ | Unfit ↓ | Time/op ↓ |
|-----------|-------:|---------:|------------:|--------:|----------:|
| ff | 418 | 99.4 | 100.0 | 0 | **1.028ms** |
| bf | 418 | 99.4 | 100.0 | 0 | 1.123ms |
| wf | 464 | 89.6 | 100.0 | 0 | 1.766ms |
| ffd | **416** | **99.9** | 100.0 | 0 | 1.191ms |
| bfd | **416** | **99.9** | 100.0 | 0 | 1.567ms |
| wfd | **416** | **99.9** | 100.0 | 0 | 1.558ms |
| mffd | **416** | **99.9** | 100.0 | 0 | 1.196ms |
<!-- BENCH:1d:END -->

<!-- BENCH:END -->

## Development

```
go build ./...
go test ./...           # add -race for the concurrency-sensitive paths
go vet ./...
GOOS=js GOARCH=wasm go build ./cmd/wasm   # wasm build check

cd bench && go run .     # benchmark vs boxpacker3 (separate module)
```
