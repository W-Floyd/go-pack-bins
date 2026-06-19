// Package d2 provides 2-D rectangular items and bins with configurable placement strategies.
package d2

import "github.com/wfloyd/go-pack-bins/pack"

// Item2D is a 2-D rectangular item with optional rotation support.
type Item2D struct {
	id          string
	W, H        float64 // natural orientation dimensions
	AllowRotate bool    // allow 90° rotation during placement
}

// NewItem creates a rectangular item.
func NewItem(id string, w, h float64, allowRotate bool) *Item2D {
	return &Item2D{id: id, W: w, H: h, AllowRotate: allowRotate}
}

func (i *Item2D) ID() string      { return i.id }
func (i *Item2D) Volume() float64 { return i.W * i.H }
func (i *Item2D) Dimensions() int { return 2 }

var _ pack.Item = (*Item2D)(nil)
