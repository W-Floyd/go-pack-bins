package knapsack

import (
	"context"
	"testing"

	"github.com/W-Floyd/go-pack-bins/d2"
	"github.com/W-Floyd/go-pack-bins/pack"
)

// bin10 returns a fresh 10×10 MaxRects bin.
func bin10() *d2.Bin2D {
	return d2.NewBin("kn", 10, 10, d2.NewMaxRectsDefault(10, 10))
}

// toItems widens a slice of concrete 2-D items to the pack.Item interface.
func toItems(in []*d2.Item2D) []pack.Item {
	out := make([]pack.Item, len(in))
	for i, it := range in {
		out[i] = it
	}
	return out
}

func TestPack_PrefersHigherValue(t *testing.T) {
	// The bin (area 100) holds at most one 10×10 item. Offer two: a low-value and
	// a high-value one. The high-value item must be the one selected.
	items := []*d2.Item2D{
		d2.NewItem("cheap", 10, 10, false).WithScalar("value", 1),
		d2.NewItem("rich", 10, 10, false).WithScalar("value", 100),
	}
	r := Pack(context.Background(), toItems(items), bin10(), Options{})
	if r.TotalValue != 100 {
		t.Fatalf("total value = %v, want 100 (picked rich)", r.TotalValue)
	}
	if len(r.Rejected) != 1 || r.Rejected[0] != "cheap" {
		t.Fatalf("rejected = %v, want [cheap]", r.Rejected)
	}
}

func TestPack_FillsMultipleWhenTheyFit(t *testing.T) {
	// Four 5×5 items all fit the 10×10 bin; all should be selected.
	items := []*d2.Item2D{
		d2.NewItem("a", 5, 5, false).WithScalar("value", 3),
		d2.NewItem("b", 5, 5, false).WithScalar("value", 4),
		d2.NewItem("c", 5, 5, false).WithScalar("value", 5),
		d2.NewItem("d", 5, 5, false).WithScalar("value", 6),
	}
	r := Pack(context.Background(), toItems(items), bin10(), Options{})
	if len(r.Rejected) != 0 {
		t.Fatalf("rejected = %v, want none (all fit)", r.Rejected)
	}
	if r.TotalValue != 18 {
		t.Fatalf("total value = %v, want 18", r.TotalValue)
	}
}

func TestPack_DefaultValueIsVolume(t *testing.T) {
	// With no value scalar, value defaults to volume (area). One 10×10 slot: the
	// bigger-area item (area 64) beats the smaller (area 25).
	items := []*d2.Item2D{
		d2.NewItem("small", 5, 5, false),
		d2.NewItem("big", 8, 8, false),
	}
	r := Pack(context.Background(), toItems(items), bin10(), Options{})
	// big (64) placed; small (25) cannot also fit beside an 8×8 in a 10×10? It
	// can't: 8+5 > 10 either axis, and the 8×8 leaves an L-gap too narrow. So big
	// is selected, small rejected.
	if r.TotalValue != 64 {
		t.Fatalf("total value = %v, want 64 (big by volume)", r.TotalValue)
	}
}

func TestPack_PlacementsAlignToItemOrder(t *testing.T) {
	items := []*d2.Item2D{
		d2.NewItem("zero", 5, 5, false).WithScalar("value", 5),
		d2.NewItem("one", 5, 5, false).WithScalar("value", 5),
	}
	r := Pack(context.Background(), toItems(items), bin10(), Options{})
	if len(r.Placements) != 2 {
		t.Fatalf("placements len = %d, want 2", len(r.Placements))
	}
	for i, want := range []string{"zero", "one"} {
		if r.Placements[i] == nil || r.Placements[i].ItemID() != want {
			t.Fatalf("placement[%d] = %v, want %q", i, r.Placements[i], want)
		}
	}
}

func TestPack_CustomValueScalar(t *testing.T) {
	items := []*d2.Item2D{
		d2.NewItem("a", 10, 10, false).WithScalar("revenue", 7),
		d2.NewItem("b", 10, 10, false).WithScalar("revenue", 3),
	}
	r := Pack(context.Background(), toItems(items), bin10(), Options{ValueScalar: "revenue"})
	if r.TotalValue != 7 {
		t.Fatalf("total value = %v, want 7 (picked a by revenue)", r.TotalValue)
	}
}
