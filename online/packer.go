// Package online provides all online bin-packing algorithms.
// Each algorithm wraps a shared Packer engine with a BinSelector implementation.
// The Packer drives the common loop: ask the selector which bin to use,
// fall back to opening a new bin if the selector returns -1.
package online

import (
	"errors"

	"github.com/W-Floyd/go-pack-bins/pack"
)

// Packer is the shared engine for online bin-packing algorithms.
// The algorithm-specific behaviour lives entirely in the BinSelector.
type Packer struct {
	selector pack.BinSelector
	factory  pack.BinFactory
	name     string
	bins     []pack.Bin
	result   pack.Result
}

// NewPacker constructs a Packer with the given selector, factory, and name.
func NewPacker(name string, selector pack.BinSelector, factory pack.BinFactory) *Packer {
	return &Packer{name: name, selector: selector, factory: factory}
}

// Pack places item using the selector's policy.
func (p *Packer) Pack(item pack.Item) (pack.Placement, error) {
	placement, idx, selErr := p.selector.Select(p.bins, item)
	if selErr != nil {
		p.result.Unplaced = append(p.result.Unplaced, item.ID())
		p.result.SetPlacementError(item.ID(), selErr)
		return nil, pack.ErrItemTooLarge
	}
	if idx >= 0 {
		p.result.Placements = append(p.result.Placements, placement)
		return placement, nil
	}

	// No existing bin accepted the item — open a new one.
	if p.factory == nil {
		p.result.Unplaced = append(p.result.Unplaced, item.ID())
		return nil, pack.ErrNoOpenBin
	}
	newBin := p.factory.Open()
	placement, err := newBin.TryPlace(item)
	if err != nil && !errors.Is(err, pack.ErrNoRoom) {
		// Permanent error — item can never fit in any bin of this config.
		p.result.Unplaced = append(p.result.Unplaced, item.ID())
		p.result.SetPlacementError(item.ID(), err)
		return nil, pack.ErrItemTooLarge
	}
	if errors.Is(err, pack.ErrNoRoom) {
		p.result.Unplaced = append(p.result.Unplaced, item.ID())
		return nil, pack.ErrItemTooLarge
	}
	p.bins = append(p.bins, newBin)
	p.result.Bins = append(p.result.Bins, newBin)
	p.result.Placements = append(p.result.Placements, placement)
	return placement, nil
}

func (p *Packer) Result() pack.Result { return p.result }
func (p *Packer) Name() string        { return p.name }

func (p *Packer) Reset() {
	p.bins = nil
	p.result = pack.Result{}
}

var _ pack.OnlinePacker = (*Packer)(nil)
