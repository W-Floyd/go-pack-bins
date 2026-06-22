package gbpp_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/W-Floyd/go-pack-bins/d3"
	"github.com/W-Floyd/go-pack-bins/gbpp"
	"github.com/W-Floyd/go-pack-bins/offline"
	"github.com/W-Floyd/go-pack-bins/pack"
)

// Regression for testdata/1.json: optional items must consolidate with
// compulsory ones into a single bin, not spill into an extra bin. Four 6³ cubes
// (two of them optional) plus 27 3³ cubes all fit one 12³ bin; the earlier
// compulsory-then-optional GBPP used two. The improved Pack should match FFD.
func TestGBPPConsolidatesWithCompulsory(t *testing.T) {
	items := func() []pack.Item {
		its := []pack.Item{
			d3.NewItem("a", 6, 6, 6, false),
			d3.NewItem("b", 6, 6, 6, false),
			d3.NewItem("hi", 6, 6, 6, false).WithScalar("profit", 50),
			d3.NewItem("lo", 6, 6, 6, false).WithScalar("profit", 1),
		}
		for i := 0; i < 27; i++ {
			its = append(its, d3.NewItem("s"+strconv.Itoa(i), 3, 3, 3, false))
		}
		return its
	}
	factory := func() pack.BinFactory {
		return d3.NewFactory(12, 12, 12, d3.NewExtremePointStrategyContact(d3.ContactSpec{}))
	}

	ffd, _ := offline.FirstFitDecreasing(factory()).PackAll(items())
	g := gbpp.Pack(context.Background(), items(), factory(),
		gbpp.Options{BinCost: 5, ProfitScalar: "profit", OptionalScalar: "profit"})

	if g.BinsUsed() != ffd.BinsUsed() {
		t.Fatalf("GBPP used %d bins, FFD used %d — should match (no spill)", g.BinsUsed(), ffd.BinsUsed())
	}
	if g.BinsUsed() != 1 {
		t.Fatalf("expected all 31 items in 1 bin, got %d", g.BinsUsed())
	}
	if len(g.Rejected) != 0 {
		t.Fatalf("nothing should be rejected when everything fits one bin, got %v", g.Rejected)
	}
	if g.IncludedProfit != 51 { // 50 + 1, both optional fit the shared bin for free
		t.Fatalf("expected included profit 51, got %g", g.IncludedProfit)
	}
}
