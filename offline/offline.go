// Package offline provides offline bin-packing algorithms.
// Offline algorithms sort or preprocess all items before packing begins.
// Most are implemented as sort-then-delegate wrappers around online algorithms.
package offline

import (
	"context"
	"sort"

	"github.com/W-Floyd/go-pack-bins/pack"
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

// DecreasingHeight sorts items from tallest to shortest. Combined with a shelf
// placement strategy (d2.Shelf) it realises the classic NFDH / FFDH / BFDH
// algorithms. Items that expose PackHeight() are ordered by it; others fall back
// to Volume.
var DecreasingHeight SortPolicy = func(items []pack.Item) {
	sort.SliceStable(items, func(i, j int) bool {
		return itemHeight(items[i]) > itemHeight(items[j])
	})
}

func itemHeight(it pack.Item) float64 {
	if h, ok := it.(interface{ PackHeight() float64 }); ok {
		return h.PackHeight()
	}
	return it.Volume()
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
	return w.PackAllCtx(context.Background(), items)
}

// PackAllCtx is PackAll with cancellation: it checks ctx before each item and
// returns ctx.Err() (with the partial result) if cancelled mid-solve.
func (w *Wrapper) PackAllCtx(ctx context.Context, items []pack.Item) (pack.Result, error) {
	// Make a working copy so we don't mutate the caller's slice.
	sorted := make([]pack.Item, len(items))
	copy(sorted, items)
	w.policy(sorted)

	w.online.Reset()
	var lastErr error
	for _, item := range sorted {
		if err := ctx.Err(); err != nil {
			return w.online.Result(), err
		}
		if _, err := w.online.Pack(item); err != nil {
			lastErr = err
		}
	}
	return w.online.Result(), lastErr
}

func (w *Wrapper) Name() string { return w.name }

// Observe forwards to the wrapped online packer so a sort-then-online offline
// algorithm (FFD/BFD/NFD/WFD) streams each placement as it is committed. No-op
// if the underlying packer is not observable.
func (w *Wrapper) Observe(fn pack.PlaceObserver) {
	if o, ok := w.online.(pack.Observable); ok {
		o.Observe(fn)
	}
}

var _ pack.OfflinePacker = (*Wrapper)(nil)
var _ pack.CtxOfflinePacker = (*Wrapper)(nil)
var _ pack.Observable = (*Wrapper)(nil)
