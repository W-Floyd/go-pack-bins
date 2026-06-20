package online

import (
	"errors"
	"sort"

	"github.com/W-Floyd/go-pack-bins/pack"
)

// bfSelector implements the Best Fit bin selection policy.
// Places the item in the bin with the highest current utilisation that can
// still accept it (i.e. the bin that will be "tightest" after placement).
type bfSelector struct{}

func (bfSelector) Select(bins []pack.Bin, item pack.Item) (pack.Placement, int, error) {
	type cand struct {
		idx  int
		util float64
	}
	var candidates []cand
	vol := item.Volume()
	for i, b := range bins {
		if b.Remaining() >= vol {
			candidates = append(candidates, cand{i, b.Utilization()})
		}
	}
	// Try highest-utilisation bins first (best fit = tightest).
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].util > candidates[j].util
	})
	for _, c := range candidates {
		p, err := bins[c.idx].TryPlace(item)
		if err == nil {
			return p, c.idx, nil
		}
		if !errors.Is(err, pack.ErrNoRoom) {
			return nil, -1, err // propagate permanent error
		}
		// ErrNoRoom: continue to next bin
	}
	return nil, -1, nil
}

// BestFit returns a Best Fit online packer.
// BF(L) ≤ ⌊1.7·OPT(L)⌋, same asymptotic bound as First Fit.
func BestFit(factory pack.BinFactory) *Packer {
	return NewPacker("BF", bfSelector{}, factory)
}
