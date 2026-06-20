package pack

import "errors"

var (
	// ErrItemTooLarge is returned by OnlinePacker.Pack when an item cannot be
	// placed in any bin, either because it is geometrically too large or because
	// a constraint would be violated even on a completely empty bin.
	// Result.PlacementErrors carries the per-item reason when more detail is available.
	ErrItemTooLarge = errors.New("pack: item too large for any bin")

	// ErrNoRoom is returned by Bin.TryPlace when the item does not fit in this
	// particular bin instance (insufficient remaining space or a constraint that
	// only fails because the bin is non-empty). The caller should try another bin.
	ErrNoRoom = errors.New("pack: no room in this bin")

	// ErrNoOpenBin is returned when no existing bin can accept the item and
	// no BinFactory is configured to open a new one.
	ErrNoOpenBin = errors.New("pack: no open bin available and no factory configured")
)
