package pack_test

import (
	"testing"

	"github.com/W-Floyd/go-pack-bins/d1"
	"github.com/W-Floyd/go-pack-bins/online"
	"github.com/W-Floyd/go-pack-bins/pack"
)

// Two items of incompatible categories must not share a bin even when capacity
// allows it; compatible/uncategorised items pack together freely.
func TestIncompatibleConstraint(t *testing.T) {
	// Bin capacity 10; each item size 1, so all would fit in one bin absent the rule.
	cap := 10.0
	factory := pack.NewConstrainedFactory(d1.NewFactory(cap),
		pack.Incompatible("hazmat", [2]float64{1, 2})) // category 1 vs 2 forbidden

	mk := func(id string, cat float64) pack.Item {
		it := d1.NewItem(id, 1)
		if cat != 0 {
			it.WithScalar("hazmat", cat)
		}
		return it
	}
	items := []pack.Item{
		mk("a", 1), // cat 1
		mk("b", 2), // cat 2 — incompatible with a
		mk("c", 1), // cat 1 — fine with a
		mk("d", 0), // uncategorised — fine anywhere
	}

	p := online.FirstFit(factory)
	for _, it := range items {
		if _, err := p.Pack(it); err != nil {
			t.Fatalf("pack %s: %v", it.ID(), err)
		}
	}
	r := p.Result()
	if len(r.Unplaced) != 0 {
		t.Fatalf("unexpected unplaced: %v", r.Unplaced)
	}

	// Map each item to its bin and assert a (cat1) and b (cat2) differ.
	binOf := map[string]string{}
	for _, pl := range r.Placements {
		binOf[pl.ItemID()] = pl.BinID()
	}
	if binOf["a"] == binOf["b"] {
		t.Fatalf("incompatible items a and b ended up in the same bin %q", binOf["a"])
	}
	if binOf["a"] != binOf["c"] {
		t.Errorf("compatible same-category items a and c should share a bin (first-fit); got %q vs %q", binOf["a"], binOf["c"])
	}
}
