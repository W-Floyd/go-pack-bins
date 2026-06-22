package d3_test

import (
	"testing"

	"github.com/W-Floyd/go-pack-bins/d3"
)

func TestLayerStack_FirstAtOriginNoOverlap(t *testing.T) {
	dims := [][3]float64{{4, 4, 3}, {6, 3, 2}, {3, 3, 5}, {5, 5, 2}, {2, 6, 4}, {4, 4, 4}, {3, 7, 3}}
	ps := place3(t, d3.NewLayerStackStrategy(10, 10, 10), 10, 10, 10, true, dims)
	if len(ps) == 0 {
		t.Fatal("nothing placed")
	}
	if ps[0].X != 0 || ps[0].Y != 0 || ps[0].Z != 0 {
		t.Errorf("first item at (%v,%v,%v), want origin", ps[0].X, ps[0].Y, ps[0].Z)
	}
	assertNoOverlap(t, ps, 10, 10, 10)
}

func TestLayerStack_LaysItemsFlat(t *testing.T) {
	// A rotatable 2×8×8 slab must be laid flat: its smallest dimension (2) becomes
	// the height, leaving an 8×8 footprint at z=0.
	ps := place3(t, d3.NewLayerStackStrategy(10, 10, 10), 10, 10, 10, true, [][3]float64{{2, 8, 8}})
	if len(ps) != 1 {
		t.Fatalf("placed %d, want 1", len(ps))
	}
	if ps[0].H != 2 {
		t.Errorf("laid-flat height = %v, want 2 (smallest dimension vertical)", ps[0].H)
	}
}

func TestLayerStack_StacksLayersInZ(t *testing.T) {
	// Four 5×5×2 slabs (footprint 5×5, height 2) tile a 10×10 floor as one layer
	// at z=0; the next four start a second layer at z=2.
	dims := make([][3]float64, 8)
	for i := range dims {
		dims[i] = [3]float64{5, 5, 2}
	}
	strat := d3.NewLayerStackStrategy(10, 10, 10)
	ps := place3(t, strat, 10, 10, 10, false, dims)
	if len(ps) != 8 {
		t.Fatalf("placed %d, want 8", len(ps))
	}
	assertNoOverlap(t, ps, 10, 10, 10)
	var z0, z2 int
	for _, p := range ps {
		switch p.Z {
		case 0:
			z0++
		case 2:
			z2++
		default:
			t.Errorf("box %s at unexpected z=%v (layers should sit at 0 and 2)", p.ItemID(), p.Z)
		}
	}
	if z0 != 4 || z2 != 4 {
		t.Errorf("layer occupancy z0=%d z2=%d, want 4 and 4", z0, z2)
	}
}

func TestLayerStack_PacksPerfectGridNoWaste(t *testing.T) {
	dims := make([][3]float64, 8)
	for i := range dims {
		dims[i] = [3]float64{5, 5, 5}
	}
	strat := d3.NewLayerStack(10, 10, 10)
	ps := place3(t, strat, 10, 10, 10, false, dims)
	if len(ps) != 8 {
		t.Fatalf("placed %d, want 8", len(ps))
	}
	assertNoOverlap(t, ps, 10, 10, 10)
	if u := strat.Utilization(); u < 0.999 {
		t.Errorf("utilization %.3f, want ~1.0 (perfect tiling)", u)
	}
}
