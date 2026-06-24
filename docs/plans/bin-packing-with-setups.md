# Plan: Bin Packing with Setups (BPPS) — Two-Phase Approximation

**Status:** Proposed — not started. Captured for possible later implementation.
**Source:** Baldacci, Ciccarelli, Dose, Coniglio & Furini (2025), "A first
approximation algorithm for the Bin Packing Problem with Setups", arXiv:2512.24785.
Problem first introduced by Baldacci, Ciccarelli, Coniglio, Dose & Furini (2025),
"The Bin Packing Problem with Setups: Formulation, Structural Properties and
Computational Insights", Optimization Online.

**Pros:** small self-contained package; reuses existing FFD/BFD; ships a *proven*
3-approximation (rare for this library — most algos here have no guarantee); fills a
real modelling gap the scalar contract can't express; no core-contract change; easy to
test (clean corners + the paper's worst-case families). Lowest effort-to-value of the
shelved papers.
**Cons:** 1-D only (setup-weight-eats-capacity is inherently scalar); BPPS is its own
objective so it composes with nothing else; merge-order is a quality knob the proof
doesn't pin down; needs a use case with genuine per-class setup cost/weight to justify
building at all.

---

## 1. Why this exists / the use case

BPPS partitions items into **classes**. Each class `c` carries a **setup weight**
`s_c` and a **setup cost** `f_c`. If a bin contains at least one item of class `c`,
that class is *active* in the bin: the bin's usable capacity drops by `s_c` and the
objective gains `f_c`. Objective (minimise total cost):

```
min  r·(bins used)  +  Σ_bins Σ_{c active in bin} f_c
```

subject to, per bin, `Σ item weights + Σ setup weights of active classes ≤ d`.

Real instances of this shape:

- **production / cutting** with a per-class machine setup (tooling change, die,
  colour) that consumes both capacity (warm-up scrap, a header) and a fixed cost
  every time a class appears in a run,
- **logistics** where mixing a product family into a container needs a divider,
  liner, or paperwork (capacity + fixed handling cost per family per container),
- **shipping manifests** where each hazard/temperature class active in a container
  incurs a fixed compliance cost and reserved space.

The paper's contribution is an **approximation algorithm with a proven guarantee**:
run any α-approximation BPP algorithm class-by-class, then merge bins. The result is
a **2α-approximation**; with FFD/BFD (α = 3/2) it is a **3-approximation** (Thm 1 /
Cor 2). It also shows (Props 1–2, Cor 1) that naïvely adapting NF/FF/BF/FFD/BFD to
account for setups has **unbounded** worst-case ratio — so the two-phase structure is
the point, not just "reuse FFD".

## 2. Why the library does NOT already cover it

The setup model is genuinely new and is **not** expressible with existing scalars:

- **Capacity is a sum of per-item scalars.** `MaxAggregate`
  ([pack/scalar.go:93](../../pack/scalar.go)) enforces `Σ itemScalar ≤ limit`. A setup
  weight is charged **once per class present in the bin**, not per item — it is a
  step function of *which classes are present*, not a sum. No per-item scalar encoding
  reproduces "first class-`c` item costs `s_c` capacity, subsequent ones cost 0".
- **The objective is bins + per-bin-per-class fixed costs.** The library minimises bin
  *count* (`meta.BestOf`) or a lexicographic metric stack (`meta.LexBestOf`); `gbpp`
  adds a per-item profit and a flat bin cost ([gbpp/gbpp.go](../../gbpp/gbpp.go)).
  None model a fixed cost incurred per *active class per bin*.
- **`AllSame` is only the single-class-per-bin corner.** `AllSame("class")`
  ([pack/scalar.go:121](../../pack/scalar.go)) forces every bin to hold one class — that
  is exactly Phase 1's class-by-class packing, but BPPS *allows mixing* classes in a
  bin and paying multiple setups. The whole value of BPPS over "one class per bin" is
  the **merge** that trades extra setup costs for fewer bins. `AllSame` cannot express
  the mixed-class bins the merge produces.

### What IS already covered (reuse, do not reimplement)

- **Phase 1 per-class packing** = `offline.FirstFitDecreasing` / `BestFitDecreasing`
  run on each class's items with a reduced capacity `d − s_c`
  ([offline/ffd.go](../../offline/ffd.go)). These are the α = 3/2 algorithms the proof
  instantiates.
- **Capacity feasibility** via the bin factory / `Remaining` — a Phase-1 bin of class
  `c` is built against a `MaxAggregate("weight", d − s_c)` bin.

## 3. Explicitly OUT of scope (do not port)

- **The ILP formulation and its LP relaxation / valid inequalities** from the
  companion paper [1] (the 1/2-ratio lower bound work). The main module has no
  LP/MILP machinery; out of reach and not needed for the heuristic.
- **Chasing a tighter ratio than 3.** The paper stops at 2α and notes 3/2 is
  best-possible for BPP itself unless P = NP. A 3-approximation with a clean proof is
  the deliverable; don't over-engineer.

## 4. Design

This needs **no core-contract change**. It is a thin new package that orchestrates
existing offline packers plus a merge pass — the cleanest of the shelved plans.

### 4.1 Data model & API

New package `setups` (dimension-agnostic in spirit, but **1-D weight only** to start —
the setup-weight-eats-capacity model is scalar; see §6):

```go
type Class struct {
    ID        string
    SetupWeight float64 // s_c — capacity consumed when this class is active in a bin
    SetupCost   float64 // f_c — fixed cost added per bin this class is active in
}

type Options struct {
    BinCapacity float64        // d
    BinCost     float64        // r
    Classes     []Class        // s_c, f_c per class
    ClassOf     func(pack.Item) string // maps item → class ID
    Base        offline.OfflinePacker  // the α-approx BPP packer; default FFD
}

type Result struct {
    pack.Result          // final bins (mixed-class), placements
    Objective    float64 // r·bins + Σ active-class setup costs
    BinCostTotal float64
    SetupTotal   float64
}

func Pack(ctx context.Context, items []pack.Item, opts Options) (Result, error)
```

### 4.2 Algorithm (faithful to Thm 1)

**Phase 1 — class-by-class.** For each class `c`, build a bin factory with capacity
`d − s_c` (reject if any item has `w_i + s_c > d` — the paper's feasibility
assumption) and run `opts.Base` (FFD by default) on just that class's items. Union the
results into `B1`. Every `B1` bin is single-class, so its load including the one setup
weight is `≤ d`.

**Phase 2 — merge.** Starting from `B1`, repeatedly find two distinct bins `B, B'`
whose **combined load** `ℓ(B ∪ B')` — items plus the *union* of active-class setup
weights — is `≤ d`, and merge them. Stop when no feasible merge remains; output `B2`.
Load and cost are defined exactly as in the paper:

```
ℓ(S) = Σ_{i∈S} w_i + Σ_{c active in S} s_c
κ(S) = r        + Σ_{c active in S} f_c
```

**Merge order matters for quality, not validity.** The proof's 2α bound holds for *any*
maximal merge. A sensible greedy (e.g. merge the pair that best fills a bin, or
first-fit over `B1` bins) improves the practical objective; document that the bound is
order-independent but the realised cost is not.

### 4.3 Objective convention

Keep the library's "lower is better": `Objective = BinCost·|bins| + Σ_bins Σ_{active}
f_c`. Recompute exactly from the final `B2` at the end (don't trust an incremental
running total). This composes with nothing else by default — BPPS is its own objective
— but mirrors `gbpp`'s reporting shape (`Objective`, plus the two component totals).

### 4.4 The merge needs class-aware bin state

The merge feasibility test (`ℓ(B ∪ B') ≤ d`) is the one non-trivial bit: it must know
the **set of active classes** in each bin, not just a scalar sum, because two bins of
the *same* class merge without adding a second `s_c`, while two different classes pay
both. Track active-class sets per bin during the merge as plain Go data structures
(`map[string]struct{}` per bin) — this lives entirely inside `setups`, never touches
the `pack.Constraint` contract. Phase-1 bins are single-class so the initial sets are
trivial.

## 5. Implementation steps (when picked up)

1. `setups`: `Class`/`Options`/`Result` + the exact objective/load evaluator
   (`ℓ`, `κ`) with a hand-checked tiny instance unit test.
2. `setups`: Phase 1 — per-class FFD/BFD against capacity `d − s_c`; reject infeasible
   items (`w_i + s_c > d`) up front per the feasibility assumption.
3. `setups`: Phase 2 — greedy maximal merge with class-aware load; assert the realised
   objective ≤ Phase-1 objective (merging never increases cost — Thm 1 proof).
4. `setups`: tests for the parameter corners — `s_c = f_c = 0` ⇒ behaves like plain
   BPP on the union; one class only ⇒ identical to FFD with capacity `d − s`; the
   paper's NF/FF worst-case families (Props 1–2) to confirm the two-phase result stays
   within 3× of a hand-computed optimum.
5. `packapi`: register a `1d/setups` solver in
   [packapi/algos_1d.go](../../packapi/algos_1d.go) via `registerSolve` and advertise
   it in `AlgoCapabilities()` (per CLAUDE.md "Adding an algorithm" — the drift tests
   enforce both). Needs request fields for class assignment + per-class `s_c`/`f_c`.
   **Not** stream-incremental (the merge is global) — do **not** add to `isStreamable`.
6. (If surfaced in the demo) `cmd/webdemo/static/index.html`: way to tag items with a
   class and supply per-class setup weight/cost. Flag for user UI verification.
7. `ATTRIBUTION.md` + package doc: attribute BPPS / Baldacci, Ciccarelli et al. (2025);
   note the 2α-approximation result and that the ILP work [1] was deliberately not
   ported (no LP machinery).

## 6. Risks / decisions to revisit

- **1-D only.** The setup *weight* eating bin capacity is inherently a scalar (1-D)
  notion. A 2-D/3-D analogue (a setup *area/volume* reservation, e.g. a divider) is
  plausible but the paper is 1-D and the clean 2α proof is 1-D; keep this 1-D and
  revisit a geometric setup later if a use case appears.
- **Merge-order heuristic.** The proof guarantees 2α for any maximal merge, so a poor
  order is *correct* but leaves objective on the table. Pick one documented greedy
  (recommend first-fit over `B1` bins sorted by descending load) and note it's a
  quality knob, not a correctness one.
- **Relationship to `gbpp` and `AllSame`.** Be explicit in docs that BPPS ≠ `gbpp`
  (per-item profit) and ≠ `AllSame` (single-class bins). The genuinely new ingredient
  is the *per-active-class-per-bin* cost+capacity, realised by the merge.
- **Is it worth it at all?** High ROI relative to effort — small self-contained
  package, reuses FFD, ships a *proven* 3-approximation, fills a real modelling gap
  (per-class setup) the scalar contract can't express. The strongest candidate among
  the recently-reviewed papers to actually build.
