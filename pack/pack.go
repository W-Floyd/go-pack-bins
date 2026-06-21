// Package pack provides a unified interface for 1-D, 2-D, and 3-D bin packing.
//
// Online algorithms process items one at a time with irreversible placements.
// Offline algorithms may sort or rearrange the full item list before packing.
//
// The package is organised as:
//
//	pack       – core interfaces (this package)
//	d1         – 1-D items and bins (scalar capacity)
//	d2         – 2-D rectangular items and bins (MaxRects / Guillotine / Skyline / Shelf placement)
//	d3         – 3-D box items and bins (extreme-point / bottom-left-fill); manifold solid items/bins
//	online     – all online bin-selection algorithms (NF, NkF, FF, BF, WF, AWF, RFF, Harmonic family, Sum-of-Squares)
//	offline    – offline wrappers and exact solvers (FFD, NFD, BFD, WFD, MFFD, NFDH/FFDH/BFDH shelf, KK, BinCompletion)
package pack

import "context"

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
	// On failure it returns a non-nil error:
	//   ErrNoRoom      – item doesn't fit in this bin instance (try another bin).
	//   any other err  – item can never be placed in any bin of this configuration
	//                    (geometrically too large, or constraint violated even on
	//                    a completely empty bin). The caller must not open another bin.
	TryPlace(item Item) (Placement, error)

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
	// PlacementErrors holds the reason an item could not be placed, keyed by item ID.
	// Only populated when placement failed due to a permanent constraint or size
	// violation. Items that are unplaced purely due to insufficient space (the bin
	// was full and no new bin could hold them) are in Unplaced but not here.
	PlacementErrors map[string]error
}

// SetPlacementError records a permanent placement failure reason for the given item.
func (r *Result) SetPlacementError(id string, err error) {
	if r.PlacementErrors == nil {
		r.PlacementErrors = make(map[string]error)
	}
	r.PlacementErrors[id] = err
}

// BinsUsed returns the number of bins that were opened.
func (r Result) BinsUsed() int { return len(r.Bins) }

// BinSelector is the single decision an online algorithm makes: given the
// current set of open bins, attempt to place item in one of them.
//
//   - (p, idx, nil)  – placed in bin at index idx.
//   - (nil, -1, nil) – no existing bin fit; caller should open a new one.
//   - (nil, -1, err) – permanent failure; item can never be placed in any bin
//                      of this factory's configuration. Caller must not open a
//                      new bin. err is the same kind returned by Bin.TryPlace.
//
// Selectors are responsible for calling TryPlace on their chosen bin(s).
// The Packer calls Open on the factory and retries if Select returns -1, nil.
type BinSelector interface {
	Select(bins []Bin, item Item) (Placement, int, error)
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

// PlaceObserver is notified of each placement at the moment the packer commits
// it, in commit order. It enables streaming a solve as it progresses.
type PlaceObserver func(Placement)

// Observable is implemented by packers that can report each placement as it is
// committed mid-solve. The sequential packers support this: every online
// algorithm, and the sort-then-online offline wrappers that delegate to one.
// Global or multi-phase packers (exact solvers, BestOf, balancing) do not —
// they have no meaningful partial state to observe.
type Observable interface {
	// Observe registers fn to be called once per placement as it is committed.
	// A nil fn detaches any current observer. Survives Reset.
	Observe(fn PlaceObserver)
}

// OfflinePacker inspects all items before packing any of them.
type OfflinePacker interface {
	// PackAll sorts or otherwise preprocesses items, then packs them all.
	PackAll(items []Item) (Result, error)

	// Name returns the algorithm identifier string.
	Name() string
}

// CtxOfflinePacker is an OfflinePacker that supports cancellation. PackAllCtx
// behaves like PackAll but returns ctx.Err() promptly if ctx is cancelled
// mid-solve. Implemented by the offline wrappers, the exact solvers, BestOf, and
// JointFit; callers holding a context should prefer it. Anything implementing
// this also implements OfflinePacker (PackAll delegates with context.Background).
type CtxOfflinePacker interface {
	OfflinePacker
	PackAllCtx(ctx context.Context, items []Item) (Result, error)
}
