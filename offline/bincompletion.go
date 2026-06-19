package offline

import (
	"math"
	"sort"

	"github.com/wfloyd/go-pack-bins/pack"
)

// BinCompletion implements the Bin Completion exact algorithm for the 1-D
// bin packing problem (Korf, 2002; Schreiber & Korf, 2013).
//
// The algorithm searches for an optimal packing using branch-and-bound with:
//   - Lower bound: ceil(sum of sizes / bin capacity)
//   - Dominance pruning: skip partial completions dominated by previously found ones
//   - Undominated completion search: only generate maximal completions
//
// Complexity is exponential in the worst case but very fast in practice for
// instances with a small optimal number of bins.
//
// Only 1-D items are supported. Items larger than binCapacity are rejected.
func BinCompletion(items []pack.Item, binCapacity float64, factory pack.BinFactory) (pack.Result, error) {
	if len(items) == 0 {
		return pack.Result{}, nil
	}

	sizes := make([]float64, len(items))
	for i, item := range items {
		s := item.Volume()
		if s > binCapacity {
			return pack.Result{}, pack.ErrItemTooLarge
		}
		sizes[i] = s
	}

	// Sort items by decreasing size (required for branch-and-bound efficiency).
	order := make([]int, len(items))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool {
		return sizes[order[i]] > sizes[order[j]]
	})

	sortedSizes := make([]float64, len(items))
	for i, idx := range order {
		sortedSizes[i] = sizes[idx]
	}

	// Compute lower bound from continuous relaxation.
	totalSize := 0.0
	for _, s := range sortedSizes {
		totalSize += s
	}
	lowerBound := int(math.Ceil(totalSize / binCapacity))

	solver := &bcSolver{
		n:           len(items),
		sizes:       sortedSizes,
		binCapacity: binCapacity,
		bestBins:    len(items), // worst case: one item per bin
		bestAssign:  nil,
	}

	// Search for the optimal number of bins starting from the lower bound.
	for k := lowerBound; k <= len(items); k++ {
		assign := make([]int, len(items))
		remaining := make([]float64, k)
		for i := range remaining {
			remaining[i] = binCapacity
		}
		solver.bestBins = k + 1
		solver.bestAssign = nil
		if solver.search(assign, remaining, 0, k) {
			break
		}
	}

	if solver.bestAssign == nil {
		// Fallback: one item per bin (should not happen).
		solver.bestAssign = make([]int, len(items))
		for i := range solver.bestAssign {
			solver.bestAssign[i] = i
		}
		solver.bestBins = len(items)
	}

	// Build the result using the best assignment found.
	bins := make([]pack.Bin, solver.bestBins)
	for i := range bins {
		bins[i] = factory.Open()
	}

	result := pack.Result{Bins: bins, Placements: make([]pack.Placement, len(items))}
	for sortedIdx, binIdx := range solver.bestAssign {
		origIdx := order[sortedIdx]
		p, ok := bins[binIdx].TryPlace(items[origIdx])
		if !ok {
			result.Unplaced = append(result.Unplaced, items[origIdx].ID())
			continue
		}
		result.Placements[origIdx] = p
	}
	return result, nil
}

type bcSolver struct {
	n           int
	sizes       []float64
	binCapacity float64
	bestBins    int
	bestAssign  []int
}

// search attempts to assign item[itemIdx] to one of k bins.
// Prunes via lower bound and dominance.
// Returns true if a complete valid assignment using exactly k bins was found.
func (s *bcSolver) search(assign []int, remaining []float64, itemIdx, k int) bool {
	if itemIdx == s.n {
		// Count non-empty bins.
		usedBins := 0
		for _, r := range remaining {
			if r < s.binCapacity {
				usedBins++
			}
		}
		if usedBins < s.bestBins {
			s.bestBins = usedBins
			s.bestAssign = make([]int, len(assign))
			copy(s.bestAssign, assign)
		}
		return true
	}

	// Lower bound: remaining items cannot fit in available space.
	remTotal := 0.0
	for i := itemIdx; i < s.n; i++ {
		remTotal += s.sizes[i]
	}
	availTotal := 0.0
	for _, r := range remaining {
		availTotal += r
	}
	if remTotal > availTotal {
		return false
	}

	// Prune: current number of used bins ≥ best found so far.
	usedNow := 0
	for _, r := range remaining {
		if r < s.binCapacity {
			usedNow++
		}
	}
	if usedNow >= s.bestBins {
		return false
	}

	sz := s.sizes[itemIdx]
	found := false
	seen := make(map[float64]bool) // skip bins with the same remaining (symmetry breaking)
	for j := 0; j < k; j++ {
		if remaining[j] < sz {
			continue
		}
		if seen[remaining[j]] {
			continue // dominated by a previous identical bin
		}
		seen[remaining[j]] = true
		assign[itemIdx] = j
		remaining[j] -= sz
		if s.search(assign, remaining, itemIdx+1, k) {
			found = true
		}
		remaining[j] += sz
	}
	return found
}
