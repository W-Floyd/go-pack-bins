// Package d1 provides 1-D items and bins for classical bin packing.
package d1

import "github.com/wfloyd/go-pack-bins/pack"

// Item1D is a one-dimensional item characterised solely by its size.
type Item1D struct {
	id   string
	size float64
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

// Ensure interface satisfaction.
var _ pack.Item = (*Item1D)(nil)
