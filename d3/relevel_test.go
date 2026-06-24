package d3

import (
	"fmt"
	"testing"

	"github.com/W-Floyd/go-pack-bins/pack"
)

// surfaceStats reconstructs the top surface of bin 0 over a unit grid and returns
// its peak and the total empty space beneath that peak (the flatness measure —
// lower is flatter).
func surfaceStats(pr pack.Result, w, d int) (peak, gap float64) {
	hm := make([][]float64, w)
	for i := range hm {
		hm[i] = make([]float64, d)
	}
	for _, p := range pr.Placements {
		p3, ok := p.(*Placement3D)
		if !ok {
			continue
		}
		if t := p3.Z + p3.H; t > peak {
			peak = t
		}
		for x := int(p3.X); x < int(p3.X+p3.W) && x < w; x++ {
			for y := int(p3.Y); y < int(p3.Y+p3.D) && y < d; y++ {
				if p3.Z+p3.H > hm[x][y] {
					hm[x][y] = p3.Z + p3.H
				}
			}
		}
	}
	for x := 0; x < w; x++ {
		for y := 0; y < d; y++ {
			gap += peak - hm[x][y]
		}
	}
	return peak, gap
}

// hasOverlap reports whether any two placements intersect in 3-D.
func hasOverlap(pr pack.Result) bool {
	var bs []*Placement3D
	for _, p := range pr.Placements {
		if p3, ok := p.(*Placement3D); ok {
			bs = append(bs, p3)
		}
	}
	const e = 1e-6
	for i := range bs {
		for j := i + 1; j < len(bs); j++ {
			a, b := bs[i], bs[j]
			if a.X < b.X+b.W-e && b.X < a.X+a.W-e &&
				a.Y < b.Y+b.D-e && b.Y < a.Y+a.D-e &&
				a.Z < b.Z+b.H-e && b.Z < a.Z+a.H-e {
				return true
			}
		}
	}
	return false
}

// TestRelevelTop_FlatDenseBin guards the top-region re-levelling finishing pass on
// a dense single-bin instance like the stress demo: 10k mixed 1–6 boxes in a 75³
// bin. The block body packs this so densely the one-flat-layer endgame never fires,
// yet its waste-free layers leave a standing-tall top. The finishing pass must
// level that — keeping the surface valid (no overlap, all placed) while hugging a
// peak close to the volume lower bound, with little empty space beneath it.
func TestRelevelTop_FlatDenseBin(t *testing.T) {
	next := newLCG(99)
	sz := []float64{1, 2, 2, 3, 3, 4, 4, 5, 6}
	pick := func() float64 { return sz[int(next()*9)%9] }
	var items []pack.Item
	vol := 0.0
	for i := 0; i < 10000; i++ {
		w, d, h := pick(), pick(), pick()
		items = append(items, NewItem(fmt.Sprintf("i%d", i), w, d, h, true))
		vol += w * d * h
	}
	pr, err := NewBlockPacker(75, 75, 75).PackAll(items)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	if len(pr.Unplaced) != 0 {
		t.Fatalf("unplaced=%d, want 0", len(pr.Unplaced))
	}
	if hasOverlap(pr) {
		t.Fatal("re-levelled pack has overlapping boxes")
	}
	peak, gap := surfaceStats(pr, 75, 75)
	volBound := vol / (75 * 75) // perfectly dense flat height
	// The top must hug a low peak: within ~1.1× the volume bound, and the empty
	// space beneath the peak small relative to a bin layer (5625 per unit).
	if peak > volBound*1.1+1 {
		t.Errorf("peak %.1f too tall vs volume bound %.1f — top not levelled", peak, volBound)
	}
	if gap > 3*5625 {
		t.Errorf("gap-below-peak %.0f too large — surface still ragged", gap)
	}
}

// newLCG mirrors packapi's benchMix generator so the test exercises the same item
// distribution the demo's stress preset uses.
func newLCG(seed uint32) func() float64 {
	s := seed
	if s == 0 {
		s = 1
	}
	return func() float64 { s = s*1664525 + 1013904223; return float64(s>>8) / (1 << 24) }
}
