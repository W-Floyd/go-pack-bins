package packapi

import (
	"context"
	"testing"
)

func TestStrip2D_ReportsHeight(t *testing.T) {
	// Four 5×5 into a width-10 strip → height 10, all placed, one bin.
	req := PackRequest{
		Mode: "2d", Algorithm: "strip",
		Bin: BinSpec{Width: 10, Height: 1}, // height is ignored by strip
		Items: []ItemSpec{
			{ID: "a", Width: 5, Height: 5}, {ID: "b", Width: 5, Height: 5},
			{ID: "c", Width: 5, Height: 5}, {ID: "d", Width: 5, Height: 5},
		},
	}
	resp := PackCtx(context.Background(), req)
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if len(resp.Unplaced) != 0 {
		t.Fatalf("unplaced=%v, want none", resp.Unplaced)
	}
	if resp.BinsUsed != 1 {
		t.Fatalf("bins used=%d, want 1", resp.BinsUsed)
	}
	if resp.ReturnedHeight < 9.999 || resp.ReturnedHeight > 10.001 {
		t.Fatalf("returned height=%v, want ~10", resp.ReturnedHeight)
	}
	// Single-container objective must not claim a multi-bin lower bound.
	if resp.LowerBound != 0 {
		t.Fatalf("strip should not report a bin-count lower bound, got %d", resp.LowerBound)
	}
}

func TestStrip_RejectsConstraints(t *testing.T) {
	req := PackRequest{
		Mode: "2d", Algorithm: "strip",
		Bin:         BinSpec{Width: 10, Height: 10},
		Items:       []ItemSpec{{ID: "a", Width: 5, Height: 5}},
		Constraints: []ConstraintSpec{{Scalar: "weight", Op: "max-sum", Value: 10}},
	}
	resp := PackCtx(context.Background(), req)
	if resp.Error == "" {
		t.Fatalf("expected an error when strip is given constraints")
	}
}

func TestKnapsack2D_PicksHighValueAndRejects(t *testing.T) {
	// Bin holds one 10×10; offer a cheap and a rich one — rich is selected.
	req := PackRequest{
		Mode: "2d", Algorithm: "knapsack",
		Bin: BinSpec{Width: 10, Height: 10},
		Items: []ItemSpec{
			{ID: "cheap", Width: 10, Height: 10, Scalars: map[string]float64{"value": 1}},
			{ID: "rich", Width: 10, Height: 10, Scalars: map[string]float64{"value": 100}},
		},
	}
	resp := PackCtx(context.Background(), req)
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if resp.BinsUsed != 1 {
		t.Fatalf("bins used=%d, want 1", resp.BinsUsed)
	}
	if resp.TotalValue != 100 {
		t.Fatalf("total value=%v, want 100 (picked rich)", resp.TotalValue)
	}
	if len(resp.Rejected) != 1 || resp.Rejected[0] != "cheap" {
		t.Fatalf("rejected=%v, want [cheap]", resp.Rejected)
	}
}

func TestKnapsack3D_FitsAll(t *testing.T) {
	var items []ItemSpec
	for i := 0; i < 4; i++ {
		items = append(items, ItemSpec{ID: string(rune('a' + i)), Width: 5, Depth: 5, Height: 5,
			Scalars: map[string]float64{"value": float64(i + 1)}})
	}
	req := PackRequest{Mode: "3d", Algorithm: "knapsack", Bin: BinSpec{Width: 10, Depth: 10, Height: 10}, Items: items}
	resp := PackCtx(context.Background(), req)
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if len(resp.Rejected) != 0 {
		t.Fatalf("rejected=%v, want none (all fit)", resp.Rejected)
	}
	if resp.TotalValue != 10 {
		t.Fatalf("total value=%v, want 10", resp.TotalValue)
	}
}
