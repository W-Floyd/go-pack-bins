package online

import (
	"errors"
	"sort"

	"github.com/wfloyd/go-pack-bins/pack"
)

// wfSelector implements the Worst Fit bin selection policy.
// Places the item in the bin with the lowest current utilisation (most space).
type wfSelector struct{}

func (wfSelector) Select(bins []pack.Bin, item pack.Item) (pack.Placement, int, error) {
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
	// Try lowest-utilisation bins first (worst fit = roomiest).
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].util < candidates[j].util
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

// WorstFit returns a Worst Fit online packer.
// WF can behave as badly as Next Fit in the worst case.
func WorstFit(factory pack.BinFactory) *Packer {
	return NewPacker("WF", wfSelector{}, factory)
}
