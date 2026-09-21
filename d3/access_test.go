package d3

import (
	"math"
	"testing"
)

func TestAccessCostTerms(t *testing.T) {
	sc := map[string]float64{"weight": 10}
	tests := []struct {
		name    string
		cost    AccessCost
		x, y, z float64
		want    float64
	}{
		{"fixed only", AccessCost{Fixed: 3}, 0, 0, 0, 3},
		{"height", AccessCost{PerHeight: 2}, 0, 0, 5, 10},
		{"distance from the access point", AccessCost{PerDistance: 1}, 3, 4, 0, 5},
		{"distance is to the nearest face, not the centre",
			AccessCost{PerDistance: 1, AccessX: 0, AccessY: 0}, 2, 0, 0, 2},
		{"per-scalar", AccessCost{PerScalar: map[string]float64{"weight": 0.5}}, 0, 0, 0, 5},
		{"height-scalar interaction",
			AccessCost{PerHeightScalar: map[string]float64{"weight": 0.1}}, 0, 0, 4, 4},
		{"terms add up", AccessCost{Fixed: 1, PerHeight: 1,
			PerScalar: map[string]float64{"weight": 0.1}}, 0, 0, 2, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cost.At(tt.x, tt.y, tt.z, 1, 1, sc)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("cost = %v, want %v", got, tt.want)
			}
		})
	}
}

// An item straddling the access point costs nothing in distance: you are
// standing at it.
func TestAccessCostZeroDistanceWhenAtHand(t *testing.T) {
	c := AccessCost{PerDistance: 5, AccessX: 2, AccessY: 2}
	if got := c.At(0, 0, 0, 4, 4, nil); got != 0 {
		t.Errorf("cost = %v, want 0 for an item spanning the access point", got)
	}
}

// The interaction term is the point of the model: heavy-and-high must cost more
// than the additive terms alone would say.
func TestAccessCostInteractionExceedsAdditive(t *testing.T) {
	heavy := map[string]float64{"weight": 20}
	light := map[string]float64{"weight": 1}
	c := AccessCost{PerHeight: 1, PerHeightScalar: map[string]float64{"weight": 0.5}}

	lowHeavy := c.At(0, 0, 0, 1, 1, heavy)
	highLight := c.At(0, 0, 6, 1, 1, light)
	highHeavy := c.At(0, 0, 6, 1, 1, heavy)

	if highHeavy <= lowHeavy+highLight-lowHeavy {
		t.Errorf("heavy-and-high (%v) is not worse than high-and-light (%v) plus the weight alone (%v)",
			highHeavy, highLight, lowHeavy)
	}
}

func TestAccessFrequencyDefaults(t *testing.T) {
	sc := map[string]float64{"uses": 12}
	if got := AccessFrequency(sc, "uses", 1); got != 12 {
		t.Errorf("got %v, want the item's own value", got)
	}
	if got := AccessFrequency(sc, "missing", 3); got != 3 {
		t.Errorf("got %v, want the default for an item with no value", got)
	}
	if got := AccessFrequency(sc, "", 7); got != 7 {
		t.Errorf("got %v, want the default when no scalar is named", got)
	}
}

// The objective weights each item's cost by how often it is wanted, so moving a
// frequently-wanted item to a cheap spot beats moving a rarely-wanted one.
func TestTotalAccessCostWeightsByFrequency(t *testing.T) {
	cost := AccessCost{PerHeight: 1}
	scalars := map[string]map[string]float64{
		"daily":  {"uses": 100},
		"yearly": {"uses": 1},
	}
	// Daily item high, yearly low: expensive.
	bad := []*Placement3D{
		NewPlacement3D("b", "daily", 0, 0, 10, 1, 1, 1),
		NewPlacement3D("b", "yearly", 0, 0, 0, 1, 1, 1),
	}
	// Swapped: cheap.
	good := []*Placement3D{
		NewPlacement3D("b", "daily", 0, 0, 0, 1, 1, 1),
		NewPlacement3D("b", "yearly", 0, 0, 10, 1, 1, 1),
	}
	badCost := TotalAccessCost(bad, cost, scalars, "uses", 1)
	goodCost := TotalAccessCost(good, cost, scalars, "uses", 1)
	if goodCost >= badCost {
		t.Errorf("putting the daily item within reach cost %v, burying it cost %v", goodCost, badCost)
	}
	if badCost != 1000 || goodCost != 10 {
		t.Errorf("costs = %v and %v, want 1000 and 10", badCost, goodCost)
	}
}
