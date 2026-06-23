package packapi

import (
	"context"

	"github.com/W-Floyd/go-pack-bins/d1"
	"github.com/W-Floyd/go-pack-bins/meta"
	"github.com/W-Floyd/go-pack-bins/offline"
	"github.com/W-Floyd/go-pack-bins/online"
	"github.com/W-Floyd/go-pack-bins/pack"
)

// 1-D algorithm dispatch. Each entry is the body of one former pack1D switch case,
// registered by id; pack1D builds the solveCtx and looks the solver up here.

func init() {
	reg := func(algo string, fn solveFn) { registerSolve("1d", algo, fn) }

	reg("ff", solveFF)
	reg("nf", onlineSolve(func(sc *solveCtx) pack.OnlinePacker { return online.NextFit(sc.factory) }))
	reg("nkf", onlineSolve(func(sc *solveCtx) pack.OnlinePacker { return online.NextKFit(3, sc.factory) }))
	reg("bf", onlineSolve(func(sc *solveCtx) pack.OnlinePacker { return online.BestFit(sc.factory) }))
	reg("wf", onlineSolve(func(sc *solveCtx) pack.OnlinePacker { return online.WorstFit(sc.factory) }))
	reg("awf", onlineSolve(func(sc *solveCtx) pack.OnlinePacker { return online.AlmostWorstFit(sc.factory) }))
	reg("rff", onlineSolve(func(sc *solveCtx) pack.OnlinePacker { return online.NewRFF(sc.cap, sc.factory) }))
	reg("hk", onlineSolve(func(sc *solveCtx) pack.OnlinePacker { return online.NewHarmonicK(11, sc.cap, sc.factory) }))
	reg("ss", onlineSolve(func(sc *solveCtx) pack.OnlinePacker { return online.SumOfSquares(sc.cap, sc.factory) }))

	reg("ffd", offlineSolve(func(sc *solveCtx) pack.OfflinePacker { return offline.FirstFitDecreasing(sc.factory) }))
	reg("bfd", offlineSolve(func(sc *solveCtx) pack.OfflinePacker { return offline.BestFitDecreasing(sc.factory) }))
	reg("nfd", offlineSolve(func(sc *solveCtx) pack.OfflinePacker { return offline.NextFitDecreasing(sc.factory) }))
	reg("wfd", offlineSolve(func(sc *solveCtx) pack.OfflinePacker { return offline.WorstFitDecreasing(sc.factory) }))
	reg("mffd", offlineSolve(func(sc *solveCtx) pack.OfflinePacker { return offline.ModifiedFirstFitDecreasing(sc.cap, sc.factory) }))

	reg("kk", func(sc *solveCtx) (pack.Result, solveMeta, error) {
		r, e := offline.KarmarkarKarpCtx(sc.ctx, sc.items, sc.cap, sc.factory)
		return r, solveMeta{}, e
	})
	reg("bc", func(sc *solveCtx) (pack.Result, solveMeta, error) {
		r, e := offline.BinCompletionCtx(sc.ctx, sc.items, sc.cap, d1.NewFactory(sc.cap), buildConstraints(sc.req.Constraints)...)
		return r, solveMeta{}, e
	})
	reg("brute", func(sc *solveCtx) (pack.Result, solveMeta, error) {
		r, e := offline.BruteForce(sc.ctx, sc.items, sc.factory, sc.req.bruteForceOptions(sc.ctx, shapeKey1D))
		return r, solveMeta{}, e
	})

	reg("beam", solveBeam)
	reg("rr", solveRR)
	reg("grasp", solveGRASP)
	reg("gbpp", solveGBPP)
	reg("lex", solveLex)

	reg("auto", func(sc *solveCtx) (pack.Result, solveMeta, error) {
		p := meta.BestOf(
			offline.FirstFitDecreasing(sc.factory),
			offline.BestFitDecreasing(sc.factory),
			offline.WorstFitDecreasing(sc.factory),
			offline.ModifiedFirstFitDecreasing(sc.cap, sc.factory),
			meta.NewFuncCtx("kk", func(ctx context.Context, it []pack.Item) (pack.Result, error) {
				return offline.KarmarkarKarpCtx(ctx, it, sc.cap, d1.NewFactory(sc.cap))
			}),
		)
		r, e := packAllCtx(sc.ctx, p, sc.items)
		return r, solveMeta{bestPacker: p.Winner()}, e
	})
}
