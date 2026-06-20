package d2

import (
	"fmt"
	"sync/atomic"

	"github.com/W-Floyd/go-pack-bins/pack"
)

// Bin2D is a rectangular 2-D bin that delegates placement to a strategy.
type Bin2D struct {
	id       string
	W, H     float64
	strategy PlacementStrategy2D
	items    []pack.Item
}

// NewBin creates a Bin2D with the given strategy.
// Use maxrects.New(w, h) for the default MaxRects strategy.
func NewBin(id string, w, h float64, strategy PlacementStrategy2D) *Bin2D {
	return &Bin2D{id: id, W: w, H: h, strategy: strategy}
}

func (b *Bin2D) ID() string      { return b.id }
func (b *Bin2D) Dimensions() int { return 2 }

func (b *Bin2D) TryPlace(item pack.Item) (pack.Placement, error) {
	i2, ok := item.(*Item2D)
	if !ok {
		return nil, pack.ErrNoRoom
	}
	// Pre-check: if item dims don't fit in bin dims in any orientation, it's permanent.
	fitsNatural := i2.W <= b.W && i2.H <= b.H
	fitsRotated := i2.AllowRotate && i2.H <= b.W && i2.W <= b.H
	if !fitsNatural && !fitsRotated {
		return nil, pack.ErrItemTooLarge
	}
	x, y, rotated, placed := b.strategy.TryInsert(i2.W, i2.H, i2.AllowRotate)
	if !placed {
		return nil, pack.ErrNoRoom
	}
	w, h := i2.W, i2.H
	if rotated {
		w, h = i2.H, i2.W
	}
	p := &Placement2D{
		binID:   b.id,
		itemID:  item.ID(),
		X:       x,
		Y:       y,
		W:       w,
		H:       h,
		Rotated: rotated,
	}
	b.items = append(b.items, item)
	return p, nil
}

func (b *Bin2D) Utilization() float64     { return b.strategy.Utilization() }
func (b *Bin2D) Remaining() float64       { return b.strategy.Remaining() }
func (b *Bin2D) Items() []pack.Item       { return b.items }
func (b *Bin2D) Strategy() PlacementStrategy2D { return b.strategy }

var _ pack.Bin = (*Bin2D)(nil)

// Factory2D creates Bin2D instances using a strategy constructor function.
type Factory2D struct {
	w, h        float64
	makeStrategy func(w, h float64) PlacementStrategy2D
	counter     atomic.Int64
}

// NewFactory returns a factory that creates w×h bins using the given strategy constructor.
func NewFactory(w, h float64, makeStrategy func(w, h float64) PlacementStrategy2D) *Factory2D {
	return &Factory2D{w: w, h: h, makeStrategy: makeStrategy}
}

func (f *Factory2D) Open() pack.Bin {
	n := f.counter.Add(1)
	id := fmt.Sprintf("bin2d-%d", n)
	return NewBin(id, f.w, f.h, f.makeStrategy(f.w, f.h))
}

var _ pack.BinFactory = (*Factory2D)(nil)
