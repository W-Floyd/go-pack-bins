package d3

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/W-Floyd/go-pack-bins/pack"
)

// topHeight returns the highest top edge over all placements.
func topHeight(pr pack.Result) float64 {
	m := 0.0
	for _, p := range pr.Placements {
		if p3, ok := p.(*Placement3D); ok {
			if t := p3.Z + p3.H; t > m {
				m = t
			}
		}
	}
	return m
}

// TestBlockPacker_FinalLayerLaidFlat guards that the final-layer leftovers are
// placed in their flattest orientation (lowest top), not stood up. A dense cube
// body (height 8) plus rotatable 1×6×6 slabs: laid flat each adds height 1, so the
// peak must stay near the body, far below the ~14 it would reach if a slab stood on
// its 6-tall edge.
func TestBlockPacker_FinalLayerLaidFlat(t *testing.T) {
	var items []pack.Item
	for i := 0; i < 256; i++ { // 2×2×2 cubes: 64 per 16×16 layer × 4 layers = height 8
		items = append(items, NewItem(fmt.Sprintf("c%d", i), 2, 2, 2, false))
	}
	for i := 0; i < 4; i++ { // rotatable slabs: flattest height 1, tallest 6
		items = append(items, NewItem(fmt.Sprintf("s%d", i), 1, 6, 6, true))
	}
	pr, err := NewBlockPacker(16, 16, 500).PackAll(items)
	if err != nil || len(pr.Unplaced) != 0 {
		t.Fatalf("pack err=%v unplaced=%d", err, len(pr.Unplaced))
	}
	if peak := topHeight(pr); peak > 11 {
		t.Fatalf("peak %.0f too tall — final-layer slabs stood up instead of lying flat (flat ≈ 9–10)", peak)
	}
}

// TestBlockPacker_TallBinUsesBodyNotLAFF guards the fix for the endgame degenerating
// to a single LAFF pass in a very tall bin. The endgame must gate on a bounded
// working height, so the block-building body (with vertical fusion) runs for the
// bulk of a heterogeneous instance instead of laying everything flat. The body must
// therefore pack a tall bin meaningfully tighter than LAFF alone, and close to the
// volume lower bound — matching what it achieves in bounded bins.
func TestBlockPacker_TallBinUsesBodyNotLAFF(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	var items []pack.Item
	vol := 0.0
	for i := 0; i < 400; i++ {
		w := float64(2 + rng.Intn(5))
		d := float64(2 + rng.Intn(5))
		h := float64(1 + rng.Intn(6))
		items = append(items, NewItem(fmt.Sprintf("i%d", i), w, d, h, true))
		vol += w * d * h
	}
	const base = 20.0
	volBound := vol / (base * base)

	blk, err := NewBlockPacker(base, base, 2000).PackAll(items) // very tall bin
	if err != nil {
		t.Fatalf("block pack: %v", err)
	}
	if len(blk.Unplaced) != 0 {
		t.Fatalf("block pack left %d unplaced", len(blk.Unplaced))
	}
	laff, _ := LAFF(items, base, base, 2000)

	bh, lh := topHeight(blk), topHeight(laff)
	// The body must run: block packing a tall bin must beat the flat-only LAFF
	// degeneration by a clear margin, and land near the volume bound.
	if bh >= lh {
		t.Fatalf("tall-bin block height %.1f not better than LAFF %.1f — endgame degenerated to LAFF", bh, lh)
	}
	if bh > volBound*1.2 {
		t.Fatalf("tall-bin block height %.1f exceeds 1.2× volume bound %.1f — body not packing densely", bh, volBound)
	}
}
