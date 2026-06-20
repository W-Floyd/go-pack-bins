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
