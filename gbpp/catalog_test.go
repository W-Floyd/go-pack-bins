package gbpp_test

import (
	"context"
	"testing"

	"github.com/W-Floyd/go-pack-bins/d1"
	"github.com/W-Floyd/go-pack-bins/gbpp"
	"github.com/W-Floyd/go-pack-bins/pack"
)

func binType(label string, cap, cost float64, max int) gbpp.BinType {
	return gbpp.BinType{Label: label, Cost: cost, MaxCount: max, Open: func() pack.Bin { return d1.NewFactory(cap).Open() }}
}

// PackCatalog should open the cheapest suitable type per item — three small
// cheap bins rather than one big expensive one that could also hold all three.
func TestPackCatalogPrefersCheapMix(t *testing.T) {
	types := []gbpp.BinType{
		binType("small", 5, 1, 0), // each holds one size-5 item, cost 1
		binType("big", 20, 20, 0), // could hold all three, but cost 20
	}
	items := []pack.Item{d1.NewItem("a", 5), d1.NewItem("b", 5), d1.NewItem("c", 5)}
	r := gbpp.PackCatalog(context.Background(), items, types, gbpp.Options{})
	if len(r.Unplaced) != 0 {
		t.Fatalf("unplaced: %v", r.Unplaced)
	}
	if r.BinsUsed() != 3 {
		t.Fatalf("expected 3 small bins, got %d", r.BinsUsed())
	}
	for i, ti := range r.BinTypeIdx {
		if ti != 0 {
			t.Fatalf("bin %d used type %d, expected the cheap 'small' type (0)", i, ti)
		}
	}
	if r.NetCost != 3 {
		t.Fatalf("expected net cost 3 (3 small bins), got %g", r.NetCost)
	}
}

// Optional items: included when they fit free space; an optional that would need
// a new bin is taken only if its profit covers that bin's cost.
func TestPackCatalogOptionalGatedByCost(t *testing.T) {
	types := []gbpp.BinType{binType("std", 10, 5, 0)}
	items := []pack.Item{
		d1.NewItem("c", 6),                          // compulsory → 1 bin (cost 5)
		d1.NewItem("x", 4).WithScalar("profit", 3),  // optional, fits the bin's free 4 → include
		d1.NewItem("z", 10).WithScalar("profit", 2), // optional, needs a new bin, 2 < 5 → reject
	}
	r := gbpp.PackCatalog(context.Background(), items, types, gbpp.Options{ProfitScalar: "profit", OptionalScalar: "profit"})
	if len(r.Rejected) != 1 || r.Rejected[0] != "z" {
		t.Fatalf("expected only z rejected, got %v", r.Rejected)
	}
	if r.IncludedProfit != 3 {
		t.Fatalf("expected included profit 3, got %g", r.IncludedProfit)
	}
	if r.BinsUsed() != 1 || r.NetCost != 5-3 {
		t.Fatalf("expected 1 bin and net cost 2, got %d bins / %g", r.BinsUsed(), r.NetCost)
	}
}
