package packapi

import (
	"context"
	"testing"
)

// mixedCubes returns n cubes with side cycling 2..5, rotatable.
func mixedCubes(n int) []ItemSpec {
	items := make([]ItemSpec, n)
	for i := 0; i < n; i++ {
		w := float64(2 + i%4)
		items[i] = ItemSpec{ID: itoa(i), Width: w, Height: w, Depth: w, AllowRotate: true}
	}
	return items
}

// The 3-D order-search metaheuristics (rr / arr / grasp / beam) decode through
// the EMS strategy by default; each must place everything without overlap and no
// worse than plain FFD on a feasible instance.
func TestSearch3DDecodesAndPacks(t *testing.T) {
	bin := BinSpec{Width: 10, Height: 10, Depth: 10}
	items := mixedCubes(40)
	ffd := Pack(PackRequest{Mode: "3d", Algorithm: "ffd", Bin: bin, Items: items})
	if ffd.Error != "" {
		t.Fatal(ffd.Error)
	}
	for _, algo := range []string{"rr", "arr", "grasp", "beam"} {
		resp := PackCtx(context.Background(), PackRequest{
			Mode: "3d", Algorithm: algo, Bin: bin, Items: items,
			AlgorithmOptions: map[string]float64{"search_max_iters": 200, "beam_width": 16},
		})
		if resp.Error != "" {
			t.Fatalf("%s: %s", algo, resp.Error)
		}
		if len(resp.Unplaced) != 0 {
			t.Errorf("%s: %d unplaced", algo, len(resp.Unplaced))
		}
		if len(resp.Placements) != len(items) {
			t.Errorf("%s: placed %d of %d", algo, len(resp.Placements), len(items))
		}
		if resp.BinsUsed > ffd.BinsUsed {
			t.Errorf("%s used %d bins, worse than FFD's %d", algo, resp.BinsUsed, ffd.BinsUsed)
		}
		assertValid3D(t, resp, bin.Width, bin.Depth, bin.Height)
	}
}

// The explicit decoder override is honoured (no error, valid packing) for each
// accepted decoder name.
func TestSearch3DDecoderOverride(t *testing.T) {
	bin := BinSpec{Width: 10, Height: 10, Depth: 10}
	items := mixedCubes(24)
	for _, dec := range []string{"", "ems", "fit", "blf", "heightmap", "extreme"} {
		resp := PackCtx(context.Background(), PackRequest{
			Mode: "3d", Algorithm: "arr", Decoder: dec, Bin: bin, Items: items,
			AlgorithmOptions: map[string]float64{"search_max_iters": 120},
		})
		if resp.Error != "" {
			t.Fatalf("decoder %q: %s", dec, resp.Error)
		}
		if len(resp.Unplaced) != 0 {
			t.Errorf("decoder %q: %d unplaced", dec, len(resp.Unplaced))
		}
		assertValid3D(t, resp, bin.Width, bin.Depth, bin.Height)
	}
}

// 3-D auto races the full constructive portfolio (incl. Fit/Layer/Blocks/Assemble/
// LAFF); it must name a winner, place everything, and be no worse than FFD alone.
func TestAuto3DPortfolio(t *testing.T) {
	bin := BinSpec{Width: 10, Height: 10, Depth: 10}
	items := mixedCubes(40)
	ffd := Pack(PackRequest{Mode: "3d", Algorithm: "ffd", Bin: bin, Items: items})
	resp := Pack(PackRequest{Mode: "3d", Algorithm: "auto", Bin: bin, Items: items})
	if resp.Error != "" {
		t.Fatal(resp.Error)
	}
	if resp.BestPacker == "" {
		t.Error("auto did not report a winning packer")
	}
	if len(resp.Unplaced) != 0 {
		t.Errorf("auto: %d unplaced", len(resp.Unplaced))
	}
	if resp.BinsUsed > ffd.BinsUsed {
		t.Errorf("auto used %d bins, worse than FFD's %d", resp.BinsUsed, ffd.BinsUsed)
	}
	assertValid3D(t, resp, bin.Width, bin.Depth, bin.Height)
}
