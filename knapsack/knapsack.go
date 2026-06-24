// Package knapsack implements single-container packing that maximises the total
// value of the packed subset, rather than minimising the number of bins. Given
// ONE container and more items than fit, it chooses a high-value subset that fits:
// the geometric ("which boxes?") generalisation of the 0/1 knapsack problem.
//
// Use cases: "what is the most valuable load that fits this one truck/pallet?",
// revenue-maximising container stuffing, or — with the default value = item
// volume — simply maximising packed volume (utilisation) of a single container.
//
// It is dimension-agnostic: it drives any single pack.Bin (1-D, 2-D or 3-D) via
// TryPlace, so feasibility (geometry, rotation, constraints) is whatever the bin
// enforces. The heuristic is greedy by value density with a leftover-fill pass;
// because the Bin contract is append-only (no removal), it does not do swap-based
// improvement. It reports the achieved total value, not an optimality bound.
package knapsack

import (
	"context"
	"sort"

	"github.com/W-Floyd/go-pack-bins/pack"
)

// Options configures the knapsack solve.
type Options struct {
	// ValueScalar names the item scalar holding each item's value (default
	// "value"). An item without that scalar falls back to its Volume(), so the
	// default behaviour with no value scalars set is to maximise packed volume.
	ValueScalar string
}

func (o Options) valueKey() string {
	if o.ValueScalar == "" {
		return "value"
	}
	return o.ValueScalar
}

// Result is a single-container knapsack packing.
type Result struct {
	pack.Result
	// TotalValue is the summed value of the selected (placed) items.
	TotalValue float64
	// Rejected lists the IDs of items deliberately left out (did not fit, or were
	// skipped as lower value once the container filled).
	Rejected []string
}

// valueOf returns an item's value: its ValueScalar if present and non-zero, else
// its Volume() (so the default objective is to maximise packed volume).
func valueOf(it pack.Item, key string) float64 {
	if v, ok := pack.ScalarsOf(it)[key]; ok && v != 0 {
		return v
	}
	return it.Volume()
}

// Pack selects and places a high-value subset of items into the single container
// bin, maximising total value. Items are tried in decreasing value-density order
// (value / volume, ties broken by larger value then larger volume); each is placed
// if it fits, else skipped. A second pass retries skipped items against the space
// the first pass left, since geometry often leaves gaps a later, smaller item can
// use. ctx is honoured — on cancellation the best-so-far selection is returned.
func Pack(ctx context.Context, items []pack.Item, bin pack.Bin, opts Options) Result {
	key := opts.valueKey()

	order := make([]pack.Item, len(items))
	copy(order, items)
	density := func(it pack.Item) float64 {
		v := valueOf(it, key)
		if vol := it.Volume(); vol > 0 {
			return v / vol
		}
		return v
	}
	sort.SliceStable(order, func(i, j int) bool {
		di, dj := density(order[i]), density(order[j])
		if di != dj {
			return di > dj
		}
		vi, vj := valueOf(order[i], key), valueOf(order[j], key)
		if vi != vj {
			return vi > vj
		}
		return order[i].Volume() > order[j].Volume()
	})

	var res Result
	res.Bins = []pack.Bin{bin}
	res.Placements = make([]pack.Placement, len(items))
	idx := make(map[string]int, len(items))
	for i, it := range items {
		idx[it.ID()] = i
	}

	placed := make(map[string]bool, len(items))
	place := func(it pack.Item) bool {
		p, err := bin.TryPlace(it)
		if err != nil {
			return false
		}
		res.Placements[idx[it.ID()]] = p
		res.TotalValue += valueOf(it, key)
		placed[it.ID()] = true
		return true
	}

	// Pass 1: greedy by density. Pass 2: retry the skipped items (a smaller item
	// may slot into a gap the bigger ones left). Two passes are cheap and recover
	// most of the easy leftover fill without needing item removal.
	for pass := 0; pass < 2; pass++ {
		progressed := false
		for _, it := range order {
			if ctx.Err() != nil {
				break
			}
			if placed[it.ID()] {
				continue
			}
			if place(it) {
				progressed = true
			}
		}
		if !progressed {
			break
		}
	}

	for _, it := range items {
		if !placed[it.ID()] {
			res.Unplaced = append(res.Unplaced, it.ID())
			res.Rejected = append(res.Rejected, it.ID())
		}
	}
	return res
}

// PackWith selects a high-value subset and packs it densely using the supplied
// offline packer (which carries its own single container), instead of the
// one-at-a-time greedy of Pack. Items are fed in decreasing value-density order,
// so the denser packer (e.g. a 3-D block/column packer) tends to seat the most
// valuable items; whatever it leaves Unplaced is rejected. This typically beats
// Pack when a fused-layer packer fits more items than greedy placement would —
// callers that want a guarantee should run both and keep the higher TotalValue.
//
// Placements are returned in the packer's order (not the input order). The packer
// must pack into a single container of fixed capacity.
func PackWith(ctx context.Context, items []pack.Item, packer pack.CtxOfflinePacker, opts Options) Result {
	key := opts.valueKey()
	order := make([]pack.Item, len(items))
	copy(order, items)
	density := func(it pack.Item) float64 {
		v := valueOf(it, key)
		if vol := it.Volume(); vol > 0 {
			return v / vol
		}
		return v
	}
	sort.SliceStable(order, func(i, j int) bool {
		di, dj := density(order[i]), density(order[j])
		if di != dj {
			return di > dj
		}
		return valueOf(order[i], key) > valueOf(order[j], key)
	})

	pr, _ := packer.PackAllCtx(ctx, order)
	var res Result
	res.Result = pr
	placed := make(map[string]bool, len(pr.Placements))
	for _, p := range pr.Placements {
		if p != nil {
			placed[p.ItemID()] = true
		}
	}
	for _, it := range items {
		if placed[it.ID()] {
			res.TotalValue += valueOf(it, key)
		} else {
			res.Rejected = append(res.Rejected, it.ID())
		}
	}
	return res
}
