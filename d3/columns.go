package d3

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/W-Floyd/go-pack-bins/d2"
	"github.com/W-Floyd/go-pack-bins/pack"
)

// ColumnPacker is the vertical-first counterpart of the BlockPacker. Where block
// building grows horizontal layers that span the whole bin floor, column building
// grows vertical columns of a fixed XY footprint and tiles those columns across
// the floor:
//
//  1. Pick the largest footprint profile P still present among the items.
//  2. Build a column of footprint P by stacking the items that *share* P — those
//     that present exactly the P footprint in some orientation — tallest-first up
//     to the container height. It does NOT horizontally combine different,
//     smaller footprints to fill out P: items smaller than P are not "composed"
//     into a P column, they go into their own (smaller) columns when the profile
//     resets. Stacking same-footprint items *is* the pseudo-item-matching-P idea.
//  3. Reserve the column's footprint on the bin floor (2-D MaxRects over the
//     floor, axis-aligned) and commit the column there. Repeat: the next column
//     uses the largest profile among the *remaining* items, so columns of a given
//     profile keep forming while that profile has material, then the method drops
//     to the next-largest profile — until the floor can hold no further column.
//  4. Best-effort fill the leftover space (above short columns, gaps between them,
//     and the room left because columns never mix footprints) by reconstructing
//     the bin's free volume with EMS and dropping the remaining items into the
//     voids largest-first (grounded — nothing floats). This is the only stage
//     that places an item somewhere other than a matching-footprint column.
//
// Overflow opens a fresh bin and repeats. Each placement commits through an
// observer as it lands, so a solve streams its progress like the block packer.
// See ATTRIBUTION.md (wall/column building).
type ColumnPacker struct {
	w, d, h  float64
	observer pack.PlaceObserver
	// bp is an internal BlockPacker, used only for its buildBlocks combination
	// logic (footprint grouping + same-footprint vertical stacks) when assembling
	// a fused level that completely tiles a column's footprint.
	bp *BlockPacker
}

// NewColumnPacker creates a column-building packer for a bin of the given dimensions.
func NewColumnPacker(w, d, h float64) *ColumnPacker {
	return &ColumnPacker{w: w, d: d, h: h, bp: NewBlockPacker(w, d, h)}
}

// Observe registers a per-placement callback (pack.Observable), enabling streaming.
func (cp *ColumnPacker) Observe(fn pack.PlaceObserver) { cp.observer = fn }

// Name satisfies pack.OfflinePacker so the packer can join a meta.BestOf race.
func (cp *ColumnPacker) Name() string { return "Columns" }

// PackAll runs the solve with no cancellation.
func (cp *ColumnPacker) PackAll(items []pack.Item) (pack.Result, error) {
	return cp.PackAllCtx(context.Background(), items)
}

// PackAllCtx packs items into footprint-sized columns, opening fresh bins for
// overflow. It samples ctx between bins and inside each column's slice loop.
func (cp *ColumnPacker) PackAllCtx(ctx context.Context, items []pack.Item) (pack.Result, error) {
	var result pack.Result

	// Collect placeable items and their valid orientations (mirrors BlockPacker).
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
			if fh <= cp.h+blockEps && fw <= cp.w+blockEps && fd <= cp.d+blockEps {
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
	for binIdx := 0; remaining(its, consumed); binIdx++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if cp.packBin(ctx, &result, binIdx, its, consumed) == 0 {
			break // no progress in an empty bin (items too large) — stop, report unplaced
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
	}

	for i, it := range its {
		if !consumed[i] {
			result.Unplaced = append(result.Unplaced, it.id)
		}
	}
	return result, nil
}

// packBin tiles one bin's floor with footprint-sized columns, then best-effort
// fills the leftover space. Returns the number of items placed in the bin.
func (cp *ColumnPacker) packBin(ctx context.Context, result *pack.Result, binIdx int, its []*pitem, consumed []bool) int {
	binID := fmt.Sprintf("columns-bin-%d", binIdx)
	floor := d2.NewBin(binID, cp.w, cp.d, d2.NewMaxRectsDefault(cp.w, cp.d))
	placed := 0

	live := make([]int, 0, len(its))
	for i := range its {
		if !consumed[i] {
			live = append(live, i)
		}
	}

	colNum := 0
	for {
		if err := ctx.Err(); err != nil {
			return placed
		}
		live = compactLive(live, consumed)
		if len(live) == 0 {
			break
		}
		pw, pd, ok := largestProfile(its, live, consumed, cp.w, cp.d)
		if !ok {
			break
		}
		// Reserve the column footprint on the floor, axis-aligned (no rotation):
		// largestProfile already orients pw×pd to fit, and keeping the column's
		// axes aligned with the bin lets us translate its slices by a simple offset.
		colNum++
		p, err := floor.TryPlace(d2.NewItem(fmt.Sprintf("col-%d", colNum), pw, pd, false))
		if err != nil {
			break // floor is full — no room for even the largest remaining column
		}
		pl, ok := p.(*d2.Placement2D)
		if !ok {
			break
		}
		n := cp.fillColumn(result, binID, pl.X, pl.Y, pw, pd, its, live, consumed)
		placed += n
		if n == 0 {
			break // safety: a reserved column placed nothing (shouldn't happen)
		}
	}

	placed += cp.voidFill(binID, its, consumed, result)
	if placed > 0 {
		result.Bins = append(result.Bins, NewBin(binID, cp.w, cp.d, cp.h, NewExtremePointStrategy(cp.w, cp.d, cp.h)))
	}
	return placed
}

// fillColumn stacks items that *share* the column's footprint P vertically, from
// the floor up, at the bin position (cx,cy). It does not horizontally combine
// different footprints to fill P — that "composing" is exactly what we avoid:
// items smaller than P form their own (smaller) columns once the profile resets,
// and only the final void-fill mixes footprints. Matching items are placed
// tallest-first while they fit the remaining height (a greedy fill of the
// column), which is the vertical-fusion "combination logic" that builds
// pseudo-items matching P. Returns the number of items placed.
func (cp *ColumnPacker) fillColumn(result *pack.Result, binID string, cx, cy, pw, pd float64, its []*pitem, live []int, consumed []bool) int {
	// Items presenting footprint P, with the height of their P-facing orientation.
	type cand struct {
		idx int
		h   float64
	}
	var cands []cand
	for _, i := range live {
		if consumed[i] {
			continue
		}
		if h, ok := heightInFootprint(its[i], pw, pd); ok {
			cands = append(cands, cand{i, h})
		}
	}
	// Tallest-first (ties by item index) so the stack fills densely and
	// deterministically.
	sort.SliceStable(cands, func(a, b int) bool {
		if math.Abs(cands[a].h-cands[b].h) > blockEps {
			return cands[a].h > cands[b].h
		}
		return cands[a].idx < cands[b].idx
	})

	base := 0.0
	cnt := 0
	for _, c := range cands {
		if base+c.h > cp.h+blockEps {
			continue // too tall for the remaining column height; a shorter one may still fit
		}
		p3 := &Placement3D{binID: binID, itemID: its[c.idx].id, X: cx, Y: cy, Z: base, W: pw, D: pd, H: c.h}
		result.Placements = append(result.Placements, p3)
		if cp.observer != nil {
			cp.observer(p3)
		}
		consumed[c.idx] = true
		base += c.h
		cnt++
	}

	// With a real P-footprint item now anchoring the column, continue upward with
	// fused pseudo-items: smaller items that *together completely tile* P at a
	// uniform level height. A level is committed only if it fully covers P (no
	// partial patchwork), so composing only happens when it cleanly forms the
	// profile. Stop at the first height that cannot be completed.
	if cnt == 0 {
		return 0 // no real item of this size — don't fabricate the profile
	}
	for base < cp.h-blockEps {
		var colLive []int
		for _, i := range live {
			if !consumed[i] && footprintFitsWithin(its[i], pw, pd) {
				colLive = append(colLive, i)
			}
		}
		H := maxHeightWithin(its, colLive, consumed, cp.h-base, pw, pd)
		if H <= 0 {
			break
		}
		placements, used, ok := cp.buildLevel(its, binID, cx, cy, base, pw, pd, H, colLive, consumed)
		if !ok {
			break // can't fully tile P at this height — column is done
		}
		for _, p3 := range placements {
			result.Placements = append(result.Placements, p3)
			if cp.observer != nil {
				cp.observer(p3)
			}
		}
		for _, i := range used {
			consumed[i] = true
		}
		cnt += len(used)
		base += H
	}
	return cnt
}

// buildLevel trial-assembles a solid level of height H that completely tiles the
// pw×pd column footprint, from exact-height blocks of the (footprint-fitting)
// items — without consuming anything. It returns the level's placements (at
// bin coordinates), the item indices it uses, and whether the level fully covers
// P. Incomplete coverage (gaps) returns ok=false so the caller drops the level.
func (cp *ColumnPacker) buildLevel(its []*pitem, binID string, cx, cy, base, pw, pd, H float64, colLive []int, consumed []bool) ([]*Placement3D, []int, bool) {
	blocks := cp.bp.buildBlocks(its, colLive, consumed, H)
	floor := d2.NewBin(binID, pw, pd, d2.NewMaxRectsDefault(pw, pd))
	var placements []*Placement3D
	var used []int
	usedSet := map[int]bool{}
	covered := 0.0
	for _, blk := range blocks {
		skip := false
		for _, idx := range blk.idxs {
			if consumed[idx] || usedSet[idx] {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		p, err := floor.TryPlace(d2.NewItem("lvl", blk.fw, blk.fd, true))
		if err != nil {
			continue // footprint doesn't fit the remaining slice floor
		}
		pl, ok := p.(*d2.Placement2D)
		if !ok {
			continue
		}
		for _, s := range blk.subs {
			placements = append(placements, &Placement3D{
				binID: binID, itemID: s.id,
				X: cx + pl.X, Y: cy + pl.Y, Z: base + s.dz,
				W: pl.W, D: pl.H, H: s.fh,
			})
		}
		for _, idx := range blk.idxs {
			usedSet[idx] = true
			used = append(used, idx)
		}
		covered += pl.W * pl.H
	}
	if covered >= pw*pd-blockEps {
		return placements, used, true
	}
	return nil, nil, false
}

// voidFill drops the still-unconsumed items, largest-first, into the bin's
// remaining free space — the room above short columns and the gaps between them.
// It reconstructs that free volume from the bin's committed boxes with EMS and
// requires resting support, so nothing floats. Returns the number placed.
func (cp *ColumnPacker) voidFill(binID string, its []*pitem, consumed []bool, result *pack.Result) int {
	e := NewEmptyMaximalSpace(cp.w, cp.d, cp.h)
	e.contact = ContactSpec{NoFloating: true}
	for _, raw := range result.Placements {
		p3, ok := raw.(*Placement3D)
		if !ok || p3.binID != binID {
			continue
		}
		e.Occupy(p3.X, p3.Y, p3.Z, p3.W, p3.D, p3.H)
	}

	idx := make([]int, 0)
	for i := range its {
		if !consumed[i] {
			idx = append(idx, i)
		}
	}
	sort.SliceStable(idx, func(a, b int) bool { return orientVolume(its[idx[a]]) > orientVolume(its[idx[b]]) })

	placed := 0
	for _, i := range idx {
		if x, y, z, w, d, h, ok := e.TryInsert(orientationsOf(its[i])); ok {
			p3 := &Placement3D{binID: binID, itemID: its[i].id, X: x, Y: y, Z: z, W: w, D: d, H: h}
			result.Placements = append(result.Placements, p3)
			if cp.observer != nil {
				cp.observer(p3)
			}
			consumed[i] = true
			placed++
		}
	}
	return placed
}

// largestProfile returns the largest-area footprint (oriented to fit the bin
// floor, pw≤binW and pd≤binD) over the orientations of the unconsumed items, or
// ok=false if none remain. It is the footprint of the next column to build.
func largestProfile(its []*pitem, live []int, consumed []bool, binW, binD float64) (pw, pd float64, ok bool) {
	best := -1.0
	for _, i := range live {
		if consumed[i] {
			continue
		}
		for _, o := range its[i].orient {
			a, b := o.fw, o.fd
			fitsAB := a <= binW+blockEps && b <= binD+blockEps
			fitsBA := b <= binW+blockEps && a <= binD+blockEps
			if !fitsAB && !fitsBA {
				continue
			}
			if area := a * b; area > best+blockEps {
				best = area
				if fitsAB {
					pw, pd = a, b
				} else {
					pw, pd = b, a
				}
				ok = true
			}
		}
	}
	return
}

// heightInFootprint returns the item's height when oriented to present exactly
// the pw×pd footprint (in either rotation), and whether such an orientation
// exists. A column of profile P is anchored by items that match P this way.
func heightInFootprint(it *pitem, pw, pd float64) (float64, bool) {
	eq := func(a, b float64) bool { return math.Abs(a-b) <= blockEps }
	for _, o := range it.orient {
		if (eq(o.fw, pw) && eq(o.fd, pd)) || (eq(o.fw, pd) && eq(o.fd, pw)) {
			return o.fh, true
		}
	}
	return 0, false
}

// footprintFitsWithin reports whether some orientation of the item has a
// footprint that fits within the pw×pd column (in either rotation) — a candidate
// for fusing into a P-tiling level.
func footprintFitsWithin(it *pitem, pw, pd float64) bool {
	for _, o := range it.orient {
		if (o.fw <= pw+blockEps && o.fd <= pd+blockEps) || (o.fd <= pw+blockEps && o.fw <= pd+blockEps) {
			return true
		}
	}
	return false
}

// maxHeightWithin is the tallest orientation height ≤ cap among unconsumed items
// whose footprint fits within pw×pd — the candidate height for the next fused
// level.
func maxHeightWithin(its []*pitem, live []int, consumed []bool, cap, pw, pd float64) float64 {
	best := 0.0
	for _, i := range live {
		if consumed[i] {
			continue
		}
		for _, o := range its[i].orient {
			within := (o.fw <= pw+blockEps && o.fd <= pd+blockEps) || (o.fd <= pw+blockEps && o.fw <= pd+blockEps)
			if within && o.fh <= cap+blockEps && o.fh > best+blockEps {
				best = o.fh
			}
		}
	}
	return best
}

var _ pack.Observable = (*ColumnPacker)(nil)
