package online

import (
	"sort"

	"github.com/wfloyd/go-pack-bins/pack"
)

// awfSelector implements the Almost Worst Fit bin selection policy.
// Places the item in the second-most-empty bin that can accept it;
// falls back to the most-empty bin if there is only one option.
// R∞_AWF ≤ 17/10.
type awfSelector struct{}

func (awfSelector) Select(bins []pack.Bin, item pack.Item) (pack.Placement, int) {
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
	if len(candidates) == 0 {
		return nil, -1
	}
	// Sort ascending by utilisation (lowest utilisation = most empty).
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].util < candidates[j].util
	})
	// Try the second-most-empty first, then the rest in order.
	order := make([]cand, 0, len(candidates))
	if len(candidates) >= 2 {
		order = append(order, candidates[1]) // second-most-empty
		order = append(order, candidates[0]) // most-empty fallback
		order = append(order, candidates[2:]...)
	} else {
		order = candidates
	}
	for _, c := range order {
		if p, ok := bins[c.idx].TryPlace(item); ok {
			return p, c.idx
		}
	}
	return nil, -1
}

// AlmostWorstFit returns an Almost Worst Fit online packer.
func AlmostWorstFit(factory pack.BinFactory) *Packer {
	return NewPacker("AWF", awfSelector{}, factory)
}
