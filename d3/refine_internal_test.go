package d3

import (
	"context"
	"math"
	"testing"
)

// assertRefineValid checks a refined packing is physically valid: every item is
// inside the bin, no two in a bin overlap, and each rests on the floor or another
// item (nothing floats).
func assertRefineValid(t *testing.T, ps []*Placement3D, w, d, h float64) {
	t.Helper()
	const eps = 1e-6
	for _, p := range ps {
		if p.X < -eps || p.Y < -eps || p.Z < -eps ||
			p.X+p.W > w+eps || p.Y+p.D > d+eps || p.Z+p.H > h+eps {
			t.Errorf("item %s escapes bin: (%v,%v,%v) %vx%vx%v", p.itemID, p.X, p.Y, p.Z, p.W, p.D, p.H)
		}
	}
	for i := 0; i < len(ps); i++ {
		for j := i + 1; j < len(ps); j++ {
			a, b := ps[i], ps[j]
			if a.binID != b.binID {
				continue
			}
			if overlap1D(a.X, a.X+a.W, b.X, b.X+b.W) > eps &&
				overlap1D(a.Y, a.Y+a.D, b.Y, b.Y+b.D) > eps &&
				overlap1D(a.Z, a.Z+a.H, b.Z, b.Z+b.H) > eps {
				t.Errorf("items %s and %s overlap", a.itemID, b.itemID)
			}
		}
	}
	for _, p := range ps {
		if p.Z <= eps {
			continue
		}
		supported := false
		for _, q := range ps {
			if q == p || q.binID != p.binID {
				continue
			}
			if restsOn(p, q) {
				supported = true
				break
			}
		}
		if !supported {
			t.Errorf("item %s at z=%v floats (nothing beneath)", p.itemID, p.Z)
		}
	}
}

func sumZ(ps []*Placement3D) float64 {
	s := 0.0
	for _, p := range ps {
		s += p.Z
	}
	return s
}

// A removable item perched on top of another should be pulled down into the open
// floor space beside it: B sits on A at z=6; the refiner drops it to z=0.
func TestRefineVoids_LowersTopItem(t *testing.T) {
	ps := []*Placement3D{
		{binID: "b", itemID: "A", X: 0, Y: 0, Z: 0, W: 6, D: 6, H: 6},
		{binID: "b", itemID: "B", X: 0, Y: 0, Z: 6, W: 4, D: 4, H: 4},
	}
	orients := map[string][][3]float64{"A": {{6, 6, 6}}, "B": {{4, 4, 4}}}
	if !RefineVoids(context.Background(), ps, orients, 10, 10, 10, ContactSpec{}, RefineOptions{}) {
		t.Fatal("expected the refiner to move B down")
	}
	var b *Placement3D
	for _, p := range ps {
		if p.itemID == "B" {
			b = p
		}
	}
	if math.Abs(b.Z) > 1e-6 {
		t.Errorf("B at z=%v, want 0 (dropped to the floor)", b.Z)
	}
	assertRefineValid(t, ps, 10, 10, 10)
}

// A needless tower (five flats stacked in one column) should spread across the
// floor, lowering ΣZ and the peak; a second pass must then be a fixed point.
func TestRefineVoids_SpreadsTowerAndIsIdempotent(t *testing.T) {
	var ps []*Placement3D
	orients := map[string][][3]float64{}
	for i := 0; i < 5; i++ {
		id := string(rune('a' + i))
		ps = append(ps, &Placement3D{binID: "b", itemID: id, X: 0, Y: 0, Z: float64(i), W: 2, D: 2, H: 1})
		orients[id] = [][3]float64{{2, 2, 1}} // no rotation, keep the 2×2 footprint
	}
	before := sumZ(ps)
	if !RefineVoids(context.Background(), ps, orients, 10, 10, 10, ContactSpec{}, RefineOptions{}) {
		t.Fatal("expected the tower to be lowered")
	}
	assertRefineValid(t, ps, 10, 10, 10)
	after := sumZ(ps)
	if !(after < before-1e-6) {
		t.Errorf("ΣZ did not improve: before %v, after %v", before, after)
	}
	peak := 0.0
	for _, p := range ps {
		if top := p.Z + p.H; top > peak {
			peak = top
		}
	}
	if peak > 1+1e-6 {
		t.Errorf("peak height = %v, want 1 (all flats on the floor)", peak)
	}
	// Fixed point: refining the already-tight packing changes nothing.
	if RefineVoids(context.Background(), ps, orients, 10, 10, 10, ContactSpec{}, RefineOptions{}) {
		t.Error("second refine pass moved items — not a fixed point")
	}
}

// The refiner must never raise ΣZ, drop items, overlap, or float — on an
// arbitrary (suboptimal) grounded packing.
func TestRefineVoids_NeverWorsens(t *testing.T) {
	// A staircase of cubes each resting on the previous — lots of room to drop.
	var ps []*Placement3D
	orients := map[string][][3]float64{}
	for i := 0; i < 6; i++ {
		id := string(rune('a' + i))
		ps = append(ps, &Placement3D{binID: "b", itemID: id, X: float64(i), Y: 0, Z: float64(i), W: 3, D: 3, H: 3})
		orients[id] = [][3]float64{{3, 3, 3}}
	}
	// The staircase as built floats (each cube's base is at z=i but only the i-th
	// column supports it); ground it first by dropping to z=0 is the refiner's job,
	// but the input here is contrived — only assert validity post-refine and ΣZ↓.
	// Build a valid grounded input instead: stack them in one column.
	ps = ps[:0]
	for i := 0; i < 6; i++ {
		id := string(rune('a' + i))
		ps = append(ps, &Placement3D{binID: "b", itemID: id, X: 0, Y: 0, Z: float64(3 * i), W: 3, D: 3, H: 3})
		orients[id] = [][3]float64{{3, 3, 3}}
	}
	before := sumZ(ps)
	n := len(ps)
	RefineVoids(context.Background(), ps, orients, 12, 12, 18, ContactSpec{}, RefineOptions{})
	if len(ps) != n {
		t.Fatalf("item count changed: %d → %d", n, len(ps))
	}
	assertRefineValid(t, ps, 12, 12, 18)
	if sumZ(ps) > before+1e-6 {
		t.Errorf("ΣZ increased: before %v, after %v", before, sumZ(ps))
	}
}
