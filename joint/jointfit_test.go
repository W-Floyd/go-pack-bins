package joint_test

import (
	"testing"

	"github.com/W-Floyd/go-pack-bins/d3"
	"github.com/W-Floyd/go-pack-bins/joint"
	"github.com/W-Floyd/go-pack-bins/pack"
)

// Two heavy + two light boxes that pack two-per-bin geometrically. FFD would put
// both heavy together (sums 20 / 2); the joint balance objective spreads them so
// each bin ends at 11 — proving balance drives bin selection while placement
// stays valid in one pass.
func TestJointFit_BalancesWeightAcrossBins(t *testing.T) {
	w := map[string]float64{"h1": 10, "h2": 10, "l1": 1, "l2": 1}
	items := []pack.Item{
		d3.NewItem("h1", 5, 10, 10, false).WithScalar("weight", 10),
		d3.NewItem("h2", 5, 10, 10, false).WithScalar("weight", 10),
		d3.NewItem("l1", 5, 10, 10, false).WithScalar("weight", 1),
		d3.NewItem("l2", 5, 10, 10, false).WithScalar("weight", 1),
	}
	jf := joint.New(10, 10, 10, d3.ContactSpec{},
		[]pack.Preference{pack.ColocateLow("weight")}, []float64{1}, nil)

	r, err := jf.PackAll(items)
	if err != nil {
		t.Fatalf("packall: %v", err)
	}
	if r.BinsUsed() != 2 {
		t.Fatalf("bins = %d, want 2", r.BinsUsed())
	}
	sums := map[string]float64{}
	for _, p := range r.Placements {
		sums[p.BinID()] += w[p.ItemID()]
	}
	for id, s := range sums {
		if s != 11 {
			t.Errorf("bin %s weight = %v, want 11 (balanced)", id, s)
		}
	}
}

// With a hard Bottom support gate every placed item must rest on the floor or a
// box — the joint packer respects the same gate the strategy enforces.
func TestJointFit_RespectsSupportGate(t *testing.T) {
	var items []pack.Item
	dims := [][3]float64{{4, 4, 3}, {6, 3, 2}, {3, 3, 5}, {5, 5, 2}, {2, 6, 4}, {4, 4, 4}, {3, 7, 3}, {5, 4, 3}}
	for i, d := range dims {
		items = append(items, d3.NewItem("i"+string(rune('a'+i)), d[0], d[1], d[2], false))
	}
	jf := joint.New(10, 10, 10, d3.ContactSpec{Bottom: 1.0}, nil, nil, nil)

	r, err := jf.PackAll(items)
	if err != nil && err != pack.ErrItemTooLarge {
		t.Fatalf("packall: %v", err)
	}
	placed := map[string]*d3.Placement3D{}
	byBin := map[string][]*d3.Placement3D{}
	for _, p := range r.Placements {
		p3 := p.(*d3.Placement3D)
		placed[p3.ItemID()] = p3
		byBin[p3.BinID()] = append(byBin[p3.BinID()], p3)
	}
	for _, ps := range byBin {
		for _, p := range ps {
			if p.Z == 0 {
				continue
			}
			rests := false
			for _, q := range ps {
				if q == p {
					continue
				}
				ox := min(p.X+p.W, q.X+q.W) - max(p.X, q.X)
				oy := min(p.Y+p.D, q.Y+q.D) - max(p.Y, q.Y)
				if q.Z+q.H == p.Z && ox > 0 && oy > 0 {
					rests = true
					break
				}
			}
			if !rests {
				t.Errorf("item %s at z=%v is floating under a full-support gate", p.ItemID(), p.Z)
			}
		}
	}
}

// The observer fires once per placement, in commit order, matching the result.
func TestJointFit_Observable(t *testing.T) {
	var items []pack.Item
	for i := 0; i < 12; i++ {
		items = append(items, d3.NewItem("i"+string(rune('a'+i)), 3, 3, 3, false))
	}
	jf := joint.New(9, 9, 9, d3.ContactSpec{}, nil, nil, nil)

	var seen []pack.Placement
	jf.Observe(func(p pack.Placement) { seen = append(seen, p) })
	r, err := jf.PackAll(items)
	if err != nil {
		t.Fatalf("packall: %v", err)
	}
	if len(seen) != len(r.Placements) {
		t.Fatalf("observed %d placements, result has %d", len(seen), len(r.Placements))
	}
	if len(r.Unplaced) != 0 {
		t.Errorf("unplaced: %v", r.Unplaced)
	}
	for i := range r.Placements {
		if seen[i] != r.Placements[i] {
			t.Errorf("observation %d differs from result", i)
		}
	}
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
