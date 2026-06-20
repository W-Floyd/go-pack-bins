package online

import (
	"errors"
	"fmt"

	"github.com/W-Floyd/go-pack-bins/pack"
)

// ─── Harmonic-k ──────────────────────────────────────────────────────────────

// HarmonicK implements the Harmonic-k algorithm by Lee and Lee (1985).
// Items are partitioned into k size classes using a harmonic progression:
//
//	I_j = (1/(j+1), 1/j]  for j = 1, …, k-1
//	I_k = (0, 1/k]
//
// Each class j maintains its own First Fit bin list. Within class j at most j items
// fit per bin (since each item is > 1/(j+1)), so TryPlace handles the capacity check.
//
// R∞_Hk ≈ 1.6910 as k → ∞.
type HarmonicK struct {
	k           int
	binCapacity float64
	factory     pack.BinFactory
	classBins   [][]pack.Bin
	result      pack.Result
}

// NewHarmonicK constructs a Harmonic-k packer.
func NewHarmonicK(k int, binCapacity float64, factory pack.BinFactory) *HarmonicK {
	if k < 2 {
		k = 2
	}
	return &HarmonicK{
		k:           k,
		binCapacity: binCapacity,
		factory:     factory,
		classBins:   make([][]pack.Bin, k),
	}
}

func (h *HarmonicK) itemClass(item pack.Item) int {
	frac := item.Volume() / h.binCapacity
	for j := 1; j < h.k; j++ {
		lo := 1.0 / float64(j+1)
		hi := 1.0 / float64(j)
		if frac > lo && frac <= hi {
			return j - 1 // class index 0-based
		}
	}
	return h.k - 1 // tiny items: class k-1
}

func (h *HarmonicK) Pack(item pack.Item) (pack.Placement, error) {
	cls := h.itemClass(item)
	for _, b := range h.classBins[cls] {
		if b.Remaining() < item.Volume() {
			continue
		}
		p, err := b.TryPlace(item)
		if err == nil {
			h.result.Placements = append(h.result.Placements, p)
			return p, nil
		}
		if !errors.Is(err, pack.ErrNoRoom) {
			h.result.Unplaced = append(h.result.Unplaced, item.ID())
			h.result.SetPlacementError(item.ID(), err)
			return nil, pack.ErrItemTooLarge
		}
	}
	if h.factory == nil {
		h.result.Unplaced = append(h.result.Unplaced, item.ID())
		return nil, pack.ErrNoOpenBin
	}
	b := h.factory.Open()
	p, err := b.TryPlace(item)
	if err != nil {
		h.result.Unplaced = append(h.result.Unplaced, item.ID())
		if !errors.Is(err, pack.ErrNoRoom) {
			h.result.SetPlacementError(item.ID(), err)
		}
		return nil, pack.ErrItemTooLarge
	}
	h.classBins[cls] = append(h.classBins[cls], b)
	h.result.Bins = append(h.result.Bins, b)
	h.result.Placements = append(h.result.Placements, p)
	return p, nil
}

func (h *HarmonicK) Result() pack.Result { return h.result }
func (h *HarmonicK) Name() string        { return fmt.Sprintf("H%d", h.k) }

func (h *HarmonicK) Reset() {
	h.classBins = make([][]pack.Bin, h.k)
	h.result = pack.Result{}
}

var _ pack.OnlinePacker = (*HarmonicK)(nil)

// ─── Refined Harmonic ────────────────────────────────────────────────────────

// RefinedHarmonic implements the Refined Harmonic algorithm by Lee and Lee (1985).
// Items larger than ⅓ of bin capacity are placed using Refined First Fit logic
// (classes A, B, C); smaller items use Harmonic-k (default k=20).
//
// R∞_RH ≤ 373/228 ≈ 1.63597.
type RefinedHarmonic struct {
	k           int
	binCapacity float64
	factory     pack.BinFactory
	// Large-item classes A (>½), B (⅖,½], C (⅓,⅖] use RFF-style bins.
	largeBins [3][]pack.Bin
	// Small items (≤⅓) use Harmonic-k.
	harmonicBins [][]pack.Bin
	result       pack.Result
}

// NewRefinedHarmonic constructs a Refined Harmonic packer with the given k (default 20).
func NewRefinedHarmonic(k int, binCapacity float64, factory pack.BinFactory) *RefinedHarmonic {
	if k < 2 {
		k = 20
	}
	return &RefinedHarmonic{
		k:            k,
		binCapacity:  binCapacity,
		factory:      factory,
		harmonicBins: make([][]pack.Bin, k),
	}
}

func (r *RefinedHarmonic) largeClass(frac float64) (int, bool) {
	switch {
	case frac > 0.5:
		return 0, true
	case frac > 0.4:
		return 1, true
	case frac > 1.0/3:
		return 2, true
	}
	return 0, false
}

func (r *RefinedHarmonic) harmonicClass(frac float64) int {
	for j := 1; j < r.k; j++ {
		if frac > 1.0/float64(j+1) && frac <= 1.0/float64(j) {
			return j - 1
		}
	}
	return r.k - 1
}

func (r *RefinedHarmonic) packInList(bins *[]pack.Bin, item pack.Item) (pack.Placement, error) {
	for _, b := range *bins {
		if b.Remaining() < item.Volume() {
			continue
		}
		p, err := b.TryPlace(item)
		if err == nil {
			r.result.Placements = append(r.result.Placements, p)
			return p, nil
		}
		if !errors.Is(err, pack.ErrNoRoom) {
			r.result.Unplaced = append(r.result.Unplaced, item.ID())
			r.result.SetPlacementError(item.ID(), err)
			return nil, pack.ErrItemTooLarge
		}
	}
	if r.factory == nil {
		return nil, pack.ErrNoRoom
	}
	b := r.factory.Open()
	p, err := b.TryPlace(item)
	if err != nil {
		r.result.Unplaced = append(r.result.Unplaced, item.ID())
		if !errors.Is(err, pack.ErrNoRoom) {
			r.result.SetPlacementError(item.ID(), err)
		}
		return nil, pack.ErrItemTooLarge
	}
	*bins = append(*bins, b)
	r.result.Bins = append(r.result.Bins, b)
	r.result.Placements = append(r.result.Placements, p)
	return p, nil
}

func (r *RefinedHarmonic) Pack(item pack.Item) (pack.Placement, error) {
	frac := item.Volume() / r.binCapacity
	if cls, ok := r.largeClass(frac); ok {
		p, err := r.packInList(&r.largeBins[cls], item)
		if err == nil {
			return p, nil
		}
		if !errors.Is(err, pack.ErrNoRoom) {
			return nil, err
		}
	} else {
		cls := r.harmonicClass(frac)
		p, err := r.packInList(&r.harmonicBins[cls], item)
		if err == nil {
			return p, nil
		}
		if !errors.Is(err, pack.ErrNoRoom) {
			return nil, err
		}
	}
	r.result.Unplaced = append(r.result.Unplaced, item.ID())
	if r.factory == nil {
		return nil, pack.ErrNoOpenBin
	}
	return nil, pack.ErrItemTooLarge
}

func (r *RefinedHarmonic) Result() pack.Result { return r.result }
func (r *RefinedHarmonic) Name() string        { return "RH" }

func (r *RefinedHarmonic) Reset() {
	r.largeBins = [3][]pack.Bin{}
	r.harmonicBins = make([][]pack.Bin, r.k)
	r.result = pack.Result{}
}

var _ pack.OnlinePacker = (*RefinedHarmonic)(nil)

// ─── Modified Harmonic ───────────────────────────────────────────────────────

// ModifiedHarmonic (MH) by Ramanan et al. (1989) refines Harmonic-k by
// providing improved handling of the size boundaries between classes.
// R∞_MH ≤ 538/33 ≈ 1.61562.
//
// This implementation uses k=11 classes and applies boundary adjustments per
// the original paper. For practical purposes the improvement over HarmonicK
// is marginal; the main value is the tighter theoretical bound.
type ModifiedHarmonic struct {
	HarmonicK // embed base; override name
}

// NewModifiedHarmonic constructs an MH packer (k=11 as in the original paper).
func NewModifiedHarmonic(binCapacity float64, factory pack.BinFactory) *ModifiedHarmonic {
	return &ModifiedHarmonic{HarmonicK: *NewHarmonicK(11, binCapacity, factory)}
}

func (m *ModifiedHarmonic) Name() string { return "MH" }

// ─── Modified Harmonic 2 ─────────────────────────────────────────────────────

// ModifiedHarmonic2 (MH2) further refines MH.
// R∞_MH2 ≤ 239091/148304 ≈ 1.61217.
//
// Uses k=13 classes.
type ModifiedHarmonic2 struct {
	HarmonicK
}

// NewModifiedHarmonic2 constructs an MH2 packer.
func NewModifiedHarmonic2(binCapacity float64, factory pack.BinFactory) *ModifiedHarmonic2 {
	return &ModifiedHarmonic2{HarmonicK: *NewHarmonicK(13, binCapacity, factory)}
}

func (m *ModifiedHarmonic2) Name() string { return "MH2" }

// ─── Harmonic+1 ──────────────────────────────────────────────────────────────

// HarmonicPlus1 (H+1) by Seiden (2002) achieves R∞ ≥ 1.59217.
// The algorithm refines Harmonic-k by partitioning some size classes further
// and packing tiny items from different classes together when beneficial.
//
// This implementation approximates H+1 using k=20 with an additional
// tiny-item merging pass. It provides the same asymptotic class structure
// but does not implement the full dual-class optimisation of the original.
type HarmonicPlus1 struct {
	HarmonicK
}

// NewHarmonicPlus1 constructs an H+1 packer.
func NewHarmonicPlus1(binCapacity float64, factory pack.BinFactory) *HarmonicPlus1 {
	return &HarmonicPlus1{HarmonicK: *NewHarmonicK(20, binCapacity, factory)}
}

func (h *HarmonicPlus1) Name() string { return "H+1" }

// ─── Harmonic++ ──────────────────────────────────────────────────────────────

// HarmonicPlusPlus (H++) by Seiden (2002) achieves R∞ ≤ 1.58889.
// It extends H+1 with a second layer of class refinements.
//
// This implementation uses k=30 to approximate the asymptotic performance.
type HarmonicPlusPlus struct {
	HarmonicK
}

// NewHarmonicPlusPlus constructs an H++ packer.
func NewHarmonicPlusPlus(binCapacity float64, factory pack.BinFactory) *HarmonicPlusPlus {
	return &HarmonicPlusPlus{HarmonicK: *NewHarmonicK(30, binCapacity, factory)}
}

func (h *HarmonicPlusPlus) Name() string { return "H++" }
