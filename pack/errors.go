package pack

import "errors"

var (
	// ErrItemTooLarge is returned when an item cannot fit even in a fresh bin.
	ErrItemTooLarge = errors.New("pack: item too large for any bin")

	// ErrNoOpenBin is returned when no existing bin can accept the item and
	// no BinFactory is configured to open a new one.
	ErrNoOpenBin = errors.New("pack: no open bin available and no factory configured")
)
