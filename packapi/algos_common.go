package packapi

import (
	"github.com/W-Floyd/go-pack-bins/gbpp"
	"github.com/W-Floyd/go-pack-bins/offline"
	"github.com/W-Floyd/go-pack-bins/online"
	"github.com/W-Floyd/go-pack-bins/pack"
)

// Shared solve adapters and mode-independent solvers, reused by the per-mode
// registrations in algos_1d/2d/3d.go. The online/offline selectors and the
// order-search metaheuristics operate purely on the prepared solveCtx (factory +
// items), so a single definition serves every mode — only the factory the wrapper
// builds differs.

// onlineSolve adapts an online packer (built from the context) into a solveFn.
func onlineSolve(mk func(sc *solveCtx) pack.OnlinePacker) solveFn {
	return func(sc *solveCtx) (pack.Result, solveMeta, error) {
		r, e := runOnline(sc.ctx, mk(sc), sc.items)
		return r, solveMeta{}, e
	}
}

// offlineSolve adapts an offline packer (built from the context) into a solveFn.
func offlineSolve(mk func(sc *solveCtx) pack.OfflinePacker) solveFn {
	return func(sc *solveCtx) (pack.Result, solveMeta, error) {
		r, e := packAllCtx(sc.ctx, mk(sc), sc.items)
		return r, solveMeta{}, e
	}
}

func solveFF(sc *solveCtx) (pack.Result, solveMeta, error) {
	r, e := runOnline(sc.ctx, online.FirstFit(sc.factory), sc.items)
	return r, solveMeta{}, e
}

func solveBeam(sc *solveCtx) (pack.Result, solveMeta, error) {
	return offline.BeamSearch(sc.ctx, sc.items, sc.factory, sc.req.beamOptions(sc.ctx)), solveMeta{}, nil
}

func solveRR(sc *solveCtx) (pack.Result, solveMeta, error) {
	return offline.RuinRecreate(sc.ctx, sc.items, sc.factory, sc.req.searchOptions(sc.ctx)), solveMeta{}, nil
}

func solveARR(sc *solveCtx) (pack.Result, solveMeta, error) {
	return offline.AdaptiveRuinRecreate(sc.ctx, sc.items, sc.factory, sc.req.searchOptions(sc.ctx)), solveMeta{}, nil
}

func solveGRASP(sc *solveCtx) (pack.Result, solveMeta, error) {
	return offline.GRASP(sc.ctx, sc.items, sc.factory, sc.req.searchOptions(sc.ctx)), solveMeta{}, nil
}

func solveGBPP(sc *solveCtx) (pack.Result, solveMeta, error) {
	g := gbpp.Pack(sc.ctx, sc.items, sc.factory, gbpp.Options{BinCost: sc.req.BinCost, ProfitScalar: "profit", OptionalScalar: "profit"})
	return g.Result, solveMeta{netCost: g.NetCost, includedProfit: g.IncludedProfit, rejected: g.Rejected}, nil
}

func solveLex(sc *solveCtx) (pack.Result, solveMeta, error) {
	r, winner, e := lexResult(sc.ctx, sc.req, sc.factory, sc.items)
	return r, solveMeta{bestPacker: winner}, e
}
