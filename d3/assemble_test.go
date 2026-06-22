package d3_test

import (
	"testing"

	"github.com/W-Floyd/go-pack-bins/d3"
	"github.com/W-Floyd/go-pack-bins/pack"
)

// perBinNoOverlap groups placements by bin and asserts non-overlap within each.
func perBinNoOverlap(t *testing.T, ps []*d3.Placement3D, bw, bd, bh float64) {
	t.Helper()
	byBin := map[string][]*d3.Placement3D{}
	for _, p := range ps {
		byBin[p.BinID()] = append(byBin[p.BinID()], p)
	}
	for _, g := range byBin {
		assertNoOverlap(t, g, bw, bd, bh)
	}
}

// Eight 5³ cubes fuse perfectly (stack → join → join) into a single solid
// 10×10×10 block, which EMS places in one bin with full utilisation.
func TestAssembler_FusesCubesIntoOneBin(t *testing.T) {
	a := d3.NewAssembler(10, 10, 10)
	var items []pack.Item
	for i := 0; i < 8; i++ {
		items = append(items, d3.NewItem("c"+string(rune('a'+i)), 5, 5, 5, true))
	}
	res, err := a.PackAll(items)
	if err != nil {
		t.Fatal(err)
	}
	ps, _ := collect3D(t, res)
	if res.BinsUsed() != 1 || len(ps) != 8 || len(res.Unplaced) != 0 {
		t.Fatalf("got %d placements / %d bins / %d unplaced, want 8 / 1 / 0", len(ps), res.BinsUsed(), len(res.Unplaced))
	}
	perBinNoOverlap(t, ps, 10, 10, 10)
	vol := 0.0
	for _, p := range ps {
		vol += p.W * p.D * p.H
	}
	if vol < 0.999*1000 {
		t.Errorf("packed volume %.0f, want 1000 (perfect fusion fills the bin)", vol)
	}
}

// Heterogeneous boxes that share a face must fuse: a 4×4×6 and a 4×4×4 share the
// 4×4 face and stack into a 4×4×10 column; rotation lets the assembler find it. The
// bin is 8×8×10 so the column tiles it (4|8, 10|10) and the bin-aligned gate keeps
// the merge.
func TestAssembler_FusesHeterogeneousSharedFace(t *testing.T) {
	a := d3.NewAssembler(8, 8, 10)
	res, err := a.PackAll([]pack.Item{
		d3.NewItem("tall", 4, 4, 6, true),
		d3.NewItem("short", 4, 4, 4, true),
	})
	if err != nil {
		t.Fatal(err)
	}
	ps, _ := collect3D(t, res)
	if len(ps) != 2 || len(res.Unplaced) != 0 {
		t.Fatalf("got %d placements / %d unplaced, want 2 / 0", len(ps), len(res.Unplaced))
	}
	perBinNoOverlap(t, ps, 8, 8, 10)
	// Fused as a 4×4×10 column, the two boxes share a footprint and stack flush:
	// their combined bounding box is exactly their summed volume (no internal gap).
	minX, minY, minZ := 1e18, 1e18, 1e18
	maxX, maxY, maxZ := -1e18, -1e18, -1e18
	vol := 0.0
	for _, p := range ps {
		minX, minY, minZ = minf(minX, p.X), minf(minY, p.Y), minf(minZ, p.Z)
		maxX, maxY, maxZ = maxf(maxX, p.X+p.W), maxf(maxY, p.Y+p.D), maxf(maxZ, p.Z+p.H)
		vol += p.W * p.D * p.H
	}
	bbox := (maxX - minX) * (maxY - minY) * (maxZ - minZ)
	if bbox > vol+1e-6 {
		t.Errorf("bounding box %.1f exceeds packed volume %.1f — boxes did not fuse flush", bbox, vol)
	}
}

func TestAssembler_StreamsAndNoOverlap(t *testing.T) {
	a := d3.NewAssembler(10, 10, 10)
	var streamed int
	a.Observe(func(pack.Placement) { streamed++ })

	var items []pack.Item
	for i := 0; i < 40; i++ {
		w := float64(2 + i%4) // 2..5
		items = append(items, d3.NewItem("i"+string(rune('a'+i%26))+string(rune('0'+i/26)), w, w, w, true))
	}
	res, err := a.PackAll(items)
	if err != nil {
		t.Fatal(err)
	}
	ps, _ := collect3D(t, res)
	if streamed != len(ps) {
		t.Errorf("observer fired %d times, want %d", streamed, len(ps))
	}
	if len(res.Unplaced) != 0 {
		t.Errorf("%d items unplaced", len(res.Unplaced))
	}
	perBinNoOverlap(t, ps, 10, 10, 10)
}
