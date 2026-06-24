# Plan: Hierarchical 2-D Rectangle Packing (Free Container Dims, N Levels)

**Status:** Proposed — not started. Captured for possible later implementation.
**Source:** Grus, Hanzálek, Artigues, Briand & Hebrard (2026), "Hierarchical Rectangle
Packing Solved by Multi-Level Recursive Logic-based Benders Decomposition",
arXiv:2512.20239. (2DHRP.)

**Pros:** §4.A (free-dimension "smallest enclosing rectangle" / `W+H` minimisation) is
an independently useful, modest-effort capability the library lacks today; reuses the
existing `d2` packers and the new `sat` solver as oracles; maps onto a real extension of
nested mode (logistics nesting); gives a free optimality-gap report via `LB_{W+H}`.
**Cons:** large total scope — the hierarchy machinery (§4.B) is a big build; the paper's
exact methods (MILP/CP) are out of reach (no solver), so the default path is heuristic
with no optimality guarantee; float-vs-integer dimension discipline is a real
correctness surface; the common cartons→pallets case is *already* served by fixed-size
nested mode, so §4.B needs a concrete deep / free-dimension use case to be worth it.

---

## 1. Why this exists / the use case

2DHRP packs 2-D rectangles into a tree of **block types**. A block type's items are
either fixed rectangles or **occurrences of a child block type**, whose dimensions are
themselves the *solution* of packing that child. Two things distinguish it from the
library's current 2-D / nested modes:

1. **Container dimensions are free.** Unlike strip packing (fixed width) or bin packing
   (fixed bins), the block type's width *and* height are decided by the solver, and the
   objective minimises the top block's **half-perimeter `W + H`** (a square-favouring
   proxy for area; the paper finds it beats minimising area directly).
2. **Arbitrary depth + reuse.** The hierarchy is an out-tree of any depth (the paper
   goes to 7 levels). Every occurrence of a block type must be packed *identically*
   (reuse constraint from IC design) — so each block type is solved once and its chosen
   dimensions propagate to every parent that instantiates it.

Use cases (the paper's motivation): analog IC layout (op-amps inside larger blocks),
facility layout (rooms inside buildings), and **logistics nesting** (items → boxes →
larger boxes → container) — the last maps directly onto why this library already has a
nested mode.

The transferable algorithmic content (the parts implementable here):

- a **Bottom-Up** baseline (solve leaf blocks first, pass dimensions up as item
  variants), and
- a **recursive Logic-based Benders Decomposition (LBBD)** heuristic where a parent
  "master" approximates each child by an **area lower bound** `W'·H' ≥ LB_area`,
  proposes child dimensions, the child verifies by actually packing, and **cuts**
  (`W' ≤ W_plan ⇒ H' ≥ H_act`) refine the parent's view until consistent.

## 2. Why the library does NOT already cover it

The library has the *ingredients* but not this problem:

- **Nested mode is exactly two levels and fixed-size.** `PackNested` /
  `NestedLevelSpec` ([packapi/packapi.go:2170](../../packapi/packapi.go)) is cartons →
  pallets: each level has a fixed `Bin` size (or a catalog of fixed `Containers`). It
  does **not** support arbitrary depth, and the container size is chosen from a fixed
  catalog, never *minimised as a free continuous decision*.
- **No free-dimension / minimise-perimeter objective.** The 2-D packers
  ([d2/maxrects.go](../../d2/maxrects.go), guillotine, skyline, shelf) all pack into a
  **given** bin and the library minimises bin *count*. There is no "find the smallest
  enclosing rectangle for these items" mode and no `W + H` objective.
- **No identical-reuse propagation across levels.** Nested mode feeds each carton's
  *actual* chosen dims up to the pallet level, but there is no notion of "block type B
  is solved once and reused everywhere", nor a tree of types.

### What IS already covered (reuse, do not reimplement)

- **The single-block packing subproblem** — "do these rectangles fit a given W×H, and
  how?" — is precisely what `d2` MaxRects/Guillotine/Skyline/Shelf do. They become the
  per-block solver the decomposition calls (the paper's role for its MILP/CP solver).
- **Strip-packing-style "minimise height for a fixed width"** is achievable today by
  binary-searching height with the existing 2-D packers against a fixed-width bin.
- **The new SAT exact 2-D solver** ([sat package](../../docs/plans/sat-exact-2d.md))
  can serve as the *exact* single-block oracle for the exact LBBD variant (§4.3).

## 3. Explicitly OUT of scope (do not port)

- **The monolithic MILP and CP formulations** (M-MILP, M-CP — §3.2/§3.3 of the paper,
  solved with Gurobi / CP Optimizer). The main module has no MILP/CP solver and won't
  gain one. The decomposition is built *around* the library's own 2-D packers instead.
- **The exact LBBD as the default.** The paper itself shows exact LBBD can't finish one
  top-level iteration on a 4-level instance; it ships the **heuristic** LBBD for
  everything real. Build the heuristic version; treat exact as an optional oracle swap
  (§4.3) gated on small instances only.
- **IC-specific objectives/constraints** (interconnect length, non-uniform minimum
  spacing — paper §7). Orthogonal to this library; skip.
- **The `g^{i,x}` topological-ordering MILP strengthening** (eqs 21–25) — an artefact of
  the relative-position MILP, irrelevant without a MILP.

## 4. Design

Two separable deliverables. (A) is independently useful and a prerequisite; (B) is the
paper's actual contribution.

### 4.A Free-dimension single-block packing ("smallest enclosing rectangle")

The missing primitive: given rectangles (each possibly with size **variants** /
rotations) and an optional max-width, find a small enclosing W×H minimising `W + H`
(or minimise H for a fixed W = strip packing). Implement as:

- a **min-area / min-half-perimeter** wrapper over the existing `d2` packers: sweep
  candidate widths (the paper sweeps a range of aspect ratios from the area lower
  bound), pack each with a fixed-width strip variant, keep the best `W + H`;
- seed with the cheap heuristics the paper uses — **bottom-left** (Chazelle) and
  **best-fit** (Imahori–Yagiura) — which are simple constructive packers worth adding
  to `d2` regardless;
- lower bound `LB_area = Σ areas`, `LB_{W+H} = 2·√LB_area` (paper eqs 45–46) — a free
  optimality-gap report for the single-block result.

This alone is a useful new capability ("pack these boxes into the tightest pallet
footprint"), independent of the hierarchy.

### 4.B Recursive decomposition over a block-type tree

Data model — a new package `hpack` (or extend nested mode; see §6):

```go
type BlockType struct {
    ID         string
    Rectangles []Rect          // fixed items, each with variant list
    Children   []Occurrence    // block-type occurrences (with multiplicity)
}
type Occurrence struct{ TypeID string; Count int }
```

**Bottom-Up baseline (ship first).** Process block types in reverse topological order.
For each, solve the free-dimension single-block problem (§4.A), generating **N width
variants** (different aspect ratios) per non-root block; a parent treats a child
occurrence as a rectangle that must pick one of the child's variants (all occurrences
of a type share the chosen variant). Root minimises `W + H`. This is a faithful port of
the paper's BU baseline and needs only §4.A + topological traversal + variant bookkeeping.

**Heuristic LBBD (the contribution).** Recursive procedure per block type:

1. **Master**: pack this block's fixed rectangles + child occurrences, where each child
   occurrence's dims `(W', H')` are *free* variables constrained only by the area lower
   bound `W'·H' ≥ LB_area(child)` (a hyperbola; approximate by a few breakpoints).
   Minimise `H` subject to `W ≤ W_parent` (or `W + H` at the root).
2. **Verify**: for each child, recurse — actually pack the child into the proposed
   `W'_plan` (minimise its height). Get the real `H'_act`.
3. **Cut**: if `H'_act > H'_plan`, add `W' ≤ W'_plan ⇒ H' ≥ H'_act` to the master and
   re-solve. If the child didn't fit at `W'_plan` at all, add `W' > W'_plan`. Iterate
   until all children fit their proposed dims (then this block is optimal *given* the
   heuristic cuts).
4. **Restricted master** (so a feasible packing exists early): re-solve fixing each
   child to its last actually-packed dims — a guaranteed-feasible upper bound while
   iterating.
5. **Fine-tuning** (`LEFT`/`RIGHT` in the paper): after the loop, minimise the block's
   width at the found height (improves the parent's cut), optionally widen the cut.

The master here is **not** a MILP — it is the §4.A free-dimension packer with the extra
twist that child occurrences are rectangles whose size is bounded below by area and
above by accumulated cuts. The cuts are simple `(W,H)` step constraints the width-sweep
can honour directly (each cut just forbids a `(width-range, height-below)` region).

### 4.3 Exact oracle (optional, small instances)

Swap the per-block heuristic packer for the **SAT exact 2-D solver** to get the exact
LBBD of paper §5.1–5.2 (convergence proof holds when each single-block problem is solved
optimally). Gate strictly on small blocks (the paper: ≤ ~10–12 rectangles per block);
above that, SAT/CP both blow up. Report it as a certified-optimal mode, not the default.

## 5. Implementation steps (when picked up)

1. `d2`: bottom-left (Chazelle) + best-fit (Imahori–Yagiura) constructive packers, if
   not already adequate via existing shelf/skyline; unit tests vs known small optima.
2. `d2` (or new `d2/enclose.go`): free-dimension wrapper — width sweep + strip-pack +
   `W + H` objective + `LB_{W+H}` gap report. **Independently shippable.**
3. `hpack`: block-type tree model + topological order + variant propagation.
4. `hpack`: Bottom-Up baseline (faithful BU port). Tests on the paper-style synthetic
   2–4 level instances; assert `W + H` within a sane factor of `LB_{W+H}`.
5. `hpack`: heuristic LBBD (master / verify / cut / restricted-master / fine-tune) with
   the width-sweep master honouring `(W,H)` cuts. Honour `ctx`/`Deadline` (the per-block
   loops are the hot path — sample `ctx.Err()` off it).
6. (Optional) `hpack`: exact oracle via the `sat` package, gated on small blocks.
7. `packapi`: either extend `PackNested` to N levels with a free-dimension level
   objective, or expose `hpack` as its own endpoint. Add a `packapi` test; wire UI only
   if surfaced (the demo's nested UI already renders two levels — N levels + free dims is
   a real UI change to flag for the user).
8. `ATTRIBUTION.md` + doc comments: attribute 2DHRP / Grus et al. (2026); note BU
   baseline provenance (Xu et al. 2017), bottom-left (Chazelle 1983), best-fit
   (Imahori–Yagiura 2010), and that MILP/CP formulations were deliberately not ported.

## 6. Risks / decisions to revisit

- **Scope is large — split it.** §4.A (free-dimension single-block) is the high-ROI,
  low-risk slice and is useful on its own ("tightest enclosing footprint"). Ship that
  first; only build §4.B if a real multi-level free-dimension need exists. Don't let the
  LBBD machinery block the cheap win.
- **New package vs. extend nested mode.** Nested mode is hard-wired to two levels and
  fixed bin sizes. Generalising it to N levels *and* free dimensions is a bigger surgery
  than a clean `hpack` package; recommend `hpack` standalone, reusing `d2` + `sat`,
  rather than overloading `NestedLevelSpec`.
- **Float dimensions vs. the paper's integers.** The paper assumes integer coordinates
  (clean breakpoints, SAT/CP). The library is `float64`. The width sweep and cuts need a
  tolerance discipline (mirror `Remaining`/`Utilization` comparisons); the area-LB
  hyperbola breakpoints must round *conservatively* so the master never under-constrains.
- **No optimality guarantee in the default path.** The heuristic LBBD reports `W + H`
  and a gap to `LB_{W+H}`, not a proven optimum. The paper shows it lands within a few
  percent of the lower bound, and that even the simple Bottom-Up does well — so set
  expectations: this is a strong heuristic, exactness only via the gated SAT oracle on
  small blocks.
- **Is it worth it at all?** §4.A: yes, broadly useful and modest. §4.B: only if
  multi-level *free-dimension* nesting is a real requirement — the existing fixed-size
  two-level nested mode already covers the common cartons→pallets logistics case. Build
  the hierarchy machinery only when a concrete IC-layout-style or deep-nesting use case
  appears.
