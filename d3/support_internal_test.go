package d3

import "testing"

// White-box tests for the support fraction that the NoFloating / Bottom gate
// relies on (unexported, so this lives in package d3).
func TestSupportFrac(t *testing.T) {
	ep := NewExtremePoint(10, 10, 10)
	// On the floor: fully supported.
	if got := ep.supportFrac(0, 0, 0, 4, 4); got != 1.0 {
		t.Errorf("floor support = %v, want 1", got)
	}
	// Place a 4×4×4 box on the floor, then probe boxes resting on its top (z=4).
	ep.placed = append(ep.placed, box{0, 0, 0, 4, 4, 4})
	// Fully on top → 1.0.
	if got := ep.supportFrac(0, 0, 4, 4, 4); got != 1.0 {
		t.Errorf("on-top support = %v, want 1", got)
	}
	// Half over the box, half over empty space → 0.5.
	if got := ep.supportFrac(2, 0, 4, 4, 4); got != 0.5 {
		t.Errorf("half-overhang support = %v, want 0.5", got)
	}
	// Floating in mid-air (z=4, no box beneath the footprint) → 0.
	if got := ep.supportFrac(6, 6, 4, 2, 2); got != 0 {
		t.Errorf("floating support = %v, want 0", got)
	}
}

// neighbourFrac is the anti-slosh objective: the fraction of a box's lateral
// faces pressed flush against placed boxes — walls excluded.
func TestNeighbourFrac(t *testing.T) {
	ep := NewExtremePoint(10, 10, 10)

	// Empty bin: no neighbours, and a wall must NOT count — score 0 even flush in
	// the corner.
	if f := ep.neighbourFrac(box{0, 0, 0, 2, 2, 2}, 0); f != 0 {
		t.Errorf("corner box neighbour frac = %v, want 0 (walls don't count)", f)
	}

	ep.placed = append(ep.placed, box{0, 0, 0, 3, 10, 10}) // slab over x[0,3], full y,z
	// Box flush against the slab's +x face, fully overlapping in y,z → full -x
	// face contact; +x face open → fraction = (1 + 0)/2 = 0.5.
	if f := ep.neighbourFrac(box{3, 0, 0, 2, 10, 10}, 0); f != 0.5 {
		t.Errorf("flush-to-slab neighbour frac = %v, want 0.5", f)
	}
	// A box not touching the slab (gap at x[5,7]) has no neighbour contact.
	if f := ep.neighbourFrac(box{5, 0, 0, 2, 10, 10}, 0); f != 0 {
		t.Errorf("detached box neighbour frac = %v, want 0", f)
	}
	// A box flush in x but with no z overlap (sits above the slab's height range)
	// gets no contact — contact needs overlap in the other two axes.
	ep2 := NewExtremePoint(10, 10, 10)
	ep2.placed = append(ep2.placed, box{0, 0, 0, 3, 10, 2}) // slab only z[0,2]
	if f := ep2.neighbourFrac(box{3, 0, 5, 2, 10, 2}, 0); f != 0 {
		t.Errorf("no-z-overlap neighbour frac = %v, want 0", f)
	}
}

// The objective prefers pressing against a neighbour over hugging a wall: with a
// slab placed, a box flush to it scores higher than one flush to the far wall.
func TestLateralScore_PrefersNeighbourOverWall(t *testing.T) {
	ep := NewExtremePoint(10, 10, 10)
	ep.contact = ContactSpec{SideX: 1}
	ep.placed = []box{{0, 0, 0, 2, 10, 10}} // x[0,2]
	neighbour := box{2, 0, 0, 2, 10, 10}    // flush against the slab
	wall := box{8, 0, 0, 2, 10, 10}         // flush against the far wall, open toward slab
	if ns, ws := ep.lateralScore(neighbour), ep.lateralScore(wall); !(ns > ws) {
		t.Errorf("neighbour score %v should beat wall-hug score %v", ns, ws)
	}
}
