package d2

import "testing"

func TestShelf_FirstFitOpensAndReusesShelves(t *testing.T) {
	s := NewShelf(10, 10, ShelfFirstFit)

	// First item opens shelf 0 at y=0, height 4.
	if x, y, _, ok := s.TryInsert(4, 4, false); !ok || x != 0 || y != 0 {
		t.Fatalf("first at (%v,%v) ok=%v, want (0,0)", x, y, ok)
	}
	// Fits on shelf 0 (h=3 ≤ 4, cursor 4+3 ≤ 10): placed at (4,0).
	if x, y, _, ok := s.TryInsert(3, 3, false); !ok || x != 4 || y != 0 {
		t.Fatalf("second at (%v,%v), want (4,0)", x, y)
	}
	// Too tall for shelf 0 (6 > 4) → opens shelf 1 at y=4.
	if x, y, _, ok := s.TryInsert(5, 6, false); !ok || x != 0 || y != 4 {
		t.Fatalf("third at (%v,%v), want (0,4)", x, y)
	}
}

func TestShelf_NextFitOnlyTopShelf(t *testing.T) {
	s := NewShelf(10, 10, ShelfNextFit)
	s.TryInsert(4, 4, false) // shelf 0
	s.TryInsert(6, 6, false) // too tall → shelf 1 at y=4
	// A short item would fit shelf 0 under FirstFit, but NextFit only sees the
	// top shelf (shelf 1, height 6), so it lands there.
	if _, y, _, ok := s.TryInsert(2, 2, false); !ok || y != 4 {
		t.Fatalf("next-fit placed at y=%v, want 4 (top shelf only)", y)
	}
}

func TestShelf_BestFitMinimisesLeftover(t *testing.T) {
	s := NewShelf(20, 20, ShelfBestFit)
	// Open two shelves of different heights.
	s.TryInsert(3, 5, false) // shelf 0 height 5
	s.TryInsert(3, 8, false) // too tall for 0 → shelf 1 height 8
	// A height-5 item fits both shelves; best-fit picks shelf 0 (leftover 0) over
	// shelf 1 (leftover 3), i.e. y=0.
	if _, y, _, ok := s.TryInsert(3, 5, false); !ok || y != 0 {
		t.Fatalf("best-fit placed at y=%v, want 0 (tightest shelf)", y)
	}
}
