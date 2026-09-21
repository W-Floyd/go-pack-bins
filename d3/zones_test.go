package d3

import "testing"

// A non-supporting zone is an obstacle: nothing may rest on it. A supporting one
// is a surface: items may sit on its top face, though still not inside it.
func TestZoneSupportsDistinguishesLedgeFromPipe(t *testing.T) {
	// A 4x4 ledge/pipe 1 high across the floor of a 4x4x4 bin. With it
	// supporting, an item can sit at z=1; without, it cannot rest anywhere.
	for _, tc := range []struct {
		name     string
		supports bool
		wantZ    float64
		wantOK   bool
	}{
		{"ledge carries the item", true, 1, true},
		{"pipe carries nothing", false, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			zone := Zone{X: 0, Y: 0, Z: 0, W: 4, D: 4, H: 1, Supports: tc.supports}
			ep := NewExtremePoint(4, 4, 4)
			ep.contact = ContactSpec{NoFloating: true} // must rest on something
			WithZones(ep, []Zone{zone})

			bin := NewBin("b", 4, 4, 4, ep)
			p, err := bin.TryPlace(NewItem("x", 4, 4, 1, false))
			if tc.wantOK {
				if err != nil {
					t.Fatalf("item refused though the ledge should hold it: %v", err)
				}
				if p.(*Placement3D).Z != tc.wantZ {
					t.Errorf("placed at z=%v, want %v (on the ledge)", p.(*Placement3D).Z, tc.wantZ)
				}
			} else if err == nil {
				t.Errorf("item placed at z=%v though nothing may rest on a pipe",
					p.(*Placement3D).Z)
			}
		})
	}
}

// Supporting or not, a zone still blocks placement inside itself.
func TestZoneSupportsStillBlocksInside(t *testing.T) {
	for _, supports := range []bool{false, true} {
		zone := Zone{X: 0, Y: 0, Z: 0, W: 4, D: 4, H: 2, Supports: supports}
		ep := WithZones(NewExtremePoint(4, 4, 4), []Zone{zone})
		bin := NewBin("b", 4, 4, 4, ep)
		p, err := bin.TryPlace(NewItem("x", 4, 4, 2, false))
		if err != nil {
			t.Fatalf("supports=%v: nothing placed at all", supports)
		}
		if got := p.(*Placement3D).Z; got < 2 {
			t.Errorf("supports=%v: item at z=%v is inside the zone", supports, got)
		}
	}
}

// supportArea counts only supporting zones, and only where their top face meets
// the footprint's base.
func TestZoneSupportArea(t *testing.T) {
	zs := zoneSet{
		{X: 0, Y: 0, Z: 0, W: 2, D: 2, H: 1, Supports: true},
		{X: 2, Y: 0, Z: 0, W: 2, D: 2, H: 1}, // not supporting
	}
	if got := zs.supportArea(0, 0, 1, 4, 2); got != 4 {
		t.Errorf("support area = %v, want 4 (only the supporting half counts)", got)
	}
	if got := zs.supportArea(0, 0, 3, 4, 2); got != 0 {
		t.Errorf("support area = %v at the wrong height, want 0", got)
	}
}

// A supporting zone is structure, not cargo: weight resting on it leaves the
// stack as it would at the floor, so it has no crush limit of its own and
// nothing beneath it is loaded.
func TestZoneSupportsCarriesNoBearingLoad(t *testing.T) {
	spec := BearingSpec{WeightScalar: "weight", LimitScalar: "lim", DefaultLimit: NoLimit}
	ledge := Zone{X: 0, Y: 0, Z: 0, W: 4, D: 4, H: 1, Supports: true}

	ep := NewExtremePoint(4, 4, 4)
	ep.contact = ContactSpec{NoFloating: true}
	WithZones(ep, []Zone{ledge})
	BearingStrategy(func(w, d, h float64) PlacementStrategy3D { return ep }, spec)(4, 4, 4)

	bin := NewBin("b", 4, 4, 4, WithBearing(ep, spec))
	heavy := NewItem("heavy", 4, 4, 1, false).WithScalar("weight", 1e6)
	if _, err := bin.TryPlace(heavy); err != nil {
		t.Errorf("a ledge refused a heavy item; structure has no crush limit: %v", err)
	}
}
