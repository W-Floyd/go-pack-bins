package packapi

import "testing"

// The void-refiner post-pass must keep a packing valid (every item placed, inside
// the bin, no overlaps, nothing floating) and never use more bins than the base
// solve — it only relocates items downward into voids.
func TestPackRefineVoidsStaysValid(t *testing.T) {
	items := []ItemSpec{}
	for i := 0; i < 60; i++ {
		w := float64(2 + i%4) // 2..5
		items = append(items, ItemSpec{ID: itoa(i), Width: w, Height: w, Depth: w, AllowRotate: true})
	}
	bin := BinSpec{Width: 12, Height: 12, Depth: 12}

	for _, algo := range []string{"ff", "heightmap", "blocks"} {
		base := Pack(PackRequest{Mode: "3d", Algorithm: algo, Bin: bin, Items: items})
		refined := Pack(PackRequest{Mode: "3d", Algorithm: algo, Bin: bin, Items: items, RefineVoids: true})

		if refined.Error != "" {
			t.Fatalf("%s+refine: %s", algo, refined.Error)
		}
		if len(refined.Placements) != len(base.Placements) {
			t.Errorf("%s+refine: placed %d, base placed %d (refiner dropped items)",
				algo, len(refined.Placements), len(base.Placements))
		}
		if len(refined.Unplaced) != len(base.Unplaced) {
			t.Errorf("%s+refine: %d unplaced, base %d", algo, len(refined.Unplaced), len(base.Unplaced))
		}
		if refined.BinsUsed > base.BinsUsed {
			t.Errorf("%s+refine: used %d bins, base %d (refine should not add bins)",
				algo, refined.BinsUsed, base.BinsUsed)
		}
		assertValid3D(t, refined, bin.Width, bin.Depth, bin.Height)
	}
}
