package strip

import (
	"math"
	"testing"

	"github.com/W-Floyd/go-pack-bins/d2"
	"github.com/W-Floyd/go-pack-bins/d3"
)

func TestPack2D_PerfectTile(t *testing.T) {
	// Four 5×5 items into a width-10 strip tile into a 10×10 square: the minimal
	// height is 10, which equals the area lower bound (100/10) — provably tight.
	items := []*d2.Item2D{
		d2.NewItem("a", 5, 5, false),
		d2.NewItem("b", 5, 5, false),
		d2.NewItem("c", 5, 5, false),
		d2.NewItem("d", 5, 5, false),
	}
	r := Pack2D(items, 10)
	if len(r.Unplaced) != 0 {
		t.Fatalf("unplaced=%v, want none", r.Unplaced)
	}
	if math.Abs(r.Height-10) > 1e-6 {
		t.Fatalf("height=%v, want 10", r.Height)
	}
	if math.Abs(r.LowerBound-10) > 1e-6 {
		t.Fatalf("lower bound=%v, want 10", r.LowerBound)
	}
	if r.Height < r.LowerBound-1e-6 {
		t.Fatalf("height %v below lower bound %v", r.Height, r.LowerBound)
	}
}

func TestPack2D_HeightNeverBelowLowerBound(t *testing.T) {
	items := []*d2.Item2D{
		d2.NewItem("a", 7, 3, false), d2.NewItem("b", 4, 8, false),
		d2.NewItem("c", 6, 6, false), d2.NewItem("d", 3, 3, false),
		d2.NewItem("e", 9, 2, false), d2.NewItem("f", 5, 5, false),
	}
	r := Pack2D(items, 10)
	if len(r.Unplaced) != 0 {
		t.Fatalf("unplaced=%v, want none", r.Unplaced)
	}
	if r.Height < r.LowerBound-1e-6 {
		t.Fatalf("height %v below lower bound %v (impossible)", r.Height, r.LowerBound)
	}
	if r.Strategy == "" {
		t.Fatalf("no winning strategy recorded")
	}
}

func TestPack2D_PlacementsAlignToItemOrder(t *testing.T) {
	items := []*d2.Item2D{
		d2.NewItem("first", 4, 4, false),
		d2.NewItem("second", 6, 6, false),
	}
	r := Pack2D(items, 10)
	if len(r.Placements) != 2 {
		t.Fatalf("placements len=%d, want 2", len(r.Placements))
	}
	if r.Placements[0] == nil || r.Placements[0].ItemID() != "first" {
		t.Fatalf("placement[0] = %v, want item 'first'", r.Placements[0])
	}
	if r.Placements[1] == nil || r.Placements[1].ItemID() != "second" {
		t.Fatalf("placement[1] = %v, want item 'second'", r.Placements[1])
	}
}

func TestPack2D_OversizedItemUnplaced(t *testing.T) {
	items := []*d2.Item2D{
		d2.NewItem("wide", 15, 2, false), // 15 > strip width 10, no rotation
		d2.NewItem("ok", 5, 5, false),
	}
	r := Pack2D(items, 10)
	if len(r.Unplaced) != 1 || r.Unplaced[0] != "wide" {
		t.Fatalf("unplaced=%v, want [wide]", r.Unplaced)
	}
}

func TestPack3D_PerfectTile(t *testing.T) {
	// Eight 5×5×5 cubes into a 10×10 base tile into a 10×10×10 cube: minimal
	// height 10 == volume lower bound (1000/100).
	var items []*d3.Item3D
	for i := 0; i < 8; i++ {
		items = append(items, d3.NewItem(string(rune('a'+i)), 5, 5, 5, false))
	}
	r := Pack3D(items, 10, 10)
	if len(r.Unplaced) != 0 {
		t.Fatalf("unplaced=%v, want none", r.Unplaced)
	}
	if math.Abs(r.Height-10) > 1e-6 {
		t.Fatalf("height=%v, want 10", r.Height)
	}
	if r.Height < r.LowerBound-1e-6 {
		t.Fatalf("height %v below lower bound %v", r.Height, r.LowerBound)
	}
}

func TestPack3D_BlockPacksDense(t *testing.T) {
	// A grouped instance (runs of identical boxes) is where block-building wins:
	// it must land near the volume lower bound in the tall strip container (the
	// block packer detects its own last layer, so no segmentation is needed).
	var items []*d3.Item3D
	for i := 0; i < 200; i++ {
		items = append(items, d3.NewItem("a"+string(rune(i)), 2, 2, 2, false))
	}
	for i := 0; i < 100; i++ {
		items = append(items, d3.NewItem("b"+string(rune(i)), 2, 2, 4, false))
	}
	r := Pack3D(items, 10, 10) // base area 100; volume 1600+1600=3200 → bound 32
	if len(r.Unplaced) != 0 {
		t.Fatalf("unplaced=%d, want none", len(r.Unplaced))
	}
	if r.Height < r.LowerBound-1e-6 {
		t.Fatalf("height %v below lower bound %v (impossible)", r.Height, r.LowerBound)
	}
	// Must be tight: within 15% of the volume bound (single-tall-bin blocks was ~35%+).
	if r.Height > r.LowerBound*1.15 {
		t.Fatalf("height %.1f exceeds 1.15× lower bound %.1f — block-stacking not engaging", r.Height, r.LowerBound)
	}
}

func TestPack3D_HeightNeverBelowLowerBound(t *testing.T) {
	items := []*d3.Item3D{
		d3.NewItem("a", 4, 4, 6, false), d3.NewItem("b", 6, 3, 5, false),
		d3.NewItem("c", 5, 5, 5, false), d3.NewItem("d", 3, 7, 4, false),
	}
	r := Pack3D(items, 10, 10)
	if len(r.Unplaced) != 0 {
		t.Fatalf("unplaced=%v, want none", r.Unplaced)
	}
	if r.Height < r.LowerBound-1e-6 {
		t.Fatalf("height %v below lower bound %v (impossible)", r.Height, r.LowerBound)
	}
}
