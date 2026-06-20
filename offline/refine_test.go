package offline_test

import (
	"testing"

	"github.com/W-Floyd/go-pack-bins/d1"
	"github.com/W-Floyd/go-pack-bins/offline"
	"github.com/W-Floyd/go-pack-bins/pack"
)

func TestRefineBalance_TightensWeightSpread(t *testing.T) {
	// Two width-6 items force two bins; four width-2 items fill them. Best-Fit
	// distribution leaves weight at 20/11; the optimal split given the geometry
	// is 13/18 (each bin = one width-6 + two width-2). RefineBalance should reach
	// a strictly smaller weight spread via swaps.
	factory := pack.NewConstrainedFactory(d1.NewFactory(10))
	items := []pack.Item{
		d1.NewItem("x", 6).WithScalar("weight", 10),
		d1.NewItem("y", 6).WithScalar("weight", 1),
		d1.NewItem("p", 2).WithScalar("weight", 9),
		d1.NewItem("q", 2).WithScalar("weight", 1),
		d1.NewItem("r", 2).WithScalar("weight", 8),
		d1.NewItem("s", 2).WithScalar("weight", 2),
	}
	start, err := offline.NewBalancedFit(factory, pack.FillHigh()).PackAll(items)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}

	spread := func(r pack.Result) float64 {
		per := map[string]float64{}
		sc := map[string]float64{"x": 10, "y": 1, "p": 9, "q": 1, "r": 8, "s": 2}
		for _, pl := range r.Placements {
			if pl != nil {
				per[pl.BinID()] += sc[pl.ItemID()]
			}
		}
		min, max := 0.0, 0.0
		first := true
		for _, v := range per {
			if first || v < min {
				min = v
			}
			if first || v > max {
				max = v
			}
			first = false
		}
		return max - min
	}

	before := spread(start)
	refined := offline.RefineBalance(factory, start, items)
	after := spread(refined)

	if refined.BinsUsed() != start.BinsUsed() {
		t.Errorf("bin count changed: %d → %d", start.BinsUsed(), refined.BinsUsed())
	}
	if after >= before {
		t.Errorf("weight spread not improved: before=%v after=%v", before, after)
	}
	// All items still placed.
	placed := 0
	for _, p := range refined.Placements {
		if p != nil {
			placed++
		}
	}
	if placed != len(items) {
		t.Errorf("placed %d, want %d", placed, len(items))
	}
}
