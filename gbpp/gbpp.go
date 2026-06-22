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
	// NetCost is (total bin cost) − IncludedProfit (lower is better).
	NetCost float64
	// IncludedProfit is the total profit of the optional items that were packed.
	IncludedProfit float64
	// Rejected lists optional items deliberately left out (not worth a bin).
	Rejected []string
	// BinTypeIdx is set by PackCatalog: the catalog type index of each bin in
	// Bins (so callers can recover each bin's size/cost). Nil for Pack.
	BinTypeIdx []int
}

// BinType is one container type in a GBPP catalog: a cost per bin used, an
// optional usage cap, and a factory that opens a fresh bin of this type.
type BinType struct {
	Label    string
	Cost     float64
	MaxCount int // 0 = unlimited
	Open     func() pack.Bin
}

// PackCatalog solves the GBPP over a *catalog* of bin types, choosing the most
// profitable mix rather than exhausting one type before trying another: each
// item is placed into an open bin if it fits, else into a freshly opened bin of
// the cheapest type that can hold it (respecting MaxCount). Compulsory items are
// always packed; an optional item only opens a new bin when its profit covers
// that type's cost. Net cost = Σ opened-bin costs − Σ included profit.
//
// This is the heterogeneous (variable-cost) extension of Pack; the per-bin type
// is reported in Result.BinTypeIdx. ctx is honoured.
func PackCatalog(ctx context.Context, items []pack.Item, types []BinType, opts Options) Result {
	profitKey, optKey := opts.profitKey(), opts.optionalKey()

	// Try the cheapest type first when opening a new bin.
	order := make([]int, len(types))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return types[order[a]].Cost < types[order[b]].Cost })

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
	sort.SliceStable(compulsory, func(i, j int) bool { return compulsory[i].Volume() > compulsory[j].Volume() })
	sort.SliceStable(optional, func(i, j int) bool { return profitOf[optional[i].ID()] > profitOf[optional[j].ID()] })

	type openBin struct {
		bin     pack.Bin
		typeIdx int
	}
	var open []openBin
	counts := make([]int, len(types))
	var res Result

	placeExisting := func(it pack.Item) bool {
		for _, o := range open {
			if p, err := o.bin.TryPlace(it); err == nil {
				res.Placements = append(res.Placements, p)
				return true
			}
		}
		return false
	}
	// openFor opens the cheapest feasible new bin for it. When optional, it only
	// opens a type whose cost the item's profit covers.
	openFor := func(it pack.Item, optional bool, profit float64) bool {
		for _, ti := range order {
			t := types[ti]
			if t.MaxCount > 0 && counts[ti] >= t.MaxCount {
				continue
			}
			if optional && profit < t.Cost {
				continue
			}
			b := t.Open()
			p, err := b.TryPlace(it)
			if err != nil {
				continue
			}
			open = append(open, openBin{b, ti})
			counts[ti]++
			res.Bins = append(res.Bins, b)
			res.BinTypeIdx = append(res.BinTypeIdx, ti)
			res.Placements = append(res.Placements, p)
			return true
		}
		return false
	}

	for _, it := range compulsory {
		if ctx.Err() != nil {
			res.Unplaced = append(res.Unplaced, it.ID())
			continue
		}
		if placeExisting(it) || openFor(it, false, 0) {
			continue
		}
		res.Unplaced = append(res.Unplaced, it.ID())
	}
	for _, it := range optional {
		if ctx.Err() != nil {
			res.Rejected = append(res.Rejected, it.ID())
			res.Unplaced = append(res.Unplaced, it.ID())
			continue
		}
		profit := profitOf[it.ID()]
		if placeExisting(it) {
			res.IncludedProfit += profit
			continue
		}
		if openFor(it, true, profit) {
			res.IncludedProfit += profit
			continue
		}
		res.Rejected = append(res.Rejected, it.ID())
		res.Unplaced = append(res.Unplaced, it.ID())
	}

	binCost := 0.0
	for _, ti := range res.BinTypeIdx {
		binCost += types[ti].Cost
	}
	res.NetCost = binCost - res.IncludedProfit
	return res
}

// Pack solves the GBPP for items. To pack tightly, it first packs *all* items
// together (compulsory and optional) with First-Fit-Decreasing, so optional
// items consolidate with compulsory ones instead of being slotted into leftover
// gaps afterwards. It then drops only the optional items that genuinely forced an
// extra bin not worth its cost: any bin containing no compulsory item whose total
// optional profit is below the bin cost is removed and its items rejected. An
// optional item sharing a bin with compulsory items is always kept (its profit is
// free — that bin's cost is already paid). ctx is honoured.
func Pack(ctx context.Context, items []pack.Item, factory pack.BinFactory, opts Options) Result {
	profitKey, optKey := opts.profitKey(), opts.optionalKey()
	optional := make(map[string]bool, len(items))
	profitOf := make(map[string]float64, len(items))
	for _, it := range items {
		s := pack.ScalarsOf(it)
		profitOf[it.ID()] = s[profitKey]
		optional[it.ID()] = s[optKey] != 0
	}

	// Phase 1: pack everything together for the tightest consolidation.
	full, _ := offline.FirstFitDecreasing(factory).PackAllCtx(ctx, items)

	byBin := make(map[string][]pack.Placement, len(full.Bins))
	for _, p := range full.Placements {
		if p != nil {
			byBin[p.BinID()] = append(byBin[p.BinID()], p)
		}
	}

	// Phase 2: keep every bin that holds a compulsory item; drop an all-optional
	// bin only when its optional profit can't cover the bin cost.
	var res Result
	res.PlacementErrors = full.PlacementErrors
	for _, b := range full.Bins {
		pls := byBin[binID(b)]
		hasCompulsory := false
		binProfit := 0.0
		for _, p := range pls {
			if optional[p.ItemID()] {
				binProfit += profitOf[p.ItemID()]
			} else {
				hasCompulsory = true
			}
		}
		if !hasCompulsory && binProfit < opts.BinCost {
			continue // not worth its cost — reject this bin's (all-optional) items
		}
		res.Bins = append(res.Bins, b)
		for _, p := range pls {
			res.Placements = append(res.Placements, p)
			if optional[p.ItemID()] {
				res.IncludedProfit += profitOf[p.ItemID()]
			}
		}
	}

	// Anything not in the final packing is unplaced; unplaced optionals are
	// reported as rejected.
	placed := make(map[string]bool, len(res.Placements))
	for _, p := range res.Placements {
		placed[p.ItemID()] = true
	}
	for _, it := range items {
		if !placed[it.ID()] {
			res.Unplaced = append(res.Unplaced, it.ID())
			if optional[it.ID()] {
				res.Rejected = append(res.Rejected, it.ID())
			}
		}
	}

	res.NetCost = opts.BinCost*float64(res.BinsUsed()) - res.IncludedProfit
	return res
}

// binID returns a bin's ID via its optional ID() method.
func binID(b pack.Bin) string {
	if idr, ok := b.(interface{ ID() string }); ok {
		return idr.ID()
	}
	return ""
}
