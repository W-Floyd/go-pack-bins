package offline_test

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/W-Floyd/go-pack-bins/d3"
	"github.com/W-Floyd/go-pack-bins/offline"
	"github.com/W-Floyd/go-pack-bins/pack"
)

// mixed3D builds n pseudo-random boxes for a bw×bd×bh bin.
func mixed3D(n int, bw, bd, bh float64, seed int64) []pack.Item {
	rng := rand.New(rand.NewSource(seed))
	items := make([]pack.Item, n)
	for i := 0; i < n; i++ {
		w := 2 + rng.Float64()*(bw/3)
		d := 2 + rng.Float64()*(bd/3)
		h := 2 + rng.Float64()*(bh/3)
		items[i] = d3.NewItem(fmt.Sprintf("it%d", i), w, d, h, true)
	}
	return items
}

// assertAccountsForAll checks the incremental engine never loses or duplicates an
// item: every input item must appear exactly once across bins ∪ unplaced.
func assertAccountsForAll(t *testing.T, items []pack.Item, r pack.Result) {
	t.Helper()
	seen := make(map[string]int, len(items))
	for _, b := range r.Bins {
		for _, it := range b.Items() {
			seen[it.ID()]++
		}
	}
	for _, id := range r.Unplaced {
		seen[id]++
	}
	for _, it := range items {
		if seen[it.ID()] != 1 {
			t.Fatalf("item %s accounted %d times, want 1", it.ID(), seen[it.ID()])
		}
	}
	if len(seen) != len(items) {
		t.Fatalf("result references %d distinct items, want %d", len(seen), len(items))
	}
	// Placements must match an item in a bin and not exceed the placed count.
	placed := 0
	for _, b := range r.Bins {
		placed += len(b.Items())
	}
	if len(r.Placements) != placed {
		t.Fatalf("got %d placements for %d placed items", len(r.Placements), placed)
	}
}

// The incremental ruin-and-recreate engine must produce a valid, complete packing
// — and never use more bins than the FFD-through-EMS baseline it starts from.
func TestIncrementalRuinRecreateValid(t *testing.T) {
	const bw, bd, bh = 20.0, 20.0, 20.0
	items := mixed3D(150, bw, bd, bh, 42)
	factory := func() pack.BinFactory {
		return d3.NewFactory(bw, bd, bh, d3.NewEMSStrategyContact(d3.ContactSpec{}))
	}
	baseline, _ := offline.FirstFitDecreasing(factory()).PackAll(append([]pack.Item(nil), items...))

	cases := map[string]func() pack.Result{
		"rr": func() pack.Result {
			return offline.RuinRecreate(context.Background(), items, factory(), offline.SearchOptions{MaxIters: 300})
		},
		"arr": func() pack.Result {
			return offline.AdaptiveRuinRecreate(context.Background(), items, factory(), offline.SearchOptions{MaxIters: 300})
		},
		"grasp": func() pack.Result {
			return offline.GRASP(context.Background(), items, factory(), offline.SearchOptions{MaxIters: 300, Restarts: 4})
		},
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			r := run()
			assertAccountsForAll(t, items, r)
			if r.BinsUsed() > baseline.BinsUsed() {
				t.Fatalf("%s used %d bins, worse than FFD baseline %d", name, r.BinsUsed(), baseline.BinsUsed())
			}
		})
	}
}

// With a DecodeFactory the search runs on a cheap surrogate decoder and the final
// answer is re-decoded through the strong factory — it must still place everything.
func TestRuinRecreateDecodeFactory(t *testing.T) {
	const bw, bd, bh = 20.0, 20.0, 20.0
	items := mixed3D(150, bw, bd, bh, 7)
	strong := d3.NewFactory(bw, bd, bh, d3.NewEMSStrategyContact(d3.ContactSpec{}))
	cheap := d3.NewFactory(bw, bd, bh, d3.NewExtremePointStrategyContact(d3.ContactSpec{}))

	r := offline.RuinRecreate(context.Background(), items, strong,
		offline.SearchOptions{MaxIters: 300, DecodeFactory: cheap})
	assertAccountsForAll(t, items, r)
}

// A wall-clock Deadline must stop the search promptly (not run all MaxIters) and
// still return a valid, complete packing.
func TestRuinRecreateDeadline(t *testing.T) {
	const bw, bd, bh = 20.0, 20.0, 20.0
	items := mixed3D(300, bw, bd, bh, 11)
	factory := d3.NewFactory(bw, bd, bh, d3.NewEMSStrategyContact(d3.ContactSpec{}))

	t0 := time.Now()
	r := offline.RuinRecreate(context.Background(), items, factory,
		offline.SearchOptions{MaxIters: 1 << 30, Deadline: time.Now().Add(300 * time.Millisecond)})
	elapsed := time.Since(t0)
	assertAccountsForAll(t, items, r)
	if elapsed > 3*time.Second {
		t.Fatalf("deadline ignored: ran %v for a 300ms budget", elapsed)
	}
}

// An already-expired deadline must bound even the initial construction: the search
// returns promptly and still accounts for every item (unbuilt ones as unplaced),
// rather than running the full O(n·placement) build before noticing the budget.
func TestRuinRecreateDeadlineBoundsInitialBuild(t *testing.T) {
	const bw, bd, bh = 20.0, 20.0, 20.0
	items := mixed3D(500, bw, bd, bh, 5)
	factory := d3.NewFactory(bw, bd, bh, d3.NewEMSStrategyContact(d3.ContactSpec{}))

	t0 := time.Now()
	r := offline.RuinRecreate(context.Background(), items, factory,
		offline.SearchOptions{MaxIters: 1 << 30, Deadline: time.Now().Add(-time.Second)})
	elapsed := time.Since(t0)
	assertAccountsForAll(t, items, r)
	if elapsed > 500*time.Millisecond {
		t.Fatalf("initial build not bounded by deadline: ran %v", elapsed)
	}
}

// The Snapshot observer must fire at least once (the initial packing) and only
// ever hand back valid, complete packings.
func TestRuinRecreateSnapshot(t *testing.T) {
	const bw, bd, bh = 20.0, 20.0, 20.0
	items := mixed3D(120, bw, bd, bh, 3)
	factory := d3.NewFactory(bw, bd, bh, d3.NewEMSStrategyContact(d3.ContactSpec{}))

	var snaps int
	opts := offline.SearchOptions{MaxIters: 300, Snapshot: func(r pack.Result) {
		snaps++
		assertAccountsForAll(t, items, r)
	}}
	offline.RuinRecreate(context.Background(), items, factory, opts)
	if snaps == 0 {
		t.Fatal("Snapshot never fired")
	}
}
