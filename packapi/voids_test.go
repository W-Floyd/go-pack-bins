package packapi

import "testing"

func TestVoidsEnclosed(t *testing.T) {
	// 3×3×3 bin filled by 26 unit cubes, centre left empty and sealed.
	var pls []PlacementResult
	for k := 0; k < 3; k++ {
		for j := 0; j < 3; j++ {
			for i := 0; i < 3; i++ {
				if i == 1 && j == 1 && k == 1 {
					continue
				}
				pls = append(pls, PlacementResult{
					BinIndex: 0, X: float64(i), Y: float64(j), Z: float64(k), W: 1, D: 1, H: 1,
				})
			}
		}
	}
	resp := Voids(VoidRequest{Bin: BinSpec{Width: 3, Height: 3, Depth: 3}, Placements: pls})
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if len(resp.Voids) != 1 {
		t.Fatalf("want 1 void, got %d", len(resp.Voids))
	}
	if resp.VoidVolume != 1 {
		t.Errorf("void volume = %v, want 1", resp.VoidVolume)
	}
	if resp.BinVolume != 27 {
		t.Errorf("bin volume = %v, want 27", resp.BinVolume)
	}
	if v := resp.Voids[0]; v.BinIndex != 0 {
		t.Errorf("void bin index = %d, want 0", v.BinIndex)
	}
}

func TestVoidsPerBinIndependent(t *testing.T) {
	// Two bins, each with a sealed centre: expect a void per bin.
	var pls []PlacementResult
	for bin := 0; bin < 2; bin++ {
		for k := 0; k < 3; k++ {
			for j := 0; j < 3; j++ {
				for i := 0; i < 3; i++ {
					if i == 1 && j == 1 && k == 1 {
						continue
					}
					pls = append(pls, PlacementResult{
						BinIndex: bin, X: float64(i), Y: float64(j), Z: float64(k), W: 1, D: 1, H: 1,
					})
				}
			}
		}
	}
	resp := Voids(VoidRequest{Bin: BinSpec{Width: 3, Height: 3, Depth: 3}, Placements: pls})
	if len(resp.Voids) != 2 {
		t.Fatalf("want 2 voids (one per bin), got %d", len(resp.Voids))
	}
	if resp.Voids[0].BinIndex != 0 || resp.Voids[1].BinIndex != 1 {
		t.Errorf("voids not sorted by bin: %+v", resp.Voids)
	}
	if resp.BinVolume != 54 {
		t.Errorf("bin volume = %v, want 54 (27×2)", resp.BinVolume)
	}
}

func TestVoidsRejectsNon3D(t *testing.T) {
	resp := Voids(VoidRequest{Bin: BinSpec{Width: 3, Height: 3}}) // no depth
	if resp.Error == "" {
		t.Error("expected error for non-3D bin")
	}
}
