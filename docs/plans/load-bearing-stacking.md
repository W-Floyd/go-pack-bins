# Plan: 3-D Load-Bearing & Stacking Constraints

**Status:** In progress. Re-verified against the tree 2026-09-21 (see §7); §2, §4.A.1,
§4.A.3 and §4.B were wrong and are corrected below. Steps 1–4 (the bearing core, the
plumbing and the constructive gate on the four candidate-loop strategies) are implemented
in [d3/bearing.go](../../d3/bearing.go); steps 5–8 are outstanding.
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

- ~~**The support relationship is already computed.**~~ **Wrong** (verified 2026-09-21).
  `footprintSupport` ([d3/ems.go:318](../../d3/ems.go)) returns a *single aggregate
  fraction* `sup/fp` — it sums the supporters and throws away which they were. The
  bearing rule needs them individually, to apportion load. Built as `supportersOf`
  in [d3/bearing.go](../../d3/bearing.go).
- ~~**The support graph.**~~ **Overstated.** No support graph exists. What exists is
  `restsOn(b, a)`, a pairwise predicate ([d3/refine.go:222](../../d3/refine.go)), scanned
  `O(n²)` by `removable` to find leaves. It is also typed on `*Placement3D`, while the
  strategies work over the unexported `box` — so it cannot be shared with a gate that
  runs inside a strategy. Hence `BearBox` as the common currency.
- **Floor-upward order does hold**, and `BorneLoads` exploits it: a single top-down pass
  settles transitive load, no iteration to fixpoint.
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

1. **Carry weight + limit into the strategy.** ~~Change the `TryInsert` signature.~~
   **Revised** — don't. There are **six** `TryInsert` implementations (BLF, EMS, Fit,
   ExtremePoint, Heightmap, LayerStack; the plan named four), plus callers in `bin.go`,
   `refine.go` and `columns.go`. Instead: `Bin3D.TryPlace` already holds the item and
   calls `pack.ScalarsOf(item)` ([d3/bin.go:31](../../d3/bin.go)), so it can hand the
   pending weight/limit to the strategy through a small *optional* interface
   (`interface{ setPending(weight, limit float64) }`) before `TryInsert`.
   `PlacementStrategy3D` is unchanged, strategies opt in one at a time, and the
   default-off path is untouched by construction.

   Also **not** reserved keys: `MinimizeCG(massScalar string)` establishes the
   convention that the caller *names* the mass scalar. The reserved `"\x00m:"` keys are
   for bin metrics reported outward, not item inputs. The gate config should take
   scalar names (`WeightScalar`, `LimitScalar`) to match.
2. **Bearing check at placement.** When a strategy tests placing item `B` at `(x,y,z)`,
   compute its supporters via `footprintSupport` (already there). For each supporter `S`,
   the load `B` adds to `S` is `weight(B) · (contact fraction)`. **Propagate transitively
   down**: `B`'s weight (plus whatever already rests on `B`) flows to `S`, then from `S`
   to *its* supporters, etc. Reject the placement if any item's accumulated borne weight
   would exceed its `bearLimit`. The floor bears infinitely.
3. **Where it gates.** The gate sites are uniform and confirmed —
   `EmptyMaximalSpace.gated` ([ems.go:134](../../d3/ems.go)), `Heightmap.gated`
   ([heightmap.go:198](../../d3/heightmap.go)), `BottomLeftFill.supported`
   ([blf.go:119](../../d3/blf.go)), `ExtremePoint.supportFrac`
   ([extremepoint.go:223](../../d3/extremepoint.go)); all take `(x,y,z,w,d)`, which is
   exactly `CanBear`'s shape.

   **But gating there does not cover the 3-D surface.** `blocks`, `columns`, `assemble`,
   `laff` and `joint` construct `Placement3D` directly and never touch a strategy — per
   the registry ([packapi/algos_3d.go:191+](../../packapi/algos_3d.go)) they are
   `selfManaged3D`. The constructive gate reaches the ~12 strategy-backed algos
   (ff/blf/ems/fit/heightmap/nf/bf/wf/ffd/bfd/nfd/layer); the other six would silently
   emit crushing stacks. So the deliverable is **two-sided**: a constructive gate
   (`CanBear`) for the strategy path, and the whole-configuration validator
   (`BearingOK`) that every path is checked against — with self-managed algos rejecting
   the option until they gate properly, rather than quietly ignoring it.
4. **Cumulative bookkeeping.** Track per-placed-item *currently borne weight* incremented
   as items land on top. A new placement walks down the support chain adding its weight;
   the gate compares against each `bearLimit` en route. `O(stack depth)` per placement.

### 4.B Fragility / stacking class + orientation (polish)

- **No-stack flag / class.** `bearLimit = 0` already means "nothing may rest on this"
  (any positive load fails the gate) — fragility falls out of (A) for free. A *class*
  ("only ≤ my class may rest on me") is an extra per-supporter predicate in the gate.
- **Bearing face orientation.** `L_i` is a property of the *up-face* — and this is
  **not buildable on today's API**. `computeOrientations`
  ([d3/item.go:47](../../d3/item.go)) de-duplicates orientations *by dimensions* into
  `[][3]float64`, discarding the permutation that produced each. Given a `[3]float64`
  you cannot recover which original face points up (a cube collapses to one entry).
  Per-up-face limits therefore need `Item3D` to carry an orientation *index* — as
  `SolidPlacement3D.RotationIndex` already does for the solid path, and `Placement3D`
  does not. Defer: ship a single per-item limit first, treat the up-face refinement as
  a separate change gated on that plumbing.

### 4.C Interaction with post-passes

**Confirmed mandatory, not optional.** Every registered 3-D algo gets a relocating
post-pass: `compact3D` runs `Compact` on the strategy-backed ones and `selfManaged3D`
runs the void-refiner (and `settle` for blocks/columns). The void-refiner is built
([d3/refine.go](../../d3/refine.go)), not "planned". So no configuration reaches the
caller without passing through relocation — a gate that only runs constructively is
worthless on its own.

`Compact` and the void-refiner **relocate** items. Any relocation must re-run the
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

## 7. Verification log

**2026-09-21** — re-checked the plan against the tree before starting. Motivated by
[issue #1](https://github.com/W-Floyd/go-pack-bins/issues/1), which asks for exactly this
(plus exclusion zones and access priority, both still unaddressed). Four claims were
wrong; each is corrected in place above:

| Claim | Verdict |
|---|---|
| `footprintSupport` identifies supporters | **No** — returns one aggregate fraction (§2) |
| A support graph exists to reuse | **No** — only a pairwise `restsOn`, wrong type (§2) |
| 4 strategies to gate, via a `TryInsert` signature change | **6**, and the signature need not change (§4.A.1) |
| Gating the strategies covers 3-D | **No** — 6 self-managed algos bypass them (§4.A.3) |
| Per-up-face bearing limits | **Not expressible** — orientations lose face identity (§4.B) |
| Post-pass re-validation is a caveat | **Load-bearing**: every algo relocates (§4.C) |

Held up: the gate-site shape is uniform across strategies, floor-upward placement order
makes the load pass single-shot, and `bearLimit = 0` does give fragility for free.

**Done:** step 2 — [d3/bearing.go](../../d3/bearing.go): `BearBox`, `supportersOf`,
`BorneLoads`, `BearingOK`, `CanBear`. Pure, no dependency on the rest of `d3` beyond
`overlap1D`/`compactEps`. Table-tested including branching support, uneven contact
apportionment, floating boxes, edge-touching footprints, order-independence, and
`CanBear` ≡ `BearingOK` agreement.

**Decisions taken:** total borne weight, not pressure (§6 left it open — pressure needs
the contact area a supporter offers, which `supportersOf` now returns, so it stays a
drop-in refinement). `Limit` zero value means fragile, so `NoLimit` must be spelled out
explicitly — chosen because silently defaulting absent limits to infinity would make a
mis-wired scalar fail open, which is the dangerous direction for a safety constraint.

**Next:** step 1 (the `setPending` plumbing) then step 3 (gate into the four strategies).

**2026-09-21, continued** — steps 1, 3 and the fragility half of 4 are done.

- **Step 1 (plumbing).** `PlacementStrategy3D` is untouched, as §4.A.1 now specifies.
  `Bin3D.TryPlace` type-asserts the optional `pendingBearer` and hands over
  `pack.ScalarsOf(item)` before `TryInsert`; each strategy holds a `*bearState` that is
  nil when bearing is off, and every method on it tolerates a nil receiver. `WithBearing`
  / `BearingStrategy` / `Bearable` are the public surface.
- **Step 3 (gating).** Gated into ExtremePoint (both `TryInsert` and the `Candidates`
  path `joint` uses), EMS, BLF and Heightmap, beside the existing support gates.
- **Step 4 (fragility).** Falls out of the core as predicted: `Limit = 0` admits no load.
  Stacking *classes* and the up-face refinement remain unbuilt (§4.B).

**Fail-closed choices**, both locked in by tests. A strategy with bearing enabled but no
pending item set refuses every placement rather than treating items as weightless — this
catches a caller driving a strategy directly and skipping `Bin3D.TryPlace`.
`BearingSpec.DefaultLimit` is likewise *not* defaulted to `NoLimit`, so a mis-named limit
scalar yields a packing that will not stack rather than one that permits every crush.

**New finding — the gate cannot divert under EMS.** EMS places only at the
back-bottom-left corner of a free space. Once the floor is full the single remaining
space spans the full width with its corner over whatever sits at x=0; if that item cannot
bear the load, no alternative position exists *in EMS's candidate set*, so the gate can
only refuse. ExtremePoint, BLF and Heightmap all divert, because their candidate sets
include the far edges of placed boxes. Recorded in `canDivert` in
[d3/bearing_strategy_test.go](../../d3/bearing_strategy_test.go) so the difference is
locked in rather than discovered again. Refinement worth considering: when a space's
corner is bearing-blocked, have EMS also try the space's other three bottom corners.

This also cost a test: the first version of the diversion test was **vacuous** — ungated,
all four strategies already place the heavy item on the floor beside the weak one, since
they minimise z. The gate's observable effect is refusal or diversion only once the floor
is full, and the test now sets that up explicitly.

**Step 5 (post-pass re-validation) is now done too.** `BearingGuard` holds the spec plus
item scalars by ID and re-checks a whole bin; a nil guard permits everything, so the
disabled path is a single nil check. Wired in at every accept step:

- `CompactGuarded` reverts a slide that fails the check (`Compact` delegates with nil, so
  its signature and behaviour are unchanged).
- `RefineOptions.Bearing` carries the guard through the refiner.

**The refiner had three movers, not one.** The plan said "the void-refiner already
re-derives the support graph per round — extend that to re-validate bearing", which reads
as a single hook. In fact `RefineVoids` relocates in three independent places:
`gravitySettle` (drops every item at once — no per-move accept step, so the whole drop is
taken or reverted), `tryLower` (one item into the lowest free space) and `liftAndRedrop`
(a sub-stack). Guarding only `liftAndRedrop` left the gate defeatable through the other
two, which is how the first version of the refiner test failed. All three are guarded now.
The general lesson matches §4.A.3: in this codebase, relocation is never in one place, so
"add the check at the accept step" has to be preceded by finding *every* accept step.

**Step 6 (packapi) and step 8 (attribution) are done.** `PackRequest.Bearing` carries
`weight_scalar` / `limit_scalar` / `default_limit`, plus `default_unlimited` because JSON
cannot express `d3.NoLimit` as a number. `bearingAlgos3D` is the enforced set and
`AlgoCapabilities` *derives* the advertised `bearing` flag from it, so the two cannot
drift; `TestBearingCapabilityMatchesEnforcement` pins that, and `TestBearingAlgosEnforce`
walks every advertised 3-D algorithm and requires it either to enforce the constraint or
to refuse the request.

**Validation runs at two seams, not one.** `dispatch` is described in the code as "the
one seam every solve path funnels through", but catalog mode is decided in `PackCtx`
*before* dispatch and its inner solves clear `Containers` — so a catalog request would
have passed the check. `StreamPack` likewise bypasses `dispatch`. Both now validate.
This is the third instance of the same pattern in this work: the codebase has more entry
and mutation points than its own comments suggest, so "add the check at the seam" always
has to be preceded by enumerating the seams.

**Post-pass coverage completed.** Guards are wired into every relocation the 3-D path can
reach: `finishCompact3D` (registry), the balanced bf/wf path, the streaming path's
settle+compact, and `refineResult3D`. `settleResult3D` is guarded too even though no
currently bearing-capable algorithm settles — otherwise adding one later would bypass the
constraint silently.

**Nested mode does not support bearing** and cannot express it: `NestedLevelSpec` has no
`Bearing` field, so there is no bypass to guard against. Adding it means deciding what a
bearing limit means for a carton that becomes an item at the next level, which is a real
modelling question, not just plumbing.

**Step 7 (demo UI) is done.** No per-item inputs were needed after all: items already
carry arbitrary named scalars through the existing Scalars field, so `weight=8,
bearlimit=20` works as-is — the plan assumed new per-item controls (step 7's "per-item
bearing limit / fragile checkbox") that the existing UI already covers. What was missing
was the panel that turns the constraint on and names the two scalars.

- A "Load-bearing (crush limits)" panel, shown for 3-D single-container solves.
- It stays visible when the selected algorithm cannot enforce bearing, showing an inline
  warning instead of hiding. Hiding it would have silently dropped the constraint on an
  algorithm switch; this way the request still carries `bearing` and the server's refusal
  names the algorithms that work.
- Hidden in nested mode, where `NestedLevelSpec` cannot carry the field at all.
- Round-trips through config export/import, so saving a setup does not quietly lose it.
- Demo preset "Fragile on top (load-bearing limits)": four fragile cartons FFD places on
  the floor first, then four heavy crates. Ungated it packs into one bin by stacking the
  heavy crates on the fragile ones; gated it opens a second bin.
  `TestBearingPresetDemonstratesTheConstraint` asserts that difference, so the preset
  cannot rot into one that solves identically either way and teaches nothing.

Both front-ends pick this up automatically: `goAlgos()` in the WASM bundle serves the
same capability payload as `/api/algos`, and `cmd/wasm` decodes into `PackRequest`, so
`bearing` passes through the worker with no change.

**Awaiting user verification in the running demo**, per CLAUDE.md — Go tests cannot catch
a blank render or a JS error.

## 8. Bearing-aware ordering, and auto

**The gate constrains; it never reorders.** Discovered from the demo: with the default
volume-decreasing order a large *fragile* item takes the floor, fills it, and forces every
heavy item into a new bin — even though "heavy underneath, fragile on top" fits in one.
Measured across the ten enforcing algorithms on that case: all needed 2 bins with the
items offered fragile-first; feeding them heavy-first let the online ones (ff/nf/bf/wf/
blf/ems/heightmap) reach 1, while ffd/bfd/nfd still needed 2 because they re-sort by
volume regardless of input order.

`offline.DecreasingBearing(limitScalar, defaultLimit)` orders by bearing limit descending,
volume descending within a limit — the Ratcliff–Bischoff layer-from-floor idea. It is
**opt-in, not the default**, because measurement says it is a fix for a specific pathology
rather than a general win: over 200 random 3-D instances with mixed limits it totalled 351
bins against volume-order's 350 (better on 11, worse on 12).

A second variant was written and measured: keep volume order but defer only zero-limit
items. It looked like the conservative choice and was **clearly worse** (366 bins; better
on 6, worse on 22) — it leaves items with intermediate limits to be buried and refused
while still breaking the large-first order. Deleted rather than shipped. Respecting the
whole strength ordering is what makes the policy coherent.

**`auto` now owns this decision.** The goal is that a user enters items and constraints and
gets the best legal packing without choosing an algorithm or knowing an ordering exists, so
`auto` is bearing-capable and, when bearing is on:

- races only candidates whose strategies enforce the gate — dropping `fit` and `layer`, and
  the self-managing blocks/assemble/LAFF packers, for exactly the reason the existing code
  already drops the latter when scalar constraints are set: *they would win the race with
  an infeasible packing*;
- races **both** orderings (volume and strength) and keeps whichever uses fewer bins.

On the pathological case `auto` returns one bin with the winner reported as `FFD·strength`,
so the reason is visible. The manual "order by bearing strength" checkbox remains for the
single-algorithm modes and is hidden under `auto`, which explains in the panel that it
races both.

`auto3DPlans` is the single definition of that candidate set, shared by the registry solver
and `autoCandidates` (the streaming mirror). Those two had already drifted once over the
gate itself — see §7 — so the set is defined once rather than written twice.

## 9. Container-catalog mode

Bearing was initially refused in catalog mode. That was over-cautious rather than
necessary: `solveCatalogSingle` and `solveCatalogCascade` both solve each candidate
container through `packOneBin` → `dispatch` → `pack3D`, which already builds the gated
factory, so the gate applied all along — only the refusal stood in the way. Removing it
lets a user pick the best container size *and* respect crush limits, which is one job, not
two.

`solveCatalogGBPP` is the exception: it builds its own factory via `containerFactory` and
is reached only by `algorithm: "gbpp"`, which is not in `bearingAlgos3D`, so the algorithm
check still refuses it. `containerFactory` is gated anyway — leaving an ungated 3-D factory
in the tree is exactly how the previous escapes happened.

Two tests cover it, and both had to be repaired before they meant anything: the first
version of the cascade test never reached the cascade, because an uncapped container type
held the whole order and the single-type branch won. It now caps every type and asserts
the single-type branch fails first.

**Remaining gaps**, in the order they matter for "enter items and constraints, get the best
option":

- **Balance preferences + bearing** produce a legal result, but via `runBalanced`, which
  does not race the orderings — 2 bins where `auto` finds 1.
- **Nested mode** cannot express bearing: `NestedLevelSpec` has no field, and adding one
  needs a decision about what a limit means for a carton that becomes an item at the next
  level.
- **Issue #1's other two asks** — exclusion zones and access priority — are untouched. See
  the issue investigation: exclusion zones have a workaround via `EmptyMaximalSpace.Occupy`
  but no API, and access priority has no mechanism at all, since `pack.Preference` scores
  bin choice rather than position within a bin.

## 10. Pressure limits

§6 left open whether `L_i` is total borne weight or *pressure*, and said to start with
weight and note pressure as a refinement. Both now apply, as separate checks: a box rated
for 10 kg spread across its whole top can still be crushed by 3 kg on a narrow foot, and
neither check excuses the other.

- `BearBox.PressureLimit` caps weight per unit of contact area; `NoLimit` is unrestricted.
- Pressure is measured **per contact patch** — the share crossing an interface divided by
  *that interface's* area, not by the supporter's whole top face — so a small heavy item
  concentrates load exactly as it should.
- The per-box figure is the **peak** patch pressure, not the sum: two separate patches at
  2.0 do not combine into one patch at 4.0. `TestBearingPressureIsPeakPerPatch` pins this.
- `BorneLoads` now returns `[]Load{Weight, Pressure}` rather than `[]float64`, since both
  come from the same traversal.

`supportersOf` already returned per-supporter contact areas, so this was the drop-in §6
predicted. The pressure scalar is opt-in: naming no `PressureScalar` leaves the check off
entirely, and naming one no item carries is refused like the other scalar names.

## 11. Load paths through containers

The nested-mode question — what a bearing limit means for a carton that becomes an item at
the pallet level — resolved into something larger than a per-carton limit. **The core is
implemented** in [d3/bearing_nested.go](../../d3/bearing_nested.go); wiring it to nested
mode in `packapi` is outstanding. The decisions taken:

- **Flush inheritance uses the contents' limit alone**, not `min(container, contents)`.
  When load passes through the contents, the box structure contributes nothing.
- **Per-item flag: `rigid` vs inherit.** A rigid item always uses its own declared limit
  regardless of what is inside it.
- **"Flush" is not a binary property of the container.** It is resolved *per contact
  patch*, by physical layout: four strong corner posts reaching the top can carry a box
  above through those corners, while the carton's own rating need only cover what actually
  rests on its structure. So a patch loading a container's top face decomposes into the
  part overlapping flush contents (which transfers into them, recursively) and the
  remainder (which loads the container's own lid, checked against its own limit).

This makes bearing **recursive across the nesting boundary**, reusing the same
split-by-contact-area logic `supportersOf` already performs within a bin. It is closer to
a load-path model than to the static per-item rule in §4, and the user has confirmed that
level of physical fidelity is in scope.

**Explicitly deferred:** shear strength. It was raised as a possible further
consideration, not a requirement, and it is a different failure mode from crush —
worth its own design rather than being folded into this one.

### 11.1 What was built

`BearBox` gains `Contents []BearBox` (in the *same* frame as the box, so overlap tests need
no conversion) and `Rigid bool`. `LoadPathOK` replaces the flat check, and `BearingOK`
delegates to it — one rule, not two that can drift. With no box carrying contents the two
are identical, which the entire pre-existing flat suite exercises unchanged.

A patch arriving on a container's top face splits: the part over contents that *reach that
face* passes into them and is checked against their limits, recursively; the remainder
rests on the container's own structure and is checked against its limits, at the pressure
of the uncovered area alone. `Rigid` skips the split entirely.

**Downward propagation is unaffected** — whatever enters a container leaves its underside,
however it split on the way through. The decomposition changes *which limits are checked*,
not how much load reaches the floor. That realisation is what kept this tractable.

Two corrections during implementation, both caught by tests:

- **Container interiors must settle once, not per arriving patch.** The first version built
  a fresh sub-analysis for every patch, so contents' limits were checked in isolation
  rather than cumulatively. Patches are now accumulated per container and the interior is
  settled after the frame has been walked. Interiors settle even with no external load,
  since contents bear each other.
- **A container's weight is its tare plus its contents, recursively** (`laden`). Without it
  the contents' weight never reached the level below. Derived rather than asked of the
  caller: a forgotten sum would silently under-load everything beneath.

### 11.2 Wired to nested mode

`NestedLevelSpec.Bearing` gates the items packed *at* that level, and
`NestedLevelSpec.ContainerBearing` rates that level's bin as a load-bearing object once it
becomes an item above — `Limit`, `Pressure`, `Rigid` and `Tare`.

`nestedBearTree` rebuilds the two-level result as a `BearBox` tree (level-0 placements are
carton-local, so each content's pallet-frame position is the carton's plus its own) and
`nestedBearingError` checks each pallet with `LoadPathOK`.

**The carton's scalars needed fixing, not just forwarding.** A carton item at level 1
carries the *sum* of its contents' scalars, which is right for weight and meaningless for a
limit: summing "what each item can bear" is not "what the carton can bear". The effective
limit is now what the load path through it can carry — the limits of the contents reaching
its lid, added up; its own rating where nothing reaches the lid; its own rating always if
`Rigid`. Verified: a carton rated 1 with a flush post rated 500 gets an effective limit of
500, and the same carton with a short content falls back to its own rating.

### 11.3 The gate knows about contents

The gate no longer works from a collapsed number. `BearingSpec.Contents` is an optional
lookup from item id to what is inside it, in the item's *own* frame; the gate translates
those into place once the position is known, which is precisely what a single pre-computed
limit could never do. `CanBear` switches to the exact `LoadPathOK` whenever any container
is involved and keeps the flat incremental shortcut otherwise, so content-free packing is
unchanged in both behaviour and cost.

Every gate-construction site now builds from `req.bearingD3()` (spec + contents lookup)
rather than `Bearing.toD3()`; the remaining `toD3()` calls read scalar names for sort
policies and are correct as they are. `BearingGuard` resolves contents too, so relocation
post-passes judge a moved carton the same way the gate that placed it did.

**This inverted the meaning of a carton's limit scalar, and the first version was wrong.**
Once the gate reads contents itself, `BearBox.Limit` on a carton means its *own lid*, so
`applyCartonBearing` setting it to the collapsed sum of its contents' limits rated the lid
at 500 when its real rating was 1 — far too permissive. It now carries the container's own
rating and `cartonEffectiveLimit` is deleted rather than left to mislead.

### 11.4 Demo UI, and a seventh escape

The bearing panel now shows in nested mode too, applying the same spec to both levels — the
goods inside cartons and the cartons on pallets — with a "Carton load-bearing" panel below
it for the carton's own rating: lid limit, lid pressure, tare and rigid. Zero fields are
omitted from the request so the server reads them as unrestricted; a carton with no stated
rating must not silently become fragile.

**Nested solves bypassed the bearing validation entirely.** `doNestedPack` reaches `pack3D`
through `packByMode`, not `dispatch` or `PackCtx`, so a level using an algorithm that
cannot enforce the gate packed without it and said nothing — verified by stacking a 50 kg
item on a fragile one under `laff`. `nestedBearingSpecError` now applies the
single-container checks per level and names the level in the message. The scalar-presence
checks apply only to level 0, which packs the caller's items; level 1 packs cartons.

That is the **seventh** distinct solve path this constraint has had to be threaded through
(`pack3D`, `streamSolve`, the registry's `auto`, `sweepRefine3D`'s decoder, the balanced
path, `containerFactory`, and now nested). The recurring lesson is unchanged: this codebase
has consistently more entry, factory and mutation points than its own comments claim, and
each one had to be found by testing the behaviour rather than by reading.

Verified end to end: three cartons whose contents are rated 500 stack three-high on one
pallet despite each lid being rated 1, because the load runs through the flush posts; drop
the contents to 5 and the same order needs three pallets.

### 11.5 Outstanding

- Shear strength remains deferred: a different failure mode from crush, and worth its own
  design rather than being folded into this one.

## 12. Exclusion zones

The second of [issue #1](https://github.com/W-Floyd/go-pack-bins/issues/1)'s three asks.
A zone is an axis-aligned region no item may occupy: a pipe crossing a basement, a door
swing, a wheel arch. It is *not* a pre-placed dummy item, which would both offer support
(items could rest on the pipe) and count as occupied volume (skewing utilisation and the
Best/Worst-Fit selectors). The original investigation noted `EmptyMaximalSpace.Occupy` as
a workaround with exactly those two defects; `d3/zones.go` is the version without them.

Supported on **thirteen** 3-D algorithms — the bearing set plus `fit`, whose
maximal-space strategy gates zones even though it has no bearing gate. `layer` is out:
its `LayerStack` delegates each layer to a 2-D bin, which would need the zones projected
onto each layer rather than tested directly. Unsupported algorithms refuse the request.

**A zone must generate candidate positions, not merely veto them.** The first version
packed zero of ten items on the demo preset. An empty bin's only candidate position is the
origin, so a zone covering it left nowhere to place anything — and every subsequent
candidate is derived from an already-placed box, of which there were none. Zones now seed
extreme points, BLF corners and heightmap anchors the way placed boxes do, and are carved
out of the maximal-space free set (`EmptyMaximalSpace.carve`, factored out of `commit` so
a zone is removed from the free spaces without being recorded as placed volume or as a
surface). `TestZoneOverOriginStillPacks` pins this across every supported algorithm.

The post-pass guards were unified for this: `d3.Guard` bundles the bearing guard and the
zones, so `CompactGuarded`, `SettleGuarded` and `RefineOptions.Guard` take one value
rather than growing a parameter per constraint. Every relocation re-checks both.

**Still outstanding from issue #1:** access priority — the soft "some items need to be
easy to reach" constraint. It remains the hardest of the three, because `pack.Preference`
scores *which bin* an item goes in and nothing scores *where within a bin*.

## 13. Access cost (issue #1's third ask)

"Most accessed versus cost to access": place the things you reach for often where
they are cheap to reach. Unlike the first two asks this is a **soft objective** —
a bad arrangement still packs — and the cost is caller-defined rather than fixed:

    cost = Fixed
         + PerHeight   × z
         + PerDistance × distance from the access point to the item's nearest face
         + Σ PerScalar[s]       × scalar(s)
         + Σ PerHeightScalar[s] × scalar(s) × z

The last term is the one that matters and the one an additive model misses:
lifting something *heavy* to shoulder height is worse than the height and the weight
separately suggest. `Fixed` covers "this container is slow to open at all" — a taped
carton against a drawer. Distance is measured to the item's nearest face, not its
centre: you reach the front of a box, not through it.

The objective is `Σ frequency × cost`, with frequency read from a per-item scalar.

**Two mechanisms, because one was never going to be enough.** The original
investigation's finding still holds: `pack.Preference` scores *which bin* an item goes
in and nothing in the library scores *where within a bin*. So:

- **Ordering** (`offline.DecreasingAccess`) puts the frequently-wanted items in front of
  the strategies' own bottom-first, corner-first placement. Works with any algorithm.
- **Candidate scoring** (`JointFit.WithAccessCost`) ranks positions by retrieval cost
  directly. `joint` is the only packer that scores positions within a bin, so it is the
  only one that can act on this properly. The item's own frequency is constant across its
  own candidates, so it changes nothing there — it decides placement *order*, which is
  what settles who gets the cheap positions.

Measured over 24 items in one 8×8×8 bin, net access cost: **2674** unaided, **2051** with
candidate scoring, **1240** with ordering as well — and no extra bins in any case. Both
mechanisms carry real weight, and ordering carries more of it than the scoring does.

**Outstanding:** the `packapi` surface, the demo UI, and a preset. The core and the
`joint` integration are done.

### 12.1 Load-bearing zones

A keep-out is not always an obstacle. A pipe carries nothing, but a ledge, a plinth or a
flat wheel arch is structure you can stack on even though you cannot pack inside it.
`Zone.Supports` opts into that, and only the caller knows which they have.

A supporting zone counts as a surface everywhere support is judged —
`footprintSupportZones` (EMS, Fit, heightmap gates), `ExtremePoint.supportFrac`,
`BottomLeftFill.supported`, and `Heightmap.restingHeight` so an item comes to rest on it
rather than falling through. It still blocks placement inside itself, still occupies no
usable volume, and is **structure rather than cargo** for the load-bearing rule: weight
resting on it leaves the stack the way weight resting on the floor does, so it has no
crush limit of its own. That falls out for free — a supporting zone is not a `BearBox`,
so `supportersOf` finds no supporter above it and the load simply exits.

The demo draws obstacles amber and load-bearing zones slate, because they behave
differently and should not look the same.
