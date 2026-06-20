package d3_test

import (
	"testing"

	"github.com/W-Floyd/go-pack-bins/d3"
)

func TestCompact_SlidesToOriginAndContact(t *testing.T) {
	// a at origin, b floating at (5,5). x-slide pulls b to x=0 (no y-overlap with a
	// yet). Now both share x, so y-slide stacks b flush against a at y=2.
	a := &d3.Placement3D{X: 0, Y: 0, Z: 0, W: 2, D: 2, H: 2}
	b := &d3.Placement3D{X: 5, Y: 5, Z: 0, W: 2, D: 2, H: 2}
	d3.Compact([]*d3.Placement3D{a, b}, 10, 10, 10, true, true)

	if b.X != 0 || b.Y != 2 {
		t.Errorf("b compacted to (%v,%v), want (0,2) flush against a", b.X, b.Y)
	}
	// z (height) is never touched.
	if b.Z != 0 || a.Z != 0 {
		t.Errorf("z changed by compaction: a.z=%v b.z=%v", a.Z, b.Z)
	}
}

func TestCompact_StopsAtNeighbor(t *testing.T) {
	// b overlaps a in y and z, so sliding -x stops flush against a's right face.
	a := &d3.Placement3D{X: 0, Y: 0, Z: 0, W: 3, D: 4, H: 4}
	b := &d3.Placement3D{X: 6, Y: 0, Z: 0, W: 2, D: 4, H: 4}
	d3.Compact([]*d3.Placement3D{a, b}, 10, 10, 10, true, true)
	if b.X != 3 {
		t.Errorf("b.x = %v, want 3 (flush against a)", b.X)
	}
}

func TestContactStrategy_PrefersWallHugging(t *testing.T) {
	// With a lateral contact target, a small item should be placed touching walls
	// rather than floating mid-bin. First item always goes to the origin corner.
	strat := d3.NewExtremePointStrategyContact(d3.ContactSpec{SideX: 1, SideY: 1})(10, 10, 10)
	x, y, z, _, _, _, ok := strat.TryInsert([][3]float64{{2, 2, 2}})
	if !ok {
		t.Fatal("placement failed")
	}
	if x != 0 || y != 0 || z != 0 {
		t.Errorf("first item at (%v,%v,%v), want origin corner", x, y, z)
	}
}

func TestNoFloating_EveryItemSupported(t *testing.T) {
	// Pack a mix of boxes with NoFloating and assert each is on the floor or
	// resting (with positive overlap) on the top of another box — never airborne.
	strat := d3.NewExtremePointStrategyContact(d3.ContactSpec{NoFloating: true})
	bin := d3.NewBin("b", 10, 10, 10, strat(10, 10, 10))
	dims := [][3]float64{{4, 4, 3}, {6, 3, 2}, {3, 3, 5}, {5, 5, 2}, {2, 6, 4}, {4, 4, 4}}
	var placed []*d3.Placement3D
	for i, dm := range dims {
		p, err := bin.TryPlace(d3.NewItem("i"+string(rune('0'+i)), dm[0], dm[1], dm[2], false))
		if err != nil {
			continue // ran out of room; fine
		}
		placed = append(placed, p.(*d3.Placement3D))
	}
	for _, p := range placed {
		if p.Z == 0 {
			continue // on the floor
		}
		supported := false
		for _, q := range placed {
			if q == p {
				continue
			}
			top := q.Z + q.H
			ox := minf(p.X+p.W, q.X+q.W) - maxf(p.X, q.X)
			oy := minf(p.Y+p.D, q.Y+q.D) - maxf(p.Y, q.Y)
			if top == p.Z && ox > 0 && oy > 0 {
				supported = true
				break
			}
		}
		if !supported {
			t.Errorf("item at z=%v is floating (no box beneath)", p.Z)
		}
	}
}

func minf(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func TestContactStrategy_SupportGate(t *testing.T) {
	// Bottom support gate still rejects oversize / unsupported as before.
	strat := d3.NewExtremePointStrategyContact(d3.ContactSpec{Bottom: 0.9})(10, 10, 10)
	if _, _, _, _, _, _, ok := strat.TryInsert([][3]float64{{5, 5, 5}}); !ok {
		t.Error("floor placement should satisfy support gate")
	}
}
