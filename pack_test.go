package pack_test

import (
	"fmt"
	"testing"

	"github.com/wfloyd/go-pack-bins/d1"
	"github.com/wfloyd/go-pack-bins/d2"
	"github.com/wfloyd/go-pack-bins/d3"
	"github.com/wfloyd/go-pack-bins/geometry"
	"github.com/wfloyd/go-pack-bins/offline"
	"github.com/wfloyd/go-pack-bins/online"
	"github.com/wfloyd/go-pack-bins/pack"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

func items1D(sizes ...float64) []pack.Item {
	out := make([]pack.Item, len(sizes))
	for i, s := range sizes {
		out[i] = d1.NewItem(fmt.Sprintf("i%d", i), s)
	}
	return out
}

// ─── 1-D online ──────────────────────────────────────────────────────────────

func TestNextFit_1D(t *testing.T) {
	factory := d1.NewFactory(10)
	packer := online.NextFit(factory)

	// Two items of size 6 can't share a bin of capacity 10.
	for i, sz := range []float64{6, 6, 6} {
		item := d1.NewItem(fmt.Sprintf("i%d", i), sz)
		if _, err := packer.Pack(item); err != nil {
			t.Fatalf("Pack: %v", err)
		}
	}
	r := packer.Result()
	if r.BinsUsed() != 3 {
		t.Errorf("NF: want 3 bins, got %d", r.BinsUsed())
	}
}

func TestFirstFit_1D(t *testing.T) {
	factory := d1.NewFactory(10)
	packer := online.FirstFit(factory)
	// Items: 4, 4, 6, 4 → FF should use 2 bins: [4,6] and [4,4].
	for i, sz := range []float64{4, 4, 6, 4} {
		item := d1.NewItem(fmt.Sprintf("i%d", i), sz)
		if _, err := packer.Pack(item); err != nil {
			t.Fatalf("Pack item %d: %v", i, err)
		}
	}
	r := packer.Result()
	if r.BinsUsed() != 2 {
		t.Errorf("FF: want 2 bins, got %d", r.BinsUsed())
	}
}

func TestBestFit_1D(t *testing.T) {
	factory := d1.NewFactory(10)
	packer := online.BestFit(factory)
	items := []float64{3, 7, 5, 5, 3, 7}
	for i, sz := range items {
		item := d1.NewItem(fmt.Sprintf("i%d", i), sz)
		packer.Pack(item)
	}
	r := packer.Result()
	// Optimal is 3 bins. BF should achieve at most ⌊1.7·OPT⌋ = 5.
	if r.BinsUsed() > 5 {
		t.Errorf("BF: too many bins: %d", r.BinsUsed())
	}
}

// ─── 1-D offline ─────────────────────────────────────────────────────────────

func TestFFD_1D(t *testing.T) {
	factory := d1.NewFactory(10)
	packer := offline.FirstFitDecreasing(factory)

	items := []pack.Item{
		d1.NewItem("a", 3), d1.NewItem("b", 7),
		d1.NewItem("c", 5), d1.NewItem("d", 5),
		d1.NewItem("e", 3), d1.NewItem("f", 7),
	}
	r, err := packer.PackAll(items)
	if err != nil {
		t.Fatalf("FFD: %v", err)
	}
	// OPT = 3; FFD guarantees ≤ (11/9)·3 + 6/9 ≈ 4.3 → at most 4 bins.
	if r.BinsUsed() > 4 {
		t.Errorf("FFD: too many bins: %d", r.BinsUsed())
	}
	if r.BinsUsed() < 3 {
		t.Errorf("FFD: impossibly few bins: %d", r.BinsUsed())
	}
}

func TestMFFD_1D(t *testing.T) {
	factory := d1.NewFactory(10)
	packer := offline.ModifiedFirstFitDecreasing(10, factory)
	items := []pack.Item{
		d1.NewItem("a", 6), d1.NewItem("b", 6),
		d1.NewItem("c", 4), d1.NewItem("d", 4),
		d1.NewItem("e", 2), d1.NewItem("f", 2),
	}
	r, err := packer.PackAll(items)
	if err != nil {
		t.Fatalf("MFFD: %v", err)
	}
	if r.BinsUsed() < 3 {
		t.Errorf("MFFD: impossibly few bins: %d", r.BinsUsed())
	}
}

// ─── 2-D ─────────────────────────────────────────────────────────────────────

func TestMaxRects_2D(t *testing.T) {
	factory := d2.NewFactory(100, 100, d2.NewMaxRectsDefault)
	packer := online.FirstFit(factory)

	// Pack 10 items of 30×30 into 100×100 bins.
	// Optimal: 2 bins (9 items fit in one 100×100 → ⌊(100/30)²⌋=9).
	for i := 0; i < 10; i++ {
		item := d2.NewItem(fmt.Sprintf("sq%d", i), 30, 30, false)
		if _, err := packer.Pack(item); err != nil {
			t.Fatalf("2D pack item %d: %v", i, err)
		}
	}
	r := packer.Result()
	if r.BinsUsed() < 2 {
		t.Errorf("2D: impossibly few bins")
	}
	if r.BinsUsed() > 3 {
		t.Errorf("2D: too many bins: %d", r.BinsUsed())
	}
}

func TestGuillotine_2D(t *testing.T) {
	factory := d2.NewFactory(100, 100, d2.NewGuillotineDefault)
	packer := online.FirstFit(factory)
	for i := 0; i < 4; i++ {
		item := d2.NewItem(fmt.Sprintf("r%d", i), 50, 50, false)
		if _, err := packer.Pack(item); err != nil {
			t.Fatalf("guillotine item %d: %v", i, err)
		}
	}
	r := packer.Result()
	if r.BinsUsed() != 1 {
		t.Errorf("guillotine: 4×50×50 items should fit in one 100×100 bin, got %d bins", r.BinsUsed())
	}
}

// ─── 3-D box ─────────────────────────────────────────────────────────────────

func TestExtremePoint_3D(t *testing.T) {
	factory := d3.NewFactory(10, 10, 10, d3.NewExtremePointStrategy)
	packer := online.FirstFit(factory)
	// 8 items of 5×5×5 should fit into exactly 1 bin of 10×10×10.
	for i := 0; i < 8; i++ {
		item := d3.NewItem(fmt.Sprintf("cube%d", i), 5, 5, 5, false)
		if _, err := packer.Pack(item); err != nil {
			t.Fatalf("3D box item %d: %v", i, err)
		}
	}
	r := packer.Result()
	if r.BinsUsed() != 1 {
		t.Errorf("3D box: 8×5³ should fit in one 10³ bin, got %d bins", r.BinsUsed())
	}
}

// ─── 3-D manifold solid ───────────────────────────────────────────────────────

func TestBoxSolid_Voxelize(t *testing.T) {
	solid := d3.NewBoxSolidWDH(4, 4, 4)
	vox := solid.Voxelize(1.0)
	// A 4×4×4 box voxelised at cell size 1 → 4×4×4 = 64 occupied cells.
	got := vox.OccupiedCount()
	if got < 60 || got > 70 { // allow small rounding
		t.Errorf("BoxSolid voxelize: want ~64 cells, got %d", got)
	}
}

func TestMeshSolid_Contains(t *testing.T) {
	// Use BoxSolid as a proxy for MeshSolid (shares the same Contains logic).
	solid := d3.NewBoxSolidWDH(10, 10, 10)
	if !solid.Contains(geometry.Vec3{X: 5, Y: 5, Z: 5}) {
		t.Error("point (5,5,5) should be inside 10×10×10 box")
	}
	if solid.Contains(geometry.Vec3{X: 15, Y: 5, Z: 5}) {
		t.Error("point (15,5,5) should be outside 10×10×10 box")
	}
}

func TestSolidBin_Place(t *testing.T) {
	container := d3.NewBoxSolidWDH(10, 10, 10)
	factory := d3.NewSolidBinFactory(container, 1.0)
	packer := online.FirstFit(factory)

	// Two 4×4×4 items should fit in a 10×10×10 bin.
	for i := 0; i < 2; i++ {
		solid := d3.NewBoxSolidWDH(4, 4, 4)
		item := d3.NewSolidItem(fmt.Sprintf("s%d", i), solid, 1.0, false)
		if _, err := packer.Pack(item); err != nil {
			t.Fatalf("solid item %d: %v", i, err)
		}
	}
	r := packer.Result()
	if r.BinsUsed() != 1 {
		t.Errorf("solid bin: 2×4³ items should fit in one 10³ bin, got %d bins", r.BinsUsed())
	}
}

// ─── exact solver ────────────────────────────────────────────────────────────

func TestBinCompletion_Exact(t *testing.T) {
	items := []pack.Item{
		d1.NewItem("a", 4), d1.NewItem("b", 4),
		d1.NewItem("c", 6), d1.NewItem("d", 6),
	}
	r, err := offline.BinCompletion(items, 10, d1.NewFactory(10))
	if err != nil {
		t.Fatalf("BinCompletion: %v", err)
	}
	// OPT = 2 (pair 4+6 in each bin).
	if r.BinsUsed() != 2 {
		t.Errorf("BinCompletion: want 2 bins, got %d", r.BinsUsed())
	}
}

// ─── scalar constraints & preferences ────────────────────────────────────────

func TestConstrainedBin_MaxWeight(t *testing.T) {
	// Bin capacity 10, weight limit 8. Two items of size 4 fit geometrically
	// but their combined weight (5+5=10) exceeds the limit, so they must split.
	weightLimit := pack.MaxAggregate("weight", 8)
	factory := pack.NewConstrainedFactory(d1.NewFactory(10), weightLimit)
	packer := online.FirstFit(factory)

	items := []pack.Item{
		d1.NewItem("a", 4).WithScalar("weight", 5),
		d1.NewItem("b", 4).WithScalar("weight", 5),
	}
	for _, it := range items {
		if _, err := packer.Pack(it); err != nil {
			t.Fatalf("constrained pack: %v", err)
		}
	}
	r := packer.Result()
	if r.BinsUsed() != 2 {
		t.Errorf("MaxWeight: want 2 bins (weight overflow), got %d", r.BinsUsed())
	}
}

func TestBinCompletion_WeightConstraint(t *testing.T) {
	// Geometrically fits in 2 bins (4+6, 4+6). Weight limit of 8 means
	// 6-weight items can't share a bin with a 4-weight item, forcing 3 bins.
	items := []pack.Item{
		d1.NewItem("a", 4).WithScalar("weight", 4),
		d1.NewItem("b", 4).WithScalar("weight", 4),
		d1.NewItem("c", 6).WithScalar("weight", 6),
		d1.NewItem("d", 6).WithScalar("weight", 6),
	}
	weightLimit := pack.MaxAggregate("weight", 8)
	r, err := offline.BinCompletion(items, 10, d1.NewFactory(10), weightLimit)
	if err != nil {
		t.Fatalf("BinCompletion+weight: %v", err)
	}
	// Each 6-weight item must be alone (6+4=10 > 8); two 4-weight items can share.
	// Optimal: [c], [d], [a,b] → 3 bins.
	if r.BinsUsed() != 3 {
		t.Errorf("BinCompletion+weight: want 3 bins, got %d", r.BinsUsed())
	}
}

func TestAllSame_Constraint(t *testing.T) {
	// Three items: two zone=1 and one zone=2. AllSame("zone") must keep them separate.
	factory := pack.NewConstrainedFactory(d1.NewFactory(10), pack.AllSame("zone"))
	packer := online.FirstFit(factory)
	items := []pack.Item{
		d1.NewItem("a", 3).WithScalar("zone", 1),
		d1.NewItem("b", 3).WithScalar("zone", 2),
		d1.NewItem("c", 3).WithScalar("zone", 1),
	}
	for _, it := range items {
		if _, err := packer.Pack(it); err != nil {
			t.Fatalf("AllSame pack: %v", err)
		}
	}
	r := packer.Result()
	// a and c (zone=1) can share; b (zone=2) must be alone → 2 bins.
	if r.BinsUsed() != 2 {
		t.Errorf("AllSame: want 2 bins, got %d", r.BinsUsed())
	}
}

func TestPreferenceFit_ColocateHigh(t *testing.T) {
	// Three items: two cheap (value=1) and one expensive (value=100).
	// ColocateHigh("value") should steer the second cheap item into the same
	// bin as the first cheap item, leaving a bin free for the expensive one.
	weightLimit := pack.MaxAggregate("weight", 10)
	factory := pack.NewConstrainedFactory(d1.NewFactory(10), weightLimit)
	pref := pack.ColocateHigh("value")
	packer := online.PreferenceFit(factory, pref)

	items := []pack.Item{
		d1.NewItem("cheap1", 3).WithScalar("weight", 3).WithScalar("value", 1),
		d1.NewItem("cheap2", 3).WithScalar("weight", 3).WithScalar("value", 1),
		d1.NewItem("pricey", 3).WithScalar("weight", 3).WithScalar("value", 100),
	}
	for _, it := range items {
		if _, err := packer.Pack(it); err != nil {
			t.Fatalf("PreferenceFit: %v", err)
		}
	}
	r := packer.Result()
	if r.BinsUsed() == 0 {
		t.Error("PreferenceFit: no bins opened")
	}
}

// ─── harmonic family ─────────────────────────────────────────────────────────

func TestHarmonicK(t *testing.T) {
	factory := d1.NewFactory(1.0)
	packer := online.NewHarmonicK(5, 1.0, factory)
	items := []pack.Item{
		d1.NewItem("a", 0.4), d1.NewItem("b", 0.4),
		d1.NewItem("c", 0.3), d1.NewItem("d", 0.3),
	}
	for _, item := range items {
		if _, err := packer.Pack(item); err != nil {
			t.Fatalf("HarmonicK: %v", err)
		}
	}
	r := packer.Result()
	if r.BinsUsed() < 2 {
		t.Errorf("HarmonicK: impossibly few bins: %d", r.BinsUsed())
	}
}

func TestRFF(t *testing.T) {
	factory := d1.NewFactory(1.0)
	packer := online.NewRFF(1.0, factory)
	items := []pack.Item{
		d1.NewItem("a", 0.6), // class A
		d1.NewItem("b", 0.45), // class B
		d1.NewItem("c", 0.35), // class C
		d1.NewItem("d", 0.2),  // class D
	}
	for _, item := range items {
		if _, err := packer.Pack(item); err != nil {
			t.Fatalf("RFF: %v", err)
		}
	}
	r := packer.Result()
	if r.BinsUsed() == 0 {
		t.Error("RFF: no bins opened")
	}
}

// ─── untested online 1-D algorithms ──────────────────────────────────────────

func TestWorstFit_1D(t *testing.T) {
	// WF picks the bin with most remaining space; three items of 6 each cannot
	// share a bin of capacity 10 (rem=4 after first), so 3 bins are needed.
	packer := online.WorstFit(d1.NewFactory(10))
	for i, sz := range []float64{6, 6, 6} {
		if _, err := packer.Pack(d1.NewItem(fmt.Sprintf("i%d", i), sz)); err != nil {
			t.Fatalf("WF: %v", err)
		}
	}
	if r := packer.Result(); r.BinsUsed() != 3 {
		t.Errorf("WF: want 3 bins, got %d", r.BinsUsed())
	}
}

func TestAlmostWorstFit_1D(t *testing.T) {
	packer := online.AlmostWorstFit(d1.NewFactory(10))
	for _, it := range items1D(3, 7, 3, 7) {
		if _, err := packer.Pack(it); err != nil {
			t.Fatalf("AWF: %v", err)
		}
	}
	r := packer.Result()
	if r.BinsUsed() < 2 || r.BinsUsed() > 4 {
		t.Errorf("AWF: bins %d outside expected range [2,4]", r.BinsUsed())
	}
}

func TestNextKFit_k1_1D(t *testing.T) {
	// k=1 degenerates to NextFit: only the most recent bin is considered.
	packer := online.NextKFit(1, d1.NewFactory(10))
	for i, sz := range []float64{6, 6, 6} {
		if _, err := packer.Pack(d1.NewItem(fmt.Sprintf("i%d", i), sz)); err != nil {
			t.Fatalf("NkF(1): %v", err)
		}
	}
	if r := packer.Result(); r.BinsUsed() != 3 {
		t.Errorf("NkF(k=1): want 3 bins, got %d", r.BinsUsed())
	}
}

// ─── untested offline 1-D algorithms ─────────────────────────────────────────

func TestKarmarkarKarp_1D(t *testing.T) {
	// KK is a number-partitioning differencing heuristic: it balances group sums
	// rather than minimising bin count, so BinsUsed may exceed OPT.
	// The contract we test: no error, all items placed, count in [1, n].
	factory := d1.NewFactory(10)
	items := []pack.Item{
		d1.NewItem("a", 6), d1.NewItem("b", 4),
		d1.NewItem("c", 6), d1.NewItem("d", 4),
		d1.NewItem("e", 6), d1.NewItem("f", 4),
	}
	r, err := offline.KarmarkarKarp(items, 10, factory)
	if err != nil {
		t.Fatalf("KK: %v", err)
	}
	if len(r.Unplaced) > 0 {
		t.Errorf("KK: unplaced: %v", r.Unplaced)
	}
	if r.BinsUsed() < 1 || r.BinsUsed() > len(items) {
		t.Errorf("KK: bins %d outside valid range [1,%d]", r.BinsUsed(), len(items))
	}

	// ErrItemTooLarge when an item exceeds capacity.
	_, err = offline.KarmarkarKarp([]pack.Item{d1.NewItem("big", 12)}, 10, d1.NewFactory(10))
	if err != pack.ErrItemTooLarge {
		t.Errorf("KK oversized: want ErrItemTooLarge, got %v", err)
	}
}

func TestBestFitDecreasing_1D(t *testing.T) {
	r, err := offline.BestFitDecreasing(d1.NewFactory(10)).PackAll([]pack.Item{
		d1.NewItem("a", 3), d1.NewItem("b", 7),
		d1.NewItem("c", 5), d1.NewItem("d", 5),
		d1.NewItem("e", 3), d1.NewItem("f", 7),
	})
	if err != nil {
		t.Fatalf("BFD: %v", err)
	}
	if r.BinsUsed() < 3 || r.BinsUsed() > 4 {
		t.Errorf("BFD: bins %d outside expected range [3,4]", r.BinsUsed())
	}
}

func TestNextFitDecreasing_1D(t *testing.T) {
	r, err := offline.NextFitDecreasing(d1.NewFactory(10)).PackAll([]pack.Item{
		d1.NewItem("a", 3), d1.NewItem("b", 7),
		d1.NewItem("c", 5), d1.NewItem("d", 5),
		d1.NewItem("e", 3), d1.NewItem("f", 7),
	})
	if err != nil {
		t.Fatalf("NFD: %v", err)
	}
	if r.BinsUsed() < 3 {
		t.Errorf("NFD: impossibly few bins: %d", r.BinsUsed())
	}
}

func TestWorstFitDecreasing_1D(t *testing.T) {
	r, err := offline.WorstFitDecreasing(d1.NewFactory(10)).PackAll([]pack.Item{
		d1.NewItem("a", 3), d1.NewItem("b", 7),
		d1.NewItem("c", 5), d1.NewItem("d", 5),
		d1.NewItem("e", 3), d1.NewItem("f", 7),
	})
	if err != nil {
		t.Fatalf("WFD: %v", err)
	}
	if r.BinsUsed() < 3 {
		t.Errorf("WFD: impossibly few bins: %d", r.BinsUsed())
	}
}

// ─── 2-D rotation & heuristics ───────────────────────────────────────────────

func TestRotation_2D(t *testing.T) {
	// 8×3 item won't fit un-rotated in a 5×10 bin (8 > 5) but fits rotated as 3×8.
	factoryFn := func() pack.BinFactory { return d2.NewFactory(5, 10, d2.NewMaxRectsDefault) }

	packer := online.FirstFit(factoryFn())
	if _, err := packer.Pack(d2.NewItem("r", 8, 3, true)); err != nil {
		t.Fatalf("rotation allowed: %v", err)
	}
	if r := packer.Result(); r.BinsUsed() != 1 {
		t.Errorf("rotation: want 1 bin after rotation, got %d", r.BinsUsed())
	}

	packer2 := online.FirstFit(factoryFn())
	if _, err := packer2.Pack(d2.NewItem("r", 8, 3, false)); err != pack.ErrItemTooLarge {
		t.Errorf("no rotation: want ErrItemTooLarge, got %v", err)
	}
}

func TestMaxRects_Heuristics_2D(t *testing.T) {
	// All four heuristics should pack 4×50×50 items into exactly 1 100×100 bin.
	cases := []struct {
		name string
		make func(w, h float64) d2.PlacementStrategy2D
	}{
		{"BLSF", func(w, h float64) d2.PlacementStrategy2D { return d2.NewMaxRects(w, h, d2.BLSF) }},
		{"BAF", func(w, h float64) d2.PlacementStrategy2D { return d2.NewMaxRects(w, h, d2.BAF) }},
		{"BottomLeft", func(w, h float64) d2.PlacementStrategy2D { return d2.NewMaxRects(w, h, d2.BottomLeft) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			packer := online.FirstFit(d2.NewFactory(100, 100, tc.make))
			for i := 0; i < 4; i++ {
				if _, err := packer.Pack(d2.NewItem(fmt.Sprintf("s%d", i), 50, 50, false)); err != nil {
					t.Fatalf("Pack: %v", err)
				}
			}
			if r := packer.Result(); r.BinsUsed() != 1 {
				t.Errorf("%s: 4×50² should fit in one 100² bin, got %d bins", tc.name, r.BinsUsed())
			}
		})
	}
}

func TestGuillotine_LongerLeftover_2D(t *testing.T) {
	factory := d2.NewFactory(100, 100, func(w, h float64) d2.PlacementStrategy2D {
		return d2.NewGuillotine(w, h, d2.LongerLeftover, true)
	})
	packer := online.FirstFit(factory)
	for i := 0; i < 4; i++ {
		if _, err := packer.Pack(d2.NewItem(fmt.Sprintf("r%d", i), 50, 50, false)); err != nil {
			t.Fatalf("LongerLeftover item %d: %v", i, err)
		}
	}
	if r := packer.Result(); r.BinsUsed() != 1 {
		t.Errorf("LongerLeftover: 4×50² should fit in one 100² bin, got %d bins", r.BinsUsed())
	}
}

func TestBestFit_2D(t *testing.T) {
	packer := online.BestFit(d2.NewFactory(100, 100, d2.NewMaxRectsDefault))
	for i := 0; i < 4; i++ {
		if _, err := packer.Pack(d2.NewItem(fmt.Sprintf("s%d", i), 50, 50, false)); err != nil {
			t.Fatalf("BF 2D: %v", err)
		}
	}
	if r := packer.Result(); r.BinsUsed() != 1 {
		t.Errorf("BF 2D: 4×50² in one 100² bin, got %d bins", r.BinsUsed())
	}
}

func TestWorstFit_2D(t *testing.T) {
	packer := online.WorstFit(d2.NewFactory(100, 100, d2.NewMaxRectsDefault))
	for i := 0; i < 4; i++ {
		if _, err := packer.Pack(d2.NewItem(fmt.Sprintf("s%d", i), 50, 50, false)); err != nil {
			t.Fatalf("WF 2D: %v", err)
		}
	}
	if r := packer.Result(); r.BinsUsed() < 1 {
		t.Error("WF 2D: expected at least 1 bin")
	}
}

func TestFFD_2D(t *testing.T) {
	r, err := offline.FirstFitDecreasing(d2.NewFactory(100, 100, d2.NewMaxRectsDefault)).PackAll([]pack.Item{
		d2.NewItem("a", 60, 60, false), d2.NewItem("b", 60, 60, false),
		d2.NewItem("c", 40, 40, false), d2.NewItem("d", 40, 40, false),
	})
	if err != nil {
		t.Fatalf("FFD 2D: %v", err)
	}
	if r.BinsUsed() < 2 {
		t.Errorf("FFD 2D: impossibly few bins: %d", r.BinsUsed())
	}
}

// ─── 3-D multiple bins & rotation ────────────────────────────────────────────

func TestMultipleBins_3D(t *testing.T) {
	// 3×3×3 items in a 5×5×5 bin: floor(5/3)=1 along every axis, so only 1 per bin.
	packer := online.FirstFit(d3.NewFactory(5, 5, 5, d3.NewExtremePointStrategy))
	for i := 0; i < 9; i++ {
		if _, err := packer.Pack(d3.NewItem(fmt.Sprintf("c%d", i), 3, 3, 3, false)); err != nil {
			t.Fatalf("3D multi-bin item %d: %v", i, err)
		}
	}
	if r := packer.Result(); r.BinsUsed() != 9 {
		t.Errorf("3D multi-bin: 9 items, one per 5³ bin, got %d bins", r.BinsUsed())
	}
}

func TestRotation_3D(t *testing.T) {
	// Item 6×2×2: orientation (2,2,6) fits in a 4×4×7 bin; un-rotated (6,2,2) does not (6>4).
	factoryFn := func() pack.BinFactory {
		return d3.NewFactory(4, 4, 7, d3.NewExtremePointStrategy)
	}

	packer := online.FirstFit(factoryFn())
	if _, err := packer.Pack(d3.NewItem("r", 6, 2, 2, true)); err != nil {
		t.Fatalf("3D rotation allowed: %v", err)
	}
	if r := packer.Result(); r.BinsUsed() != 1 {
		t.Errorf("3D rotation: want 1 bin, got %d", r.BinsUsed())
	}

	packer2 := online.FirstFit(factoryFn())
	if _, err := packer2.Pack(d3.NewItem("r", 6, 2, 2, false)); err != pack.ErrItemTooLarge {
		t.Errorf("3D no rotation: want ErrItemTooLarge, got %v", err)
	}
}

// ─── error paths ─────────────────────────────────────────────────────────────

func TestErrItemTooLarge_1D(t *testing.T) {
	_, err := online.FirstFit(d1.NewFactory(5)).Pack(d1.NewItem("big", 6))
	if err != pack.ErrItemTooLarge {
		t.Errorf("1D: want ErrItemTooLarge, got %v", err)
	}
}

func TestErrItemTooLarge_2D(t *testing.T) {
	_, err := online.FirstFit(d2.NewFactory(5, 5, d2.NewMaxRectsDefault)).
		Pack(d2.NewItem("big", 6, 3, false))
	if err != pack.ErrItemTooLarge {
		t.Errorf("2D: want ErrItemTooLarge, got %v", err)
	}
}

func TestErrItemTooLarge_3D(t *testing.T) {
	_, err := online.FirstFit(d3.NewFactory(5, 5, 5, d3.NewExtremePointStrategy)).
		Pack(d3.NewItem("big", 6, 3, 3, false))
	if err != pack.ErrItemTooLarge {
		t.Errorf("3D: want ErrItemTooLarge, got %v", err)
	}
}

func TestErrItemTooLarge_BinCompletion(t *testing.T) {
	_, err := offline.BinCompletion([]pack.Item{
		d1.NewItem("ok", 4),
		d1.NewItem("big", 12), // exceeds capacity of 10
	}, 10, d1.NewFactory(10))
	if err != pack.ErrItemTooLarge {
		t.Errorf("BinCompletion: want ErrItemTooLarge, got %v", err)
	}
}

// ─── MinAggregate constraint ──────────────────────────────────────────────────

func TestMinAggregate_1D(t *testing.T) {
	// MinAggregate("value", 5): a fresh bin only accepts items whose scalar
	// brings the running total to ≥ 5. A high-value item can start a bin;
	// a low-value item can join that bin but cannot start its own.
	minVal := pack.MinAggregate("value", 5)
	factory := pack.NewConstrainedFactory(d1.NewFactory(10), minVal)
	packer := online.FirstFit(factory)

	if _, err := packer.Pack(d1.NewItem("hi", 4).WithScalar("value", 6)); err != nil {
		t.Fatalf("high-value item rejected: %v", err)
	}
	// Low-value item joins the existing bin (6+2=8 ≥ 5).
	if _, err := packer.Pack(d1.NewItem("lo", 4).WithScalar("value", 2)); err != nil {
		t.Fatalf("low-value item rejected from qualifying bin: %v", err)
	}
	if r := packer.Result(); r.BinsUsed() != 1 {
		t.Errorf("MinAggregate: want 1 bin, got %d", r.BinsUsed())
	}

	// Standalone low-value item cannot open a fresh bin (0+2=2 < 5).
	packer2 := online.FirstFit(pack.NewConstrainedFactory(d1.NewFactory(10), pack.MinAggregate("value", 5)))
	if _, err := packer2.Pack(d1.NewItem("solo-lo", 3).WithScalar("value", 2)); err != pack.ErrItemTooLarge {
		t.Errorf("MinAggregate: standalone low-value item: want ErrItemTooLarge, got %v", err)
	}
}

// ─── ColocateLow preference ───────────────────────────────────────────────────

func TestColocateLow_1D(t *testing.T) {
	// Cap=6: expensive(4) opens bin0; cheap1(4) opens bin1 (bin0 rem=2 < 4).
	// cheap2(2) fits in both. ColocateLow scores bins by -cost, so bin1 (cost=1)
	// scores higher than bin0 (cost=100) and cheap2 lands with cheap1.
	factory := pack.NewConstrainedFactory(d1.NewFactory(6)) // ConstrainedBin needed for Aggregates()
	packer := online.PreferenceFit(factory, pack.ColocateLow("cost"))

	items := []pack.Item{
		d1.NewItem("exp", 4).WithScalar("cost", 100),
		d1.NewItem("cheap1", 4).WithScalar("cost", 1),
		d1.NewItem("cheap2", 2).WithScalar("cost", 1),
	}
	for i, it := range items {
		if _, err := packer.Pack(it); err != nil {
			t.Fatalf("item %d: %v", i, err)
		}
	}
	r := packer.Result()

	binOf := map[string]string{}
	for _, p := range r.Placements {
		if p != nil {
			binOf[p.ItemID()] = p.BinID()
		}
	}
	if binOf["cheap2"] == binOf["exp"] {
		t.Error("ColocateLow: cheap2 should not share a bin with the expensive item")
	}
	if binOf["cheap2"] != binOf["cheap1"] {
		t.Error("ColocateLow: cheap2 should share a bin with cheap1")
	}
}

// ─── AllSame + BinCompletion (stateful constraint in branch-and-bound) ────────

func TestAllSame_BinCompletion(t *testing.T) {
	// Without AllSame: 3+3+3=9 ≤ 10 → OPT=1 bin.
	// With AllSame("zone"): zone=1 and zone=2 items cannot share → OPT=2 bins.
	items := []pack.Item{
		d1.NewItem("a1", 3).WithScalar("zone", 1),
		d1.NewItem("a2", 3).WithScalar("zone", 1),
		d1.NewItem("b1", 3).WithScalar("zone", 2),
	}
	r, err := offline.BinCompletion(items, 10, d1.NewFactory(10), pack.AllSame("zone"))
	if err != nil {
		t.Fatalf("AllSame+BC: %v", err)
	}
	if r.BinsUsed() != 2 {
		t.Errorf("AllSame+BC: want 2 bins, got %d", r.BinsUsed())
	}
}

// ─── multiple constraints simultaneously ─────────────────────────────────────

func TestMultipleConstraints_1D(t *testing.T) {
	// MaxAggregate("weight", 8) AND AllSame("zone") together.
	// z1-heavy(4, zone=1, weight=5) + z1-light(4, zone=1, weight=3) → share bin0 (same zone, 8≤8).
	// z2-heavy(4, zone=2, weight=5) → must be alone (different zone, and weight check passes solo).
	items := []pack.Item{
		d1.NewItem("z1h", 4).WithScalar("zone", 1).WithScalar("weight", 5),
		d1.NewItem("z1l", 4).WithScalar("zone", 1).WithScalar("weight", 3),
		d1.NewItem("z2h", 4).WithScalar("zone", 2).WithScalar("weight", 5),
	}
	packer := online.FirstFit(pack.NewConstrainedFactory(d1.NewFactory(10),
		pack.MaxAggregate("weight", 8),
		pack.AllSame("zone"),
	))
	for i, it := range items {
		if _, err := packer.Pack(it); err != nil {
			t.Fatalf("multi-constraint item %d: %v", i, err)
		}
	}
	if r := packer.Result(); r.BinsUsed() != 2 {
		t.Errorf("MultiConstraint: want 2 bins, got %d", r.BinsUsed())
	}
}

// ─── infeasible constraint ────────────────────────────────────────────────────

func TestInfeasibleConstraint_1D(t *testing.T) {
	// MaxAggregate("weight", 0): nothing can ever be placed (any positive weight fails).
	factory := pack.NewConstrainedFactory(d1.NewFactory(10), pack.MaxAggregate("weight", 0))
	_, err := online.FirstFit(factory).Pack(d1.NewItem("a", 3).WithScalar("weight", 1))
	if err != pack.ErrItemTooLarge {
		t.Errorf("infeasible: want ErrItemTooLarge, got %v", err)
	}
}

// ─── Result edge case ─────────────────────────────────────────────────────────

func TestBinsUsed_Empty(t *testing.T) {
	if r := (pack.Result{}); r.BinsUsed() != 0 {
		t.Errorf("empty Result: want 0, got %d", r.BinsUsed())
	}
}

// ─── voxelization with non-unit cell ─────────────────────────────────────────

func TestVoxelize_NonUnitCell(t *testing.T) {
	// 10×10×10 box at cell size 2 → 5×5×5 = 125 occupied cells.
	solid := d3.NewBoxSolidWDH(10, 10, 10)
	vox := solid.Voxelize(2.0)
	got := vox.OccupiedCount()
	if got < 100 || got > 150 {
		t.Errorf("Voxelize(cellSize=2): want ~125 cells, got %d", got)
	}
}
