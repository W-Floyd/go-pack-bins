package packapi

import "testing"

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
