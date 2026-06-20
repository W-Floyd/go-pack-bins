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

func TestContactStrategy_SupportGate(t *testing.T) {
	// Bottom support gate still rejects oversize / unsupported as before.
	strat := d3.NewExtremePointStrategyContact(d3.ContactSpec{Bottom: 0.9})(10, 10, 10)
	if _, _, _, _, _, _, ok := strat.TryInsert([][3]float64{{5, 5, 5}}); !ok {
		t.Error("floor placement should satisfy support gate")
	}
}
