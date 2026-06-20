package offline_test

import (
	"testing"

	"github.com/W-Floyd/go-pack-bins/d1"
	"github.com/W-Floyd/go-pack-bins/offline"
	"github.com/W-Floyd/go-pack-bins/pack"
)

func TestDecreasingVolume_Sorts(t *testing.T) {
	items := []pack.Item{d1.NewItem("s", 2), d1.NewItem("l", 9), d1.NewItem("m", 5)}
	offline.DecreasingVolume(items)
	got := []string{items[0].ID(), items[1].ID(), items[2].ID()}
	want := []string{"l", "m", "s"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestBalancedFit_NoEmptyBinsReported(t *testing.T) {
	// FFD fits these in 2 bins; BalancedFit pre-opens 2 and distributes. Every
	// reported bin must be non-empty (pruned), and every item placed.
	factory := pack.NewConstrainedFactory(d1.NewFactory(10))
	items := []pack.Item{
		d1.NewItem("a", 6), d1.NewItem("b", 6),
		d1.NewItem("c", 3), d1.NewItem("d", 3),
	}
	r, err := offline.NewBalancedFit(factory, pack.BalanceCount()).PackAll(items)
	if err != nil {
		t.Fatalf("PackAll: %v", err)
	}
	placed := 0
	for _, p := range r.Placements {
		if p != nil {
			placed++
		}
	}
	if placed != len(items) {
		t.Errorf("placed %d items, want %d", placed, len(items))
	}
	// No reported bin should be empty.
	counts := map[string]int{}
	for _, p := range r.Placements {
		if p != nil {
			counts[p.BinID()]++
		}
	}
	if len(counts) != r.BinsUsed() {
		t.Errorf("BinsUsed=%d but %d bins actually hold items (empty bins not pruned)", r.BinsUsed(), len(counts))
	}
}
