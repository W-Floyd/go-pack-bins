package offline_test

import (
	"context"
	"testing"

	"github.com/W-Floyd/go-pack-bins/d1"
	"github.com/W-Floyd/go-pack-bins/offline"
	"github.com/W-Floyd/go-pack-bins/pack"
)

func bfItems(sizes ...float64) []pack.Item {
	out := make([]pack.Item, len(sizes))
	for i, s := range sizes {
		out[i] = d1.NewItem(string(rune('a'+i)), s)
	}
	return out
}

// BruteForce must place everything feasible and never use more bins than FFD —
// it searches every ordering, including the one FFD would choose.
func TestBruteForceNoWorseThanFFD(t *testing.T) {
	const cap = 10
	items := bfItems(7, 2, 6, 3, 4, 5) // sum 27 → 3 bins optimal
	br, err := offline.BruteForce(context.Background(), items, d1.NewFactory(cap), offline.BruteForceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ffd, _ := offline.FirstFitDecreasing(d1.NewFactory(cap)).PackAll(items)
	if len(br.Unplaced) != 0 {
		t.Fatalf("brute-force left items unplaced: %v", br.Unplaced)
	}
	if br.BinsUsed() > ffd.BinsUsed() {
		t.Fatalf("brute-force used more bins (%d) than FFD (%d)", br.BinsUsed(), ffd.BinsUsed())
	}
	if br.BinsUsed() != 3 {
		t.Fatalf("expected optimal 3 bins, got %d", br.BinsUsed())
	}
}

// Duplicate-key pruning must not change correctness: six identical items into
// capacity-10 bins still needs 3 bins, with everything placed.
func TestBruteForcePruningCorrect(t *testing.T) {
	items := bfItems(5, 5, 5, 5, 5, 5)
	r, err := offline.BruteForce(context.Background(), items, d1.NewFactory(10),
		offline.BruteForceOptions{Key: func(it pack.Item) string { return "same" }})
	if err != nil {
		t.Fatal(err)
	}
	if r.BinsUsed() != 3 || len(r.Unplaced) != 0 {
		t.Fatalf("expected 3 bins, all placed; got %d bins, unplaced %v", r.BinsUsed(), r.Unplaced)
	}
}

// Above MaxItems it falls back to FFD rather than attempting n! orderings.
func TestBruteForceFallsBackOverCap(t *testing.T) {
	items := bfItems(1, 1, 1, 1, 1) // 5 items
	r, err := offline.BruteForce(context.Background(), items, d1.NewFactory(3),
		offline.BruteForceOptions{MaxItems: 3})
	if err != nil {
		t.Fatal(err)
	}
	// cap 3, five unit items → 2 bins (3+2). Just assert it ran and placed all.
	if len(r.Unplaced) != 0 {
		t.Fatalf("unexpected unplaced: %v", r.Unplaced)
	}
}

// A cancelled context aborts the search.
func TestBruteForceCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := offline.BruteForce(ctx, bfItems(1, 2, 3, 4), d1.NewFactory(10), offline.BruteForceOptions{})
	if err == nil {
		t.Fatal("expected cancellation error")
	}
}
