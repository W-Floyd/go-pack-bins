package d2

import "testing"

func TestLowerBound_AreaBound(t *testing.T) {
	// Four 5×5 items (area 25 each = 100) into a 10×10 bin (area 100): area bound
	// is exactly 1, and they tile perfectly, so the optimum is also 1.
	items := []*Item2D{
		NewItem("a", 5, 5, false),
		NewItem("b", 5, 5, false),
		NewItem("c", 5, 5, false),
		NewItem("d", 5, 5, false),
	}
	if got := LowerBound(items, 10, 10); got != 1 {
		t.Fatalf("area bound = %d, want 1", got)
	}
}

func TestLowerBound_AreaBoundRoundsUp(t *testing.T) {
	// Five 5×5 items (area 125) into a 10×10 bin (area 100): ceil(125/100) = 2.
	items := make([]*Item2D, 5)
	for i := range items {
		items[i] = NewItem(string(rune('a'+i)), 5, 5, false)
	}
	if got := LowerBound(items, 10, 10); got != 2 {
		t.Fatalf("area bound = %d, want 2", got)
	}
}

func TestLowerBound_BigItemBeatsAreaBound(t *testing.T) {
	// Three items each 6×6 in a 10×10 bin: area bound = ceil(108/100) = 2, but no
	// two 6×6 items share a bin (6 > 5 in both axes), so the optimum is 3. The
	// big-item bound must dominate.
	items := []*Item2D{
		NewItem("a", 6, 6, false),
		NewItem("b", 6, 6, false),
		NewItem("c", 6, 6, false),
	}
	if got := LowerBound(items, 10, 10); got != 3 {
		t.Fatalf("big-item bound = %d, want 3", got)
	}
}

func TestLowerBound_ExcludesUnplaceable(t *testing.T) {
	// A 20×20 item cannot fit a 10×10 bin in any orientation; it must not inflate
	// the bound. Only the single fitting 5×5 item counts ⇒ 1.
	items := []*Item2D{
		NewItem("toobig", 20, 20, false),
		NewItem("ok", 5, 5, false),
	}
	if got := LowerBound(items, 10, 10); got != 1 {
		t.Fatalf("bound = %d, want 1 (unplaceable excluded)", got)
	}
}

func TestLowerBound_NeverExceedsActualPack(t *testing.T) {
	// Property: the bound must never exceed a real feasible packing's bin count.
	// FFD-style pack into 10×10 bins via the shelf strategy, then compare.
	items := []*Item2D{
		NewItem("a", 7, 3, false), NewItem("b", 4, 8, false),
		NewItem("c", 6, 6, false), NewItem("d", 3, 3, false),
		NewItem("e", 9, 2, false), NewItem("f", 5, 5, false),
	}
	lb := LowerBound(items, 10, 10)
	// Greedy first-fit into a growing set of 10×10 bins.
	type b struct{ s *Shelf }
	var bins []*b
	for _, it := range items {
		placed := false
		for _, bn := range bins {
			if _, _, _, ok := bn.s.TryInsert(it.W, it.H, false); ok {
				placed = true
				break
			}
		}
		if !placed {
			nb := &b{s: NewShelf(10, 10, ShelfFirstFit)}
			nb.s.TryInsert(it.W, it.H, false)
			bins = append(bins, nb)
		}
	}
	if lb > len(bins) {
		t.Fatalf("lower bound %d exceeds actual packing %d bins", lb, len(bins))
	}
}
