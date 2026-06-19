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
