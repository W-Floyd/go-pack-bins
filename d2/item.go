// Package d2 provides 2-D rectangular items and bins with configurable placement strategies.
package d2

import "github.com/W-Floyd/go-pack-bins/pack"

// Item2D is a 2-D rectangular item with optional rotation support.
type Item2D struct {
	id          string
	W, H        float64 // natural orientation dimensions
	AllowRotate bool    // allow 90° rotation during placement
	scalars     map[string]float64
}

// NewItem creates a rectangular item.
func NewItem(id string, w, h float64, allowRotate bool) *Item2D {
	return &Item2D{id: id, W: w, H: h, AllowRotate: allowRotate}
}

func (i *Item2D) ID() string      { return i.id }
func (i *Item2D) Volume() float64 { return i.W * i.H }
func (i *Item2D) Dimensions() int { return 2 }

// WithScalar attaches a named scalar value to the item and returns the item
// for chaining: d2.NewItem("box", 30, 40, false).WithScalar("weight", 2.5)
func (i *Item2D) WithScalar(name string, value float64) *Item2D {
	if i.scalars == nil {
		i.scalars = make(map[string]float64)
	}
	i.scalars[name] = value
	return i
}

// Scalars returns a snapshot of all named scalar values on this item.
func (i *Item2D) Scalars() map[string]float64 {
	out := make(map[string]float64, len(i.scalars))
	for k, v := range i.scalars {
		out[k] = v
	}
	return out
}

var _ pack.Item = (*Item2D)(nil)
var _ pack.Scalar = (*Item2D)(nil)
