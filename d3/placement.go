package d3

import "github.com/W-Floyd/go-pack-bins/pack"

// Placement3D records the spatial result of a 3-D box placement.
type Placement3D struct {
	binID, itemID string
	X, Y, Z       float64 // position of the item's min-corner
	W, D, H       float64 // actual placed dimensions (may differ from item if rotated)
}

func (p *Placement3D) BinID() string  { return p.binID }
func (p *Placement3D) ItemID() string { return p.itemID }

var _ pack.Placement = (*Placement3D)(nil)

// PlacementStrategy3D is the interface for within-bin 3-D placement engines.
type PlacementStrategy3D interface {
	// TryInsert attempts to place an item with any of the given orientations.
	// Returns the chosen position, the dimensions used (after orientation choice), and success.
	TryInsert(orientations [][3]float64) (x, y, z, w, d, h float64, ok bool)

	// Utilization returns occupied volume / total volume in [0, 1].
	Utilization() float64

	// Remaining returns un-occupied volume.
	Remaining() float64
}
