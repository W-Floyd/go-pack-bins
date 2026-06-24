package d2

import "math"

const boundEps = 1e-9

// LowerBound returns a combinatorial lower bound on the number of binW×binH bins
// required to pack items. It is the maximum of two valid bounds:
//
//   - the area bound ceil(Σ area / (binW·binH)) — a bin holds at most binW·binH
//     of area, so this many bins are always needed; and
//   - the big-item bound — the count of items that occupy more than half of both
//     bin dimensions in every orientation they may take. Two such items cannot
//     share a bin (each spans >½ of both axes, so they must overlap), so each
//     needs its own bin.
//
// Items that cannot fit the bin in any orientation are excluded (they are
// unplaceable, not a reason to open a bin). The result is always <= the true
// optimum, so a packing that uses exactly this many bins is provably optimal and
// the gap (bins used − LowerBound) is a real optimality gap to report.
//
// Sources: the area bound is the classic Martello & Toth L1; the big-item /
// "large rectangles" bound is standard in 2-D bin packing lower-bound work.
func LowerBound(items []*Item2D, binW, binH float64) int {
	binArea := binW * binH
	area := 0.0
	big := 0
	for _, it := range items {
		if !fits2D(it, binW, binH) {
			continue
		}
		area += it.W * it.H
		if itemBig2D(it, binW, binH) {
			big++
		}
	}
	lb := 0
	if binArea > 0 && area > 0 {
		lb = int(math.Ceil(area/binArea - boundEps))
	}
	if big > lb {
		lb = big
	}
	return lb
}

// fits2D reports whether the item fits the bin in at least one allowed orientation.
func fits2D(it *Item2D, binW, binH float64) bool {
	if it.W <= binW+boundEps && it.H <= binH+boundEps {
		return true
	}
	return it.AllowRotate && it.H <= binW+boundEps && it.W <= binH+boundEps
}

// itemBig2D reports whether the item occupies more than half of both bin
// dimensions in every orientation it may take, so no two such items can coexist
// in one bin.
func itemBig2D(it *Item2D, binW, binH float64) bool {
	big := func(w, h float64) bool { return w > binW/2+boundEps && h > binH/2+boundEps }
	if !big(it.W, it.H) {
		return false
	}
	if it.AllowRotate && !big(it.H, it.W) {
		return false
	}
	return true
}
