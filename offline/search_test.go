package offline_test

import (
	"context"
	"testing"

	"github.com/W-Floyd/go-pack-bins/d1"
	"github.com/W-Floyd/go-pack-bins/offline"
	"github.com/W-Floyd/go-pack-bins/pack"
)

func searchItems(sizes ...float64) []pack.Item {
	out := make([]pack.Item, len(sizes))
	for i, s := range sizes {
		out[i] = d1.NewItem(string(rune('a'+i)), s)
	}
	return out
}

// Each metaheuristic must place everything and never use more bins than FFD
// (whose ordering is in their search space), on a feasible instance.
func TestSearchesNoWorseThanFFD(t *testing.T) {
	const cap = 10
	items := searchItems(7, 2, 6, 3, 4, 5, 8, 1, 5, 4) // sum 45 → 5 bins lower bound
	ffd, _ := offline.FirstFitDecreasing(d1.NewFactory(cap)).PackAll(items)

	cases := map[string]func() pack.Result{
		"RuinRecreate": func() pack.Result {
			return offline.RuinRecreate(context.Background(), items, d1.NewFactory(cap), offline.SearchOptions{MaxIters: 300})
		},
		"AdaptiveRuinRecreate": func() pack.Result {
			return offline.AdaptiveRuinRecreate(context.Background(), items, d1.NewFactory(cap), offline.SearchOptions{MaxIters: 300})
		},
		"GRASP": func() pack.Result {
			return offline.GRASP(context.Background(), items, d1.NewFactory(cap), offline.SearchOptions{MaxIters: 300})
		},
		"BeamSearch": func() pack.Result {
			return offline.BeamSearch(context.Background(), items, d1.NewFactory(cap), offline.BeamOptions{})
		},
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			r := run()
			if len(r.Unplaced) != 0 {
				t.Fatalf("%s left items unplaced: %v", name, r.Unplaced)
			}
			placed := 0
			for _, p := range r.Placements {
				if p != nil {
					placed++
				}
			}
			if placed != len(items) {
				t.Fatalf("%s placed %d of %d items", name, placed, len(items))
			}
			if r.BinsUsed() > ffd.BinsUsed() {
				t.Fatalf("%s used %d bins, worse than FFD's %d", name, r.BinsUsed(), ffd.BinsUsed())
			}
		})
	}
}

// Searches must be reproducible given the same seed.
func TestSearchDeterministic(t *testing.T) {
	items := searchItems(5, 4, 3, 6, 2, 7, 1, 8)
	mk := func() pack.Result {
		return offline.RuinRecreate(context.Background(), items, d1.NewFactory(10), offline.SearchOptions{Seed: 42, MaxIters: 200})
	}
	a, b := mk(), mk()
	if a.BinsUsed() != b.BinsUsed() || len(a.Unplaced) != len(b.Unplaced) {
		t.Fatalf("same seed gave different results: %d/%d vs %d/%d",
			a.BinsUsed(), len(a.Unplaced), b.BinsUsed(), len(b.Unplaced))
	}
}

// A cancelled context returns promptly with a valid (initial) packing.
func TestSearchCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	items := searchItems(3, 3, 3, 3, 3, 3)
	r := offline.RuinRecreate(ctx, items, d1.NewFactory(9), offline.SearchOptions{MaxIters: 1 << 20})
	if r.BinsUsed() == 0 {
		t.Fatal("expected at least the initial FFD packing even when cancelled")
	}
}
