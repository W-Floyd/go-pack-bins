package packapi

import (
	"context"
	"testing"
)

// TestAutoSweepFindsOptimalChunky guards the multi-objective seed-sweep escalation.
// The 3D-combinatorial instance (24 sized boxes into 12³) has volume lower bound 3,
// but the one-shot constructive packers strand a 4th bin; only the order-search
// found 3. With the sweep, "auto" itself now reaches the optimal 3-bin packing.
func TestAutoSweepFindsOptimalChunky(t *testing.T) {
	sc, ok := BenchScenarioBySlug("3d-chunky")
	if !ok {
		t.Skip("3d-chunky scenario missing")
	}
	resp := PackCtx(context.Background(), PackRequest{
		Mode: "3d", Algorithm: "auto", Bin: sc.Bin, Items: sc.Items,
	})
	if resp.BinsUsed != 3 || len(resp.Unplaced) != 0 {
		t.Fatalf("auto chunky: bins=%d unplaced=%d, want 3/0 (sweep should reach the lower bound)", resp.BinsUsed, len(resp.Unplaced))
	}
}

// TestSweepNeverRegresses checks the escalation is safe: on a tiny instance that the
// constructive race already packs optimally, auto must still place everything and
// never use more bins than the volume lower bound.
func TestSweepNeverRegresses(t *testing.T) {
	var items []ItemSpec
	for i := 0; i < 8; i++ { // eight 5×5×5 cubes → 8 fit one 10×10×10 bin (2×2×2)
		items = append(items, ItemSpec{ID: string(rune('a' + i)), Width: 5, Depth: 5, Height: 5, AllowRotate: true})
	}
	resp := PackCtx(context.Background(), PackRequest{
		Mode: "3d", Algorithm: "auto", Bin: BinSpec{Width: 10, Depth: 10, Height: 10}, Items: items,
	})
	if resp.BinsUsed != 1 || len(resp.Unplaced) != 0 {
		t.Fatalf("auto 8-cubes: bins=%d unplaced=%d, want 1/0", resp.BinsUsed, len(resp.Unplaced))
	}
}
