package offline

import (
	"context"
	"sync/atomic"

	"github.com/W-Floyd/go-pack-bins/online"
	"github.com/W-Floyd/go-pack-bins/pack"
)

// DefaultBruteForceMaxItems is the largest order BruteForce will exhaustively
// search; above it, BruteForce falls back to a single FFD pass. 8! = 40320
// orderings is comfortably fast; the factorial growth makes larger orders
// impractical (the same trade-off as skjolber's brute-force packagers).
const DefaultBruteForceMaxItems = 8

// BruteForceOptions configures BruteForce.
type BruteForceOptions struct {
	// MaxItems caps exhaustive search; 0 uses DefaultBruteForceMaxItems.
	MaxItems int
	// Key maps an item to an interchangeability signature: permutations that only
	// reorder items with equal keys are pruned (they pack identically). Defaults
	// to item ID, i.e. no pruning — always correct, just slower. Pass a shape key
	// (e.g. sorted dimensions) so identical boxes collapse and the search shrinks.
	Key func(pack.Item) string
	// Progress, if set, receives first-item subtrees completed out of the total as
	// the search fans out.
	Progress pack.ProgressObserver
}

// BruteForce finds the item ordering that packs best, by exhaustively trying
// every distinct permutation and packing each with First-Fit through factory,
// keeping the result with the fewest unplaced items, then fewest bins. Rotation
// and within-bin position are handled by the factory's bins, so this searches
// the one lever a greedy packer fixes up front: the order items are offered.
//
// It is meant for small orders (see DefaultBruteForceMaxItems); larger ones fall
// back to First-Fit-Decreasing. The search honours ctx (e.g. a deadline): on
// cancellation the best ordering found so far is returned with ctx.Err().
//
// Inspired by the brute-force packagers in skjolber/3d-bin-container-packing
// (Apache-2.0; see ATTRIBUTION.md for the pinned commit), including its pruning of permutations over equal items.
func BruteForce(ctx context.Context, items []pack.Item, factory pack.BinFactory, opts BruteForceOptions) (pack.Result, error) {
	maxItems := opts.MaxItems
	if maxItems <= 0 {
		maxItems = DefaultBruteForceMaxItems
	}
	key := opts.Key
	if key == nil {
		key = func(it pack.Item) string { return it.ID() }
	}

	n := len(items)
	if n == 0 {
		return pack.Result{}, nil
	}
	if n > maxItems {
		// Too large to brute-force; fall back to FFD (still ctx-aware).
		return FirstFitDecreasing(factory).PackAllCtx(ctx, items)
	}

	// pack runs one ordering through a fresh First-Fit packer.
	packOrder := func(order []pack.Item) pack.Result {
		p := online.FirstFit(factory)
		for _, it := range order {
			p.Pack(it) // failures are recorded as unplaced in the result
		}
		return p.Result()
	}

	// searchFrom exhaustively explores all orderings that start with items[first],
	// using its own local state so tasks never share mutable data. It keeps the
	// first strictly-better result in DFS order (so ties resolve to the earliest).
	searchFrom := func(first int) (pack.Result, bool, error) {
		used := make([]bool, n)
		cur := make([]pack.Item, 0, n)
		used[first] = true
		cur = append(cur, items[first])
		var best pack.Result
		haveBest := false
		var search func() error
		search = func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if len(cur) == n {
				r := packOrder(cur)
				if !haveBest || betterBrute(r, best) {
					best = r
					haveBest = true
				}
				return nil
			}
			// Pick the next item; skip any whose key was already tried at this depth
			// (those permutations are identical), so multisets don't blow up to n!.
			triedKeys := make(map[string]bool)
			for i := 0; i < n; i++ {
				if used[i] {
					continue
				}
				k := key(items[i])
				if triedKeys[k] {
					continue
				}
				triedKeys[k] = true
				used[i] = true
				cur = append(cur, items[i])
				if err := search(); err != nil {
					return err
				}
				cur = cur[:len(cur)-1]
				used[i] = false
			}
			return nil
		}
		err := search()
		return best, haveBest, err
	}

	// Fan out by the choice of first item: each distinct first-item key roots an
	// independent subtree. The set (and its order) matches the sequential DFS's
	// depth-0 expansion, so reducing the per-task bests in first-item order with
	// the same strict-better rule yields the exact result a sequential search would.
	var firsts []int
	tried := make(map[string]bool)
	for i := 0; i < n; i++ {
		k := key(items[i])
		if tried[k] {
			continue
		}
		tried[k] = true
		firsts = append(firsts, i)
	}

	type taskResult struct {
		best pack.Result
		have bool
		err  error
	}
	outs := make([]taskResult, len(firsts))
	var completed int64
	parallelFor(len(firsts), func(t int) {
		b, have, err := searchFrom(firsts[t])
		outs[t] = taskResult{best: b, have: have, err: err}
		if opts.Progress != nil {
			opts.Progress(int(atomic.AddInt64(&completed, 1)), len(firsts))
		}
	})

	var best pack.Result
	haveBest := false
	var cancelErr error
	for _, tr := range outs {
		if tr.err != nil {
			cancelErr = tr.err
		}
		if tr.have && (!haveBest || betterBrute(tr.best, best)) {
			best = tr.best
			haveBest = true
		}
	}
	if cancelErr != nil {
		if haveBest {
			return best, cancelErr // partial best on cancellation
		}
		return pack.Result{}, cancelErr
	}
	return best, nil
}

// betterBrute ranks brute-force candidates: fewer unplaced wins, then fewer bins.
func betterBrute(a, b pack.Result) bool {
	if len(a.Unplaced) != len(b.Unplaced) {
		return len(a.Unplaced) < len(b.Unplaced)
	}
	return a.BinsUsed() < b.BinsUsed()
}
