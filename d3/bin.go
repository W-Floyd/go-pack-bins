package d3

import (
	"fmt"
	"sync/atomic"

	"github.com/wfloyd/go-pack-bins/pack"
)

// Bin3D is a box-shaped 3-D bin that delegates placement to a strategy.
type Bin3D struct {
	id       string
	W, D, H  float64
	strategy PlacementStrategy3D
	items    []pack.Item
}

// NewBin creates a Bin3D with the given strategy.
// Use extremepoint.New(w, d, h) for the default extreme-point strategy.
func NewBin(id string, w, d, h float64, strategy PlacementStrategy3D) *Bin3D {
	return &Bin3D{id: id, W: w, D: d, H: h, strategy: strategy}
}

func (b *Bin3D) ID() string      { return b.id }
func (b *Bin3D) Dimensions() int { return 3 }

func (b *Bin3D) TryPlace(item pack.Item) (pack.Placement, error) {
	i3, ok := item.(*Item3D)
	if !ok {
		return nil, pack.ErrNoRoom
	}
	// Pre-check: if item doesn't fit in bin dimensions in any orientation, it's permanent.
	if !anyOrientationFits(i3.Orientations(), b.W, b.D, b.H) {
		return nil, pack.ErrItemTooLarge
	}
	x, y, z, w, d, h, placed := b.strategy.TryInsert(i3.Orientations())
	if !placed {
		return nil, pack.ErrNoRoom
	}
	p := &Placement3D{
		binID: b.id, itemID: item.ID(),
		X: x, Y: y, Z: z,
		W: w, D: d, H: h,
	}
	b.items = append(b.items, item)
	return p, nil
}

// anyOrientationFits returns true if any of the given (w,d,h) orientations fits
// within the box defined by w, d, h.
func anyOrientationFits(orientations [][3]float64, w, d, h float64) bool {
	for _, o := range orientations {
		if o[0] <= w && o[1] <= d && o[2] <= h {
			return true
		}
	}
	return false
}

func (b *Bin3D) Utilization() float64 { return b.strategy.Utilization() }
func (b *Bin3D) Remaining() float64   { return b.strategy.Remaining() }
func (b *Bin3D) Items() []pack.Item   { return b.items }

var _ pack.Bin = (*Bin3D)(nil)

// Factory3D creates Bin3D instances.
type Factory3D struct {
	w, d, h      float64
	makeStrategy func(w, d, h float64) PlacementStrategy3D
	counter      atomic.Int64
}

// NewFactory returns a factory that creates w×d×h bins using the given strategy constructor.
func NewFactory(w, d, h float64, makeStrategy func(w, d, h float64) PlacementStrategy3D) *Factory3D {
	return &Factory3D{w: w, d: d, h: h, makeStrategy: makeStrategy}
}

func (f *Factory3D) Open() pack.Bin {
	n := f.counter.Add(1)
	id := fmt.Sprintf("bin3d-%d", n)
	return NewBin(id, f.w, f.d, f.h, f.makeStrategy(f.w, f.d, f.h))
}

var _ pack.BinFactory = (*Factory3D)(nil)
