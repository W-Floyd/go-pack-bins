package offline

import (
	"math"

	"github.com/W-Floyd/go-pack-bins/pack"
)

// boundEps guards ceil against floating-point noise: a total that is an exact
// multiple of capacity must not round up to one bin too many.
const boundEps = 1e-9

// LowerBoundVolume returns the continuous (length/area/volume) lower bound on the
// number of bins of the given capacity needed to pack items:
// ceil(sum(item.Volume) / capacity). Because Item.Volume is length in 1-D, area
// in 2-D and volume in 3-D, this single helper is dimension-agnostic.
//
// It is always <= the true optimum — a bin of capacity C holds at most C worth of
// volume, so packing total volume V needs at least ceil(V/C) bins. A heuristic
// whose bin count equals this bound is therefore provably optimal. Items with
// non-positive volume and a non-positive capacity contribute nothing.
//
// This is the classic L1 / area bound (Martello & Toth); it is cheap and valid
// but can be loose in 2-D/3-D, where geometry may forbid achieving it. For the
// geometry-aware refinements see d2.LowerBound and d3.LowerBound.
func LowerBoundVolume(items []pack.Item, capacity float64) int {
	if capacity <= 0 {
		return 0
	}
	total := 0.0
	for _, it := range items {
		if v := it.Volume(); v > 0 {
			total += v
		}
	}
	if total <= 0 {
		return 0
	}
	return int(math.Ceil(total/capacity - boundEps))
}
