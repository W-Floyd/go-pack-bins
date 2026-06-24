package packapi

import (
	"context"
	"testing"
)

// TestSATCertifiesOptimum drives the SAT exact solver through the public API and
// checks the optimality certificate surfaces in the response.
func TestSATCertifiesOptimum(t *testing.T) {
	// Four 6×6 items in a 10×10 bin: two don't fit side by side (12>10), so each
	// needs its own bin. Optimum is 4 (area bound is only 2), proving 2 and 3 UNSAT.
	items := make([]ItemSpec, 4)
	for i := range items {
		items[i] = ItemSpec{ID: string(rune('a' + i)), Width: 6, Height: 6}
	}
	req := PackRequest{Mode: "2d", Algorithm: "sat", Bin: BinSpec{Width: 10, Height: 10}, Items: items}
	resp := PackCtx(context.Background(), req)
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if resp.BinsUsed != 4 {
		t.Errorf("BinsUsed=%d, want 4 (proof=%q)", resp.BinsUsed, resp.OptimalityProof)
	}
	if !resp.ProvenOptimal {
		t.Errorf("expected ProvenOptimal=true")
	}
	if resp.LowerBound != 2 {
		t.Errorf("LowerBound=%d, want 2", resp.LowerBound)
	}
	if len(resp.Unplaced) != 0 {
		t.Errorf("got %d unplaced, want 0", len(resp.Unplaced))
	}
}

// TestSATMemoryBudgetForcesFallback confirms the "Memory budget" UI tunable is
// honoured: a tiny budget shrinks the derived clause cap so the instance exceeds it,
// and the solver degrades to the heuristic packing (uncertified) instead of
// certifying. The same instance certifies at the default budget.
func TestSATMemoryBudgetForcesFallback(t *testing.T) {
	items := make([]ItemSpec, 20)
	for i := range items {
		items[i] = ItemSpec{ID: string(rune('a' + i)), Width: 5, Height: 5}
	}
	bin := BinSpec{Width: 50, Height: 50}

	// Default budget: certifies.
	base := PackRequest{Mode: "2d", Algorithm: "sat", Bin: bin, Items: items}
	if resp := PackCtx(context.Background(), base); resp.Error != "" || !resp.ProvenOptimal {
		t.Fatalf("default budget should certify: error=%q optimal=%v", resp.Error, resp.ProvenOptimal)
	}

	// 1 MB budget: clause cap ≈4k < this instance's clauses → fallback, uncertified.
	tiny := base
	tiny.AlgorithmOptions = map[string]float64{"sat_max_memory_mb": 1}
	resp := PackCtx(context.Background(), tiny)
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if resp.ProvenOptimal {
		t.Errorf("with a 1 MB budget the solve must not certify optimality")
	}
	if len(resp.Placements) != len(items) {
		t.Errorf("expected a heuristic packing of all items, got %d placements", len(resp.Placements))
	}
}

// TestSATRejectsConstraints confirms the exact solver refuses scalar constraints
// it cannot honour rather than silently ignoring them.
func TestSATRejectsConstraints(t *testing.T) {
	req := PackRequest{
		Mode: "2d", Algorithm: "sat", Bin: BinSpec{Width: 10, Height: 10},
		Items:       []ItemSpec{{ID: "a", Width: 4, Height: 4}},
		Constraints: []ConstraintSpec{{Scalar: "weight", Op: "max", Value: 5}},
	}
	resp := PackCtx(context.Background(), req)
	if resp.Error == "" {
		t.Fatalf("expected an error when constraints are present, got none")
	}
}
