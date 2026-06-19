package offline

import (
	"sort"

	"github.com/wfloyd/go-pack-bins/online"
	"github.com/wfloyd/go-pack-bins/pack"
)

// ModifiedFirstFitDecreasing returns an MFFD offline packer.
// MFFD classifies items into four size categories relative to bin capacity:
//
//	Large:  size > ½ bin
//	Medium: size > ⅓ bin
//	Small:  size > ⅙ bin
//	Tiny:   size ≤ ⅙ bin
//
// Items within each class are sorted by decreasing size.  The classes are then
// packed together in the order Large, Medium, Small, Tiny using First Fit.
//
// MFFD(I) ≤ (71/60)·OPT(I) + 1, improving on FFD for large items.
func ModifiedFirstFitDecreasing(binCapacity float64, factory pack.BinFactory) *mffdPacker {
	return &mffdPacker{binCapacity: binCapacity, factory: factory}
}

type mffdPacker struct {
	binCapacity float64
	factory     pack.BinFactory
}

func (m *mffdPacker) Name() string { return "MFFD" }

func (m *mffdPacker) PackAll(items []pack.Item) (pack.Result, error) {
	large := filterClass(items, m.binCapacity, 0.5, 1.0)
	medium := filterClass(items, m.binCapacity, 1.0/3, 0.5)
	small := filterClass(items, m.binCapacity, 1.0/6, 1.0/3)
	tiny := filterClass(items, m.binCapacity, 0, 1.0/6)

	sortDesc(large)
	sortDesc(medium)
	sortDesc(small)
	sortDesc(tiny)

	ordered := make([]pack.Item, 0, len(items))
	ordered = append(ordered, large...)
	ordered = append(ordered, medium...)
	ordered = append(ordered, small...)
	ordered = append(ordered, tiny...)

	ff := online.FirstFit(m.factory)
	var lastErr error
	for _, item := range ordered {
		if _, err := ff.Pack(item); err != nil {
			lastErr = err
		}
	}
	return ff.Result(), lastErr
}

func filterClass(items []pack.Item, cap, lo, hi float64) []pack.Item {
	var result []pack.Item
	for _, item := range items {
		frac := item.Volume() / cap
		if frac > lo && frac <= hi {
			result = append(result, item)
		}
	}
	return result
}

func sortDesc(items []pack.Item) {
	sort.Slice(items, func(i, j int) bool {
		return items[i].Volume() > items[j].Volume()
	})
}

var _ pack.OfflinePacker = (*mffdPacker)(nil)
