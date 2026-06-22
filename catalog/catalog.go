// Package catalog selects the best container type for an order from a catalog of
// candidate container types, honouring an optional per-type maximum count — the
// heterogeneous-container model from skjolber/3d-bin-container-packing
// (Apache-2.0; see ATTRIBUTION.md for the pinned commit) and bavix/boxpacker3.
//
// It is dimension-agnostic: each candidate supplies a Pack closure that packs the
// items into bins of that one container type (built however the caller likes —
// 1-D, 2-D or 3-D). catalog.Best runs every candidate, enforces its MaxCount by
// dropping items that spill past the allowed bin count, and returns the result
// that packs the most items into the fewest, tightest containers.
//
// This implements "choose the single best container type for the order". Packing
// one order across several *different* container types simultaneously is a larger
// problem left for future work.
package catalog

import (
	"context"
	"strings"

	"github.com/W-Floyd/go-pack-bins/pack"
)

// Candidate is one container type in the catalog.
type Candidate struct {
	// Label identifies the container type (returned as the winner's name).
	Label string
	// MaxCount caps how many containers of this type are available; 0 = unlimited.
	MaxCount int
	// BinVolume is the capacity of one container of this type, used to score
	// wasted space when breaking ties. 0 disables the waste tie-break.
	BinVolume float64
	// Pack packs items into bins of this container type. It should respect ctx.
	Pack func(ctx context.Context, items []pack.Item) (pack.Result, error)
}

// Result is the winning candidate's packing plus its label.
type Result struct {
	pack.Result
	Label string
}

// Best runs every candidate and returns the best result, ranked by (in order):
//  1. most items placed (fewest unplaced),
//  2. fewest containers used,
//  3. least wasted volume (BinVolume×bins − packed item volume).
//
// A candidate whose Pack returns a non-ErrItemTooLarge error is skipped; if that
// error is a context cancellation, Best aborts and returns it. Returns
// ErrItemTooLarge if no candidate could place anything.
func Best(ctx context.Context, items []pack.Item, candidates []Candidate) (Result, error) {
	volByID := make(map[string]float64, len(items))
	for _, it := range items {
		volByID[it.ID()] = it.Volume()
	}

	var best Result
	var bestBinVol float64
	found := false
	for _, c := range candidates {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		r, err := c.Pack(ctx, items)
		if err != nil {
			if ctx.Err() != nil {
				return Result{}, ctx.Err()
			}
			if !isTooLarge(err) {
				continue
			}
		}
		r = truncateToMaxCount(r, c.MaxCount, volByID)
		cand := Result{Result: r, Label: c.Label}
		if !found || betterCatalog(cand, best, c.BinVolume, bestBinVol, volByID) {
			best = cand
			bestBinVol = c.BinVolume
			found = true
		}
	}
	if !found {
		return Result{}, pack.ErrItemTooLarge
	}
	return best, nil
}

// PackSequential packs items by cascading across the candidate container types
// in the given order: it fills each type up to its MaxCount, then spills the
// items that didn't fit (because the type's cap was reached, or they were too
// large) into the next type, and so on. Use this — rather than Best — when no
// single type can hold the whole order and you want a mix of sizes; list the
// preferred/cheaper types first.
//
// The returned Result may contain bins of different types (in the order they
// were opened, grouped by type); Label lists the types actually used. Items that
// fit no remaining type end up in Unplaced. ctx is honoured.
func PackSequential(ctx context.Context, items []pack.Item, candidates []Candidate) (Result, error) {
	volByID := make(map[string]float64, len(items))
	byID := make(map[string]pack.Item, len(items))
	for _, it := range items {
		volByID[it.ID()] = it.Volume()
		byID[it.ID()] = it
	}

	remaining := append([]pack.Item(nil), items...)
	var merged pack.Result
	var used []string

	for _, c := range candidates {
		if len(remaining) == 0 {
			break
		}
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		r, err := c.Pack(ctx, remaining)
		if err != nil {
			if ctx.Err() != nil {
				return Result{}, ctx.Err()
			}
			if !isTooLarge(err) {
				continue // this type failed hard; try the next
			}
		}
		r = truncateToMaxCount(r, c.MaxCount, volByID)
		if len(r.Placements) > 0 {
			merged.Bins = append(merged.Bins, r.Bins...)
			merged.Placements = append(merged.Placements, r.Placements...)
			used = append(used, c.Label)
		}
		// Whatever this type couldn't place (over cap, or too large) cascades on.
		remaining = itemsFromIDs(byID, r.Unplaced)
	}
	for _, it := range remaining {
		merged.Unplaced = append(merged.Unplaced, it.ID())
	}
	return Result{Result: merged, Label: strings.Join(used, "+")}, nil
}

// itemsFromIDs maps a list of item IDs back to their items, preserving order and
// skipping any unknown IDs.
func itemsFromIDs(byID map[string]pack.Item, ids []string) []pack.Item {
	out := make([]pack.Item, 0, len(ids))
	for _, id := range ids {
		if it, ok := byID[id]; ok {
			out = append(out, it)
		}
	}
	return out
}

func isTooLarge(err error) bool {
	return err == pack.ErrItemTooLarge || err.Error() == pack.ErrItemTooLarge.Error()
}

// truncateToMaxCount enforces maxCount by keeping only the first maxCount opened
// bins; placements in later bins are removed and their items become unplaced.
func truncateToMaxCount(r pack.Result, maxCount int, volByID map[string]float64) pack.Result {
	if maxCount <= 0 || len(r.Bins) <= maxCount {
		return r
	}
	allowed := make(map[string]bool, maxCount)
	for i := 0; i < maxCount; i++ {
		if idr, ok := r.Bins[i].(interface{ ID() string }); ok {
			allowed[idr.ID()] = true
		}
	}
	kept := r.Placements[:0:0]
	for _, p := range r.Placements {
		if p == nil {
			continue
		}
		if allowed[p.BinID()] {
			kept = append(kept, p)
		} else {
			r.Unplaced = append(r.Unplaced, p.ItemID())
		}
	}
	r.Placements = kept
	r.Bins = r.Bins[:maxCount]
	return r
}

func betterCatalog(a, b Result, aVol, bVol float64, volByID map[string]float64) bool {
	if len(a.Unplaced) != len(b.Unplaced) {
		return len(a.Unplaced) < len(b.Unplaced)
	}
	if a.BinsUsed() != b.BinsUsed() {
		return a.BinsUsed() < b.BinsUsed()
	}
	if aVol > 0 && bVol > 0 {
		return waste(a, aVol, volByID) < waste(b, bVol, volByID)
	}
	return false
}

func waste(r Result, binVol float64, volByID map[string]float64) float64 {
	packed := 0.0
	for _, p := range r.Placements {
		if p != nil {
			packed += volByID[p.ItemID()]
		}
	}
	return binVol*float64(r.BinsUsed()) - packed
}
