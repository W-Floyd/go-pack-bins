package d3

import (
	"math"
	"testing"
)

// unit is a 1×1 box at (x,y,z) with the given weight and bearing limit.
func unit(x, y, z, wt, lim float64) BearBox {
	return BearBox{X: x, Y: y, Z: z, W: 1, D: 1, H: 1, Weight: wt, Limit: lim, PressureLimit: NoLimit}
}

func TestBorneLoads(t *testing.T) {
	tests := []struct {
		name string
		bs   []BearBox
		want []float64
	}{
		{
			name: "single box on floor bears nothing",
			bs:   []BearBox{unit(0, 0, 0, 5, NoLimit)},
			want: []float64{0},
		},
		{
			name: "two-high stack: bottom bears the top's weight",
			bs: []BearBox{
				unit(0, 0, 0, 5, NoLimit),
				unit(0, 0, 1, 3, NoLimit),
			},
			want: []float64{3, 0},
		},
		{
			name: "three-high stack accumulates transitively",
			bs: []BearBox{
				unit(0, 0, 0, 5, NoLimit),
				unit(0, 0, 1, 3, NoLimit),
				unit(0, 0, 2, 2, NoLimit),
			},
			want: []float64{5, 2, 0},
		},
		{
			name: "branching: one box spanning two supporters splits by contact area",
			bs: []BearBox{
				unit(0, 0, 0, 1, NoLimit),
				unit(1, 0, 0, 1, NoLimit),
				// 2×1 box bridging both, equal contact on each.
				{X: 0, Y: 0, Z: 1, W: 2, D: 1, H: 1, Weight: 10, Limit: NoLimit, PressureLimit: NoLimit},
			},
			want: []float64{5, 5, 0},
		},
		{
			name: "uneven contact splits proportionally",
			bs: []BearBox{
				{X: 0, Y: 0, Z: 0, W: 3, D: 1, H: 1, Weight: 1, Limit: NoLimit, PressureLimit: NoLimit},
				{X: 3, Y: 0, Z: 0, W: 1, D: 1, H: 1, Weight: 1, Limit: NoLimit, PressureLimit: NoLimit},
				// 4-wide bridge: 3 units of contact left, 1 unit right → 3:1.
				{X: 0, Y: 0, Z: 1, W: 4, D: 1, H: 1, Weight: 8, Limit: NoLimit, PressureLimit: NoLimit},
			},
			want: []float64{6, 2, 0},
		},
		{
			name: "pyramid: weight of both upper boxes reaches the base",
			bs: []BearBox{
				{X: 0, Y: 0, Z: 0, W: 2, D: 1, H: 1, Weight: 1, Limit: NoLimit, PressureLimit: NoLimit},
				unit(0, 0, 1, 2, NoLimit),
				unit(1, 0, 1, 3, NoLimit),
			},
			want: []float64{5, 0, 0},
		},
		{
			name: "floating box sheds nothing (gap below)",
			bs: []BearBox{
				unit(0, 0, 0, 1, NoLimit),
				unit(0, 0, 5, 9, NoLimit),
			},
			want: []float64{0, 0},
		},
		{
			name: "side-by-side boxes do not bear each other",
			bs: []BearBox{
				unit(0, 0, 0, 4, NoLimit),
				unit(1, 0, 0, 4, NoLimit),
			},
			want: []float64{0, 0},
		},
		{
			name: "edge-touching footprints are not support",
			bs: []BearBox{
				unit(0, 0, 0, 1, NoLimit),
				unit(1, 0, 1, 7, NoLimit), // rests at z=1 but footprint only shares an edge
			},
			want: []float64{0, 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BorneLoads(tt.bs)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d loads, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if math.Abs(got[i].Weight-tt.want[i]) > 1e-9 {
					t.Errorf("box %d: borne = %v, want %v (all: %v)", i, got[i].Weight, tt.want[i], got)
				}
			}
		})
	}
}

// BorneLoads must not depend on the order boxes are listed in — the result is a
// property of the configuration, and strategies append in placement order while
// post-passes reorder freely.
func TestBorneLoadsOrderIndependent(t *testing.T) {
	asc := []BearBox{
		unit(0, 0, 0, 5, NoLimit),
		unit(0, 0, 1, 3, NoLimit),
		unit(0, 0, 2, 2, NoLimit),
	}
	desc := []BearBox{asc[2], asc[1], asc[0]}

	a, d := BorneLoads(asc), BorneLoads(desc)
	// d is reversed relative to a.
	for i := range a {
		if math.Abs(a[i].Weight-d[len(d)-1-i].Weight) > 1e-9 {
			t.Fatalf("order changed the result: ascending %v, descending %v", a, d)
		}
	}
}

func TestBearingOK(t *testing.T) {
	tests := []struct {
		name string
		bs   []BearBox
		want bool
	}{
		{
			name: "within limit",
			bs: []BearBox{
				unit(0, 0, 0, 1, 10),
				unit(0, 0, 1, 4, NoLimit),
			},
			want: true,
		},
		{
			name: "over limit",
			bs: []BearBox{
				unit(0, 0, 0, 1, 3),
				unit(0, 0, 1, 4, NoLimit),
			},
			want: false,
		},
		{
			name: "exactly at limit is allowed",
			bs: []BearBox{
				unit(0, 0, 0, 1, 4),
				unit(0, 0, 1, 4, NoLimit),
			},
			want: true,
		},
		{
			name: "zero limit means fragile: nothing may rest on it",
			bs: []BearBox{
				unit(0, 0, 0, 1, 0),
				unit(0, 0, 1, 1, NoLimit),
			},
			want: false,
		},
		{
			name: "fragile on top of a stack is fine",
			bs: []BearBox{
				unit(0, 0, 0, 1, 10),
				unit(0, 0, 1, 1, 0),
			},
			want: true,
		},
		{
			name: "floor bears infinitely: a heavy box alone is fine",
			bs:   []BearBox{unit(0, 0, 0, 1e9, 0)},
			want: true,
		},
		{
			name: "crush detected deep in the stack, not just directly above",
			bs: []BearBox{
				unit(0, 0, 0, 1, 5), // bears 2+4 = 6 > 5
				unit(0, 0, 1, 2, 10),
				unit(0, 0, 2, 4, NoLimit),
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BearingOK(tt.bs); got != tt.want {
				t.Errorf("BearingOK = %v, want %v (loads %v)", got, tt.want, BorneLoads(tt.bs))
			}
		})
	}
}

// CanBear is the incremental gate a strategy calls; it must agree with the
// whole-configuration BearingOK on the configuration that results from the
// placement. Disagreement would let a constructive gate admit a stack the
// post-pass validator then rejects.
func TestCanBearAgreesWithBearingOK(t *testing.T) {
	bases := [][]BearBox{
		{unit(0, 0, 0, 1, 3)},
		{unit(0, 0, 0, 1, 3), unit(0, 0, 1, 1, 3)},
		{unit(0, 0, 0, 1, 2), unit(1, 0, 0, 1, 10)},
		{{X: 0, Y: 0, Z: 0, W: 2, D: 1, H: 1, Weight: 1, Limit: 6, PressureLimit: NoLimit}},
	}
	cands := []BearBox{
		unit(0, 0, 1, 1, NoLimit),
		unit(0, 0, 1, 5, NoLimit),
		unit(0, 0, 2, 2, NoLimit),
		{X: 0, Y: 0, Z: 1, W: 2, D: 1, H: 1, Weight: 7, Limit: NoLimit, PressureLimit: NoLimit},
		unit(0, 0, 0, 9, NoLimit), // on the floor beside things
	}

	for bi, base := range bases {
		for ci, cand := range cands {
			// Skip candidates that would overlap the base — the bearing rule
			// assumes a collision-free configuration, which the caller's
			// geometry gate guarantees.
			if overlapsAny(base, cand) {
				continue
			}
			full := append(append([]BearBox{}, base...), cand)
			want := BearingOK(full)
			if got := CanBear(base, cand); got != want {
				t.Errorf("base %d cand %d: CanBear = %v, BearingOK(full) = %v (loads %v)",
					bi, ci, got, want, BorneLoads(full))
			}
		}
	}
}

func overlapsAny(bs []BearBox, c BearBox) bool {
	for _, b := range bs {
		if overlap1D(b.X, b.X+b.W, c.X, c.X+c.W) > compactEps &&
			overlap1D(b.Y, b.Y+b.D, c.Y, c.Y+c.D) > compactEps &&
			overlap1D(b.Z, b.Z+b.H, c.Z, c.Z+c.H) > compactEps {
			return true
		}
	}
	return false
}

func TestSupportersOf(t *testing.T) {
	bs := []BearBox{
		unit(0, 0, 0, 1, NoLimit),
		unit(1, 0, 0, 1, NoLimit),
		unit(5, 0, 0, 1, NoLimit),
	}
	sup := supportersOf(bs, 0, 0, 1, 2, 1)
	if len(sup) != 2 {
		t.Fatalf("got %d supporters, want 2: %+v", len(sup), sup)
	}
	for _, s := range sup {
		if s.area != 1 {
			t.Errorf("supporter %d: area = %v, want 1", s.idx, s.area)
		}
	}
	if got := supportersOf(bs, 0, 0, 0, 1, 1); got != nil {
		t.Errorf("on the floor: got %+v, want no supporters", got)
	}
}

// A box may take a given total weight spread out and still be crushed by far
// less on a narrow foot, so the pressure cap is a separate check from Limit and
// both apply.
func TestBearingPressure(t *testing.T) {
	// A 5x5 base with a generous weight limit but a modest pressure cap.
	base := func(pressure float64) BearBox {
		return BearBox{X: 0, Y: 0, Z: 0, W: 5, D: 5, H: 1,
			Weight: 1, Limit: 100, PressureLimit: pressure}
	}
	// Two loads of equal weight: one spread over the whole face, one on a foot.
	spread := BearBox{X: 0, Y: 0, Z: 1, W: 5, D: 5, H: 1,
		Weight: 25, Limit: NoLimit, PressureLimit: NoLimit} // 25 / 25 = 1.0
	narrow := BearBox{X: 0, Y: 0, Z: 1, W: 1, D: 1, H: 1,
		Weight: 25, Limit: NoLimit, PressureLimit: NoLimit} // 25 / 1 = 25.0

	tests := []struct {
		name     string
		bs       []BearBox
		wantOK   bool
		wantPeak float64
	}{
		{"spread load within pressure cap", []BearBox{base(2), spread}, true, 1},
		{"same weight on a narrow foot exceeds it", []BearBox{base(2), narrow}, false, 25},
		{"narrow foot fine under a high cap", []BearBox{base(100), narrow}, true, 25},
		{"no pressure cap ignores concentration", []BearBox{base(NoLimit), narrow}, true, 25},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BearingOK(tt.bs); got != tt.wantOK {
				t.Errorf("BearingOK = %v, want %v", got, tt.wantOK)
			}
			if got := BorneLoads(tt.bs)[0].Pressure; math.Abs(got-tt.wantPeak) > 1e-9 {
				t.Errorf("peak pressure = %v, want %v", got, tt.wantPeak)
			}
		})
	}
}

// Total weight and pressure are independent: passing one does not excuse the
// other, in either direction.
func TestBearingPressureAndWeightAreIndependent(t *testing.T) {
	base := BearBox{X: 0, Y: 0, Z: 0, W: 5, D: 5, H: 1, Weight: 1, Limit: 10, PressureLimit: 2}
	// Light but concentrated: within the weight limit, over the pressure cap.
	concentrated := BearBox{X: 0, Y: 0, Z: 1, W: 1, D: 1, H: 1, Weight: 5, Limit: NoLimit, PressureLimit: NoLimit}
	if BearingOK([]BearBox{base, concentrated}) {
		t.Error("5kg on one unit of area (pressure 5 > 2) was allowed despite being within the 10kg limit")
	}
	// Heavy but spread: within the pressure cap, over the weight limit.
	heavySpread := BearBox{X: 0, Y: 0, Z: 1, W: 5, D: 5, H: 1, Weight: 40, Limit: NoLimit, PressureLimit: NoLimit}
	if BearingOK([]BearBox{base, heavySpread}) {
		t.Error("40kg spread thinly (pressure 1.6 < 2) was allowed despite exceeding the 10kg limit")
	}
}

// Peak, not sum: two separate patches on one box do not combine into a single
// more concentrated patch.
func TestBearingPressureIsPeakPerPatch(t *testing.T) {
	base := BearBox{X: 0, Y: 0, Z: 0, W: 4, D: 1, H: 1, Weight: 1, Limit: 100, PressureLimit: 3}
	a := BearBox{X: 0, Y: 0, Z: 1, W: 1, D: 1, H: 1, Weight: 2, Limit: NoLimit, PressureLimit: NoLimit}
	b := BearBox{X: 3, Y: 0, Z: 1, W: 1, D: 1, H: 1, Weight: 2, Limit: NoLimit, PressureLimit: NoLimit}
	bs := []BearBox{base, a, b}
	if got := BorneLoads(bs)[0].Pressure; math.Abs(got-2) > 1e-9 {
		t.Errorf("peak pressure = %v, want 2 (each patch separately, not 4 combined)", got)
	}
	if !BearingOK(bs) {
		t.Error("two 2.0-pressure patches were treated as one 4.0 patch")
	}
}

// The incremental gate must agree with the whole-configuration check on pressure
// too, or a strategy would admit stacks the validator rejects.
func TestCanBearAgreesOnPressure(t *testing.T) {
	base := []BearBox{{X: 0, Y: 0, Z: 0, W: 4, D: 4, H: 1, Weight: 1, Limit: 100, PressureLimit: 2}}
	cands := []BearBox{
		{X: 0, Y: 0, Z: 1, W: 1, D: 1, H: 1, Weight: 5, Limit: NoLimit, PressureLimit: NoLimit},
		{X: 0, Y: 0, Z: 1, W: 4, D: 4, H: 1, Weight: 5, Limit: NoLimit, PressureLimit: NoLimit},
		{X: 0, Y: 0, Z: 1, W: 2, D: 2, H: 1, Weight: 8, Limit: NoLimit, PressureLimit: NoLimit},
	}
	for i, c := range cands {
		full := append(append([]BearBox{}, base...), c)
		if got, want := CanBear(base, c), BearingOK(full); got != want {
			t.Errorf("cand %d: CanBear = %v, BearingOK = %v (loads %v)", i, got, want, BorneLoads(full))
		}
	}
}
