package d3_test

import (
	"testing"

	"github.com/W-Floyd/go-pack-bins/d3"
)

func TestHeightmap_NoOverlapAndFirstAtOrigin(t *testing.T) {
	dims := [][3]float64{{4, 4, 3}, {6, 3, 2}, {3, 3, 5}, {5, 5, 2}, {2, 6, 4}, {4, 4, 4}, {3, 7, 3}}
	ps := place3(t, d3.NewHeightmapStrategy(10, 10, 10), 10, 10, 10, false, dims)
	if len(ps) == 0 {
		t.Fatal("nothing placed")
	}
	if ps[0].X != 0 || ps[0].Y != 0 || ps[0].Z != 0 {
		t.Errorf("first item at (%v,%v,%v), want origin", ps[0].X, ps[0].Y, ps[0].Z)
	}
	assertNoOverlap(t, ps, 10, 10, 10)
}

func TestHeightmap_FillsFloorBeforeStacking(t *testing.T) {
	dims := make([][3]float64, 16)
	for i := range dims {
		dims[i] = [3]float64{1, 1, 1}
	}
	ps := place3(t, d3.NewHeightmapStrategy(4, 4, 4), 4, 4, 4, false, dims)
	if len(ps) != 16 {
		t.Fatalf("placed %d, want 16 (a full floor layer)", len(ps))
	}
	for _, p := range ps {
		if p.Z != 0 {
			t.Errorf("box %s at z=%v stacked before the floor was full", p.ItemID(), p.Z)
		}
	}
	assertNoOverlap(t, ps, 4, 4, 4)
}

func TestHeightmap_LandsOnTopOfObstacle(t *testing.T) {
	// A wide base, then a box dropped over its footprint must rest on its top
	// (z = base height), not intersect it nor float higher.
	strat := d3.NewHeightmapStrategy(10, 10, 10)
	ps := place3(t, strat, 10, 10, 10, false, [][3]float64{{10, 10, 3}, {4, 4, 2}})
	if len(ps) != 2 {
		t.Fatalf("placed %d, want 2", len(ps))
	}
	assertNoOverlap(t, ps, 10, 10, 10)
	if ps[1].Z != 3 {
		t.Errorf("second box at z=%v, want 3 (resting on the base slab)", ps[1].Z)
	}
}

func TestHeightmap_BridgesAcrossGap(t *testing.T) {
	// Build two tall pillars (z 0..3) separated by a short filler (z 0..1), then
	// drop a plank over the whole width. The heightmap rests it on the highest
	// support under its footprint — the pillar tops at z=3 — leaving a void above
	// the filler. Bottom-Left-Fill could not do this: no corner under the plank is
	// supported across the gap.
	strat := d3.NewHeightmapStrategy(10, 10, 10)
	bin := d3.NewBin("b", 10, 10, 10, strat)
	bin.TryPlace(d3.NewItem("p0", 2, 10, 3, false))     // pillar x 0..2
	bin.TryPlace(d3.NewItem("filler", 6, 10, 1, false)) // short filler x 2..8, top z=1
	bin.TryPlace(d3.NewItem("p2", 2, 10, 3, false))     // pillar x 8..10
	plank, err := bin.TryPlace(d3.NewItem("plank", 10, 10, 1, false))
	if err != nil {
		t.Fatalf("plank not placed: %v", err)
	}
	pp := plank.(*d3.Placement3D)
	if pp.Z != 3 {
		t.Errorf("plank at z=%v, want 3 (bridging on the pillar tops over the filler)", pp.Z)
	}
}
