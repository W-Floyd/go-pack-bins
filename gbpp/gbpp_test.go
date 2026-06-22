package gbpp_test

import (
	"context"
	"testing"

	"github.com/W-Floyd/go-pack-bins/d1"
	"github.com/W-Floyd/go-pack-bins/gbpp"
	"github.com/W-Floyd/go-pack-bins/pack"
)

func opt(id string, size, profit float64) pack.Item {
	return d1.NewItem(id, size).WithScalar("optional", 1).WithScalar("profit", profit)
}

// GBPP must always pack compulsory items, include optional items that fit free
// space or that earn more than a bin costs, and reject optional items that would
// need a bin they can't pay for.
func TestGBPPSelectsByNetCost(t *testing.T) {
	const cap, binCost = 10.0, 50.0
	items := []pack.Item{
		d1.NewItem("c1", 6), // compulsory
		d1.NewItem("c2", 6), // compulsory → 2 bins, 4 free each
		opt("x", 4, 100),    // fits free space of a bin → include
		opt("y", 4, 100),    // fits the other bin → include
		opt("w", 10, 100),   // needs a new bin, profit ≥ cost → include
		opt("z", 10, 1),     // needs a new bin, profit < cost → reject
	}
	res := gbpp.Pack(context.Background(), items, d1.NewFactory(cap), gbpp.Options{BinCost: binCost})

	if got := rejectedSet(res); !got["z"] || len(got) != 1 {
		t.Fatalf("expected only z rejected, got %v", res.Rejected)
	}
	if res.IncludedProfit != 300 {
		t.Fatalf("expected included profit 300 (x+y+w), got %g", res.IncludedProfit)
	}
	if res.BinsUsed() != 3 {
		t.Fatalf("expected 3 bins (2 compulsory + 1 for w), got %d", res.BinsUsed())
	}
	if res.NetCost != binCost*3-300 {
		t.Fatalf("expected net cost %g, got %g", binCost*3-300, res.NetCost)
	}
	// Compulsory items must never be rejected.
	for _, id := range res.Unplaced {
		if id == "c1" || id == "c2" {
			t.Fatalf("compulsory item %s was left unplaced", id)
		}
	}
}

// With zero bin cost, every optional item that fits should be included.
func TestGBPPFreeBinsIncludeAll(t *testing.T) {
	items := []pack.Item{
		d1.NewItem("c", 5),
		opt("a", 5, 10),
		opt("b", 5, 10),
	}
	res := gbpp.Pack(context.Background(), items, d1.NewFactory(10), gbpp.Options{})
	if len(res.Rejected) != 0 {
		t.Fatalf("with free bins nothing should be rejected, got %v", res.Rejected)
	}
	if res.IncludedProfit != 20 {
		t.Fatalf("expected profit 20, got %g", res.IncludedProfit)
	}
}

func rejectedSet(r gbpp.Result) map[string]bool {
	m := make(map[string]bool, len(r.Rejected))
	for _, id := range r.Rejected {
		m[id] = true
	}
	return m
}
