package online_test

import (
	"testing"

	"github.com/W-Floyd/go-pack-bins/d1"
	"github.com/W-Floyd/go-pack-bins/online"
	"github.com/W-Floyd/go-pack-bins/pack"
)

func TestSumOfSquares_BeatsNextFitOnLevels(t *testing.T) {
	// cap 10, items 5,6,5,4. Next Fit needs 3 bins ({5},{6},{5,4}); the optimal
	// is 2 ({5,5},{6,4}). Sum-of-Squares keeps levels balanced and finds 2.
	items := []pack.Item{d1.NewItem("a", 5), d1.NewItem("b", 6), d1.NewItem("c", 5), d1.NewItem("d", 4)}

	ss := online.SumOfSquares(10, d1.NewFactory(10))
	for _, it := range items {
		if _, err := ss.Pack(it); err != nil {
			t.Fatalf("pack %s: %v", it.ID(), err)
		}
	}
	if got := ss.Result().BinsUsed(); got != 2 {
		t.Errorf("SS used %d bins, want 2", got)
	}

	nf := online.NextFit(d1.NewFactory(10))
	for _, it := range items {
		nf.Pack(it)
	}
	if nf.Result().BinsUsed() != 3 {
		t.Logf("note: NextFit used %d bins (expected the weaker 3)", nf.Result().BinsUsed())
	}
}

func TestSumOfSquares_PlacesAllWithinCapacity(t *testing.T) {
	ss := online.SumOfSquares(10, d1.NewFactory(10))
	sizes := []float64{3, 7, 2, 8, 5, 5, 1, 9, 4, 6}
	for i, s := range sizes {
		if _, err := ss.Pack(d1.NewItem(string(rune('a'+i)), s)); err != nil {
			t.Fatalf("pack: %v", err)
		}
	}
	r := ss.Result()
	if len(r.Unplaced) != 0 {
		t.Errorf("unplaced items: %v", r.Unplaced)
	}
	// No bin may exceed capacity.
	for _, b := range r.Bins {
		if b.Remaining() < -1e-9 {
			t.Errorf("bin %s overfilled (remaining %v)", b.ID(), b.Remaining())
		}
	}
}
