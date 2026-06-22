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
	for _, algo := range []string{"ems", "fit", "heightmap", "layer", "blocks"} {
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
	for _, algo := range []string{"ems", "fit", "heightmap", "layer", "blocks"} {
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

// The layered packers stream live, then deliver their settle post-pass as a final
// "reposition" frame; the repositioned (and non-streaming) results have no floating
// items — every elevated box rests on the floor or the top of another box.
func TestPackLayeredSettleViaReposition(t *testing.T) {
	// Sizes that drive a tall layer with a short cell underneath a higher box, so
	// the layered packers float something that the settle pass must drop.
	items := []ItemSpec{
		{ID: "tall", Width: 4, Depth: 4, Height: 6},
		{ID: "top1", Width: 4, Depth: 4, Height: 4},
		{ID: "top2", Width: 4, Depth: 4, Height: 4},
		{ID: "short", Width: 4, Depth: 4, Height: 2},
	}
	for _, algo := range []string{"layer", "blocks"} {
		req := PackRequest{Mode: "3d", Algorithm: algo, Bin: BinSpec{Width: 8, Depth: 4, Height: 10}, Items: items}
		if !isStreamable(req) {
			t.Errorf("%s should stream (settle delivered via a reposition frame)", algo)
		}
		var reposed []PlacementResult
		sawReposition := false
		StreamPack(context.Background(), req, func(f StreamFrame) {
			switch f.Type {
			case "reposition":
				sawReposition, reposed = true, f.Placements
			}
		})
		if !sawReposition {
			t.Fatalf("%s: expected a reposition frame for the settle post-pass", algo)
		}
		assertNoFloating(t, algo+" (reposition)", PackResponse{Placements: reposed})
		// The non-streaming path settles to the same float-free result.
		assertNoFloating(t, algo+" (non-stream)", Pack(req))
	}
}

// assertNoFloating fails if any elevated placement lacks support directly beneath it.
func assertNoFloating(t *testing.T, algo string, resp PackResponse) {
	t.Helper()
	byBin := map[int][]PlacementResult{}
	for _, p := range resp.Placements {
		byBin[p.BinIndex] = append(byBin[p.BinIndex], p)
	}
	for _, ps := range byBin {
		for _, p := range ps {
			if p.Z <= 1e-9 {
				continue // on the floor
			}
			supported := false
			for _, q := range ps {
				if q == p {
					continue
				}
				if q.Z+q.H >= p.Z-1e-9 && q.Z+q.H <= p.Z+1e-9 &&
					overlaps1(p.X, p.W, q.X, q.W) && overlaps1(p.Y, p.D, q.Y, q.D) {
					supported = true
					break
				}
			}
			if !supported {
				t.Errorf("%s: item %s floats at z=%v with nothing beneath it", algo, p.ItemID, p.Z)
			}
		}
	}
}

func overlaps1(a, aw, b, bw float64) bool { return min(a+aw, b+bw)-max(a, b) > 1e-9 }
