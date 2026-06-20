package d3_test

import (
	"testing"

	"github.com/W-Floyd/go-pack-bins/d3"
	"github.com/W-Floyd/go-pack-bins/pack"
)

func TestExtremePoint_PeakHeightAndFit(t *testing.T) {
	bin := d3.NewBin("b", 10, 10, 10, d3.NewExtremePointStrategy(10, 10, 10))

	// A 4-tall item placed on the floor → peak height 4 (z=0, h=4).
	if _, err := bin.TryPlace(d3.NewItem("a", 5, 5, 4, false)); err != nil {
		t.Fatalf("place a: %v", err)
	}
	if hr, ok := bin.Metrics()[pack.MetricPeakHeight]; !ok || hr != 4 {
		t.Errorf("peak height = %v (present=%v), want 4", hr, ok)
	}

	// An item taller than the bin in every orientation is permanently rejected.
	_, err := bin.TryPlace(d3.NewItem("big", 20, 20, 20, false))
	if err != pack.ErrItemTooLarge {
		t.Errorf("oversize item err = %v, want ErrItemTooLarge", err)
	}
}

func TestExtremePoint_MinSupportRejectsFloating(t *testing.T) {
	// With 90% minimum support, a small item cannot perch on the corner of a
	// larger one (insufficient supported base) and must sit elsewhere/lower.
	strat := d3.NewExtremePointStrategyWithSupport(0.9)
	bin := d3.NewBin("b", 10, 10, 10, strat(10, 10, 10))
	if _, err := bin.TryPlace(d3.NewItem("base", 10, 10, 5, false)); err != nil {
		t.Fatalf("place base: %v", err)
	}
	p, err := bin.TryPlace(d3.NewItem("top", 4, 4, 4, false))
	if err != nil {
		t.Fatalf("place top: %v", err)
	}
	p3 := p.(*d3.Placement3D)
	// Fully supported by the base top face (z=5) or the floor (z=0) — never floating.
	if p3.Z != 5 && p3.Z != 0 {
		t.Errorf("top placed at z=%v; expected fully-supported z (0 or 5)", p3.Z)
	}
}
