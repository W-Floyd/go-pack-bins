package packapi

import (
	"context"
	"errors"

	"github.com/W-Floyd/go-pack-bins/d2"
	"github.com/W-Floyd/go-pack-bins/meta"
	"github.com/W-Floyd/go-pack-bins/offline"
	"github.com/W-Floyd/go-pack-bins/online"
	"github.com/W-Floyd/go-pack-bins/pack"
	"github.com/W-Floyd/go-pack-bins/sat"
)

// 2-D algorithm dispatch. The factory's placement strategy is chosen by
// strat2DFor(algo) in pack2D, so ff/maxrects/guillotine/skyline and the shelf
// policies all run the same online/offline packer over the strategy-laden factory;
// only "auto" builds its own multi-strategy candidate set.

// shelf2D builds the decreasing-height shelf packer for nfdh/ffdh/bfdh; the shelf
// policy itself is baked into sc.factory's strategy (set by strat2DFor).
func shelf2D(algo string) solveFn {
	return offlineSolve(func(sc *solveCtx) pack.OfflinePacker {
		return offline.New(shelfLabel[algo], offline.DecreasingHeight, online.FirstFit(sc.factory))
	})
}

// solveSAT runs the SAT-based exact 2-D packer (package sat), which certifies the
// minimum bin count. Rotation follows each item's AllowRotate flag; symmetry
// breaking is on. The solver encodes its own geometry, so it cannot honour the
// scalar-aggregate constraints the constrained factory applies — a request with
// constraints is rejected rather than silently ignoring them. A grid too large to
// encode degrades to the heuristic packing (uncertified); non-integral dimensions
// are a hard error (the order-encoding needs an integer grid). The "time_limit_ms"
// tunable caps the search; on timeout the best feasible packing is returned
// uncertified.
func solveSAT(sc *solveCtx) (pack.Result, solveMeta, error) {
	if len(sc.req.Constraints) > 0 {
		return pack.Result{}, solveMeta{}, errors.New("sat: exact solver does not support constraints; choose another algorithm")
	}
	items := make([]*d2.Item2D, 0, len(sc.items))
	for _, it := range sc.items {
		if i2, ok := it.(*d2.Item2D); ok {
			items = append(items, i2)
		}
	}

	ctx := sc.ctx
	if d := sc.req.timeLimit(); d > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d)
		defer cancel()
	}

	// The "Max clauses / grid cells" tunables (in millions) let the UI trade memory
	// for exact certification. optInt returns 0 when absent (sat uses its package
	// defaults) and clamps to a safe ceiling so the guard can never be removed.
	r, err := sat.Pack2D(ctx, items, sc.bw, sc.bh, sat.Options{
		AllowRotation: true,
		SymmetryBreak: true,
		MaxClauses:    sc.req.optInt("sat_max_clauses", maxSATClauses),
		MaxGridCells:  sc.req.optInt("sat_max_grid_cells", maxSATGridCells),
	})
	m := solveMeta{optimal: r.Optimal, lowerBound: r.LowerBound, upperBound: r.UpperBound, proof: r.Proof}
	switch {
	case errors.Is(err, sat.ErrItemTooLarge):
		// Mirror the rest of the API: too-large items are a tolerated partial pack.
		return r.Result, m, pack.ErrItemTooLarge
	case errors.Is(err, sat.ErrGridTooLarge):
		// Degrade gracefully: heuristic packing, no certificate, no error surfaced.
		return r.Result, m, nil
	case err != nil:
		// ErrNonIntegral or context cancellation/deadline.
		return r.Result, m, err
	}
	return r.Result, m, nil
}

func init() {
	reg := func(algo string, fn solveFn) { registerSolve("2d", algo, fn) }

	// ff/maxrects/guillotine/skyline are all First-Fit; the strategy (MaxRects /
	// Guillotine / Skyline) is selected by strat2DFor(algo) into sc.factory.
	reg("ff", solveFF)
	reg("guillotine", solveFF)
	reg("skyline", solveFF)
	reg("nf", onlineSolve(func(sc *solveCtx) pack.OnlinePacker { return online.NextFit(sc.factory) }))
	reg("bf", onlineSolve(func(sc *solveCtx) pack.OnlinePacker { return online.BestFit(sc.factory) }))
	reg("wf", onlineSolve(func(sc *solveCtx) pack.OnlinePacker { return online.WorstFit(sc.factory) }))

	reg("ffd", offlineSolve(func(sc *solveCtx) pack.OfflinePacker { return offline.FirstFitDecreasing(sc.factory) }))
	reg("bfd", offlineSolve(func(sc *solveCtx) pack.OfflinePacker { return offline.BestFitDecreasing(sc.factory) }))
	reg("nfd", offlineSolve(func(sc *solveCtx) pack.OfflinePacker { return offline.NextFitDecreasing(sc.factory) }))

	reg("nfdh", shelf2D("nfdh"))
	reg("ffdh", shelf2D("ffdh"))
	reg("bfdh", shelf2D("bfdh"))

	reg("brute", func(sc *solveCtx) (pack.Result, solveMeta, error) {
		r, e := offline.BruteForce(sc.ctx, sc.items, sc.factory, sc.req.bruteForceOptions(sc.ctx, shapeKey2D))
		return r, solveMeta{}, e
	})
	reg("beam", solveBeam)
	reg("rr", solveRR)
	reg("grasp", solveGRASP)
	reg("gbpp", solveGBPP)
	reg("lex", solveLex)
	reg("sat", solveSAT)

	reg("auto", func(sc *solveCtx) (pack.Result, solveMeta, error) {
		mrFactory := constrainedFactory(d2.NewFactory(sc.bw, sc.bh, d2.NewMaxRectsDefault), sc.req.Constraints)
		gFactory := constrainedFactory(d2.NewFactory(sc.bw, sc.bh, d2.NewGuillotineDefault), sc.req.Constraints)
		skyFactory := constrainedFactory(d2.NewFactory(sc.bw, sc.bh, d2.NewSkylineDefault), sc.req.Constraints)
		p := meta.BestOf(
			offline.FirstFitDecreasing(mrFactory),
			offline.BestFitDecreasing(mrFactory),
			offline.NextFitDecreasing(mrFactory),
			offline.FirstFitDecreasing(gFactory),
			offline.BestFitDecreasing(gFactory),
			offline.FirstFitDecreasing(skyFactory),
		)
		r, e := packAllCtx(sc.ctx, p, sc.items)
		return r, solveMeta{bestPacker: p.Winner()}, e
	})
}
