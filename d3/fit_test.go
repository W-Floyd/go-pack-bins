package d3_test

import (
	"testing"

	"github.com/W-Floyd/go-pack-bins/d3"
)

func TestFit_NoOverlapAndFirstAtOrigin(t *testing.T) {
	dims := [][3]float64{{4, 4, 3}, {6, 3, 2}, {3, 3, 5}, {5, 5, 2}, {2, 6, 4}, {4, 4, 4}, {3, 7, 3}}
	ps := place3(t, d3.NewFitStrategy(10, 10, 10), 10, 10, 10, false, dims)
	if len(ps) == 0 {
		t.Fatal("nothing placed")
	}
	if ps[0].X != 0 || ps[0].Y != 0 || ps[0].Z != 0 {
		t.Errorf("first item at (%v,%v,%v), want origin", ps[0].X, ps[0].Y, ps[0].Z)
	}
	assertNoOverlap(t, ps, 10, 10, 10)
}

// Eight 5³ cubes tile a 10³ bin exactly; the contact-maximising strategy must
// reach full utilisation with no gaps.
func TestFit_TilesPerfectly(t *testing.T) {
	dims := make([][3]float64, 8)
	for i := range dims {
		dims[i] = [3]float64{5, 5, 5}
	}
	strat := d3.NewFitStrategy(10, 10, 10)
	ps := place3(t, strat, 10, 10, 10, false, dims)
	if len(ps) != 8 {
		t.Fatalf("placed %d of 8 cubes", len(ps))
	}
	assertNoOverlap(t, ps, 10, 10, 10)
	if u := strat.Utilization(); u < 0.999 {
		t.Errorf("utilization %.3f, want full (1.0)", u)
	}
}

// Gravity: every placed box must rest on the floor or on the top of another
// box — the contact criterion must never float a box high against a wall or the
// ceiling. This mix produces overhanging maximal spaces that the old, ungated
// fitness scoring would climb into.
func TestFit_RespectsGravity(t *testing.T) {
	dims := [][3]float64{{6, 4, 4}, {2, 4, 4}, {5, 4, 3}, {2, 3, 5}, {3, 5, 5}, {2, 6, 3}}
	ps := place3(t, d3.NewFitStrategy(10, 10, 10), 10, 10, 10, false, dims)
	if len(ps) == 0 {
		t.Fatal("nothing placed")
	}
	const eps = 1e-6
	for _, p := range ps {
		if p.Z <= eps {
			continue // on the floor
		}
		supported := false
		for _, q := range ps {
			if q == p {
				continue
			}
			if absf(q.Z+q.H-p.Z) > eps {
				continue // q's top is not at p's bottom
			}
			ox := overlap(p.X, p.X+p.W, q.X, q.X+q.W)
			oy := overlap(p.Y, p.Y+p.D, q.Y, q.Y+q.D)
			if ox > eps && oy > eps {
				supported = true
				break
			}
		}
		if !supported {
			t.Errorf("box at (%v,%v,%v) %vx%vx%v floats: no floor or box beneath it", p.X, p.Y, p.Z, p.W, p.D, p.H)
		}
	}
	assertNoOverlap(t, ps, 10, 10, 10)
}

func absf(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func overlap(a0, a1, b0, b1 float64) float64 {
	lo := a0
	if b0 > lo {
		lo = b0
	}
	hi := a1
	if b1 < hi {
		hi = b1
	}
	return hi - lo
}

// The contact criterion should wedge a second equal box flush against the first
// (sharing a face) rather than leaving a gap.
func TestFit_WedgesAgainstNeighbour(t *testing.T) {
	dims := [][3]float64{{5, 10, 10}, {5, 10, 10}}
	ps := place3(t, d3.NewFitStrategy(10, 10, 10), 10, 10, 10, false, dims)
	if len(ps) != 2 {
		t.Fatalf("placed %d of 2", len(ps))
	}
	// Two 5×10×10 slabs fill the 10×10×10 bin; the second must sit at x=5, flush
	// against the first, leaving no internal void.
	second := ps[1]
	if second.X != 5 {
		t.Errorf("second slab at x=%v, want 5 (flush against first)", second.X)
	}
	assertNoOverlap(t, ps, 10, 10, 10)
}
