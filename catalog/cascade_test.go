package catalog_test

import (
	"context"
	"testing"

	"github.com/W-Floyd/go-pack-bins/catalog"
)

// When every container type has a max count too small to hold the whole order on
// its own, the order should still pack by spilling across types (2 of A + 1 of B
// here). With the single-type selection this fails — items beyond the chosen
// type's cap are dropped instead of going into the next available size.
func TestCatalogCascadesAcrossSizes(t *testing.T) {
	items := items1D(8, 8, 8) // each needs its own cap-10 bin → 3 bins total
	cands := []catalog.Candidate{
		{Label: "A(cap10,max2)", MaxCount: 2, BinVolume: 10, Pack: ffdInto(10)},
		{Label: "B(cap10,max2)", MaxCount: 2, BinVolume: 10, Pack: ffdInto(10)},
	}
	res, err := catalog.PackSequential(context.Background(), items, cands)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Unplaced) != 0 {
		t.Fatalf("expected all 3 items placed by spilling across A and B, got %d unplaced: %v",
			len(res.Unplaced), res.Unplaced)
	}
	placed := 0
	for _, p := range res.Placements {
		if p != nil {
			placed++
		}
	}
	if placed != 3 {
		t.Fatalf("expected 3 placements, got %d", placed)
	}
}
