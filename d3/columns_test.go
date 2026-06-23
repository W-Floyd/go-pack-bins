package d3_test

import (
	"testing"

	"github.com/W-Floyd/go-pack-bins/d3"
	"github.com/W-Floyd/go-pack-bins/pack"
)

// Identical XY profiles stack into one column: four 5×5×5 cubes fill a 5×5×20
// bin as a single column, one on top of another.
func TestColumnPacker_StacksIdenticalProfile(t *testing.T) {
	cp := d3.NewColumnPacker(5, 5, 20)
	res, err := cp.PackAll([]pack.Item{
		d3.NewItem("a", 5, 5, 5, false),
		d3.NewItem("b", 5, 5, 5, false),
		d3.NewItem("c", 5, 5, 5, false),
		d3.NewItem("d", 5, 5, 5, false),
	})
	if err != nil {
		t.Fatal(err)
	}
	ps, _ := collect3D(t, res)
	if len(ps) != 4 || res.BinsUsed() != 1 {
		t.Fatalf("got %d placements in %d bins, want 4 in 1", len(ps), res.BinsUsed())
	}
	assertNoOverlap(t, ps, 5, 5, 20)
	top := 0.0
	for _, p := range ps {
		if p.X != 0 || p.Y != 0 {
			t.Errorf("item off the column footprint at (%v,%v)", p.X, p.Y)
		}
		if z := p.Z + p.H; z > top {
			top = z
		}
	}
	if top != 20 {
		t.Errorf("column top = %v, want 20 (four 5-tall cubes stacked)", top)
	}
}

// Once a real P-footprint item anchors a column, smaller items may be fused into
// a level that *completely* tiles P and stacked on top. A 6×6×6 anchors the 6×6
// column (z=0), then four 3×3×6 fuse into a complete 6×6 level at z=6 — composing
// only because they fully form the profile above a real same-size item.
func TestColumnPacker_FusesCompleteLevelAboveAnchor(t *testing.T) {
	cp := d3.NewColumnPacker(6, 6, 12)
	items := []pack.Item{d3.NewItem("big", 6, 6, 6, false)}
	for i := 0; i < 4; i++ {
		items = append(items, d3.NewItem("s"+string(rune('0'+i)), 3, 3, 6, false))
	}
	res, err := cp.PackAll(items)
	if err != nil {
		t.Fatal(err)
	}
	ps, by := collect3D(t, res)
	if len(ps) != 5 || res.BinsUsed() != 1 {
		t.Fatalf("got %d placements in %d bins, want 5 in 1", len(ps), res.BinsUsed())
	}
	assertNoOverlap(t, ps, 6, 6, 12)
	if by["big"].Z != 0 {
		t.Errorf("big at z=%v, want 0 (seeds the column)", by["big"].Z)
	}
	for i := 0; i < 4; i++ {
		if s := by["s"+string(rune('0'+i))]; s.Z != 6 {
			t.Errorf("small s%d at z=%v, want 6 (fused slice above big)", i, s.Z)
		}
	}
}

// A mixed instance must produce a physically valid packing: every item placed,
// inside the bin, with no overlaps.
func TestColumnPacker_NoOverlapMixed(t *testing.T) {
	rng := uint64(0x123456789)
	next := func(n int) int {
		rng = rng*6364136223846793005 + 1442695040888963407
		return int((rng >> 33) % uint64(n))
	}
	sizes := []float64{2, 3, 4, 5, 6}
	var items []pack.Item
	for i := 0; i < 120; i++ {
		items = append(items, d3.NewItem("i"+string(rune('a'+i%26))+string(rune('0'+i/26)),
			sizes[next(len(sizes))], sizes[next(len(sizes))], sizes[next(len(sizes))], true))
	}
	cp := d3.NewColumnPacker(20, 20, 20)
	res, err := cp.PackAll(items)
	if err != nil {
		t.Fatal(err)
	}
	ps, _ := collect3D(t, res)
	if len(res.Unplaced) != 0 {
		t.Errorf("%d items unplaced, want 0", len(res.Unplaced))
	}
	if res.BinsUsed() == 0 {
		t.Fatal("no bins used")
	}
	// Check no-overlap per bin (placements in different bins share coordinates).
	byBin := map[string][]*d3.Placement3D{}
	for _, p := range ps {
		byBin[p.BinID()] = append(byBin[p.BinID()], p)
	}
	for _, group := range byBin {
		assertNoOverlap(t, group, 20, 20, 20)
	}
}
