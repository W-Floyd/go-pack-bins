package catalog_test

import (
	"context"
	"testing"

	"github.com/W-Floyd/go-pack-bins/catalog"
	"github.com/W-Floyd/go-pack-bins/d1"
	"github.com/W-Floyd/go-pack-bins/offline"
	"github.com/W-Floyd/go-pack-bins/pack"
)

// build returns a Pack closure that FFD-packs into bins of the given capacity.
func ffdInto(cap float64) func(context.Context, []pack.Item) (pack.Result, error) {
	return func(ctx context.Context, items []pack.Item) (pack.Result, error) {
		return offline.FirstFitDecreasing(d1.NewFactory(cap)).PackAllCtx(ctx, items)
	}
}

func items1D(sizes ...float64) []pack.Item {
	out := make([]pack.Item, len(sizes))
	for i, s := range sizes {
		out[i] = d1.NewItem(string(rune('a'+i)), s)
	}
	return out
}

// With items summing to 12, a capacity-10 container needs 2 bins (waste 8) while
// a capacity-12 container needs 1 (waste 0): the catalog should pick the latter.
func TestBestPicksTighterContainer(t *testing.T) {
	items := items1D(5, 4, 3) // sum 12
	cands := []catalog.Candidate{
		{Label: "small(10)", BinVolume: 10, Pack: ffdInto(10)},
		{Label: "exact(12)", BinVolume: 12, Pack: ffdInto(12)},
	}
	res, err := catalog.Best(context.Background(), items, cands)
	if err != nil {
		t.Fatal(err)
	}
	if res.Label != "exact(12)" {
		t.Fatalf("expected exact(12) to win, got %q with %d bins", res.Label, res.BinsUsed())
	}
	if res.BinsUsed() != 1 {
		t.Fatalf("expected 1 bin, got %d", res.BinsUsed())
	}
}

// MaxCount caps containers: with only 1 small container allowed, items that
// spill past it are reported unplaced.
func TestMaxCountTruncates(t *testing.T) {
	items := items1D(6, 6, 6) // needs 3 bins of cap 6 (one each)
	cands := []catalog.Candidate{
		{Label: "cap6 max1", MaxCount: 1, BinVolume: 6, Pack: ffdInto(6)},
	}
	res, err := catalog.Best(context.Background(), items, cands)
	if err != nil {
		t.Fatal(err)
	}
	if res.BinsUsed() != 1 {
		t.Fatalf("expected 1 bin (capped), got %d", res.BinsUsed())
	}
	if len(res.Unplaced) != 2 {
		t.Fatalf("expected 2 unplaced (spilled past max count), got %d: %v", len(res.Unplaced), res.Unplaced)
	}
}
