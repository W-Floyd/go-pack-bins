package meta_test

import (
	"testing"

	"github.com/W-Floyd/go-pack-bins/d1"
	"github.com/W-Floyd/go-pack-bins/meta"
	"github.com/W-Floyd/go-pack-bins/pack"
)

// fixedResult builds a meta.Func that returns a result occupying n bins.
func fixedResult(name string, n int) *meta.Func {
	f := d1.NewFactory(10)
	bins := make([]pack.Bin, n)
	for i := range bins {
		bins[i] = f.Open()
	}
	return meta.NewFunc(name, func([]pack.Item) (pack.Result, error) {
		return pack.Result{Bins: bins}, nil
	})
}

func TestBestOf_PicksFewestBinsAndReportsWinner(t *testing.T) {
	p := meta.BestOf(
		fixedResult("bad", 3),
		fixedResult("good", 2),
		fixedResult("worst", 5),
	)
	r, err := p.PackAll(nil)
	if err != nil {
		t.Fatalf("PackAll: %v", err)
	}
	if r.BinsUsed() != 2 {
		t.Errorf("BinsUsed = %d, want 2 (fewest)", r.BinsUsed())
	}
	if p.Winner() != "good" {
		t.Errorf("Winner = %q, want \"good\"", p.Winner())
	}
}
