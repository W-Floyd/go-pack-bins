package d3

import "testing"

func rp(id string, x, y, z, w, d, h float64) *Placement3D {
	return NewPlacement3D("b", id, x, y, z, w, d, h)
}

// A box in open space is retrievable; one walled in on every side is not.
func TestRetrievableOpenVersusBuried(t *testing.T) {
	const W, D, H = 9.0, 9.0, 9.0
	centre := rp("mid", 3, 3, 3, 3, 3, 3)

	if !Retrievable(centre, []*Placement3D{centre}, nil, W, D, H, RetrievalSpec{}) {
		t.Error("a lone box in an empty bin is not retrievable")
	}

	// Surround it on all four sides and above, each neighbour thick enough to
	// block a full own-extent withdrawal.
	walled := []*Placement3D{
		centre,
		rp("xneg", 0, 3, 3, 3, 3, 3),
		rp("xpos", 6, 3, 3, 3, 3, 3),
		rp("yneg", 3, 0, 3, 3, 3, 3),
		rp("ypos", 3, 6, 3, 3, 3, 3),
		rp("zpos", 3, 3, 6, 3, 3, 3),
	}
	if Retrievable(centre, walled, nil, W, D, H, RetrievalSpec{}) {
		t.Error("a box enclosed on all five faces is reported retrievable")
	}
	// Removing one neighbour restores the exit.
	if !Retrievable(centre, walled[:5], nil, W, D, H, RetrievalSpec{}) {
		t.Error("clearing the face above did not make the box retrievable")
	}
}

// The default clearance is the item's own extent: enough to pull it clear of its
// slot, not merely a crack.
func TestRetrievableDefaultClearanceIsOwnExtent(t *testing.T) {
	// Bin just 5 deep in x: a 3-wide box at x=0 has 2 units in front of it.
	p := rp("a", 0, 0, 0, 3, 1, 1)
	spec := RetrievalSpec{SidesOnly: true}
	if Retrievable(p, []*Placement3D{p}, nil, 5, 1, 1, spec) {
		t.Error("2 units of space accepted for a 3-wide box; it cannot come fully out")
	}
	if !Retrievable(p, []*Placement3D{p}, nil, 6, 1, 1, spec) {
		t.Error("3 units of space refused for a 3-wide box")
	}
	// An explicit clearance overrides the own-extent default.
	if !Retrievable(p, []*Placement3D{p}, nil, 5, 1, 1, RetrievalSpec{Clearance: 2, SidesOnly: true}) {
		t.Error("an explicit 2-unit clearance was not honoured")
	}
}

// SidesOnly drops the top face, for a store with no room to lift.
func TestRetrievableSidesOnly(t *testing.T) {
	// Boxed in on all four sides, open above.
	p := rp("mid", 3, 3, 0, 3, 3, 3)
	others := []*Placement3D{p,
		rp("a", 0, 3, 0, 3, 3, 3), rp("b", 6, 3, 0, 3, 3, 3),
		rp("c", 3, 0, 0, 3, 3, 3), rp("d", 3, 6, 0, 3, 3, 3)}

	if !Retrievable(p, others, nil, 9, 9, 9, RetrievalSpec{}) {
		t.Error("lifting straight up should be a way out by default")
	}
	if Retrievable(p, others, nil, 9, 9, 9, RetrievalSpec{SidesOnly: true}) {
		t.Error("SidesOnly still allowed the item to be lifted out")
	}
}

// With the openings named, an item needs a clear run out through one of them.
// A box flush with an opening is at the exit; a box flush with a solid wall is
// not, which a plain "reach the boundary" rule could not tell apart.
func TestRetrievableThroughNamedOpenings(t *testing.T) {
	// A trailer loaded through x = 0 only.
	spec := RetrievalSpec{OpenFaces: []Face{FaceXNeg}}

	flush := rp("a", 0, 0, 0, 2, 2, 2)
	if !Retrievable(flush, []*Placement3D{flush}, nil, 20, 2, 2, spec) {
		t.Error("a box at the opening is not retrievable")
	}

	front, back := rp("a", 0, 0, 0, 2, 2, 2), rp("b", 2, 0, 0, 2, 2, 2)
	both := []*Placement3D{front, back}
	if Retrievable(back, both, nil, 4, 2, 2, spec) {
		t.Error("a box behind another reached the opening through it")
	}
	// Flush with the far wall is not an exit: that wall is not an opening.
	far := rp("c", 2, 0, 0, 2, 2, 2)
	if Retrievable(far, []*Placement3D{front, far}, nil, 4, 2, 2, spec) {
		t.Error("a box flush with a solid wall was treated as being at an exit")
	}
	// Remove what is in front of it and the run to the opening is clear.
	if !Retrievable(back, []*Placement3D{back}, nil, 4, 2, 2, spec) {
		t.Error("a clear run to the opening was refused")
	}
}

// An impermeable zone blocks the way out; reserved clearance is exactly what you
// slide into.
func TestRetrievableZonePermeability(t *testing.T) {
	spec := RetrievalSpec{SidesOnly: true}
	p := rp("a", 2, 0, 0, 2, 2, 2)
	// A zone filling the only space the box could be drawn into.
	at := func(permeable bool) []Zone {
		return []Zone{{X: 0, Y: 0, Z: 0, W: 2, D: 2, H: 2, Permeable: permeable}}
	}
	// Bin is 4 wide: the box at x=2 can only go toward x=0.
	if Retrievable(p, []*Placement3D{p}, at(false), 4, 2, 2, spec) {
		t.Error("a solid zone let the box be slid through it")
	}
	if !Retrievable(p, []*Placement3D{p}, at(true), 4, 2, 2, spec) {
		t.Error("an aisle is reserved empty space; sliding into it is the point")
	}
	// A supporting zone is structure whatever its permeability says.
	ledge := []Zone{{X: 0, Y: 0, Z: 0, W: 2, D: 2, H: 2, Permeable: true, Supports: true}}
	if Retrievable(p, []*Placement3D{p}, ledge, 4, 2, 2, spec) {
		t.Error("a load-bearing ledge is solid and cannot be slid through")
	}
}

// The constraint is a property of the configuration: a new box can seal in one
// that was retrievable a moment earlier.
func TestRetrievableAllCatchesSealingAnItemIn(t *testing.T) {
	const W, D, H = 9.0, 9.0, 3.0
	spec := RetrievalSpec{SidesOnly: true}
	mid := rp("mid", 3, 3, 0, 3, 3, 3)
	ring := []*Placement3D{mid,
		rp("a", 0, 3, 0, 3, 3, 3), rp("b", 6, 3, 0, 3, 3, 3), rp("c", 3, 0, 0, 3, 3, 3)}

	if !RetrievableAll(ring, nil, W, D, H, spec) {
		t.Fatal("the open y+ side should still be a way out")
	}
	sealed := append(ring, rp("d", 3, 6, 0, 3, 3, 3))
	if RetrievableAll(sealed, nil, W, D, H, spec) {
		t.Error("closing the last side did not make the packing unretrievable")
	}
	if got := Buried(sealed, nil, W, D, H, spec); len(got) != 1 || got[0] != "mid" {
		t.Errorf("buried = %v, want just [mid]", got)
	}
}

// The gate must refuse a placement that would seal an item in, and it must not
// stop the packing dead: the packer should simply put the box elsewhere.
func TestRetrievalGateRefusesToBuryAnItem(t *testing.T) {
	ctors := map[string]func(w, d, h float64) PlacementStrategy3D{
		"extremepoint": func(w, d, h float64) PlacementStrategy3D { return NewExtremePoint(w, d, h) },
		"ems":          func(w, d, h float64) PlacementStrategy3D { return NewEmptyMaximalSpace(w, d, h) },
		"blf":          func(w, d, h float64) PlacementStrategy3D { return NewBottomLeftFill(w, d, h) },
		"heightmap":    func(w, d, h float64) PlacementStrategy3D { return NewHeightmap(w, d, h) },
	}
	// A 3x3x1 tray holds nine unit boxes. Filling it completely walls the centre
	// one in on all four sides, and SidesOnly rules out lifting it straight out,
	// so the gate must stop short of the last box.
	spec := RetrievalSpec{SidesOnly: true}
	for name, ctor := range ctors {
		t.Run(name, func(t *testing.T) {
			s := WithRetrieval(ctor(3, 3, 1), spec, nil, 3, 3, 1)
			if !RetrievalAware(s) {
				t.Fatalf("%s should be retrieval-aware", name)
			}
			bin := NewBin("b", 3, 3, 1, s)
			var ps []*Placement3D
			for i := 0; i < 9; i++ {
				if p, err := bin.TryPlace(NewItem(string(rune('a'+i)), 1, 1, 1, false)); err == nil {
					ps = append(ps, p.(*Placement3D))
				}
			}
			if len(ps) == 9 {
				t.Errorf("%s filled the tray, burying the centre box", name)
			}
			if !RetrievableAll(ps, nil, 3, 3, 1, spec) {
				t.Errorf("%s left a box with no way out", name)
			}
			// The gate is expensive with a greedy corner-based packer, and that is
			// worth seeing rather than hiding. A checkerboard would fit five boxes
			// here; filling the first row and then refusing everything that would
			// bury it gets three. The packers cannot find the checkerboard because
			// their candidate positions are corners of what is already placed, so
			// the useful gaps are never offered.
			t.Logf("%s placed %d of 9 (an optimal retrievable layout fits 5)", name, len(ps))
			if len(ps) < 3 {
				t.Errorf("%s placed only %d; even a single row should survive", name, len(ps))
			}
		})
	}
}

// With the gate off, the same bin fills and the middle box is buried — so the
// test above is not merely observing a bin that was always too small.
func TestRetrievalGateIsNotVacuous(t *testing.T) {
	bin := NewBin("b", 3, 3, 1, NewExtremePoint(3, 3, 1))
	var ps []*Placement3D
	for i := 0; i < 9; i++ {
		if p, err := bin.TryPlace(NewItem(string(rune('a'+i)), 1, 1, 1, false)); err == nil {
			ps = append(ps, p.(*Placement3D))
		}
	}
	if len(ps) != 9 {
		t.Fatalf("ungated tray took %d boxes, want 9", len(ps))
	}
	if RetrievableAll(ps, nil, 3, 3, 1, RetrievalSpec{SidesOnly: true}) {
		t.Error("the ungated packing leaves every box retrievable; the gate test proves nothing")
	}
}
