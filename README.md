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
  anti-slosh), Bottom-Left-Fill, and **LAFF** (largest-area-fit-first layers).
- **`joint`** — a 3-D packer that decides bin selection *and* placement position
  together under one multi-objective score (balance + anti-slosh, single pass).
- **`catalog`** — picks the best container type for an order from a catalog of
  candidate sizes, honouring a per-type max count.
- **`gbpp`** — the Generalized Bin Packing objective: optional items carry a
  profit, bins carry a cost, and the solve minimises net cost (with rejection).
- **`geometry`** — vector/matrix helpers for 3-D solids.
- **`packapi`** — transport-independent solve API (plain structs in/out) shared by
  the server and the WASM build.
- **`cmd/webdemo`** — HTTP server + single-page visualiser.
- **`cmd/wasm`** — the same `packapi` compiled to `js/wasm`; see [WebAssembly](#webassembly).
- **`bench/`** — a separate module benchmarking against
  [bavix/boxpacker3](https://github.com/bavix/boxpacker3).

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
optional items that aren't worth a bin.

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
for the ⚖ balanceable algorithms — add balance objectives. The pack streams in
progressively; the right-hand panel reports per-metric sum / average / σ across
bins plus a per-bin breakdown. Setups can be saved and reloaded as JSON.

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

## Development

```
go build ./...
go test ./...           # add -race for the concurrency-sensitive paths
go vet ./...
GOOS=js GOARCH=wasm go build ./cmd/wasm   # wasm build check

cd bench && go run .     # benchmark vs boxpacker3 (separate module)
```
