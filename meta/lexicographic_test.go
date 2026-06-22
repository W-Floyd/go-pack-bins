package meta_test

import (
	"testing"

	"github.com/W-Floyd/go-pack-bins/d1"
	"github.com/W-Floyd/go-pack-bins/meta"
	"github.com/W-Floyd/go-pack-bins/offline"
	"github.com/W-Floyd/go-pack-bins/pack"
)

func lexItems(sizes ...float64) []pack.Item {
	out := make([]pack.Item, len(sizes))
	for i, s := range sizes {
		out[i] = d1.NewItem(string(rune('a'+i)), s)
	}
	return out
}

// With BinsUsed as the top objective, LexBestOf must pick the candidate using
// the fewest bins, breaking ties by the secondary metric.
func TestLexBestOfPrioritisesBins(t *testing.T) {
	cap := 10.0
	items := lexItems(6, 4, 6, 4) // FFD → 2 bins; NextFit (in arrival order) → also 2 here
	p := meta.LexBestOf(
		[]meta.Metric{meta.Unplaced, meta.BinsUsed, meta.UtilizationSpread()},
		offline.FirstFitDecreasing(d1.NewFactory(cap)),
		offline.NextFitDecreasing(d1.NewFactory(cap)),
		offline.BestFitDecreasing(d1.NewFactory(cap)),
	)
	r, err := p.PackAll(items)
	if err != nil {
		t.Fatal(err)
	}
	if r.BinsUsed() != 2 {
		t.Fatalf("expected 2 bins, got %d", r.BinsUsed())
	}
	if len(r.Unplaced) != 0 {
		t.Fatalf("unexpected unplaced: %v", r.Unplaced)
	}
	if p.Winner() == "" {
		t.Fatal("expected a winning candidate name")
	}
}

// The lexicographic order matters: a metric earlier in the list dominates later
// ones. Here a deliberately bin-greedy primary still yields the min-bin result.
func TestLexOrderingDominates(t *testing.T) {
	cap := 10.0
	items := lexItems(7, 3, 7, 3, 5, 5)
	// Primary: minimise unplaced; secondary: minimise bins.
	p := meta.LexBestOf(
		[]meta.Metric{meta.Unplaced, meta.BinsUsed},
		offline.FirstFitDecreasing(d1.NewFactory(cap)),
		offline.WorstFitDecreasing(d1.NewFactory(cap)),
	)
	r, _ := p.PackAll(items)
	// All items fit, so unplaced ties at 0 and bins decides: best achievable is 3.
	if len(r.Unplaced) != 0 || r.BinsUsed() != 3 {
		t.Fatalf("expected 0 unplaced and 3 bins, got %d unplaced / %d bins", len(r.Unplaced), r.BinsUsed())
	}
}
