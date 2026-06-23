package packapi

import (
	"context"

	"github.com/W-Floyd/go-pack-bins/pack"
)

// This file holds the dispatch registry: the per-mode algorithm switches in
// pack1D/2D/3D are replaced by registered solve funcs, so an algorithm's dispatch
// lives in one place (its registration in algos_<mode>.go) instead of a switch
// case. The cross-cutting request modifiers that aren't a single algorithm —
// balance-objective layering — stay as pre-checks in the pack* wrappers.

// solveCtx carries the shared per-mode setup a registered solver needs: the request,
// the constrained bin factory, the converted items, and the bin dimensions. pack1D/
// 2D/3D build it once, then dispatch to the algorithm's registered solve func.
type solveCtx struct {
	ctx        context.Context
	req        PackRequest
	cap        float64 // 1-D capacity (== bw)
	bw, bd, bh float64 // bin dims (2-D uses bw,bh; 1-D uses bw)
	items      []pack.Item
	factory    pack.BinFactory
}

// solveMeta carries the non-placement extras a solve produces: the winning packer
// name (auto/lex) and the GBPP net-cost figures. Empty for ordinary algorithms.
type solveMeta struct {
	bestPacker     string
	netCost        float64
	includedProfit float64
	rejected       []string
}

// solveFn runs one algorithm against a prepared context, returning the packing, any
// extras, and an error. ErrItemTooLarge is tolerated by callers as a partial pack.
type solveFn func(sc *solveCtx) (pack.Result, solveMeta, error)

// solveReg maps "<mode>/<algo>" to its solver, registered from the algos_*.go
// init()s. Adding an algorithm means registering it here and advertising it in
// AlgoCapabilities; the drift test (TestAdvertisedAlgosSolve) keeps the two in sync.
var solveReg = map[string]solveFn{}

func registerSolve(mode, algo string, fn solveFn) { solveReg[mode+"/"+algo] = fn }

// lookupSolve returns the solver for (mode, algo), falling back to the mode's
// First-Fit default for an unknown/empty algorithm (matching the old switch default).
func lookupSolve(mode, algo string) solveFn {
	if fn, ok := solveReg[mode+"/"+algo]; ok {
		return fn
	}
	return solveReg[mode+"/ff"]
}
