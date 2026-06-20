package online_test

import (
	"testing"

	"github.com/W-Floyd/go-pack-bins/d1"
	"github.com/W-Floyd/go-pack-bins/online"
	"github.com/W-Floyd/go-pack-bins/pack"
)

// binOf maps each placed item to its bin ID for assertions.
func binOf(r pack.Result) map[string]string {
	m := map[string]string{}
	for _, p := range r.Placements {
		if p != nil {
			m[p.ItemID()] = p.BinID()
		}
	}
	return m
}

// Two width-6 items open two bins; a probe then fits in either. Preference A
// (scalar "a", values in the thousands) points at bin0, preference B (scalar
// "b", value 1) points at bin1.
func twoBinSetup(t *testing.T, packer *online.Packer) map[string]string {
	t.Helper()
	items := []pack.Item{
		d1.NewItem("x", 6).WithScalar("a", 1000),
		d1.NewItem("y", 6).WithScalar("b", 1),
		d1.NewItem("probe", 3),
	}
	for i, it := range items {
		if _, err := packer.Pack(it); err != nil {
			t.Fatalf("item %d: %v", i, err)
		}
	}
	return binOf(packer.Result())
}

func TestPreferenceFitNorm_WeightsBeatScale(t *testing.T) {
	// Raw-sum PreferenceFit lets the large-scale "a" preference dominate, so the
	// probe joins bin0 (x). With normalization both preferences are [0,1], and a
	// weight of 2 on the tiny-scale "b" preference flips the probe to bin1 (y).
	factory := func() pack.BinFactory { return pack.NewConstrainedFactory(d1.NewFactory(10)) }

	rawBins := twoBinSetup(t, online.PreferenceFit(factory(), pack.ColocateHigh("a"), pack.ColocateHigh("b")))
	if rawBins["probe"] != rawBins["x"] {
		t.Errorf("raw PreferenceFit: probe=%s, want bin of x=%s (large scale dominates)", rawBins["probe"], rawBins["x"])
	}

	normBins := twoBinSetup(t, online.PreferenceFitNorm(factory(),
		[]pack.Preference{pack.ColocateHigh("a"), pack.ColocateHigh("b")},
		[]float64{1, 2}))
	if normBins["probe"] != normBins["y"] {
		t.Errorf("normalized: probe=%s, want bin of y=%s (weight overrides scale)", normBins["probe"], normBins["y"])
	}
}
