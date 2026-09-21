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

## 5. Stage A — unit geometry and room layout

`Compartments()` per unit kind, and `LayoutRuns(room, unit, clearance)` to fill a room
with runs of one unit type and the clearance strips between them: front to back, starting
with a strip so the door end is clear, stopping when a whole run no longer fits.

It returns the units *and* their clearance zones together, so the two cannot drift apart.
That is not hypothetical — hand-placing shelving and hand-placing its aisles is exactly
what went wrong building the demo, where moving one run meant moving two aisles, and it is
why the UI generator in `7420169` exists. Stage A is that generator moved into Go where
the planner can call it, generalised past shelving.

Pure geometry, so it is fully testable without a solve. Build it first.

## 6. Stage B — containerise and fill

- Items → containers with "loose" a legal answer, reusing the GBPP optional-item
  objective: a container earns its place only if its contents justify it.
- Containers and loose items → compartments, as a catalog solve where the bin types are
  the compartments from stage A. A drawer and a shelf gap are both just bins here, which
  is what makes one solver cover all the furniture.
- Emit the result as `Zone`s and placements so the existing 3-D view draws it with no
  rendering work.

## 7. Stage C — allocation and priority search

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

## 8. What this is not

- **Not a warehouse simulator.** No routing, no pick paths, no time. Access cost is the
  static proxy already built in the bearing plan's §13.
- **Not a rack-engineering tool.** Compartment capacity is the load-bearing rule already
  there; deflection, anchoring and seismic bracing are out.
- **Not a free-form 2-D layout.** Furniture lines up in runs against walls; it does not
  tetris into corners. A general 2-D placement would be a worse model of how anyone
  actually fits out a room, as well as a much harder problem.
- **Not a furniture designer.** Unit types are an input. It chooses among what you have or
  can buy; it does not invent a shelf spacing.

## 9. Suggested order

1. Stage A alone, with tests, for **open shelving only**. Pure geometry, and it replaces
   hand arithmetic that has already caused one wrong demo.
2. Drawers and cupboards as further unit kinds, once the shelving case is solid. The
   retrievability difference (§3) is the part to get right, and it deserves its own tests
   rather than riding along.
3. Stage B with a *fixed* layout in a *single* location, so containerising and filling can
   be judged without the searches moving underneath them.
4. Locations, then the within-room search, then the cross-location allocation.

Each step is useful on its own, which is the test of whether the decomposition is right.

## 10. Risks

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
