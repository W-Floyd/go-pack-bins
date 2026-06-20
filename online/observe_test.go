package online_test

import (
	"testing"

	"github.com/W-Floyd/go-pack-bins/d1"
	"github.com/W-Floyd/go-pack-bins/online"
	"github.com/W-Floyd/go-pack-bins/pack"
)

// The observer must fire once per committed placement, in commit order, and the
// sequence must equal the final Result.Placements (no misses, no extras).
func TestObserve_FiresPerCommitInOrder(t *testing.T) {
	p := online.FirstFit(d1.NewFactory(10))

	var seen []pack.Placement
	p.Observe(func(pl pack.Placement) { seen = append(seen, pl) })

	items := []pack.Item{
		d1.NewItem("a", 6), d1.NewItem("b", 6), // each opens its own bin
		d1.NewItem("c", 4), // fits in bin0 alongside a
		d1.NewItem("d", 9), // opens bin2
	}
	for _, it := range items {
		if _, err := p.Pack(it); err != nil {
			t.Fatalf("pack %s: %v", it.ID(), err)
		}
	}

	got := p.Result().Placements
	if len(seen) != len(got) {
		t.Fatalf("observed %d placements, result has %d", len(seen), len(got))
	}
	for i := range got {
		if seen[i] != got[i] {
			t.Errorf("observation %d = %v, result = %v (order/identity mismatch)",
				i, seen[i].ItemID(), got[i].ItemID())
		}
	}
	// nil detaches.
	p.Observe(nil)
	before := len(seen)
	if _, err := p.Pack(d1.NewItem("e", 2)); err != nil {
		t.Fatalf("pack e: %v", err)
	}
	if len(seen) != before {
		t.Errorf("observer fired after being detached with nil")
	}
}
