package packapi

import "testing"

// cube builds n identical w³ items.
func cubes(n int, w float64) []ItemSpec {
	out := make([]ItemSpec, n)
	for i := range out {
		out[i] = ItemSpec{ID: itoa(i), Width: w, Height: w, Depth: w, AllowRotate: true}
	}
	return out
}

// LAFF wired through packapi packs eight 5³ cubes into one 10³ container.
func TestPackLAFF(t *testing.T) {
	resp := Pack(PackRequest{
		Mode: "3d", Algorithm: "laff",
		Bin:   BinSpec{Width: 10, Height: 10, Depth: 10},
		Items: cubes(8, 5),
	})
	if resp.Error != "" {
		t.Fatal(resp.Error)
	}
	if resp.BinsUsed != 1 || len(resp.Placements) != 8 {
		t.Fatalf("laff: expected 1 bin / 8 placements, got %d / %d", resp.BinsUsed, len(resp.Placements))
	}
}

// Brute-force wired through packapi places a small order with no leftovers.
func TestPackBruteForce(t *testing.T) {
	resp := Pack(PackRequest{
		Mode: "3d", Algorithm: "brute",
		Bin:   BinSpec{Width: 10, Height: 10, Depth: 10},
		Items: cubes(6, 5),
	})
	if resp.Error != "" {
		t.Fatal(resp.Error)
	}
	if len(resp.Unplaced) != 0 || len(resp.Placements) != 6 {
		t.Fatalf("brute: expected all 6 placed, got %d placed, unplaced %v", len(resp.Placements), resp.Unplaced)
	}
}

// Catalog mode chooses the container type that packs the order best: eight 5³
// cubes fit one 10³ container but need eight 8³ containers, so 10³ wins.
func TestPackCatalog(t *testing.T) {
	resp := Pack(PackRequest{
		Mode: "3d", Algorithm: "ffd",
		Items: cubes(8, 5),
		Containers: []ContainerSpec{
			{Bin: BinSpec{Width: 8, Height: 8, Depth: 8}},
			{Bin: BinSpec{Width: 10, Height: 10, Depth: 10}},
		},
	})
	if resp.Error != "" {
		t.Fatal(resp.Error)
	}
	if resp.BinsUsed != 1 {
		t.Fatalf("expected the 10³ container to win with 1 bin, got %d bins", resp.BinsUsed)
	}
	if resp.Container != "10×10×10" || resp.ContainerBin == nil || resp.ContainerBin.Width != 10 {
		t.Fatalf("expected chosen container 10×10×10, got %q (%+v)", resp.Container, resp.ContainerBin)
	}
}

// MaxCount caps a container type: one 10³ allowed, but the order needs 2 → the
// overflow is reported unplaced.
func TestPackCatalogMaxCount(t *testing.T) {
	resp := Pack(PackRequest{
		Mode: "3d", Algorithm: "ffd",
		Items:      cubes(16, 5), // needs 2 of the 10³ container
		Containers: []ContainerSpec{{Bin: BinSpec{Width: 10, Height: 10, Depth: 10}, MaxCount: 1}},
	})
	if resp.Error != "" {
		t.Fatal(resp.Error)
	}
	if resp.BinsUsed != 1 {
		t.Fatalf("expected 1 bin (capped), got %d", resp.BinsUsed)
	}
	if len(resp.Unplaced) != 8 {
		t.Fatalf("expected 8 unplaced (spilled past the cap), got %d", len(resp.Unplaced))
	}
}

// When one container size's max count is exhausted, the catalog must cascade the
// overflow into the next available size rather than dropping items. Four 6³
// cubes each need their own 10³ bin; with two types capped at 2 bins each, only a
// 2+2 mix places them all.
func TestPackCatalogCascadesWhenMaxExhausted(t *testing.T) {
	resp := Pack(PackRequest{
		Mode: "3d", Algorithm: "ffd",
		Items: cubes(4, 6),
		Containers: []ContainerSpec{
			{Bin: BinSpec{Width: 10, Height: 10, Depth: 10}, MaxCount: 2},
			{Bin: BinSpec{Width: 10, Height: 10, Depth: 10}, MaxCount: 2},
		},
	})
	if resp.Error != "" {
		t.Fatal(resp.Error)
	}
	if len(resp.Unplaced) != 0 {
		t.Fatalf("expected the overflow to cascade into the second size, got %d unplaced: %v",
			len(resp.Unplaced), resp.Unplaced)
	}
	if resp.BinsUsed != 4 {
		t.Fatalf("expected 4 bins (2 of each size), got %d", resp.BinsUsed)
	}
	if len(resp.BinDims) != 4 {
		t.Fatalf("expected per-bin dimensions for all 4 mixed bins, got %d", len(resp.BinDims))
	}
}

// The incompatible constraint, via the "incompatible" op, keeps category-1 and
// category-2 items in separate bins even though both fit in one.
func TestPackIncompatible(t *testing.T) {
	items := []ItemSpec{
		{ID: "a", Width: 2, Height: 2, Depth: 2, AllowRotate: true, Scalars: map[string]float64{"hazmat": 1}},
		{ID: "b", Width: 2, Height: 2, Depth: 2, AllowRotate: true, Scalars: map[string]float64{"hazmat": 2}},
	}
	resp := Pack(PackRequest{
		Mode: "3d", Algorithm: "ff",
		Bin:         BinSpec{Width: 10, Height: 10, Depth: 10},
		Items:       items,
		Constraints: []ConstraintSpec{{Scalar: "hazmat", Op: "incompatible", Value: 1, Value2: 2}},
	})
	if resp.Error != "" {
		t.Fatal(resp.Error)
	}
	if resp.BinsUsed != 2 {
		t.Fatalf("incompatible: expected the two items in 2 separate bins, got %d", resp.BinsUsed)
	}
}
