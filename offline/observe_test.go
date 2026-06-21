package offline_test

import (
	"testing"

	"github.com/W-Floyd/go-pack-bins/d1"
	"github.com/W-Floyd/go-pack-bins/offline"
	"github.com/W-Floyd/go-pack-bins/pack"
)

// The decreasing offline wrappers delegate to an online packer, so they satisfy
// pack.Observable by forwarding: the observer must fire once per placement as it
// is committed during PackAll, matching the final result.
func TestObserve_OfflineWrapperForwards(t *testing.T) {
	w := offline.FirstFitDecreasing(d1.NewFactory(10))

	obs, ok := interface{}(w).(pack.Observable)
	if !ok {
		t.Fatal("offline.Wrapper does not implement pack.Observable")
	}
	var n int
	obs.Observe(func(pack.Placement) { n++ })

	items := []pack.Item{d1.NewItem("a", 6), d1.NewItem("b", 6), d1.NewItem("c", 4)}
	r, err := w.PackAll(items)
	if err != nil {
		t.Fatalf("packall: %v", err)
	}
	if n != len(r.Placements) {
		t.Fatalf("observed %d placements during PackAll, result has %d", n, len(r.Placements))
	}
}

// MFFD runs a single First-Fit pass over class-ordered items, so it streams too:
// the observer must fire once per placement, in the same order and identity as
// the final result (the stream is byte-identical to a plain PackAll).
func TestObserve_MFFDStreamsInOrder(t *testing.T) {
	items := []pack.Item{
		d1.NewItem("a", 6), d1.NewItem("b", 4), d1.NewItem("c", 5),
		d1.NewItem("d", 2), d1.NewItem("e", 3), d1.NewItem("f", 1),
	}

	mp := offline.ModifiedFirstFitDecreasing(10, d1.NewFactory(10))
	var _ pack.Observable = mp // compile-time: MFFD is observable

	var seen []pack.Placement
	mp.Observe(func(p pack.Placement) { seen = append(seen, p) })
	r, err := mp.PackAll(items)
	if err != nil {
		t.Fatalf("mffd packall: %v", err)
	}

	if len(seen) != len(r.Placements) {
		t.Fatalf("observed %d, result has %d", len(seen), len(r.Placements))
	}
	for i := range r.Placements {
		if seen[i] != r.Placements[i] {
			t.Errorf("placement %d: streamed %s, result %s", i, seen[i].ItemID(), r.Placements[i].ItemID())
		}
	}
}
