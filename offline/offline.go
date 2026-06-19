// Package offline provides offline bin-packing algorithms.
// Offline algorithms sort or preprocess all items before packing begins.
// Most are implemented as sort-then-delegate wrappers around online algorithms.
package offline

import (
	"sort"

	"github.com/wfloyd/go-pack-bins/pack"
)

// SortPolicy sorts a slice of items in-place before packing.
type SortPolicy func(items []pack.Item)

// DecreasingVolume sorts items from largest to smallest (used by FFD, BFD, MFFD).
var DecreasingVolume SortPolicy = func(items []pack.Item) {
	sort.Slice(items, func(i, j int) bool {
		return items[i].Volume() > items[j].Volume()
	})
}

// IncreasingVolume sorts items from smallest to largest (used by NFD equivalents).
var IncreasingVolume SortPolicy = func(items []pack.Item) {
	sort.Slice(items, func(i, j int) bool {
		return items[i].Volume() < items[j].Volume()
	})
}

// Wrapper wraps an online packer with a sort policy to create an offline packer.
type Wrapper struct {
	name   string
	policy SortPolicy
	online pack.OnlinePacker
}

// New creates an offline packer that applies policy then delegates to the given
// online packer.
func New(name string, policy SortPolicy, online pack.OnlinePacker) *Wrapper {
	return &Wrapper{name: name, policy: policy, online: online}
}

func (w *Wrapper) PackAll(items []pack.Item) (pack.Result, error) {
	// Make a working copy so we don't mutate the caller's slice.
	sorted := make([]pack.Item, len(items))
	copy(sorted, items)
	w.policy(sorted)

	w.online.Reset()
	var lastErr error
	for _, item := range sorted {
		if _, err := w.online.Pack(item); err != nil {
			lastErr = err
		}
	}
	return w.online.Result(), lastErr
}

func (w *Wrapper) Name() string { return w.name }

var _ pack.OfflinePacker = (*Wrapper)(nil)
