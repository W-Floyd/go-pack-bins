// Package strip implements strip packing: unlike bin packing (fixed-size bins,
// minimise the count) the container's base dimensions are fixed and the remaining
// "open" dimension is minimised. In 2-D the width is fixed and the used height is
// minimised (roll/coil/fabric/paper cutting, PCB panelisation); in 3-D the base
// footprint is fixed and the used height is minimised (a 3-D-printer bed, a
// fixed-base pallet, a truck of fixed cross-section whose length is minimised).
//
// The packers reuse the existing d2 / d3 within-bin placement strategies: items
// are packed into a single container made tall enough to hold them all, and the
// achieved extent is read back from the placements. Several strategies are tried
// and the one yielding the smallest extent is returned.
//
// Attribution: strip packing is classical (Baker, Coffman & Rivest 1980; Coffman
// et al. NFDH/FFDH level algorithms). This package adds no new placement
// heuristic — it minimises extent over the library's existing strategies.
package strip

import (
	"math"
	"sort"

	"github.com/W-Floyd/go-pack-bins/d2"
	"github.com/W-Floyd/go-pack-bins/d3"
	"github.com/W-Floyd/go-pack-bins/pack"
)

// Result2D is a 2-D strip packing: the placements plus the achieved height and a
// lower bound on it.
type Result2D struct {
	pack.Result
	// Width is the fixed strip width.
	Width float64
	// Height is the minimal achieved used height (the strip extent): the largest
	// top edge over all placed items. 0 when nothing was placed.
	Height float64
	// LowerBound is the continuous height lower bound Σarea / Width — no packing
	// of these items into this width can be shorter. Height == LowerBound means
	// the packing is provably area-optimal (perfectly tight).
	LowerBound float64
	// Strategy names the placement strategy that achieved Height.
	Strategy string
}

// Result3D is a 3-D strip packing: the placements plus the achieved height and a
// lower bound on it.
type Result3D struct {
	pack.Result
	// BaseW, BaseD are the fixed base footprint dimensions.
	BaseW, BaseD float64
	// Height is the minimal achieved used height (the largest top edge over all
	// placed boxes). 0 when nothing was placed.
	Height float64
	// LowerBound is the continuous height lower bound Σvolume / (BaseW·BaseD).
	LowerBound float64
	// Strategy names the placement strategy that achieved Height.
	Strategy string
}

// strat2D pairs a named 2-D strategy constructor with the item ordering it packs
// best under.
type strat2D struct {
	name string
	make func(w, h float64) d2.PlacementStrategy2D
	// byHeight sorts items by decreasing height (for the shelf/level algorithms);
	// otherwise items are sorted by decreasing area.
	byHeight bool
}

// Pack2D packs items into a strip of the given fixed width, minimising the used
// height. It tries the shelf (FFDH/BFDH), skyline and MaxRects strategies and
// returns the packing with the smallest height. Items keep their AllowRotate
// flag. An item wider than the strip in every orientation cannot be placed and is
// reported in Unplaced.
func Pack2D(items []*d2.Item2D, width float64) Result2D {
	strats := []strat2D{
		{"ffdh", d2.NewShelfStrategy(d2.ShelfFirstFit), true},
		{"bfdh", d2.NewShelfStrategy(d2.ShelfBestFit), true},
		{"skyline", d2.NewSkylineDefault, false},
		{"maxrects", d2.NewMaxRectsDefault, false},
	}

	// A height tall enough that every item fits in a single strip regardless of
	// strategy or rotation: stack them all in one column, worst orientation.
	hUB := 0.0
	for _, it := range items {
		hUB += math.Max(it.W, it.H)
	}
	hUB = math.Max(hUB, 1)

	best := Result2D{Strategy: ""}
	first := true
	for _, st := range strats {
		r := packStrip2D(items, width, hUB, st)
		// Prefer the result that places the most items; break ties by smaller
		// height. A strategy that drops items can have a smaller extent, so height
		// alone must not decide.
		if first || better2D(r, best) {
			best, first = r, false
		}
	}
	best.Width = width
	best.LowerBound = stripLowerBound2D(items, width)
	return best
}

// better2D reports whether candidate c is a better strip result than the current
// best: fewer unplaced items wins; ties go to the smaller height.
func better2D(c, best Result2D) bool {
	if len(c.Unplaced) != len(best.Unplaced) {
		return len(c.Unplaced) < len(best.Unplaced)
	}
	return c.Height < best.Height
}

func packStrip2D(items []*d2.Item2D, width, hUB float64, st strat2D) Result2D {
	order := make([]*d2.Item2D, len(items))
	copy(order, items)
	if st.byHeight {
		sort.SliceStable(order, func(i, j int) bool { return order[i].H > order[j].H })
	} else {
		sort.SliceStable(order, func(i, j int) bool { return order[i].Volume() > order[j].Volume() })
	}

	bin := d2.NewBin("strip2d", width, hUB, st.make(width, hUB))
	r := Result2D{Strategy: st.name}
	r.Bins = []pack.Bin{bin}
	height := 0.0
	// Placements must align to the original item order (pack.Result contract).
	pByID := make(map[string]pack.Placement, len(order))
	for _, it := range order {
		p, err := bin.TryPlace(it)
		if err != nil {
			r.SetPlacementError(it.ID(), err)
			continue
		}
		pByID[it.ID()] = p
		if p2, ok := p.(*d2.Placement2D); ok {
			if top := p2.Y + p2.H; top > height {
				height = top
			}
		}
	}
	r.Placements = make([]pack.Placement, len(items))
	for i, it := range items {
		if p, ok := pByID[it.ID()]; ok {
			r.Placements[i] = p
		} else {
			r.Unplaced = append(r.Unplaced, it.ID())
		}
	}
	r.Height = height
	return r
}

func stripLowerBound2D(items []*d2.Item2D, width float64) float64 {
	if width <= 0 {
		return 0
	}
	area := 0.0
	for _, it := range items {
		area += it.W * it.H
	}
	return area / width
}

// strat3D pairs a named 3-D strategy constructor with its label.
type strat3D struct {
	name string
	make func(w, d, h float64) d3.PlacementStrategy3D
}

// Pack3D packs items into a strip with the given fixed base footprint (baseW ×
// baseD), minimising the used height. It tries the extreme-point and
// bottom-left-fill strategies and returns the packing with the smallest height.
// An item that cannot fit the base in any orientation is reported in Unplaced.
func Pack3D(items []*d3.Item3D, baseW, baseD float64) Result3D {
	strats := []strat3D{
		{"extreme-point", d3.NewExtremePointStrategy},
		{"blf", d3.NewBottomLeftFillStrategy},
	}

	// Single-column ceiling: tall enough to hold any packing in one bin.
	hSum, maxDim := 0.0, 0.0
	for _, it := range items {
		m := math.Max(it.W, math.Max(it.D, it.H))
		hSum += m
		if m > maxDim {
			maxDim = m
		}
	}
	hSum = math.Max(hSum, 1)

	best := Result3D{Strategy: ""}
	first := true
	consider := func(r Result3D) {
		if first || better3D(r, best) {
			best, first = r, false
		}
	}

	// Block-building fuses items into dense flat layers and, since the packer now
	// detects its own last layer, packs a single tall container densely — no
	// segmenting into bounded bins. It always places everything, so its height is a
	// tight, feasible ceiling for the per-item strategies below: a loose
	// sum-of-heights ceiling would coarsen their spatial grid and slow them at scale.
	blockRes := packStrip3DOffline(items, "blocks", d3.NewBlockPacker(baseW, baseD, hSum))
	consider(blockRes)

	hUB := hSum
	if len(blockRes.Unplaced) == 0 && blockRes.Height > 0 {
		hUB = blockRes.Height*1.5 + maxDim // generous slack, far below the single-column sum
	}
	for _, st := range strats {
		consider(packStrip3D(items, baseW, baseD, hUB, st))
	}
	best.BaseW, best.BaseD = baseW, baseD
	best.LowerBound = stripLowerBound3D(items, baseW, baseD)
	return best
}

// better3D reports whether candidate c is a better strip result than best:
// fewer unplaced items wins; ties go to the smaller height.
func better3D(c, best Result3D) bool {
	if len(c.Unplaced) != len(best.Unplaced) {
		return len(c.Unplaced) < len(best.Unplaced)
	}
	return c.Height < best.Height
}

// packStrip3DOffline runs a whole-bin offline packer (block-building) into the tall
// container and measures the achieved height from its placements. With hUB tall
// enough to hold a single worst-orientation column, the packer fits everything in
// one bin, so the extent is the max top edge.
func packStrip3DOffline(items []*d3.Item3D, name string, packer pack.OfflinePacker) Result3D {
	in := make([]pack.Item, len(items))
	for i, it := range items {
		in[i] = it
	}
	pr, err := packer.PackAll(in)
	r := Result3D{Strategy: name}
	if err != nil {
		// Treat a packer error as "placed nothing" so best-of discards it.
		r.Unplaced = make([]string, len(items))
		for i, it := range items {
			r.Unplaced[i] = it.ID()
		}
		return r
	}
	r.Result = pr
	height := 0.0
	for _, p := range pr.Placements {
		if p3, ok := p.(*d3.Placement3D); ok {
			if top := p3.Z + p3.H; top > height {
				height = top
			}
		}
	}
	r.Height = height
	return r
}

func packStrip3D(items []*d3.Item3D, baseW, baseD, hUB float64, st strat3D) Result3D {
	order := make([]*d3.Item3D, len(items))
	copy(order, items)
	sort.SliceStable(order, func(i, j int) bool { return order[i].Volume() > order[j].Volume() })

	bin := d3.NewBin("strip3d", baseW, baseD, hUB, st.make(baseW, baseD, hUB))
	r := Result3D{Strategy: st.name}
	r.Bins = []pack.Bin{bin}
	height := 0.0
	pByID := make(map[string]pack.Placement, len(order))
	for _, it := range order {
		p, err := bin.TryPlace(it)
		if err != nil {
			r.SetPlacementError(it.ID(), err)
			continue
		}
		pByID[it.ID()] = p
		if p3, ok := p.(*d3.Placement3D); ok {
			if top := p3.Z + p3.H; top > height {
				height = top
			}
		}
	}
	r.Placements = make([]pack.Placement, len(items))
	for i, it := range items {
		if p, ok := pByID[it.ID()]; ok {
			r.Placements[i] = p
		} else {
			r.Unplaced = append(r.Unplaced, it.ID())
		}
	}
	r.Height = height
	return r
}

func stripLowerBound3D(items []*d3.Item3D, baseW, baseD float64) float64 {
	base := baseW * baseD
	if base <= 0 {
		return 0
	}
	vol := 0.0
	for _, it := range items {
		vol += it.W * it.D * it.H
	}
	return vol / base
}
