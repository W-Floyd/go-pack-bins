package d3

import (
	"context"
	"testing"
)

// byID finds a placement by item ID. Compact sorts the slice it is given, so
// positional indexing into it after the call reads the wrong box.
func byID(ps []*Placement3D, id string) *Placement3D {
	for _, p := range ps {
		if p.itemID == id {
			return p
		}
	}
	return nil
}

// pl builds a placement directly, so a post-pass can be handed a hand-built
// configuration rather than one a packer happened to produce.
func pl(binID, itemID string, x, y, z, w, d, h float64) *Placement3D {
	return &Placement3D{binID: binID, itemID: itemID, X: x, Y: y, Z: z, W: w, D: d, H: h}
}

// guardFor builds a guard over a literal id→(weight, limit) table.
func guardFor(t map[string][2]float64) *Guard {
	sc := map[string]map[string]float64{}
	for id, wl := range t {
		sc[id] = map[string]float64{wScalar: wl[0], lScalar: wl[1]}
	}
	return NewGuard(NewBearingGuard(bearSpec(), sc), nil)
}

// Compact slides riderless boxes toward the walls. A slide can move a box off a
// strong supporter and onto a weak one — support-preserving is not
// bearing-preserving. The guard must revert such a slide.
func TestCompactGuardBlocksCrushingSlide(t *testing.T) {
	// Floor: fragile at x=0, strong at x=2. Cargo sits on the strong one at x=2
	// with a gap at x=1; compaction wants to slide it left onto the fragile box.
	build := func() []*Placement3D {
		return []*Placement3D{
			pl("b", "fragile", 0, 0, 0, 1, 1, 1),
			pl("b", "strong", 1, 0, 0, 2, 1, 1),
			pl("b", "cargo", 2, 0, 1, 1, 1, 1),
		}
	}
	guard := guardFor(map[string][2]float64{
		"fragile": {1, 0},       // bears nothing
		"strong":  {1, NoLimit}, // bears anything
		"cargo":   {5, NoLimit},
	})

	// Ungated: the slide happens and crushes the fragile box.
	ps := build()
	Compact(ps, 3, 1, 3, true, false, 0)
	if x := byID(ps, "cargo").X; x > compactEps {
		t.Fatalf("ungated compaction did not slide cargo to x=0 (got %v); the guard test proves nothing", x)
	}
	if guard.OK(ps) {
		t.Fatal("ungated compaction produced a configuration the guard accepts; expected a crush")
	}

	// Guarded: the slide is reverted.
	ps = build()
	CompactGuarded(ps, 3, 1, 3, true, false, 0, guard)
	if !guard.OK(ps) {
		t.Errorf("guarded compaction left a crushing configuration: cargo at x=%v", byID(ps, "cargo").X)
	}
}

// A slide that crushes nothing must still happen — the guard must not freeze
// compaction wholesale.
func TestCompactGuardAllowsSafeSlide(t *testing.T) {
	ps := []*Placement3D{
		pl("b", "base", 0, 0, 0, 3, 1, 1),
		pl("b", "cargo", 2, 0, 1, 1, 1, 1),
	}
	guard := guardFor(map[string][2]float64{
		"base":  {1, 100},
		"cargo": {5, NoLimit},
	})
	CompactGuarded(ps, 3, 1, 3, true, false, 0, guard)
	if x := byID(ps, "cargo").X; x > compactEps {
		t.Errorf("safe slide was blocked: cargo at x=%v, want 0", x)
	}
}

// A nil guard must leave Compact's behaviour exactly as it was.
func TestCompactGuardNilMatchesCompact(t *testing.T) {
	build := func() []*Placement3D {
		return []*Placement3D{
			pl("b", "a", 0, 0, 0, 1, 1, 1),
			pl("b", "b", 2, 0, 0, 1, 1, 1),
			pl("b", "c", 1, 2, 0, 1, 1, 1),
			pl("b", "d", 2, 0, 1, 1, 1, 1),
		}
	}
	plain, guarded := build(), build()
	Compact(plain, 4, 4, 4, true, true, 0)
	CompactGuarded(guarded, 4, 4, 4, true, true, 0, nil)
	for _, id := range []string{"a", "b", "c", "d"} {
		if *byID(plain, id) != *byID(guarded, id) {
			t.Errorf("%s differs: %+v vs %+v", id, *byID(plain, id), *byID(guarded, id))
		}
	}
}

// The void refiner re-drops items through an ungated ExtremePoint, so its accept
// step is the only place the bearing rule can be enforced.
func TestRefineGuardRejectsCrushingRedrop(t *testing.T) {
	// A fragile box on the floor with a void beside it; cargo perched high.
	// Refining wants to drop cargo down — onto the fragile box.
	build := func() []*Placement3D {
		return []*Placement3D{
			pl("b", "fragile", 0, 0, 0, 2, 2, 1),
			pl("b", "pillar", 2, 0, 0, 1, 2, 3),
			pl("b", "cargo", 0, 0, 3, 2, 2, 1),
		}
	}
	orients := map[string][][3]float64{
		"fragile": {{2, 2, 1}},
		"pillar":  {{1, 2, 3}},
		"cargo":   {{2, 2, 1}},
	}
	guard := guardFor(map[string][2]float64{
		"fragile": {1, 0},
		"pillar":  {1, NoLimit},
		"cargo":   {9, NoLimit},
	})

	// Ungated, the refiner pulls cargo down onto the fragile box.
	ps := build()
	movedPlain := RefineVoids(context.Background(), ps, orients, 3, 2, 4, ContactSpec{}, RefineOptions{})
	if z := byID(ps, "cargo").Z; !movedPlain || z > 1+compactEps {
		t.Skipf("refiner did not lower cargo (moved=%v, z=%v); nothing to guard here", movedPlain, z)
	}
	if guard.OK(ps) {
		t.Fatal("ungated refine produced an acceptable configuration; expected a crush")
	}

	// Guarded, the re-drop is rejected and the bin is left untouched.
	ps = build()
	RefineVoids(context.Background(), ps, orients, 3, 2, 4, ContactSpec{}, RefineOptions{Guard: guard})
	if !guard.OK(ps) {
		t.Errorf("guarded refine left a crushing configuration: cargo at z=%v", byID(ps, "cargo").Z)
	}
}

// With no bearing limits in play the refiner must behave identically, so the
// guard costs nothing when the feature is off.
func TestRefineGuardNilMatchesRefine(t *testing.T) {
	build := func() []*Placement3D {
		return []*Placement3D{
			pl("b", "base", 0, 0, 0, 2, 2, 1),
			pl("b", "pillar", 2, 0, 0, 1, 2, 3),
			pl("b", "cargo", 0, 0, 3, 2, 2, 1),
		}
	}
	orients := map[string][][3]float64{
		"base": {{2, 2, 1}}, "pillar": {{1, 2, 3}}, "cargo": {{2, 2, 1}},
	}
	plain, guarded := build(), build()
	a := RefineVoids(context.Background(), plain, orients, 3, 2, 4, ContactSpec{}, RefineOptions{})
	b := RefineVoids(context.Background(), guarded, orients, 3, 2, 4, ContactSpec{}, RefineOptions{Guard: nil})
	if a != b {
		t.Fatalf("moved differs: %v vs %v", a, b)
	}
	for _, id := range []string{"base", "pillar", "cargo"} {
		if *byID(plain, id) != *byID(guarded, id) {
			t.Errorf("%s differs: %+v vs %+v", id, *byID(plain, id), *byID(guarded, id))
		}
	}
}
