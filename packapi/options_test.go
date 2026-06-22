package packapi

import (
	"context"
	"testing"
)

// optInt: absent/invalid → 0 (solver default); present → clamped to [1, max].
func TestOptIntClamp(t *testing.T) {
	req := PackRequest{AlgorithmOptions: map[string]float64{
		"in_range": 10,
		"too_big":  1e9,
		"zero":     0,
		"negative": -5,
	}}
	cases := []struct {
		key  string
		max  int
		want int
	}{
		{"in_range", 64, 10}, // within range, returned as-is
		{"too_big", 64, 64},  // clamped down to the ceiling
		{"zero", 64, 0},      // non-positive → default sentinel
		{"negative", 64, 0},  // non-positive → default sentinel
		{"absent", 64, 0},    // missing key → default sentinel
	}
	for _, c := range cases {
		if got := req.optInt(c.key, c.max); got != c.want {
			t.Errorf("optInt(%q, %d) = %d, want %d", c.key, c.max, got, c.want)
		}
	}
}

// The brute-force cap is the critical guardrail: a wild UI value must clamp to
// maxBruteForceMaxItems so n! can never hang the solve.
func TestBruteForceMaxItemsClamped(t *testing.T) {
	req := PackRequest{AlgorithmOptions: map[string]float64{"brute_max_items": 100}}
	if got := req.bruteForceOptions(context.Background(), nil).MaxItems; got != maxBruteForceMaxItems {
		t.Fatalf("brute_max_items clamped to %d, want %d", got, maxBruteForceMaxItems)
	}
	// Absent → 0 so the solver applies DefaultBruteForceMaxItems.
	if got := (PackRequest{}).bruteForceOptions(context.Background(), nil).MaxItems; got != 0 {
		t.Fatalf("absent brute_max_items = %d, want 0 (solver default)", got)
	}
}

// Beam/search/refine builders map keys through, clamp ceilings, and pass the
// seed unclamped.
func TestOptionBuildersMapAndClamp(t *testing.T) {
	req := PackRequest{AlgorithmOptions: map[string]float64{
		"beam_width":         8,
		"beam_branch":        1e6, // clamps to maxBeamBranch
		"search_max_iters":   5000,
		"search_seed":        42,
		"refine_eval_budget": 1e12, // clamps to maxRefineEvalBudget
	}}
	if b := req.beamOptions(context.Background()); b.Width != 8 || b.Branch != maxBeamBranch {
		t.Fatalf("beamOptions = %+v, want Width=8 Branch=%d", b, maxBeamBranch)
	}
	if s := req.searchOptions(context.Background()); s.MaxIters != 5000 || s.Seed != 42 {
		t.Fatalf("searchOptions = %+v, want MaxIters=5000 Seed=42", s)
	}
	if r := req.refineOptions(); r.EvalBudget != maxRefineEvalBudget {
		t.Fatalf("refineOptions.EvalBudget = %d, want %d", r.EvalBudget, maxRefineEvalBudget)
	}
}

// Nested end-to-end: per-level AlgorithmOptions flow into each level's solve. The
// inner level carries an absurd brute_max_items that must clamp (not hang), and
// the outer level carries beam tunables; both levels still solve cleanly.
func TestPackNestedWithOptions(t *testing.T) {
	resp, err := PackNested(NestedPackRequest{
		Mode:  "3d",
		Items: cubes(8, 3),
		Levels: []NestedLevelSpec{
			{Algorithm: "brute", Bin: BinSpec{Width: 6, Height: 6, Depth: 6},
				AlgorithmOptions: map[string]float64{"brute_max_items": 9999}}, // clamps to 11 → FFD fallback
			{Algorithm: "beam", Bin: BinSpec{Width: 30, Height: 30, Depth: 30},
				AlgorithmOptions: map[string]float64{"beam_width": 8, "beam_branch": 5}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Levels) != 2 {
		t.Fatalf("expected 2 levels, got %d", len(resp.Levels))
	}
	if l0 := resp.Levels[0]; len(l0.Placements) != 8 || len(l0.Unplaced) != 0 {
		t.Fatalf("level 0: expected 8 placed / 0 unplaced, got %d / %v", len(l0.Placements), l0.Unplaced)
	}
	if resp.Levels[1].BinsUsed == 0 {
		t.Fatal("level 1: expected a non-empty packing")
	}
}

// End-to-end: a beam request carrying tunables still solves cleanly, proving the
// options flow through dispatch without breaking the solve.
func TestPackBeamWithOptions(t *testing.T) {
	resp := Pack(PackRequest{
		Mode:             "3d",
		Algorithm:        "beam",
		Bin:              BinSpec{Width: 10, Height: 10, Depth: 10},
		Items:            cubes(8, 5),
		AlgorithmOptions: map[string]float64{"beam_width": 8, "beam_branch": 5},
	})
	if resp.Error != "" {
		t.Fatal(resp.Error)
	}
	if resp.BinsUsed == 0 {
		t.Fatal("expected a non-empty packing")
	}
}
