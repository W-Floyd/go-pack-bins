# go-pack-bins

A bin-packing library for Go covering 1-D, 2-D, and 3-D problems, with online and
offline algorithms, hard scalar **constraints**, soft **preferences** (balancing,
colocation, stability), and a browser demo for visualising any of it.

```
go get github.com/W-Floyd/go-pack-bins
```

## Architecture

The core is a single decision interface and one shared engine; every algorithm is
a small variation on it.

- **`pack`** — the vocabulary. `Item`, `Bin`, `Placement`, `BinFactory`, the
  `BinSelector` decision (`Select(bins, item) → placement, idx, err`), and the
  `OnlinePacker` / `OfflinePacker` interfaces. Also `Constraint`, `Preference`,
  and the scalar/metric helpers.
- **`online`** — one shared `Packer` loop plus pluggable `BinSelector`s:
  First/Next/Best/Worst-Fit, Almost-Worst, Next-K-Fit, Refined-First-Fit,
  Harmonic-k, and `PreferenceFit`. Bins open lazily: a new bin is opened only when
  the current item fits in none.
- **`offline`** — packers that see all items first. Most are *sort-then-delegate*
  wrappers around an online selector (`FFD = decreasing volume + First-Fit`, etc.).
  Bespoke ones: Karmarkar-Karp, Bin-Completion, Modified-FFD, and `BalancedFit`
  (see below).
- **`meta`** — composition: `BestOf(...)` runs several packers and keeps the
  result with the fewest bins (exposing the winner via `Winner()`).
- **`d1` / `d2` / `d3`** — dimensioned bins, items, and placement geometry
  (2-D MaxRects & Guillotine; 3-D extreme-point with optional support checking).
- **`geometry`** — vector/matrix helpers for 3-D solids.
- **`cmd/webdemo`** — an HTTP server + single-page visualiser.

## Constraints (hard) vs preferences (soft)

**Constraints** gate placement and are enforced by `ConstrainedBin` regardless of
which selector you use, so they compose with every algorithm:

```go
factory := pack.NewConstrainedFactory(d1.NewFactory(10),
    pack.MaxAggregate("weight", 8),   // no bin may exceed total weight 8
    pack.AllSame("zone"))             // one zone per bin
```

Also available: `MinAggregate`.

**Per-face contact** (`d3.ContactSpec`) unifies physical stacking and
anti-slosh as one primitive — the contacted fraction of a box's faces:

- `Bottom` is a hard **support gate**: ≥ this fraction of the −z face must rest
  on the floor or boxes below (enforced at placement, since support comes from
  already-placed items).
- `SideX` / `SideY` are soft **anti-slosh targets** on the lateral faces: a
  positive value makes placement prefer wall/neighbour contact, and a compaction
  pass (`d3.Compact` / `d2.Compact`) then slides items together to close the
  lateral gaps that let a load shift in transit. They're targets, not gates,
  because lateral neighbours usually arrive after a box is placed.

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

## Quick start

```go
factory := d1.NewFactory(10)
packer := offline.FirstFitDecreasing(factory)
result, err := packer.PackAll([]pack.Item{
    d1.NewItem("a", 6), d1.NewItem("b", 4), d1.NewItem("c", 5),
})
fmt.Println(result.BinsUsed())
```

## Web demo

```
cd cmd/webdemo && go run .   # then open http://localhost:8082
```

Pick a dimension and algorithm, add items (with scalars), set constraints, and —
for the ⚖ balanceable algorithms — add balance objectives. The right-hand panel
reports the sum / average / σ of every metric across bins (low σ = well balanced).

## Development

```
go build ./...
go test ./...
go vet ./...
```
