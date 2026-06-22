package packapi

import (
	"context"
	"testing"
)

// overlaps3D reports whether two 3-D placements share positive volume.
func overlaps3D(a, b PlacementResult) bool {
	ov := func(a0, a1, b0, b1 float64) bool {
		return min(a1, b1)-max(a0, b0) > 1e-9
	}
	return ov(a.X, a.X+a.W, b.X, b.X+b.W) &&
		ov(a.Y, a.Y+a.D, b.Y, b.Y+b.D) &&
		ov(a.Z, a.Z+a.H, b.Z, b.Z+b.H)
}

// assertValid3D fails if any placement escapes the bin or two placements overlap.
func assertValid3D(t *testing.T, resp PackResponse, bw, bd, bh float64) {
	t.Helper()
	byBin := map[int][]PlacementResult{}
	for _, p := range resp.Placements {
		byBin[p.BinIndex] = append(byBin[p.BinIndex], p)
		if p.X < -1e-9 || p.Y < -1e-9 || p.Z < -1e-9 ||
			p.X+p.W > bw+1e-9 || p.Y+p.D > bd+1e-9 || p.Z+p.H > bh+1e-9 {
			t.Errorf("placement %s escapes bin: (%v,%v,%v) size (%v,%v,%v)", p.ItemID, p.X, p.Y, p.Z, p.W, p.D, p.H)
		}
	}
	for _, ps := range byBin {
		for i := 0; i < len(ps); i++ {
			for j := i + 1; j < len(ps); j++ {
				if overlaps3D(ps[i], ps[j]) {
					t.Errorf("placements %s and %s overlap in bin %d", ps[i].ItemID, ps[j].ItemID, ps[i].BinIndex)
				}
			}
		}
	}
}

// EMS, heightmap and the layer packer each tile eight 5³ cubes into a single 10³
// bin with no leftovers, no overlaps, and full utilisation — exercising the
// packapi wiring.
func TestPackEMSAndHeightmapTilePerfectly(t *testing.T) {
	for _, algo := range []string{"ems", "heightmap", "layer"} {
		resp := Pack(PackRequest{
			Mode: "3d", Algorithm: algo,
			Bin:   BinSpec{Width: 10, Height: 10, Depth: 10},
			Items: cubes(8, 5),
		})
		if resp.Error != "" {
			t.Fatalf("%s: %s", algo, resp.Error)
		}
		if resp.BinsUsed != 1 || len(resp.Placements) != 8 || len(resp.Unplaced) != 0 {
			t.Errorf("%s: expected 1 bin / 8 placed / 0 unplaced, got %d / %d / %v",
				algo, resp.BinsUsed, len(resp.Placements), resp.Unplaced)
		}
		assertValid3D(t, resp, 10, 10, 10)
	}
}

// A mixed order spilling into several bins still places every item without
// overlap under both new strategies.
func TestPackEMSAndHeightmapMixedNoOverlap(t *testing.T) {
	items := []ItemSpec{}
	for i := 0; i < 40; i++ {
		w := float64(2 + i%4) // 2..5
		items = append(items, ItemSpec{ID: itoa(i), Width: w, Height: w, Depth: w, AllowRotate: true})
	}
	for _, algo := range []string{"ems", "heightmap", "layer"} {
		resp := Pack(PackRequest{
			Mode: "3d", Algorithm: algo,
			Bin:   BinSpec{Width: 10, Height: 10, Depth: 10},
			Items: items,
		})
		if resp.Error != "" {
			t.Fatalf("%s: %s", algo, resp.Error)
		}
		if len(resp.Unplaced) != 0 {
			t.Errorf("%s: %d items left unplaced", algo, len(resp.Unplaced))
		}
		if len(resp.Placements) != len(items) {
			t.Errorf("%s: placed %d of %d", algo, len(resp.Placements), len(items))
		}
		assertValid3D(t, resp, 10, 10, 10)
	}
}

// The layer packer is streamable: a solve emits batch placements and numeric
// progress incrementally, not just a final result.
func TestPackLayerStreams(t *testing.T) {
	req := PackRequest{
		Mode: "3d", Algorithm: "layer",
		Bin:   BinSpec{Width: 10, Height: 10, Depth: 10},
		Items: cubes(40, 3),
	}
	if !isStreamable(req) {
		t.Fatal("layer should be streamable")
	}
	var batches, progress, placed int
	StreamPack(context.Background(), req, func(f StreamFrame) {
		switch f.Type {
		case "batch":
			batches++
			placed += len(f.Placements)
		case "progress":
			progress++
		}
	})
	if batches == 0 || progress == 0 {
		t.Errorf("layer stream: batches=%d progress=%d, want both > 0", batches, progress)
	}
	if placed != 40 {
		t.Errorf("layer stream: %d items placed across batches, want 40", placed)
	}
}
