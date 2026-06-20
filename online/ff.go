package online

import (
	"errors"

	"github.com/W-Floyd/go-pack-bins/pack"
)

// ffSelector implements the First Fit bin selection policy.
// Scans all open bins in order of opening and uses the first one that fits.
type ffSelector struct{}

func (ffSelector) Select(bins []pack.Bin, item pack.Item) (pack.Placement, int, error) {
	for i, b := range bins {
		if b.Remaining() < item.Volume() {
			continue // fast reject
		}
		p, err := b.TryPlace(item)
		if err == nil {
			return p, i, nil
		}
		if !errors.Is(err, pack.ErrNoRoom) {
			return nil, -1, err // propagate permanent error
		}
		// ErrNoRoom: continue to next bin
	}
	return nil, -1, nil
}

// FirstFit returns a First Fit online packer.
// FF(L) ≤ ⌊1.7·OPT(L)⌋ and runs in O(|L|·log|L|) time.
func FirstFit(factory pack.BinFactory) *Packer {
	return NewPacker("FF", ffSelector{}, factory)
}
