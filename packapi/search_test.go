package packapi

import "testing"

// GBPP via packapi: optional items (profit scalar) are included when they fit
// free space, rejected when they'd need a bin their profit can't cover.
func TestPackGBPP(t *testing.T) {
	resp := Pack(PackRequest{
		Mode: "1d", Algorithm: "gbpp", BinCost: 50,
		Bin: BinSpec{Width: 10},
		Items: []ItemSpec{
			{ID: "c1", Width: 6},
			{ID: "c2", Width: 6},
			{ID: "x", Width: 4, Scalars: map[string]float64{"profit": 100}}, // optional, fits free space
			{ID: "z", Width: 10, Scalars: map[string]float64{"profit": 1}},  // optional, needs a bin it can't pay for
		},
	})
	if resp.Error != "" {
		t.Fatal(resp.Error)
	}
	if len(resp.Rejected) != 1 || resp.Rejected[0] != "z" {
		t.Fatalf("expected only z rejected, got %v", resp.Rejected)
	}
	if resp.IncludedProfit != 100 {
		t.Fatalf("expected included profit 100, got %g", resp.IncludedProfit)
	}
	if resp.NetCost != 50*2-100 {
		t.Fatalf("expected net cost %g, got %g", 50.0*2-100, resp.NetCost)
	}
}

// GBPP + catalog: choose the cheapest mix of bin types. Two 6³ cubes each need
// their own bin; a cheap 8³ (cost 1) beats an expensive 12³ (cost 50), so both
// go in 8³ bins, mixed dims reported, net cost = 2 (no optional profit).
func TestPackGBPPCatalog(t *testing.T) {
	resp := Pack(PackRequest{
		Mode: "3d", Algorithm: "gbpp",
		Items: cubes(2, 6),
		Containers: []ContainerSpec{
			{Bin: BinSpec{Width: 8, Height: 8, Depth: 8}, Cost: 1},
			{Bin: BinSpec{Width: 12, Height: 12, Depth: 12}, Cost: 50},
		},
	})
	if resp.Error != "" {
		t.Fatal(resp.Error)
	}
	if len(resp.Unplaced) != 0 || resp.BinsUsed != 2 {
		t.Fatalf("expected 2 bins / 0 unplaced, got %d bins / %v", resp.BinsUsed, resp.Unplaced)
	}
	if len(resp.BinDims) != 2 {
		t.Fatalf("expected per-bin dims for the mix, got %d", len(resp.BinDims))
	}
	for _, d := range resp.BinDims {
		if d.Width != 8 {
			t.Fatalf("expected the cheap 8³ bins to be chosen, got width %g", d.Width)
		}
	}
	if resp.NetCost != 2 {
		t.Fatalf("expected net cost 2 (two cheap bins), got %g", resp.NetCost)
	}
}

// Lexicographic via packapi: places a feasible order; winner reported.
func TestPackLex(t *testing.T) {
	resp := Pack(PackRequest{
		Mode: "3d", Algorithm: "lex", LexObjectives: []string{"unplaced", "bins"},
		Bin:   BinSpec{Width: 10, Height: 10, Depth: 10},
		Items: cubes(8, 5),
	})
	if resp.Error != "" {
		t.Fatal(resp.Error)
	}
	if len(resp.Unplaced) != 0 || resp.BinsUsed != 1 {
		t.Fatalf("expected 8 cubes in 1 bin, got %d bins / %d unplaced", resp.BinsUsed, len(resp.Unplaced))
	}
	if resp.BestPacker == "" {
		t.Fatal("expected a winning packer name")
	}
}

// The order-search algorithms (beam / ruin-recreate / GRASP) are wired for all
// dimensions and place a feasible order with no leftovers.
func TestPackSearchAlgorithms(t *testing.T) {
	for _, algo := range []string{"beam", "rr", "grasp"} {
		t.Run(algo, func(t *testing.T) {
			resp := Pack(PackRequest{
				Mode: "3d", Algorithm: algo,
				Bin:   BinSpec{Width: 10, Height: 10, Depth: 10},
				Items: cubes(8, 5),
			})
			if resp.Error != "" {
				t.Fatal(resp.Error)
			}
			if len(resp.Unplaced) != 0 || len(resp.Placements) != 8 {
				t.Fatalf("%s: expected 8 placed / 0 unplaced, got %d placed / %v unplaced",
					algo, len(resp.Placements), resp.Unplaced)
			}
		})
	}
}
