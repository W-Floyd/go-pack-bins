# Plan: 3-D Load-Bearing & Stacking Constraints

**Status:** Proposed — not started. Captured for possible later implementation.
**Source:** Bischoff (2006), "Three-dimensional packing of items with limited load
bearing strength", *European Journal of Operational Research* 168(3); Junqueira,
Morabito & Yamashita (2012), "Three-dimensional container loading models with cargo
stability and load bearing constraints", *Computers & Operations Research* 39(1);
Ratcliff & Bischoff (1998) layer-from-floor load-bearing scheme. Background search:
"Solving a 3D bin packing problem with stacking constraints", *C&OR* (2024).

**Pros:** real, common container-loading requirement the library can't express today
(boxes crush; fragile/this-side-up cargo can't be buried); reuses the existing support
machinery (`footprintSupport`, the support graph already built by the void/compact
work) so the geometry is mostly in place; pure-Go, deterministic; composes cleanly with
the existing `ContactSpec` support gates.
**Cons:** touches the 3-D *placement* decision (weight must flow into strategies that
today see only geometry) — the main architectural cost; the constraint is *cumulative
up a support stack*, so it interacts with order-of-placement and post-pass relocation
(`Compact`/void-refiner); fragility/orientation rules multiply the orientation-filtering
logic; validating "stack pressure never exceeds limit" needs care under every relocation.

---

## 1. Why this exists / the use case

Real cargo can only bear so much weight on top before it crushes, and some items must
not be stacked on at all (fragile, "this side up"). Pure geometric 3-D packing
([d3](../../d3)) happily buries a carton of eggs under a pallet of bricks: it checks
*fit* and (optionally) *support beneath*, never *pressure from above*. Load-bearing
packing adds, per item:

- a **load-bearing limit** `L_i` — the maximum weight (or pressure) its top face can
  carry,
- optionally a **fragility / stacking class** — "nothing may rest on this", or "only
  same-or-lighter class may rest on this",
- optionally an **orientation restriction** — "this face must point up" (already
  partially expressible by feeding fewer orientations, but tied to load-bearing because
  the bearing face is orientation-dependent).

Feasibility rule: for every item `i`, the **total weight of everything resting on `i`,
transitively** (the column above `i`, apportioned by contact area) must not exceed
`L_i`. The classic schemes (Ratcliff–Bischoff, Junqueira et al.) enforce this by
packing **floor-upward in layers** and tracking accumulated load per supporting footprint.

Use cases: container/truck loading (the headline application), palletisation, warehouse
stacking — anywhere "will the bottom box survive the stack?" matters, which is most
real freight.

## 2. Why the library does NOT already cover it

The library models **support from below** but has **no notion of load from above**:

- **`ContactSpec` is geometric support only.** `Bottom` / `NoFloating`
  ([d3/extremepoint.go:24](../../d3/extremepoint.go)) gate the *fraction of an item's
  bottom face that rests on something* — they ensure an item isn't floating, not that
  whatever it rests on can *bear its weight*. There is no weight field anywhere in the
  placement path.
- **The placement `box` is weightless.** `type box struct{ x,y,z,w,d,h float64 }`
  ([d3/extremepoint.go:43](../../d3/extremepoint.go)) — strategies (ExtremePoint, BLF,
  EMS, heightmap, LAFF) reason purely over geometry and orientations
  (`[][3]float64`). Item weight lives as a `pack.ScalarsOf` scalar but is **invisible**
  to the 3-D strategies; only the *bin-level* aggregate (e.g. a `MaxAggregate("weight")`
  total cap) sees it, and a total-weight cap says nothing about *which item bears what*.
- **No fragility / stacking-class concept.** `Incompatible` is pairwise category
  exclusion with no spatial meaning; nothing expresses "may not be stacked upon".

### What IS already covered (reuse, do not reimplement)

- **The support relationship is already computed.** `footprintSupport(placed, x,y,z,w,d)`
  ([d3/ems.go:289](../../d3/ems.go)) returns exactly which placed boxes a candidate rests
  on and the contact-area fraction — the precise input a load-bearing check needs
  (apportion the resting item's weight across its supporters by contact area).
- **The support graph.** The void-refiner / `Compact` work builds (or specs) a support
  graph (edge `A→B` iff `A.top ≈ B.bottom` with footprint overlap,
  [void-refiner.md §3](./void-refiner.md)). Transitive load = sum over the sub-tree
  above a node. Reuse it; don't rebuild.
- **`boxgrid` broadphase** ([d3/boxgrid.go](../../d3/boxgrid.go)) keeps the "who is
  directly above me" query local instead of `O(k)`.
- **Floor-upward placement order** is already what extreme-point / BLF / heightmap do
  (minimise z first) — the layer-from-floor load-bearing schemes assume exactly this.

## 3. Explicitly OUT of scope (do not port)

- **Genetic algorithm / SA / Q-learning LNS wrappers** from the container-loading
  literature — generic metaheuristics, redundant with existing
  `GRASP`/`RuinRecreate`/`BeamSearch`, non-deterministic, and the practical gap is the
  *constraint*, not another search shell.
- **Full continuous dynamic-stability physics** (centre-of-mass toppling, inertial
  shift). The library already has anti-slosh (`SideX/SideY`) and CG (`MinimizeCG`) as
  *soft* targets; a full rigid-body stability model is out of proportion. Load-bearing
  is a *static crush* constraint — keep it static.
- **Multi-drop / axle-weight / sequencing constraints** (route-aware loading) —
  orthogonal logistics concerns; not packing feasibility.

## 4. Design

The crux: make **per-item weight and bearing limit visible to the placement decision**,
and reject (or down-rank) a placement that would crush an item below it. Two layered
deliverables; (A) is the core, (B) is polish.

### 4.A Load-bearing gate (the core)

Extend the contact/support path the way `Bottom` already gates support:

1. **Carry weight + limit into the strategy.** The placement `box` (or a parallel slice)
   gains `weight` and `bearLimit`. These come from item scalars — define reserved keys
   (`pack.WeightKey`, `pack.BearLimitKey`) or pass them alongside orientations. This is
   the one real architectural change: weight must flow from `pack.Item` scalars into the
   `PlacementStrategy3D` insert path, which today takes only `[][3]float64`.
2. **Bearing check at placement.** When a strategy tests placing item `B` at `(x,y,z)`,
   compute its supporters via `footprintSupport` (already there). For each supporter `S`,
   the load `B` adds to `S` is `weight(B) · (contact fraction)`. **Propagate transitively
   down**: `B`'s weight (plus whatever already rests on `B`) flows to `S`, then from `S`
   to *its* supporters, etc. Reject the placement if any item's accumulated borne weight
   would exceed its `bearLimit`. The floor bears infinitely.
3. **Where it gates.** Mirror the `Bottom`/`NoFloating` gate sites — `ems.go` insert
   (the `footprintSupport < Bottom` check, [ems.go:106-113](../../d3/ems.go)), the BLF
   `supported` check ([blf.go:119](../../d3/blf.go)), extreme-point, heightmap. A shared
   helper `bearingOK(placed, supportGraph, candidate) bool` keeps the rule in one place.
4. **Cumulative bookkeeping.** Track per-placed-item *currently borne weight* incremented
   as items land on top. A new placement walks down the support chain adding its weight;
   the gate compares against each `bearLimit` en route. `O(stack depth)` per placement.

### 4.B Fragility / stacking class + orientation (polish)

- **No-stack flag / class.** `bearLimit = 0` already means "nothing may rest on this"
  (any positive load fails the gate) — fragility falls out of (A) for free. A *class*
  ("only ≤ my class may rest on me") is an extra per-supporter predicate in the gate.
- **Bearing face orientation.** `L_i` is a property of the *up-face*. When a strategy
  enumerates orientations, the bearing limit must track which face ends up on top
  (and "this-side-up" simply drops disallowed orientations from the candidate set). Fold
  into the per-orientation candidate generation.

### 4.C Interaction with post-passes

`Compact` and the planned void-refiner **relocate** items. Any relocation must re-run the
bearing gate — moving an item can both relieve a crush and create one. The bearing check
must therefore be a reusable predicate the post-passes call, not logic baked only into
the constructive insert. (The void-refiner already re-derives the support graph per
round — extend that to re-validate bearing.)

## 5. Implementation steps (when picked up)

1. `d3`: thread item weight + bearing limit into the placement path — extend `box`
   (or a parallel array) and the `TryInsert`/strategy-construction signatures; reserved
   scalar keys in `pack` for weight and bear-limit. Unit test the plumbing carries values.
2. `d3`: `bearingOK` shared helper — given placed boxes + support relationships +
   candidate, compute transitive borne weight down the support chain and compare to each
   `bearLimit`. Pure, table-tested on hand-built stacks (single stack, branching support,
   floor-infinite).
3. `d3`: gate `bearingOK` into ExtremePoint / BLF / EMS / heightmap inserts, beside the
   existing `Bottom`/`NoFloating` gates. Add a `LoadBearing bool`/limit toggle to the
   strategy constructors (default off ⇒ byte-identical to today; assert via existing
   tests).
4. `d3`: fragility (`bearLimit = 0`) + optional stacking-class predicate; orientation
   restriction by candidate filtering. Tests: a fragile item is never buried; a heavy
   box never lands on a low-limit box.
5. `d3`: make `Compact` (and the void-refiner, if built) re-validate `bearingOK` on every
   relocation; test that a post-pass never produces a crushing stack.
6. `packapi`: surface per-item weight + bearing limit + fragility in the 3-D request
   (item scalars already exist; add the bear-limit/fragility fields) and a solve flag to
   enable the gate. Add a `packapi` test. Not a new algorithm — a constraint on existing
   3-D algos — so no `registerSolve`; wire as a contact-style option in `pack3D`.
7. (If surfaced in the demo) `cmd/webdemo/static/index.html`: per-item bearing limit /
   fragile checkbox alongside weight; flag UI for user verification.
8. `ATTRIBUTION.md` + doc comments: attribute Bischoff (2006) and Junqueira et al. (2012);
   note it's a static crush model, not dynamic stability.

## 6. Risks / decisions to revisit

- **Weight-into-placement is a real surface change.** The 3-D strategies are
  deliberately geometry-only today. Threading weight through every strategy's insert path
  touches the busiest code in `d3`. Keep the gate behind a default-off flag so the
  common (no-bearing) path is provably unchanged — re-run the full `d3` suite with
  `-race`.
- **Cumulative semantics ordering.** Borne weight depends on what's *already* placed.
  Constructive packers place floor-up (fine), but a relocation or an out-of-order insert
  can momentarily violate then satisfy — define the gate over the *final* configuration
  and re-check on every move (§4.C), or a post-pass can silently leave a crushed stack.
- **Pressure vs. total weight.** Bischoff models *pressure* (weight per unit contact
  area), not just weight — a small heavy item concentrates load. Decide whether `L_i` is
  total borne weight (simpler) or pressure (more faithful, needs area division). Start
  with total weight; note pressure as a refinement.
- **Apportioning across multiple supporters.** When an item rests on several boxes,
  splitting its weight by contact-area fraction is an approximation (real load
  distribution is statically indeterminate). Document the contact-area apportionment as
  the chosen model.
- **Is it worth it at all?** Strong yes if container/truck/pallet loading with real cargo
  is a target — crush and fragility are first-order constraints there, and the library
  genuinely can't express them. If 3-D use is only abstract volume packing, this stays on
  the shelf. Highest practical value of the 3-D-direction candidates, but more
  invasive than the scalar-only plans (BPPS, VBP).
