package d3

import "testing"

func TestLowerBound_VolumeBound(t *testing.T) {
	// Eight 5×5×5 cubes (vol 125 each = 1000) into a 10×10×10 bin (vol 1000):
	// volume bound = 1, and they tile perfectly so the optimum is 1.
	var items []*Item3D
	for i := 0; i < 8; i++ {
		items = append(items, NewItem(string(rune('a'+i)), 5, 5, 5, false))
	}
	if got := LowerBound(items, 10, 10, 10); got != 1 {
		t.Fatalf("volume bound = %d, want 1", got)
	}
}

func TestLowerBound_VolumeBoundRoundsUp(t *testing.T) {
	// Nine 5×5×5 cubes (vol 1125) into a 10×10×10 bin (vol 1000): ceil = 2.
	var items []*Item3D
	for i := 0; i < 9; i++ {
		items = append(items, NewItem(string(rune('a'+i)), 5, 5, 5, false))
	}
	if got := LowerBound(items, 10, 10, 10); got != 2 {
		t.Fatalf("volume bound = %d, want 2", got)
	}
}

func TestLowerBound_BigItemBeatsVolumeBound(t *testing.T) {
	// Two 6×6×6 boxes in a 10×10×10 bin: volume bound = ceil(432/1000) = 1, but
	// neither fits beside the other (6 > 5 on every axis), so the optimum is 2.
	items := []*Item3D{
		NewItem("a", 6, 6, 6, false),
		NewItem("b", 6, 6, 6, false),
	}
	if got := LowerBound(items, 10, 10, 10); got != 2 {
		t.Fatalf("big-item bound = %d, want 2", got)
	}
}

func TestLowerBound_ExcludesUnplaceable(t *testing.T) {
	items := []*Item3D{
		NewItem("toobig", 20, 20, 20, false),
		NewItem("ok", 5, 5, 5, false),
	}
	if got := LowerBound(items, 10, 10, 10); got != 1 {
		t.Fatalf("bound = %d, want 1 (unplaceable excluded)", got)
	}
}
