package online

import "github.com/wfloyd/go-pack-bins/pack"

// RefinedFirstFit implements the Refined First Fit (RFF) algorithm by Yao (1980).
// Items are partitioned into 4 size classes based on their size relative to bin capacity:
//
//	Class A: size ∈ (½, 1]
//	Class B: size ∈ (⅖, ½]
//	Class C: size ∈ (⅓, ⅖]
//	Class D: size ∈ (0, ⅓]
//
// Within each class, First Fit is applied to class-specific bins.
// This is NOT an AnyFit algorithm — it may open a new bin even when the item
// fits in a bin of a different class.
//
// R_RFF ≤ (5/3)·OPT + 5.
type RefinedFirstFit struct {
	factory     pack.BinFactory
	binCapacity float64
	classBins   [4][]pack.Bin
	result      pack.Result
}

// NewRFF constructs an RFF packer.
// binCapacity is the capacity of each bin, used for item size classification.
func NewRFF(binCapacity float64, factory pack.BinFactory) *RefinedFirstFit {
	return &RefinedFirstFit{
		factory:     factory,
		binCapacity: binCapacity,
	}
}

func (r *RefinedFirstFit) itemClass(item pack.Item) int {
	frac := item.Volume() / r.binCapacity
	switch {
	case frac > 0.5:
		return 0 // A: (½, 1]
	case frac > 0.4:
		return 1 // B: (⅖, ½]
	case frac > 1.0/3:
		return 2 // C: (⅓, ⅖]
	default:
		return 3 // D: (0, ⅓]
	}
}

func (r *RefinedFirstFit) Pack(item pack.Item) (pack.Placement, error) {
	cls := r.itemClass(item)
	// First Fit within this class.
	for _, b := range r.classBins[cls] {
		if b.Remaining() < item.Volume() {
			continue
		}
		if p, ok := b.TryPlace(item); ok {
			r.result.Placements = append(r.result.Placements, p)
			return p, nil
		}
	}
	// No bin in this class can accept the item — open a new one.
	if r.factory == nil {
		r.result.Unplaced = append(r.result.Unplaced, item.ID())
		return nil, pack.ErrNoOpenBin
	}
	b := r.factory.Open()
	p, ok := b.TryPlace(item)
	if !ok {
		r.result.Unplaced = append(r.result.Unplaced, item.ID())
		return nil, pack.ErrItemTooLarge
	}
	r.classBins[cls] = append(r.classBins[cls], b)
	r.result.Bins = append(r.result.Bins, b)
	r.result.Placements = append(r.result.Placements, p)
	return p, nil
}

func (r *RefinedFirstFit) Result() pack.Result { return r.result }
func (r *RefinedFirstFit) Name() string        { return "RFF" }

func (r *RefinedFirstFit) Reset() {
	r.classBins = [4][]pack.Bin{}
	r.result = pack.Result{}
}

var _ pack.OnlinePacker = (*RefinedFirstFit)(nil)
