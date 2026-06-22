// Package gbpp implements the Generalized Bin Packing Problem objective from
// Baldi, Crainic, Perboli & Tadei (2012), as surveyed by Mantzou & Dimitriadis
// (2025): items may be compulsory or optional, optional items carry a profit,
// and using a bin incurs a cost. The goal is to minimise net cost = (bins used ×
// bin cost) − (profit of included optional items). This generalises classic bin
// packing (the BPP minimises bins only) and bin-packing-with-rejections (BPRC),
// where an optional item is rejected if it is not worth the space it needs.
//
// It is dimension-agnostic: profit and the optional flag are read from item
// scalars, and packing uses any pack.BinFactory, so it works for 1-D, 2-D and
// 3-D items alike.
package gbpp

import (
	"context"
	"sort"

	"github.com/W-Floyd/go-pack-bins/offline"
	"github.com/W-Floyd/go-pack-bins/pack"
)

// Options configures the GBPP solve.
type Options struct {
	// BinCost is the cost charged per bin used (default 0 — then optional items
	// are included whenever they fit, never charged for a new bin).
	BinCost float64
	// ProfitScalar names the item scalar holding an optional item's profit
	// (default "profit").
	ProfitScalar string
	// OptionalScalar names the item scalar that, when non-zero, marks an item as
	// optional (default "optional"). Items without it are compulsory.
	OptionalScalar string
}

func (o Options) profitKey() string {
	if o.ProfitScalar == "" {
		return "profit"
	}
	return o.ProfitScalar
}

func (o Options) optionalKey() string {
	if o.OptionalScalar == "" {
		return "optional"
	}
	return o.OptionalScalar
}

// Result is a GBPP packing plus its economics.
type Result struct {
	pack.Result
	// NetCost is BinCost×BinsUsed − IncludedProfit (lower is better).
	NetCost float64
	// IncludedProfit is the total profit of the optional items that were packed.
	IncludedProfit float64
	// Rejected lists optional items deliberately left out (not worth a bin).
	Rejected []string
}

// Pack solves the GBPP for items. Compulsory items are packed first (First-Fit-
// Decreasing); optional items are then considered by decreasing profit and
// included greedily: into an existing bin's free space whenever they fit, or
// into a new bin only when their profit covers the bin cost. ctx is honoured.
func Pack(ctx context.Context, items []pack.Item, factory pack.BinFactory, opts Options) Result {
	profitKey, optKey := opts.profitKey(), opts.optionalKey()

	var compulsory, optional []pack.Item
	profitOf := make(map[string]float64, len(items))
	for _, it := range items {
		s := pack.ScalarsOf(it)
		profitOf[it.ID()] = s[profitKey]
		if s[optKey] != 0 {
			optional = append(optional, it)
		} else {
			compulsory = append(compulsory, it)
		}
	}

	// Phase 1: pack all compulsory items.
	base, _ := offline.FirstFitDecreasing(factory).PackAllCtx(ctx, compulsory)
	res := Result{Result: base}

	// Phase 2: include optional items by decreasing profit.
	sort.SliceStable(optional, func(i, j int) bool {
		return profitOf[optional[i].ID()] > profitOf[optional[j].ID()]
	})
	for _, it := range optional {
		if ctx.Err() != nil {
			res.Rejected = append(res.Rejected, it.ID())
			res.Unplaced = append(res.Unplaced, it.ID())
			continue
		}
		if placed := tryExistingBins(&res.Result, it); placed {
			res.IncludedProfit += profitOf[it.ID()]
			continue
		}
		// Doesn't fit any open bin — opening a new one is worth it only if the
		// item's profit covers the bin cost.
		if profitOf[it.ID()] >= opts.BinCost {
			b := factory.Open()
			if p, err := b.TryPlace(it); err == nil {
				res.Bins = append(res.Bins, b)
				res.Placements = append(res.Placements, p)
				res.IncludedProfit += profitOf[it.ID()]
				continue
			}
		}
		res.Rejected = append(res.Rejected, it.ID())
		res.Unplaced = append(res.Unplaced, it.ID())
	}

	res.NetCost = opts.BinCost*float64(res.BinsUsed()) - res.IncludedProfit
	return res
}

// tryExistingBins attempts to place item into one of the already-open bins
// (no new bin). On success it records the placement in r and returns true.
func tryExistingBins(r *pack.Result, item pack.Item) bool {
	for _, b := range r.Bins {
		if p, err := b.TryPlace(item); err == nil {
			r.Placements = append(r.Placements, p)
			return true
		}
	}
	return false
}
