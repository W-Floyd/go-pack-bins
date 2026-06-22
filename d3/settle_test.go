package d3_test

import (
	"testing"

	"github.com/W-Floyd/go-pack-bins/d3"
)

func TestSettle_DropsFloatersOntoSupport(t *testing.T) {
	base := &d3.Placement3D{X: 0, Y: 0, Z: 0, W: 4, D: 4, H: 3}    // on the floor
	onBase := &d3.Placement3D{X: 0, Y: 0, Z: 7, W: 2, D: 2, H: 2}  // floats above base
	overGap := &d3.Placement3D{X: 6, Y: 0, Z: 5, W: 2, D: 2, H: 2} // floats over empty floor
	ps := []*d3.Placement3D{base, onBase, overGap}

	d3.Settle(ps)

	if base.Z != 0 {
		t.Errorf("base moved to z=%v, want 0", base.Z)
	}
	if onBase.Z != 3 {
		t.Errorf("floater over base at z=%v, want 3 (resting on base top)", onBase.Z)
	}
	if overGap.Z != 0 {
		t.Errorf("floater over empty floor at z=%v, want 0", overGap.Z)
	}
	assertNoOverlap(t, ps, 100, 100, 100)
}

func TestSettle_CascadesAndKeepsStacks(t *testing.T) {
	// A genuine stack (b on a) stays intact and drops as a unit when a is floating;
	// non-overlapping footprints are unaffected.
	a := &d3.Placement3D{X: 0, Y: 0, Z: 5, W: 3, D: 3, H: 2} // floats; should drop to 0
	b := &d3.Placement3D{X: 0, Y: 0, Z: 7, W: 3, D: 3, H: 2} // rests on a (a.Z+H=7); should follow to 2
	ps := []*d3.Placement3D{a, b}

	d3.Settle(ps)

	if a.Z != 0 {
		t.Errorf("a at z=%v, want 0", a.Z)
	}
	if b.Z != 2 {
		t.Errorf("b at z=%v, want 2 (still resting on a)", b.Z)
	}
	assertNoOverlap(t, ps, 100, 100, 100)
}
