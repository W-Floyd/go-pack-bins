package offline_test

import (
	"context"
	"testing"

	"github.com/W-Floyd/go-pack-bins/d1"
	"github.com/W-Floyd/go-pack-bins/offline"
	"github.com/W-Floyd/go-pack-bins/pack"
)

// The now-parallel searches must give the same result every run (deterministic
// despite concurrency): GRASP via per-restart seeds, BruteForce/Beam via fixed
// enumeration order + stable reduction. Run with -race to also catch data races.
func TestParallelSearchesDeterministic(t *testing.T) {
	const cap = 10
	items := searchItems(7, 2, 6, 3, 4, 5, 8, 1) // 8 items → within brute's default cap

	qty := func(r pack.Result) [2]int { return [2]int{r.BinsUsed(), len(r.Unplaced)} }

	cases := map[string]func() pack.Result{
		"GRASP": func() pack.Result {
			return offline.GRASP(context.Background(), items, d1.NewFactory(cap), offline.SearchOptions{Seed: 7, MaxIters: 400})
		},
		"BruteForce": func() pack.Result {
			r, _ := offline.BruteForce(context.Background(), items, d1.NewFactory(cap), offline.BruteForceOptions{})
			return r
		},
		"BeamSearch": func() pack.Result {
			return offline.BeamSearch(context.Background(), items, d1.NewFactory(cap), offline.BeamOptions{})
		},
	}
	for name, run := range cases {
		want := qty(run())
		for i := 0; i < 25; i++ {
			if got := qty(run()); got != want {
				t.Fatalf("%s nondeterministic: run %d gave %v, want %v", name, i, got, want)
			}
		}
	}
}
