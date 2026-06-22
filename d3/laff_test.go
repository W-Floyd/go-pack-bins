package d3

import (
	"testing"

	"github.com/W-Floyd/go-pack-bins/pack"
)

// overlaps reports whether two axis-aligned boxes intersect (with a tiny epsilon
// so face-touching does not count).
func overlaps(a, b *Placement3D) bool {
	const e = 1e-6
	return a.X < b.X+b.W-e && b.X < a.X+a.W-e &&
		a.Y < b.Y+b.D-e && b.Y < a.Y+a.D-e &&
		a.Z < b.Z+b.H-e && b.Z < a.Z+a.H-e
}

func TestLAFFPacksWithoutOverlap(t *testing.T) {
	const W, D, H = 10.0, 10.0, 10.0
	var items []pack.Item
	for i := 0; i < 8; i++ {
		items = append(items, NewItem(string(rune('a'+i)), 5, 5, 5, true)) // 2×2 per level, 2 levels
	}
	r, err := LAFF(items, W, D, H)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Unplaced) != 0 {
		t.Fatalf("expected all placed, unplaced: %v", r.Unplaced)
	}
	if r.BinsUsed() != 1 {
		t.Fatalf("expected 8 unit-cubes to fit one 10³ container, got %d bins", r.BinsUsed())
	}

	pls := make([]*Placement3D, 0, len(r.Placements))
	for _, p := range r.Placements {
		pl := p.(*Placement3D)
		// within container bounds
		if pl.X < -1e-9 || pl.Y < -1e-9 || pl.Z < -1e-9 ||
			pl.X+pl.W > W+1e-9 || pl.Y+pl.D > D+1e-9 || pl.Z+pl.H > H+1e-9 {
			t.Fatalf("placement %s out of bounds: %+v", pl.itemID, pl)
		}
		pls = append(pls, pl)
	}
	for i := 0; i < len(pls); i++ {
		for j := i + 1; j < len(pls); j++ {
			if overlaps(pls[i], pls[j]) {
				t.Fatalf("overlap between %s and %s", pls[i].itemID, pls[j].itemID)
			}
		}
	}
}

func TestLAFFRejectsOversizeItem(t *testing.T) {
	items := []pack.Item{
		NewItem("ok", 4, 4, 4, true),
		NewItem("toobig", 12, 1, 1, false), // 12 > container width 10, no rotation
	}
	r, err := LAFF(items, 10, 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Unplaced) != 1 || r.Unplaced[0] != "toobig" {
		t.Fatalf("expected only 'toobig' unplaced, got %v", r.Unplaced)
	}
}
