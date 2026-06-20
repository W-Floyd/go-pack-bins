package offline

import (
	"errors"

	"github.com/W-Floyd/go-pack-bins/meta"
	"github.com/W-Floyd/go-pack-bins/online"
	"github.com/W-Floyd/go-pack-bins/pack"
)

// BalancedFit packs in two phases so that balancing preferences level the load
// without wasting bins:
//
//  1. Learn the minimum bin count K from a tight First-Fit-Decreasing pack
//     (under the same factory/constraints).
//  2. Pre-open K bins and distribute items (largest first) using the given
//     preferences (e.g. pack.BalanceCount).
//
// The online PreferenceFit selector opens bins lazily — it fills bin 0 before
// bin 1 exists, so a balancing preference can only even out the bins already
// open and the tail can spill into an extra bin. Pre-opening K bins lets the
// preference spread items across all of them from the start, keeping the bin
// count at the achievable minimum while still balancing within it.
type BalancedFit struct {
	factory pack.BinFactory
	prefs   []pack.Preference
}

// NewBalancedFit returns a BalancedFit using factory and the given preferences.
// The factory should produce *pack.ConstrainedBin (wrap with NewConstrainedFactory)
// so preferences can read bin aggregates.
func NewBalancedFit(factory pack.BinFactory, prefs ...pack.Preference) *BalancedFit {
	return &BalancedFit{factory: factory, prefs: prefs}
}

func (b *BalancedFit) Name() string { return "BalancedFit" }

func (b *BalancedFit) PackAll(items []pack.Item) (pack.Result, error) {
	// Phase 1: learn the minimum achievable bin count under the same constraints,
	// taking the tightest of several decreasing-fit heuristics.
	estimator := meta.BestOf(
		FirstFitDecreasing(b.factory),
		BestFitDecreasing(b.factory),
		WorstFitDecreasing(b.factory),
	)
	probe, err := estimator.PackAll(items)
	if err != nil && !errors.Is(err, pack.ErrItemTooLarge) {
		return pack.Result{}, err
	}
	target := probe.BinsUsed()

	// Phase 2: pre-open K bins, then balance items (largest first) across them.
	sorted := make([]pack.Item, len(items))
	copy(sorted, items)
	DecreasingVolume(sorted)

	packer := online.PreferenceFit(b.factory, b.prefs...)
	packer.Prefill(target)
	var lastErr error
	for _, it := range sorted {
		if _, e := packer.Pack(it); e != nil {
			lastErr = e
		}
	}
	return pruneEmptyBins(packer.Result()), lastErr
}

var _ pack.OfflinePacker = (*BalancedFit)(nil)

// pruneEmptyBins drops bins that received no placement (e.g. pre-opened bins
// that ended up unused) so Result.BinsUsed reflects only occupied bins.
func pruneEmptyBins(r pack.Result) pack.Result {
	used := make(map[string]bool)
	for _, p := range r.Placements {
		if p != nil {
			used[p.BinID()] = true
		}
	}
	kept := r.Bins[:0:0]
	for _, bin := range r.Bins {
		if id, ok := bin.(interface{ ID() string }); ok && used[id.ID()] {
			kept = append(kept, bin)
		}
	}
	r.Bins = kept
	return r
}
