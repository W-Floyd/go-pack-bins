package offline

import (
	"context"
	"math/rand"
	"sort"
	"sync/atomic"
	"time"

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

// withinRecord reports whether score a is acceptable as a lateral/near move under
// record-to-record travel relative to ref: never worse on (unplaced, bins), and
// fill no more than dev below ref's fill.
func (a resultScore) withinRecord(ref resultScore, dev float64) bool {
	if a.unplaced > ref.unplaced || a.bins > ref.bins {
		return false
	}
	return a.fill >= ref.fill-dev
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
	// DecodeFactory, if set, is the cheap surrogate decoder the search evaluates
	// candidate orderings through, instead of the (potentially expensive) primary
	// factory. The single best ordering found is then re-decoded once through the
	// primary factory for the returned placement — so the search runs many more
	// iterations on a fast packer while the final answer still comes from the
	// strong one. Nil means decode every candidate through the primary factory.
	DecodeFactory pack.BinFactory
	// Deadline, if non-zero, is a wall-clock cutoff: the search stops once it passes,
	// returning the best packing found so far. It is checked directly in the loop
	// (not via ctx) so it works even where ctx timer goroutines aren't scheduled
	// during a tight solve — notably js/wasm. ctx still stops the search sooner.
	Deadline time.Time
	// Snapshot, if set, receives the current best packing each time it improves, so a
	// caller can show the search converging live. Called synchronously from the solve
	// goroutine (for GRASP, from a restart worker), so it must be cheap and safe to
	// call concurrently; the packing it receives is not mutated afterwards.
	Snapshot func(pack.Result)
}

// expired reports whether the wall-clock deadline (if any) has passed.
func (o SearchOptions) expired() bool {
	return !o.Deadline.IsZero() && time.Now().After(o.Deadline)
}

// snapshot delivers the current best to the Snapshot observer, if one is set.
func (o SearchOptions) snapshot(r pack.Result) {
	if o.Snapshot != nil {
		o.Snapshot(r)
	}
}

// emitProgress reports search progress. Under a wall-clock Deadline it reports
// elapsed/total time (so the bar tracks the budget the user actually set, not the
// huge iteration cap); otherwise it reports iterations completed out of MaxIters.
func (o SearchOptions) emitProgress(iter, maxIters int, start time.Time) {
	if o.Progress == nil {
		return
	}
	if !o.Deadline.IsZero() {
		total := o.Deadline.Sub(start)
		done := time.Since(start)
		if done > total {
			done = total
		}
		o.Progress(int(done.Milliseconds()), int(total.Milliseconds()))
		return
	}
	o.Progress(iter, maxIters)
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

// ruinCap bounds the ruin magnitude. Incremental ruin-and-recreate rebuilds only
// the bins a ruin touches, so its per-iteration cost grows with the ruin size; a
// small ruin keeps each step cheap (a handful of bins) and lets the search run far
// more iterations within a budget — the regime R&R is designed for. We cap the
// removed-item count at a small constant, not a fraction of n, so the step cost
// stays bounded as the instance grows.
const ruinCap = 12

// ruinSize draws a ruin magnitude in [1, min(n, ruinCap)].
func ruinSize(n int, rng *rand.Rand) int {
	hi := n
	if hi > ruinCap {
		hi = ruinCap
	}
	if hi < 1 {
		hi = 1
	}
	return 1 + rng.Intn(hi)
}

// RuinRecreate improves a packing by repeated ruin-and-recreate: starting from the
// decreasing-volume packing it removes a random subset of items, repacks only the
// bins those items touched, re-inserts the removed items, and keeps the result
// whenever it packs at least as well — the goal-driven ruin-and-recreate scheme of
// Gardeyn & Wauters (2022) for the 2-D variable-sized BPP, generalised to any
// dimension. Each step costs work proportional to the disturbed region rather than
// the whole instance (see incremental.go). It returns the best packing found and
// honours ctx as a deadline.
func RuinRecreate(ctx context.Context, items []pack.Item, factory pack.BinFactory, opts SearchOptions) pack.Result {
	if len(items) == 0 {
		return pack.Result{}
	}
	rng := rand.New(rand.NewSource(opts.seed()))

	decode := factory
	if opts.DecodeFactory != nil {
		decode = opts.DecodeFactory
	}

	stop := func() bool { return opts.expired() } // bound the initial build by the wall-clock budget; ctx-cancel keeps the FFD baseline (see TestSearchCancel)
	order := append([]pack.Item(nil), items...)
	sort.SliceStable(order, func(i, j int) bool { return order[i].Volume() > order[j].Volume() })
	best := buildPartialLimited(decode, order, stop)
	bestScore := best.score()
	n := len(items)

	opts.snapshot(best.result()) // show the starting packing immediately

	maxIters := opts.maxIters()
	step := maxIters / 100 // throttle to ~100 progress updates over the run
	if step < 1 {
		step = 1
	}
	start := time.Now()
	for iter := 0; iter < maxIters; iter++ {
		if ctx.Err() != nil || opts.expired() {
			break
		}
		k := ruinSize(n, rng)
		cand := ruinRecreateStep(decode, best, k, rng)
		if s := cand.score(); s.better(bestScore) {
			best, bestScore = cand, s
			opts.snapshot(best.result())
		}
		if (iter+1)%step == 0 {
			opts.emitProgress(iter+1, maxIters, start)
		}
	}
	if opts.DecodeFactory != nil {
		return finalDecode(factory, best, items)
	}
	return best.result()
}

// AdaptiveRuinRecreate is a stronger ruin-and-recreate that, unlike RuinRecreate's
// greedy hill-climb from the incumbent, walks a *current* ordering and adapts its
// search to progress:
//
//   - Acceptance is record-to-record travel: a perturbed ordering becomes the new
//     current solution if it is no worse on (unplaced, bins) and its fill is within
//     a small deviation of the best fill. Accepting these lateral/near moves lets
//     the walk traverse plateaus that strict hill-climbing gets stuck on.
//   - The ruin magnitude is adaptive: it starts small (intensify — refine the
//     current arrangement) and grows after a run of non-improving iterations
//     (diversify — escape the basin), snapping back to small on any new best and
//     re-seeding the walk from the incumbent.
//
// The decode strategy is whatever `factory` builds (pass the EMS/Fit decoder for
// 3-D), so this composes with the strong constructive packers. It returns the
// best packing found and honours ctx as a deadline.
func AdaptiveRuinRecreate(ctx context.Context, items []pack.Item, factory pack.BinFactory, opts SearchOptions) pack.Result {
	n := len(items)
	if n == 0 {
		return pack.Result{}
	}
	rng := rand.New(rand.NewSource(opts.seed()))

	decode := factory
	if opts.DecodeFactory != nil {
		decode = opts.DecodeFactory
	}

	stop := func() bool { return opts.expired() } // bound the initial build by the wall-clock budget; ctx-cancel keeps the FFD baseline (see TestSearchCancel)
	order := append([]pack.Item(nil), items...)
	sort.SliceStable(order, func(i, j int) bool { return order[i].Volume() > order[j].Volume() })
	best := buildPartialLimited(decode, order, stop)
	bestScore := best.score()

	opts.snapshot(best.result()) // show the starting packing immediately

	cur := best
	curScore := bestScore

	// Ruin magnitude in items: the adaptive walk grows k from 1 up to a small cap
	// when stalled (diversify) and snaps back to 1 on a new best (intensify). The
	// cap stays a small constant — incremental recreate rebuilds only the touched
	// bins, so a bounded ruin keeps each step cheap regardless of instance size.
	minK := 1
	maxK := ruinCap
	if maxK > n {
		maxK = n
	}
	k := minK
	// Deviation band for record-to-record acceptance: a fraction of the best
	// fill (fill is summed bin utilization, so it scales with bin count).
	dev := 0.03 * (bestScore.fill + 1)
	stall, stallLimit := 0, 8

	maxIters := opts.maxIters()
	step := maxIters / 100
	if step < 1 {
		step = 1
	}
	start := time.Now()
	for iter := 0; iter < maxIters; iter++ {
		if ctx.Err() != nil || opts.expired() {
			break
		}
		cand := ruinRecreateStep(decode, cur, k, rng)
		s := cand.score()

		switch {
		case s.better(bestScore):
			best, bestScore = cand, s
			cur, curScore = cand, s
			k, stall = minK, 0 // new best → intensify, reset the walk's patience
			opts.snapshot(best.result())
		case s.withinRecord(bestScore, dev) && !curScore.better(s):
			cur, curScore = cand, s // accept lateral/near move
			stall++
		default:
			stall++
		}
		if stall >= stallLimit { // stuck → widen the ruin and restart from incumbent
			k++
			if k > maxK {
				k = minK
			}
			cur, curScore = best, bestScore
			stall = 0
		}
		if (iter+1)%step == 0 {
			opts.emitProgress(iter+1, maxIters, start)
		}
	}
	if opts.DecodeFactory != nil {
		return finalDecode(factory, best, items)
	}
	return best.result()
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
	n := len(items)

	decode := factory
	if opts.DecodeFactory != nil {
		decode = opts.DecodeFactory
	}

	// Restarts are independent, so run them concurrently. Each draws from its own
	// RNG seeded deterministically (base seed + restart index), keeping the run
	// reproducible for a given Seed regardless of scheduling.
	type restartResult struct {
		sol   partial
		score resultScore
		have  bool
	}
	outs := make([]restartResult, restarts)
	var completed int64
	parallelFor(restarts, func(r int) {
		if ctx.Err() != nil || opts.expired() {
			return // leaves have=false; skipped in the reduction
		}
		rng := rand.New(rand.NewSource(opts.seed() + int64(r)))
		order := randomizedGreedyOrder(items, rng)
		// Short incremental ruin-and-recreate local search around this start.
		cur := buildPartialLimited(decode, order, func() bool { return opts.expired() })
		curScore := cur.score()
		for i := 0; i < localBudget; i++ {
			if ctx.Err() != nil || opts.expired() {
				break
			}
			k := ruinSize(n, rng)
			cand := ruinRecreateStep(decode, cur, k, rng)
			if s := cand.score(); s.better(curScore) {
				cur, curScore = cand, s
			}
		}
		outs[r] = restartResult{sol: cur, score: curScore, have: true}
		if opts.Progress != nil {
			opts.Progress(int(atomic.AddInt64(&completed, 1)), restarts)
		}
	})

	// Reduce in restart order so the winner is deterministic.
	var best partial
	var bestScore resultScore
	have := false
	for _, o := range outs {
		if !o.have {
			continue
		}
		if !have || o.score.better(bestScore) {
			best, bestScore, have = o.sol, o.score, true
		}
	}
	if !have {
		return pack.Result{}
	}
	if opts.DecodeFactory != nil {
		return finalDecode(factory, best, items)
	}
	return best.result()
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
