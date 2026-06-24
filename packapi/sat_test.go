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

// TestSATMaxClausesForcesFallback confirms the "Max clauses" UI tunable is honoured:
// a tiny cap makes even a small instance exceed it, so the solver degrades to the
// heuristic packing (uncertified) instead of certifying.
func TestSATMaxClausesForcesFallback(t *testing.T) {
	items := make([]ItemSpec, 4)
	for i := range items {
		items[i] = ItemSpec{ID: string(rune('a' + i)), Width: 6, Height: 6}
	}
	req := PackRequest{
		Mode: "2d", Algorithm: "sat", Bin: BinSpec{Width: 10, Height: 10}, Items: items,
		AlgorithmOptions: map[string]float64{"sat_max_clauses": 1}, // scaled value, as the UI sends it
	}
	resp := PackCtx(context.Background(), req)
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if resp.ProvenOptimal {
		t.Errorf("with a 1-clause cap the solve must not certify optimality")
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
