package d3

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"

	"github.com/W-Floyd/go-pack-bins/d2"
	"github.com/W-Floyd/go-pack-bins/pack"
)

// BlockPacker is a layered "block-building" 3-D packer. It packs the container in
// horizontal layers from the floor up; each layer has a target height H (the
// tallest item that still fits the remaining height) and is filled with solid,
// waste-free rectangular *blocks* of exactly that height, whose footprints are
// then tiled into the layer floor with 2-D MaxRects.
//
// Blocks are built in tiers (this is the bounded tier-1/tier-2 version; tile-and-
// join tiers are a planned extension):
//
//  1. Direct — an item already H tall (laid flat), used as a one-item block.
//  2. Vertical stack — several items that share a footprint and whose heights sum
//     to exactly H (e.g. an 8×8×6 under an 8×8×2), fused into one 8×8×H column.
//
// Each item is considered in every orientation whose footprint fits the bin, so a
// rotatable item can contribute at up to three different heights; once an item is
// placed (in some block) it is consumed from all the others. Like d3.LAFF this
// packer manages its own geometry and bins, but it commits each placement through
// an observer as the block lands, so a solve streams its progress.
//
// Shallow-top deferral (multi-bin): a bin's final layer is often shorter than the
// tallest item still to be packed — and filling it would "steal" the short items
// that later bins need to close their own gaps. So a layer is only built when its
// available height can hold the tallest unpacked item; otherwise the rest of the
// bin (a shallow top) is *deferred*. Deferred regions are revisited once the
// remaining items have shrunk to fit them (cap ≥ that height), and are preferred
// over opening a new bin so the leftover space is reused. Because a too-tall item
// could never have gone in the shallow top anyway, deferral never costs an extra
// bin — it just keeps the small items available for the full-height layers that
// benefit most.
//
// The "tallest unpacked item" is measured by each item's *flattest* orientation
// (its minimum height), so a rotatable long-skinny box — which can lie flat —
// never triggers a deferral; only items tall in every orientation do.
//
// Final layer: when every remaining item fits flat (in its flattest orientation)
// in a single layer, they are laid flat rather than fused into vertical stacks —
// there is nothing above the top layer, so stacking would only add height and
// voids. Stacking is used on the top layer only when the items do not all fit
// flat, i.e. only when required.
type BlockPacker struct {
	w, d, h  float64
	observer pack.PlaceObserver
	maxStack int // cap on items fused into one vertical stack (bounds the search)
}

// NewBlockPacker creates a block-building packer for a bin of the given dimensions.
func NewBlockPacker(w, d, h float64) *BlockPacker {
	return &BlockPacker{w: w, d: d, h: h, maxStack: 6}
}

// Observe registers a per-placement callback (pack.Observable), enabling streaming.
func (bp *BlockPacker) Observe(fn pack.PlaceObserver) { bp.observer = fn }

// PackAll runs the solve with no cancellation.
func (bp *BlockPacker) PackAll(items []pack.Item) (pack.Result, error) {
	return bp.PackAllCtx(context.Background(), items)
}

// Name satisfies pack.OfflinePacker so the packer can join a meta.BestOf race.
func (bp *BlockPacker) Name() string { return "Blocks" }

const blockEps = 1e-9

// orient is one valid orientation of an item: a footprint (fw×fd) and height fh,
// kept only when the footprint fits the bin floor and fh fits the bin.
type orient struct{ fw, fd, fh float64 }

// pitem is a placeable item with its distinct valid orientations. item is the
// original, kept so leftovers can be handed to LAFF for a flat finish.
type pitem struct {
	id     string
	item   pack.Item
	orient []orient
}

// sub is one real item inside a block, at z-offset dz above the block's base,
// occupying the block footprint (fw×fd) for height fh.
type sub struct {
	id     string
	dz, fh float64
}

// block is a solid layer-height column: a footprint, the item indices it consumes,
// and the stacked sub-items that realise it.
type block struct {
	fw, fd float64
	idxs   []int
	subs   []sub
}

// region is a free vertical span [base, base+cap) within an already-opened bin
// awaiting fill — produced when a bin's shallow top is deferred.
type region struct {
	binID string
	base  float64
	cap   float64
}

// PackAllCtx packs items into layers of fused blocks, committing each placement
// through the observer as it lands. It samples ctx between layers and inside the
// stack search, returning the partial result and ctx.Err() if cancelled.
func (bp *BlockPacker) PackAllCtx(ctx context.Context, items []pack.Item) (pack.Result, error) {
	var result pack.Result

	// Collect placeable items and their valid orientations; anything that fits no
	// orientation is immediately unplaced (as LAFF does).
	its := make([]*pitem, 0, len(items))
	for _, raw := range items {
		i3, ok := raw.(*Item3D)
		if !ok {
			result.Unplaced = append(result.Unplaced, raw.ID())
			continue
		}
		var os []orient
		for _, o := range i3.Orientations() {
			fw, fd, fh := o[0], o[1], o[2]
			if fh <= bp.h+blockEps && fw <= bp.w+blockEps && fd <= bp.d+blockEps {
				os = append(os, orient{fw, fd, fh})
			}
		}
		if len(os) == 0 {
			result.Unplaced = append(result.Unplaced, i3.ID())
			continue
		}
		its = append(its, &pitem{id: i3.ID(), item: i3, orient: os})
	}

	consumed := make([]bool, len(its))
	binIdx := 0
	var nextID int
	var deferred []region // shallow tops parked until a short-enough item remains

	// live holds the indices of still-unconsumed items, in ascending (original)
	// order. The per-layer scans iterate it instead of all items, so their cost
	// shrinks as the pack consumes items rather than staying O(total) every layer.
	// It is recompacted (consumed entries dropped, order preserved) here and inside
	// fillRegion; functions still consult consumed[] for items consumed mid-layer.
	live := make([]int, len(its))
	for i := range live {
		live[i] = i
	}

	for remaining(its, consumed) {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		live = compactLive(live, consumed)
		// The decisive height: the largest, over remaining items, of each item's
		// *flattest* orientation. A region (or fresh bin) may only be filled when its
		// height can hold this — otherwise filling it is the shallow-top steal we
		// avoid. Using the flattest orientation means a rotatable long-skinny item
		// (which can lie flat) never forces a deferral; only items tall in every
		// orientation do.
		flat := bp.flattestTallest(its, live, consumed)
		if flat <= 0 {
			break // no remaining item fits any bin (pre-filtered; defensive)
		}

		// Final stage: once the remaining items fit (laid flat) within a single bin,
		// first slot whatever fits into the voids/wells of the already-packed bins,
		// then lay the rest flat (LAFF) — as flat as possible, even if a few items
		// can't be fully flattened — rather than standing them up in tall stacks.
		if bp.endgame(its, live, consumed) {
			bp.finalStage(&result, its, consumed, &binIdx)
			break
		}

		// Prefer reusing a deferred shallow top now tall enough (tightest first, to
		// reserve roomier regions); else open a fresh bin.
		if ri := pickDeferred(deferred, flat); ri >= 0 {
			r := deferred[ri]
			deferred = append(deferred[:ri], deferred[ri+1:]...)
			rem, _, l := bp.fillRegion(ctx, &result, r, its, live, consumed, &nextID)
			live = l
			if err := ctx.Err(); err != nil {
				return result, err
			}
			if rem != nil {
				deferred = append(deferred, *rem)
			}
			continue
		}

		binID := fmt.Sprintf("blocks-bin-%d", binIdx)
		rem, placed, l := bp.fillRegion(ctx, &result, region{binID: binID, base: 0, cap: bp.h}, its, live, consumed, &nextID)
		live = l
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if placed == 0 {
			break // safety against a non-progressing bin (no empty bin recorded)
		}
		result.Bins = append(result.Bins, NewBin(binID, bp.w, bp.d, bp.h, NewExtremePointStrategy(bp.w, bp.d, bp.h)))
		binIdx++
		if rem != nil {
			deferred = append(deferred, *rem)
		}
	}

	// Any item still unconsumed (e.g. after a non-progressing safety break) is
	// reported unplaced rather than silently dropped.
	for i, it := range its {
		if !consumed[i] {
			result.Unplaced = append(result.Unplaced, it.id)
		}
	}
	return result, nil
}

// fillRegion stacks height-matched layers up region r from its base. A layer is
// built only while its remaining height can hold the tallest unpacked item; the
// first time it cannot, the rest of r is returned as a deferred region (a shallow
// top) instead of being filled with short items. Returns the deferred remainder
// (nil if the region was filled to its top / exhausted), items placed, and the
// updated live index-set (recompacted as items are consumed; the caller must
// adopt it so the shared backing array never accumulates duplicate indices).
func (bp *BlockPacker) fillRegion(ctx context.Context, result *pack.Result, r region, its []*pitem, live []int, consumed []bool, nextID *int) (*region, int, []int) {
	base := r.base
	top := r.base + r.cap
	placed := 0
	for base < top-blockEps {
		if err := ctx.Err(); err != nil {
			return nil, placed, live
		}
		live = compactLive(live, consumed) // drop items consumed by earlier layers
		cap := top - base
		H := bp.maxHeight(its, live, consumed, cap) // tallest item fitting this span
		if H <= 0 {
			break // nothing left fits the remaining height of this region
		}
		if cap < bp.flattestTallest(its, live, consumed)-blockEps {
			// Shallow top: some remaining item can't fit this span even laid flat.
			// Defer the rest of the region rather than steal short items for a thin
			// layer; it will be revisited once the remaining items fit.
			return &region{binID: r.binID, base: base, cap: cap}, placed, live
		}
		floor := d2.NewBin(r.binID, bp.w, bp.d, d2.NewMaxRectsDefault(bp.w, bp.d))
		// First fill the layer with blocks exactly H tall (clean layer lines).
		n := bp.placeOnFloor(floor, result, r.binID, bp.buildBlocks(its, live, consumed, H), consumed, base, nextID)
		// Last resort: drop the tallest blocks that still fit the layer height into
		// any leftover floor cells, so a gap becomes a short item with a small void
		// above it rather than a full-height void. The layer top stays at base+H, so
		// the layer lines are preserved.
		n += bp.placeOnFloor(floor, result, r.binID, bp.buildFallbackBlocks(its, live, consumed, H), consumed, base, nextID)
		if n == 0 {
			break // safety: no block fit the floor (shouldn't happen at the base)
		}
		placed += n
		base += H
	}
	return nil, placed, live
}

// compactLive returns live with consumed indices dropped, preserving order. It
// filters in place (reusing the backing array), so the caller must adopt the
// returned slice and not keep the old header.
func compactLive(live []int, consumed []bool) []int {
	out := live[:0]
	for _, i := range live {
		if !consumed[i] {
			out = append(out, i)
		}
	}
	return out
}

// pickDeferred returns the index of the tightest deferred region whose height can
// hold an item of the given height, or -1 if none can. Tightest-first reuse keeps
// the roomier parked regions available for taller items.
func pickDeferred(deferred []region, tallest float64) int {
	best := -1
	for i, r := range deferred {
		if r.cap >= tallest-blockEps && (best < 0 || r.cap < deferred[best].cap) {
			best = i
		}
	}
	return best
}

// flattestTallest is the largest, over the available items, of each item's
// *flattest* orientation height (the minimum height across its valid
// orientations). It is the least height a layer must have to accommodate every
// remaining item when each is laid as flat as it can be — so the deferral test
// ignores items that are only tall in some orientation (e.g. a rotatable
// long-skinny box), treating them by their lie-flat height.
func (bp *BlockPacker) flattestTallest(its []*pitem, live []int, consumed []bool) float64 {
	best := 0.0
	for _, i := range live {
		it := its[i]
		if consumed[i] || len(it.orient) == 0 {
			continue
		}
		min := it.orient[0].fh
		for _, o := range it.orient[1:] {
			if o.fh < min {
				min = o.fh
			}
		}
		if min > best {
			best = min
		}
	}
	return best
}

// remainingItems returns the still-unconsumed items (their original pack.Items).
func remainingItems(its []*pitem, consumed []bool) []pack.Item {
	var out []pack.Item
	for i, it := range its {
		if !consumed[i] {
			out = append(out, it.item)
		}
	}
	return out
}

// endgame reports whether the packing has reached its final stage: the remaining
// items, laid flat, fit within a single bin. When true the finish lays them flat
// (LAFF) rather than standing them up in tall stacks. A cheap volume check gates
// the LAFF probe so it only runs once a bin's worth (or less) remains — keeping
// huge multi-bin solves fast.
func (bp *BlockPacker) endgame(its []*pitem, live []int, consumed []bool) bool {
	vol := 0.0
	for _, i := range live {
		if !consumed[i] {
			vol += orientVolume(its[i])
		}
	}
	if vol <= 0 || vol > bp.w*bp.d*bp.h+blockEps {
		return false // more than one bin's worth left — still the body phase
	}
	r, _ := LAFF(remainingItems(its, consumed), bp.w, bp.d, bp.h)
	return len(r.Unplaced) == 0 && r.BinsUsed() <= 1
}

// finalStage finishes the solve once the remaining items fit flat within one bin.
// First it slots whatever fits into the voids/wells of the already-packed bins
// (reconstructing each bin's free space with EMS), then it lays the rest flat
// with LAFF — as flat as possible, opening a fresh bin only for the leftovers.
// Resting support is required for void placements, so nothing floats. Bins with
// very many boxes are skipped for the (super-linear) void scan to keep huge
// single-bin solves fast.
func (bp *BlockPacker) finalStage(result *pack.Result, its []*pitem, consumed []bool, binIdx *int) {
	const voidScanMaxBoxes = 1200 // cap EMS reconstruction cost per bin

	commit := func(binID, itemID string, x, y, z, w, d, h float64) {
		p := &Placement3D{binID: binID, itemID: itemID, X: x, Y: y, Z: z, W: w, D: d, H: h}
		result.Placements = append(result.Placements, p)
		if bp.observer != nil {
			bp.observer(p)
		}
	}

	// Reconstruct each existing bin's free space from its committed boxes (skipping
	// bins too full of boxes to scan cheaply).
	count := map[string]int{}
	for _, p := range result.Placements {
		count[bin3DID(p)]++
	}
	var binIDs []string
	ems := map[string]*EmptyMaximalSpace{}
	for _, b := range result.Bins {
		id := b.ID()
		if count[id] == 0 || count[id] > voidScanMaxBoxes {
			continue
		}
		e := NewEmptyMaximalSpace(bp.w, bp.d, bp.h)
		e.contact = ContactSpec{NoFloating: true} // void placements must rest on something
		ems[id] = e
		binIDs = append(binIDs, id)
	}
	for _, raw := range result.Placements {
		pl, ok := raw.(*Placement3D)
		if !ok {
			continue
		}
		if e := ems[pl.binID]; e != nil {
			e.Occupy(pl.X, pl.Y, pl.Z, pl.W, pl.D, pl.H)
		}
	}

	// Void-fill: largest items first into the first existing bin whose free space
	// accepts them (even partial-height wells).
	idxByVol := make([]int, 0, len(its))
	for i := range its {
		if !consumed[i] {
			idxByVol = append(idxByVol, i)
		}
	}
	sort.SliceStable(idxByVol, func(a, b int) bool {
		return orientVolume(its[idxByVol[a]]) > orientVolume(its[idxByVol[b]])
	})
	for _, i := range idxByVol {
		orients := orientationsOf(its[i])
		for _, id := range binIDs {
			if x, y, z, w, d, h, ok := ems[id].TryInsert(orients); ok {
				commit(id, its[i].id, x, y, z, w, d, h)
				consumed[i] = true
				break
			}
		}
	}

	// Whatever remains is laid out flat with LAFF (smallest dimension vertical, in
	// flat layers) — as flat as possible — into one or more fresh bins. LAFF's bins
	// are renamed to this packer's scheme and its placements re-emitted.
	leftover := remainingItems(its, consumed)
	if len(leftover) == 0 {
		return
	}
	idxByID := map[string]int{}
	for i, it := range its {
		idxByID[it.id] = i
	}
	r, _ := LAFF(leftover, bp.w, bp.d, bp.h)
	binMap := map[string]string{}
	for _, raw := range r.Placements {
		pl, ok := raw.(*Placement3D)
		if !ok {
			continue
		}
		newID, seen := binMap[pl.binID]
		if !seen {
			newID = fmt.Sprintf("blocks-bin-%d", *binIdx)
			binMap[pl.binID] = newID
			result.Bins = append(result.Bins, NewBin(newID, bp.w, bp.d, bp.h, NewExtremePointStrategy(bp.w, bp.d, bp.h)))
			*binIdx++
		}
		commit(newID, pl.itemID, pl.X, pl.Y, pl.Z, pl.W, pl.D, pl.H)
		if i, ok := idxByID[pl.itemID]; ok {
			consumed[i] = true
		}
	}
}

// bin3DID returns the bin id a placement belongs to.
func bin3DID(p pack.Placement) string {
	if pl, ok := p.(*Placement3D); ok {
		return pl.binID
	}
	return ""
}

// orientationsOf returns an item's valid orientations as (w,d,h) triples for EMS.
func orientationsOf(it *pitem) [][3]float64 {
	os := make([][3]float64, len(it.orient))
	for i, o := range it.orient {
		os[i] = [3]float64{o.fw, o.fd, o.fh}
	}
	return os
}

// orientVolume is the (orientation-invariant) volume of an item.
func orientVolume(it *pitem) float64 {
	if len(it.orient) == 0 {
		return 0
	}
	o := it.orient[0]
	return o.fw * o.fd * o.fh
}

// maxHeight is the tallest item height (over available items' valid orientations)
// that fits within cap — the height of the next layer.
func (bp *BlockPacker) maxHeight(its []*pitem, live []int, consumed []bool, cap float64) float64 {
	best := 0.0
	for _, i := range live {
		if consumed[i] {
			continue
		}
		for _, o := range its[i].orient {
			if o.fh <= cap+blockEps && o.fh > best+blockEps {
				best = o.fh
			}
		}
	}
	return best
}

// buildBlocks proposes height-H blocks from the available items: tier 1 (items that
// are H tall) and tier 2 (same-footprint stacks whose heights sum to H). Candidate
// blocks may share items; placeLayer resolves that by skipping any block whose
// items were already consumed. Blocks are returned largest-footprint-first so the
// layer floor seeds with big blocks (as LAFF does).
func (bp *BlockPacker) buildBlocks(its []*pitem, live []int, consumed []bool, H float64) []block {
	var blocks []block

	// Tier 1: an item already H tall.
	for _, i := range live {
		it := its[i]
		if consumed[i] {
			continue
		}
		for _, o := range it.orient {
			if math.Abs(o.fh-H) <= blockEps {
				blocks = append(blocks, block{
					fw: o.fw, fd: o.fd, idxs: []int{i},
					subs: []sub{{id: it.id, dz: 0, fh: o.fh}},
				})
				break
			}
		}
	}

	// Tier 2: group shorter items by footprint, then carve exact height-sum stacks.
	type ent struct {
		idx int
		fh  float64
	}
	groups := map[[2]float64][]ent{}
	seen := map[[2]float64]bool{} // reused per item (cleared) — footprint-key dedup
	for _, i := range live {
		it := its[i]
		if consumed[i] {
			continue
		}
		clear(seen) // one (idx, height) per footprint key per item
		for _, o := range it.orient {
			if o.fh >= H-blockEps {
				continue // H-tall handled by tier 1; taller can't go in this layer
			}
			key := [2]float64{math.Min(o.fw, o.fd), math.Max(o.fw, o.fd)}
			if seen[key] {
				continue
			}
			seen[key] = true
			groups[key] = append(groups[key], ent{i, o.fh})
		}
	}
	for _, key := range sortedKeys(groups) { // sorted for deterministic block order
		es := groups[key]
		sort.Slice(es, func(a, b int) bool { return es[a].fh > es[b].fh }) // tallest-first prunes the search
		used := make([]bool, len(es))
		// heights/pos are rebuilt from the not-yet-used entries each round; reuse
		// the backing arrays (reset to [:0]) instead of reallocating per round —
		// a big footprint group otherwise allocates O(entries × blocks) of them,
		// which dominated the packer's bytes.
		heights := make([]float64, 0, len(es))
		pos := make([]int, 0, len(es))
		for {
			heights, pos = heights[:0], pos[:0]
			for j, e := range es {
				if !used[j] {
					heights = append(heights, e.fh)
					pos = append(pos, j)
				}
			}
			pick := findStack(heights, H, bp.maxStack)
			if pick == nil {
				break
			}
			var blk block
			blk.fw, blk.fd = key[0], key[1]
			dz := 0.0
			for _, p := range pick {
				e := es[pos[p]]
				used[pos[p]] = true
				blk.idxs = append(blk.idxs, e.idx)
				blk.subs = append(blk.subs, sub{id: its[e.idx].id, dz: dz, fh: e.fh})
				dz += e.fh
			}
			blocks = append(blocks, blk)
		}
	}

	sort.SliceStable(blocks, func(a, b int) bool { return blocks[a].fw*blocks[a].fd > blocks[b].fw*blocks[b].fd })
	return blocks
}

// placeOnFloor tiles candidate blocks into the given layer floor (2-D MaxRects) at
// height baseZ, committing each placed block's sub-items and consuming them. A
// block referencing an already-consumed item is skipped. Called once for the
// exact-height blocks and again for the fallback blocks, so both share the floor's
// remaining free space. Returns items placed.
func (bp *BlockPacker) placeOnFloor(floor *d2.Bin2D, result *pack.Result, binID string, blocks []block, consumed []bool, baseZ float64, nextID *int) int {
	placed := 0
	for _, blk := range blocks {
		if anyConsumed(blk.idxs, consumed) {
			continue
		}
		*nextID++
		// The 2-D floor item id is throwaway (the real placement uses each sub's
		// own id), so a plain Itoa avoids fmt.Sprintf's per-block formatting cost.
		p, err := floor.TryPlace(d2.NewItem(strconv.Itoa(*nextID), blk.fw, blk.fd, true))
		if err != nil {
			continue // footprint doesn't fit the remaining floor — leave for a later layer
		}
		pl, ok := p.(*d2.Placement2D)
		if !ok {
			continue
		}
		for _, s := range blk.subs {
			pl3 := &Placement3D{
				binID: binID, itemID: s.id,
				X: pl.X, Y: pl.Y, Z: baseZ + s.dz,
				W: pl.W, D: pl.H, H: s.fh, // pl.W/H are the (possibly rotated) footprint extents
			}
			result.Placements = append(result.Placements, pl3)
			if bp.observer != nil {
				bp.observer(pl3)
			}
			placed++
		}
		for _, idx := range blk.idxs {
			consumed[idx] = true
		}
	}
	return placed
}

// buildFallbackBlocks is the last-resort tier: from the items still available
// after the exact-height pass, it builds blocks no taller than H — the tallest
// that fit — so leftover floor cells can be filled. Per footprint it greedily
// fuses the tallest items whose heights sum to ≤ H (a short stack), then orders
// the blocks tallest-first so the void left under the layer line is minimised.
func (bp *BlockPacker) buildFallbackBlocks(its []*pitem, live []int, consumed []bool, H float64) []block {
	type ent struct {
		idx int
		fh  float64
	}
	groups := map[[2]float64][]ent{}
	seen := map[[2]float64]bool{} // reused per item (cleared) — footprint-key dedup
	for _, i := range live {
		it := its[i]
		if consumed[i] {
			continue
		}
		clear(seen)
		for _, o := range it.orient {
			if o.fh > H+blockEps {
				continue // can't fit this layer's height
			}
			key := [2]float64{math.Min(o.fw, o.fd), math.Max(o.fw, o.fd)}
			if seen[key] {
				continue
			}
			seen[key] = true
			groups[key] = append(groups[key], ent{i, o.fh})
		}
	}
	var blocks []block
	for _, key := range sortedKeys(groups) { // sorted for deterministic block order
		es := groups[key]
		sort.Slice(es, func(a, b int) bool { return es[a].fh > es[b].fh }) // tallest-first
		used := make([]bool, len(es))
		for {
			var blk block
			blk.fw, blk.fd = key[0], key[1]
			dz := 0.0
			for j := range es { // greedily stack tallest-first up to H
				if used[j] || len(blk.subs) >= bp.maxStack || dz+es[j].fh > H+blockEps {
					continue
				}
				used[j] = true
				blk.idxs = append(blk.idxs, es[j].idx)
				blk.subs = append(blk.subs, sub{id: its[es[j].idx].id, dz: dz, fh: es[j].fh})
				dz += es[j].fh
			}
			if len(blk.subs) == 0 {
				break
			}
			blocks = append(blocks, blk)
		}
	}
	sort.SliceStable(blocks, func(a, b int) bool {
		if ha, hb := blockHeight(blocks[a]), blockHeight(blocks[b]); math.Abs(ha-hb) > blockEps {
			return ha > hb // tallest block first — least wasted height under the line
		}
		return blocks[a].fw*blocks[a].fd > blocks[b].fw*blocks[b].fd
	})
	return blocks
}

// sortedKeys returns a footprint-group map's keys in ascending order, so blocks
// are built in a deterministic order regardless of Go's randomized map iteration.
func sortedKeys[V any](m map[[2]float64]V) [][2]float64 {
	keys := make([][2]float64, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(a, b int) bool {
		if keys[a][0] != keys[b][0] {
			return keys[a][0] < keys[b][0]
		}
		return keys[a][1] < keys[b][1]
	})
	return keys
}

// blockHeight is the total stacked height of a block.
func blockHeight(b block) float64 {
	h := 0.0
	for _, s := range b.subs {
		h += s.fh
	}
	return h
}

// findStack returns indices into heights of a subset summing to target (within
// eps), using at most maxStack items, or nil. heights must be sorted descending so
// the sum>target cutoff prunes; a node budget bounds the worst case.
//
// The current subset is carried in a single reused buffer with push/pop
// backtracking rather than a fresh append-copy per recursive call — the
// len(pick) >= maxStack guard runs before every push and pick is pre-sized to
// maxStack, so it never reallocates. This keeps the search allocation-free
// (the only allocation is cloning a hit into found), which matters because the
// block packer runs this per footprint-group per layer: on a 10k-item solve the
// old per-call append dominated allocation (≈48M of 50M) and the GC churn that
// followed. Search order, node budget and result are unchanged.
func findStack(heights []float64, target float64, maxStack int) []int {
	budget := 20000
	var found []int
	pick := make([]int, 0, maxStack)
	var dfs func(start int, sum float64) bool
	dfs = func(start int, sum float64) bool {
		if budget <= 0 {
			return false
		}
		budget--
		if math.Abs(sum-target) <= blockEps {
			found = append([]int(nil), pick...)
			return true
		}
		if sum > target+blockEps || len(pick) >= maxStack {
			return false
		}
		for i := start; i < len(heights); i++ {
			pick = append(pick, i)
			if dfs(i+1, sum+heights[i]) {
				return true
			}
			pick = pick[:len(pick)-1]
		}
		return false
	}
	dfs(0, 0)
	return found
}

func remaining(its []*pitem, consumed []bool) bool {
	for i := range its {
		if !consumed[i] {
			return true
		}
	}
	return false
}

func anyConsumed(idxs []int, consumed []bool) bool {
	for _, i := range idxs {
		if consumed[i] {
			return true
		}
	}
	return false
}

var _ pack.Observable = (*BlockPacker)(nil)
