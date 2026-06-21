package d3_test

import (
	"testing"

	"github.com/W-Floyd/go-pack-bins/d3"
)

func TestDBLF_FirstAtOriginNeverFloats(t *testing.T) {
	strat := d3.NewDeepBottomLeftStrategy(10, 10, 10)
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
	// Every elevated box must rest on another box (DBLF never floats).
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

func TestDBLF_PrefersDeepThenBottomThenLeft(t *testing.T) {
	// Two unit items into a wide/deep bin: first at origin, second furthest back
	// at the same height — DBLF prioritises minimal y (depth) over x.
	strat := d3.NewDeepBottomLeftStrategy(10, 10, 10)
	bin := d3.NewBin("b", 10, 10, 10, strat)
	a, _ := bin.TryPlace(d3.NewItem("a", 2, 2, 2, false))
	b, _ := bin.TryPlace(d3.NewItem("b", 2, 2, 2, false))
	pa, pb := a.(*d3.Placement3D), b.(*d3.Placement3D)
	if pa.X != 0 || pa.Y != 0 {
		t.Fatalf("a at (%v,%v), want origin", pa.X, pa.Y)
	}
	// b's chosen corner is (a.x+w=2, 0, 0) — same y/z, next x — the deepest, lowest
	// option available.
	if pb.Z != 0 || pb.Y != 0 {
		t.Errorf("b at (%v,%v,%v); want it kept deep (y=0) and on the floor (z=0)", pb.X, pb.Y, pb.Z)
	}
}
