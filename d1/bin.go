package d1

import (
	"fmt"
	"sync/atomic"

	"github.com/wfloyd/go-pack-bins/pack"
)

// Bin1D is a one-dimensional bin with a fixed scalar capacity.
type Bin1D struct {
	id       string
	capacity float64
	used     float64
	items    []pack.Item
}

// Placement1D records a 1-D placement (no spatial information beyond the bin).
type Placement1D struct {
	binID  string
	itemID string
}

func (p *Placement1D) BinID() string  { return p.binID }
func (p *Placement1D) ItemID() string { return p.itemID }

var _ pack.Placement = (*Placement1D)(nil)

// NewBin creates a Bin1D with the given identifier and capacity.
func NewBin(id string, capacity float64) *Bin1D {
	return &Bin1D{id: id, capacity: capacity}
}

func (b *Bin1D) ID() string      { return b.id }
func (b *Bin1D) Dimensions() int { return 1 }

func (b *Bin1D) TryPlace(item pack.Item) (pack.Placement, bool) {
	if item.Volume() > b.capacity-b.used {
		return nil, false
	}
	b.used += item.Volume()
	b.items = append(b.items, item)
	return &Placement1D{binID: b.id, itemID: item.ID()}, true
}

func (b *Bin1D) Utilization() float64 {
	if b.capacity == 0 {
		return 1
	}
	return b.used / b.capacity
}

func (b *Bin1D) Remaining() float64 { return b.capacity - b.used }
func (b *Bin1D) Items() []pack.Item  { return b.items }

var _ pack.Bin = (*Bin1D)(nil)

// Factory1D is a BinFactory that produces Bin1D instances.
type Factory1D struct {
	capacity float64
	counter  atomic.Int64
}

// NewFactory returns a Factory1D that creates bins of the given capacity.
func NewFactory(capacity float64) *Factory1D {
	return &Factory1D{capacity: capacity}
}

func (f *Factory1D) Open() pack.Bin {
	n := f.counter.Add(1)
	return NewBin(fmt.Sprintf("bin-%d", n), f.capacity)
}

var _ pack.BinFactory = (*Factory1D)(nil)
