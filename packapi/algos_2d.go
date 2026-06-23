package packapi

import (
	"github.com/W-Floyd/go-pack-bins/d2"
	"github.com/W-Floyd/go-pack-bins/meta"
	"github.com/W-Floyd/go-pack-bins/offline"
	"github.com/W-Floyd/go-pack-bins/online"
	"github.com/W-Floyd/go-pack-bins/pack"
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
