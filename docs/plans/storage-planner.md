# Plan: Storage planner — room, shelving, containers, items

**Status:** Proposed — nothing built. Captured before implementation because the
decomposition below is the design decision, and it is cheaper to argue with on paper than
after three packages exist.

**The ask:** "here are my shelving units available, here's my room, here are all my items,
and available containers — pack it all optimally."

Everything the library does today takes the containers as *given* and asks where the items
go. This asks the opposite as well: **what storage should exist, and where**. Shelving
choice and placement become decision variables.

---

## 1. What is being decided

| Decision | Today | Here |
|---|---|---|
| Which items in which container | catalog / GBPP | same, but **optional** per item |
| Where a container sits | 3-D packing | onto a *shelf level* |
| Which shelving to install | — | **new** |
| Where shelving goes in the room | hand-placed zones | **new** |

Answers taken from the user:

- **Containers are optional per item.** An item goes into one only if that helps; a large
  awkward thing may sit straight on a shelf. The solver weighs the container against the
  packing it buys — the shape `gbpp` already has for optional items.
- **The solver chooses the layout**: which shelving types, how many, and where, subject to
  aisle clearance.
- **Priority is configurable**, not a fixed objective. The candidates are fewest shelving
  units, best accessibility, fewest containers, most spare capacity — ordered by the
  caller and compared lexicographically, which is what `meta.LexBestOf` and the existing
  `LexObjectives` request field already do for bin counts.

## 2. Why a pipeline, and where it is exact

The joint problem — choose shelving, place it, choose containers, assign items, place
containers on levels — is not one that can be solved exactly at any useful size. It
decomposes into three stages, each of which maps onto machinery that exists:

1. **Containerise.** Items → container types, with "loose" a legal answer. This is the
   optional-item objective `gbpp` implements.
2. **Shelf-fit.** A shelving unit's levels are *just bins*: a level is `width × depth ×
   clear height`. So "how much shelving do I need" is bin packing over heterogeneous bin
   types, which `catalog` already does.
3. **Room layout.** Place the chosen units into the room with aisles between them. A 2-D
   strip layout, not a packing — shelving lines up in runs against walls, it does not
   tetris.

**The decomposition is the approximation.** Choosing containers before knowing the shelf
depth can pick a container that wastes a level; choosing shelving before knowing the
container sizes can install a unit whose levels do not divide well. Stage C closes some of
that by searching over the stage boundaries rather than running each once.

## 3. Stage A — shelving geometry and room layout

`ShelvingType`: footprint, board thickness, how many boards and how far apart, and an
optional cost. `Levels()` turns it into the bins it offers — one per usable gap, and
**including the floor space under the lowest board**, which is real storage and the easy
thing to forget.

`LayoutRuns(room, type, aisle)` fills a room with runs of one type and the aisles between
them, front to back, starting with an aisle so the door end is clear and stopping when a
whole run no longer fits. It returns the units *and* their aisles together, so the two
cannot drift apart. That is not hypothetical: hand-placing shelving and hand-placing its
aisles is exactly what went wrong building the demo, where moving one run meant moving two
aisles, and it is why the UI generator added in `7420169` exists. Stage A is that
generator moved into Go, where the planner can call it.

Pure geometry, so it is fully testable without a solve. Build it first.

## 4. Stage B — containerise and fill

- Items → containers with "loose" a legal answer, reusing the GBPP optional-item
  objective: a container earns its place only if its contents justify it.
- Containers and loose items → shelf levels, as a catalog solve where the bin types are
  the levels from stage A.
- Emit the result as `Zone`s and placements so the existing 3-D view draws it with no
  rendering work.

## 5. Stage C — priority search

Enumerate plausible layouts — which types, how many runs, which aisle width — and score
each lexicographically on the caller's priority order. The search space is small (a room
holds a handful of runs), so this can be near-exhaustive rather than heuristic, unlike the
packing inside it.

**Objectives**, each needing to be one number comparable across candidate layouts:
shelving units installed; net access cost (`d3.TotalAccessCost`); containers consumed;
spare capacity left.

Lexicographic rather than weighted, because a weighted score hides which objective is
actually deciding — and the caller asked to choose the priority, which is precisely the
thing a single blended number throws away.

## 6. What this is not

- **Not a warehouse simulator.** No routing, no pick paths, no time. Access cost is the
  static proxy already built in §13.
- **Not a rack-engineering tool.** Shelf capacity is the load-bearing rule already there;
  deflection, anchoring and seismic bracing are out.
- **Not a free-form 2-D layout.** Shelving lines up in runs against walls; it does not
  tetris into corners. A general 2-D placement would be a worse model of how anyone
  actually fits out a room, as well as a much harder problem.

## 7. Suggested order

1. Stage A alone, with tests. It is pure geometry and it replaces hand arithmetic that has
   already caused one wrong demo.
2. Stage B with a *fixed* layout, so containerising and shelf-filling can be judged
   without the layout search moving underneath them.
3. Stage C last, measured on small instances against a brute-force solve to find out what
   the stage boundaries are costing.

Each stage is useful on its own, which is the test of whether the decomposition is right.

## 8. Risks

- **Stage boundaries hide good answers.** Noted above; stage C is the mitigation, and it
  should be measured against a brute-force solve on small instances rather than assumed.
- **"Optimally" is four different things.** Hence the configurable priority. A single
  weighted score would have been easier and would have hidden which one was winning.
- **Retrievability is expensive** (§14): a shelf plan that is legal but seals boxes in is
  useless, yet the gate costs a lot of fill with greedy packers. The planner should apply
  it at the *level* scale, where a level is shallow and everything is reachable from the
  aisle, rather than to the room as a whole.
