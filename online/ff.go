package online

import "github.com/wfloyd/go-pack-bins/pack"

// ffSelector implements the First Fit bin selection policy.
// Scans all open bins in order of opening and uses the first one that fits.
type ffSelector struct{}

func (ffSelector) Select(bins []pack.Bin, item pack.Item) (pack.Placement, int) {
	for i, b := range bins {
		if b.Remaining() < item.Volume() {
			continue // fast reject
		}
		if p, ok := b.TryPlace(item); ok {
			return p, i
		}
	}
	return nil, -1
}

// FirstFit returns a First Fit online packer.
// FF(L) ≤ ⌊1.7·OPT(L)⌋ and runs in O(|L|·log|L|) time.
func FirstFit(factory pack.BinFactory) *Packer {
	return NewPacker("FF", ffSelector{}, factory)
}
