package d3_test

import (
	"testing"

	"github.com/W-Floyd/go-pack-bins/d3"
)

func TestBLF_FirstAtOriginNeverFloats(t *testing.T) {
	strat := d3.NewBottomLeftFillStrategy(10, 10, 10)
	bin := d3.NewBin("b", 10, 10, 10, strat)

	dims := [][3]float64{{4, 4, 3}, {6, 3, 2}, {3, 3, 5}, {5, 5, 2}, {2, 6, 4}, {4, 4, 4}, {3, 7, 3}}
	var placed []*d3.Placement3D
	for i, dm := range dims {
		p, err := bin.TryPlace(d3.NewItem("i"+string(rune('0'+i)), dm[0], dm[1], dm[2], false))
		if err != nil {
			continue
		}
		placed = append(placed, p.(*d3.Placement3D))
	}
	if len(placed) == 0 {
		t.Fatal("nothing placed")
	}
	if placed[0].X != 0 || placed[0].Y != 0 || placed[0].Z != 0 {
		t.Errorf("first item at (%v,%v,%v), want origin", placed[0].X, placed[0].Y, placed[0].Z)
	}
	// Every elevated box must rest on another box (BLF never floats).
	for _, p := range placed {
		if p.Z == 0 {
			continue
		}
		rests := false
		for _, q := range placed {
			if q == p {
				continue
			}
			ox := minf(p.X+p.W, q.X+q.W) - maxf(p.X, q.X)
			oy := minf(p.Y+p.D, q.Y+q.D) - maxf(p.Y, q.Y)
			if q.Z+q.H == p.Z && ox > 0 && oy > 0 {
				rests = true
				break
			}
		}
		if !rests {
			t.Errorf("box at z=%v is floating", p.Z)
		}
	}
}

func TestBLF_FillsFloorBeforeStacking(t *testing.T) {
	// A bin one unit tall in z but wide/deep: many unit boxes must all sit on the
	// floor (z=0) spread across x/y — BLF fills the bottom, never climbs a wall.
	strat := d3.NewBottomLeftFillStrategy(4, 4, 4)
	bin := d3.NewBin("b", 4, 4, 4, strat)
	var placed []*d3.Placement3D
	for i := 0; i < 16; i++ { // a full 4×4 floor layer of 1×1×1 boxes
		p, err := bin.TryPlace(d3.NewItem("i"+string(rune('a'+i)), 1, 1, 1, false))
		if err != nil {
			break
		}
		placed = append(placed, p.(*d3.Placement3D))
	}
	if len(placed) != 16 {
		t.Fatalf("placed %d, want 16 (a full floor layer)", len(placed))
	}
	for _, p := range placed {
		if p.Z != 0 {
			t.Errorf("box at z=%v stacked before the floor was full", p.Z)
		}
	}
}

func TestBLF_PrefersBottomThenLeftThenDeep(t *testing.T) {
	strat := d3.NewBottomLeftFillStrategy(10, 10, 10)
	bin := d3.NewBin("b", 10, 10, 10, strat)
	a, _ := bin.TryPlace(d3.NewItem("a", 2, 2, 2, false))
	b, _ := bin.TryPlace(d3.NewItem("b", 2, 2, 2, false))
	pa, pb := a.(*d3.Placement3D), b.(*d3.Placement3D)
	if pa.X != 0 || pa.Y != 0 || pa.Z != 0 {
		t.Fatalf("a at (%v,%v,%v), want origin", pa.X, pa.Y, pa.Z)
	}
	// b stays on the floor (z=0). Among floor corners BLF keeps x left-most (x=0)
	// and advances in depth, so it lands directly behind a at (0,2,0) — never
	// elevated. (The point is bottom-first: z stays 0, unlike the old back-climbing.)
	if pb.Z != 0 || pb.X != 0 || pb.Y != 2 {
		t.Errorf("b at (%v,%v,%v); want (0,2,0) — on the floor, left column", pb.X, pb.Y, pb.Z)
	}
}
