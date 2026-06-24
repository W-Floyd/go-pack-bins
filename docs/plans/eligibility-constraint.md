# Plan: Item→Bin-Type Eligibility Constraint (Restricted Bin Packing)

**Status:** Proposed — not started. Captured for possible later implementation.
**Source:** Fu & Banerjee (2020), "Heuristic/meta-heuristic methods for restricted
bin packing problem", *J. Heuristics*. (Restricted Bin Packing Problem, RBPP.)

---

## 1. Why this exists / the use case

RBPP adds an **item–bin restriction matrix** `a_ij` (item `j` may only be placed in
bin `i` when `a_ij = 1`). Real instances of this shape:

- temperature / zone classes (frozen items only in refrigerated containers),
- hazmat-only or certified containers,
- machine capability in scheduling (machine X can only run job types {1,3,5}),
- region/destination routing where only certain trucks serve a destination.

The general use case is: **each item is eligible for a subset of bin *types*, and
that subset can overlap arbitrarily across items** (item₁ → {A,B}, item₂ → {B,C},
item₃ → {A,C}).

## 2. Why the library does NOT already cover it

The practically common "grouping" subclass of RBPP **is already covered** and should
NOT be reimplemented:

- *"Put items for the same destination together"* (the paper's own marquee example)
  is exactly `AllSame("destination")` — a bin becomes a destination-L bin when the
  first item lands, and only same-destination items may join thereafter. This is RBPP
  eligibility acquired *dynamically*, and is arguably a better model than a static
  `a_ij` because you don't pre-commit how many bins each group gets.
- Pairwise exclusion is covered by `Incompatible`.

What is **not** expressible today is the **general overlapping matrix over distinct
typed bins**:

- `AllSame` induces only a *transitive* partition — no scalar value assignment
  reproduces item₁→{A,B}, item₂→{B,C}, item₃→{A,C}.
- `Incompatible` is pairwise category exclusion with no bin reference.
- The root blocker: `Constraint.Check(binAgg, itemScalars)` (see
  [pack/scalar.go:18-20](../../pack/scalar.go)) only sees the *accumulated scalars of
  items already in the bin* plus the incoming item's scalars. It has **no view of the
  bin's identity or type**. Bins are anonymous and opened on demand by the factory.

### Key observation that bounds the value

Where the library *can* express the restriction today (`AllSame`/`Incompatible`),
feasibility is **never at risk** — both are non-permanent rejections
([pack/constrained.go:44-45](../../pack/constrained.go)): an item can always open its
own bin, so the packer never dead-ends; only bin *count* can come out suboptimal. The
paper's central contribution (the 1-degree-first "feasibility-retaining" repair) only
earns its keep in the general overlapping-matrix case — i.e. exactly the case the
library can't currently express. So this plan targets the one genuine gap and
deliberately drops the rest of the paper.

## 3. Explicitly OUT of scope (do not port)

- **Zigzag sorting** — claimed 1.5-approx for next-fit is not credibly proven; MFFD
  (71/60) and the Harmonic family already give better, rigorous bounds.
- **Max-fit / max-degree selector** — the paper admits MF ≡ NF without restrictions.
- **Simulated Annealing** — redundant with existing `RuinRecreate`/`GRASP`/`BeamSearch`
  /`BruteForce`/`BinCompletion`/`KK`.
- **AA / MR / AAMR makespan objective** (`c_u·#bins + c_e·(maxload − l)`) — orthogonal
  to the library's count-minimization identity; the MR "reject with revenue" idea is
  already approximated by `gbpp` optional-items.
- **Clique-based GRASP construction** — existing GRASP construction is adequate; the
  clique reframing only matters once eligibility exists, and even then is marginal.

## 4. Design

### 4.1 The contract problem (the real cost)

`ConstrainedBin.TryPlace` distinguishes *permanent* from *temporary* rejection by
re-running `Check` against an **empty** binAgg
([pack/constrained.go:36-37](../../pack/constrained.go)): if a constraint fails even on
an empty bin, the item is permanently rejected. This logic assumes **constraints
depend only on placed-item aggregates** — an empty bin accepts anything.

A bin-type-eligibility constraint *depends on the bin*, not on placed items. So the
empty-bin probe must still carry the bin's type, or it will misclassify (an item
ineligible for *this* type is not permanently rejected — it may fit another type).
**This invariant break is the bulk of the work**, not the constraint itself.

### 4.2 Approach: seed bin type into the aggregate map

Make the bin's type visible to constraints by seeding a reserved key into `binAgg` at
bin-open time, so `Check` can read it without changing the `Constraint` signature
(preserves all existing constraints).

1. **Reserved key.** Define `BinTypeKey()` returning a reserved scalar key (mirror the
   `\x00`-prefixed reserved-key convention already used by `AllSame`/`Incompatible`,
   e.g. `"\x00bintype"`). Value is a numeric type id.
2. **Seed at open.** `ConstrainedBin` must initialise `agg[BinTypeKey()]` when the bin
   is opened, from the bin/factory's type. Requires `ConstrainedFactory.Open` to know
   the type of the bin it just opened — wire the type id through
   ([pack/constrained.go:96-98](../../pack/constrained.go)). For `catalog` bins the
   type is the chosen container type; for a homogeneous factory it's a single id.
3. **The constraint.** `Eligible(allowedScalar string)` where each item carries a
   scalar encoding its allowed-type set. Single-type eligibility is a plain id match;
   for *sets*, encode as a bitmask scalar (type ids are bit positions) and check
   `itemMask & (1 << binType) != 0`. Document the 53-bit float-mantissa ceiling on the
   number of distinct types (use a different encoding if >53 types is ever needed).
4. **Fix permanence detection.** The empty-bin probe in `TryPlace` must seed the same
   `BinTypeKey()` value into its probe map so eligibility is evaluated against *this*
   bin's type. An item ineligible for this type returns `ErrNoRoom` (try another
   type), and is only permanently rejected if it is eligible for *no* type that the
   factory/catalog can open. Consider computing a real permanence signal from the set
   of available types rather than the single-empty-bin probe.

### 4.3 Selection side (catalog / online)

A post-hoc `Check` only *rejects* placements; it does not *steer* the catalog toward
opening an eligible type. For correctness `Check` is sufficient (ineligible placements
are refused, a new eligible bin opens). For quality, the catalog's type-choice logic
([d3/catalog](../../d3/catalog), `gbpp.PackCatalog`) should prefer/open a type the item
is eligible for. Decide during implementation whether to:
- (a) ship constraint-only first (correct, possibly more bins), then
- (b) add eligibility-aware type selection as a follow-up.

### 4.4 Most-constrained-variable ordering (optional, the paper's good idea)

The paper's transferable insight: order items by **fewest eligible bins/types first**
(classic CSP most-constrained-variable), with a repair swap when an item can't place.
Worth adding as an offline sort policy *only if* the general overlapping case is
implemented — it reduces dead-ends and bin count there. Independent of full RBPP, an
MCV-style ordering could also modestly help existing `AllSame`/`Incompatible` packing,
but feasibility is already guaranteed there so the payoff is count-only.

## 5. Implementation steps (when picked up)

1. `pack`: add `BinTypeKey()` reserved key + `Eligible(allowedScalar string)`
   constraint (`Constraint` + `ConstraintDescriber`; stateless — no Apply/Revert).
2. `pack`: thread a bin-type id through `BinFactory`/`ConstrainedFactory.Open` and seed
   `agg[BinTypeKey()]` in `NewConstrainedBin`.
3. `pack`: fix `TryPlace` permanence detection to seed the bin-type into the probe map;
   add a unit test proving ineligible-for-this-type ⇒ `ErrNoRoom`, eligible-for-no-type
   ⇒ permanent.
4. `pack`: tests for overlapping eligibility sets (the {A,B}/{B,C}/{A,C} case) that
   `AllSame` provably cannot express.
5. (Follow-up) `catalog`/`gbpp.PackCatalog`: eligibility-aware type selection.
6. (Follow-up) `offline`: MCV sort policy + repair swap.
7. `packapi`: expose eligibility in the solve API; add a `packapi` test (per CLAUDE.md
   "Adding an algorithm" checklist — wire UI `ALGOS`/streamability only if surfaced in
   the demo).
8. `ATTRIBUTION.md` + doc comments: attribute RBPP / Fu & Banerjee (2020) and note the
   CSP MCV-ordering provenance.

## 6. Risks / decisions to revisit

- **Contract change blast radius.** Seeding bin type into `binAgg` and fixing
  permanence detection touches the core `ConstrainedBin` path used by every
  constrained solve, including `BinCompletion`'s stateful apply/revert. Re-run the full
  suite with `-race`.
- **Encoding ceiling.** Bitmask-in-a-float caps distinct types at 53. Fine for
  realistic catalogs; document and guard.
- **Is it worth it at all?** Only build if a concrete arbitrary item→typed-bin
  eligibility use case exists. If the need is just grouping/exclusion, `AllSame` /
  `Incompatible` already cover it and this plan should stay on the shelf.
