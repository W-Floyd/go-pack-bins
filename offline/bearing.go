package offline

import (
	"sort"

	"github.com/W-Floyd/go-pack-bins/pack"
)

// Bearing-aware orderings. A load-bearing gate only ever *refuses* a crushing
// placement — it never reorders to find an arrangement that avoids one. So with
// a volume-decreasing order a large fragile item lands on the floor first, fills
// it, and forces every heavy item into a new bin, even when "heavy underneath,
// fragile on top" would have fitted in one. These policies put the load-bearers
// down first so that arrangement is reachable.
//
// Source: the layer-from-floor schemes of Ratcliff & Bischoff (1998) and
// Bischoff (2006) order cargo by bearing strength from the floor upward for
// exactly this reason.

// bearLimitOf reads an item's bearing limit from its scalars, falling back to
// dflt when the item does not carry one — the same rule the gate applies, so the
// ordering and the constraint agree on which items are fragile.
func bearLimitOf(it pack.Item, limitScalar string, dflt float64) float64 {
	if sc := pack.ScalarsOf(it); sc != nil {
		if v, ok := sc[limitScalar]; ok {
			return v
		}
	}
	return dflt
}

// DecreasingBearing orders items by bearing limit descending, breaking ties by
// volume descending. The strongest items reach the floor first and the most
// fragile are placed last, on top.
//
// It overrides the volume-first heuristic FFD/BFD rely on, so it is a fix for a
// specific pathology rather than a general improvement: over 200 random 3-D
// instances with mixed limits it totalled 351 bins against volume-order's 350
// (better on 11, worse on 12). Where bearing order and volume order genuinely
// conflict — a large fragile item that volume-order would put on the floor — it
// is the difference between one bin and two. Hence opt-in, not the default.
//
// A variant deferring only zero-limit items while keeping volume order otherwise
// was measured too and was clearly worse (366 bins): it leaves items with
// intermediate limits to be buried and refused, while still breaking the
// large-first order. Respecting the whole strength ordering is what makes this
// coherent.
func DecreasingBearing(limitScalar string, defaultLimit float64) SortPolicy {
	return func(items []pack.Item) {
		sort.SliceStable(items, func(i, j int) bool {
			li := bearLimitOf(items[i], limitScalar, defaultLimit)
			lj := bearLimitOf(items[j], limitScalar, defaultLimit)
			if li != lj {
				return li > lj
			}
			return items[i].Volume() > items[j].Volume()
		})
	}
}

// DecreasingAccess orders items by how often they are wanted, most-accessed
// first, breaking ties by volume descending.
//
// Ordering is most of the access objective for a packer that has no intra-bin
// scoring. Every 3-D strategy already places bottom-first and near-corner-first,
// so whatever is offered earliest lands in the cheapest positions; handing them
// the frequently-wanted items first is what puts those within reach. Where a
// packer *does* score positions (joint), this still matters: it decides who gets
// to choose from the full set of cheap spots and who chooses from what is left.
func DecreasingAccess(freqScalar string, dflt float64) SortPolicy {
	return func(items []pack.Item) {
		sort.SliceStable(items, func(i, j int) bool {
			fi := accessFreqOf(items[i], freqScalar, dflt)
			fj := accessFreqOf(items[j], freqScalar, dflt)
			if fi != fj {
				return fi > fj
			}
			return items[i].Volume() > items[j].Volume()
		})
	}
}

func accessFreqOf(it pack.Item, name string, dflt float64) float64 {
	if name == "" {
		return dflt
	}
	if sc := pack.ScalarsOf(it); sc != nil {
		if v, ok := sc[name]; ok {
			return v
		}
	}
	return dflt
}
