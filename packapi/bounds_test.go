package packapi

import (
	"context"
	"testing"
)

// A heuristic 2-D solve should report the geometric lower bound, and certify
// optimality when it meets it.
func TestPack2D_ReportsLowerBoundAndCertifies(t *testing.T) {
	// Four 5×5 items into a 10×10 bin: they tile into a single bin, and the area
	// bound is 1, so FFD must be reported as proven optimal with LowerBound 1.
	req := PackRequest{
		Mode:      "2d",
		Algorithm: "ffd",
		Bin:       BinSpec{Width: 10, Height: 10},
		Items: []ItemSpec{
			{ID: "a", Width: 5, Height: 5},
			{ID: "b", Width: 5, Height: 5},
			{ID: "c", Width: 5, Height: 5},
			{ID: "d", Width: 5, Height: 5},
		},
	}
	resp := PackCtx(context.Background(), req)
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if resp.BinsUsed != 1 {
		t.Fatalf("bins used = %d, want 1", resp.BinsUsed)
	}
	if resp.LowerBound != 1 {
		t.Fatalf("lower bound = %d, want 1", resp.LowerBound)
	}
	if !resp.ProvenOptimal {
		t.Fatalf("expected ProvenOptimal=true when bins == lower bound")
	}
}

func TestPack2D_ReportsGapWhenLoose(t *testing.T) {
	// Three 6×6 items into a 10×10 bin: big-item bound forces 3 bins, and FFD also
	// uses 3, so it certifies optimal via the big-item bound.
	req := PackRequest{
		Mode:      "2d",
		Algorithm: "ffd",
		Bin:       BinSpec{Width: 10, Height: 10},
		Items: []ItemSpec{
			{ID: "a", Width: 6, Height: 6},
			{ID: "b", Width: 6, Height: 6},
			{ID: "c", Width: 6, Height: 6},
		},
	}
	resp := PackCtx(context.Background(), req)
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if resp.LowerBound != 3 {
		t.Fatalf("lower bound = %d, want 3 (big-item bound)", resp.LowerBound)
	}
	if resp.BinsUsed < resp.LowerBound {
		t.Fatalf("bins %d below lower bound %d (impossible)", resp.BinsUsed, resp.LowerBound)
	}
}

func TestPack3D_ReportsLowerBound(t *testing.T) {
	// Eight 5×5×5 cubes into a 10×10×10 bin tile into one bin; volume bound 1.
	var items []ItemSpec
	for i := 0; i < 8; i++ {
		items = append(items, ItemSpec{ID: string(rune('a' + i)), Width: 5, Depth: 5, Height: 5})
	}
	req := PackRequest{
		Mode:      "3d",
		Algorithm: "ffd",
		Bin:       BinSpec{Width: 10, Depth: 10, Height: 10},
		Items:     items,
	}
	resp := PackCtx(context.Background(), req)
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if resp.LowerBound != 1 {
		t.Fatalf("lower bound = %d, want 1", resp.LowerBound)
	}
	if resp.BinsUsed < resp.LowerBound {
		t.Fatalf("bins %d below lower bound %d (impossible)", resp.BinsUsed, resp.LowerBound)
	}
}
