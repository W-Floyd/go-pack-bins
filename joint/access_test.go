package joint

import (
	"fmt"
	"testing"

	"github.com/W-Floyd/go-pack-bins/d3"
	"github.com/W-Floyd/go-pack-bins/offline"
	"github.com/W-Floyd/go-pack-bins/pack"
)

// accessItems: a mix of things wanted often and things wanted rarely, all the
// same size so only the objective can decide where they go.
func accessItems(n int) []pack.Item {
	var out []pack.Item
	for i := 0; i < n; i++ {
		uses := 1.0
		weight := 1.0
		if i%2 == 0 {
			uses = 50 // reached for weekly
			weight = 8
		}
		out = append(out, d3.NewItem(fmt.Sprintf("i%d", i), 2, 2, 2, false).
			WithScalar("uses", uses).WithScalar("weight", weight))
	}
	return out
}

func scalarsOf(items []pack.Item) map[string]map[string]float64 {
	m := map[string]map[string]float64{}
	for _, it := range items {
		m[it.ID()] = pack.ScalarsOf(it)
	}
	return m
}

func placements(r pack.Result) []*d3.Placement3D {
	var out []*d3.Placement3D
	for _, p := range r.Placements {
		if p3, ok := p.(*d3.Placement3D); ok {
			out = append(out, p3)
		}
	}
	return out
}

// The objective must actually lower the total retrieval cost, and must not cost
// bins to do it — a packing that spreads items over more containers to get them
// all within reach is not a better answer to the question asked.
func TestAccessCostObjectiveLowersTotal(t *testing.T) {
	cost := d3.AccessCost{
		PerHeight:       1,
		PerDistance:     0.5,
		PerHeightScalar: map[string]float64{"weight": 0.2},
	}
	items := accessItems(24)
	sc := scalarsOf(items)

	run := func(weight float64, order bool) (float64, int) {
		its := append([]pack.Item(nil), items...)
		if order {
			offline.DecreasingAccess("uses", 1)(its)
		}
		j := New(8, 8, 8, d3.ContactSpec{}, nil, nil, nil)
		if weight > 0 {
			j = j.WithAccessCost(cost, weight)
		}
		r, err := j.PackAll(its)
		if err != nil {
			t.Fatalf("pack: %v", err)
		}
		if len(r.Placements) != len(items) {
			t.Fatalf("placed %d of %d", len(r.Placements), len(items))
		}
		return d3.TotalAccessCost(placements(r), cost, sc, "uses", 1), r.BinsUsed()
	}

	plain, plainBins := run(0, false)
	scored, scoredBins := run(1, false)
	both, bothBins := run(1, true)

	t.Logf("net access cost: plain=%.1f (%d bins), scored=%.1f (%d bins), scored+ordered=%.1f (%d bins)",
		plain, plainBins, scored, scoredBins, both, bothBins)

	if scored >= plain {
		t.Errorf("scoring candidates by access cost did not lower the total: %.1f vs %.1f", scored, plain)
	}
	if both > scored {
		t.Errorf("ordering by frequency made it worse: %.1f vs %.1f", both, scored)
	}
	if scoredBins > plainBins || bothBins > plainBins {
		t.Errorf("the objective cost bins: %d/%d against %d", scoredBins, bothBins, plainBins)
	}
}

// Frequently-wanted items must end up lower than rarely-wanted ones — the
// property a user would actually check by looking at the result.
func TestAccessCostPutsWantedItemsWithinReach(t *testing.T) {
	cost := d3.AccessCost{PerHeight: 1}
	items := accessItems(24)
	offline.DecreasingAccess("uses", 1)(items)

	r, err := New(8, 8, 8, d3.ContactSpec{}, nil, nil, nil).
		WithAccessCost(cost, 1).PackAll(items)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	sc := scalarsOf(accessItems(24))

	var hotZ, coldZ, hotN, coldN float64
	for _, p := range placements(r) {
		if sc[p.ItemID()]["uses"] > 1 {
			hotZ += p.Z
			hotN++
		} else {
			coldZ += p.Z
			coldN++
		}
	}
	if hotN == 0 || coldN == 0 {
		t.Fatal("expected both frequently and rarely wanted items")
	}
	if hotZ/hotN >= coldZ/coldN {
		t.Errorf("frequently-wanted items average z=%.2f, rarely-wanted z=%.2f — the wanted ones are not lower",
			hotZ/hotN, coldZ/coldN)
	}
}

// A zero cost must leave the packer exactly as it was.
func TestAccessCostZeroIsInert(t *testing.T) {
	items := accessItems(12)
	a, _ := New(8, 8, 8, d3.ContactSpec{}, nil, nil, nil).PackAll(items)
	b, _ := New(8, 8, 8, d3.ContactSpec{}, nil, nil, nil).
		WithAccessCost(d3.AccessCost{}, 1).PackAll(items)
	if len(a.Placements) != len(b.Placements) || a.BinsUsed() != b.BinsUsed() {
		t.Fatalf("zero cost changed the packing")
	}
	for i := range placements(a) {
		if *placements(a)[i] != *placements(b)[i] {
			t.Errorf("placement %d differs under a zero access cost", i)
		}
	}
}
