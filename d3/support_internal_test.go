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
