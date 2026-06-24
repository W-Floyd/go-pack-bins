# Plan: Vector (Multi-Resource) Bin Packing Heuristics

**Status:** Proposed — not started. Captured for possible later implementation.
**Source:** Panigrahy, Talwar, Uyeda & Wieder (2011), "Heuristics for Vector Bin
Packing" (Microsoft Research — the dot-product / norm-based family); Mommessin,
Perez-Salazar, Trystram et al. (2024), "Classification and evaluation of the algorithms
for vector bin packing", *Computers & Operations Research* 165:106860 (the modern
classification/benchmark). Background: Spieksma; Caprara & Toth on d-dimensional VBP.

**Pros:** pure combinatorial (no LP) and a clean fit — it's a new *selector* +
*ordering*, the exact extension points the library already exposes; large, real
application (cloud VM / container placement, multi-resource scheduling) the library
can't currently do well; reuses existing `MaxAggregate` stacking for feasibility, so
only the *scoring* is new; deterministic and `-race`-friendly.
**Cons:** distinct from *geometric* multi-D packing (could confuse users — needs clear
naming/docs); the win over plain per-scalar Best-Fit is empirical, not a proven ratio;
many heuristic variants exist (must pick a curated few, not ship a zoo); only as good
as the caller's demand-vector normalisation (heterogeneous resource scales need care).

---

## 1. Why this exists / the use case

**Vector Bin Packing (VBP)** is *not* geometric packing. Each item is a **demand
vector** `(w¹, w², …, w^d)` across `d` independent resources; each bin has a **capacity
vector** `(C¹, …, C^d)`; an item fits iff it fits in **every** dimension simultaneously
(`Σ loads + demand ≤ C` componentwise). Minimise bins. The dimensions are *abstract
resources*, not spatial axes — CPU/RAM/disk/bandwidth, weight/volume/value, etc.

The defining difficulty: an item that is "small" in one resource can be "large" in
another, so single-scalar fit rules pack badly. The literature's answer is **fit
measures that look at all resources at once**:

- **Dot-Product (DP):** score a (bin, item) pair by `Σ_k a_k · demand_k · residual_k` —
  reward items whose demand *aligns* with the bin's remaining capacity. Consistently
  among the best in both source papers.
- **L2-norm / "Norm-Based Greedy":** place to minimise the norm of the residual
  capacity vector after placement — pack so bins finish *balanced*, not lopsided.
- **Weighted FFD:** collapse each demand vector to one scalar via a weight function
  (sum, max, or resource-scarcity weights `a_k`), sort descending, then First-Fit.
  Simple, strong baseline; the weights are where the multi-resource intelligence lives.
- **Bin Balancing / "Fitness":** variants that track per-resource utilisation and steer
  toward even fill across resources.

Use cases (the library can't serve these well today): cloud/VM consolidation
(CPU+RAM+disk+net per VM), multi-commodity container loading (weight+volume+value),
multi-resource job-to-machine assignment.

## 2. Why the library does NOT already cover it

The **feasibility** half is expressible; the **intelligence** half is not:

- **Feasibility is already possible.** Stacking several `MaxAggregate("cpu", C¹)`,
  `MaxAggregate("ram", C²)`, … constraints
  ([pack/scalar.go:93](../../pack/scalar.go)) on a bin correctly enforces a vector
  capacity — an item is rejected unless it fits in *every* resource. So a caller *can*
  pack VBP instances today with FFD.
- **But every selector is single-scalar.** `FirstFit`/`BestFit`/`WorstFit`/`AlmostWorstFit`
  ([online/bf.go](../../online/bf.go), [online/ff.go](../../online/ff.go), …) and the
  offline FFD/BFD orderings rank bins/items by **one** scalar (the sort key /
  `Remaining`). None compute a *cross-resource* fit. Best-Fit on "total volume" is blind
  to the fact that a bin is CPU-full but RAM-empty — it will keep stuffing CPU-heavy
  items into a bin that can't take them, fragmenting the other resources.
- **`PreferenceFit` is the closest hook but still per-scalar.** The preference selectors
  (`pfSelector`/`pfNormSelector`, [online/preference.go](../../online/preference.go))
  score bins via `Preference`s like `FillHigh`/`ColocateLow` — each keyed to a *single*
  named scalar. There is no preference that scores the **joint** alignment of an item's
  demand vector with a bin's residual vector (the DP / L2 measures). That joint score is
  the entire content of VBP heuristics.

### What IS already covered (reuse, do not reimplement)

- **Vector feasibility** via stacked `MaxAggregate` constraints — the bin contract and
  `Remaining`/`TryPlace` already do componentwise rejection correctly.
- **The selector interface** `Select(bins, item) (Placement, int, error)`
  ([online/packer.go](../../online/packer.go)) — the exact plug-in point for a new
  vector-aware selector; no contract change.
- **Per-scalar utilisation** via `ScalarsOf` and the bin aggregate map — the raw
  material the DP/L2 scores consume.

## 3. Explicitly OUT of scope (do not port)

- **LP-rounding / iterated-randomised-rounding VBP approximations** (e.g. Bansal–Eliáš–Khan;
  the `(0.807·d)`-type results). They need an LP solver; the main module has none.
  This plan is **heuristic only**, matching the library's other packers.
- **Geometric reinterpretation.** Do not conflate VBP's `d` *resources* with 2-D/3-D
  *spatial* packing (`d2`/`d3`). They are different problems; VBP items have no shape, no
  rotation, no placement coordinates. Name the package/algos to make this unambiguous.
- **The full heuristic zoo.** The Mommessin et al. classification enumerates dozens of
  (measure × weight × tie-break) combinations. Ship a curated 2–3 (DP, L2-norm,
  weighted-FFD) that dominate the benchmarks; don't port the whole taxonomy.
- **ML / RL VBP placement** (cloud-scheduling DRL papers) — non-deterministic, not
  `-race`-testable, wrong fit for this library.

## 4. Design

No core-contract change. New scoring functions plugged into the existing selector and
ordering machinery.

### 4.1 API

A small `vbp` helper package (or fold into `online`/`offline` as new selector/ordering
constructors — decide during impl). The caller declares the resource scalar names and
per-resource capacities; the bin factory is built from stacked `MaxAggregate`.

```go
// Resource names define the demand/capacity vector dimensions, in order.
type Spec struct {
    Resources  []string   // e.g. {"cpu","ram","disk"}
    Capacities []float64  // bin capacity per resource (same length/order)
    Weights    []float64  // optional per-resource importance a_k; nil ⇒ scarcity-derived
}

// Vector-aware online selectors:
func DotProductFit(spec Spec) online.Selector  // maximise Σ a_k·demand_k·residual_k
func L2NormFit(spec Spec) online.Selector       // minimise ‖residual after placement‖₂

// Vector-aware offline ordering (weighted FFD):
func WeightedFFD(spec Spec) offline.OfflinePacker
```

The selectors return the best feasible (bin, placement); feasibility is delegated to the
bin's stacked `MaxAggregate` constraints, so a placement the score likes but that
violates a resource is simply skipped — score and feasibility stay decoupled.

### 4.2 Scoring details

- **Residual vector** for a bin = `Capacities − binAgg[resource]` per dimension (read
  from the aggregate map). **Demand vector** = `ScalarsOf(item)` projected onto
  `Resources`.
- **DP score** = `Σ_k a_k · demand_k · residual_k`; pick the **max** over feasible bins
  (open a new bin only if none feasible). Weights `a_k` default to **scarcity**
  (e.g. `a_k = total demand_k / capacity_k`, the standard normalisation) so resources at
  different scales are comparable — this normalisation is the single most important knob.
- **L2 score** = `‖residual − demand‖₂` after placement; pick the **min** (finish bins
  balanced). 
- **Weighted-FFD key** = `Σ_k a_k · demand_k`; sort items descending, First-Fit into the
  stacked-constraint bins.

### 4.3 Normalisation (the correctness/quality crux)

Resources live on different scales (GB vs. cores vs. Mbps). Raw dot products are
dominated by the largest-magnitude resource. Normalise demands/residuals by capacity
(work in *fractions of capacity*) before scoring, and derive default `a_k` from
aggregate scarcity. Document that callers supplying explicit `Weights` override this.

## 5. Implementation steps (when picked up)

1. `vbp`: `Spec` + a factory builder that stacks `MaxAggregate` per resource (vector
   feasibility), with a unit test proving componentwise rejection.
2. `vbp`: `DotProductFit` and `L2NormFit` selectors implementing `online.Selector`;
   scarcity-derived default weights + capacity normalisation. Unit tests on a
   hand-built 2-resource instance where per-scalar Best-Fit demonstrably wastes a bin
   and DP/L2 do not.
3. `vbp`: `WeightedFFD` offline packer (weighted scalar key → FFD). Test it beats
   single-scalar FFD on a skewed-resource instance.
4. `vbp`: benchmark vs. plain FFD/BF on synthetic VBP instances (mirror the Mommessin
   et al. setup: correlated vs. anti-correlated resource demands) — report bins + per
   resource fill. Confirm DP/L2 win on anti-correlated, tie on correlated.
5. `packapi`: register `vbp/dp`, `vbp/l2`, `vbp/wffd` (or one `vbp` algo with a
   measure tunable) in [packapi/algos_1d.go](../../packapi/algos_1d.go) via
   `registerSolve` and advertise in `AlgoCapabilities()` (drift tests enforce both).
   Request needs resource names + capacities + optional weights. DP/L2 are online/
   incremental ⇒ candidates for `isStreamable`; WeightedFFD sorts first ⇒ not.
6. (If surfaced in the demo) `cmd/webdemo/static/index.html`: multi-resource input
   (resource columns per item + per-resource capacity). Flag UI for user verification.
7. `ATTRIBUTION.md` + doc comments: attribute Panigrahy et al. (2011) for DP/norm-based
   and Mommessin et al. (2024) for the classification/benchmark; state heuristic-only.

## 6. Risks / decisions to revisit

- **Naming collision with geometric multi-D.** The library is *spatial* 2-D/3-D in most
  users' minds; "vector / multi-resource" must be unmistakable in package name, algo
  ids, and docs, or callers will reach for `d3` expecting this and vice-versa.
- **Normalisation is the whole ballgame.** A DP/L2 score on un-normalised, differently
  scaled resources is worse than plain FFD. The scarcity-weight default must be solid
  and documented; expose explicit `Weights` for callers who know their scales.
- **No proven guarantee.** These are empirical heuristics; report bins + fill, not a
  ratio. (The proven-good VBP results are LP-based and out of scope.)
- **How much to ship.** DP is the single best all-rounder in both sources; if effort is
  tight, ship `DotProductFit` + `WeightedFFD` only and defer L2.
- **Is it worth it at all?** Strong yes *if* multi-resource placement is a target use
  case (cloud/VM, multi-commodity loading) — it's a genuine capability gap with a clean
  implementation path. If callers only ever constrain one real scalar (volume) plus
  incidental others, the existing per-scalar packers may suffice; build when a true
  vector instance appears.
