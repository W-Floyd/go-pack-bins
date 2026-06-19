// Package pack provides a unified interface for 1-D, 2-D, and 3-D bin packing.
//
// Online algorithms process items one at a time with irreversible placements.
// Offline algorithms may sort or rearrange the full item list before packing.
//
// The package is organised as:
//
//	pack       – core interfaces (this package)
//	d1         – 1-D items and bins (scalar capacity)
//	d2         – 2-D rectangular items and bins (MaxRects / Guillotine placement)
//	d3         – 3-D box items and bins (extreme-point placement); manifold solid items/bins
//	online     – all online bin-selection algorithms (NF, NkF, FF, BF, WF, AWF, RFF, Harmonic family)
//	offline    – offline wrappers and exact solvers (FFD, NFD, BFD, MFFD, KK, BinCompletion)
package pack

// Item is the unit of work placed into a bin.
// Concrete implementations live in the d1, d2, and d3 sub-packages.
type Item interface {
	// ID returns a stable, unique identifier for this item.
	ID() string

	// Volume returns the scalar measure used by bin-selection heuristics:
	//   1-D → length
	//   2-D → width × height
	//   3-D → width × depth × height (or voxel volume for solid items)
	// Offline sorters use Volume to order items by decreasing size.
	Volume() float64

	// Dimensions returns 1, 2, or 3.
	Dimensions() int
}

// Bin is a container that accepts items one at a time.
// The packing algorithm decides *which* bin to offer an item to; the bin
// decides *where* inside itself to place the item.
type Bin interface {
	// ID returns a stable, unique identifier for this bin.
	ID() string

	// TryPlace attempts to place item into the bin.
	// On success it mutates the bin's internal state and returns the placement.
	// On failure it leaves the bin unchanged and returns (nil, false).
	TryPlace(item Item) (Placement, bool)

	// Utilization returns the fraction of total capacity currently occupied,
	// in [0.0, 1.0]. Used by Best Fit and Worst Fit selectors.
	Utilization() float64

	// Remaining returns un-occupied capacity in the same units as Item.Volume().
	// Used as a fast-reject heuristic before calling TryPlace.
	Remaining() float64

	// Dimensions returns 1, 2, or 3.
	Dimensions() int

	// Items returns all items placed in this bin, in insertion order.
	Items() []Item
}

// BinFactory creates fresh, empty bins on demand.
// Packers call Open() whenever no existing bin can accept an item.
type BinFactory interface {
	Open() Bin
}

// Placement records where and how an item was placed in a bin.
// The concrete type carries dimension-specific spatial data and can be
// type-asserted by callers that need it (e.g. *d2.Placement2D).
type Placement interface {
	// BinID identifies the bin that contains this placement.
	BinID() string
	// ItemID identifies the placed item.
	ItemID() string
}

// Result is the output of a complete packing run.
type Result struct {
	// Bins holds the bins opened during packing, in order of opening.
	Bins []Bin
	// Placements is indexed parallel to the items slice passed to Pack.
	// A nil entry means the corresponding item was not placed.
	Placements []Placement
	// Unplaced holds IDs of items that could not be placed (empty on success).
	Unplaced []string
}

// BinsUsed returns the number of bins that were opened.
func (r Result) BinsUsed() int { return len(r.Bins) }

// BinSelector is the single decision an online algorithm makes: given the
// current set of open bins, attempt to place item in one of them.
// Returns the Placement and the bin index on success, or (nil, -1) to
// signal that a new bin should be opened.
//
// Selectors are responsible for calling TryPlace on their chosen bin(s).
// The Packer calls Open on the factory and retries if Select returns -1.
type BinSelector interface {
	Select(bins []Bin, item Item) (Placement, int)
}

// OnlinePacker packs items one at a time with no look-ahead.
type OnlinePacker interface {
	// Pack places item using the algorithm's bin-selection policy.
	// Returns ErrItemTooLarge if even a fresh bin cannot accept the item.
	// Returns ErrNoOpenBin if no factory is configured and all bins are full.
	Pack(item Item) (Placement, error)

	// Result returns the accumulated result. Safe to call at any point.
	Result() Result

	// Reset clears all packing state; the factory remains configured.
	Reset()

	// Name returns the algorithm identifier string.
	Name() string
}

// OfflinePacker inspects all items before packing any of them.
type OfflinePacker interface {
	// PackAll sorts or otherwise preprocesses items, then packs them all.
	PackAll(items []Item) (Result, error)

	// Name returns the algorithm identifier string.
	Name() string
}
