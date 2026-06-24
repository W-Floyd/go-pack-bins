# Plan: Soft Pairwise (Quadratic) Bin Packing Objective

**Status:** Proposed — not started. Captured for possible later implementation.
**Source:** Chagas, Locatelli, Miyazawa & Iori (2026), "The Quadratic Bin Packing
Problem: Exact Formulations and Algorithm", arXiv:2604.03078. (QBPP.)

---

## 1. Why this exists / the use case

The QBPP generalises classic BPP by adding, on top of a fixed per-bin cost `α`, a
**pairwise term `d_ij`** charged whenever items `i` and `j` are packed into the same
bin (`d_ij > 0` = penalty, `d_ij ≤ 0` = profit). Objective:

```
minimize  α·(bins used)  +  Σ_{i<j co-packed} d_ij
```

It unifies several problems as parameter corners:

- **BPP**: `α = 1`, `d_ij = 0`.
- **Bin packing with conflicts (BPPC)**: `d_ij = +∞` for forbidden pairs.
- **Capacitated clustering / cluster analysis**: `α = 0`, fixed bin count — group
  similar objects into capacity-limited clusters (the paper's headline motivation).

Real instances of this shape:

- cluster analysis: group similar customers / SKUs / records into capacity-limited
  clusters,
- soft co-location: reward shipping correlated SKUs together (`d < 0`) or penalise
  mixing product families / temperature zones (`d > 0`) without an outright ban,
- mild incompatibilities: chemicals you'd *pay* to separate but aren't legally
  forbidden — a finite-penalty version of the manifest rule,
- event seating / team formation (QMKP-adjacent): seat affinities within table
  capacity.

## 2. Why the library does NOT already cover it

The repo has the **linear** half of QBPP and the **infinite-penalty** corner, but
nothing in between:

- [gbpp](../../gbpp/gbpp.go) models `α·bins − Σ profit_i`: a fixed bin cost plus a
  **per-item** (linear) profit. No pairwise term.
- [pack/incompatible.go](../../pack/incompatible.go) `Incompatible(catScalar, pairs…)`
  is the only item-relational mechanism. It is the `d_ij = +∞` corner, but expressed
  as a **hard, category-based** constraint — it can *forbid* co-packing, not *price*
  it.
- The root blocker: every `Constraint`/`Preference` is built on the assumption that a
  bin's state is a **sum of independent per-item scalars** (`map[string]float64`
  aggregates — see [pack/scalar.go](../../pack/scalar.go)). A pairwise term breaks that
  additivity: the objective depends on *which* items are co-resident, not on a scalar
  sum. No scalar encoding reproduces an arbitrary `d_ij` matrix.

### Key observation that bounds the value

The constructive packers and the existing metaheuristics in
[offline/metaheuristic.go](../../offline/metaheuristic.go) operate on an item
**ordering**, decode it with First-Fit (`packOrdering`, line 60), and score with
`resultScore{unplaced, bins, fill}` (line 23). A pairwise term is about **which items
end up grouped**, which First-Fit decoding neither sees nor optimises. So this cannot
be bolted onto the ordering-based search — it needs an **assignment-based** search.
That is the bulk of the work, not the objective evaluation.

## 3. Explicitly OUT of scope (do not port)

- **The three compact MILP formulations (FFGW / 2A / R) and their enhanced variants,
  and the set-partitioning formulation + Branch-and-Price.** All are solved with CPLEX
  in the paper. The main module forbids external dependencies and has no LP/MILP
  machinery (no simplex, no column generation, no arc-flow — confirmed absent). Porting
  an exact QBPP solver is therefore off the table; this plan is **heuristic only**.
- **Symmetry-breaking inequalities, RLT strengthening, valid inequalities.** These are
  artefacts of the MILP formulations; irrelevant without a MILP.
- **The GQKP pricing problem (MCH heuristic + combinatorial B&B).** Only meaningful
  inside column generation, which we are not building.
- **Exact optimality / lower bounds for the quadratic objective.** Out of reach in pure
  Go at useful sizes; we report objective value, not a gap.

## 4. Design

### 4.1 Data model & API

New package `quad` (mirrors `gbpp`'s shape — dimension-agnostic, reads any
`pack.BinFactory`):

```go
// Sparse, symmetric pairwise costs keyed by item ID. Missing pair => 0.
// Sparse matters: the paper's instances have density δ; most pairs are zero.
type PairCosts struct{ m map[[2]string]float64 } // canonical a<b key ordering
func NewPairCosts() *PairCosts
func (p *PairCosts) Set(a, b string, cost float64) // math.Inf(1) for a hard conflict
func (p *PairCosts) Cost(a, b string) float64

type Options struct {
    BinCost      float64    // α
    Pairs        *PairCosts // d_ij
    ProfitScalar string     // optional linear term (QBPP ∪ GBPP); "" = none
    offline.SearchOptions   // Seed, MaxIters, Deadline, Progress, Snapshot — reused
}

type Result struct {
    pack.Result
    Objective     float64 // α·bins + pair total − profit
    BinCostTotal  float64
    PairCostTotal float64
}

func Pack(ctx context.Context, items []pack.Item, factory pack.BinFactory, opts Options) Result
```

### 4.2 Algorithm (assignment-based local search)

1. **Seed**: capacity-feasible start via existing
   `offline.FirstFitDecreasing(factory)` — valid assignment, ignores pairwise cost.
2. **Local search over assignments**, reusing the acceptance machinery already in
   `AdaptiveRuinRecreate` (record-to-record-travel acceptance, adaptive ruin
   magnitude, `ctx`/`Deadline` honouring — [offline/metaheuristic.go:224](../../offline/metaheuristic.go)):
   - *relocate*: move item `i` from bin B to a feasible bin C (or a new bin),
   - *swap*: exchange `i∈B` and `j∈C` if both stay feasible,
   - *ruin-and-recreate*: tear out `k` items, re-insert greedily by **pairwise-aware**
     marginal cost.
3. **Incremental delta** keeps moves cheap: removing `i` from B saves
   `Σ_{j∈B} d_ij`; inserting into C costs `Σ_{j∈C} d_ij`; a bin-count change
   contributes `±α`. With the sparse map this is O(nonzeros touched), not O(|bin|²).
4. **Feasibility** via `bin.TryPlace`/`Remaining`, so it is dimension-agnostic (1-D /
   2-D / 3-D bins all work). A `+∞` pair cost is treated as hard infeasibility (or
   delegated to an `Incompatible` constraint layered on the bins).
5. Return the best assignment; **recompute the objective exactly** at the end to guard
   against incremental-delta drift.

### 4.3 Objective sign convention

Keep `gbpp`'s "lower is better" net-cost convention so the two compose:
`Objective = BinCost·bins + Σ d_ij − Σ profit_i`. Document that negative `d_ij`
(co-location profit) and the optional linear `profit` scalar both *reduce* the
objective, and that the two are independent terms (a pair profit is earned once per
co-resident pair; a linear profit once per packed item).

## 5. Implementation steps (when picked up)

1. `quad`: `PairCosts` (sparse, symmetric, canonical key) + `Options`/`Result` + the
   exact objective evaluator (`func objective(bins, pairs, profit) (float64, …)`),
   with a focused unit test on a hand-checked tiny instance.
2. `quad`: assignment-based local search (`relocate`/`swap`/ruin-recreate) with
   incremental deltas; reuse `offline.SearchOptions` and the RRT acceptance helpers.
   Honour `ctx` and `Deadline` (sample `ctx.Err()` off the hot path, per the repo's
   cancellation convention).
3. `quad`: tests for the parameter corners — `α=1, d=0` ⇒ behaves like bin
   minimisation; `d=+∞` ⇒ never co-packs the conflicting pair; `α=0` ⇒ pure
   clustering groups low-`d` items together. Cross-check objective monotonically
   non-increasing across accepted moves.
4. `packapi`: wire as a new algorithm (per CLAUDE.md "Adding an algorithm": case in
   `pack1D`/`pack2D`/`pack3D`, `packapi` test). It is **not** stream-incremental
   (grouping is global), so it emits one batched frame like `auto`/`gbpp` — do **not**
   add it to `isStreamable`.
5. (If surfaced in the demo) `cmd/webdemo/static/index.html`: add to `ALGOS`; provide a
   way to supply the pair matrix (likely a sparse edge list in the request payload).
   Flag the UI change for the user to verify (per CLAUDE.md — UI isn't covered by Go
   tests).
6. `ATTRIBUTION.md` + package doc comment: attribute QBPP / Chagas et al. (2026); note
   that only the objective is taken, and the solution method is an original heuristic,
   not the paper's exact B&P.

## 6. Risks / decisions to revisit

- **No optimality guarantee.** This is a metaheuristic; it reports an objective value,
  not a bound or gap. If a caller needs proven-optimal QBPP, this plan does not deliver
  it and (given the no-deps rule) nothing in pure Go realistically will at useful
  sizes. Set expectations in the doc comment.
- **Matrix specification cost.** Dense `d_ij` is O(n²) to supply and store. The sparse
  map mitigates storage, but a dense instance is still a caller burden and a memory
  concern at large n — document the expectation that `d` is sparse (density `δ`, as in
  the paper).
- **Incremental-delta correctness.** The relocate/swap deltas are the most bug-prone
  part. The end-of-run exact recompute is a cheap safety net; also assert in tests that
  the incremental running objective equals a from-scratch recompute after every
  accepted move (debug build / test-only check).
- **Interaction with `Incompatible`.** Two ways to express a hard conflict (`d=+∞` vs.
  an `Incompatible` constraint on the bins). Pick one canonical path to avoid
  double-handling; recommend `d=+∞` inside `quad` and document that the constraint
  layer is for callers already using it.
- **Is it worth it at all?** Build only if a concrete grouping/affinity use case exists
  (clustering, soft co-location). If the need is a *hard* ban only, `Incompatible`
  already covers it and this plan should stay on the shelf.
