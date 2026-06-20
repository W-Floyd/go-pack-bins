// Package d1 provides 1-D items and bins for classical bin packing.
package d1

import "github.com/W-Floyd/go-pack-bins/pack"

// Item1D is a one-dimensional item characterised solely by its size.
type Item1D struct {
	id      string
	size    float64
	scalars map[string]float64
}

// NewItem creates an Item1D with the given identifier and size.
// Size must be > 0.
func NewItem(id string, size float64) *Item1D {
	return &Item1D{id: id, size: size}
}

func (i *Item1D) ID() string      { return i.id }
func (i *Item1D) Volume() float64 { return i.size }
func (i *Item1D) Dimensions() int { return 1 }
func (i *Item1D) Size() float64   { return i.size }

// WithScalar attaches a named scalar value to the item and returns the item
// for chaining: d1.NewItem("box", 3).WithScalar("weight", 5.2)
func (i *Item1D) WithScalar(name string, value float64) *Item1D {
	if i.scalars == nil {
		i.scalars = make(map[string]float64)
	}
	i.scalars[name] = value
	return i
}

// Scalars returns a snapshot of all named scalar values on this item.
func (i *Item1D) Scalars() map[string]float64 {
	out := make(map[string]float64, len(i.scalars))
	for k, v := range i.scalars {
		out[k] = v
	}
	return out
}

var _ pack.Item = (*Item1D)(nil)
var _ pack.Scalar = (*Item1D)(nil)
