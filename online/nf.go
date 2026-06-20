package online

import (
	"errors"

	"github.com/wfloyd/go-pack-bins/pack"
)

// nfSelector implements the Next Fit bin selection policy.
// It only ever considers the most recently opened bin.
type nfSelector struct{}

func (nfSelector) Select(bins []pack.Bin, item pack.Item) (pack.Placement, int, error) {
	if len(bins) == 0 {
		return nil, -1, nil
	}
	i := len(bins) - 1
	p, err := bins[i].TryPlace(item)
	if err == nil {
		return p, i, nil
	}
	if !errors.Is(err, pack.ErrNoRoom) {
		return nil, -1, err // propagate permanent error
	}
	return nil, -1, nil
}

// NextFit returns a Next Fit online packer.
// NF(L) ≤ 2·OPT(L)−1 and runs in O(|L|) time.
// It is a bounded-space algorithm: only one bin is ever open at a time.
func NextFit(factory pack.BinFactory) *Packer {
	return NewPacker("NF", nfSelector{}, factory)
}
