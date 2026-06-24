# Plan: Stronger Lower Bounds for the Exact 1-D Solver (L2 + DFF)

**Status:** Proposed — not started. Captured for possible later implementation.
**Source:** Martello & Toth (1990), *Knapsack Problems* (the L2 bound); Fekete &
Schepers (2001), dual-feasible functions (DFF). Motivated by da Silva, de Lima,
Schouery, Côté & Iori (2026), "Polynomial and Pseudopolynomial Algorithms for Two
Classes of Bin Packing Instances", arXiv:2604.05152 — see §3 for what we deliberately
do *not* take from it.

---

## 1. Why this exists / the use case

[offline/bincompletion.go](../../offline/bincompletion.go) is the only exact 1-D
solver. It starts the search at the **L1 bound** only:

```go
// bincompletion.go:64-69
lowerBound := int(math.Ceil(totalSize / binCapacity))
```

The outer loop then tries each `k` upward, *proving `k` infeasible by exhaustive
search* before incrementing ([line 82](../../offline/bincompletion.go)). A tighter
**starting** LB skips provably-impossible `k` values entirely and makes the first `k`
tried more likely to be the answer — both cut search dramatically on instances where
L1 is loose (few large items, awkward size mixes). The per-node pruning today is just
L1-style "remaining items fit in remaining capacity" + the incumbent bound
([lines 171-193](../../offline/bincompletion.go)).

Add the standard, pure-Go 1-D lower bounds:

- **Martello–Toth L2.** For a threshold `α ∈ [0, C/2]`, partition items into large
  (`> C−α`), medium (`(C/2, C−α]`), and small (`[α, C/2]`); a counting argument bounds
  bins below. `L2 = max_α L2(α)`, evaluated over the distinct small-item sizes.
  Dominates L1.
- **Dual-feasible functions (DFF).** A DFF `u` maps sizes→sizes such that
  `⌈Σ u(size) / u(C)⌉` is always a valid LB. Maximise over a parameter family (e.g.
  Fekete–Schepers `F⁰_k`). Often beats L2.

### Cross-cutting bonus

Expose the combined bound as a public helper. Then any heuristic (FFD, BFD, …) whose
bin count equals the LB is **provably optimal** — callers get a free optimality
certificate and a real gap to report ("FFD used 12 bins; LB is 11"), and the exact
solver can be short-circuited when a heuristic already meets the bound.

## 2. Why the library does NOT already cover it

Confirmed by sweep: the repo computes **only** the L1 bound, in
`BinCompletionCtx`. No L2, no Martello–Toth bounds, no dual-feasible functions, no
arc-flow / column-generation / set-partitioning LP relaxation (and none possible — the
main module has zero external dependencies and no LP machinery). The heuristics (FFD,
KK, metaheuristics) compute no lower bound at all, so none can self-certify
optimality.

## 3. Explicitly OUT of scope (do not port from arXiv:2604.05152)

The da Silva et al. paper is **diagnostic, not constructive** for this codebase. Do
**not** attempt to port:

- **The poly-time AI solver / pseudo-poly ANI solver.** They reverse-engineer the
  specific construction of the Caprara/Delorme AI & ANI benchmark instances
  (perfect-packing triplets, items `≥ W/2`, integer LP value). They do not generalise
  to arbitrary instances and would not strengthen `BinCompletion`.
- **The Multiplicity-Flow Formulation (MFF) / arc-flow DP.** Pseudo-polynomial in `W`
  and tied to the integer-weight CSP setting; the library is float-sized and
  count-minimising. Out of scope.
- **IRUP/MIRUP machinery and benchmark-class detection.** The library doesn't ship
  AI/ANI/triplet instances; there's nothing to special-case. (If
  [packapi/benchmarks.json](../../packapi/benchmarks.json) ever pulls in such
  instances, revisit — but that's a benchmarking decision, not a solver feature.)

The transferable lesson from that paper's *surroundings* — that L1 is weak and standard
stronger bounds exist — is exactly what this plan implements, via the classical
Martello–Toth / DFF bounds rather than anything instance-class-specific.

## 4. Design

### 4.1 Public helper (the reusable core)

```go
// LowerBound1D returns a valid lower bound on the number of bins of the given
// capacity needed to pack sizes. It is the max of L1, the Martello–Toth L2 bound,
// and the best dual-feasible-function bound. Always ≤ the true optimum.
func LowerBound1D(sizes []float64, capacity float64) int
```

Pure, allocation-light, trivially testable. Lives in `offline` (or a small
`offline/bounds.go`). No existing API changes.

### 4.2 Wiring into the exact solver

- **Root** ([bincompletion.go:69](../../offline/bincompletion.go)): replace the L1-only
  computation with `lowerBound := LowerBound1D(sortedSizes, binCapacity)`. This is the
  high-ROI, low-risk change: fewer `k` values to disprove from scratch, and the first
  `k` tried is more often the optimum.
- **Per-node (optional follow-up)**, after the feasibility prune
  ([line 182](../../offline/bincompletion.go)): a residual LB over not-yet-placed items
  plus the leftover capacities of partially-filled bins. Trickier — partial bins have
  arbitrary remainders, so the clean L2/DFF forms (which assume identical empty bins)
  don't apply directly. Ship the root change first; treat node-level as a separate,
  carefully-bounded follow-up.

Correctness is unaffected either way: the search remains exhaustive *from a valid LB*;
lower bounds only ever rule out impossible `k`.

### 4.3 Floating-point sizes

L2 and DFFs are textbook for **integer** sizes. The library uses `float64` volumes, so:

- compute bounds with a tolerance (the same spirit as `Remaining`/`Utilization`
  comparisons elsewhere), or
- scale sizes+capacity to integers when they're cleanly commensurable and fall back to
  L1 when they're not.

This is the main subtlety and a real correctness surface — a DFF applied with sloppy
rounding can over-count and produce an LB **above** the optimum, which would make the
exact solver return a wrong (too-large) `k` or loop. Tests must assert `LB ≤ optimum`
on every fixture.

## 5. Implementation steps (when picked up)

1. `offline`: `LowerBound1D` = `max(L1, L2, maxDFF)`. Start with L1 + L2; add one or
   two DFFs from the `F⁰_k` family once L2 is proven correct.
2. `offline`: property tests — for random instances, assert `LowerBound1D ≤
   BinCompletion's optimal bin count` **always**; assert `L2 ≥ L1` and `DFF ≥ L1`. Add
   hand-built fixtures where L1 is strictly loose (e.g. three items of size `0.6·C`:
   L1 = 2, optimum = 3) and check the bound tightens.
3. `offline`: wire `LowerBound1D` into `BinCompletionCtx`'s root LB; re-run the exact
   solver's existing tests with `-race` to confirm identical *optima* (only speed
   should change).
4. (Bonus) `offline` or `packapi`: when a heuristic's bin count equals `LowerBound1D`,
   mark/report it as proven-optimal; optionally short-circuit the exact solver in
   `auto`-style flows.
5. (Follow-up) per-node residual bound in `search`, gated behind its own tests.
6. `ATTRIBUTION.md` + doc comments: attribute Martello & Toth (1990) L2 and Fekete &
   Schepers (2001) DFF; note arXiv:2604.05152 as the motivation and explicitly that its
   AI/ANI algorithms were **not** ported and why.

## 6. Risks / decisions to revisit

- **Reach is modest.** This helps only the *exact* solver, which is already exponential
  and used for small/medium `n`. On easy IRUP instances L1 already equals the optimum,
  so the gain concentrates on adversarial size distributions the library may rarely
  see. It does **not** move the real scaling wall (the search itself); truly hard
  instances still time out — that would need arc-flow/B&P, which the no-deps rule
  forbids.
- **Float DFF correctness.** The single biggest risk: an LB that exceeds the optimum is
  a *correctness* bug, not just a slowdown. Guard with the `LB ≤ optimum` property test
  on every run and prefer conservative rounding (round bounds *down*).
- **Low risk to existing behaviour.** The root-LB change cannot alter which optimum is
  returned (still exhaustive from a valid LB) — so the blast radius is small and the
  payoff is purely speed + the reusable helper. Good candidate to ship even if Design 1
  (quadratic objective) never happens; the two are independent.
