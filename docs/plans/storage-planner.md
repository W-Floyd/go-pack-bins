# Plan: Storage planner — locations, furniture, containers, items

**Status:** Proposed — nothing built. Captured before implementation because the
decomposition below is the design decision, and it is cheaper to argue with on paper than
after three packages exist.

**The ask:** "here are my shelving units available, here's my room, here are all my items,
and available containers — pack it all optimally", extended to drawers, cabinets and
cupboards, and to more than one storage location.

Everything the library does today takes the containers as *given* and asks where the items
go. This asks the opposite as well: **what storage should exist, and where** — which
furniture, in which room, holding what.

Shelving is the simplest case, not the whole problem. A chest of drawers, a cupboard with
doors and a stack of totes are all somewhere to put things, and they differ in ways that
change the answer (§3). Several rooms differ again (§4).

---

## 1. What is being decided

| Decision | Today | Here |
|---|---|---|
| Which items in which container | catalog / GBPP | same, but **optional** per item |
| Where a container sits | 3-D packing | into a *storage compartment* |
| Which storage furniture to install | — | **new** |
| Where it goes in the room | hand-placed zones | **new** |
| Which room or area it goes in | — | **new** |
| Whether it can be got out again | straight pull only | **routed to an exit** |

Answers taken from the user:

- **Containers are optional per item.** An item goes into one only if that helps; a large
  awkward thing may sit straight on a shelf. The solver weighs the container against the
  packing it buys — the shape `gbpp` already has for optional items.
- **The solver chooses the layout**: which unit types, how many, and where, subject to the
  clearance each needs in front of it.
- **Priority is configurable**, not a fixed objective. The candidates are fewest units
  installed, best accessibility, fewest containers, most spare capacity — ordered by the
  caller and compared lexicographically, which is what `meta.LexBestOf` and the existing
  `LexObjectives` request field already do for bin counts.

## 2. Why a pipeline, and where it is exact

The joint problem — choose the furniture, place it, choose containers, assign items,
place containers in compartments — is not one that can be solved exactly at any useful
size. It decomposes into three stages, each of which maps onto machinery that exists:

1. **Containerise.** Items → container types, with "loose" a legal answer. This is the
   optional-item objective `gbpp` implements.
2. **Compartment-fit.** A unit's compartments are *just bins*: a shelf gap is `width ×
   depth × clear height`, and so is a drawer. So "how much storage do I need" is bin
   packing over heterogeneous bin types, which `catalog` already does — and the same
   solver covers every kind of furniture, which is the payoff of treating them alike.
3. **Room layout.** Place the chosen units into the room with their clearances between
   them. A 2-D strip layout, not a packing — furniture lines up in runs against walls, it
   does not tetris.

**The decomposition is the approximation.** Choosing containers before knowing the
compartment depth can pick one that wastes a shelf; choosing the furniture before knowing
the container sizes can install a unit whose compartments do not divide well. Stage C
closes some of that by searching over the stage boundaries rather than running each once.

## 3. Storage units: shelving is one kind of several

Open shelving, a chest of drawers, a cupboard with doors and a stack of totes on the floor
are all "somewhere to put things", but they differ in three ways that the model has to
carry, and each one already has machinery behind it:

| | Compartments | Clearance it needs | Cost to open |
|---|---|---|---|
| **Open shelving** | one bin per gap, plus the floor under the lowest board | aisle to stand in | none |
| **Drawers** | one bin per drawer | the drawer's own depth, pulled out | a little |
| **Cupboard / cabinet** | interior, often shelved | door swing | more |
| **Floor stack** | the footprint itself | aisle | none, but digging |

So `StorageUnit` rather than `ShelvingType`: a footprint, a list of compartments it
offers, the clearance it needs in front, and a fixed cost to get into it.
`Compartments()` is the generalisation of `Levels()` — for shelving it is the gaps between
boards (**including the floor space under the lowest board**, which is real storage and
the easy thing to forget); for a chest it is the drawers; for a cupboard it is its
interior.

**Each difference maps onto something already built**, which is the reason this
generalisation is cheap rather than a second system:

- **Clearance in front** is an exclusion zone, and a *permeable* one — reserved empty
  space you may still slide a box out through (`Zone.Permeable`, `bdc4a93`). A door swing
  and an aisle are the same object.
- **Cost to open** is `AccessCost.Fixed`, which exists precisely because "some containers
  take longer to open than a drawer".
- **A drawer changes retrievability.** Inside a drawer, an item does *not* need its own
  clear face: the drawer comes out and you reach in from above. So §14's rule applies to
  the drawer as a unit, and inside it only the top face matters —
  `RetrievalSpec{SidesOnly: false}` against the compartment rather than the room. Open
  shelving is the opposite: an item is reached from the aisle, so it needs a clear face
  toward it. **Getting this wrong in either direction is the main modelling risk here**,
  because it looks like a detail and decides whether a plan is usable.
- **A cupboard's interior is a nested container** — the carton-in-pallet machinery, one
  level further in.

## 4. Storage locations

"Different storage locations" is a second axis: a basement, a loft, a garage, a shed. Each
is its own room with its own furniture, and an item has to be assigned to one before any
of the above applies.

What makes them different is almost entirely **access cost**: the loft is a ladder, the
shed is across the garden, the hall cupboard is on the way out of the door. That is the
per-location `Fixed` term, with the in-room distance term already handling where in the
room something is.

Locations also carry their own constraints — a loft may cap weight, a shed may be damp and
so unsuitable for some items, a garage may be unheated. Those are ordinary scalar
constraints (`MaxAggregate`, `Incompatible`) applied per location, not new machinery.

**Consequence for the search:** allocating items across locations sits *above* the three
stages, and it is where the access objective does most of its work — putting the things
you want weekly in the hall cupboard and the Christmas decorations in the loft is a bigger
win than anything the within-room placement can achieve. It should therefore be decided
first and revisited last, not bolted on.

## 5. Getting it out of the room

**Today there is no route planning.** `d3.Retrievable` extrudes an item's face straight
outward and asks whether that box-shaped channel is clear — one of five axis directions,
in a straight line. `OpenFaces` extends the run to a wall, but the wall is open along its
*entire* length. So the library can answer "can this be pulled off the shelf" and cannot
answer "can it then get to the door".

For a trailer loaded through its end those are the same question. For a room they are not:
you pull a box off a shelf, turn, and carry it down an aisle to a door that is two metres
wide in a wall that is six. Aisles exist for exactly that, and nothing currently checks
they connect to anything.

### 5.1 Two separate questions

1. **Extraction** — can the item leave its slot? Already built (§14 of the bearing plan).
2. **Egress** — from the space it is extracted into, can it reach an exit? New.

Keeping them apart matters: an item can be perfectly extractable and still be behind a
wall of other shelving, and the fix for each is different — the first is about its
neighbours, the second about the layout.

### 5.2 Exits are regions, not faces

An `Exit` is a rectangle on a wall: which wall, its span along that wall, and its height
range. A doorway is 0.9 wide and 2.0 high in a 6-metre wall; a roller door is most of one
end; a loft hatch is in the ceiling. `OpenFaces` — a whole face, all of it — is the
degenerate case and should become sugar for one exit covering the face rather than a
parallel mechanism.

### 5.3 How egress is decided

The free space is everything not occupied by an item or an impermeable zone. **Permeable
zones are free**, which is the whole reason `Zone.Permeable` exists: an aisle is not an
obstacle, it is the route.

Given that, egress is reachability for a body of the item's size:

- Discretise the room at a resolution the caller picks — the smallest item dimension is a
  reasonable default.
- A cell is *traversable for this item* if the item's bounding box, placed there, hits
  nothing. This is an erosion of free space by the item's size, and it is why a narrow
  aisle blocks a wide box while passing a small one: the check is per item, not per room.
- Flood-fill from the exits through traversable cells. The item can leave if the space it
  extracts into is in the filled set.

### 5.4 Rotation is part of the route

Turning a box on its side to get it through a door is a normal thing to do, and the
library already knows which items may be turned: `allow_rotate` per item, expanded by
`computeOrientations` into the distinct axis-aligned orientations — at most six for a box,
fewer when two dimensions match.

So the search space is **(cell × orientation)**, not cell alone, with two kinds of move:

- **Translate** — neighbouring cell, same orientation, the usual erosion test.
- **Rotate in place** — same cell, different permitted orientation.

A rotation sweeps volume that neither the start nor the end pose occupies, so testing
both poses would be optimistic — the classic way to produce a route that cannot actually
be walked. The conservative test is that the *swept* region is clear: for a turn about the
vertical axis, a square of side equal to the footprint diagonal. Cheap, axis-aligned, and
wrong only in the safe direction.

An item that may not rotate has exactly one orientation, and the search collapses to pure
translation. That is the special case, not the rule — which is the opposite of how an
earlier draft of this plan had it.

### 5.5 A container is as restricted as its contents

A carton of freely-tumbling items may be turned on its side. The same carton with one
this-side-up item in it may not. So a container's permitted orientations are the
**intersection** of its own and every one of its contents': one restricted item restricts
the whole box, and the rule composes upward through nesting, a pallet being as restricted
as its strictest carton.

This matters beyond egress — it is how the carton should be *packed* at the level above,
not just how it is carried out. It holds whenever the container moves as a unit; §5.6 is
the case where it does not.

**Nested packing gets this wrong today**, though in the safe direction: `doNestedPack`
builds each carton item without setting `AllowRotate`, so it defaults to false and no
carton is ever turned. That protects the this-side-up case by never exercising the
freedom, at the cost of every carton whose contents would happily tumble. Replacing it
with the intersection is a small, self-contained change to existing code and does not
depend on the rest of this plan.

### 5.6 Unpacking to get it out

§5.5 assumes a container leaves as a unit. Often it does not: you empty the tote where it
stands, carry the contents out, and take the empty box separately. If that is allowed, the
contents' orientation limits stop constraining the container at all — they are not in it
when it moves.

That is a real escape from the inheritance rule, and a per-container toggle:

- **Sealed** (default) — never unpacked. A shrink-wrapped pallet, a taped crate, anything
  where opening it is a loss. §5.5 applies in full: the container inherits its contents'
  restrictions and carries their weight.
- **May unpack** — the solver decides. Unpack only if the container cannot otherwise get
  out.
- **Unpack expected** — always emptied in place. A shelf tote you pull things out of
  rather than carry.

Sealed is the default because unpacking is an assumption about how the space will be used,
and assuming it when it is not true produces a plan whose boxes cannot actually leave.

**Unpacking moves the constraint, it does not remove it.** What changes:

| | Carried whole | Unpacked first |
|---|---|---|
| Container's orientations | intersection with contents (§5.5) | its own |
| Container's weight to carry | laden | tare |
| Routes needed | one, for the container | one per item, plus one for the empty box |
| Must be openable in place | no | **yes** — clearance at its opening face |
| Cost to retrieve one item | open it | open it *and* handle everything in it |

So a container that may be unpacked trades one hard route for many easier ones, and buys
that with handling. The last row is the one that bites: fetching a single item from an
unpacked-in-place tote means touching everything above it, which is exactly the "digging"
cost `AccessCost.Fixed` is for — and it should be charged per *retrieval*, not once.

The new obligation is **openable in place**: an unpacked container must have room at its
opening face to get the lid off and reach in, which is the same clearance test §5.3
already does, applied to the container rather than to an item.

### 5.7 Not everything has to come out

Some things are installed once. Shelving is bolted to a wall; a cabinet is carried in
empty, assembled, and never leaves; a chest freezer goes in the corner and stays. Being
stuck is fine for those and fatal for a box of files.

So **removability is a per-item property**, not a global rule:

- Items default to *must be removable* when egress checking is on at all. That is the
  fail-closed direction: forgetting to mark a box is far worse than forgetting to mark a
  wardrobe, because the first produces a plan that quietly cannot be used.
- Fixtures — the storage units of §3 — are exempt by construction. They are not packed
  items, they are the furniture, and asking whether a bolted-down shelf can reach the door
  is meaningless.
- An item may opt out (`Removable: false`), which is the escape hatch for the heavy thing
  that is going in once.

This also gives the planner something useful to say: not just "this fits" but "these six
boxes fit and can be got out again; this one will be stuck behind the shelving".

### 5.8 What it costs, and what it buys

The flood fill is per distinct *(size, permitted-orientation set)* rather than per item,
so a room of uniform boxes costs one fill however many there are. Rotation multiplies each
fill by the number of orientations — at most six, usually one or two once equal dimensions
collapse — which is a constant factor, not a change in kind.

It runs **after** a candidate layout is built, not inside the placement gate: routing
every candidate placement would be far too slow, and unlike bearing there is no cheap
local test — a placement can block a route on the other side of the room.

That makes egress a *validator* over a finished plan, with the same consequence the nested
bearing check had: a plan that fails is reported, not repaired. The repair is the layout
search (§7) trying a wider aisle or fewer runs, which is a decision it is already making.

## 6. Stage A — unit geometry and room layout

`Compartments()` per unit kind, and `LayoutRuns(room, unit, clearance)` to fill a room
with runs of one unit type and the clearance strips between them: front to back, starting
with a strip so the door end is clear, stopping when a whole run no longer fits.

It returns the units *and* their clearance zones together, so the two cannot drift apart.
That is not hypothetical — hand-placing shelving and hand-placing its aisles is exactly
what went wrong building the demo, where moving one run meant moving two aisles, and it is
why the UI generator in `7420169` exists. Stage A is that generator moved into Go where
the planner can call it, generalised past shelving.

Pure geometry, so it is fully testable without a solve. Build it first.

## 7. Stage B — containerise and fill

- Items → containers with "loose" a legal answer, reusing the GBPP optional-item
  objective: a container earns its place only if its contents justify it.
- Containers and loose items → compartments, as a catalog solve where the bin types are
  the compartments from stage A. A drawer and a shelf gap are both just bins here, which
  is what makes one solver cover all the furniture.
- Emit the result as `Zone`s and placements so the existing 3-D view draws it with no
  rendering work.

## 8. Stage C — allocation and priority search

Two nested searches. **Across locations**: which room each item goes to, which is where
the access objective earns most of its value — the weekly things in the hall cupboard, the
decorations in the loft. **Within a room**: which unit types, how many runs, what
clearance, scored lexicographically on the caller's priority order.

The within-room space is small (a room holds a handful of runs), so it can be
near-exhaustive rather than heuristic, unlike the packing inside it.

**Objectives**, each needing to be one number comparable across candidates: units
installed (or their cost); net access cost (`d3.TotalAccessCost`); containers consumed;
spare capacity left.

Lexicographic rather than weighted, because a weighted score hides which objective is
actually deciding — and the caller asked to choose the priority, which is precisely the
thing a single blended number throws away.

## 9. What this is not

- **Not a warehouse simulator.** No pick sequencing, no travel time, no people. Egress
  (§5) answers *whether* a box can get out, not how long it takes or in what order things
  are fetched; the cost of reaching something is the static proxy in the bearing plan's
  §13.
- **Not a general motion planner.** Egress searches position and the item's *permitted
  axis-aligned orientations* (§5.4). Turning a box on its side to get it through a door is
  in; tilting it diagonally, carrying it at an angle, or shuffling it past an obstacle in
  a continuous arc is out. Those are things a person does and a `SO(3)` planner would need
  — the discrete orientation set is where the line sits.
- **Not a rack-engineering tool.** Compartment capacity is the load-bearing rule already
  there; deflection, anchoring and seismic bracing are out.
- **Not a free-form 2-D layout.** Furniture lines up in runs against walls; it does not
  tetris into corners. A general 2-D placement would be a worse model of how anyone
  actually fits out a room, as well as a much harder problem.
- **Not a furniture designer.** Unit types are an input. It chooses among what you have or
  can buy; it does not invent a shelf spacing.

## 10. Suggested order

1. **The container-inherits-orientation rule (§5.5).** Independent of everything else
   here, a small change to nested packing that already exists, and pessimistic today in a
   way that costs packing quality. Nothing needs to be designed first.
2. **Stage A**, with tests, for **open shelving only**. Pure geometry, and it replaces
   hand arithmetic that has already caused one wrong demo.
3. Drawers and cupboards as further unit kinds, once the shelving case is solid. The
   access-rule difference (§3) is the part to get right, and it deserves its own tests
   rather than riding along.
4. **Stage B** with a *fixed* layout in a *single* location, so containerising and filling
   can be judged without the searches moving underneath them.
5. **Exits and egress (§5)**, still against a fixed layout: translation first, rotation
   second, unpacking (§5.6) last. Before the layout search, because "can everything get
   out" is the constraint that search will be trying to satisfy, and building it first
   means tuning against a check that does not exist yet.

   Unpacking comes last of the three because it is the only one that can make a failing
   plan pass, so it will paper over bugs in the other two if it lands first.
6. Locations, then the within-room search, then the cross-location allocation.

Each step is useful on its own, which is the test of whether the decomposition is right.

## 11. Effort and viability

**Verdict: viable, and the cost is not where it looks.** The algorithms here are ordinary —
a flood fill, a strip layout, a catalog solve. What this codebase charges for is
*integration*: the load-bearing work in this repo had to be threaded through **eight**
separate solve paths (`pack3D`, `streamSolve`, the registry `auto`, `sweepRefine3D`'s
decoder, the balanced path, `containerFactory`, both nested levels), and each one was
found by testing behaviour rather than by reading code. Budget that tax per new
constraint, not per new algorithm.

**Heuristic is the right target, and the line is not where it is usually drawn.** Placement
quality can be heuristic — nobody needs the optimal shelf layout. Feasibility cannot: "will
this crush", "can this get out", "does this fit past the pipe" have to be exact, because a
plan that is 5% worse is fine and a plan that is wrong is worthless. So the search may be
greedy and the *checks* may not.

| Piece | Size | Why |
|---|---|---|
| Container inherits orientation (§5.5) | **S**, but see below | One computation on carton items |
| Stage A — geometry + layout (§6) | **S–M** | Pure geometry, no solver, fully testable; a JS version already exists to port |
| Drawers / cupboards (§3) | **M** | Shape is easy; the per-kind *access rule* is the work |
| Stage B — containerise + fill (§7) | **M–L** | Reuses catalog and GBPP, but GBPP is currently excluded from bearing and zones, so composing pays the integration tax |
| Egress: translation (§5.3) | **M** | Grid + flood fill, self-contained |
| Egress: rotation (§5.4) | **M**, risky | Needs the orientation model below, plus the swept-volume test |
| Egress: unpacking (§5.6) | **M** | Mostly bookkeeping once the above exists |
| Locations + searches (§8) | **L** | Two nested searches, lexicographic scoring, plus measurement against brute force |

### 11.1 A prerequisite the plan assumed and the code lacks

§5.5 intersects "permitted orientations". **There is no such set.** `Item3D` carries a
single `allowRotate bool`, and `computeOrientations` returns either all six axis-aligned
orientations or exactly one. "This side up" — the four rotations about the vertical — is
not expressible at all. Only `SolidBin3D`, on the voxel path, has a `RotationIndex`.

So §5.5 splits in two:

- **As a boolean AND** over contents: genuinely small, and fixes the pessimism in nested
  packing today. Cannot express this-side-up, so it is a partial rule.
- **As a true orientation set**: touches `Item3D`, `computeOrientations`, `ItemSpec`, and
  everywhere orientations are consumed. Medium, and a **shared prerequisite** — the
  bearing plan's §4.B deferred per-up-face crush limits for exactly the same missing
  model.

Doing the boolean version first is right, provided the plan does not then pretend the
rule is complete.

### 11.2 The two things that decide viability

Neither is effort. Both are unproven assumptions.

**Retrievability is expensive, and that is measured, not feared.** In a 3×3×1 tray of nine
unit boxes with no lifting, the gate admits **three**, where a checkerboard fits five. The
greedy packers cannot find the checkerboard because their candidate positions are corners
of what is already placed, so the access gaps are never offered. If that ratio carries into
real rooms, the planner produces sparse, disappointing layouts — and the constraint that
makes a plan *usable* is the one degrading it. The plan's mitigation is to apply it per
compartment, where a shelf gap is shallow and everything is reachable from the aisle. That
is plausible and **untested**, and it should be the first thing measured, because if it
fails the feature's value is in question rather than its cost.

**Egress has no local repair.** Bearing and extraction can be gated at placement because a
box only affects its neighbours. Blocking a *route* affects the far side of the room, so
egress can only validate a finished plan, and the only repair is the layout search trying
a wider aisle. If that loop turns out to thrash — widen, re-pack, fail differently — the
search cost is unbounded in a way none of the existing solvers are.

### 11.3 Cheapest way to find out

Before building any of it, a throwaway spike worth a fraction of the whole:

1. Take the basement preset that already exists — 54 boxes, three shelving runs, aisles.
2. Run the existing `RetrievableAll` over it per compartment and then over the whole room.
3. Compare the fill each allows against the unconstrained 54.

That answers the only question that matters — *does the usability constraint destroy the
packing* — using code that already exists, against a layout that already exists, in far
less time than Stage A. If per-compartment retrievability holds up, the rest is ordinary
work with a known integration tax. If it does not, the plan needs a different answer for
access before anything else is built.

## 12. Risks

- **Stage boundaries hide good answers.** Stage C is the mitigation, and it should be
  measured against a brute-force solve on small instances rather than assumed.
- **"Optimally" is four different things.** Hence the configurable priority. A single
  weighted score would have been easier and would have hidden which one was winning.
- **Retrievability is expensive** (bearing plan §14): a plan that is legal but seals boxes
  in is useless, yet the gate costs a lot of fill with greedy packers — three of nine in a
  small tray. Applying it per *compartment*, where a shelf gap is shallow and everything
  is reachable from the aisle, should avoid that; applying it to a whole room would not.
- **The furniture kinds differ in exactly one subtle way.** Not their shape — their access
  rule. A drawer comes out whole and its contents need no side clearance; a shelf's
  contents each need a clear face toward the aisle. Modelling both as "a box with bins in
  it" and forgetting that distinction would produce plans that look fine and cannot be
  used.
- **Egress has no cheap local test.** Bearing and extraction can be gated at placement
  because a box only affects its neighbours; blocking a route affects the far side of the
  room. So egress can only validate a finished plan, and the repair has to come from the
  layout search widening an aisle — which means a plan can be rejected with no obvious
  local cause. The message needs to name the blocked items and the aisle that failed them,
  or it will be untraceable.
- **Grid resolution is a correctness knob, not a performance one.** Too coarse and a route
  through a just-wide-enough gap is missed, or worse, a route through a just-too-narrow
  one is allowed. Erosion by the item's bounding box is conservative in the first
  direction; it must not be made optimistic in the second by rounding cells generously.
- **Rotation is the easy place to be optimistic.** Testing only the start and end poses of
  a turn ignores the volume swept between them, and a route built on that is one that
  cannot be walked. The swept-region test (§5.4) is the guard, and it needs a case where
  both poses fit and the turn does not — otherwise the bug ships looking correct.
- **The container-inherits rule has to hold at every level.** Intersecting contents'
  orientations is obvious one level deep and easy to drop when a carton goes inside a
  crate. A pallet is as restricted as its strictest carton, which is as restricted as its
  strictest item, and nothing in the existing nested code carries that today.
- **Unpacking looks free and is not.** It relaxes the container's own constraints by
  moving them onto its contents: more routes to find, a new openable-in-place obligation,
  and a per-retrieval handling cost for everything above the item you actually want. A
  solver allowed to unpack anything, charged nothing for it, will unpack everything and
  produce a plan that is miserable to live with. The handling cost has to be real before
  "may unpack" is offered.
