package d3_test

import (
	"testing"

	"github.com/W-Floyd/go-pack-bins/d3"
)

// place3 runs a sequence of item dims through a bin and returns the placements.
func place3(t *testing.T, strat d3.PlacementStrategy3D, bw, bd, bh float64, rotate bool, dims [][3]float64) []*d3.Placement3D {
	t.Helper()
	bin := d3.NewBin("b", bw, bd, bh, strat)
	var out []*d3.Placement3D
	for i, dm := range dims {
		p, err := bin.TryPlace(d3.NewItem("i"+string(rune('a'+i)), dm[0], dm[1], dm[2], rotate))
		if err != nil {
			continue
		}
		out = append(out, p.(*d3.Placement3D))
	}
	return out
}

// assertNoOverlap fails if any two placements share positive volume or escape the bin.
func assertNoOverlap(t *testing.T, ps []*d3.Placement3D, bw, bd, bh float64) {
	t.Helper()
	for _, p := range ps {
		if p.X < -1e-9 || p.Y < -1e-9 || p.Z < -1e-9 ||
			p.X+p.W > bw+1e-9 || p.Y+p.D > bd+1e-9 || p.Z+p.H > bh+1e-9 {
			t.Errorf("box %s at (%v,%v,%v) size (%v,%v,%v) escapes bin %v×%v×%v",
				p.ItemID(), p.X, p.Y, p.Z, p.W, p.D, p.H, bw, bd, bh)
		}
	}
	for i := 0; i < len(ps); i++ {
		for j := i + 1; j < len(ps); j++ {
			a, b := ps[i], ps[j]
			ox := minf(a.X+a.W, b.X+b.W) - maxf(a.X, b.X)
			oy := minf(a.Y+a.D, b.Y+b.D) - maxf(a.Y, b.Y)
			oz := minf(a.Z+a.H, b.Z+b.H) - maxf(a.Z, b.Z)
			if ox > 1e-9 && oy > 1e-9 && oz > 1e-9 {
				t.Errorf("boxes %s and %s overlap by %v×%v×%v", a.ItemID(), b.ItemID(), ox, oy, oz)
			}
		}
	}
}

func TestEMS_NoOverlapAndFirstAtOrigin(t *testing.T) {
	dims := [][3]float64{{4, 4, 3}, {6, 3, 2}, {3, 3, 5}, {5, 5, 2}, {2, 6, 4}, {4, 4, 4}, {3, 7, 3}}
	ps := place3(t, d3.NewEMSStrategy(10, 10, 10), 10, 10, 10, false, dims)
	if len(ps) == 0 {
		t.Fatal("nothing placed")
	}
	if ps[0].X != 0 || ps[0].Y != 0 || ps[0].Z != 0 {
		t.Errorf("first item at (%v,%v,%v), want origin", ps[0].X, ps[0].Y, ps[0].Z)
	}
	assertNoOverlap(t, ps, 10, 10, 10)
}

func TestEMS_FillsFloorBeforeStacking(t *testing.T) {
	dims := make([][3]float64, 16)
	for i := range dims {
		dims[i] = [3]float64{1, 1, 1}
	}
	ps := place3(t, d3.NewEMSStrategy(4, 4, 4), 4, 4, 4, false, dims)
	if len(ps) != 16 {
		t.Fatalf("placed %d, want 16 (a full floor layer)", len(ps))
	}
	for _, p := range ps {
		if p.Z != 0 {
			t.Errorf("box %s at z=%v stacked before the floor was full", p.ItemID(), p.Z)
		}
	}
	assertNoOverlap(t, ps, 4, 4, 4)
}

func TestEMS_PacksPerfectGridWithNoWaste(t *testing.T) {
	// Eight 5×5×5 cubes tile a 10×10×10 bin exactly: a correct EMS must place all
	// eight with full utilisation and no overlap.
	dims := make([][3]float64, 8)
	for i := range dims {
		dims[i] = [3]float64{5, 5, 5}
	}
	strat := d3.NewEMSStrategy(10, 10, 10)
	ps := place3(t, strat, 10, 10, 10, false, dims)
	if len(ps) != 8 {
		t.Fatalf("placed %d, want 8", len(ps))
	}
	assertNoOverlap(t, ps, 10, 10, 10)
	if u := strat.Utilization(); u < 0.999 {
		t.Errorf("utilization %.3f, want ~1.0 (perfect tiling)", u)
	}
}

func TestEMS_SupportGateRejectsFloating(t *testing.T) {
	// With a full-support gate, a small box cannot perch unsupported above a gap.
	strat := d3.NewEMSStrategyContact(d3.ContactSpec{Bottom: 1})(10, 10, 10)
	ps := place3(t, strat, 10, 10, 10, false, [][3]float64{{10, 10, 2}, {2, 2, 2}, {2, 2, 2}})
	assertNoOverlap(t, ps, 10, 10, 10)
	for _, p := range ps {
		if p.Z == 0 {
			continue
		}
		// every elevated box must be fully supported by the slab below
		if p.Z != 2 {
			t.Errorf("box %s at z=%v not resting on the base slab", p.ItemID(), p.Z)
		}
	}
}
