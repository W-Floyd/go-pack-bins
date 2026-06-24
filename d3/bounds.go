package d3

import "math"

const boundEps = 1e-9

// LowerBound returns a combinatorial lower bound on the number of binW×binD×binH
// bins required to pack items. It is the maximum of two valid bounds:
//
//   - the volume bound ceil(Σ volume / (binW·binD·binH)); and
//   - the big-item bound — the count of items that occupy more than half of all
//     three bin dimensions in every orientation they may take. Two such items
//     cannot share a bin (each spans >½ of every axis, so they must overlap on at
//     least one axis-aligned interval per axis), so each needs its own bin.
//
// Items that cannot fit the bin in any orientation are excluded. The result is
// always <= the true optimum, so a packing using exactly this many bins is
// provably optimal and (bins used − LowerBound) is a real optimality gap.
func LowerBound(items []*Item3D, binW, binD, binH float64) int {
	binVol := binW * binD * binH
	vol := 0.0
	big := 0
	for _, it := range items {
		if !anyOrientationFits(it.Orientations(), binW, binD, binH) {
			continue
		}
		vol += it.W * it.D * it.H
		if itemBig3D(it, binW, binD, binH) {
			big++
		}
	}
	lb := 0
	if binVol > 0 && vol > 0 {
		lb = int(math.Ceil(vol/binVol - boundEps))
	}
	if big > lb {
		lb = big
	}
	return lb
}

// itemBig3D reports whether the item occupies more than half of all three bin
// dimensions in every orientation it may take, so no two such items can coexist.
func itemBig3D(it *Item3D, binW, binD, binH float64) bool {
	big := func(o [3]float64) bool {
		return o[0] > binW/2+boundEps && o[1] > binD/2+boundEps && o[2] > binH/2+boundEps
	}
	for _, o := range it.Orientations() {
		if !big(o) {
			return false
		}
	}
	return true
}
