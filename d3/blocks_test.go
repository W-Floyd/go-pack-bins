package d3_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/W-Floyd/go-pack-bins/d3"
	"github.com/W-Floyd/go-pack-bins/pack"
)

// collect3D converts a result's placements to *d3.Placement3D, keyed by item id.
func collect3D(t *testing.T, res pack.Result) ([]*d3.Placement3D, map[string]*d3.Placement3D) {
	t.Helper()
	var ps []*d3.Placement3D
	by := map[string]*d3.Placement3D{}
	for _, p := range res.Placements {
		pl, ok := p.(*d3.Placement3D)
		if !ok {
			t.Fatalf("placement %T is not *Placement3D", p)
		}
		ps = append(ps, pl)
		by[pl.ItemID()] = pl
	}
	return ps, by
}

// The headline case: A=8×8×8 fixes a height-8 layer; B=8×8×6 and C=8×8×2 fuse into
// an 8×8×8 stack (6+2=8) that sits beside A in a 16-wide bin. All three land in one
// 8-tall layer with no wasted height — the "collapse" the slab approach couldn't do
// reliably, here built explicitly as a vertical-stack block.
func TestBlockPacker_FusesStackToLayerHeight(t *testing.T) {
	bp := d3.NewBlockPacker(16, 8, 8)
	res, err := bp.PackAll([]pack.Item{
		d3.NewItem("A", 8, 8, 8, false),
		d3.NewItem("B", 8, 8, 6, false),
		d3.NewItem("C", 8, 8, 2, false),
	})
	if err != nil {
		t.Fatal(err)
	}
	ps, by := collect3D(t, res)
	if len(ps) != 3 || res.BinsUsed() != 1 {
		t.Fatalf("got %d placements in %d bins, want 3 in 1", len(ps), res.BinsUsed())
	}
	assertNoOverlap(t, ps, 16, 8, 8)
	top := 0.0
	for _, p := range ps {
		if z := p.Z + p.H; z > top {
			top = z
		}
	}
	if top != 8 {
		t.Errorf("stack top = %v, want 8 (B+C fused under A's height)", top)
	}
	if by["C"].Z != 6 {
		t.Errorf("C at z=%v, want 6 (stacked on B)", by["C"].Z)
	}
}

// Stacks must sum to the layer height exactly: four 4×4×2 items fuse two-by-two
// into 4×4×4 columns to fill a height-4 layer seeded by a 4×4×4 cube.
func TestBlockPacker_ExactHeightStacks(t *testing.T) {
	bp := d3.NewBlockPacker(8, 8, 4)
	items := []pack.Item{d3.NewItem("seed", 4, 4, 4, false)}
	for i := 0; i < 6; i++ {
		items = append(items, d3.NewItem("h"+string(rune('a'+i)), 4, 4, 2, false))
	}
	res, err := bp.PackAll(items)
	if err != nil {
		t.Fatal(err)
	}
	ps, _ := collect3D(t, res)
	if len(ps) != 7 {
		t.Fatalf("placed %d, want 7", len(ps))
	}
	assertNoOverlap(t, ps, 8, 8, 4)
	// One 8×8×4 bin holds the 4³ seed + six 4×4×2 (= three columns) → 4 footprint
	// cells of an 8×8 floor, all within height 4: a single bin, fully used.
	if res.BinsUsed() != 1 {
		t.Errorf("used %d bins, want 1", res.BinsUsed())
	}
	for _, p := range ps {
		if p.Z+p.H > 4+1e-9 {
			t.Errorf("box %s exceeds layer height: z=%v h=%v", p.ItemID(), p.Z, p.H)
		}
	}
}

// When a layer cell can't be filled to the exact layer height, the fallback tier
// drops in the tallest item that still fits rather than sealing the cell as a
// full-height void or spilling it into a new bin. Here a 8×4×10 fixes a height-10
// layer (filling half an 8×8 floor); a 8×4×7 has no exact-10 partner, so it must
// land in the other half of the same layer (z=0), keeping one bin.
func TestBlockPacker_FallbackFillsLayerGap(t *testing.T) {
	bp := d3.NewBlockPacker(8, 8, 10)
	res, err := bp.PackAll([]pack.Item{
		d3.NewItem("tall", 8, 4, 10, false),
		d3.NewItem("short", 8, 4, 7, false),
	})
	if err != nil {
		t.Fatal(err)
	}
	ps, by := collect3D(t, res)
	if res.BinsUsed() != 1 || len(ps) != 2 {
		t.Fatalf("got %d placements in %d bins, want 2 in 1 (fallback should fill the layer)", len(ps), res.BinsUsed())
	}
	assertNoOverlap(t, ps, 8, 8, 10)
	if by["short"].Z != 0 {
		t.Errorf("short item at z=%v, want 0 (fallback into the tall layer, not a new course)", by["short"].Z)
	}
}

// eightTallFourPerLayer builds 8 items of 5×5×9: four 5×5 footprints tile a 10×10
// floor, so each bin holds one height-9 layer of four, leaving a 1-tall shallow
// top (cap 1) that no 9-tall item can use.
func eightTallFourPerLayer() []pack.Item {
	its := make([]pack.Item, 0, 8)
	for i := 0; i < 8; i++ {
		its = append(its, d3.NewItem("T"+string(rune('a'+i)), 5, 5, 9, false))
	}
	return its
}

// While 9-tall items still remain, a bin's 1-tall shallow top must not be filled
// with the short 10×10×1 filler during the body phase — the filler is more useful
// later. It is only placed once the tall items are gone, backfilling a bin's top
// (z=9), rather than being stolen into a body layer's floor.
func TestBlockPacker_DefersShallowTopWhileTallItemsRemain(t *testing.T) {
	bp := d3.NewBlockPacker(10, 10, 10)
	items := append(eightTallFourPerLayer(), d3.NewItem("S", 10, 10, 1, false))
	res, err := bp.PackAll(items)
	if err != nil {
		t.Fatal(err)
	}
	ps, by := collect3D(t, res)
	if len(ps) != 9 || len(res.Unplaced) != 0 {
		t.Fatalf("placed %d (unplaced %d), want 9 / 0", len(ps), len(res.Unplaced))
	}
	if res.BinsUsed() != 2 {
		t.Fatalf("used %d bins, want 2", res.BinsUsed())
	}
	// The filler was deferred (never stolen into a tall layer's floor at z=0): it
	// ends up backfilling a bin's top once the tall items are placed.
	if by["S"].Z != 9 {
		t.Errorf("filler at z=%v, want 9 (deferred to a top, not stolen into a body floor)", by["S"].Z)
	}
}

// Once only short-enough items remain, the deferred shallow tops are revisited
// and backfilled: two fillers fill both bins' 1-tall tops (one each).
func TestBlockPacker_RevisitsDeferredTopWhenShortEnough(t *testing.T) {
	bp := d3.NewBlockPacker(10, 10, 10)
	items := append(eightTallFourPerLayer(),
		d3.NewItem("S0", 10, 10, 1, false),
		d3.NewItem("S1", 10, 10, 1, false))
	res, err := bp.PackAll(items)
	if err != nil {
		t.Fatal(err)
	}
	ps, by := collect3D(t, res)
	if len(ps) != 10 || len(res.Unplaced) != 0 {
		t.Fatalf("placed %d (unplaced %d), want 10 / 0", len(ps), len(res.Unplaced))
	}
	if res.BinsUsed() != 2 {
		t.Fatalf("used %d bins, want 2", res.BinsUsed())
	}
	// Both fillers placed at the top (z=9), one per bin — the deferred tops were
	// revisited once the tall items were gone.
	if by["S0"].Z != 9 || by["S1"].Z != 9 {
		t.Errorf("fillers at z=%v / %v, want both at 9 (deferred tops backfilled)", by["S0"].Z, by["S1"].Z)
	}
	if by["S0"].BinID() == by["S1"].BinID() {
		t.Errorf("both fillers in %s; want one in each bin's top", by["S0"].BinID())
	}
}

// On the final layer the packer lays items flat rather than fusing vertical
// stacks. Three same-footprint items (heights 6, 4, 2) all fit flat on the floor,
// so none should be stacked — even though 4+2 would fuse into a 6-tall column to
// match the tallest. (The old build stacked the 4 and 2; flat keeps both at z=0.)
func TestBlockPacker_FinalLayerPacksFlat(t *testing.T) {
	bp := d3.NewBlockPacker(8, 8, 10)
	res, err := bp.PackAll([]pack.Item{
		d3.NewItem("P", 4, 4, 6, false),
		d3.NewItem("Q", 4, 4, 4, false),
		d3.NewItem("R", 4, 4, 2, false),
	})
	if err != nil {
		t.Fatal(err)
	}
	ps, by := collect3D(t, res)
	if res.BinsUsed() != 1 || len(ps) != 3 {
		t.Fatalf("got %d placements in %d bins, want 3 in 1", len(ps), res.BinsUsed())
	}
	assertNoOverlap(t, ps, 8, 8, 10)
	for _, id := range []string{"P", "Q", "R"} {
		if by[id].Z != 0 {
			t.Errorf("%s at z=%v, want 0 (final layer laid flat, not stacked)", id, by[id].Z)
		}
	}
}

// When the items do not all fit flat on the floor, stacking is required and still
// happens: six 4×4×2 plus a 4×4×4 seed (footprint area 7×16 > the 8×8 floor) fuse
// into 4-tall columns as before.
func TestBlockPacker_StacksWhenFlatDoesNotFit(t *testing.T) {
	bp := d3.NewBlockPacker(8, 8, 4)
	items := []pack.Item{d3.NewItem("seed", 4, 4, 4, false)}
	for i := 0; i < 6; i++ {
		items = append(items, d3.NewItem("h"+string(rune('a'+i)), 4, 4, 2, false))
	}
	res, err := bp.PackAll(items)
	if err != nil {
		t.Fatal(err)
	}
	ps, _ := collect3D(t, res)
	if res.BinsUsed() != 1 || len(ps) != 7 {
		t.Fatalf("got %d placements in %d bins, want 7 in 1 (stacking required)", len(ps), res.BinsUsed())
	}
	stacked := false
	for _, p := range ps {
		if p.Z > 1e-9 {
			stacked = true
		}
	}
	if !stacked {
		t.Error("expected some items stacked above z=0 (flat would not fit)")
	}
	assertNoOverlap(t, ps, 8, 8, 4)
}

// The void-fill pre-pass slots a leftover item into an existing bin's well rather
// than opening a new bin for it. Two 10×10×8 items each fill a bin's floor leaving
// a 10×10×2 top well; a 10×10×2 leftover must drop into one of those wells, so the
// solve uses 2 bins, not 3.
func TestBlockPacker_VoidFillUsesExistingWells(t *testing.T) {
	bp := d3.NewBlockPacker(10, 10, 10)
	res, err := bp.PackAll([]pack.Item{
		d3.NewItem("A", 10, 10, 8, false),
		d3.NewItem("C", 10, 10, 8, false),
		d3.NewItem("D", 10, 10, 2, false),
	})
	if err != nil {
		t.Fatal(err)
	}
	ps, by := collect3D(t, res)
	if len(ps) != 3 || len(res.Unplaced) != 0 {
		t.Fatalf("placed %d (unplaced %d), want 3 / 0", len(ps), len(res.Unplaced))
	}
	if res.BinsUsed() != 2 {
		t.Errorf("used %d bins, want 2 (D should fill an existing well, not a new bin)", res.BinsUsed())
	}
	if by["D"].Z != 8 {
		t.Errorf("D at z=%v, want 8 (dropped into a top well above an 8-tall item)", by["D"].Z)
	}
	// Every box rests on the floor or another box — the void-fill must not float.
	for _, group := range groupByBin(ps) {
		assertNoOverlap(t, group, 10, 10, 10)
	}
}

func groupByBin(ps []*d3.Placement3D) map[string][]*d3.Placement3D {
	m := map[string][]*d3.Placement3D{}
	for _, p := range ps {
		m[p.BinID()] = append(m[p.BinID()], p)
	}
	return m
}

// The packing must be deterministic: identical input yields identical output,
// regardless of Go's randomized map iteration (block groups are keyed by
// footprint in a map, so they must be ordered before use).
func TestBlockPacker_Deterministic(t *testing.T) {
	sig := func() string {
		items := make([]pack.Item, 0, 60)
		for i := 0; i < 60; i++ {
			w := float64(1 + i%5)
			d := float64(1 + (i*7)%5)
			h := float64(1 + (i*3)%5)
			items = append(items, d3.NewItem("i"+string(rune('a'+i%26)), w, d, h, true))
		}
		res, err := d3.NewBlockPacker(9, 9, 9).PackAll(items)
		if err != nil {
			t.Fatal(err)
		}
		var b strings.Builder
		for _, p := range res.Placements {
			pl := p.(*d3.Placement3D)
			fmt.Fprintf(&b, "%s@%g,%g,%g:%g,%g,%g;", pl.ItemID(), pl.X, pl.Y, pl.Z, pl.W, pl.D, pl.H)
		}
		return b.String()
	}
	first := sig()
	for i := 0; i < 12; i++ {
		if got := sig(); got != first {
			t.Fatalf("run %d differs: non-deterministic packing", i)
		}
	}
}

func TestBlockPacker_StreamsAndNoOverlap(t *testing.T) {
	bp := d3.NewBlockPacker(10, 10, 10)
	var streamed int
	bp.Observe(func(pack.Placement) { streamed++ })

	items := make([]pack.Item, 0, 40)
	for i := 0; i < 40; i++ {
		w := float64(2 + i%4) // 2..5
		items = append(items, d3.NewItem("i"+string(rune('a'+i%26))+string(rune('0'+i/26)), w, w, w, true))
	}
	res, err := bp.PackAll(items)
	if err != nil {
		t.Fatal(err)
	}
	ps, _ := collect3D(t, res)
	if streamed != len(ps) {
		t.Errorf("observer fired %d times, want %d (one per placement)", streamed, len(ps))
	}
	if len(res.Unplaced) != 0 {
		t.Errorf("%d items unplaced", len(res.Unplaced))
	}
	// This packer spans several bins; placements are per-bin coordinates, so check
	// non-overlap within each bin.
	byBin := map[string][]*d3.Placement3D{}
	for _, p := range ps {
		byBin[p.BinID()] = append(byBin[p.BinID()], p)
	}
	for _, group := range byBin {
		assertNoOverlap(t, group, 10, 10, 10)
	}
}
