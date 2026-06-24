package packapi

import (
	"testing"

	"github.com/W-Floyd/go-pack-bins/d3"
	"github.com/W-Floyd/go-pack-bins/pack"
)

// The block packer's final fill must run (not be skipped) on a many-thousand-box
// bin, thanks to the solid-slab reduction that ignores everything below the last
// full layer. Asserts all items placed into one bin and the top-region placements
// don't overlap (guarding the slab/clip reconstruction).
func TestBlockPacker_FinalFillScales(t *testing.T) {
	specs := GenerateItems("3d", "mix", 3000, 7)
	in := make([]pack.Item, len(specs))
	for i, s := range specs {
		in[i] = d3.NewItem(s.ID, s.Width, s.Depth, s.Height, s.AllowRotate)
	}
	pr, err := d3.NewBlockPacker(20, 20, 999999).PackAll(in) // one tall bin
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	if len(pr.Unplaced) != 0 {
		t.Fatalf("unplaced=%d", len(pr.Unplaced))
	}
	if pr.BinsUsed() != 1 {
		t.Fatalf("bins=%d, want 1", pr.BinsUsed())
	}
	var b []*d3.Placement3D
	for _, p := range pr.Placements {
		if p3, ok := p.(*d3.Placement3D); ok {
			b = append(b, p3)
		}
	}
	overlap := func(a, c *d3.Placement3D) bool {
		const e = 1e-6
		return a.X < c.X+c.W-e && a.X+a.W > c.X+e &&
			a.Y < c.Y+c.D-e && a.Y+a.D > c.Y+e &&
			a.Z < c.Z+c.H-e && a.Z+a.H > c.Z+e
	}
	for _, a := range b[len(b)-300:] { // the final-fill zone
		for _, c := range b {
			if a != c && overlap(a, c) {
				t.Fatalf("overlap: %s @(%.0f,%.0f,%.0f) vs %s @(%.0f,%.0f,%.0f)",
					a.ItemID(), a.X, a.Y, a.Z, c.ItemID(), c.X, c.Y, c.Z)
			}
		}
	}
}
