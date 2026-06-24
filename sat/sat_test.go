package sat

import (
	"context"
	"errors"
	"testing"

	"github.com/W-Floyd/go-pack-bins/d2"
)

// validate checks that every item is placed, lies within its bin, and no two
// items sharing a bin overlap.
func validate(t *testing.T, r Result, items []*d2.Item2D, W, H float64) {
	t.Helper()
	const eps = 1e-9
	if len(r.Placements) != len(items) {
		t.Fatalf("got %d placements, want %d", len(r.Placements), len(items))
	}
	byBin := map[string][]*d2.Placement2D{}
	for i, p := range r.Placements {
		if p == nil {
			t.Fatalf("item %d unplaced", i)
		}
		pl := p.(*d2.Placement2D)
		if pl.X < -eps || pl.Y < -eps || pl.X+pl.W > W+eps || pl.Y+pl.H > H+eps {
			t.Errorf("item %d out of bin: x=%g y=%g w=%g h=%g (bin %g×%g)", i, pl.X, pl.Y, pl.W, pl.H, W, H)
		}
		byBin[pl.BinID()] = append(byBin[pl.BinID()], pl)
	}
	for bin, pls := range byBin {
		for i := 0; i < len(pls); i++ {
			for j := i + 1; j < len(pls); j++ {
				a, b := pls[i], pls[j]
				if a.X+a.W > b.X+eps && b.X+b.W > a.X+eps &&
					a.Y+a.H > b.Y+eps && b.Y+b.H > a.Y+eps {
					t.Errorf("overlap in %s: (%g,%g,%g,%g) vs (%g,%g,%g,%g)",
						bin, a.X, a.Y, a.W, a.H, b.X, b.Y, b.W, b.H)
				}
			}
		}
	}
}

func TestPerfectTiling(t *testing.T) {
	// Four 1×1 items in a 2×2 bin → exactly one bin, meets the area bound.
	items := []*d2.Item2D{
		d2.NewItem("a", 1, 1, false), d2.NewItem("b", 1, 1, false),
		d2.NewItem("c", 1, 1, false), d2.NewItem("d", 1, 1, false),
	}
	r, err := Pack2D(context.Background(), items, 2, 2, Options{SymmetryBreak: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.BinsUsed(); got != 1 {
		t.Errorf("BinsUsed=%d, want 1", got)
	}
	if !r.Optimal {
		t.Errorf("expected Optimal=true, proof=%q", r.Proof)
	}
	validate(t, r, items, 2, 2)
}

func TestQuarterItemsNeedFourBins(t *testing.T) {
	// Four 0.6×0.6 items in a 1×1 bin. Two don't fit side by side (1.2>1), so
	// each needs its own bin: optimum 4. Area bound is only 2, so this proves
	// 2 and 3 bins UNSAT.
	items := []*d2.Item2D{
		d2.NewItem("a", 0.6, 0.6, false), d2.NewItem("b", 0.6, 0.6, false),
		d2.NewItem("c", 0.6, 0.6, false), d2.NewItem("d", 0.6, 0.6, false),
	}
	r, err := Pack2D(context.Background(), items, 1, 1, Options{SymmetryBreak: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.BinsUsed(); got != 4 {
		t.Errorf("BinsUsed=%d, want 4 (proof=%q)", got, r.Proof)
	}
	if !r.Optimal {
		t.Errorf("expected Optimal=true")
	}
	if r.LowerBound != 2 {
		t.Errorf("LowerBound=%d, want 2 (area bound)", r.LowerBound)
	}
	validate(t, r, items, 1, 1)
}

func TestSideBySide(t *testing.T) {
	// Two 1×2 items in a 2×2 bin → one bin, placed side by side.
	items := []*d2.Item2D{
		d2.NewItem("a", 1, 2, false), d2.NewItem("b", 1, 2, false),
	}
	r, err := Pack2D(context.Background(), items, 2, 2, Options{SymmetryBreak: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.BinsUsed(); got != 1 {
		t.Errorf("BinsUsed=%d, want 1", got)
	}
	if !r.Optimal {
		t.Errorf("expected Optimal=true")
	}
	validate(t, r, items, 2, 2)
}

func TestRotationEnablesFit(t *testing.T) {
	// A 2×3 item only fits a 3×2 bin when rotated.
	items := []*d2.Item2D{d2.NewItem("a", 2, 3, true)}
	r, err := Pack2D(context.Background(), items, 3, 2, Options{AllowRotation: true, SymmetryBreak: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.BinsUsed(); got != 1 {
		t.Fatalf("BinsUsed=%d, want 1 (proof=%q)", got, r.Proof)
	}
	if !r.Placements[0].(*d2.Placement2D).Rotated {
		t.Errorf("expected the item to be rotated")
	}
	validate(t, r, items, 3, 2)
}

func TestRotationDisabledTooLarge(t *testing.T) {
	// Same 2×3 item, rotation disabled → cannot fit a 3×2 bin at all.
	items := []*d2.Item2D{d2.NewItem("a", 2, 3, true)}
	_, err := Pack2D(context.Background(), items, 3, 2, Options{AllowRotation: false})
	if !errors.Is(err, ErrItemTooLarge) {
		t.Fatalf("got %v, want ErrItemTooLarge", err)
	}
}

func TestNonIntegralRejected(t *testing.T) {
	items := []*d2.Item2D{d2.NewItem("a", 0.1234567, 0.5, false)}
	_, err := Pack2D(context.Background(), items, 1, 1, Options{})
	if !errors.Is(err, ErrNonIntegral) {
		t.Fatalf("got %v, want ErrNonIntegral", err)
	}
}

func TestSB2IdenticalItems(t *testing.T) {
	// 12 identical 3×3 items in a 6×6 bin: four per bin (2×2), so the optimum is 3
	// bins (meets the area bound). With many duplicates this is the case SB2 prunes;
	// the result must still be correct and certified.
	items := make([]*d2.Item2D, 12)
	for i := range items {
		items[i] = d2.NewItem(string(rune('a'+i)), 3, 3, false)
	}
	r, err := Pack2D(context.Background(), items, 6, 6, Options{SymmetryBreak: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.BinsUsed() != 3 {
		t.Errorf("BinsUsed=%d, want 3 (proof=%q)", r.BinsUsed(), r.Proof)
	}
	if !r.Optimal {
		t.Errorf("expected Optimal=true")
	}
	validate(t, r, items, 6, 6)

	// SB2 must not change the optimum vs symmetry breaking off.
	r2, err := Pack2D(context.Background(), items, 6, 6, Options{SymmetryBreak: false})
	if err != nil {
		t.Fatal(err)
	}
	if r2.BinsUsed() != r.BinsUsed() {
		t.Errorf("symmetry breaking changed the optimum: %d vs %d", r2.BinsUsed(), r.BinsUsed())
	}
}

func TestNormalPatternsShrinksUniformGrid(t *testing.T) {
	// 50 identical 100×100 items in a 1000×1000 bin. On the full integer grid this
	// formula (~5M clauses) would exceed the cap and fall back; the normal-pattern
	// reduction collapses positions to multiples of 100 (~11 per axis), so it now
	// solves and certifies — exactly the memory win the reduction delivers.
	items := make([]*d2.Item2D, 50)
	for i := range items {
		items[i] = d2.NewItem(string(rune('a'+i%26))+string(rune('0'+i/26)), 100, 100, false)
	}
	r, err := Pack2D(context.Background(), items, 1000, 1000, Options{SymmetryBreak: true})
	if err != nil {
		t.Fatalf("unexpected error (expected the reduction to make this tractable): %v", err)
	}
	if r.BinsUsed() != 1 || !r.Optimal { // 10×10 = 100 cells hold all 50
		t.Errorf("BinsUsed=%d Optimal=%v, want 1 / true (proof=%q)", r.BinsUsed(), r.Optimal, r.Proof)
	}
	validate(t, r, items, 1000, 1000)
}

func TestBuildAbortsOnActualCount(t *testing.T) {
	// The same instance must certify under a generous clause cap and fall back under
	// a tiny one — the decision is made on the actual count reached while building,
	// not on an a-priori estimate.
	items := make([]*d2.Item2D, 24)
	for i := range items {
		items[i] = d2.NewItem(string(rune('a'+i)), 5, 5, false)
	}
	big, err := Pack2D(context.Background(), items, 50, 50, Options{SymmetryBreak: true, MaxClauses: 50_000_000})
	if err != nil || !big.Optimal {
		t.Fatalf("generous cap should certify: err=%v optimal=%v", err, big.Optimal)
	}
	small, err := Pack2D(context.Background(), items, 50, 50, Options{SymmetryBreak: true, MaxClauses: 100})
	if !errors.Is(err, ErrGridTooLarge) {
		t.Fatalf("tiny cap should overflow: got err=%v", err)
	}
	if small.Optimal || len(small.Placements) != len(items) {
		t.Errorf("tiny-cap result should be an uncertified heuristic packing of all items")
	}
}

func TestLargeGridDegradesGracefully(t *testing.T) {
	// Distinct item sizes (1..60) make the subset-sum position sets dense (≈1830 per
	// axis), so even after the normal-pattern reduction the link clauses (~13M)
	// exceed the cap. The solver must fall back to the heuristic packing rather than
	// allocating a giant formula and exhausting memory.
	items := make([]*d2.Item2D, 60)
	for i := range items {
		s := float64(i + 1)
		items[i] = d2.NewItem(string(rune('a'+i%26))+string(rune('0'+i/26)), s, s, false)
	}
	r, err := Pack2D(context.Background(), items, 1850, 1850, Options{SymmetryBreak: true})
	if !errors.Is(err, ErrGridTooLarge) {
		t.Fatalf("got err=%v, want ErrGridTooLarge", err)
	}
	if r.Optimal {
		t.Errorf("fallback packing must not claim optimality")
	}
	if r.BinsUsed() < 1 || len(r.Placements) != len(items) {
		t.Errorf("expected a heuristic packing of all items, got bins=%d placements=%d", r.BinsUsed(), len(r.Placements))
	}
	validate(t, r, items, 1850, 1850)
}

func TestIncrementalMatchesBinarySearch(t *testing.T) {
	// The incremental and non-incremental strategies must reach the same optimum and
	// the same certificate on every instance — they only differ in how they search.
	type inst struct {
		name  string
		items []*d2.Item2D
		W, H  float64
	}
	mk := func(n int, w, h float64) []*d2.Item2D {
		out := make([]*d2.Item2D, n)
		for i := range out {
			out[i] = d2.NewItem(string(rune('a'+i)), w, h, true)
		}
		return out
	}
	cases := []inst{
		{"four-0.6-unit", mk(4, 0.6, 0.6), 1, 1}, // UNSAT-proof path → 4 bins
		{"perfect-tiling", mk(8, 1, 1), 2, 2},    // 2 bins, meets area bound
		{"mixed", []*d2.Item2D{d2.NewItem("a", 2, 3, true), d2.NewItem("b", 3, 2, true), d2.NewItem("c", 1, 1, true)}, 3, 3},
		{"ten-3x3", mk(10, 3, 3), 6, 6}, // 3 bins (4 per bin)
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			inc, err := Pack2D(context.Background(), c.items, c.W, c.H, Options{SymmetryBreak: true})
			if err != nil {
				t.Fatal(err)
			}
			bin, err := Pack2D(context.Background(), c.items, c.W, c.H, Options{SymmetryBreak: true, NonIncremental: true})
			if err != nil {
				t.Fatal(err)
			}
			if inc.BinsUsed() != bin.BinsUsed() {
				t.Errorf("bin count differs: incremental=%d binary=%d", inc.BinsUsed(), bin.BinsUsed())
			}
			if inc.Optimal != bin.Optimal || !inc.Optimal {
				t.Errorf("certificate differs/absent: incremental=%v binary=%v", inc.Optimal, bin.Optimal)
			}
			validate(t, inc, c.items, c.W, c.H)
		})
	}
}

func TestEmpty(t *testing.T) {
	r, err := Pack2D(context.Background(), nil, 1, 1, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r.BinsUsed() != 0 || !r.Optimal {
		t.Errorf("empty instance: bins=%d optimal=%v", r.BinsUsed(), r.Optimal)
	}
}
