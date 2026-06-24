# Plan: Extensible Bins with Overload Cost (GEBPOC)

**Status:** Proposed — not started. Captured for possible later implementation.
**Source:** Ding, Deng & Li (2022), "Meta-Heuristic Algorithms for the Generalized
Extensible Bin Packing Problem With Overload Cost", *IEEE Access* 10. Problem first
posed by Denton et al. (2010); see also Dell'Olmo et al. (extensible bin packing) and
the late-work-minimization scheduling literature.

---

## 1. Why this exists / the use case

GEBPOC is a **fixed-machine load-balancing** problem, not a pack-to-fewest-bins
problem. There are `m` identical bins (machines) fixed up front, **all** items must be
assigned, a bin's load is **allowed to exceed** its capacity `t` (regular working
time), and exceeding it costs a linear penalty:

```
min  m·t + c·Σ_i max(load_i − t, 0)
```

`m·t` is constant once `m` is chosen, so the live objective is **minimize total
overload (overtime) across a fixed set of machines**.

Real use cases (the paper's motivation): outpatient surgery / operating-room
scheduling (open K rooms, pay overtime), server task assignment, any
identical-parallel-machine setting where capacity is a soft threshold with an
overage penalty rather than a hard wall.

## 2. Why the library does NOT already cover it

Two axes are genuinely new:

1. **Extensible (soft-capacity) bins.** The library's `MaxAggregate`
   ([pack/scalar.go](../../pack/scalar.go)) is a hard wall — placement is rejected
   once the total would exceed the limit. GEBPOC needs a bin that *accepts* the item
   and *accrues a penalty* for the overage instead.
2. **Fixed bin count + overload-cost objective.** `gbpp` added a *bin-cost* objective
   ([gbpp/gbpp.go](../../gbpp/gbpp.go)), but its capacity is hard and it minimizes
   `cost − profit`, not `c·Σ max(load − t, 0)`. The library otherwise minimizes bin
   *count* (`meta.BestOf`) or a lexicographic metric stack (`meta.LexBestOf`).

### What IS already covered (do not reimplement)

- **LPT-into-m-machines** (the paper's baseline) ≈ `BalancedFit`
  ([offline/balanced.go](../../offline/balanced.go)): pre-opens K bins and distributes
  largest-first via `PreferenceFit` + `BalanceCount`.
- **VNS Move / Swap neighborhoods** ≈ `RefineBalance`
  ([offline/refine.go](../../offline/refine.go)): post-pack local search doing
  single-item moves and two-item (1-for-1) swaps, accepting any change that lowers an
  imbalance score, with feasibility re-checked by rebuilding bins.
- **Balancing preferences**: `ColocateLow` / `FillLow` / `BalanceCount`
  ([pack/scalar.go](../../pack/scalar.go)).
- **Metaheuristic bench**: `GRASP`, `RuinRecreate`
  ([offline/metaheuristic.go](../../offline/metaheuristic.go)), `BeamSearch`,
  `BruteForce`, `BinCompletion`, `KK`.

## 3. Explicitly OUT of scope (do not port)

- **IACO (improved ant colony) and MDPSO (discrete PSO).** Generic, borrowed,
  parameter-laden metaheuristics (α, β, ρ, Q, q₀, inertia w, c₁, c₂, swarm size…),
  stochastic and hard to make deterministic / `-race`-testable, and redundant with the
  existing GRASP / RuinRecreate / BeamSearch bench. The paper's own data shows they
  beat plain LPT only marginally (ratio ~1.01 vs ~1.02) at large runtime cost (seconds
  vs milliseconds; IACO ~7× slower than basic ACO). Bad fit for a deterministic,
  zero-dependency Go library.
- The paper optimizes a **surrogate fitness** (`max load + Σ|tᵢ − t|`, eq. 10) rather
  than the stated cost (eq. 1). If implemented, optimize the *real* overload cost.

## 4. Design

The clean part: this needs **no core-contract change** (unlike the eligibility plan).
It reuses existing assignment + local-search + lexicographic machinery and adds two
small pieces.

### 4.1 Soft-capacity (extensible) bin

A bin wrapper that **never rejects** on capacity — it always accepts the item and
tracks `load`. Expose the overage `max(load − t, 0)` as a metric (via `BinMetricer`,
mirroring how `ConstrainedBin` surfaces aggregates/metrics in
[pack/constrained.go:68-81](../../pack/constrained.go)). Hard constraints
(`Incompatible`, geometry, etc.) still apply if composed; only the *capacity* limit
becomes soft. Decide: a dedicated `ExtensibleBin`/`SoftCapBin` wrapper, or a soft
variant of `MaxAggregate` that records overage instead of rejecting.

### 4.2 Overload-cost metric

`c · Σ_i max(load_i − t, 0)` as a metric function for `meta.LexBestOf`
([meta/lexicographic.go](../../meta/lexicographic.go)). With `m` fixed, the fixed term
`m·t` is constant and can be dropped from the comparison (or included for reporting).
Lets the solver rank candidate assignments by true total overtime.

### 4.3 Fixed-K assignment + refinement (reuse)

Candidates = `BalancedFit` / LPT-style largest-first assignment into the fixed K bins,
refined by `RefineBalance`. **Caveat:** `RefineBalance` currently minimizes the
imbalance score (CV of count + scalars), which is a *proxy* for, not equal to, overload
cost. Either (a) parameterize `RefineBalance` with a pluggable objective so it can
minimize overload cost directly, or (b) add an overload-cost-driven refiner. (a) is
preferable — single local-search engine, swappable objective.

### 4.4 Aswap (asymmetric 2-for-1 swap) — folded in per discussion

The paper's third VNS neighborhood (Aswap) exchanges **two** items on one machine for
**one** on another. `RefineBalance` today has Move + 1-for-1 Swap but **not** Aswap.

**Recommendation: add Aswap only as part of THIS work, gated and benchmarked — not as a
standalone addition to the balancing refiner.** Rationale:

- It is the **most expensive neighborhood**: roughly `O(bins²·itemsᵢ²·itemsⱼ)`
  feasibility rebuilds, a factor of `items` beyond the 1-for-1 Swap tier, against the
  shared `refineEvalBudget = 40000` cap ([refine.go:16](../../offline/refine.go#L16)).
  Ungated, it can exhaust the budget on a rarely-firing scan before Move/Swap converge
  — potentially **net-negative** on budget-limited mid-size instances.
- Its payoff is **objective-dependent**: for the smooth CV imbalance score, Move+Swap
  already cover most of the landscape, so Aswap's marginal value is speculative. It
  earns its keep specifically against the **spikier overload-cost objective** with
  discrete jumps — i.e. exactly this plan's objective, not the existing balancer.

**How to add it:** a third local-search tier scanned **only when Move and Swap both
fail** in a pass (the current first-improvement / restart-on-improve structure makes
this natural). Validate on a benchmark that it lowers final overload cost **without
regressing** budget-limited instances. Note the general form is a **k-for-1** swap;
Aswap is the 2-for-1 special case — if generalizing, do it once with `k` a parameter.

## 5. Implementation steps (when picked up)

1. `pack`: soft-capacity bin wrapper (`ExtensibleBin`) that accepts unconditionally and
   exposes overage via `BinMetricer`. Unit tests for load/overage accounting.
2. `pack`/`meta`: overload-cost metric `c·Σ max(loadᵢ − t, 0)` usable from `LexBestOf`.
3. `offline`: fixed-K assignment entry point (reuse/extend `BalancedFit`), refined by
   `RefineBalance`.
4. `offline`: parameterize `RefineBalance` with a pluggable objective (default keeps
   current imbalance score; new path minimizes overload cost).
5. `offline`: add Aswap as a gated third tier (per §4.4) + benchmark proving no
   budget-limited regression.
6. `packapi`: expose the extensible/overload objective in the solve API; add a
   `packapi` test (per CLAUDE.md "Adding an algorithm" checklist — wire UI
   `ALGOS`/streamability only if surfaced in the demo).
7. `ATTRIBUTION.md` + doc comments: attribute GEBPOC / Ding, Deng & Li (2022) and
   Denton et al. (2010); note VNS (Mladenović & Hansen) for the neighborhood scheme.

## 6. Risks / decisions to revisit

- **Proxy vs. true objective.** Don't ship `RefineBalance`'s CV score as a stand-in for
  overload cost; parameterize the objective (step 4) so the local search optimizes the
  real cost.
- **Budget contention from Aswap.** The 40k rebuild budget is shared across tiers; Aswap
  must be gated and measured (§4.4) or it can degrade existing balancing results.
- **`RefineBalanceMaxItems = 80` cap.** Large GEBPOC instances (the paper goes to
  n=100+) exceed the refiner's size cap and would fall back to the raw assignment.
  Decide whether the overload path needs a higher cap or a cheaper neighborhood
  strategy at scale.
- **Is it worth it at all?** Only build if a concrete fixed-machines / soft-capacity /
  overtime-penalty use case exists (surgery/OR scheduling, server task assignment). The
  paper's *algorithms* (ACO/PSO) are not worth porting regardless; the value, if any, is
  the *problem model* (extensible bins + overload cost) plus Aswap for that objective.
