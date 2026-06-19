package online

import "github.com/wfloyd/go-pack-bins/pack"

// nkfSelector implements the Next-k-Fit bin selection policy.
// It considers only the last k opened bins (a k-bounded-space algorithm).
type nkfSelector struct{ k int }

func (s nkfSelector) Select(bins []pack.Bin, item pack.Item) (pack.Placement, int) {
	start := len(bins) - s.k
	if start < 0 {
		start = 0
	}
	for i := start; i < len(bins); i++ {
		if p, ok := bins[i].TryPlace(item); ok {
			return p, i
		}
	}
	return nil, -1
}

// NextKFit returns a Next-k-Fit online packer.
// For k ≥ 2 it improves on NF in practice; increasing k beyond 2 does not
// improve the worst-case bound. k=1 is equivalent to Next Fit.
func NextKFit(k int, factory pack.BinFactory) *Packer {
	if k < 1 {
		k = 1
	}
	return NewPacker("NkF", nkfSelector{k: k}, factory)
}
