package offline_test

import (
	"context"
	"sync"
	"testing"

	"github.com/W-Floyd/go-pack-bins/d1"
	"github.com/W-Floyd/go-pack-bins/offline"
	"github.com/W-Floyd/go-pack-bins/pack"
)

// Each long-running solver must report progress that advances monotonically and
// never exceeds the reported total. The callback may fire from worker goroutines
// (GRASP/BruteForce), so guard the recorder; run with -race.
func TestProgressReported(t *testing.T) {
	const cap = 10
	items := searchItems(7, 2, 6, 3, 4, 5, 8, 1)

	check := func(name string, run func(pack.ProgressObserver)) {
		var mu sync.Mutex
		var maxDone, total, calls int
		run(func(done, tot int) {
			mu.Lock()
			defer mu.Unlock()
			calls++
			if done > maxDone {
				maxDone = done
			}
			total = tot
			if done > tot {
				t.Errorf("%s: done %d > total %d", name, done, tot)
			}
		})
		if calls == 0 {
			t.Errorf("%s: progress never reported", name)
		}
		if maxDone != total {
			t.Errorf("%s: final progress %d/%d, want done==total", name, maxDone, total)
		}
	}

	check("GRASP", func(p pack.ProgressObserver) {
		offline.GRASP(context.Background(), items, d1.NewFactory(cap), offline.SearchOptions{MaxIters: 200, Progress: p})
	})
	check("RuinRecreate", func(p pack.ProgressObserver) {
		offline.RuinRecreate(context.Background(), items, d1.NewFactory(cap), offline.SearchOptions{MaxIters: 200, Progress: p})
	})
	check("BeamSearch", func(p pack.ProgressObserver) {
		offline.BeamSearch(context.Background(), items, d1.NewFactory(cap), offline.BeamOptions{Progress: p})
	})
	check("BruteForce", func(p pack.ProgressObserver) {
		offline.BruteForce(context.Background(), items, d1.NewFactory(cap), offline.BruteForceOptions{Progress: p})
	})
}
