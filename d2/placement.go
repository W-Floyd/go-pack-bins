package d2

import "github.com/W-Floyd/go-pack-bins/pack"

// Placement2D records the spatial result of placing a 2-D item in a bin.
type Placement2D struct {
	binID, itemID string
	// X, Y is the bottom-left corner of the placed rectangle.
	X, Y float64
	// W, H are the placed dimensions (may differ from Item2D.W/H if rotated).
	W, H    float64
	Rotated bool
}

func (p *Placement2D) BinID() string  { return p.binID }
func (p *Placement2D) ItemID() string { return p.itemID }

var _ pack.Placement = (*Placement2D)(nil)

// PlacementStrategy2D is the interface for within-bin placement engines.
// Implementations track free space and decide where new items go.
type PlacementStrategy2D interface {
	// TryInsert attempts to place a rectangle of the given dimensions.
	// Returns position (x, y), whether it was rotated, and success.
	TryInsert(w, h float64, allowRotate bool) (x, y float64, rotated bool, ok bool)

	// Utilization returns occupied area / total area in [0, 1].
	Utilization() float64

	// Remaining returns the un-occupied area.
	Remaining() float64
}
