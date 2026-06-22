package offline

import (
	"context"
	"sort"

	"github.com/W-Floyd/go-pack-bins/pack"
)

// DefaultBeamMaxItems caps the order in which BeamSearch runs an exhaustive
// bounded search; above it, BeamSearch falls back to First-Fit-Decreasing.
const DefaultBeamMaxItems = 200

// BeamOptions configures BeamSearch.
type BeamOptions struct {
	// Width is the beam width — how many partial orderings are kept per level
	// (default 6). Larger is slower but explores more.
	Width int
	// Branch is how many candidate next items (the largest unplaced) each partial
	// ordering is extended by (default 4).
	Branch int
	// MaxItems caps exhaustive search; 0 uses DefaultBeamMaxItems.
	MaxItems int
	// Progress, if set, receives the level completed out of the total (one level
	// per item) as the beam advances.
	Progress pack.ProgressObserver
}

func (o BeamOptions) width() int {
	if o.Width <= 0 {
		return 6
	}
	return o.Width
}

func (o BeamOptions) branch() int {
	if o.Branch <= 0 {
		return 4
	}
	return o.Branch
}

func (o BeamOptions) maxItems() int {
	if o.MaxItems <= 0 {
		return DefaultBeamMaxItems
	}
	return o.MaxItems
}

// BeamSearch packs items by a width-limited beam search over the order in which
// items are offered to a First-Fit packer — the bounded tree search used widely
// in container loading (e.g. Araya et al. 2020; Parreño et al. 2020). It keeps
// the Width best partial orderings at each level, extending each by the Branch
// largest unplaced items, scoring partial packings by fewest bins then tightest
// fill. It is the middle ground between greedy (FFD) and exhaustive BruteForce:
// far stronger than greedy, far cheaper than n!.
//
// Above MaxItems it falls back to FFD. Honours ctx as a deadline.
func BeamSearch(ctx context.Context, items []pack.Item, factory pack.BinFactory, opts BeamOptions) pack.Result {
	n := len(items)
	if n == 0 {
		return pack.Result{}
	}
	if n > opts.maxItems() {
		r, _ := FirstFitDecreasing(factory).PackAllCtx(ctx, items)
		return r
	}

	// Work over items sorted by decreasing volume so "the largest unplaced items"
	// are simply the lowest-indexed unused entries.
	sorted := append([]pack.Item(nil), items...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Volume() > sorted[j].Volume() })

	type state struct {
		order []pack.Item
		used  []bool
		score resultScore
	}
	beam := []state{{order: nil, used: make([]bool, n)}}

	for level := 0; level < n; level++ {
		if ctx.Err() != nil {
			break
		}
		// Enumerate this level's candidate extensions in a fixed order (beam order ×
		// branch order), then score them concurrently — each extension is an
		// independent First-Fit pack. Writing into next[k] by index keeps the order
		// (and so the stable sort below) identical to the sequential version.
		type ext struct{ st, item int }
		var exts []ext
		for si, st := range beam {
			added := 0
			for i := 0; i < n && added < opts.branch(); i++ {
				if st.used[i] {
					continue
				}
				added++
				exts = append(exts, ext{st: si, item: i})
			}
		}
		if len(exts) == 0 {
			break
		}
		next := make([]state, len(exts))
		parallelFor(len(exts), func(k int) {
			st, i := beam[exts[k].st], exts[k].item
			order := make([]pack.Item, len(st.order)+1)
			copy(order, st.order)
			order[len(st.order)] = sorted[i]
			used := make([]bool, n)
			copy(used, st.used)
			used[i] = true
			next[k] = state{order: order, used: used, score: scoreResult(packOrdering(factory, order))}
		})
		// Keep the Width best partial orderings.
		sort.SliceStable(next, func(i, j int) bool { return next[i].score.better(next[j].score) })
		if len(next) > opts.width() {
			next = next[:opts.width()]
		}
		beam = next
		if opts.Progress != nil {
			opts.Progress(level+1, n)
		}
	}

	// The best full ordering in the final beam is the answer; if ctx cut the
	// search short, repack the best partial ordering plus any leftover items in
	// decreasing-volume order so nothing is dropped.
	best := beam[0]
	if len(best.order) < n {
		order := append(append([]pack.Item(nil), best.order...), leftover(sorted, best.used)...)
		return packOrdering(factory, order)
	}
	return packOrdering(factory, best.order)
}

func leftover(sorted []pack.Item, used []bool) []pack.Item {
	var out []pack.Item
	for i, it := range sorted {
		if !used[i] {
			out = append(out, it)
		}
	}
	return out
}
