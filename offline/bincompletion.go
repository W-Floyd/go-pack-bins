package offline

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/W-Floyd/go-pack-bins/pack"
)

// BinCompletion implements the Bin Completion exact algorithm for the 1-D
// bin packing problem (Korf, 2002; Schreiber & Korf, 2013).
//
// Optional scalar Constraints are checked during the branch-and-bound search:
// branches that would violate a constraint are pruned, so the solver still
// finds the globally optimal feasible packing.
//
// Only 1-D items are supported. Items larger than binCapacity are rejected.
func BinCompletion(items []pack.Item, binCapacity float64, factory pack.BinFactory, constraints ...pack.Constraint) (pack.Result, error) {
	return BinCompletionCtx(context.Background(), items, binCapacity, factory, constraints...)
}

// BinCompletionCtx is BinCompletion with cancellation. The branch-and-bound
// search checks ctx periodically and aborts with ctx.Err() if cancelled — the
// exact solver can be exponential, so this is the most important place to cancel.
func BinCompletionCtx(ctx context.Context, items []pack.Item, binCapacity float64, factory pack.BinFactory, constraints ...pack.Constraint) (pack.Result, error) {
	if err := ctx.Err(); err != nil {
		return pack.Result{}, err
	}
	if len(items) == 0 {
		return pack.Result{}, nil
	}

	sizes := make([]float64, len(items))
	scalars := make([]map[string]float64, len(items))
	for i, item := range items {
		s := item.Volume()
		if s > binCapacity {
			return pack.Result{}, pack.ErrItemTooLarge
		}
		sizes[i] = s
		scalars[i] = pack.ScalarsOf(item)
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
	sortedScalars := make([]map[string]float64, len(items))
	for i, idx := range order {
		sortedSizes[i] = sizes[idx]
		sortedScalars[i] = scalars[idx]
	}

	// Compute lower bound from continuous relaxation.
	totalSize := 0.0
	for _, s := range sortedSizes {
		totalSize += s
	}
	lowerBound := int(math.Ceil(totalSize / binCapacity))

	solver := &bcSolver{
		ctx:         ctx,
		n:           len(items),
		sizes:       sortedSizes,
		scalars:     sortedScalars,
		binCapacity: binCapacity,
		constraints: constraints,
		bestBins:    len(items),
		bestAssign:  nil,
	}

	for k := lowerBound; k <= len(items); k++ {
		assign := make([]int, len(items))
		remaining := make([]float64, k)
		binAgg := make([]map[string]float64, k)
		for i := range remaining {
			remaining[i] = binCapacity
			binAgg[i] = make(map[string]float64)
		}
		solver.bestBins = k + 1
		solver.bestAssign = nil
		if solver.search(assign, remaining, binAgg, 0, k) {
			break
		}
		if solver.canceled {
			return pack.Result{}, ctx.Err()
		}
	}

	if solver.bestAssign == nil {
		solver.bestAssign = make([]int, len(items))
		for i := range solver.bestAssign {
			solver.bestAssign[i] = i
		}
		solver.bestBins = len(items)
	}

	bins := make([]pack.Bin, solver.bestBins)
	for i := range bins {
		bins[i] = factory.Open()
	}

	result := pack.Result{Bins: bins, Placements: make([]pack.Placement, len(items))}
	for sortedIdx, binIdx := range solver.bestAssign {
		origIdx := order[sortedIdx]
		p, err := bins[binIdx].TryPlace(items[origIdx])
		if err != nil {
			result.Unplaced = append(result.Unplaced, items[origIdx].ID())
			if !errors.Is(err, pack.ErrNoRoom) {
				result.SetPlacementError(items[origIdx].ID(), err)
			}
			continue
		}
		result.Placements[origIdx] = p
	}
	return result, nil
}

type bcSolver struct {
	ctx         context.Context
	canceled    bool
	steps       int
	n           int
	sizes       []float64
	scalars     []map[string]float64
	binCapacity float64
	constraints []pack.Constraint
	bestBins    int
	bestAssign  []int
}

// search attempts to assign items[itemIdx..] to k bins.
// binAgg tracks accumulated scalar totals per bin (mutated and restored).
func (s *bcSolver) search(assign []int, remaining []float64, binAgg []map[string]float64, itemIdx, k int) bool {
	// Cancellation: once tripped, unwind the whole recursion. Sample ctx every
	// 4096 node visits to keep the check off the hot path.
	if s.canceled {
		return false
	}
	s.steps++
	if s.steps&4095 == 0 && s.ctx.Err() != nil {
		s.canceled = true
		return false
	}

	if itemIdx == s.n {
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
	itemScalars := s.scalars[itemIdx]
	found := false
	seen := make(map[string]bool) // symmetry key: remaining + scalar aggregates
	for j := 0; j < k; j++ {
		if remaining[j] < sz {
			continue
		}
		// Hard scalar constraints.
		if !s.checkConstraints(binAgg[j], itemScalars) {
			continue
		}
		// Symmetry breaking: skip bins that are identical to one already tried.
		key := symKey(remaining[j], binAgg[j])
		if seen[key] {
			continue
		}
		seen[key] = true

		assign[itemIdx] = j
		remaining[j] -= sz
		applyScalars(binAgg[j], itemScalars, +1)
		pack.ApplyConstraints(s.constraints, binAgg[j], itemScalars)
		if s.search(assign, remaining, binAgg, itemIdx+1, k) {
			found = true
		}
		pack.RevertConstraints(s.constraints, binAgg[j], itemScalars)
		applyScalars(binAgg[j], itemScalars, -1)
		remaining[j] += sz
	}
	return found
}

func (s *bcSolver) checkConstraints(binAgg, itemScalars map[string]float64) bool {
	for _, con := range s.constraints {
		if !con.Check(binAgg, itemScalars) {
			return false
		}
	}
	return true
}

// applyScalars adds (sign=+1) or subtracts (sign=-1) itemScalars into agg.
func applyScalars(agg, itemScalars map[string]float64, sign float64) {
	for k, v := range itemScalars {
		agg[k] += sign * v
	}
}

// symKey builds a canonical string key for a bin state used in symmetry pruning.
// Two bins are symmetric only if both their remaining capacity and scalar
// aggregates are identical.
func symKey(remaining float64, agg map[string]float64) string {
	if len(agg) == 0 {
		return fmt.Sprintf("%g", remaining)
	}
	keys := make([]string, 0, len(agg))
	for k := range agg {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	fmt.Fprintf(&sb, "%g", remaining)
	for _, k := range keys {
		fmt.Fprintf(&sb, ",%s=%g", k, agg[k])
	}
	return sb.String()
}
