package d3

import "testing"

// White-box test for the deferral metric (unexported, so this lives in package
// d3). The shallow-top deferral decision must judge a rotatable item by its
// flattest orientation, so a long-skinny box (which can lie flat) does not count
// as "tall" and so does not force a top to be deferred.
func TestFlattestTallestUsesFlattestOrientation(t *testing.T) {
	bp := NewBlockPacker(20, 20, 20)
	its := []*pitem{
		{id: "tall", orient: []orient{{5, 5, 9}}},                            // fixed, only 9 tall
		{id: "skinny", orient: []orient{{1, 1, 10}, {1, 10, 1}, {10, 1, 1}}}, // rotatable: flat = 1
	}
	consumed := make([]bool, len(its))

	// Decisive height is the tall item's 9 — the skinny is counted by its flat
	// height (1), not its standing height (10).
	if got := bp.flattestTallest(its, consumed); got != 9 {
		t.Errorf("flattestTallest = %v, want 9 (skinny judged by flat height 1, not 10)", got)
	}
	// Contrast: the layer-height selection still sees the skinny's tall (10)
	// orientation — the two metrics genuinely differ.
	if got := bp.maxHeight(its, consumed, 20); got != 10 {
		t.Errorf("maxHeight = %v, want 10 (tallest orientation overall)", got)
	}
	// With only the skinny left, the decisive height drops to its flat height, so a
	// shallow top tall enough for that (≥1) will be filled, not deferred.
	consumed[0] = true
	if got := bp.flattestTallest(its, consumed); got != 1 {
		t.Errorf("flattestTallest(skinny only) = %v, want 1", got)
	}
}
