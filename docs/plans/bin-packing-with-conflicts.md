# Plan: Bin Packing with Conflicts — Clique-Seeded Construction & Lower Bound

**Status:** Proposed — not started. Captured for possible later implementation.
**Source:** Gendreau, Laporte & Semet (2004), "Heuristics and lower bounds for the bin
packing problem with conflicts", *C&OR* 31(3) (the clique-based bound + FFD seeding,
"H6" being clique-then-FFD); Sadykov & Vanderbeck (2013) and Muritiba et al. (2010) for
later exact/heuristic context. Background search: variable-sized BPPC LNS (*EJOR* 2023).

**Pros:** small, pure-Go, deterministic; reuses existing FFD + `Incompatible`; adds the
one thing the library lacks for conflict packing — a **lower bound** (so a conflict
solve can self-certify or report a real gap) — plus a better *construction order* than
generic FFD; the clique bound is a genuinely good idea with a clean algorithm.
**Cons:** **incremental** — it improves an already-handled capability rather than adding
a new one (the eligibility plan already flagged clique-GRASP as marginal); the win over
plain FFD-with-`Incompatible` is empirical and concentrated on conflict-dense instances;
needs an item-*pairwise* conflict representation, which the current category-pairwise
`Incompatible` doesn't natively give (§2); max-clique is NP-hard so the bound uses a
greedy clique heuristic, not exact cliques.

---

## 1. Why this exists / the use case

In **Bin Packing with Conflicts (BPPC)** some pairs of items may not share a bin —
a *conflict graph* `G` over items, pack into fewest bins with no in-bin edge. Real
shape: chemicals that can't co-ship, tasks that can't co-locate (noisy-neighbour /
security), foods by allergen/temperature, jobs needing exclusive resources.

The transferable algorithmic content (pure combinatorial, no LP):

- **Clique lower bound.** Any clique in `G` is a set of mutually-conflicting items, each
  needing its *own* bin — so `|clique| ≤ optimal bins`. Take the best clique found by a
  fast greedy (Johnson-style: repeatedly add the highest-degree vertex still adjacent to
  all chosen). Combined with the capacity bound `⌈Σsize/C⌉`, gives a real LB.
- **Clique-seeded FFD ("H6").** Seed the packing by putting each item of a large clique
  into a **distinct** bin first (they must split anyway), then FFD the remaining items —
  conflict-aware (skip a bin if the item conflicts with its contents). Consistently the
  best simple constructive heuristic in Gendreau et al.

## 2. Why the library does NOT already cover it

Conflict *feasibility* is expressible; the *bound* and *clique-aware order* are not, and
the conflict representation is a mismatch:

- **`Incompatible` is category-pairwise, not item-pairwise.** `Incompatible(catScalar,
  pairs…)` ([pack/incompatible.go:14](../../pack/incompatible.go)) forbids two **category
  values** from sharing a bin. An arbitrary item-pairwise conflict graph (item 3 vs item
  7, but item 3 fine with item 8) is only expressible by making **every item its own
  category** and listing every conflict edge as a pair — workable, but `O(edges)` pairs
  and awkward, and it loses the natural "conflict graph" view the clique algorithms want.
- **No lower bound for conflict packing.** The exact 1-D solver
  ([offline/bincompletion.go](../../offline/bincompletion.go)) computes only the capacity
  L1 bound and knows nothing of conflicts; the heuristics compute no bound at all. So a
  BPPC solve today can never say "this is optimal" or "gap = 2 bins" — the clique bound
  is the missing certificate, exactly as the [exact-1d-lower-bounds](./exact-1d-lower-bounds.md)
  plan adds capacity bounds for the conflict-free case.
- **Construction order is conflict-blind.** FFD ([offline/ffd.go:11](../../offline/ffd.go))
  sorts by size only; with an `Incompatible` constraint it *correctly refuses* a
  conflicting bin and opens a new one, but it never *plans* around the conflict structure
  (e.g. splitting a clique up front). On dense conflict graphs that yields excess bins.

### What IS already covered (reuse, do not reimplement)

- **Conflict feasibility** via `Incompatible` (per-bin rejection is correct; opening a new
  bin is always allowed, so no dead-end) — the gate the seeded FFD still relies on.
- **FFD machinery** (`offline.Wrapper`, `SortPolicy`) — the seeded heuristic is FFD with a
  conflict-aware bin filter + a clique-derived initial assignment.
- **Reserved-key stateful-constraint pattern** ([pack/incompatible.go:25](../../pack/incompatible.go))
  — the model for an item-pairwise conflict constraint if one is added (§4.1).

## 3. Explicitly OUT of scope (do not port)

- **Exact branch-and-price / set-partitioning BPPC** (Sadykov–Vanderbeck) — needs column
  generation / LP; the main module has none. Heuristic + bound only.
- **Large-neighbourhood-search / metaheuristic BPPC** (the 2023 variable-sized work) —
  redundant with existing `RuinRecreate`/`GRASP`/`BeamSearch`; the gap is the *bound* and
  *clique seeding*, not another search shell.
- **Exact maximum clique.** NP-hard; use the greedy clique heuristic (a valid LB is any
  clique, not necessarily the max) — keep it polynomial and deterministic.
- **Interval-graph / special-structure BPPC algorithms** — niche; the general greedy
  clique covers the common case.

## 4. Design

Two pieces; (A) the bound is the high-value reusable core, (B) the seeded heuristic is
the construction win. Both are pure combinatorial.

### 4.1 Conflict-graph representation

Decide the input shape (the one non-trivial design call):

- **Option A — item-pairwise constraint.** Add `ConflictGraph` carrying an adjacency set
  over item IDs, exposed as a `Constraint` that rejects a placement if the bin holds any
  conflicting item. Cleaner than abusing categories; mirrors the `Incompatible`
  reserved-key stateful pattern but keyed on item IDs present in the bin. This is the
  natural "conflict graph" the clique code consumes directly.
- **Option B — reuse `Incompatible` with item-as-category.** No new constraint, but
  `O(edges)` category pairs and an awkward caller experience.

Recommend **Option A** — a small `ConflictGraph` value (adjacency `map[string]set`) that
serves *both* the constraint (feasibility) and the clique routines (bound + seeding) from
one representation.

### 4.2 Clique lower bound (the reusable core)

```go
// LowerBoundConflicts returns max(capacity L1, greedy-clique size) — a valid lower
// bound on bins for a BPPC instance. Always ≤ optimum.
func LowerBoundConflicts(items []pack.Item, capacity float64, g ConflictGraph) int
```

Greedy clique: sort vertices by degree desc; grow a clique by adding the next vertex
adjacent to all current members; repeat from a few seeds (deterministic) and keep the
largest. `max(⌈Σsize/C⌉, bestClique)`. Pure, allocation-light, table-testable.

### 4.3 Clique-seeded conflict FFD (the construction)

```go
func ConflictFFD(factory pack.BinFactory, g ConflictGraph) offline.OfflinePacker
```

1. Compute a large greedy clique; open one bin per clique member and assign each (they
   must split). 
2. FFD the remaining items: for each, first-fit into the lowest-indexed bin it neither
   overflows nor conflicts with (conflict tested via `g`); else open a new bin.
3. Return the result; if `bins == LowerBoundConflicts(...)`, mark **proven optimal**.

### 4.4 Optimality reporting (the cross-cutting bonus)

Mirror the [exact-1d-lower-bounds](./exact-1d-lower-bounds.md) bonus: any conflict
heuristic whose bin count equals `LowerBoundConflicts` is provably optimal — callers get
a free certificate and a real gap to report.

## 5. Implementation steps (when picked up)

1. `pack`: `ConflictGraph` (adjacency over item IDs) + an item-pairwise conflict
   `Constraint` using the reserved-key stateful pattern; unit test that a conflicting
   placement is rejected and a non-conflicting one accepted.
2. `offline` (or `pack`): `LowerBoundConflicts` = `max(L1, greedyClique)`; property test
   `LB ≤ optimum` on random instances + hand-built fixtures (a `k`-clique forces ≥ `k`
   bins; assert the bound catches it).
3. `offline`: `ConflictFFD` — greedy-clique seeding + conflict-aware FFD. Test it uses
   ≤ as many bins as plain FFD-with-`Incompatible` on conflict-dense instances, and meets
   the LB (proven optimal) on small fixtures.
4. `offline`/`packapi`: when any conflict heuristic's bin count equals the bound, report
   proven-optimal (and optionally short-circuit an exact pass).
5. `packapi`: register `1d/conflict-ffd` (or a conflict flag on FFD) via `registerSolve`
   and advertise in `AlgoCapabilities()` (drift tests enforce both); request needs the
   conflict edge list. Conflict-aware FFD is incremental ⇒ candidate for `isStreamable`.
6. (If surfaced in the demo) `cmd/webdemo/static/index.html`: a way to declare conflict
   edges between items; flag UI for user verification.
7. `ATTRIBUTION.md` + doc comments: attribute Gendreau, Laporte & Semet (2004) for the
   clique bound + seeded FFD; note exact B&P deliberately not ported (no LP).

## 6. Risks / decisions to revisit

- **It's incremental, not a new capability.** The library already packs with conflicts
  (`Incompatible`); this adds a *bound* and a *better order*. Build only if conflict-dense
  instances are a real target and "is this optimal / how far off?" matters — otherwise
  FFD-with-`Incompatible` already produces feasible packings.
- **Representation split.** Two ways to express conflicts (`Incompatible` categories vs.
  the new item-pairwise `ConflictGraph`). Document when to use which: category exclusion
  for class-level rules ("no lighters with dynamite"), the graph for arbitrary
  item-pairwise edges. Avoid double-handling.
- **Greedy clique quality.** A weak clique gives a loose bound (still valid, just less
  useful). Multi-seed greedy is a cheap quality knob; exact max-clique is out (NP-hard).
- **Capacity + conflict interaction.** The LB is `max` of the two sub-bounds, not their
  sum — a clique can also be capacity-bound. Keep them independent and take the max; do
  **not** add them (would over-count and could exceed the optimum — a correctness bug, as
  in the DFF risk of the [exact-1d-lower-bounds](./exact-1d-lower-bounds.md) plan).
- **Is it worth it at all?** Lowest-novelty of the recent candidates. Strongest case is
  the *lower bound* (certificate + gap), which composes with the existing conflict
  packing for free; the seeded FFD is a modest constructive improvement on top. Ship the
  bound first; the seeded heuristic only if dense-conflict bin counts prove to need it.
