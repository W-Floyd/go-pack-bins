package offline

import (
	"sort"

	"github.com/W-Floyd/go-pack-bins/pack"
)

// RefineBalanceMaxItems caps the problem size for RefineBalance: above it the
// local search (which re-validates moves by rebuilding bins) is skipped to keep
// packing responsive. The result is returned unchanged in that case. This is the
// default; callers may override via RefineOptions.MaxItems.
const RefineBalanceMaxItems = 80

// refineEvalBudget bounds the number of feasibility rebuilds so a single refine
// pass stays bounded regardless of instance shape. Default for
// RefineOptions.EvalBudget.
const refineEvalBudget = 40000

// RefineOptions configures RefineBalance.
type RefineOptions struct {
	// MaxItems is the problem-size cap above which the local search is skipped;
	// 0 uses RefineBalanceMaxItems.
	MaxItems int
	// EvalBudget bounds the number of feasibility rebuilds in one pass; 0 uses
	// refineEvalBudget.
	EvalBudget int
}

func (o RefineOptions) maxItems() int {
	if o.MaxItems <= 0 {
		return RefineBalanceMaxItems
	}
	return o.MaxItems
}

func (o RefineOptions) evalBudget() int {
	if o.EvalBudget <= 0 {
		return refineEvalBudget
	}
	return o.EvalBudget
}

// RefineBalance improves an existing packing by local search: it repeatedly
// moves a single item to another bin, or swaps two items between bins, accepting
// any change that lowers the imbalance score (coefficient of variation of item
// count and each scalar, summed). Feasibility — capacity, geometry, and hard
// constraints — is checked by rebuilding the affected bins via factory, so the
// returned result is always valid. The bin count never increases (empty bins are
// pruned), only the distribution within it changes.
//
// factory must match the one that produced r (same dimensions/constraints).
// Large instances (> opts.MaxItems) are returned unchanged.
func RefineBalance(factory pack.BinFactory, r pack.Result, items []pack.Item, opts RefineOptions) pack.Result {
	if len(items) == 0 || len(items) > opts.maxItems() {
		return r
	}
	byID := make(map[string]pack.Item, len(items))
	for _, it := range items {
		byID[it.ID()] = it
	}

	// Group placed items into per-bin sets, preserving bin order.
	idx := map[string]int{}
	var asn [][]pack.Item
	for _, b := range r.Bins {
		id := binID(b)
		idx[id] = len(asn)
		asn = append(asn, nil)
	}
	for _, p := range r.Placements {
		if p == nil {
			continue
		}
		if bi, ok := idx[p.BinID()]; ok {
			if it := byID[p.ItemID()]; it != nil {
				asn[bi] = append(asn[bi], it)
			}
		}
	}

	budget := opts.evalBudget()
	feasible := func(set []pack.Item) bool {
		budget--
		bin := factory.Open()
		ordered := append([]pack.Item(nil), set...)
		sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Volume() > ordered[j].Volume() })
		for _, it := range ordered {
			if _, err := bin.TryPlace(it); err != nil {
				return false
			}
		}
		return true
	}

	score := imbalanceScore(asn)
	for budget > 0 {
		improved := false

		// Moves: item from bin i → bin j.
	moveScan:
		for i := range asn {
			for j := range asn {
				if i == j || budget <= 0 {
					continue
				}
				for ai := range asn[i] {
					item := asn[i][ai]
					if !feasible(append(append([]pack.Item(nil), asn[j]...), item)) {
						continue
					}
					cand := cloneAssignment(asn)
					cand[j] = append(cand[j], item)
					cand[i] = removeAt(cand[i], ai)
					if s := imbalanceScore(cand); s < score-1e-9 {
						asn, score, improved = cand, s, true
						break moveScan
					}
				}
			}
		}

		// Swaps: item a in bin i ↔ item b in bin j.
		if !improved {
		swapScan:
			for i := range asn {
				for j := i + 1; j < len(asn); j++ {
					if budget <= 0 {
						break
					}
					for ai := range asn[i] {
						for bj := range asn[j] {
							a, b := asn[i][ai], asn[j][bj]
							si := replaceAt(asn[i], ai, b)
							sj := replaceAt(asn[j], bj, a)
							if !feasible(si) || !feasible(sj) {
								continue
							}
							cand := cloneAssignment(asn)
							cand[i], cand[j] = si, sj
							if s := imbalanceScore(cand); s < score-1e-9 {
								asn, score, improved = cand, s, true
								break swapScan
							}
						}
					}
				}
			}
		}

		if !improved {
			break
		}
	}

	return rebuildResult(factory, asn, r)
}

// rebuildResult reconstructs a Result by placing each non-empty bin's items
// (largest first) into a fresh bin, carrying over unplaced items/errors.
func rebuildResult(factory pack.BinFactory, asn [][]pack.Item, orig pack.Result) pack.Result {
	out := pack.Result{Unplaced: orig.Unplaced, PlacementErrors: orig.PlacementErrors}
	for _, set := range asn {
		if len(set) == 0 {
			continue
		}
		ordered := append([]pack.Item(nil), set...)
		sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Volume() > ordered[j].Volume() })
		bin := factory.Open()
		placedAny := false
		for _, it := range ordered {
			if p, err := bin.TryPlace(it); err == nil {
				out.Placements = append(out.Placements, p)
				placedAny = true
			}
		}
		if placedAny {
			out.Bins = append(out.Bins, bin)
		}
	}
	return out
}

// imbalanceScore sums the squared coefficient of variation (σ²/mean²) of item
// count and each scalar across the non-empty bins. Lower is more balanced; the
// scale-free terms let metrics of different magnitudes contribute comparably.
func imbalanceScore(asn [][]pack.Item) float64 {
	var bins [][]pack.Item
	for _, b := range asn {
		if len(b) > 0 {
			bins = append(bins, b)
		}
	}
	n := len(bins)
	if n == 0 {
		return 0
	}
	keys := map[string]bool{}
	for _, b := range bins {
		for _, it := range b {
			for k := range pack.ScalarsOf(it) {
				keys[k] = true
			}
		}
	}
	cv := func(vals []float64) float64 {
		mean := 0.0
		for _, v := range vals {
			mean += v
		}
		mean /= float64(n)
		if mean == 0 {
			return 0
		}
		varc := 0.0
		for _, v := range vals {
			varc += (v - mean) * (v - mean)
		}
		return (varc / float64(n)) / (mean * mean)
	}
	counts := make([]float64, n)
	for i, b := range bins {
		counts[i] = float64(len(b))
	}
	total := cv(counts)
	for k := range keys {
		vals := make([]float64, n)
		for i, b := range bins {
			for _, it := range b {
				vals[i] += pack.ScalarsOf(it)[k]
			}
		}
		total += cv(vals)
	}
	return total
}

func cloneAssignment(asn [][]pack.Item) [][]pack.Item {
	out := make([][]pack.Item, len(asn))
	for i, b := range asn {
		out[i] = append([]pack.Item(nil), b...)
	}
	return out
}

func removeAt(s []pack.Item, i int) []pack.Item {
	out := append([]pack.Item(nil), s[:i]...)
	return append(out, s[i+1:]...)
}

func replaceAt(s []pack.Item, i int, with pack.Item) []pack.Item {
	out := append([]pack.Item(nil), s...)
	out[i] = with
	return out
}

// binID extracts a bin's ID via its optional ID() method.
func binID(b pack.Bin) string {
	if idr, ok := b.(interface{ ID() string }); ok {
		return idr.ID()
	}
	return ""
}
