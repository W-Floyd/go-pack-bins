package online

import (
	"errors"
	"math"

	"github.com/W-Floyd/go-pack-bins/pack"
)

// ssSelector implements the Sum-of-Squares online heuristic (Csirik, Johnson,
// Kenyon, Orlin, Shor & Weber, 2006). It keeps the profile of partially-filled
// bins balanced: for each item it chooses the placement — an existing bin or a
// new one — that minimises Σ_h N(h)², where N(h) is the number of partial bins
// currently at level h. Minimising the sum of squares avoids piling many bins at
// the same level, which is what lets later items fill them efficiently; for item
// sizes drawn i.i.d. from many distributions SS uses OPT + o(OPT) bins.
//
// SS is classically defined for integer item sizes and bin capacity. Here it
// operates over the realised multiset of bin levels, so it degrades gracefully
// to a fit-style selector when sizes don't collide on common levels.
type ssSelector struct{ capacity float64 }

func (s ssSelector) Select(bins []pack.Bin, item pack.Item) (pack.Placement, int, error) {
	const eps = 1e-9
	vol := item.Volume()

	// Histogram of current partial-bin levels (excluding empty and full bins).
	hist := map[float64]int{}
	level := func(b pack.Bin) float64 { return s.capacity - b.Remaining() }
	for _, b := range bins {
		if L := level(b); L > eps && L < s.capacity-eps {
			hist[L]++
		}
	}
	// Change in Σ N(h)² from moving one bin from level from→from+vol. A from of 0
	// models opening a fresh bin (the empty level is not counted).
	delta := func(from float64) float64 {
		to := from + vol
		d := 0.0
		if from > eps { // leaving a counted partial level
			c := hist[from]
			d += float64((c-1)*(c-1) - c*c)
		}
		if to < s.capacity-eps { // arriving at a counted partial level
			c := hist[to]
			d += float64((c+1)*(c+1) - c*c)
		}
		return d
	}

	// Best existing bin that fits.
	bestIdx, bestDelta := -1, math.MaxFloat64
	for i, b := range bins {
		if b.Remaining()+eps < vol {
			continue
		}
		if d := delta(level(b)); d < bestDelta {
			bestIdx, bestDelta = i, d
		}
	}
	// Open a new bin only if it is strictly better than every existing option
	// (prefer reusing bins on ties to avoid waste).
	if bestIdx < 0 || delta(0) < bestDelta-eps {
		return nil, -1, nil
	}
	p, err := bins[bestIdx].TryPlace(item)
	if err == nil {
		return p, bestIdx, nil
	}
	if !errors.Is(err, pack.ErrNoRoom) {
		return nil, -1, err
	}
	return nil, -1, nil
}

// SumOfSquares returns a Sum-of-Squares online packer for bins of the given
// capacity.
func SumOfSquares(binCapacity float64, factory pack.BinFactory) *Packer {
	return NewPacker("SS", ssSelector{capacity: binCapacity}, factory)
}
