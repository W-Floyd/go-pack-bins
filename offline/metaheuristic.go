package offline

import (
	"context"
	"math/rand"
	"sort"
	"sync/atomic"

	"github.com/W-Floyd/go-pack-bins/online"
	"github.com/W-Floyd/go-pack-bins/pack"
)

// This file adds metaheuristic *improvement* search on top of the constructive
// packers, the dominant approach in the GBPP literature (heuristics &
// metaheuristics appear in 43 of 60 studies in Mantzou & Dimitriadis 2025).
// Both operate on the item *ordering* and evaluate it with a First-Fit pass, so
// they are dimension-agnostic and reuse the existing packers unchanged. Both
// honour ctx as a deadline (the cancellation work added earlier).

// resultScore ranks a packing: fewer unplaced wins, then fewer bins, then a
// tighter pack (higher summed bin utilization).
type resultScore struct {
	unplaced int
	bins     int
	fill     float64
}

func scoreResult(r pack.Result) resultScore {
	fill := 0.0
	for _, b := range r.Bins {
		fill += b.Utilization()
	}
	return resultScore{unplaced: len(r.Unplaced), bins: r.BinsUsed(), fill: fill}
}

func (a resultScore) better(b resultScore) bool {
	if a.unplaced != b.unplaced {
		return a.unplaced < b.unplaced
	}
	if a.bins != b.bins {
		return a.bins < b.bins
	}
	return a.fill > b.fill
}

// packOrdering packs items in the given order with First-Fit through factory and
// returns the result. A fresh packer is used each call, so callers may evaluate
// many orderings against the same factory.
func packOrdering(factory pack.BinFactory, order []pack.Item) pack.Result {
	p := online.FirstFit(factory)
	for _, it := range order {
		p.Pack(it) // failures recorded as unplaced in the result
	}
	return p.Result()
}

// SearchOptions configures the metaheuristic searches.
type SearchOptions struct {
	// Seed makes a run reproducible (default 1).
	Seed int64
	// MaxIters caps total evaluated orderings (default 2000). ctx can stop sooner.
	MaxIters int
	// Restarts is the number of GRASP multistarts (default 16).
	Restarts int
	// Progress, if set, receives coarse progress: GRASP reports restarts completed
	// out of the total; RuinRecreate reports iterations completed out of MaxIters.
	Progress pack.ProgressObserver
}

func (o SearchOptions) seed() int64 {
	if o.Seed == 0 {
		return 1
	}
	return o.Seed
}

func (o SearchOptions) maxIters() int {
	if o.MaxIters <= 0 {
		return 2000
	}
	return o.MaxIters
}

func (o SearchOptions) restarts() int {
	if o.Restarts <= 0 {
		return 16
	}
	return o.Restarts
}

// RuinRecreate improves a packing by repeated ruin-and-recreate: from the
// decreasing-volume ordering it removes a random subset of items and reinserts
// them at the front, repacks, and keeps the perturbed ordering whenever it packs
// at least as well — the goal-driven ruin-and-recreate scheme of Gardeyn &
// Wauters (2022) for the 2-D variable-sized BPP, generalised to any dimension.
// It returns the best packing found and honours ctx as a deadline.
func RuinRecreate(ctx context.Context, items []pack.Item, factory pack.BinFactory, opts SearchOptions) pack.Result {
	if len(items) == 0 {
		return pack.Result{}
	}
	rng := rand.New(rand.NewSource(opts.seed()))

	bestOrder := append([]pack.Item(nil), items...)
	sort.SliceStable(bestOrder, func(i, j int) bool { return bestOrder[i].Volume() > bestOrder[j].Volume() })
	best := packOrdering(factory, bestOrder)
	bestScore := scoreResult(best)

	maxIters := opts.maxIters()
	step := maxIters / 100 // throttle to ~100 progress updates over the run
	if step < 1 {
		step = 1
	}
	for iter := 0; iter < maxIters; iter++ {
		if ctx.Err() != nil {
			break
		}
		cand := ruin(bestOrder, rng)
		r := packOrdering(factory, cand)
		if s := scoreResult(r); s.better(bestScore) {
			best, bestScore, bestOrder = r, s, cand
		}
		if opts.Progress != nil && (iter+1)%step == 0 {
			opts.Progress(iter+1, maxIters)
		}
	}
	return best
}

// ruin removes a random 10–35% of items from order and reinserts them (shuffled)
// at the front, so the recreate pass packs them into a fresh arrangement.
func ruin(order []pack.Item, rng *rand.Rand) []pack.Item {
	n := len(order)
	k := 1 + rng.Intn(1+n/3) // up to ~a third
	if k > n {
		k = n
	}
	// Choose k distinct positions to remove.
	remove := make(map[int]bool, k)
	for len(remove) < k {
		remove[rng.Intn(n)] = true
	}
	removed := make([]pack.Item, 0, k)
	kept := make([]pack.Item, 0, n-k)
	for i, it := range order {
		if remove[i] {
			removed = append(removed, it)
		} else {
			kept = append(kept, it)
		}
	}
	rng.Shuffle(len(removed), func(i, j int) { removed[i], removed[j] = removed[j], removed[i] })
	return append(removed, kept...)
}

// GRASP runs a Greedy Randomized Adaptive Search Procedure: each restart builds
// a randomized-greedy ordering (a restricted candidate list over decreasing
// volume), packs it, then improves it with a short ruin-and-recreate local
// search; the best packing over all restarts is returned. GRASP is a recurring
// metaheuristic in the container-loading / pallet literature (e.g. Correcher et
// al. 2017; Calzavara et al. 2021). Honours ctx as a deadline.
func GRASP(ctx context.Context, items []pack.Item, factory pack.BinFactory, opts SearchOptions) pack.Result {
	if len(items) == 0 {
		return pack.Result{}
	}
	restarts := opts.restarts()
	localBudget := opts.maxIters() / restarts
	if localBudget < 1 {
		localBudget = 1
	}

	// Restarts are independent, so run them concurrently. Each draws from its own
	// RNG seeded deterministically (base seed + restart index), keeping the run
	// reproducible for a given Seed regardless of scheduling.
	type restartResult struct {
		res   pack.Result
		score resultScore
		have  bool
	}
	outs := make([]restartResult, restarts)
	var completed int64
	parallelFor(restarts, func(r int) {
		if ctx.Err() != nil {
			return // leaves have=false; skipped in the reduction
		}
		rng := rand.New(rand.NewSource(opts.seed() + int64(r)))
		order := randomizedGreedyOrder(items, rng)
		res := packOrdering(factory, order)
		// Short local search around this start.
		curScore := scoreResult(res)
		curOrder := order
		for i := 0; i < localBudget; i++ {
			if ctx.Err() != nil {
				break
			}
			cand := ruin(curOrder, rng)
			cr := packOrdering(factory, cand)
			if s := scoreResult(cr); s.better(curScore) {
				res, curScore, curOrder = cr, s, cand
			}
		}
		outs[r] = restartResult{res: res, score: curScore, have: true}
		if opts.Progress != nil {
			opts.Progress(int(atomic.AddInt64(&completed, 1)), restarts)
		}
	})

	// Reduce in restart order so the winner is deterministic.
	var best pack.Result
	var bestScore resultScore
	have := false
	for _, o := range outs {
		if !o.have {
			continue
		}
		if !have || o.score.better(bestScore) {
			best, bestScore, have = o.res, o.score, true
		}
	}
	return best
}

// randomizedGreedyOrder sorts items by decreasing volume, then builds an ordering
// by repeatedly drawing from a restricted candidate list (the largest few
// remaining items) — the adaptive-greedy construction at the heart of GRASP.
func randomizedGreedyOrder(items []pack.Item, rng *rand.Rand) []pack.Item {
	pool := append([]pack.Item(nil), items...)
	sort.SliceStable(pool, func(i, j int) bool { return pool[i].Volume() > pool[j].Volume() })
	const rcl = 3 // restricted candidate list size
	out := make([]pack.Item, 0, len(pool))
	for len(pool) > 0 {
		n := rcl
		if n > len(pool) {
			n = len(pool)
		}
		pick := rng.Intn(n) // choose among the n largest remaining
		out = append(out, pool[pick])
		pool = append(pool[:pick], pool[pick+1:]...)
	}
	return out
}
