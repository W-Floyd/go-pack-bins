package packapi

import (
	"context"
	"errors"

	"github.com/W-Floyd/go-pack-bins/d2"
	"github.com/W-Floyd/go-pack-bins/knapsack"
	"github.com/W-Floyd/go-pack-bins/meta"
	"github.com/W-Floyd/go-pack-bins/offline"
	"github.com/W-Floyd/go-pack-bins/online"
	"github.com/W-Floyd/go-pack-bins/pack"
	"github.com/W-Floyd/go-pack-bins/sat"
	"github.com/W-Floyd/go-pack-bins/strip"
)

// solveStrip2D minimises the strip height for the fixed bin width, building its
// own (open-height) container — so it cannot apply the scalar-aggregate
// constraints the constrained factory would, and rejects a request with any.
// The achieved height is reported via solveMeta.extent.
func solveStrip2D(sc *solveCtx) (pack.Result, solveMeta, error) {
	if len(sc.req.Constraints) > 0 {
		return pack.Result{}, solveMeta{}, errors.New("strip: open-height packing cannot apply constraints; choose another algorithm")
	}
	items := make([]*d2.Item2D, 0, len(sc.items))
	for _, it := range sc.items {
		if i2, ok := it.(*d2.Item2D); ok {
			items = append(items, i2)
		}
	}
	r := strip.Pack2D(items, sc.bw)
	return r.Result, solveMeta{extent: r.Height}, nil
}

// solveKnapsack2D packs the most valuable subset into a single container (from
// the constrained factory, so constraints are honoured). Item value is the
// "value" scalar, defaulting to area. Rejected items are reported.
func solveKnapsack2D(sc *solveCtx) (pack.Result, solveMeta, error) {
	bin := sc.factory.Open()
	r := knapsack.Pack(sc.ctx, sc.items, bin, knapsack.Options{})
	return r.Result, solveMeta{totalValue: r.TotalValue, rejected: r.Rejected}, nil
}

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

	// The "Memory budget (MB)" tunable lets the UI trade RAM for exact certification.
	// Derive the solver's clause/var caps from it via the measured ≈250 bytes/clause
	// (vars are cheaper, so reusing the same per-unit cap for them is conservative).
	// Unset → the default budget; otherwise the user's value is honoured as-is and
	// NOT clamped — if they want to push their luck with a huge budget, that's on them.
	mb := defaultSATMemoryMB
	if v := sc.req.AlgorithmOptions["sat_max_memory_mb"]; v >= 1 {
		mb = int(v)
	}
	budgetCap := int(int64(mb) << 20 / satBytesPerClause)
	opts := sat.Options{AllowRotation: true, SymmetryBreak: true, MaxClauses: budgetCap, MaxGridCells: budgetCap}
	r, err := sat.Pack2D(ctx, items, sc.bw, sc.bh, opts)
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
	reg("strip", solveStrip2D)
	reg("knapsack", solveKnapsack2D)

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
