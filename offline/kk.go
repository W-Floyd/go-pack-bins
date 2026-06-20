package offline

import (
	"container/heap"
	"errors"

	"github.com/wfloyd/go-pack-bins/pack"
)

// KarmarkarKarp implements the Karmarkar-Karp differencing heuristic for the
// 1-D bin packing problem.
//
// The algorithm iteratively replaces the two largest values with their
// difference, then reconstructs the partition by back-tracking the sequence.
// Additive approximation: KK(I) ≤ OPT(I) + O(log²(OPT(I))).
//
// Only 1-D items are supported (Dimensions()==1).
// Returns ErrItemTooLarge if any item exceeds binCapacity.
func KarmarkarKarp(items []pack.Item, binCapacity float64, factory pack.BinFactory) (pack.Result, error) {
	if len(items) == 0 {
		return pack.Result{}, nil
	}
	for _, item := range items {
		if item.Volume() > binCapacity {
			return pack.Result{}, pack.ErrItemTooLarge
		}
	}

	h := &kkHeap{}
	for i, item := range items {
		heap.Push(h, &kkNode{
			value:  item.Volume(),
			groups: [][]int{{i}},
		})
	}

	for h.Len() > 1 {
		a := heap.Pop(h).(*kkNode)
		b := heap.Pop(h).(*kkNode)
		diff := a.value - b.value
		if diff < 0 {
			diff = -diff
			a, b = b, a
		}
		combined := &kkNode{
			value:  diff,
			groups: append(a.groups, b.groups...),
		}
		heap.Push(h, combined)
	}

	root := heap.Pop(h).(*kkNode)
	var result pack.Result
	// Each group of item indices is packed into one bin.
	for _, g := range root.groups {
		b := factory.Open()
		result.Bins = append(result.Bins, b)
		for _, idx := range g {
			p, err := b.TryPlace(items[idx])
			if err != nil {
				result.Unplaced = append(result.Unplaced, items[idx].ID())
				if !errors.Is(err, pack.ErrNoRoom) {
					result.SetPlacementError(items[idx].ID(), err)
				}
				continue
			}
			result.Placements = append(result.Placements, p)
		}
	}
	return result, nil
}

// ─── max-heap ────────────────────────────────────────────────────────────────

type kkNode struct {
	value  float64
	groups [][]int // list of item-index groups
}

type kkHeap []*kkNode

func (h kkHeap) Len() int            { return len(h) }
func (h kkHeap) Less(i, j int) bool  { return h[i].value > h[j].value } // max-heap
func (h kkHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *kkHeap) Push(x interface{}) { *h = append(*h, x.(*kkNode)) }
func (h *kkHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}
