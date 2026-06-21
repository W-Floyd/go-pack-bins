package d2

import "testing"

func TestSkyline_StacksAndFillsBottomLeft(t *testing.T) {
	s := NewSkyline(10, 10)

	// First item lands at the origin.
	x, y, _, ok := s.TryInsert(4, 3, false)
	if !ok || x != 0 || y != 0 {
		t.Fatalf("first insert at (%v,%v) ok=%v, want (0,0) true", x, y, ok)
	}
	// Second item: anchored left, it rests at the lowest contour. The region
	// [0,4] is at height 3, [4,10] is at height 0, so a width-4 item prefers the
	// flat floor at x=4, y=0 (lower than resting on top of the first).
	x, y, _, ok = s.TryInsert(4, 2, false)
	if !ok || y != 0 || x != 4 {
		t.Fatalf("second insert at (%v,%v), want (4,0)", x, y)
	}
	// A wide item spanning the first column must rest on the tallest covered
	// part: width 8 from x=0 covers heights {3,0} → rests at y=3.
	x, y, _, ok = s.TryInsert(8, 1, false)
	if !ok || x != 0 || y != 3 {
		t.Fatalf("wide insert at (%v,%v), want (0,3)", x, y)
	}
}

func TestSkyline_RotatesToFit(t *testing.T) {
	s := NewSkyline(5, 10)
	// 8×3 doesn't fit (8 > 5) unless rotated to 3×8.
	_, _, rot, ok := s.TryInsert(8, 3, true)
	if !ok || !rot {
		t.Fatalf("expected rotated placement, got rot=%v ok=%v", rot, ok)
	}
}

func TestSkyline_RejectsTooTall(t *testing.T) {
	s := NewSkyline(10, 5)
	if _, _, _, ok := s.TryInsert(3, 6, false); ok {
		t.Error("placed an item taller than the bin")
	}
}

func TestSkyline_Utilization(t *testing.T) {
	s := NewSkyline(10, 10)
	s.TryInsert(10, 5, false)
	if got := s.Utilization(); got < 0.49 || got > 0.51 {
		t.Errorf("utilization = %v, want ~0.5", got)
	}
}
