package offline

import (
	"math/rand"
	"sort"

	"github.com/W-Floyd/go-pack-bins/online"
	"github.com/W-Floyd/go-pack-bins/pack"
)

// This file implements *incremental* ruin-and-recreate, the engine behind
// RuinRecreate and AdaptiveRuinRecreate. The earlier implementation evaluated
// every candidate ordering by re-decoding the whole instance from scratch
// (online.FirstFit over all n items) — so a ruin that perturbs a handful of items
// still paid the full O(n · placement) decode cost, and with the expensive
// maximal-space (EMS) packer that capped a 1 s budget at a few dozen iterations.
//
// The goal-driven ruin-and-recreate scheme of Gardeyn & Wauters (2022) keeps the
// surviving partial solution intact and only re-inserts the ruined items. This
// engine does the same: each step removes a random subset of placed items,
// rebuilds *only the bins those items touched* (replaying their kept items), and
// re-inserts the removed items into the rebuilt and freshly opened bins —
// untouched bins are carried by reference, never re-decoded. Work per iteration is
// proportional to the disturbed region, not the whole instance, so the same time
// budget buys orders of magnitude more iterations.
//
// It uses only the pack.Bin interface (TryPlace / Items), so it is dimension- and
// strategy-agnostic: the same code drives 1-D, 2-D and 3-D packers unchanged.

// liveBin pairs a bin with the placements committed into it, so a search can carry
// a fully-formed packing (bins + placements) across iterations without having to
// re-derive the placement list from the bin's geometry.
type liveBin struct {
	bin        pack.Bin
	placements []pack.Placement
}

// partial is an in-progress packing: a set of live bins plus the IDs of items that
// can never be placed (permanent failures), carried unchanged across iterations.
type partial struct {
	bins     []liveBin
	tooLarge []string
}

// score ranks a partial with the same (unplaced, bins, fill) order as scoreResult.
func (s partial) score() resultScore {
	fill := 0.0
	for _, lb := range s.bins {
		fill += lb.bin.Utilization()
	}
	return resultScore{unplaced: len(s.tooLarge), bins: len(s.bins), fill: fill}
}

// result materialises the partial into a pack.Result for return.
func (s partial) result() pack.Result {
	r := pack.Result{}
	if len(s.tooLarge) > 0 {
		r.Unplaced = append([]string(nil), s.tooLarge...)
	}
	for _, lb := range s.bins {
		r.Bins = append(r.Bins, lb.bin)
		r.Placements = append(r.Placements, lb.placements...)
	}
	return r
}

// placedOrder returns the placed items in bin order — an item ordering that, when
// re-decoded with First-Fit, tends to reproduce a similar packing. Used to re-run
// the winning ordering through a stronger final decoder (see DecodeFactory).
func (s partial) placedOrder() []pack.Item {
	var order []pack.Item
	for _, lb := range s.bins {
		order = append(order, lb.bin.Items()...)
	}
	return order
}

// buildPartial packs order through factory with First-Fit and captures the
// resulting bins and their placements as a partial.
func buildPartial(factory pack.BinFactory, order []pack.Item) partial {
	return buildPartialLimited(factory, order, nil)
}

// buildPartialLimited is buildPartial with an optional stop predicate, checked
// periodically: when it fires, the remaining (un-attempted) items are recorded as
// unplaced and construction returns early. This lets a wall-clock/ctx budget bound
// even the initial decode — so a huge instance whose construction alone exceeds the
// limit yields a partial best-so-far instead of overrunning. stop nil never stops.
func buildPartialLimited(factory pack.BinFactory, order []pack.Item, stop func() bool) partial {
	p := online.FirstFit(factory)
	stoppedAt := -1
	for i, it := range order {
		if stop != nil && i%64 == 0 && stop() {
			stoppedAt = i
			break
		}
		p.Pack(it)
	}
	res := p.Result()
	byBin := make(map[string][]pack.Placement, len(res.Bins))
	for _, pl := range res.Placements {
		byBin[pl.BinID()] = append(byBin[pl.BinID()], pl)
	}
	out := partial{}
	if len(res.Unplaced) > 0 {
		out.tooLarge = append([]string(nil), res.Unplaced...)
	}
	for _, b := range res.Bins {
		out.bins = append(out.bins, liveBin{bin: b, placements: byBin[b.ID()]})
	}
	// Items never attempted (construction was cut short) are unplaced for now.
	if stoppedAt >= 0 {
		for _, it := range order[stoppedAt:] {
			out.tooLarge = append(out.tooLarge, it.ID())
		}
	}
	return out
}

// ruinRecreateStep removes k random placed items from cur, rebuilds only the bins
// they touched (replaying each touched bin's kept items into a fresh bin), and
// re-inserts the removed items — largest first — into the rebuilt and newly opened
// bins. Untouched bins are reused by reference and never mutated, so cur remains a
// valid solution whether or not the caller keeps the returned candidate.
func ruinRecreateStep(factory pack.BinFactory, cur partial, k int, rng *rand.Rand) partial {
	// Count placed items without materialising them (this runs every iteration, so
	// it must not allocate per-item).
	total := 0
	for _, lb := range cur.bins {
		total += len(lb.bin.Items())
	}
	if total == 0 {
		return cur
	}
	if k > total {
		k = total
	}
	if k < 1 {
		k = 1
	}

	// Pick k distinct placed-item positions (a global index over all bins' items),
	// then resolve them to (bin, item) in a single pass — noting which bins are
	// touched and collecting the removed items.
	removeIdx := make(map[int]bool, k)
	for len(removeIdx) < k {
		removeIdx[rng.Intn(total)] = true
	}
	removedByBin := make(map[int]map[string]bool, k)
	removedItems := make([]pack.Item, 0, k)
	pos := 0
	for bi, lb := range cur.bins {
		for _, it := range lb.bin.Items() {
			if removeIdx[pos] {
				if removedByBin[bi] == nil {
					removedByBin[bi] = make(map[string]bool)
				}
				removedByBin[bi][it.ID()] = true
				removedItems = append(removedItems, it)
			}
			pos++
		}
	}

	frozen := make([]liveBin, 0, len(cur.bins))        // untouched, reused as-is
	mutable := make([]liveBin, 0, len(removedByBin)+1) // rebuilt + newly opened
	for bi, lb := range cur.bins {
		rem, touched := removedByBin[bi]
		if !touched {
			frozen = append(frozen, lb)
			continue
		}
		// Rebuild the bin without its removed items. A kept item always fits a
		// strictly emptier version of the same bin, so failures are ignored.
		fresh := factory.Open()
		var pls []pack.Placement
		for _, it := range lb.bin.Items() {
			if rem[it.ID()] {
				continue
			}
			if pl, err := fresh.TryPlace(it); err == nil {
				pls = append(pls, pl)
			}
		}
		if len(pls) > 0 {
			mutable = append(mutable, liveBin{bin: fresh, placements: pls})
		}
		// A bin emptied entirely is simply dropped — that is how bins are eliminated.
	}

	// Re-insert removed items largest-first via First-Fit over the mutable bins
	// only (rebuilt + newly opened); untouched bins stay frozen.
	sort.SliceStable(removedItems, func(i, j int) bool {
		return removedItems[i].Volume() > removedItems[j].Volume()
	})
	tooLarge := cur.tooLarge
	for _, it := range removedItems {
		placedOK := false
		for mi := range mutable {
			if pl, err := mutable[mi].bin.TryPlace(it); err == nil {
				mutable[mi].placements = append(mutable[mi].placements, pl)
				placedOK = true
				break
			}
		}
		if placedOK {
			continue
		}
		nb := factory.Open()
		if pl, err := nb.TryPlace(it); err == nil {
			mutable = append(mutable, liveBin{bin: nb, placements: []pack.Placement{pl}})
		} else {
			tooLarge = appendUnique(tooLarge, it.ID())
		}
	}

	return partial{bins: append(frozen, mutable...), tooLarge: tooLarge}
}

func appendUnique(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(append([]string(nil), s...), v)
}

// finalDecode re-runs the best placed ordering through the strong factory once, so
// a search driven by a cheap surrogate decoder (DecodeFactory) still returns a
// placement from the strong packer. orig supplies any items the search dropped as
// unplaceable, so the re-decode sees the full instance.
func finalDecode(strong pack.BinFactory, best partial, orig []pack.Item) pack.Result {
	order := best.placedOrder()
	seen := make(map[string]bool, len(order))
	for _, it := range order {
		seen[it.ID()] = true
	}
	for _, it := range orig {
		if !seen[it.ID()] {
			order = append(order, it)
		}
	}
	return buildPartial(strong, order).result()
}
