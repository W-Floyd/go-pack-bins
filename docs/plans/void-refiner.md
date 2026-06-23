# Plan: Void-Refiner Post-Pass (void-guided ruin & recreate)

**Status:** Proposed — not started. Captured for later implementation.
**Shape:** A *post-pass* that takes any 3-D packing (a `pack.Result`) and tightens
it by relocating items into voids. Algorithm-agnostic: runs after ff/ffd/ems/
blocks/columns/… like `Settle`/`Compact` do.

---

## 1. Intuition

Every constructive packer leaves voids — gaps beside short stacks, pockets under
overhangs, the ragged space below the peak height. A void-refiner improves fill
*after* the fact:

1. **Find a void.**
2. **Pull an item (or a fused combination of items) down from the top** to fill
   it. Moving an item *down* into a void trades top-surface height for filled
   interior — it lowers the peak, raises compactness, and can empty the top layer
   (and ultimately a whole bin).
3. **If the void can't be filled as-is, widen it:** speculatively remove an item
   *adjoining* the void to merge its volume in, producing a larger void, and try
   to fill *that* better (typically re-placing the removed item plus a top item).
   Keep the move only if the result is strictly better; otherwise roll back.

Step 3 is the crux: it is a **void-guided ruin-and-recreate** local-search move,
a targeted cousin of the random `offline.RuinRecreate` we already have.

---

## 2. What it builds on (don't reinvent)

- **Void detection** — `d3.InternalVoids(binW,binD,binH, []PlacedBox) []VoidBox`
  ([`d3/voids.go`](../../d3/voids.go)) already returns exact, coordinate-compressed
  *sealed* internal voids (merged into maximal cuboids). For *open* (top-reachable)
  voids, reuse the EMS maximal-empty-space set: `NewEmptyMaximalSpace` + `Occupy`
  each placed box, then read `e.spaces` ([`d3/ems.go`](../../d3/ems.go)).
- **Insertion / support gating** — EMS `TryInsert` already places an item at the
  lowest feasible maximal space honouring `ContactSpec` (Bottom / NoFloating). The
  refiner reuses it to test "does this item fit this void, grounded?".
- **Combination fill** — `buildBlocks` / `findStack` ([`d3/blocks.go`](../../d3/blocks.go))
  assemble a set of items into a solid footprint-matching block. Reuse to fill a
  void with a *combination* rather than a single item.
- **Relocation plumbing** — `Settle` / `Compact` ([`d3/compact.go`](../../d3/compact.go))
  are the existing relocating post-passes and the model for wiring (see §8).
- **Targeted RR framing** — `offline.RuinRecreate`: this is RR whose ruin set is
  *chosen by void adjacency* instead of at random.

---

## 3. Definitions

- **Void `V`** — a maximal empty cuboid in a bin. Two kinds:
  - *open* — reachable from the bin top (the column of free space directly above
    `V`, within `V`'s footprint, is clear to the lid). An item can be lowered in.
  - *sealed* — `InternalVoids`; covered by item(s). Not directly fillable — the
    only way in is to remove a covering/adjoining item (→ §6).
- **Removable / "top-layer" item** — an item with *nothing resting on it*: no other
  item's bottom face sits on its top face with overlapping footprint. These are the
  leaves of the **support graph** (edge `A→B` iff `A.top ≈ B.bottom` and their X/Y
  footprints overlap). Only removable items may be relocated without dropping
  others; this is what "pull from the top layer" means precisely.
- **Objective** `J` (lower is better), evaluated per bin and summed:
  `J = (bins, trappedVoidVolume, Σ peakHeight)` compared **lexicographically**.
  A move is accepted only if it makes `J` *strictly* smaller. This guarantees
  monotic progress (no cycling) and that the ultimate win — emptying a bin —
  dominates.

---

## 4. Algorithm

```
RefineVoids(result, bin, spec, budget):
  build support graph; removable = leaves
  repeat until no accepted move this round OR budget exhausted:
    voids = openVoids(bin, placed)  ⨁ InternalVoids(bin, placed)   # sorted: largest first, deterministic
    progressed = false
    for V in voids (respect budget):
      if tryFill(V): progressed = true; continue            # direct fill
      if tryWiden(V): progressed = true                     # speculative removal
    re-derive placed / support graph / removable from accepted moves
    if !progressed: break

tryFill(V):                       # direct: lower top item(s) into an OPEN void
  cands = removable items that fit V in some orientation, grounded inside V
  pick the single item, else the best fused combination (buildBlocks over cands),
       that maximises filled volume of V (deterministic tie-break)
  if none: return false
  if J(after moving cand from its top spot into V) < J(before): commit; return true
  return false

tryWiden(V):                      # speculative ruin & recreate around V
  for A in adjoiningRemovable(V) (bounded fan, largest-adjacency first):
    snapshot A
    remove A  →  V' = mergedVoid(V, A)                       # A's cells join V
    # recreate: best (item or combination, incl. A itself) that fills V'
    fill = bestFill(V', removable ∪ {A})
    if fill exists AND J(after) < J(before): commit; return true
    rollback A
  return false
```

Notes:
- **Direct fill targets open voids; widening targets sealed (or stubborn open)
  voids** — removing the adjoining item is exactly what opens a sealed pocket.
- `bestFill` reuses EMS `TryInsert` for single items and `buildBlocks`/`findStack`
  for combinations, scoped to the void's footprint (as the column packer scopes a
  slice). Bound the combination search with the existing `findStack` node budget.
- Cross-bin moves (pull an item from bin N's top into bin M's void) are the lever
  for **bin reduction**; gate behind a flag and do them last (see §6, §9).

---

## 5. Cost

Per bin let `k` = items, `V` = maximal voids (`O(k)`), `T` = removable items
(`≤ k`, usually a small fraction), `R` = refinement rounds.

| Step | Cost |
|---|---|
| Void detection (EMS `Occupy` of all placed, or `InternalVoids`) | `O(k²)` per round (EMS prune is `O(k²)`; `InternalVoids` is `O(faces³)` on the compressed grid) |
| Support graph | `O(k²)` worst case (pairwise top/bottom adjacency); `O(k·local)` with the bin grid (`d3/boxgrid.go`) |
| Direct fill: scan removable per void | `O(V·T)`; with combination search `O(V·T·budget)` |
| Widening: remove each adjoining removable + refit | `O(V · A · fillcost)`, `A` = adjoining fan (small) |
| **Per round** | `≈ O(k²)` (detection-bound) + bounded search |
| **Total** | `O(R·k²)` — so **cap `R`** and the per-round void/widen counts |

This is a *bounded local search*, not a solver: it must take `context.Context` and
a `budget` (max rounds, max voids/round, max widen fan, `findStack` node budget),
sampling `ctx.Err()` in the round loop — exactly as the metaheuristics do. The
`boxgrid` broadphase (built for extreme-point/blf) should be reused to keep
adjacency/overlap queries local instead of `O(k)`.

Incremental EMS updates after a single move would drop the per-round `O(k²)`, but
that is a hard optimisation — **MVP rebuilds per round with a small `R` cap** and
relies on the existing prune to keep the rebuild cheap.

---

## 6. Pitfalls & mitigations

1. **Dropping a load-bearing item.** Removing an item that *supports* a stack drops
   that stack. → Only ever relocate/remove **removable (leaf)** items. The "remove
   an adjoining item" in §1 means *adjoining **removable*** items. (A full
   cascade-ruin of a supported sub-tree is possible but is real RR territory —
   defer; flag clearly if attempted.)
2. **Floating / overlap after a move.** Placing into a void box ≤ the void is
   overlap-free by construction; grounding is enforced by reusing EMS `TryInsert`
   with the active `ContactSpec` (`NoFloating`/`Bottom`). Never bypass the gate.
3. **Cycling / non-termination.** Accept only **strictly `J`-improving** moves and
   cap rounds. Without the strict rule, "move A into a void that re-opens where A
   was" can oscillate forever.
4. **Gaming the metric.** Filling `V` while opening an equal void elsewhere nets
   zero — the strict-improvement rule rejects it automatically.
5. **Speculative rollback correctness.** `tryWiden` must snapshot the removed
   item's full placement (bin, x,y,z, orientation) and restore *exactly* on reject
   — including not leaving it consumed/unplaced. Cheap (one/few items) but must be
   airtight, or the pass silently loses items.
6. **Re-deriving state after each accepted move.** Voids, the support graph, and
   the removable set all change. MVP recomputes them once per round (batch accepted
   moves, then rebuild); per-move incremental update is a later optimisation.
7. **Determinism.** Voids and candidate items must be processed in a sorted,
   reproducible order (e.g. void volume desc, then x,y,z; item by id) so streamed
   output and tests are stable — same requirement we hit in EMS/columns.
8. **Contact constraints.** Under an active anti-slosh/support spec, a relocation
   that satisfies `Bottom`/`NoFloating` may still worsen lateral contact; either
   fold `SideX/SideY` into `J` or run the existing `Compact` pass afterwards.
9. **Cross-bin bookkeeping.** Cross-bin moves change `bins_used`, free a bin only
   when its *last* item leaves, and must renumber/remove the emptied bin. Keep this
   out of the MVP (per-bin only); add as the dedicated bin-reduction phase.
10. **Diminishing returns vs cost.** On volume-bound instances the refiner won't cut
    bins (cf. the columns 2-D-repack experiment) — it improves compactness/peak,
    not bin count. Report honestly which metric moved; don't run it where it can't
    pay (gate by a quick "is there sub-peak void worth chasing?" check).

---

## 7. Streaming representation

The refiner is a **relocating post-pass**, so it maps onto the existing
"reposition" mechanism ([`packapi/packapi.go`](../../packapi/packapi.go): the
`StreamFrame{Type:"reposition", Placements:…}` emitted after the batches and before
`done`, already consumed by the front-end's `onFrame`). Three fidelity levels:

- **MVP — one final `reposition` frame.** Run the refiner after the live stream
  completes; emit every item's final position in a single reposition frame. Zero
  protocol change (identical to how `settle`/`compact` already deliver). The UI
  re-renders the tightened packing in one step. Add `columns`/the chosen algos to
  the post-pass branch in `streamSolve` and to `isStreamable` reasoning.
- **Phase 2 — progressive `reposition` frames.** Emit one reposition frame per
  *round* (or per N accepted moves) so the viewer watches the packing tighten.
  Reuses the same frame type; the only front-end change is tolerating *repeated*
  reposition frames (today exactly one arrives). Each frame is the full current
  placement set — simple, idempotent.
- **Phase 3 — delta `refine` frames (animated "pull").** A new frame type carrying
  only the moved items as `{item_id, from:{x,y,z,bin}, to:{x,y,z,bin}}` so the UI
  can animate each item sliding/dropping from its old spot into the void. Most
  polished, most work (new frame type + tween logic). Optional.

Because the refiner needs the *whole* packing to find voids, it is inherently
post-stream — it cannot be a mid-solve `batch`. The reposition slot is the correct
home. It must remain cancellable, and on cancellation emit the best-so-far
positions (or none, leaving the un-refined stream result intact).

---

## 8. Integration

- **Location:** `d3.RefineVoids(result *pack.Result, w,d,h float64, spec ContactSpec, budget RefineBudget) (moved bool)` next to `Settle`/`Compact`, operating on `[]*Placement3D`.
- **packapi wiring:** an opt-in post-pass, like the anti-slosh `Compact` branch in
  `pack3D` and the reposition branch in `streamSolve` — applied to a 3-D result
  regardless of algorithm. Surface a request flag (`req.RefineVoids bool` or an
  `AlgorithmOptions` knob: `refine_rounds`, `refine_budget`).
- **Frontend:** a checkbox ("Tighten voids (post-pass)") alongside the contact
  controls; it just sets the flag. Add to the dual-mode (`/api/*` + WASM) paths.
- **Determinism + tests:** a `packapi` test asserting `RefineVoids` never increases
  `J`, never drops/overlaps items, and is idempotent on a second pass (fixed point).

---

## 9. Phasing

1. **MVP:** per-bin, open-void **direct fill** only (single items, no widening,
   no combinations), strict-`J`, capped rounds, one final reposition frame. Proves
   the plumbing and the metric.
2. **+ combinations:** fuse `buildBlocks`/`findStack` candidates to fill a void.
3. **+ speculative widening** (§6.1-restricted: removable adjoiners only) with
   snapshot/rollback.
4. **+ progressive reposition streaming** (Phase 2).
5. **+ cross-bin moves & bin reduction** (the high-value, highest-risk step).
6. **+ animated `refine` deltas** (Phase 3), incremental EMS updates.

---

## 10. Open questions

- Best `J`: is lexicographic `(bins, trappedVoid, peak)` right, or should
  compactness (envelope void-freeness, the bench metric) be primary?
- Should the refiner be its own algorithm name (`refine` as a meta-wrapper over a
  base packer) or purely a flag on any 3-D solve? (Flag is cleaner; a meta-wrapper
  lets `auto`/`BestOf` race "X" vs "X+refine".)
- Widening fan: how many adjoining items to try per void before giving up, and
  whether to allow depth-2 widening (remove two adjoiners) under budget.
- Does it ever beat a strong base packer enough to justify its cost on the
  benchmark instances, or is it mainly a quality/stability tool for specific
  (overhang-heavy, bridged) packings — e.g. heightmap output?
